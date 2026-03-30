package syncer

import (
	"context"
	"fmt"
	"log"
	"net/netip"
	"sort"
	"strconv"
	"strings"

	"porkbun-dns/internal/config"
	"porkbun-dns/internal/porkbun"
	"porkbun-dns/internal/tailscale"
)

type tailscaleSource interface {
	Status(context.Context) ([]tailscale.Node, error)
}

type PublicIPSource interface {
	IPv4(context.Context) (netip.Addr, error)
}

type PublicIPv6Source interface {
	IPv6(context.Context) (netip.Addr, error)
}

type dnsClient interface {
	ListRecords(context.Context, string) ([]porkbun.Record, error)
	CreateRecord(context.Context, string, porkbun.Record) error
	EditRecord(context.Context, string, porkbun.Record) error
	DeleteRecord(context.Context, string, string) error
}

type Service struct {
	tailscale tailscaleSource
	publicIP4 PublicIPSource
	publicIP6 PublicIPv6Source
	client    dnsClient
	cfg       config.Config
}

type Result struct {
	Desired   int `json:"desired"`
	Unchanged int `json:"unchanged"`
	Created   int `json:"created"`
	Updated   int `json:"updated"`
	Deleted   int `json:"deleted"`
}

type DesiredRecord struct {
	Name          string
	Type          string
	Values        []string
	TTL           string
	Scope         string
	Owner         string
	SourceOfTruth string
	Targets       []string
}

func New(ts tailscaleSource, publicIP4 PublicIPSource, publicIP6 PublicIPv6Source, client dnsClient, cfg config.Config) *Service {
	return &Service{
		tailscale: ts,
		publicIP4: publicIP4,
		publicIP6: publicIP6,
		client:    client,
		cfg:       cfg,
	}
}

func (s *Service) Run(ctx context.Context) (Result, error) {
	result := Result{}
	desiredA, desiredAAAA, err := s.desiredState(ctx)
	if err != nil {
		return Result{}, err
	}

	managedNamesA := make(map[string]struct{}, 2)
	for name := range desiredA {
		if name == "" || name == "*" {
			managedNamesA[name] = struct{}{}
		}
	}

	aResult, err := s.reconcileType(ctx, desiredA, "A", managedNamesA)
	if err != nil {
		return Result{}, err
	}
	result = combineResults(result, aResult)

	if len(desiredAAAA) > 0 {
		managedNamesAAAA := make(map[string]struct{}, len(desiredAAAA))
		for name := range desiredAAAA {
			managedNamesAAAA[name] = struct{}{}
		}
		aaaaResult, err := s.reconcileType(ctx, desiredAAAA, "AAAA", managedNamesAAAA)
		if err != nil {
			return Result{}, err
		}
		result = combineResults(result, aaaaResult)
	}

	return result, nil
}

func (s *Service) DesiredRecords(ctx context.Context) ([]DesiredRecord, error) {
	desiredA, desiredAAAA, err := s.desiredState(ctx)
	if err != nil {
		return nil, err
	}

	records := make([]DesiredRecord, 0, len(desiredA)+len(desiredAAAA))
	for name, ip := range desiredA {
		record := DesiredRecord{
			Name:          name,
			Type:          "A",
			Values:        []string{ip.String()},
			TTL:           strconv.Itoa(s.cfg.TTL),
			Scope:         "public",
			Owner:         ownerForRecord(name),
			SourceOfTruth: sourceOfTruthForRecord(name),
			Targets:       []string{"porkbun"},
		}
		records = append(records, record)
	}

	for name, ip := range desiredAAAA {
		record := DesiredRecord{
			Name:          name,
			Type:          "AAAA",
			Values:        []string{ip.String()},
			TTL:           strconv.Itoa(s.cfg.TTL),
			Scope:         "public",
			Owner:         "derived",
			SourceOfTruth: "porkbun-dns",
			Targets:       []string{"porkbun"},
		}
		records = append(records, record)
	}

	sort.Slice(records, func(i, j int) bool {
		if records[i].Name == records[j].Name {
			return records[i].Type < records[j].Type
		}
		return records[i].Name < records[j].Name
	})

	return records, nil
}

func (s *Service) desiredState(ctx context.Context) (map[string]netip.Addr, map[string]netip.Addr, error) {
	desiredA := make(map[string]netip.Addr)

	nodes, err := s.tailscale.Status(ctx)
	if err != nil {
		return nil, nil, err
	}
	for _, node := range nodes {
		desiredA[recordName(node.Name, s.cfg.SubdomainSuffix)] = node.IPv4
	}

	if s.cfg.PublicIPEnabled {
		if s.publicIP4 == nil {
			return nil, nil, fmt.Errorf("public IP sync enabled without a public IP source")
		}

		addr, err := s.publicIP4.IPv4(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("lookup public IPv4: %w", err)
		}

		for _, name := range []string{"", "*"} {
			desiredA[name] = addr
		}
	}

	desiredAAAA := make(map[string]netip.Addr)
	if s.cfg.PublicIPv6Enabled {
		if s.publicIP6 == nil {
			return nil, nil, fmt.Errorf("public IPv6 sync enabled without a public IPv6 source")
		}

		addr, err := s.publicIP6.IPv6(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("lookup public IPv6: %w", err)
		}

		for _, name := range s.cfg.PublicIPv6RecordNames {
			desiredAAAA[name] = addr
		}
	}

	return desiredA, desiredAAAA, nil
}

func (s *Service) reconcileType(ctx context.Context, desired map[string]netip.Addr, recordType string, managedNames map[string]struct{}) (Result, error) {
	records, err := s.client.ListRecords(ctx, s.cfg.Domain)
	if err != nil {
		return Result{}, err
	}

	filtered := filterManagedRecords(records, recordType, s.cfg.Domain, s.cfg.SubdomainSuffix, managedNames)
	result := Result{Desired: len(desired)}

	seen := make(map[string]bool, len(filtered))
	for name, existing := range filtered {
		wantIP, ok := desired[name]
		seen[name] = true
		if !ok {
			for _, record := range existing {
				if s.cfg.DryRun {
					log.Printf("dry-run delete %s -> %s", record.Name, record.Content)
					result.Deleted++
					continue
				}
				if err := s.client.DeleteRecord(ctx, s.cfg.Domain, record.ID); err != nil {
					return Result{}, fmt.Errorf("delete %s: %w", name, err)
				}
				log.Printf("deleted %s -> %s", record.Name, record.Content)
				result.Deleted++
			}
			continue
		}

		if err := s.reconcileExisting(ctx, name, recordType, wantIP, existing, &result); err != nil {
			return Result{}, err
		}
	}

	for name, ip := range desired {
		if seen[name] {
			continue
		}
		record := porkbun.Record{
			Name:    name,
			Type:    recordType,
			Content: ip.String(),
			TTL:     strconv.Itoa(s.cfg.TTL),
			Prio:    "0",
		}
		if s.cfg.DryRun {
			log.Printf("dry-run create %s -> %s", displayName(record.Name), record.Content)
			result.Created++
			continue
		}
		if err := s.client.CreateRecord(ctx, s.cfg.Domain, record); err != nil {
			return Result{}, fmt.Errorf("create %s: %w", name, err)
		}
		log.Printf("created %s -> %s", displayName(record.Name), record.Content)
		result.Created++
	}

	return result, nil
}

func (s *Service) reconcileExisting(ctx context.Context, name, recordType string, wantIP netip.Addr, existing []porkbun.Record, result *Result) error {
	primary := existing[0]
	needsUpdate := primary.Content != wantIP.String() || primary.TTL != strconv.Itoa(s.cfg.TTL)

	if !needsUpdate && len(existing) == 1 {
		result.Unchanged++
		return nil
	}

	primary.Name = name
	primary.Type = recordType
	primary.Content = wantIP.String()
	primary.TTL = strconv.Itoa(s.cfg.TTL)
	primary.Prio = "0"

	if s.cfg.DryRun {
		if needsUpdate {
			log.Printf("dry-run update %s -> %s", displayName(name), wantIP)
			result.Updated++
		} else {
			result.Unchanged++
		}
	} else if needsUpdate {
		if err := s.client.EditRecord(ctx, s.cfg.Domain, primary); err != nil {
			return fmt.Errorf("update %s: %w", name, err)
		}
		log.Printf("updated %s -> %s", displayName(name), wantIP)
		result.Updated++
	} else {
		result.Unchanged++
	}

	for _, duplicate := range existing[1:] {
		if s.cfg.DryRun {
			log.Printf("dry-run delete duplicate %s (%s)", displayName(duplicate.Name), duplicate.Content)
			result.Deleted++
			continue
		}
		if err := s.client.DeleteRecord(ctx, s.cfg.Domain, duplicate.ID); err != nil {
			return fmt.Errorf("delete duplicate %s: %w", name, err)
		}
		log.Printf("deleted duplicate %s (%s)", displayName(duplicate.Name), duplicate.Content)
		result.Deleted++
	}

	return nil
}

func filterManagedRecords(records []porkbun.Record, recordType, domain, subdomain string, exact map[string]struct{}) map[string][]porkbun.Record {
	managed := make(map[string][]porkbun.Record)
	for _, record := range records {
		if strings.ToUpper(record.Type) != strings.ToUpper(recordType) {
			continue
		}
		name := normalizeRecordName(record.Name, domain)
		if _, ok := exact[name]; ok {
			managed[name] = append(managed[name], record)
			continue
		}
		if name == "" || !strings.HasSuffix(name, "."+subdomain) {
			continue
		}
		managed[name] = append(managed[name], record)
	}
	return managed
}

func combineResults(a, b Result) Result {
	return Result{
		Desired:   a.Desired + b.Desired,
		Unchanged: a.Unchanged + b.Unchanged,
		Created:   a.Created + b.Created,
		Updated:   a.Updated + b.Updated,
		Deleted:   a.Deleted + b.Deleted,
	}
}

func normalizeRecordName(name, domain string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	name = strings.Trim(name, ".")
	domain = strings.Trim(strings.ToLower(domain), ".")

	switch {
	case name == domain:
		return ""
	case strings.HasSuffix(name, "."+domain):
		return strings.TrimSuffix(name, "."+domain)
	default:
		return name
	}
}

func recordName(hostname, subdomain string) string {
	return strings.Trim(strings.ToLower(hostname), ".") + "." + strings.Trim(strings.ToLower(subdomain), ".")
}

func displayName(name string) string {
	if strings.TrimSpace(name) == "" {
		return "@"
	}
	return name
}

func ownerForRecord(name string) string {
	return "derived"
}

func sourceOfTruthForRecord(name string) string {
	switch name {
	case "", "*":
		return "public-ip"
	default:
		return "tailscale"
	}
}

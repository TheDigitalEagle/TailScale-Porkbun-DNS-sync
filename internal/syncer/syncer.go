package syncer

import (
	"context"
	"fmt"
	"log"
	"net/netip"
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

type dnsClient interface {
	ListRecords(context.Context, string) ([]porkbun.Record, error)
	CreateRecord(context.Context, string, porkbun.Record) error
	EditRecord(context.Context, string, porkbun.Record) error
	DeleteRecord(context.Context, string, string) error
}

type Service struct {
	tailscale tailscaleSource
	publicIP  PublicIPSource
	client    dnsClient
	cfg       config.Config
}

type Result struct {
	Desired   int
	Unchanged int
	Created   int
	Updated   int
	Deleted   int
}

func New(ts tailscaleSource, publicIP PublicIPSource, client dnsClient, cfg config.Config) *Service {
	return &Service{
		tailscale: ts,
		publicIP:  publicIP,
		client:    client,
		cfg:       cfg,
	}
}

func (s *Service) Run(ctx context.Context) (Result, error) {
	desired := make(map[string]netip.Addr)

	nodes, err := s.tailscale.Status(ctx)
	if err != nil {
		return Result{}, err
	}
	for _, node := range nodes {
		desired[recordName(node.Name, s.cfg.SubdomainSuffix)] = node.IPv4
	}

	managedNames := make(map[string]struct{}, 2)
	if s.cfg.PublicIPEnabled {
		if s.publicIP == nil {
			return Result{}, fmt.Errorf("public IP sync enabled without a public IP source")
		}

		addr, err := s.publicIP.IPv4(ctx)
		if err != nil {
			return Result{}, fmt.Errorf("lookup public IPv4: %w", err)
		}

		for _, name := range []string{"", "*"} {
			desired[name] = addr
			managedNames[name] = struct{}{}
		}
	}

	records, err := s.client.ListRecords(ctx, s.cfg.Domain)
	if err != nil {
		return Result{}, err
	}

	filtered := filterManagedRecords(records, s.cfg.Domain, s.cfg.SubdomainSuffix, managedNames)
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

		if err := s.reconcileExisting(ctx, name, wantIP, existing, &result); err != nil {
			return Result{}, err
		}
	}

	for name, ip := range desired {
		if seen[name] {
			continue
		}
		record := porkbun.Record{
			Name:    name,
			Type:    s.cfg.RecordType,
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

func (s *Service) reconcileExisting(ctx context.Context, name string, wantIP netip.Addr, existing []porkbun.Record, result *Result) error {
	primary := existing[0]
	needsUpdate := primary.Content != wantIP.String() || primary.TTL != strconv.Itoa(s.cfg.TTL)

	if !needsUpdate && len(existing) == 1 {
		result.Unchanged++
		return nil
	}

	primary.Name = name
	primary.Type = s.cfg.RecordType
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

func filterManagedRecords(records []porkbun.Record, domain, subdomain string, exact map[string]struct{}) map[string][]porkbun.Record {
	managed := make(map[string][]porkbun.Record)
	for _, record := range records {
		if strings.ToUpper(record.Type) != "A" {
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

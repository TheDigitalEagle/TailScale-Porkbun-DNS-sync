package control

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"porkbun-dns/internal/model"
	"porkbun-dns/internal/porkbun"
	caddyprovider "porkbun-dns/internal/providers/caddy"
	piholeprovider "porkbun-dns/internal/providers/pihole"
)

type store interface {
	List(context.Context) ([]model.Entry, error)
	Get(context.Context, string) (model.Entry, bool, error)
	Put(context.Context, model.Entry) error
	Delete(context.Context, string) (model.Entry, bool, error)
}

type publicDNS interface {
	ListRecords(context.Context, string) ([]porkbun.Record, error)
	CreateRecord(context.Context, string, porkbun.Record) error
	EditRecord(context.Context, string, porkbun.Record) error
	DeleteRecord(context.Context, string, string) error
}

type Service struct {
	store  store
	public publicDNS
	pihole *piholeprovider.Client
	caddy  *caddyprovider.Client
	domain string
}

func New(store store, public publicDNS, pihole *piholeprovider.Client, caddy *caddyprovider.Client, domain string) *Service {
	return &Service{
		store:  store,
		public: public,
		pihole: pihole,
		caddy:  caddy,
		domain: domain,
	}
}

func (s *Service) Entries(ctx context.Context) ([]model.Entry, error) {
	return s.store.List(ctx)
}

func (s *Service) SaveEntry(ctx context.Context, entry model.Entry) error {
	entry = normalizeEntry(entry)
	if err := validateEntry(entry); err != nil {
		return err
	}
	return s.store.Put(ctx, entry)
}

func (s *Service) PreviewEntry(ctx context.Context, entry model.Entry) ([]model.Change, error) {
	entry = normalizeEntry(entry)
	if err := validateEntry(entry); err != nil {
		return nil, err
	}

	var changes []model.Change
	publicChanges, err := s.previewPublic(ctx, entry)
	if err != nil {
		return nil, err
	}
	changes = append(changes, publicChanges...)

	localChanges, err := s.previewLocal(ctx, entry)
	if err != nil {
		return nil, err
	}
	changes = append(changes, localChanges...)

	ingressChanges, err := s.previewIngress(ctx, entry)
	if err != nil {
		return nil, err
	}
	changes = append(changes, ingressChanges...)

	sort.Slice(changes, func(i, j int) bool {
		if changes[i].Target == changes[j].Target {
			if changes[i].Scope == changes[j].Scope {
				if changes[i].Name == changes[j].Name {
					return changes[i].Type < changes[j].Type
				}
				return changes[i].Name < changes[j].Name
			}
			return changes[i].Scope < changes[j].Scope
		}
		return changes[i].Target < changes[j].Target
	})

	return changes, nil
}

func (s *Service) ApplyEntry(ctx context.Context, entry model.Entry) ([]model.Change, error) {
	entry = normalizeEntry(entry)
	if err := validateEntry(entry); err != nil {
		return nil, err
	}

	changes, err := s.PreviewEntry(ctx, entry)
	if err != nil {
		return nil, err
	}

	if len(entry.Public) > 0 {
		if err := s.applyPublic(ctx, entry); err != nil {
			return nil, err
		}
	}
	if len(entry.Local) > 0 && s.pihole != nil {
		if err := s.pihole.UpsertEntry(ctx, entry); err != nil {
			return nil, err
		}
	}
	if entry.HTTP != nil && entry.HTTP.Enabled && s.caddy != nil {
		if err := s.caddy.UpsertRoute(ctx, caddyprovider.Route{
			Host:           entry.Name,
			Upstream:       entry.HTTP.Upstream,
			TLSImport:      entry.HTTP.TLSImport,
			RootRedirectTo: entry.HTTP.RootRedirectTo,
		}); err != nil {
			return nil, err
		}
	}

	if err := s.store.Put(ctx, entry); err != nil {
		return nil, err
	}

	return changes, nil
}

func (s *Service) ApplyStored(ctx context.Context) ([]model.Change, error) {
	entries, err := s.store.List(ctx)
	if err != nil {
		return nil, err
	}

	all := make([]model.Change, 0)
	for _, entry := range entries {
		changes, err := s.ApplyEntry(ctx, entry)
		if err != nil {
			return nil, err
		}
		all = append(all, changes...)
	}
	return all, nil
}

func (s *Service) DeleteEntry(ctx context.Context, name string) ([]model.Change, bool, error) {
	entry, ok, err := s.store.Get(ctx, name)
	if err != nil || !ok {
		return nil, ok, err
	}

	changes, err := s.previewDelete(ctx, entry)
	if err != nil {
		return nil, true, err
	}

	if len(entry.Public) > 0 {
		if err := s.deletePublic(ctx, entry); err != nil {
			return nil, true, err
		}
	}
	if len(entry.Local) > 0 && s.pihole != nil {
		if err := s.pihole.DeleteEntry(ctx, entry.Name); err != nil {
			return nil, true, err
		}
	}
	if entry.HTTP != nil && entry.HTTP.Enabled && s.caddy != nil {
		if err := s.caddy.DeleteRoute(ctx, entry.Name); err != nil {
			return nil, true, err
		}
	}

	if _, ok, err := s.store.Delete(ctx, name); err != nil {
		return nil, true, err
	} else if !ok {
		return nil, true, fmt.Errorf("entry disappeared during delete")
	}

	return changes, true, nil
}

func (s *Service) previewDelete(ctx context.Context, entry model.Entry) ([]model.Change, error) {
	changes := make([]model.Change, 0)
	for _, record := range entry.Public {
		for _, value := range record.Values {
			changes = append(changes, model.Change{
				Target: "porkbun",
				Scope:  "public",
				Name:   entry.Name,
				Type:   record.Type,
				Action: "delete",
				Before: value,
			})
		}
	}
	for _, record := range entry.Local {
		for _, value := range record.Values {
			changes = append(changes, model.Change{
				Target: "pihole",
				Scope:  "local",
				Name:   entry.Name,
				Type:   record.Type,
				Action: "delete",
				Before: value,
			})
		}
	}
	if entry.HTTP != nil && entry.HTTP.Enabled {
		changes = append(changes, model.Change{
			Target: "caddy",
			Scope:  "ingress",
			Name:   entry.Name,
			Type:   "HTTP_PROXY",
			Action: "delete",
			Before: entry.HTTP.Upstream,
		})
	}
	return changes, nil
}

func (s *Service) previewPublic(ctx context.Context, entry model.Entry) ([]model.Change, error) {
	if len(entry.Public) == 0 {
		return nil, nil
	}
	records, err := s.public.ListRecords(ctx, s.domain)
	if err != nil {
		return nil, err
	}

	observed := observedPublicRecords(records, s.domain, entry.Name)
	changes := make([]model.Change, 0)
	for _, desired := range entry.Public {
		key := strings.ToUpper(desired.Type)
		current := observed[key]
		desiredValues := append([]string(nil), desired.Values...)
		sort.Strings(desiredValues)
		before := strings.Join(current.values, ", ")
		after := strings.Join(desiredValues, ", ")
		action := "noop"
		if len(current.values) == 0 {
			action = "create"
		} else if before != after || (desired.TTL != "" && current.ttl != desired.TTL) {
			action = "update"
		}

		changes = append(changes, model.Change{
			Target: "porkbun",
			Scope:  "public",
			Name:   entry.Name,
			Type:   key,
			Action: action,
			Before: before,
			After:  after,
		})
	}
	return changes, nil
}

func (s *Service) applyPublic(ctx context.Context, entry model.Entry) error {
	records, err := s.public.ListRecords(ctx, s.domain)
	if err != nil {
		return err
	}

	relativeName := relativeName(entry.Name, s.domain)
	byType := observedPublicRecords(records, s.domain, entry.Name)

	for _, desired := range entry.Public {
		key := strings.ToUpper(desired.Type)
		current := byType[key]
		value := firstValue(desired.Values)
		if value == "" {
			continue
		}

		record := porkbun.Record{
			Name:    relativeName,
			Type:    key,
			Content: value,
			TTL:     desiredTTL(desired),
			Prio:    "0",
		}

		if len(current.records) == 0 {
			if err := s.public.CreateRecord(ctx, s.domain, record); err != nil {
				return err
			}
			continue
		}

		record.ID = current.records[0].ID
		if err := s.public.EditRecord(ctx, s.domain, record); err != nil {
			return err
		}
		for _, duplicate := range current.records[1:] {
			if err := s.public.DeleteRecord(ctx, s.domain, duplicate.ID); err != nil {
				return err
			}
		}
	}

	return nil
}

func (s *Service) deletePublic(ctx context.Context, entry model.Entry) error {
	records, err := s.public.ListRecords(ctx, s.domain)
	if err != nil {
		return err
	}

	byType := observedPublicRecords(records, s.domain, entry.Name)
	for _, desired := range entry.Public {
		key := strings.ToUpper(desired.Type)
		for _, record := range byType[key].records {
			if err := s.public.DeleteRecord(ctx, s.domain, record.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Service) previewLocal(ctx context.Context, entry model.Entry) ([]model.Change, error) {
	if len(entry.Local) == 0 || s.pihole == nil {
		return nil, nil
	}

	records, err := s.pihole.LocalRecords(ctx)
	if err != nil {
		return nil, err
	}

	observed := observedModelRecords(records, entry.Name, "local")
	changes := make([]model.Change, 0)
	for _, desired := range entry.Local {
		key := strings.ToUpper(desired.Type)
		current := observed[key]
		before := strings.Join(current, ", ")
		after := strings.Join(desired.Values, ", ")
		action := "noop"
		if len(current) == 0 {
			action = "create"
		} else if before != after {
			action = "update"
		}
		changes = append(changes, model.Change{
			Target: "pihole",
			Scope:  "local",
			Name:   entry.Name,
			Type:   key,
			Action: action,
			Before: before,
			After:  after,
		})
	}
	return changes, nil
}

func (s *Service) previewIngress(ctx context.Context, entry model.Entry) ([]model.Change, error) {
	if entry.HTTP == nil || !entry.HTTP.Enabled || s.caddy == nil {
		return nil, nil
	}

	records, err := s.caddy.IngressRecords(ctx)
	if err != nil {
		return nil, err
	}

	current := observedModelRecords(records, entry.Name, "ingress")["HTTP_PROXY"]
	before := strings.Join(current, ", ")
	after := entry.HTTP.Upstream
	action := "noop"
	if len(current) == 0 {
		action = "create"
	} else if before != after {
		action = "update"
	}

	return []model.Change{{
		Target: "caddy",
		Scope:  "ingress",
		Name:   entry.Name,
		Type:   "HTTP_PROXY",
		Action: action,
		Before: before,
		After:  after,
	}}, nil
}

type observedPublic struct {
	values  []string
	ttl     string
	records []porkbun.Record
}

func observedPublicRecords(records []porkbun.Record, domain, recordFQDN string) map[string]observedPublic {
	observed := make(map[string]observedPublic)
	for _, record := range records {
		name := normalizePublicRecordName(record.Name, domain)
		if fqdn(name, domain) != fqdn(strings.ToLower(recordFQDN), domain) {
			continue
		}
		key := strings.ToUpper(record.Type)
		item := observed[key]
		item.values = append(item.values, record.Content)
		item.records = append(item.records, record)
		if item.ttl == "" {
			item.ttl = record.TTL
		}
		observed[key] = item
	}
	for key, item := range observed {
		sort.Strings(item.values)
		observed[key] = item
	}
	return observed
}

func observedModelRecords(records []model.Record, fqdn, scope string) map[string][]string {
	observed := make(map[string][]string)
	for _, record := range records {
		if strings.ToLower(record.FQDN) != strings.ToLower(fqdn) || record.Scope != scope {
			continue
		}
		values := append([]string(nil), record.ObservedValues...)
		sort.Strings(values)
		observed[strings.ToUpper(record.Type)] = values
	}
	return observed
}

func normalizeEntry(entry model.Entry) model.Entry {
	entry.Name = strings.Trim(strings.ToLower(entry.Name), ".")
	for i := range entry.Public {
		entry.Public[i].Type = strings.ToUpper(strings.TrimSpace(entry.Public[i].Type))
		sort.Strings(entry.Public[i].Values)
	}
	for i := range entry.Local {
		entry.Local[i].Type = strings.ToUpper(strings.TrimSpace(entry.Local[i].Type))
		sort.Strings(entry.Local[i].Values)
	}
	if entry.HTTP != nil {
		entry.HTTP.Upstream = strings.TrimSpace(entry.HTTP.Upstream)
		entry.HTTP.TLSImport = strings.TrimSpace(entry.HTTP.TLSImport)
		entry.HTTP.RootRedirectTo = strings.TrimSpace(entry.HTTP.RootRedirectTo)
	}
	return entry
}

func validateEntry(entry model.Entry) error {
	if entry.Name == "" {
		return fmt.Errorf("entry name is required")
	}
	if len(entry.Public) == 0 && len(entry.Local) == 0 && (entry.HTTP == nil || !entry.HTTP.Enabled) {
		return fmt.Errorf("entry must include at least one public, local, or http target")
	}
	return nil
}

func normalizePublicRecordName(name, domain string) string {
	name = strings.Trim(strings.ToLower(name), ".")
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

func relativeName(fqdn, domain string) string {
	name := strings.Trim(strings.ToLower(fqdn), ".")
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

func fqdn(name, domain string) string {
	if name == "" {
		return strings.Trim(strings.ToLower(domain), ".")
	}
	if strings.Contains(name, ".") && !strings.HasSuffix(name, "."+strings.Trim(strings.ToLower(domain), ".")) {
		return strings.Trim(strings.ToLower(name), ".")
	}
	if strings.HasSuffix(strings.ToLower(name), "."+strings.Trim(strings.ToLower(domain), ".")) {
		return strings.Trim(strings.ToLower(name), ".")
	}
	return strings.Trim(strings.ToLower(name), ".") + "." + strings.Trim(strings.ToLower(domain), ".")
}

func desiredTTL(record model.RecordSet) string {
	if strings.TrimSpace(record.TTL) != "" {
		return record.TTL
	}
	return strconv.Itoa(600)
}

func firstValue(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

package syncer

import (
	"context"
	"net/netip"
	"testing"

	"porkbun-dns/internal/config"
	"porkbun-dns/internal/porkbun"
	"porkbun-dns/internal/tailscale"
)

type fakeTailscale struct {
	nodes []tailscale.Node
}

func (f fakeTailscale) Status(context.Context) ([]tailscale.Node, error) {
	return f.nodes, nil
}

type fakePublicIP struct {
	addr netip.Addr
}

func (f fakePublicIP) IPv4(context.Context) (netip.Addr, error) {
	return f.addr, nil
}

type fakeClient struct {
	records []porkbun.Record
	created []porkbun.Record
	edited  []porkbun.Record
	deleted []string
}

func (f *fakeClient) ListRecords(context.Context, string) ([]porkbun.Record, error) {
	return f.records, nil
}

func (f *fakeClient) CreateRecord(_ context.Context, _ string, record porkbun.Record) error {
	f.created = append(f.created, record)
	return nil
}

func (f *fakeClient) EditRecord(_ context.Context, _ string, record porkbun.Record) error {
	f.edited = append(f.edited, record)
	return nil
}

func (f *fakeClient) DeleteRecord(_ context.Context, _ string, recordID string) error {
	f.deleted = append(f.deleted, recordID)
	return nil
}

func TestServiceRun(t *testing.T) {
	t.Parallel()

	client := &fakeClient{
		records: []porkbun.Record{
			{ID: "1", Name: "workstation.int", Type: "A", Content: "100.64.0.9", TTL: "600"},
			{ID: "2", Name: "tablet.int", Type: "A", Content: "100.64.0.3", TTL: "600"},
			{ID: "3", Name: "old-box.int", Type: "A", Content: "100.64.0.4", TTL: "600"},
		},
	}

	nodes := []tailscale.Node{
		{Name: "workstation", IPv4: netip.MustParseAddr("100.64.0.1")},
		{Name: "tablet", IPv4: netip.MustParseAddr("100.64.0.3")},
		{Name: "new-node", IPv4: netip.MustParseAddr("100.64.0.5")},
	}

	svc := New(fakeTailscale{nodes: nodes}, nil, nil, client, config.Config{
		Domain:          "ima.fish",
		SubdomainSuffix: "int",
		TTL:             600,
		RecordType:      "A",
	})

	result, err := svc.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if got, want := result.Created, 1; got != want {
		t.Fatalf("Created = %d, want %d", got, want)
	}
	if got, want := result.Updated, 1; got != want {
		t.Fatalf("Updated = %d, want %d", got, want)
	}
	if got, want := result.Deleted, 1; got != want {
		t.Fatalf("Deleted = %d, want %d", got, want)
	}
	if got, want := result.Unchanged, 1; got != want {
		t.Fatalf("Unchanged = %d, want %d", got, want)
	}

	if got, want := len(client.created), 1; got != want {
		t.Fatalf("len(created) = %d, want %d", got, want)
	}
	if got, want := client.created[0].Name, "new-node.int"; got != want {
		t.Fatalf("created[0].Name = %q, want %q", got, want)
	}
	if got, want := client.edited[0].Content, "100.64.0.1"; got != want {
		t.Fatalf("edited[0].Content = %q, want %q", got, want)
	}
	if got, want := client.deleted[0], "3"; got != want {
		t.Fatalf("deleted[0] = %q, want %q", got, want)
	}
}

func TestServiceRunWithPublicIPSync(t *testing.T) {
	t.Parallel()

	client := &fakeClient{
		records: []porkbun.Record{
			{ID: "1", Name: "workstation.int", Type: "A", Content: "100.64.0.9", TTL: "600"},
			{ID: "2", Name: "ima.fish", Type: "A", Content: "203.0.113.9", TTL: "600"},
			{ID: "3", Name: "*.ima.fish", Type: "A", Content: "203.0.113.10", TTL: "120"},
			{ID: "4", Name: "*.ima.fish", Type: "A", Content: "203.0.113.11", TTL: "600"},
			{ID: "5", Name: "www", Type: "A", Content: "203.0.113.15", TTL: "600"},
		},
	}

	svc := New(
		fakeTailscale{
			nodes: []tailscale.Node{
				{Name: "workstation", IPv4: netip.MustParseAddr("100.64.0.1")},
			},
		},
		fakePublicIP{addr: netip.MustParseAddr("198.51.100.20")},
		nil,
		client,
		config.Config{
			Domain:          "ima.fish",
			SubdomainSuffix: "int",
			TTL:             600,
			RecordType:      "A",
			PublicIPEnabled: true,
		},
	)

	result, err := svc.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if got, want := result.Desired, 3; got != want {
		t.Fatalf("Desired = %d, want %d", got, want)
	}
	if got, want := result.Updated, 3; got != want {
		t.Fatalf("Updated = %d, want %d", got, want)
	}
	if got, want := result.Deleted, 1; got != want {
		t.Fatalf("Deleted = %d, want %d", got, want)
	}
	if got := result.Created; got != 0 {
		t.Fatalf("Created = %d, want 0", got)
	}

	if got, want := len(client.edited), 3; got != want {
		t.Fatalf("len(edited) = %d, want %d", got, want)
	}

	editedByName := make(map[string]porkbun.Record, len(client.edited))
	for _, record := range client.edited {
		editedByName[record.Name] = record
	}

	if _, ok := editedByName[""]; !ok {
		t.Fatal("expected apex record update")
	}
	if _, ok := editedByName["*"]; !ok {
		t.Fatal("expected wildcard record update")
	}
	if got, want := client.deleted[0], "4"; got != want {
		t.Fatalf("deleted[0] = %q, want %q", got, want)
	}
	if got := len(client.created); got != 0 {
		t.Fatalf("len(created) = %d, want 0", got)
	}
}

func TestServiceRunWithPublicIPv6Sync(t *testing.T) {
	t.Parallel()

	client := &fakeClient{
		records: []porkbun.Record{
			{ID: "1", Name: "pihole.int", Type: "AAAA", Content: "2001:db8::10", TTL: "600"},
			{ID: "2", Name: "old.int", Type: "AAAA", Content: "2001:db8::11", TTL: "600"},
		},
	}

	svc := New(
		fakeTailscale{},
		nil,
		fakePublicIPv6{addr: netip.MustParseAddr("2001:db8::247")},
		client,
		config.Config{
			Domain:                "ima.fish",
			SubdomainSuffix:       "int",
			TTL:                   600,
			RecordType:            "A",
			PublicIPv6Enabled:     true,
			PublicIPv6RecordNames: []string{"pihole.int"},
		},
	)

	result, err := svc.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if got, want := result.Desired, 1; got != want {
		t.Fatalf("Desired = %d, want %d", got, want)
	}
	if got, want := result.Updated, 1; got != want {
		t.Fatalf("Updated = %d, want %d", got, want)
	}
	if got, want := result.Deleted, 1; got != want {
		t.Fatalf("Deleted = %d, want %d", got, want)
	}
	if got := result.Created; got != 0 {
		t.Fatalf("Created = %d, want 0", got)
	}
	if got, want := client.edited[0].Type, "AAAA"; got != want {
		t.Fatalf("edited[0].Type = %q, want %q", got, want)
	}
	if got, want := client.edited[0].Content, "2001:db8::247"; got != want {
		t.Fatalf("edited[0].Content = %q, want %q", got, want)
	}
	if got, want := client.deleted[0], "2"; got != want {
		t.Fatalf("deleted[0] = %q, want %q", got, want)
	}
}

func TestDesiredRecords(t *testing.T) {
	t.Parallel()

	svc := New(
		fakeTailscale{
			nodes: []tailscale.Node{
				{Name: "workstation", IPv4: netip.MustParseAddr("100.64.0.1")},
			},
		},
		fakePublicIP{addr: netip.MustParseAddr("198.51.100.20")},
		fakePublicIPv6{addr: netip.MustParseAddr("2001:db8::247")},
		&fakeClient{},
		config.Config{
			Domain:                "ima.fish",
			SubdomainSuffix:       "int",
			TTL:                   600,
			PublicIPEnabled:       true,
			PublicIPv6Enabled:     true,
			PublicIPv6RecordNames: []string{"pihole.int"},
		},
	)

	records, err := svc.DesiredRecords(context.Background())
	if err != nil {
		t.Fatalf("DesiredRecords() error = %v", err)
	}

	if got, want := len(records), 4; got != want {
		t.Fatalf("len(records) = %d, want %d", got, want)
	}
	if got, want := records[0].Name, ""; got != want {
		t.Fatalf("records[0].Name = %q, want %q", got, want)
	}
	if got, want := records[0].SourceOfTruth, "public-ip"; got != want {
		t.Fatalf("records[0].SourceOfTruth = %q, want %q", got, want)
	}
	if got, want := records[3].Name, "workstation.int"; got != want {
		t.Fatalf("records[3].Name = %q, want %q", got, want)
	}
	if got, want := records[3].SourceOfTruth, "tailscale"; got != want {
		t.Fatalf("records[3].SourceOfTruth = %q, want %q", got, want)
	}
}

type fakePublicIPv6 struct {
	addr netip.Addr
}

func (f fakePublicIPv6) IPv6(context.Context) (netip.Addr, error) {
	return f.addr, nil
}

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

	svc := New(fakeTailscale{nodes: nodes}, client, config.Config{
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

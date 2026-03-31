package caddy

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestIngressRecords(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "Caddyfile")
	if err := os.WriteFile(path, []byte(`example.com {
  reverse_proxy 127.0.0.1:8080
}

# managed-by: porkbun-dns
app.example.com {
  import tls_porkbun
  reverse_proxy 127.0.0.1:9000
}
`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	client := NewClient(path, "tls_porkbun")
	records, err := client.IngressRecords(context.Background())
	if err != nil {
		t.Fatalf("IngressRecords() error = %v", err)
	}
	if got, want := len(records), 2; got != want {
		t.Fatalf("len(records) = %d, want %d", got, want)
	}
	if got, want := records[0].FQDN, "app.example.com"; got != want {
		t.Fatalf("records[0].FQDN = %q, want %q", got, want)
	}
}

func TestUpsertAndDeleteRoute(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "Caddyfile")
	if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	client := NewClient(path, "tls_porkbun")
	if err := client.UpsertRoute(context.Background(), Route{
		Host:     "grafana.example.com",
		Upstream: "127.0.0.1:3000",
	}); err != nil {
		t.Fatalf("UpsertRoute() error = %v", err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if got := string(content); got == "" || got[0] == 0 {
		t.Fatal("expected non-empty managed block")
	}

	if err := client.DeleteRoute(context.Background(), "grafana.example.com"); err != nil {
		t.Fatalf("DeleteRoute() error = %v", err)
	}
}

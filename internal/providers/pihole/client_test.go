package pihole

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLocalRecords(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"session":{"sid":"test-sid"}}`))
		case "/config":
			if got, want := r.Header.Get("sid"), "test-sid"; got != want {
				t.Fatalf("sid header = %q, want %q", got, want)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"config":{"dns":{"hosts":["192.168.2.10 router.int.ima.fish router","fd00::10 nas.int.ima.fish"],"cnameRecords":["grafana.int.ima.fish,router.int.ima.fish"],"domain":{"name":"int.ima.fish","local":true},"expandHosts":false}}}`))
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "secret")
	records, err := client.LocalRecords(context.Background())
	if err != nil {
		t.Fatalf("LocalRecords() error = %v", err)
	}

	if got, want := len(records), 4; got != want {
		t.Fatalf("len(records) = %d, want %d", got, want)
	}
	if got, want := records[0].Scope, "local"; got != want {
		t.Fatalf("records[0].Scope = %q, want %q", got, want)
	}
	if got, want := records[0].SourceOfTruth, "pihole"; got != want {
		t.Fatalf("records[0].SourceOfTruth = %q, want %q", got, want)
	}
}

func TestNormalizeLocalName(t *testing.T) {
	t.Parallel()

	name, fqdn := normalizeLocalName("router", "int.ima.fish", true, true)
	if got, want := name, "router"; got != want {
		t.Fatalf("name = %q, want %q", got, want)
	}
	if got, want := fqdn, "router.int.ima.fish"; got != want {
		t.Fatalf("fqdn = %q, want %q", got, want)
	}
}

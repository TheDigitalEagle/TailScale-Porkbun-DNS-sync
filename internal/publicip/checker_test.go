package publicip

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCheckerIPv4(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s, want %s", r.Method, http.MethodGet)
		}
		_, _ = w.Write([]byte("198.51.100.8\n"))
	}))
	defer server.Close()

	checker := NewChecker(server.URL)
	addr, err := checker.IPv4(context.Background())
	if err != nil {
		t.Fatalf("IPv4() error = %v", err)
	}

	if got, want := addr.String(), "198.51.100.8"; got != want {
		t.Fatalf("IPv4() = %s, want %s", got, want)
	}
}

func TestCheckerIPv4RejectsNonIPv4(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("2001:db8::1"))
	}))
	defer server.Close()

	checker := NewChecker(server.URL)
	if _, err := checker.IPv4(context.Background()); err == nil {
		t.Fatal("IPv4() error = nil, want non-nil")
	}
}

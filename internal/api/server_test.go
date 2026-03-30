package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"porkbun-dns/internal/model"
	"porkbun-dns/internal/porkbun"
	"porkbun-dns/internal/syncer"
)

type fakeRunner struct {
	result syncer.Result
	err    error
	calls  int
}

func (f *fakeRunner) Run(context.Context) (syncer.Result, error) {
	f.calls++
	if f.err != nil {
		return syncer.Result{}, f.err
	}
	return f.result, nil
}

type fakeDesiredSource struct {
	records []syncer.DesiredRecord
	err     error
}

func (f fakeDesiredSource) DesiredRecords(context.Context) ([]syncer.DesiredRecord, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.records, nil
}

type fakeLister struct {
	records []porkbun.Record
	err     error
}

func (f fakeLister) ListRecords(context.Context, string) ([]porkbun.Record, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.records, nil
}

func TestHealthEndpoint(t *testing.T) {
	t.Parallel()

	server := NewServer(Config{Domain: "ima.fish"}, &fakeRunner{}, fakeDesiredSource{}, fakeLister{})
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	server.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if body := rec.Body.String(); !strings.Contains(body, `"status":"ok"`) {
		t.Fatalf("body = %q, want ok payload", body)
	}
}

func TestSyncRunEndpoint(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{result: syncer.Result{Created: 1, Desired: 1}}
	server := NewServer(Config{Domain: "ima.fish"}, runner, fakeDesiredSource{}, fakeLister{})
	req := httptest.NewRequest(http.MethodPost, "/sync/run", nil)
	rec := httptest.NewRecorder()

	server.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusAccepted)
	}
	if runner.calls != 1 {
		t.Fatalf("calls = %d, want 1", runner.calls)
	}
}

func TestSyncStatusEndpoint(t *testing.T) {
	t.Parallel()

	server := NewServer(Config{Domain: "ima.fish"}, &fakeRunner{}, fakeDesiredSource{}, fakeLister{})
	_, err := server.runSync(context.Background(), "manual")
	if err != nil {
		t.Fatalf("runSync() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/sync/status", nil)
	rec := httptest.NewRecorder()
	server.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var status SyncStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if status.LastTrigger != "manual" {
		t.Fatalf("LastTrigger = %q, want manual", status.LastTrigger)
	}
	if status.LastFinishedAt == nil {
		t.Fatal("LastFinishedAt = nil, want timestamp")
	}
}

func TestPublicRecordsEndpoint(t *testing.T) {
	t.Parallel()

	server := NewServer(Config{Domain: "ima.fish"}, &fakeRunner{}, fakeDesiredSource{
		records: []syncer.DesiredRecord{
			{Name: "pihole.int", Type: "AAAA", Values: []string{"2001:db8::2"}, TTL: "600", Scope: "public", Owner: "derived", SourceOfTruth: "porkbun-dns", Targets: []string{"porkbun"}},
			{Name: "", Type: "A", Values: []string{"198.51.100.10"}, TTL: "600", Scope: "public", Owner: "derived", SourceOfTruth: "public-ip", Targets: []string{"porkbun"}},
		},
	}, fakeLister{
		records: []porkbun.Record{
			{Name: "pihole.int.ima.fish", Type: "AAAA", Content: "2001:db8::2", TTL: "600"},
			{Name: "ima.fish", Type: "A", Content: "198.51.100.10", TTL: "600"},
			{Name: "www", Type: "A", Content: "198.51.100.11", TTL: "600"},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/records/public", nil)
	rec := httptest.NewRecorder()
	server.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var response struct {
		Records []model.Record `json:"records"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode records: %v", err)
	}
	if got, want := len(response.Records), 3; got != want {
		t.Fatalf("len(records) = %d, want %d", got, want)
	}
	if got, want := response.Records[0].FQDN, "ima.fish"; got != want {
		t.Fatalf("records[0].FQDN = %q, want %q", got, want)
	}
	if got, want := response.Records[0].Status, "in_sync"; got != want {
		t.Fatalf("records[0].Status = %q, want %q", got, want)
	}
	if got, want := response.Records[2].Status, "unmanaged"; got != want {
		t.Fatalf("records[2].Status = %q, want %q", got, want)
	}
}

func TestPublicRecordsEndpointError(t *testing.T) {
	t.Parallel()

	server := NewServer(Config{Domain: "ima.fish"}, &fakeRunner{}, fakeDesiredSource{}, fakeLister{
		err: errors.New("upstream failed"),
	})

	req := httptest.NewRequest(http.MethodGet, "/records/public", nil)
	rec := httptest.NewRecorder()
	server.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadGateway)
	}
}

func TestRecordsEndpoint(t *testing.T) {
	t.Parallel()

	server := NewServer(Config{Domain: "ima.fish"}, &fakeRunner{}, fakeDesiredSource{
		records: []syncer.DesiredRecord{
			{Name: "pihole.int", Type: "AAAA", Values: []string{"2001:db8::2"}, TTL: "600", Scope: "public", Owner: "derived", SourceOfTruth: "porkbun-dns", Targets: []string{"porkbun"}},
		},
	}, fakeLister{
		records: []porkbun.Record{
			{Name: "pihole.int.ima.fish", Type: "AAAA", Content: "2001:db8::3", TTL: "600"},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/records", nil)
	rec := httptest.NewRecorder()
	server.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var response struct {
		Records []model.Record `json:"records"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode records: %v", err)
	}
	if got, want := len(response.Records), 1; got != want {
		t.Fatalf("len(records) = %d, want %d", got, want)
	}
	if got, want := response.Records[0].Status, "drifted"; got != want {
		t.Fatalf("status = %q, want %q", got, want)
	}
	if got, want := response.Records[0].DesiredValues[0], "2001:db8::2"; got != want {
		t.Fatalf("desired_values[0] = %q, want %q", got, want)
	}
	if got, want := response.Records[0].ObservedValues[0], "2001:db8::3"; got != want {
		t.Fatalf("observed_values[0] = %q, want %q", got, want)
	}
}

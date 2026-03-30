package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"porkbun-dns/internal/model"
	"porkbun-dns/internal/porkbun"
	"porkbun-dns/internal/syncer"
)

type syncRunner interface {
	Run(context.Context) (syncer.Result, error)
}

type publicRecordLister interface {
	ListRecords(context.Context, string) ([]porkbun.Record, error)
}

type desiredRecordSource interface {
	DesiredRecords(context.Context) ([]syncer.DesiredRecord, error)
}

type Config struct {
	ListenAddr   string
	Domain       string
	SyncInterval time.Duration
}

type Server struct {
	cfg     Config
	runner  syncRunner
	desired desiredRecordSource
	lister  publicRecordLister

	mu     sync.Mutex
	status SyncStatus
}

type SyncStatus struct {
	Running        bool           `json:"running"`
	LastTrigger    string         `json:"last_trigger,omitempty"`
	LastStartedAt  *time.Time     `json:"last_started_at,omitempty"`
	LastFinishedAt *time.Time     `json:"last_finished_at,omitempty"`
	LastSuccessAt  *time.Time     `json:"last_success_at,omitempty"`
	LastError      string         `json:"last_error,omitempty"`
	LastResult     *syncer.Result `json:"last_result,omitempty"`
}

func NewServer(cfg Config, runner syncRunner, desired desiredRecordSource, lister publicRecordLister) *Server {
	return &Server{
		cfg:     cfg,
		runner:  runner,
		desired: desired,
		lister:  lister,
		status:  SyncStatus{},
	}
}

func (s *Server) Run(ctx context.Context) error {
	httpServer := &http.Server{
		Addr:    s.cfg.ListenAddr,
		Handler: s.routes(),
	}

	if s.cfg.SyncInterval > 0 {
		go s.runSchedule(ctx)
	}

	go func() {
		<-ctx.Done()

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := httpServer.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("api shutdown failed: %v", err)
		}
	}()

	log.Printf("api listening on %s", s.cfg.ListenAddr)
	err := httpServer.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("GET /records", s.handleRecords)
	mux.HandleFunc("GET /sync/status", s.handleSyncStatus)
	mux.HandleFunc("POST /sync/run", s.handleSyncRun)
	mux.HandleFunc("GET /records/public", s.handlePublicRecords)
	return mux
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	s.writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"mode":   "api",
	})
}

func (s *Server) handleSyncStatus(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	status := s.status
	s.mu.Unlock()

	s.writeJSON(w, http.StatusOK, status)
}

func (s *Server) handleSyncRun(w http.ResponseWriter, r *http.Request) {
	result, err := s.runSync(r.Context(), "manual")
	if err != nil {
		if errors.Is(err, errSyncAlreadyRunning) {
			s.writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}

		s.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	s.writeJSON(w, http.StatusAccepted, map[string]any{
		"status": "completed",
		"result": result,
	})
}

func (s *Server) handlePublicRecords(w http.ResponseWriter, r *http.Request) {
	records, err := s.publicRecords(r.Context())
	if err != nil {
		s.writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]any{
		"domain":  s.cfg.Domain,
		"records": groupPublicRecords(records, s.cfg.Domain),
	})
}

func (s *Server) handleRecords(w http.ResponseWriter, r *http.Request) {
	records, err := s.publicRecords(r.Context())
	if err != nil {
		s.writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]any{
		"records": records,
	})
}

var errSyncAlreadyRunning = errors.New("sync already running")

func (s *Server) runSchedule(ctx context.Context) {
	if _, err := s.runSync(ctx, "startup"); err != nil && !errors.Is(err, context.Canceled) {
		log.Printf("startup sync failed: %v", err)
	}

	ticker := time.NewTicker(s.cfg.SyncInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := s.runSync(ctx, "interval"); err != nil {
				if errors.Is(err, context.Canceled) {
					return
				}
				if errors.Is(err, errSyncAlreadyRunning) {
					log.Printf("scheduled sync skipped: %v", err)
					continue
				}
				log.Printf("scheduled sync failed: %v", err)
			}
		}
	}
}

func (s *Server) runSync(ctx context.Context, trigger string) (syncer.Result, error) {
	s.mu.Lock()
	if s.status.Running {
		s.mu.Unlock()
		return syncer.Result{}, errSyncAlreadyRunning
	}

	startedAt := time.Now().UTC()
	s.status.Running = true
	s.status.LastTrigger = trigger
	s.status.LastStartedAt = &startedAt
	s.status.LastError = ""
	s.mu.Unlock()

	result, err := s.runner.Run(ctx)

	s.mu.Lock()
	defer s.mu.Unlock()

	finishedAt := time.Now().UTC()
	s.status.Running = false
	s.status.LastFinishedAt = &finishedAt

	if err != nil {
		s.status.LastError = err.Error()
		return syncer.Result{}, err
	}

	s.status.LastResult = &result
	s.status.LastSuccessAt = &finishedAt
	return result, nil
}

func (s *Server) writeJSON(w http.ResponseWriter, statusCode int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("encode response failed: %v", err)
	}
}

func (s *Server) publicRecords(ctx context.Context) ([]model.Record, error) {
	desiredRecords, err := s.desired.DesiredRecords(ctx)
	if err != nil {
		return nil, err
	}

	observedRecords, err := s.lister.ListRecords(ctx, s.cfg.Domain)
	if err != nil {
		return nil, err
	}

	return buildRecordInventory(desiredRecords, observedRecords, s.cfg.Domain), nil
}

func groupPublicRecords(records []model.Record, _ string) []model.Record {
	return records
}

func buildRecordInventory(desired []syncer.DesiredRecord, observed []porkbun.Record, domain string) []model.Record {
	type key struct {
		name string
		typ  string
	}

	type observedRecord struct {
		values []string
		ttl    string
	}

	desiredByKey := make(map[key]syncer.DesiredRecord, len(desired))
	for _, record := range desired {
		record.Type = strings.ToUpper(record.Type)
		sort.Strings(record.Values)
		desiredByKey[key{name: record.Name, typ: record.Type}] = record
	}

	grouped := make(map[key]observedRecord)
	for _, record := range observed {
		name := normalizeRecordName(record.Name, domain)
		k := key{name: name, typ: strings.ToUpper(record.Type)}
		item := grouped[k]
		item.values = append(item.values, record.Content)
		if item.ttl == "" {
			item.ttl = record.TTL
		}
		grouped[k] = item
	}

	keys := make([]key, 0, len(desiredByKey)+len(grouped))
	seen := make(map[key]struct{}, len(desiredByKey)+len(grouped))
	for key := range desiredByKey {
		keys = append(keys, key)
		seen[key] = struct{}{}
	}
	for key := range grouped {
		if _, ok := seen[key]; ok {
			continue
		}
		keys = append(keys, key)
	}

	sort.Slice(keys, func(i, j int) bool {
		if fqdn(keys[i].name, domain) == fqdn(keys[j].name, domain) {
			return keys[i].typ < keys[j].typ
		}
		return fqdn(keys[i].name, domain) < fqdn(keys[j].name, domain)
	})

	out := make([]model.Record, 0, len(keys))
	for _, key := range keys {
		want, hasDesired := desiredByKey[key]
		have, hasObserved := grouped[key]

		record := model.Record{
			Name:    key.name,
			FQDN:    fqdn(key.name, domain),
			Type:    key.typ,
			Scope:   "public",
			Targets: []string{"porkbun"},
		}

		if hasDesired {
			record.Owner = want.Owner
			record.SourceOfTruth = want.SourceOfTruth
			record.DesiredValues = append([]string(nil), want.Values...)
			record.DesiredTTL = want.TTL
		} else {
			record.Owner = "provider-managed"
			record.SourceOfTruth = "porkbun"
		}

		if hasObserved {
			sort.Strings(have.values)
			record.ObservedValues = append([]string(nil), have.values...)
			record.ObservedTTL = have.ttl
		}

		switch {
		case !hasDesired:
			record.Status = "unmanaged"
		case !hasObserved:
			record.Status = "drifted"
		case record.DesiredTTL != "" && record.ObservedTTL != "" && record.DesiredTTL != record.ObservedTTL:
			record.Status = "drifted"
		case !sameStrings(record.DesiredValues, record.ObservedValues):
			record.Status = "drifted"
		default:
			record.Status = "in_sync"
		}

		out = append(out, record)
	}

	return out
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func normalizeRecordName(name, domain string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	name = strings.TrimSuffix(name, ".")
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
		return domain
	}
	return fmt.Sprintf("%s.%s", name, domain)
}

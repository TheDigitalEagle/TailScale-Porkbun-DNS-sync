package api

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"io/fs"
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

//go:embed web/*
var webFS embed.FS

type syncRunner interface {
	Run(context.Context) (syncer.Result, error)
}

type publicRecordLister interface {
	ListRecords(context.Context, string) ([]porkbun.Record, error)
}

type desiredRecordSource interface {
	DesiredRecords(context.Context) ([]syncer.DesiredRecord, error)
}

type localRecordSource interface {
	LocalRecords(context.Context) ([]model.Record, error)
}

type ingressRecordSource interface {
	IngressRecords(context.Context) ([]model.Record, error)
}

type controlPlane interface {
	Entries(context.Context) ([]model.Entry, error)
	SaveEntry(context.Context, model.Entry) error
	PreviewEntry(context.Context, model.Entry) ([]model.Change, error)
	ApplyEntry(context.Context, model.Entry) ([]model.Change, error)
	ApplyStored(context.Context) ([]model.Change, error)
	DeleteEntry(context.Context, string) ([]model.Change, bool, error)
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
	local   localRecordSource
	ingress ingressRecordSource
	control controlPlane

	mu               sync.Mutex
	status           SyncStatus
	recordCache      []model.Record
	recordCacheAt    time.Time
	recordCacheError error
}

const recordCacheTTL = 10 * time.Second

type SyncStatus struct {
	Running        bool           `json:"running"`
	LastTrigger    string         `json:"last_trigger,omitempty"`
	LastStartedAt  *time.Time     `json:"last_started_at,omitempty"`
	LastFinishedAt *time.Time     `json:"last_finished_at,omitempty"`
	LastSuccessAt  *time.Time     `json:"last_success_at,omitempty"`
	LastError      string         `json:"last_error,omitempty"`
	LastResult     *syncer.Result `json:"last_result,omitempty"`
}

func NewServer(cfg Config, runner syncRunner, desired desiredRecordSource, lister publicRecordLister, local localRecordSource, ingress ingressRecordSource, control controlPlane) *Server {
	return &Server{
		cfg:     cfg,
		runner:  runner,
		desired: desired,
		lister:  lister,
		local:   local,
		ingress: ingress,
		control: control,
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
	mux.HandleFunc("GET /records/public", s.handlePublicRecords)
	mux.HandleFunc("GET /records/local", s.handleLocalRecords)
	mux.HandleFunc("GET /records/ingress", s.handleIngressRecords)
	mux.HandleFunc("GET /entries", s.handleEntries)
	mux.HandleFunc("PUT /entries/{name}", s.handlePutEntry)
	mux.HandleFunc("DELETE /entries/{name}", s.handleDeleteEntry)
	mux.HandleFunc("POST /entries/preview", s.handlePreviewEntry)
	mux.HandleFunc("POST /entries/apply", s.handleApplyEntry)
	mux.HandleFunc("POST /apply", s.handleApplyStored)
	mux.HandleFunc("GET /sync/status", s.handleSyncStatus)
	mux.HandleFunc("POST /sync/run", s.handleSyncRun)

	sub, err := fs.Sub(webFS, "web")
	if err != nil {
		panic(err)
	}
	fileServer := http.FileServer(http.FS(sub))
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		s.serveWebAsset(w, "index.html", "text/html; charset=utf-8")
	})
	mux.HandleFunc("GET /index.html", func(w http.ResponseWriter, r *http.Request) {
		s.serveWebAsset(w, "index.html", "text/html; charset=utf-8")
	})
	mux.HandleFunc("GET /app.js", func(w http.ResponseWriter, r *http.Request) {
		s.serveWebAsset(w, "app.js", "text/javascript; charset=utf-8")
	})
	mux.HandleFunc("GET /styles.css", func(w http.ResponseWriter, r *http.Request) {
		s.serveWebAsset(w, "styles.css", "text/css; charset=utf-8")
	})
	mux.Handle("GET /assets/", fileServer)

	return mux
}

func (s *Server) serveWebAsset(w http.ResponseWriter, name, contentType string) {
	data, err := webFS.ReadFile("web/" + name)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", contentType)
	_, _ = w.Write(data)
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	s.writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "mode": "api"})
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
	s.invalidateRecordCache()
	s.writeJSON(w, http.StatusAccepted, map[string]any{"status": "completed", "result": result})
}

func (s *Server) handlePublicRecords(w http.ResponseWriter, r *http.Request) {
	records, err := s.publicRecords(r.Context())
	if err != nil {
		s.writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"domain": s.cfg.Domain, "records": records})
}

func (s *Server) handleLocalRecords(w http.ResponseWriter, r *http.Request) {
	records, err := s.localRecords(r.Context())
	if err != nil {
		s.writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"records": records})
}

func (s *Server) handleIngressRecords(w http.ResponseWriter, r *http.Request) {
	records, err := s.ingressRecords(r.Context())
	if err != nil {
		s.writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"records": records})
}

func (s *Server) handleRecords(w http.ResponseWriter, r *http.Request) {
	records, err := s.allRecords(r.Context())
	if err != nil {
		s.writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"records": records})
}

func (s *Server) handleEntries(w http.ResponseWriter, r *http.Request) {
	entries, err := s.control.Entries(r.Context())
	if err != nil {
		s.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"entries": entries})
}

func (s *Server) handlePutEntry(w http.ResponseWriter, r *http.Request) {
	var entry model.Entry
	if err := json.NewDecoder(r.Body).Decode(&entry); err != nil {
		s.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json body"})
		return
	}
	entry.Name = strings.Trim(strings.ToLower(r.PathValue("name")), ".")
	if err := s.control.SaveEntry(r.Context(), entry); err != nil {
		s.writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.invalidateRecordCache()
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}

func (s *Server) handleDeleteEntry(w http.ResponseWriter, r *http.Request) {
	name := strings.Trim(strings.ToLower(r.PathValue("name")), ".")
	changes, ok, err := s.control.DeleteEntry(r.Context(), name)
	if err != nil {
		s.writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	if !ok {
		s.writeJSON(w, http.StatusNotFound, map[string]string{"error": "entry not found"})
		return
	}
	s.invalidateRecordCache()
	s.writeJSON(w, http.StatusOK, map[string]any{"status": "deleted", "changes": changes})
}

func (s *Server) handlePreviewEntry(w http.ResponseWriter, r *http.Request) {
	var entry model.Entry
	if err := json.NewDecoder(r.Body).Decode(&entry); err != nil {
		s.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json body"})
		return
	}
	changes, err := s.control.PreviewEntry(r.Context(), entry)
	if err != nil {
		s.writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"changes": changes})
}

func (s *Server) handleApplyEntry(w http.ResponseWriter, r *http.Request) {
	var entry model.Entry
	if err := json.NewDecoder(r.Body).Decode(&entry); err != nil {
		s.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json body"})
		return
	}
	changes, err := s.control.ApplyEntry(r.Context(), entry)
	if err != nil {
		s.writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	s.invalidateRecordCache()
	s.writeJSON(w, http.StatusOK, map[string]any{"status": "applied", "changes": changes})
}

func (s *Server) handleApplyStored(w http.ResponseWriter, r *http.Request) {
	changes, err := s.control.ApplyStored(r.Context())
	if err != nil {
		s.writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	s.invalidateRecordCache()
	s.writeJSON(w, http.StatusOK, map[string]any{"status": "applied", "changes": changes})
}

var errSyncAlreadyRunning = errors.New("sync already running")

func (s *Server) runSchedule(ctx context.Context) {
	if err := s.waitForStartupReadiness(ctx); err != nil {
		log.Printf("startup readiness check failed: %v", err)
	}

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

func (s *Server) waitForStartupReadiness(ctx context.Context) error {
	if s.desired == nil {
		return nil
	}

	deadline := time.Now().Add(45 * time.Second)
	for {
		records, err := s.desired.DesiredRecords(ctx)
		if err == nil && hasTailscaleDerivedRecord(records) {
			return nil
		}

		if err != nil {
			log.Printf("startup readiness: desired record probe failed: %v", err)
		}

		if time.Now().After(deadline) {
			if err != nil {
				return err
			}
			return errors.New("timeout waiting for tailscale-derived records")
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
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

func (s *Server) localRecords(ctx context.Context) ([]model.Record, error) {
	if s.local == nil {
		return []model.Record{}, nil
	}
	return s.local.LocalRecords(ctx)
}

func (s *Server) ingressRecords(ctx context.Context) ([]model.Record, error) {
	if s.ingress == nil {
		return []model.Record{}, nil
	}
	return s.ingress.IngressRecords(ctx)
}

func (s *Server) allRecords(ctx context.Context) ([]model.Record, error) {
	if records, ok, err := s.cachedRecords(); ok {
		return records, err
	}

	type result struct {
		records []model.Record
		err     error
	}

	publicCh := make(chan result, 1)
	localCh := make(chan result, 1)
	ingressCh := make(chan result, 1)

	go func() {
		records, err := s.publicRecords(ctx)
		publicCh <- result{records: records, err: err}
	}()
	go func() {
		records, err := s.localRecords(ctx)
		localCh <- result{records: records, err: err}
	}()
	go func() {
		records, err := s.ingressRecords(ctx)
		ingressCh <- result{records: records, err: err}
	}()

	publicResult := <-publicCh
	if publicResult.err != nil {
		return nil, publicResult.err
	}

	localResult := <-localCh
	if localResult.err != nil {
		return nil, localResult.err
	}

	ingressResult := <-ingressCh
	if ingressResult.err != nil {
		return nil, ingressResult.err
	}

	publicRecords := publicResult.records
	localRecords := localResult.records
	ingressRecords := ingressResult.records

	records := append(publicRecords, localRecords...)
	records = append(records, ingressRecords...)
	sort.Slice(records, func(i, j int) bool {
		if records[i].FQDN == records[j].FQDN {
			if records[i].Type == records[j].Type {
				return records[i].Scope < records[j].Scope
			}
			return records[i].Type < records[j].Type
		}
		return records[i].FQDN < records[j].FQDN
	})
	s.storeRecordCache(records)
	return records, nil
}

func (s *Server) cachedRecords() ([]model.Record, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.recordCacheAt.IsZero() || time.Since(s.recordCacheAt) > recordCacheTTL {
		return nil, false, nil
	}

	return append([]model.Record(nil), s.recordCache...), true, s.recordCacheError
}

func (s *Server) storeRecordCache(records []model.Record) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recordCache = append([]model.Record(nil), records...)
	s.recordCacheAt = time.Now()
	s.recordCacheError = nil
}

func (s *Server) invalidateRecordCache() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recordCache = nil
	s.recordCacheAt = time.Time{}
	s.recordCacheError = nil
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

func hasTailscaleDerivedRecord(records []syncer.DesiredRecord) bool {
	for _, record := range records {
		if record.SourceOfTruth == "tailscale" {
			return true
		}
	}
	return false
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
		return strings.Trim(strings.ToLower(domain), ".")
	}
	return strings.Trim(strings.ToLower(name), ".") + "." + strings.Trim(strings.ToLower(domain), ".")
}

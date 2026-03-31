package pihole

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"porkbun-dns/internal/model"
)

type Client struct {
	baseURL    string
	password   string
	httpClient *http.Client
	mu         sync.Mutex
	sid        string
}

func NewClient(baseURL, password string) *Client {
	return &Client{
		baseURL:  strings.TrimRight(baseURL, "/"),
		password: password,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

type authRequest struct {
	Password string `json:"password"`
}

type authResponse struct {
	Session struct {
		SID string `json:"sid"`
	} `json:"session"`
}

type configResponse struct {
	Config struct {
		DNS struct {
			Hosts        []string `json:"hosts"`
			CNAMERecords []string `json:"cnameRecords"`
			Domain       struct {
				Name  string `json:"name"`
				Local bool   `json:"local"`
			} `json:"domain"`
			ExpandHosts bool `json:"expandHosts"`
		} `json:"dns"`
	} `json:"config"`
}

type patchConfigRequest struct {
	Config struct {
		DNS struct {
			Hosts        []string `json:"hosts"`
			CNAMERecords []string `json:"cnameRecords"`
		} `json:"dns"`
	} `json:"config"`
}

func (c *Client) LocalRecords(ctx context.Context) ([]model.Record, error) {
	cfg, err := c.withConfig(ctx)
	if err != nil {
		return nil, err
	}

	records := make([]model.Record, 0, len(cfg.Config.DNS.Hosts)+len(cfg.Config.DNS.CNAMERecords))
	records = append(records, parseHostRecords(cfg.Config.DNS.Hosts, cfg.Config.DNS.Domain.Name, cfg.Config.DNS.Domain.Local, cfg.Config.DNS.ExpandHosts)...)
	records = append(records, parseCNAMERecords(cfg.Config.DNS.CNAMERecords)...)

	sort.Slice(records, func(i, j int) bool {
		if records[i].FQDN == records[j].FQDN {
			if records[i].Type == records[j].Type {
				return records[i].Scope < records[j].Scope
			}
			return records[i].Type < records[j].Type
		}
		return records[i].FQDN < records[j].FQDN
	})

	return records, nil
}

func (c *Client) UpsertEntry(ctx context.Context, entry model.Entry) error {
	sid, cfg, err := c.authenticatedConfig(ctx)
	if err != nil {
		return err
	}

	hosts := filterHosts(cfg.Config.DNS.Hosts, entry.Name)
	cnames := filterCNAMEs(cfg.Config.DNS.CNAMERecords, entry.Name)

	for _, record := range entry.Local {
		switch strings.ToUpper(record.Type) {
		case "A", "AAAA":
			for _, value := range record.Values {
				hosts = append(hosts, fmt.Sprintf("%s %s", value, strings.ToLower(entry.Name)))
			}
		case "CNAME":
			for _, value := range record.Values {
				cnames = append(cnames, fmt.Sprintf("%s,%s", strings.ToLower(entry.Name), strings.ToLower(value)))
			}
		}
	}

	if err := c.patchConfig(ctx, sid, hosts, cnames); err != nil {
		if isAuthError(err) {
			c.clearSID(sid)
			sid, err = c.authenticate(ctx)
			if err != nil {
				return err
			}
			return c.patchConfig(ctx, sid, hosts, cnames)
		}
		return err
	}
	return nil
}

func (c *Client) DeleteEntry(ctx context.Context, fqdn string) error {
	sid, cfg, err := c.authenticatedConfig(ctx)
	if err != nil {
		return err
	}

	hosts := filterHosts(cfg.Config.DNS.Hosts, fqdn)
	cnames := filterCNAMEs(cfg.Config.DNS.CNAMERecords, fqdn)
	if err := c.patchConfig(ctx, sid, hosts, cnames); err != nil {
		if isAuthError(err) {
			c.clearSID(sid)
			sid, err = c.authenticate(ctx)
			if err != nil {
				return err
			}
			return c.patchConfig(ctx, sid, hosts, cnames)
		}
		return err
	}
	return nil
}

func (c *Client) authenticate(ctx context.Context) (string, error) {
	c.mu.Lock()
	if c.sid != "" {
		sid := c.sid
		c.mu.Unlock()
		return sid, nil
	}
	c.mu.Unlock()

	body, err := json.Marshal(authRequest{Password: c.password})
	if err != nil {
		return "", fmt.Errorf("marshal auth request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, addPath(c.baseURL, "/auth"), bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build auth request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	var response authResponse
	if err := c.do(req, &response); err != nil {
		return "", fmt.Errorf("authenticate with pihole: %w", err)
	}
	if strings.TrimSpace(response.Session.SID) == "" {
		return "", fmt.Errorf("authenticate with pihole: missing session id")
	}
	c.mu.Lock()
	c.sid = response.Session.SID
	c.mu.Unlock()
	return response.Session.SID, nil
}

func (c *Client) config(ctx context.Context, sid string) (configResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, addPath(c.baseURL, "/config"), nil)
	if err != nil {
		return configResponse{}, fmt.Errorf("build config request: %w", err)
	}
	req.Header.Set("sid", sid)

	var response configResponse
	if err := c.do(req, &response); err != nil {
		return configResponse{}, fmt.Errorf("read pihole config: %w", err)
	}

	return response, nil
}

func (c *Client) do(req *http.Request, out any) error {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 300 {
		return &apiError{
			statusCode: resp.StatusCode,
			status:     resp.Status,
			body:       string(data),
		}
	}

	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	return nil
}

func (c *Client) patchConfig(ctx context.Context, sid string, hosts, cnames []string) error {
	var payload patchConfigRequest
	payload.Config.DNS.Hosts = hosts
	payload.Config.DNS.CNAMERecords = cnames

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal config patch: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, addPath(c.baseURL, "/config"), bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build config patch: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("sid", sid)

	var response map[string]any
	if err := c.do(req, &response); err != nil {
		return fmt.Errorf("patch pihole config: %w", err)
	}
	return nil
}

func (c *Client) withConfig(ctx context.Context) (configResponse, error) {
	_, cfg, err := c.authenticatedConfig(ctx)
	return cfg, err
}

func (c *Client) authenticatedConfig(ctx context.Context) (string, configResponse, error) {
	sid, err := c.authenticate(ctx)
	if err != nil {
		return "", configResponse{}, err
	}

	cfg, err := c.config(ctx, sid)
	if err != nil && isAuthError(err) {
		c.clearSID(sid)
		sid, err = c.authenticate(ctx)
		if err != nil {
			return "", configResponse{}, err
		}
		cfg, err = c.config(ctx, sid)
	}
	if err != nil {
		return "", configResponse{}, err
	}
	return sid, cfg, nil
}

func (c *Client) clearSID(sid string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sid == sid {
		c.sid = ""
	}
}

type apiError struct {
	statusCode int
	status     string
	body       string
}

func (e *apiError) Error() string {
	return fmt.Sprintf("unexpected status %s: %s", e.status, strings.TrimSpace(e.body))
}

func isAuthError(err error) bool {
	var apiErr *apiError
	if !errors.As(err, &apiErr) {
		return false
	}
	return apiErr.statusCode == http.StatusUnauthorized || apiErr.statusCode == http.StatusForbidden
}

func parseHostRecords(entries []string, localDomain string, domainIsLocal, expandHosts bool) []model.Record {
	records := make([]model.Record, 0)
	for _, entry := range entries {
		fields := strings.Fields(strings.TrimSpace(entry))
		if len(fields) < 2 {
			continue
		}

		addr, err := netip.ParseAddr(fields[0])
		if err != nil {
			continue
		}

		recordType := "A"
		if addr.Is6() {
			recordType = "AAAA"
		}

		for _, host := range fields[1:] {
			name, fqdn := normalizeLocalName(host, localDomain, domainIsLocal, expandHosts)
			records = append(records, model.Record{
				Name:           name,
				FQDN:           fqdn,
				Type:           recordType,
				Scope:          "local",
				Owner:          "provider-managed",
				SourceOfTruth:  "pihole",
				Targets:        []string{"pihole"},
				Status:         "unmanaged",
				ObservedValues: []string{addr.String()},
			})
		}
	}
	return records
}

func parseCNAMERecords(entries []string) []model.Record {
	records := make([]model.Record, 0, len(entries))
	for _, entry := range entries {
		parts := strings.Split(entry, ",")
		if len(parts) < 2 {
			continue
		}

		name := strings.TrimSpace(parts[0])
		target := strings.TrimSpace(parts[1])
		if name == "" || target == "" {
			continue
		}

		records = append(records, model.Record{
			Name:           strings.ToLower(name),
			FQDN:           strings.ToLower(name),
			Type:           "CNAME",
			Scope:          "local",
			Owner:          "provider-managed",
			SourceOfTruth:  "pihole",
			Targets:        []string{"pihole"},
			Status:         "unmanaged",
			ObservedValues: []string{strings.ToLower(target)},
		})
	}
	return records
}

func normalizeLocalName(host, localDomain string, domainIsLocal, expandHosts bool) (string, string) {
	host = strings.TrimSpace(strings.ToLower(host))
	host = strings.Trim(host, ".")
	if host == "" {
		return "", ""
	}

	if strings.Contains(host, ".") {
		return host, host
	}

	if domainIsLocal && expandHosts && strings.TrimSpace(localDomain) != "" {
		return host, host + "." + strings.Trim(strings.ToLower(localDomain), ".")
	}

	return host, host
}

func addPath(base, path string) string {
	u, err := url.Parse(base)
	if err != nil {
		return strings.TrimRight(base, "/") + path
	}
	u.Path = strings.TrimRight(u.Path, "/") + path
	return u.String()
}

func filterHosts(entries []string, fqdn string) []string {
	filtered := make([]string, 0, len(entries))
	for _, entry := range entries {
		fields := strings.Fields(strings.ToLower(strings.TrimSpace(entry)))
		matched := false
		for _, field := range fields[1:] {
			if strings.Trim(field, ".") == strings.Trim(strings.ToLower(fqdn), ".") {
				matched = true
				break
			}
		}
		if !matched {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

func filterCNAMEs(entries []string, fqdn string) []string {
	filtered := make([]string, 0, len(entries))
	for _, entry := range entries {
		parts := strings.Split(entry, ",")
		if len(parts) == 0 {
			continue
		}
		if strings.Trim(strings.ToLower(parts[0]), ".") == strings.Trim(strings.ToLower(fqdn), ".") {
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
}

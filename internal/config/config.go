package config

import (
	"fmt"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultBaseURL       = "https://api.porkbun.com/api/json/v3"
	defaultTailscaleBin  = "tailscale"
	defaultRecordType    = "A"
	defaultSubdomain     = "int"
	defaultTTL           = 600
	defaultPublicIPURL   = "https://api.ipify.org"
	defaultPublicIPv6URL = "https://api6.ipify.org"
	defaultAPIListenAddr = ":8080"
)

type Config struct {
	APIKey                string
	SecretAPIKey          string
	Domain                string
	SubdomainSuffix       string
	TTL                   int
	DryRun                bool
	BaseURL               string
	TailscaleBinary       string
	RecordType            string
	PublicIPEnabled       bool
	PublicIPLookupURL     string
	PublicIPv6Enabled     bool
	PublicIPv6LookupURL   string
	PublicIPv6RecordNames []string
	PublicIPv6Address     netip.Addr
	APIEnabled            bool
	APIListenAddr         string
	SyncInterval          time.Duration
}

func Load() (Config, error) {
	cfg := Config{
		APIKey:              strings.TrimSpace(os.Getenv("PORKBUN_API_KEY")),
		SecretAPIKey:        strings.TrimSpace(os.Getenv("PORKBUN_SECRET_API_KEY")),
		Domain:              normalizeDomain(os.Getenv("PORKBUN_DOMAIN")),
		SubdomainSuffix:     normalizeLabel(os.Getenv("PORKBUN_SUBDOMAIN_SUFFIX")),
		BaseURL:             strings.TrimSpace(os.Getenv("PORKBUN_BASE_URL")),
		TailscaleBinary:     strings.TrimSpace(os.Getenv("TAILSCALE_BIN")),
		RecordType:          defaultRecordType,
		PublicIPLookupURL:   strings.TrimSpace(os.Getenv("PUBLIC_IP_LOOKUP_URL")),
		PublicIPv6LookupURL: strings.TrimSpace(os.Getenv("PUBLIC_IPV6_LOOKUP_URL")),
		APIListenAddr:       strings.TrimSpace(os.Getenv("API_LISTEN_ADDR")),
	}

	if cfg.SubdomainSuffix == "" {
		cfg.SubdomainSuffix = defaultSubdomain
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultBaseURL
	}
	if cfg.TailscaleBinary == "" {
		cfg.TailscaleBinary = defaultTailscaleBin
	}
	if cfg.PublicIPLookupURL == "" {
		cfg.PublicIPLookupURL = defaultPublicIPURL
	}
	if cfg.PublicIPv6LookupURL == "" {
		cfg.PublicIPv6LookupURL = defaultPublicIPv6URL
	}
	if cfg.APIListenAddr == "" {
		cfg.APIListenAddr = defaultAPIListenAddr
	}

	ttl, err := intFromEnv("PORKBUN_TTL", defaultTTL)
	if err != nil {
		return Config{}, err
	}
	cfg.TTL = ttl

	dryRun, err := boolFromEnv("DRY_RUN", false)
	if err != nil {
		return Config{}, err
	}
	cfg.DryRun = dryRun

	publicIPEnabled, err := boolFromEnv("PUBLIC_IP_ENABLED", false)
	if err != nil {
		return Config{}, err
	}
	cfg.PublicIPEnabled = publicIPEnabled

	publicIPv6Enabled, err := boolFromEnv("PUBLIC_IPV6_ENABLED", false)
	if err != nil {
		return Config{}, err
	}
	cfg.PublicIPv6Enabled = publicIPv6Enabled
	cfg.PublicIPv6RecordNames = parseRecordNames(os.Getenv("PUBLIC_IPV6_RECORD_NAMES"), cfg.Domain)

	apiEnabled, err := boolFromEnv("API_ENABLED", false)
	if err != nil {
		return Config{}, err
	}
	cfg.APIEnabled = apiEnabled

	syncInterval, err := secondsDurationFromEnv("SYNC_INTERVAL")
	if err != nil {
		return Config{}, err
	}
	cfg.SyncInterval = syncInterval

	if rawIPv6 := strings.TrimSpace(os.Getenv("PUBLIC_IPV6_ADDRESS")); rawIPv6 != "" {
		addr, err := netip.ParseAddr(rawIPv6)
		if err != nil || !addr.Is6() {
			return Config{}, fmt.Errorf("PUBLIC_IPV6_ADDRESS must be a valid IPv6 address")
		}
		cfg.PublicIPv6Address = addr
	}

	switch {
	case cfg.APIKey == "":
		return Config{}, fmt.Errorf("PORKBUN_API_KEY is required")
	case cfg.SecretAPIKey == "":
		return Config{}, fmt.Errorf("PORKBUN_SECRET_API_KEY is required")
	case cfg.Domain == "":
		return Config{}, fmt.Errorf("PORKBUN_DOMAIN is required")
	case cfg.SubdomainSuffix == "":
		return Config{}, fmt.Errorf("PORKBUN_SUBDOMAIN_SUFFIX is required")
	case cfg.PublicIPEnabled && cfg.PublicIPLookupURL == "":
		return Config{}, fmt.Errorf("PUBLIC_IP_LOOKUP_URL is required when PUBLIC_IP_ENABLED is true")
	case cfg.PublicIPv6Enabled && !cfg.PublicIPv6Address.IsValid() && cfg.PublicIPv6LookupURL == "":
		return Config{}, fmt.Errorf("PUBLIC_IPV6_LOOKUP_URL is required when PUBLIC_IPV6_ENABLED is true")
	case cfg.PublicIPv6Enabled && len(cfg.PublicIPv6RecordNames) == 0:
		return Config{}, fmt.Errorf("PUBLIC_IPV6_RECORD_NAMES is required when PUBLIC_IPV6_ENABLED is true")
	}

	return cfg, nil
}

func intFromEnv(key string, fallback int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", key)
	}
	return value, nil
}

func secondsDurationFromEnv(key string) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return 0, nil
	}

	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", key)
	}

	return time.Duration(value) * time.Second, nil
}

func boolFromEnv(key string, fallback bool) (bool, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("%s must be a boolean", key)
	}
	return value, nil
}

func normalizeDomain(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	return strings.Trim(value, ".")
}

func normalizeLabel(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	return strings.Trim(value, ".")
}

func parseRecordNames(value, domain string) []string {
	parts := strings.Split(value, ",")
	seen := make(map[string]struct{}, len(parts))
	names := make([]string, 0, len(parts))

	for _, part := range parts {
		name := strings.TrimSpace(strings.ToLower(part))
		name = strings.Trim(name, ".")
		if name == "" {
			continue
		}

		switch {
		case name == "@":
			name = ""
		case name == domain:
			name = ""
		case strings.HasSuffix(name, "."+domain):
			name = strings.TrimSuffix(name, "."+domain)
		}

		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}

	return names
}

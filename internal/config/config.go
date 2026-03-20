package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

const (
	defaultBaseURL      = "https://api.porkbun.com/api/json/v3"
	defaultTailscaleBin = "tailscale"
	defaultRecordType   = "A"
	defaultSubdomain    = "int"
	defaultTTL          = 600
	defaultPublicIPURL  = "https://api.ipify.org"
)

type Config struct {
	APIKey            string
	SecretAPIKey      string
	Domain            string
	SubdomainSuffix   string
	TTL               int
	DryRun            bool
	BaseURL           string
	TailscaleBinary   string
	RecordType        string
	PublicIPEnabled   bool
	PublicIPLookupURL string
}

func Load() (Config, error) {
	cfg := Config{
		APIKey:            strings.TrimSpace(os.Getenv("PORKBUN_API_KEY")),
		SecretAPIKey:      strings.TrimSpace(os.Getenv("PORKBUN_SECRET_API_KEY")),
		Domain:            normalizeDomain(os.Getenv("PORKBUN_DOMAIN")),
		SubdomainSuffix:   normalizeLabel(os.Getenv("PORKBUN_SUBDOMAIN_SUFFIX")),
		BaseURL:           strings.TrimSpace(os.Getenv("PORKBUN_BASE_URL")),
		TailscaleBinary:   strings.TrimSpace(os.Getenv("TAILSCALE_BIN")),
		RecordType:        defaultRecordType,
		PublicIPLookupURL: strings.TrimSpace(os.Getenv("PUBLIC_IP_LOOKUP_URL")),
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

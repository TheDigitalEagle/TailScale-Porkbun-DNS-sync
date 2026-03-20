package publicip

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"strings"
	"time"
)

type Checker struct {
	url        string
	httpClient *http.Client
}

func NewChecker(url string) *Checker {
	return &Checker{
		url: strings.TrimSpace(url),
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

func (c *Checker) IPv4(ctx context.Context) (netip.Addr, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("build request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("lookup public ip: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64))
	if err != nil {
		return netip.Addr{}, fmt.Errorf("read public ip response: %w", err)
	}

	if resp.StatusCode >= 300 {
		return netip.Addr{}, fmt.Errorf("lookup public ip returned %s", resp.Status)
	}

	addr, err := netip.ParseAddr(strings.TrimSpace(string(body)))
	if err != nil {
		return netip.Addr{}, fmt.Errorf("parse public ip response: %w", err)
	}
	if !addr.Is4() {
		return netip.Addr{}, fmt.Errorf("public ip lookup returned non-IPv4 address %q", addr)
	}

	return addr, nil
}

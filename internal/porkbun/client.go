package porkbun

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	apiKey     string
	secretKey  string
	baseURL    string
	httpClient *http.Client
}

func NewClient(apiKey, secretKey, baseURL string) *Client {
	return &Client{
		apiKey:    apiKey,
		secretKey: secretKey,
		baseURL:   strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

type Record struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Content string `json:"content"`
	TTL     string `json:"ttl"`
	Prio    string `json:"prio"`
}

type ListResponse struct {
	Status  string   `json:"status"`
	Records []Record `json:"records"`
	Message string   `json:"message"`
}

type apiResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

type authPayload struct {
	APIKey       string `json:"apikey"`
	SecretAPIKey string `json:"secretapikey"`
}

type createRequest struct {
	authPayload
	Name    string `json:"name"`
	Type    string `json:"type"`
	Content string `json:"content"`
	TTL     string `json:"ttl"`
	Prio    string `json:"prio"`
}

func (c *Client) ListRecords(ctx context.Context, domain string) ([]Record, error) {
	var response ListResponse
	if err := c.post(ctx, "/dns/retrieve/"+domain, authPayload{
		APIKey: c.apiKey, SecretAPIKey: c.secretKey,
	}, &response); err != nil {
		return nil, err
	}
	return response.Records, nil
}

func (c *Client) CreateRecord(ctx context.Context, domain string, record Record) error {
	return c.post(ctx, "/dns/create/"+domain, createRequest{
		authPayload: authPayload{
			APIKey: c.apiKey, SecretAPIKey: c.secretKey,
		},
		Name:    record.Name,
		Type:    record.Type,
		Content: record.Content,
		TTL:     record.TTL,
		Prio:    record.Prio,
	}, nil)
}

func (c *Client) EditRecord(ctx context.Context, domain string, record Record) error {
	return c.post(ctx, "/dns/edit/"+domain+"/"+record.ID, createRequest{
		authPayload: authPayload{
			APIKey: c.apiKey, SecretAPIKey: c.secretKey,
		},
		Name:    record.Name,
		Type:    record.Type,
		Content: record.Content,
		TTL:     record.TTL,
		Prio:    record.Prio,
	}, nil)
}

func (c *Client) DeleteRecord(ctx context.Context, domain, recordID string) error {
	return c.post(ctx, "/dns/delete/"+domain+"/"+recordID, authPayload{
		APIKey: c.apiKey, SecretAPIKey: c.secretKey,
	}, nil)
}

func (c *Client) post(ctx context.Context, path string, payload any, out any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

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
		return fmt.Errorf("porkbun %s returned %s: %s", path, resp.Status, strings.TrimSpace(string(data)))
	}

	if out == nil {
		var response apiResponse
		if err := json.Unmarshal(data, &response); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
		if response.Status != "SUCCESS" {
			return fmt.Errorf("porkbun %s failed: %s", path, response.Message)
		}
		return nil
	}

	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	switch typed := out.(type) {
	case *ListResponse:
		if typed.Status != "SUCCESS" {
			return fmt.Errorf("porkbun %s failed: %s", path, typed.Message)
		}
	}
	return nil
}

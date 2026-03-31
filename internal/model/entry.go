package model

import "time"

type Entry struct {
	Name      string      `json:"name"`
	Public    []RecordSet `json:"public,omitempty"`
	Local     []RecordSet `json:"local,omitempty"`
	HTTP      *HTTPRoute  `json:"http,omitempty"`
	UpdatedAt time.Time   `json:"updated_at,omitempty"`
}

type RecordSet struct {
	Type   string   `json:"type"`
	Values []string `json:"values"`
	TTL    string   `json:"ttl,omitempty"`
}

type HTTPRoute struct {
	Enabled        bool   `json:"enabled"`
	Upstream       string `json:"upstream"`
	TLSImport      string `json:"tls_import,omitempty"`
	RootRedirectTo string `json:"root_redirect_to,omitempty"`
}

type Change struct {
	Target string `json:"target"`
	Scope  string `json:"scope"`
	Name   string `json:"name"`
	Type   string `json:"type,omitempty"`
	Action string `json:"action"`
	Before string `json:"before,omitempty"`
	After  string `json:"after,omitempty"`
}

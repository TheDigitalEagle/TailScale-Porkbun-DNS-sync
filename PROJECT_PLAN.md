# Project Plan

## Goal

Turn `porkbun-dns` from a scheduled Porkbun sync worker into the control-plane service for DNS and related ingress configuration across:

- Porkbun public DNS
- Pi-hole local DNS
- Caddy-managed HTTPS endpoints
- optionally Tailscale metadata and routing inputs later

The long-term UX target is a web frontend that talks only to the `porkbun-dns` API. The frontend should not edit Pi-hole, Porkbun, or Caddy directly.

## Principles

- Keep one external API boundary for the frontend
- Normalize records into one internal desired-state model
- Use provider adapters behind that model for Porkbun, Pi-hole, and Caddy
- Preserve the current sync engine while evolving the service
- Prefer additive phases that keep the existing interval sync working

## Target Architecture

Core layers:

- API layer
  - HTTP server for read/write operations
  - authn/authz, validation, and request shaping
- domain model
  - normalized DNS record and service inventory model
  - desired state, observed state, and drift representation
- reconciliation engine
  - computes diffs and applies changes through providers
- providers
  - Porkbun provider
  - Pi-hole provider
  - Caddy provider
  - optional Tailscale inventory provider
- persistence
  - initial lightweight store for desired state, metadata, and audit trail

## Normalized Model

The API should center around a single record model with fields like:

- `name`
- `type`
- `values`
- `scope`
  - `public`
  - `local`
  - `both`
- `owner`
  - `manual`
  - `derived`
  - `provider-managed`
- `source_of_truth`
- `targets`
  - `porkbun`
  - `pihole`
  - `caddy`
- `status`
  - `in_sync`
  - `drifted`
  - `error`
- `last_applied_at`
- `last_observed_at`

This allows the frontend to manage one logical hostname even when it maps to multiple systems.

## Phase Plan

### Phase 1: API Foundation

Deliverables:

- Add an HTTP server to `cmd/porkbun-dns`
- Add health and inventory endpoints
- Keep the current interval sync behavior intact
- Expose sync execution through the API

Suggested endpoints:

- `GET /health`
- `GET /records`
- `GET /records/public`
- `GET /sync/status`
- `POST /sync/run`

Notes:

- This phase is mostly read-only plus explicit sync triggering
- It should reuse current Porkbun reconciliation code rather than replacing it

### Phase 2: Internal Domain Model

Deliverables:

- Introduce a normalized record model separate from provider payloads
- Separate provider-specific structs from control-plane structs
- Add drift detection between desired and observed state

Notes:

- This is the phase where `porkbun-dns` stops being “just a sync script”
- The goal is to support multiple backends without contaminating the API shape

### Phase 3: Pi-hole Provider

Deliverables:

- Add a Pi-hole provider interface
- Support listing local records and writing managed local records
- Decide the real write path for Pi-hole v6 in this environment

Questions to settle:

- file-based local records vs API-based local DNS management
- ownership boundaries for records that are managed manually in Pi-hole

Suggested endpoints:

- `GET /records/local`
- `PUT /records/:name`
- `DELETE /records/:name`

### Phase 4: Desired-State Storage

Deliverables:

- Add persistent storage for desired records and metadata
- Track record ownership, sync history, and provider errors
- Add an audit trail for changes initiated through the API

Notes:

- SQLite is likely the right first persistence layer
- Keep the schema simple and operationally portable

### Phase 5: Caddy Provider

Deliverables:

- Add a provider abstraction for Caddy-managed sites
- Start with file-edit + reload if needed
- Preserve the option to replace this with a dedicated Caddy-side API later

Managed concepts:

- hostname to upstream mapping
- HTTPS enabled state
- certificate status
- redirect rules

Notes:

- The frontend should still talk only to `porkbun-dns`
- Caddy integration should be hidden behind a provider boundary

### Phase 6: Write API

Deliverables:

- Create/update/delete record endpoints
- validation rules for public, local, and hybrid records
- dry-run preview and apply modes

Suggested endpoints:

- `PUT /records/:name`
- `DELETE /records/:name`
- `POST /records/preview`
- `POST /records/apply`

### Phase 7: Frontend

Deliverables:

- Record inventory view
- drift/error view
- edit forms for public/local/both records
- sync status and manual reconciliation controls

Frontend contract:

- frontend calls only the `porkbun-dns` API
- no direct provider credentials in the browser

## Implementation Order

Recommended near-term order:

1. Add HTTP server and read-only API
2. Introduce normalized model types
3. Refactor existing Porkbun sync into a provider-backed reconciliation path
4. Add Pi-hole read integration
5. Add persistence
6. Add Pi-hole write integration
7. Add Caddy provider abstraction
8. Add write API and frontend

## Risks

- Pi-hole local record management may not align with older file-based assumptions
- Caddy file-edit integration is operationally useful but not the clean long-term API boundary
- Public IPv6 automation depends on either real container IPv6 egress or explicit configured addresses
- Desired-state storage can become messy if introduced after multiple provider-specific write paths

## Near-Term Tasks

Concrete next tasks for the next phase:

1. Add a minimal HTTP server package and `GET /health`
2. Add `GET /records/public` backed by the current Porkbun client
3. Add `POST /sync/run` that triggers the existing reconciliation path
4. Introduce a normalized `Record` struct for API responses
5. Decide whether Pi-hole local record writes should go through file sync or API sync in this environment

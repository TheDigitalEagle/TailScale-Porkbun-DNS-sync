# Repository Guidelines

## Project Structure & Module Organization
This project started as a Go-based DNS sync tool and is now expected to evolve into the control-plane service for DNS and ingress configuration across Porkbun, Pi-hole, and later Caddy.

The current production responsibilities still include scheduled Porkbun reconciliation, but future work should assume this repo is the correct place to add:

- a frontend-facing API
- normalized DNS record models
- provider adapters for Porkbun, Pi-hole, and Caddy
- reconciliation and drift detection

Before making larger architecture changes, read [`PROJECT_PLAN.md`](/home/chad/porkbun-dns/PROJECT_PLAN.md).

Use this layout as the project grows:

- `cmd/porkbun-dns/` for the main entrypoint
- `internal/tailscale/` for parsing `tailscale status`
- `internal/porkbun/` for Porkbun API calls
- `internal/syncer/` for record reconciliation logic
- `internal/api/` for HTTP handlers and request validation
- `internal/providers/` for provider abstractions as they are introduced
- `internal/store/` for persistence when desired-state storage is added
- `tests/` for integration or fixture-driven tests
- `Dockerfile` for the runtime image

## Build, Test, and Development Commands
Prefer standard Go and Docker workflows:

- `go build ./cmd/porkbun-dns` builds the binary
- `go test ./...` runs unit and package tests
- `docker build -t porkbun-dns .` builds the Tailscale-based image
- `docker run --rm porkbun-dns` executes one sync pass locally

Keep the existing single-run sync path intact even as the repo grows an API surface. New API work should reuse the same reconciliation logic rather than fork it.

## Coding Style & Naming Conventions
Write the application in Go. Format code with `gofmt` and keep packages focused on one concern. Use lowercase package names, exported identifiers only when needed, and table-driven tests where appropriate. Prefer explicit names such as `ListPeers`, `ParseStatus`, `SyncRecords`, `ListRecords`, and `ApplyChanges` over abbreviated helpers.

When adding API and provider layers:

- keep provider-specific payloads out of the API contract
- add normalized model types before spreading provider logic across handlers
- avoid making the frontend responsible for provider-specific behavior

## Testing Guidelines
Test parsing and reconciliation logic without requiring a live Tailscale or Porkbun session. Store representative `tailscale status` samples as fixtures and cover cases for added, changed, and removed records. Name Go test files `*_test.go` and run `go test ./...` before opening a PR.

As API work begins, add tests for:

- request validation
- normalized record rendering
- provider diff behavior
- write operations in dry-run mode

## Commit & Pull Request Guidelines
Git history is not available in this workspace, so use short imperative commit messages such as `Add managed AAAA sync support`. PRs should include a summary of DNS behavior changes, any new environment variables, and example command output when sync logic changes.

For larger changes, especially API or provider abstractions, PRs should also call out:

- which layer changed
- whether any provider contract changed
- whether the change affects desired state, observed state, or both

## Security & Configuration Tips
`resources.md` currently contains live Porkbun credentials and domain details. Do not hardcode these values in Go source or Docker layers. Read secrets from environment variables at runtime, and treat [`resources.md`](/home/chad/porkbun-dns/resources.md) as sensitive operational input that should be removed or sanitized before sharing the repository.

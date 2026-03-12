# Repository Guidelines

## Project Structure & Module Organization
This project builds a Go-based DNS sync tool that runs inside a Docker image derived from `tailscale/tailscale:latest`. The application should call `tailscale status`, extract machine names and Tailscale IPs, and upsert Porkbun A records under `*.int.ima.fish`.

Use this layout as the project grows:

- `cmd/porkbun-dns/` for the main entrypoint
- `internal/tailscale/` for parsing `tailscale status`
- `internal/porkbun/` for Porkbun API calls
- `internal/sync/` for record reconciliation logic
- `tests/` for integration or fixture-driven tests
- `Dockerfile` for the scheduled runtime image

## Build, Test, and Development Commands
Prefer standard Go and Docker workflows:

- `go build ./cmd/porkbun-dns` builds the sync binary
- `go test ./...` runs unit and package tests
- `docker build -t porkbun-dns .` builds the Tailscale-based image
- `docker run --rm porkbun-dns` executes one sync pass locally

Design the app as a single-run command so it can be triggered by cron, systemd timers, or container schedulers.

## Coding Style & Naming Conventions
Write the application in Go. Format code with `gofmt` and keep packages focused on one concern. Use lowercase package names, exported identifiers only when needed, and table-driven tests where appropriate. Prefer explicit names such as `ListPeers`, `ParseStatus`, and `SyncRecords` over abbreviated helpers.

## Testing Guidelines
Test parsing and reconciliation logic without requiring a live Tailscale or Porkbun session. Store representative `tailscale status` samples as fixtures and cover cases for added, changed, and removed A records. Name Go test files `*_test.go` and run `go test ./...` before opening a PR.

## Commit & Pull Request Guidelines
Git history is not available in this workspace, so use short imperative commit messages such as `Add Porkbun record sync client`. PRs should include a summary of DNS behavior changes, any new environment variables, and example command output when sync logic changes.

## Security & Configuration Tips
`resources.md` currently contains live Porkbun credentials and domain details. Do not hardcode these values in Go source or Docker layers. Read secrets from environment variables at runtime, and treat [`resources.md`](/home/chad/porkbun-dns/resources.md) as sensitive operational input that should be removed or sanitized before sharing the repository.

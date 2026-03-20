# Changelog

All notable changes to this project will be documented in this file.

## [1.0.0] - 2026-03-12

### Added

- Initial Go implementation for syncing Tailscale peer IPs to Porkbun `A` records.
- Docker image based on `tailscale/tailscale:latest`.
- Docker Compose deployment with persistent Tailscale state and interval-based sync.
- Porkbun API client, Tailscale status parser, and reconciliation engine.
- Unit tests for Tailscale parsing and DNS reconciliation behavior.
- Repository documentation, version file, and contributor guidelines.

### Changed

- Preferred Tailscale `DNSName` labels over raw `HostName` values for DNS record naming.
- Updated project-facing naming to `TailScale Porkbun DNS Sync`.
- Added support for syncing advertised Tailscale Services from `CapMap["service-host"]`.

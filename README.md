# porkbun-dns

`porkbun-dns` is a small Go service that joins a Tailscale tailnet, reads `tailscale status --json`, and keeps Porkbun `A` records in sync for a delegated subdomain such as `*.int.ima.fish`.

It is packaged as a Docker image built on top of `tailscale/tailscale:latest`, so the same container can:

- start `tailscaled`
- authenticate with a Tailscale auth key
- inspect tailnet peers
- create, update, and delete Porkbun DNS records
- repeat the sync on a fixed interval

## What It Syncs

For each tailnet node with an IPv4 Tailscale address, the service creates or updates:

- `<machine>.int.<domain>` -> `<tailscale-ip>`

It manages only `A` records under the configured subdomain suffix. Records outside that scope are ignored.

The service prefers the node label derived from Tailscale `DNSName`, so records match MagicDNS-style names such as `snke-laptop.int.ima.fish`.

## How It Works

1. The container starts `tailscaled` in userspace mode.
2. It runs `tailscale up` with `TS_AUTHKEY` or reuses persisted Tailscale state.
3. The Go binary reads `tailscale status --json`.
4. It compares desired names and IPs against Porkbun DNS records.
5. It creates, updates, and deletes managed records as needed.
6. It sleeps for `SYNC_INTERVAL` seconds and repeats.

## Quick Start

Copy the example env file and fill in your secrets:

```sh
cp .env.example .env
```

Start the stack:

```sh
docker compose up -d --build
docker compose logs -f porkbun-dns
```

Check status:

```sh
docker compose ps
docker exec porkbun-dns tailscale --socket=/var/run/tailscale/tailscaled.sock status
```

## Configuration

Required:

- `PORKBUN_API_KEY`
- `PORKBUN_SECRET_API_KEY`
- `PORKBUN_DOMAIN`
- `TS_AUTHKEY` for first-time Tailscale enrollment

Common optional values:

- `PORKBUN_SUBDOMAIN_SUFFIX=int`
- `PORKBUN_TTL=600`
- `SYNC_INTERVAL=3600`
- `TS_HOSTNAME=porkbun-dns`
- `TS_TUN_MODE=userspace-networking`
- `TS_EXTRA_ARGS=--accept-dns=false`
- `DRY_RUN=false`

If `SYNC_INTERVAL` is blank, the container runs a single sync pass and exits.

## Development

Run tests:

```sh
docker run --rm -v "$PWD:/src" -w /src golang:1.25 go test ./...
```

Build the image:

```sh
docker build -t porkbun-dns .
```

## Repository Layout

- `cmd/porkbun-dns/` main program
- `internal/config/` environment loading
- `internal/tailscale/` Tailscale status parsing
- `internal/porkbun/` Porkbun API client
- `internal/syncer/` DNS reconciliation logic
- `docker/` container startup scripts
- `compose.yaml` local deployment definition

## Security

Do not commit live credentials, auth keys, or local state.

The following should remain local only:

- `.env`
- `resources.md`
- Tailscale state volumes or exported state files

Use `.env.example` as the shareable template and rotate any secret that is ever committed accidentally.

#!/bin/sh
set -eu

SOCKET="${TS_SOCKET:-/var/run/tailscale/tailscaled.sock}"

exec tailscale --socket="${SOCKET}" "$@"

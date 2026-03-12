#!/bin/sh
set -eu

STATE_DIR="${TS_STATE_DIR:-/var/lib/tailscale}"
SOCKET="${TS_SOCKET:-/var/run/tailscale/tailscaled.sock}"
TUN_MODE="${TS_TUN_MODE:-userspace-networking}"
SYNC_INTERVAL="${SYNC_INTERVAL:-}"
STATE_FILE="${STATE_DIR}/tailscaled.state"

mkdir -p "${STATE_DIR}" "$(dirname "${SOCKET}")"

tailscaled \
  --state="${STATE_FILE}" \
  --socket="${SOCKET}" \
  --tun="${TUN_MODE}" &

TAILSCALED_PID=$!
cleanup() {
  kill "${TAILSCALED_PID}" 2>/dev/null || true
  wait "${TAILSCALED_PID}" 2>/dev/null || true
}
trap cleanup EXIT INT TERM

i=0
while [ "${i}" -lt 30 ]; do
  if [ -S "${SOCKET}" ]; then
    break
  fi
  sleep 1
  i=$((i + 1))
done

if [ ! -S "${SOCKET}" ]; then
  echo "tailscaled socket did not appear at ${SOCKET}." >&2
  exit 1
fi

if [ -n "${TS_AUTHKEY:-}" ]; then
  tailscale --socket="${SOCKET}" up \
    --auth-key="${TS_AUTHKEY}" \
    --hostname="${TS_HOSTNAME:-porkbun-dns}" \
    ${TS_EXTRA_ARGS:-}
elif [ ! -s "${STATE_FILE}" ]; then
  echo "TS_AUTHKEY is not set and no existing Tailscale state was found at ${STATE_FILE}." >&2
  exit 1
else
  echo "Using existing Tailscale state from ${STATE_FILE}." >&2
fi

export TAILSCALE_BIN="${TAILSCALE_BIN:-/usr/local/bin/tailscale-local}"

run_sync() {
  /usr/local/bin/porkbun-dns "$@"
}

if [ -n "${SYNC_INTERVAL}" ]; then
  while true; do
    run_sync "$@"
    sleep "${SYNC_INTERVAL}"
  done
fi

run_sync "$@"

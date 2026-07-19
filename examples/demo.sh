#!/usr/bin/env bash
# demo.sh — walks through every admitctl command end to end, including
# a real rollback. Safe to run repeatedly: it uses its own throwaway
# store file rather than your real ~/.admitctl store.
#
# Usage:
#   go build -o /tmp/onboard-client ./cmd/onboard-client
#   ./examples/demo.sh
set -euo pipefail

export ADMITCTL_STORE
ADMITCTL_STORE="$(mktemp -d)/tenants.json"
BIN="${ADMITCTL_BIN:-/tmp/onboard-client}"

step() { printf '\n\033[1;36m>> %s\033[0m\n' "$1"; }

step "Register a healthy tenant"
"$BIN" register --id acme-corp --name "Acme Corp" \
  --auth api_key --rps 100 --burst 250 \
  --schema-version v1 --flags beta_dashboard,new_ui

step "Register a tenant with zero rate-limit headroom (onboards, but degraded)"
"$BIN" register --id tight-startup --name "Tight Startup" \
  --auth oauth2 --rps 20

step "List all tenants"
"$BIN" list

step "Inspect one tenant in detail"
"$BIN" get acme-corp

step "Run health checks across all active tenants"
"$BIN" health

step "Attempt to onboard a tenant with an unrecognized schema version"
echo "(this should be REJECTED and rolled back — no trace left behind)"
set +e
"$BIN" register --id bad-corp --name "Bad Corp" \
  --auth api_key --rps 10 --burst 20 --schema-version v99
echo "exit code: $?"
set -e

step "Confirm bad-corp left zero trace"
"$BIN" list
"$BIN" get bad-corp 2>&1 || true

step "Deactivate a tenant"
"$BIN" deactivate tight-startup
"$BIN" list

step "Done — store file used for this demo:"
echo "$ADMITCTL_STORE"

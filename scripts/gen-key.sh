#!/usr/bin/env bash
# scripts/gen-key.sh — convenience wrapper around `agentic-coop-db-server -mint-key`.
#
# Usage:
#   ./scripts/gen-key.sh [workspace] [pg_role]
#
# Examples:
#   ./scripts/gen-key.sh                       # workspace=default, pg_role=dbadmin
#   ./scripts/gen-key.sh acme dbuser           # workspace=acme,    pg_role=dbuser
#   AGENTCOOPDB_KEY_ENV=live ./scripts/gen-key.sh # tag the key as live instead of dev
#
# Resolution order for invoking the api binary:
#
#   1. AGENTCOOPDB_API_CONTAINER env var (Coolify, k8s, any orchestrator
#      where the api runs as a named container) — `docker exec` into it
#   2. agentic-coop-db-server binary on PATH (local builds or installed binaries)
#   3. local docker container literally named agentic-coop-db-api
#      (the compose dev profile names it that way)
#
# All the actual work — random generation, argon2id hashing, INSERTing
# into agentcoopdb.workspaces and agentcoopdb.api_keys — happens inside the
# Go binary. This script is just a launcher.

set -euo pipefail

workspace="${1:-default}"
pg_role="${2:-dbadmin}"
env_tag="${AGENTCOOPDB_KEY_ENV:-dev}"

args=(-mint-key
  -mint-workspace "${workspace}"
  -mint-role "${pg_role}"
  -mint-env "${env_tag}"
)

if [[ -n "${AGENTCOOPDB_API_CONTAINER:-}" ]]; then
  exec docker exec "${AGENTCOOPDB_API_CONTAINER}" /app/agentic-coop-db-server "${args[@]}"
elif command -v agentic-coop-db-server >/dev/null 2>&1; then
  exec agentic-coop-db-server "${args[@]}"
elif command -v docker >/dev/null 2>&1 && docker ps --format '{{.Names}}' | grep -q '^agentic-coop-db-api'; then
  exec docker exec agentic-coop-db-api /app/agentic-coop-db-server "${args[@]}"
fi

echo "could not invoke agentic-coop-db-server -mint-key" >&2
echo "  set AGENTCOOPDB_API_CONTAINER to the running api container name, or" >&2
echo "  install the agentic-coop-db-server binary on PATH" >&2
exit 1

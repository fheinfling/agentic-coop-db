#!/usr/bin/env bash
# scripts/dev-up.sh — bring the local environment to a state where
# `make up-local` will succeed.
#
# Steps:
#   1. Ensure the docker daemon is running. On Linux with systemd, prompt to
#      `sudo systemctl enable --now docker` if it isn't.
#   2. Ensure deploy/secrets/postgres_password.txt exists (random if missing).
#   3. Pre-create the docker volumes the compose stack needs.
#
# This script is safe to run repeatedly.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SECRETS_DIR="${REPO_ROOT}/deploy/secrets"

log()  { printf '\033[1;34m[dev-up]\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m[dev-up]\033[0m %s\n' "$*" >&2; }
fail() { printf '\033[1;31m[dev-up]\033[0m %s\n' "$*" >&2; exit 1; }

ensure_docker() {
  if ! command -v docker >/dev/null 2>&1; then
    fail "docker is not installed. See https://docs.docker.com/engine/install/"
  fi
  if docker info >/dev/null 2>&1; then
    log "docker daemon is up"
    return
  fi
  warn "docker daemon is not responding"
  if command -v systemctl >/dev/null 2>&1; then
    warn "attempting: sudo systemctl enable --now docker"
    if sudo systemctl enable --now docker; then
      sleep 1
      if docker info >/dev/null 2>&1; then
        log "docker daemon started"
        return
      fi
    fi
  fi
  fail "could not start the docker daemon — start it manually and re-run"
}

ensure_secrets() {
  mkdir -p "${SECRETS_DIR}"
  chmod 700 "${SECRETS_DIR}"
  local pw="${SECRETS_DIR}/postgres_password.txt"
  if [[ ! -s "${pw}" ]]; then
    log "generating ${pw}"
    # 32 random bytes, base64-encoded; strip padding/newlines.
    head -c 32 /dev/urandom | base64 | tr -d '=\n' > "${pw}"
    chmod 600 "${pw}"
  fi
}

main() {
  ensure_docker
  ensure_secrets
  log "ready — run: make up-local"
}

main "$@"

# ADR 0000 — Dependencies

**Status:** accepted
**Date:** 2026-04-08

Every direct dependency in `go.mod` and `pyproject.toml` is listed here
with a one-line justification. New PRs that add a dependency MUST update
this document. CI fails on dependency drift.

## Go (`go.mod`)

| Module                                          | Why                                          |
|-------------------------------------------------|----------------------------------------------|
| `github.com/jackc/pgx/v5` + `pgxpool`           | The most active Postgres driver; native parameter binding; type-safe scans |
| `github.com/golang-migrate/migrate/v4`          | Embeddable migrator with the pgx driver and file source                    |
| `github.com/pganalyze/pg_query_go/v5`           | Embeds the real PostgreSQL parser; the only way to safely classify SQL    |
| `github.com/santhosh-tekuri/jsonschema/v6`      | RPC argument validation; pure-Go, draft 2020-12 support                   |
| `golang.org/x/crypto/argon2`                    | Argon2id hashing of API keys                                              |
| `github.com/hashicorp/golang-lru/v2`            | LRU cache for the auth verify cache and per-key rate limiter              |
| `github.com/google/uuid`                        | UUIDv4 minting for audit logs and key IDs                                 |
| `log/slog`                                      | Standard library; no third-party log dep                                  |
| `github.com/prometheus/client_golang`           | The de-facto prometheus instrumentation                                   |
| `github.com/stretchr/testify`                   | `require` for clean test assertions                                       |
| `github.com/testcontainers/testcontainers-go`   | Real Postgres in integration tests                                        |

### Removed in v0.1.0

| Module | Replaced with |
|--------|---------------|
| `github.com/go-chi/chi/v5` | `net/http.ServeMux` (Go 1.22+); custom ~60-line middleware in `internal/httpapi/middleware.go` |
| `github.com/kelseyhightower/envconfig` | Manual `os.Getenv` + `strconv` parsing in `internal/config/config.go` |
| `golang.org/x/time/rate` | Custom token-bucket implementation (~50 lines) in `internal/httpapi/middleware.go` |

## Python (`clients/python/pyproject.toml`)

| Package        | Why                                                              |
|----------------|------------------------------------------------------------------|
| `requests`     | Battle-tested HTTP client; no async required for the v1 surface  |
| `typer`        | Decorator-based CLI; rich help; tiny dep footprint               |
| `pydantic`     | Type-safe response models; the standard for Python data classes  |
| `platformdirs` | OS-correct paths for `~/.agentic-coop-db/`                               |

## Rules

1. New dep ⇒ new row in this table + a sentence on why no stdlib equivalent works.
2. Removing a dep is always free; CI rewards leanness.
3. Pinning: Go uses go.sum; Python uses `>=` ranges with the lockfile in CI.

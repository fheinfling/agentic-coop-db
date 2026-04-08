# Changelog

All notable changes to AIColDB are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.0] — 2026-04-08

Initial public release. Single-node, container-first auth gateway in front
of PostgreSQL 16 + pgvector.

### Added

- **Auth gateway** with workspace-scoped API keys (`aic_<env>_<id>_<secret>`),
  argon2id at rest, in-memory LRU verify cache, key rotation with overlap.
- **Parameterised SQL forwarding** at `POST /v1/sql/execute`. Validator
  enforces single-statement, parse-success, placeholder-count match,
  size + param caps. No statement-type allowlist — Postgres role grants
  decide what each key can run.
- **Multi-tenant isolation** via `SET LOCAL ROLE` + RLS policies keyed
  on `app.workspace_id`. Migration linter (`scripts/lint-migrations`) is
  a CI gate that fails any tenant table without the policy.
- **Built-in roles** `dbadmin` (DDL/DCL, owner of `public`, `BYPASSRLS`)
  and `dbuser` (CRUD, `NOBYPASSRLS`). Custom roles supported via
  `aicoldb key create --role`.
- **pgvector** enabled by migration 0005, with `internal/vector` helpers
  and `db.vector_upsert` / `db.vector_search` in the Python SDK.
- **Optional RPC layer** at `POST /v1/rpc/call` with JSON Schema arg
  validation, server-side idempotency-key replay/conflict, and
  `sql/rpc/upsert_document.sql` as a worked example.
- **Audit log** (`audit_logs` table) with hashed SQL/params; full capture
  via `AICOLDB_AUDIT_INCLUDE_SQL=true`.
- **Rate limiting** per key via `golang.org/x/time/rate` (60 req/s, burst
  120, configurable). Returns HTTP 429 with `Retry-After`.
- **Postgres-side hardening**: filesystem escape functions
  (`pg_read_file`, `lo_import`, `COPY ... FROM PROGRAM`, etc) revoked at
  the database level by migration 0004.
- **Container baseline**: multi-stage distroless ARM64+amd64 Dockerfile,
  read-only root, dropped capabilities, `no-new-privileges`.
- **Deployment profiles**: `local`, `pi-lite` (Pi 4/5 tuning), `cloud`
  (Caddy auto-TLS + restic backups + postgres-exporter + prometheus),
  and a `stack.swarm.yml` for Docker Swarm with external secrets.
- **Python SDK** (`pip install aicoldb`): `connect`, `execute`, `select`,
  `transaction`, `vector_upsert`, `vector_search`, `rotate_key`, `me`,
  `health`. Typed error taxonomy (`AuthError`, `ValidationError`,
  `IdempotencyConflict`, `RateLimited`, `ServerError`, `NetworkError`,
  `QueueFullError`).
- **Offline retry queue** (`aicoldb.queue.Queue`) backed by SQLite, with
  exponential backoff and a dead-letter table.
- **CLI** (`aicoldb`): `init` (interactive onboarding wizard), `me`,
  `sql`, `key create|rotate`, `queue status|flush|clear-dead`, `doctor`.
- **Documentation**: architecture, api, security threat model, RLS guide,
  RPC authoring guide, FAQ, deploy guides, ADRs 0000–0006, and a
  16-file feature roadmap under `docs/features/`.

### Security

- TLS mandatory in any non-localhost deployment (server refuses to start
  with `AICOLDB_INSECURE_HTTP=1` unset).
- Container runs as `USER 65532:65532` with read-only root filesystem and
  `cap_drop: [ALL]`.
- Migrations run as a separate role (`aicoldb_owner`); the application
  server pool only ever connects as `aicoldb_gateway`.

[Unreleased]: https://github.com/fheinfling/aicoldb/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/fheinfling/aicoldb/releases/tag/v0.1.0

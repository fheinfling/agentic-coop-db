# Changelog

All notable changes to Agentic Coop DB are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.0] — 2026-04-08

Initial public release. Single-node, container-first auth gateway in front
of PostgreSQL 16 + pgvector.

### Added

- **Auth gateway** with workspace-scoped API keys (`acd_<env>_<id>_<secret>`),
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
  `agentic-coop-db key create --role`.
- **pgvector** enabled by migration 0005, with `internal/vector` helpers
  and `db.vector_upsert` / `db.vector_search` in the Python SDK.
- **Optional RPC layer** at `POST /v1/rpc/call` with JSON Schema arg
  validation, server-side idempotency-key replay/conflict, and
  `sql/rpc/upsert_document.sql` as a worked example.
- **Audit log** (`audit_logs` table) with hashed SQL/params; full capture
  via `AGENTCOOPDB_AUDIT_INCLUDE_SQL=true`.
- **Rate limiting** per key with LRU-bounded bucket pool (60 req/s, burst
  120, configurable). Returns HTTP 429 with `Retry-After`.
- **Postgres-side hardening**: filesystem escape functions
  (`pg_read_file`, `lo_import`, `COPY ... FROM PROGRAM`, etc) revoked at
  the database level by migration 0004.
- **Container baseline**: multi-stage ARM64+amd64 Dockerfile,
  read-only root, dropped capabilities, `no-new-privileges`.
- **Deployment profiles**: `local`, `pi-lite` (Pi 4/5 tuning), `cloud`
  (Caddy auto-TLS + restic backups + postgres-exporter + prometheus),
  and a `stack.swarm.yml` for Docker Swarm with external secrets.
- **Python SDK** (`pip install agentic-coop-db`): `connect`, `execute`, `select`,
  `transaction`, `vector_upsert`, `vector_search`, `rotate_key`, `me`,
  `health`. Typed error taxonomy (`AuthError`, `ValidationError`,
  `IdempotencyConflict`, `RateLimited`, `ServerError`, `NetworkError`,
  `QueueFullError`).
- **Offline retry queue** (`agentcoopdb.queue.Queue`) backed by SQLite, with
  exponential backoff and a dead-letter table.
- **CLI** (`agentcoopdb`): `init` (interactive onboarding wizard), `me`,
  `sql`, `key create|rotate`, `queue status|flush|clear-dead`, `doctor`.
- **Documentation**: architecture, api, security threat model, RLS guide,
  RPC authoring guide, FAQ, deploy guides, ADRs 0000–0006, and a
  feature roadmap tracked via GitHub Issues.

### Changed

- **Project renamed** from `AIColDB` to `Agentic Coop DB`. Repository
  `github.com/fheinfling/agentic-coop-db`, Go module
  `github.com/fheinfling/agentic-coop-db`, Python distribution `agentic-coop-db`,
  Python import `agentcoopdb`, Postgres roles `agentcoopdb_owner` /
  `agentcoopdb_gateway`, env var prefix `AGENTCOOPDB_*`, API key prefix `acd_`.
- **Dropped 3 dependencies**: `go-chi/chi` (replaced with stdlib
  `net/http.ServeMux`), `kelseyhightower/envconfig` (replaced with manual
  `os.Getenv` parsing), `golang.org/x/time/rate` (replaced with a ~50-line
  custom token bucket). See `docs/adr/0000-dependencies.md`.

### Security

- **Control-plane tables split into a dedicated `agentcoopdb` schema**
  (migration `0007_split_control_plane_schema`). Previously, `dbadmin`
  API keys could `DROP TABLE api_keys` because the control-plane lived in
  `public` (which `dbadmin` owns). They now live in `agentcoopdb`, which
  only `agentcoopdb_gateway` has CRUD on.
- `internal/sql.Validator` now counts `$N` placeholders by walking
  `pg_query.Scan` tokens instead of running `pg_query.Normalize`.
- `POST /v1/auth/keys` now rejects requests where `workspace_id` differs
  from the calling key's workspace.
- Proper password handling via `AGENTCOOPDB_GATEWAY_PASSWORD` and
  `AGENTCOOPDB_OWNER_PASSWORD` env vars (plus docker `_FILE` variants).
- `auth.VerifyCache.RevokeByDBID` evicts cached entries immediately on
  key rotation instead of waiting up to the cache TTL.
- The auth middleware runs argon2id against a dummy hash on `ErrKeyNotFound`
  to prevent timing-based enumeration of valid key IDs.
- `rpc.HashRequest` now hashes the raw request body bytes for deterministic
  idempotency hashing.
- `POST /v1/sql/execute` now honours `Idempotency-Key`.
- The Python SDK auto-generates an `Idempotency-Key` header for every
  `_post` call.
- Per-key rate limiter uses an LRU-bounded bucket pool to prevent
  unbounded memory growth from key rotation or enumeration.
- `SetRolePassword` rejects NUL bytes in passwords.
- `isLocalAddr` no longer treats `":port"` as local (it binds 0.0.0.0).
- TLS mandatory in any non-localhost deployment (server refuses to start
  with `AGENTCOOPDB_INSECURE_HTTP=1` unset).
- Container runs as `USER 65532:65532` with read-only root filesystem and
  `cap_drop: [ALL]`.
- Migrations run as a separate role (`agentcoopdb_owner`); the application
  server pool only ever connects as `agentcoopdb_gateway`.

[Unreleased]: https://github.com/fheinfling/agentic-coop-db/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/fheinfling/agentic-coop-db/releases/tag/v0.1.0

# Changelog

All notable changes to Agentic Coop DB are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.4.0](https://github.com/fheinfling/Agentic-Coop-DB/compare/v0.3.1...v0.4.0) (2026-04-24)


### Features

* Add embedded MCP server via Streamable HTTP transport ([#41](https://github.com/fheinfling/Agentic-Coop-DB/issues/41)) ([10c28b0](https://github.com/fheinfling/Agentic-Coop-DB/commit/10c28b086e308250a83df7f86395bc425836c65b))

## [0.3.1](https://github.com/fheinfling/Agentic-Coop-DB/compare/v0.3.0...v0.3.1) (2026-04-12)


### Bug Fixes

* add GH_REPO to release-binaries upload and remove silent failure ([ab606b4](https://github.com/fheinfling/Agentic-Coop-DB/commit/ab606b427cfb9966b6ae6e4e2890d7794ec95255))
* add workflow_dispatch to release-binaries and use release tag ([b058c94](https://github.com/fheinfling/Agentic-Coop-DB/commit/b058c94317f14f23d4f1b12bfa9798a87cc05d96))
* trigger release-binaries on release event, not tag push ([b156d91](https://github.com/fheinfling/Agentic-Coop-DB/commit/b156d913ccd7dfc53bb72a9d369cd7cac9b57f12))
* use per-platform glob in release-binaries upload step ([cce6982](https://github.com/fheinfling/Agentic-Coop-DB/commit/cce6982bab7179aeb126169f48ce11bf0428d99c))

## [0.3.0](https://github.com/fheinfling/Agentic-Coop-DB/compare/v0.2.2...v0.3.0) (2026-04-12)


### Features

* easy MCP install with pre-built binaries ([#29](https://github.com/fheinfling/Agentic-Coop-DB/issues/29)) ([821d267](https://github.com/fheinfling/Agentic-Coop-DB/commit/821d267e2495bfb0f2281f629b4b2d481925e9c6))

## [0.2.2](https://github.com/fheinfling/Agentic-Coop-DB/compare/v0.2.1...v0.2.2) (2026-04-12)


### Bug Fixes

* deploy stack fixes (pi-lite, swarm, pinned deps) ([#27](https://github.com/fheinfling/Agentic-Coop-DB/issues/27)) ([51eff4e](https://github.com/fheinfling/Agentic-Coop-DB/commit/51eff4e2a79c521a7957bfd7d5f8589d3a21e1ed))

## [0.2.1](https://github.com/fheinfling/Agentic-Coop-DB/compare/v0.2.0...v0.2.1) (2026-04-12)


### Bug Fixes

* pin dependencies to commit SHAs and image digests (OpenSSF Scorecard) ([#25](https://github.com/fheinfling/Agentic-Coop-DB/issues/25)) ([ad1eeb8](https://github.com/fheinfling/Agentic-Coop-DB/commit/ad1eeb8fe0680ade86e25e63539e087c11e7eabd))
* pin pip installs by hash to satisfy Scorecard Pinned-Dependencies ([#26](https://github.com/fheinfling/Agentic-Coop-DB/issues/26)) ([00f6f6a](https://github.com/fheinfling/Agentic-Coop-DB/commit/00f6f6acae7ce358622f4253dc4693902aead4b9))
* return rows for RETURNING clauses, fix Makefile and local compose ([#23](https://github.com/fheinfling/Agentic-Coop-DB/issues/23)) ([53d2058](https://github.com/fheinfling/Agentic-Coop-DB/commit/53d205885c00812d1ebed9fdc42d043d487a5751))

## [0.2.0](https://github.com/fheinfling/agentic-coop-db/compare/v0.1.0...v0.2.0) (2026-04-10)


### Features

* add AGENTCOOPDB_AUDIT_DISABLED flag ([#18](https://github.com/fheinfling/agentic-coop-db/issues/18)) ([ae44653](https://github.com/fheinfling/agentic-coop-db/commit/ae44653dc9e30f7aa97a2668b41bc19b86b17f6a))
* add MCP server for Claude Desktop / Claude Code / Cursor ([#22](https://github.com/fheinfling/agentic-coop-db/issues/22)) ([2ddd1ca](https://github.com/fheinfling/agentic-coop-db/commit/2ddd1ca0a08c72206794a90687d601a6e965ae97))


### Bug Fixes

* auto-grant CREATE on public schema for PG 15+ external databases ([e441ac3](https://github.com/fheinfling/agentic-coop-db/commit/e441ac3c577d107d99ec04a14c44f29fa1cd5a94))
* fail with actionable message when CREATE on public schema is denied ([2c6f7dd](https://github.com/fheinfling/agentic-coop-db/commit/2c6f7dd3ad72493d535c2a3954976651ca691735))
* strengthen TestWrite_Disabled to prove disabled short-circuit ([#19](https://github.com/fheinfling/agentic-coop-db/issues/19)) ([78f070b](https://github.com/fheinfling/agentic-coop-db/commit/78f070b6bdf9b80aaa02a534e7d06413ee0b6a6f))

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

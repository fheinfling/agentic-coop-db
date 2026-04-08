# Security model

AIColDB forwards arbitrary valid SQL to Postgres. The security story therefore
has to be airtight: parameterisation, authentication, authorisation, multi-tenant
isolation, and Postgres-side hardening are five layers of defence, each
independently sufficient for its threat.

## 1. SQL injection prevention

- **Mandatory parameterisation.** The endpoint body is `{sql, params}`. The
  validator parses the SQL and counts `$N` placeholders; if the count does
  not match `len(params)` the request is rejected with HTTP 400.
- **Single statement only.** `pg_query.Parse` returns a list of top-level
  statements; the validator rejects anything other than a list of length 1.
  This blocks the canonical `'; DROP TABLE users; --` chain.
- **Statement size cap** (64 KiB) and **parameter count cap** (100).
- **Server-side parameter binding.** The executor uses pgx's positional
  binding (`tx.Query(ctx, sql, args...)`); values are sent as a separate
  field on the wire and never interpolated into the SQL text.
- **SDK ergonomics push parameterisation.** `db.execute(sql, params)` is
  easier to use than building an f-string. The Python SDK ships with
  `aicoldb-lint`, a tiny ast-based linter that flags
  `db.execute(f"...{x}...")` patterns.

## 2. Authentication

- API keys are 192-bit secrets stored as **argon2id** hashes
  (`time=2, memory=64 MiB, threads=2`). The plaintext is shown to the
  caller exactly once at creation time.
- Verification uses an in-memory **LRU+TTL cache** keyed on
  `sha256(full_key)`. Argon2id runs at most once per key per 5 minutes.
- The header is `Authorization: Bearer aic_<env>_<id>_<secret>`. The
  `<id>` is used for the lookup, the `<secret>` is verified against the
  hash. Comparison is constant-time.
- **TLS is mandatory** outside of localhost. The server refuses to start
  with `AICOLDB_INSECURE_HTTP=1` unless that env var is explicitly set.

## 3. Authorisation (Postgres role grants)

- Pool login role: `aicoldb_gateway`. No privileges of its own; member of
  `dbadmin`, `dbuser`, and any custom roles minted later.
- Every request opens a transaction and runs `SET LOCAL ROLE '<key.role>'`.
  Because `SET LOCAL ROLE` can only target roles the outer role is a member
  of, a `dbuser` key cannot escalate to `dbadmin`.
- `dbadmin` has `CREATEROLE CREATEDB BYPASSRLS`, owns the `public` schema,
  and can run DDL/DCL. **Not** a Postgres superuser, so cannot
  `ALTER SYSTEM`, `COPY ... FROM PROGRAM`, or load arbitrary libraries.
- `dbuser` is `NOBYPASSRLS` — RLS policies on tenant tables apply
  unconditionally.
- Migrations run as a separate role `aicoldb_owner` via a different
  connection string. **The application server never opens a connection
  as `aicoldb_owner`.**

## 4. Multi-tenant isolation (RLS)

- `internal/tenant.Setup` runs `SELECT set_config('app.workspace_id', $1, true)`
  inside the request transaction.
- Every tenant table has `ENABLE` + `FORCE ROW LEVEL SECURITY` and a policy:
  ```sql
  USING       (workspace_id = current_setting('app.workspace_id', true)::uuid)
  WITH CHECK  (workspace_id = current_setting('app.workspace_id', true)::uuid)
  ```
- `current_setting(..., true)` returns NULL when unset; the comparison is
  NULL, the policy denies — fail closed.
- `scripts/lint-migrations` is a CI gate that fails any PR adding a tenant
  table without the policy.
- `test/security/cross_tenant_test.go` is a matrix test: workspace B's key
  attempts every endpoint against workspace A's data. Every attempt must
  return zero rows or a permission error.

## 5. Postgres-side hardening

- Migration `0004_roles_and_grants.up.sql` revokes `EXECUTE` on filesystem
  escape functions from PUBLIC, dbadmin, and dbuser:
  - `pg_read_file`, `pg_read_binary_file`, `pg_ls_dir`
  - `lo_import`, `lo_export`
- `dblink`, `file_fdw`, `plpython3u`, `plperlu` extensions are not
  installed. `COPY ... FROM PROGRAM` requires `pg_execute_server_program`,
  which is not granted to `dbadmin` or `dbuser`.
- `postgresql.conf` (every profile) sets:
  ```
  log_min_duration_statement = 500
  log_statement              = ddl
  log_connections            = on
  log_disconnections         = on
  password_encryption        = scram-sha-256
  ssl                        = on   # cloud profile
  ```
- The cloud profile binds Postgres to a private docker network only — it is
  not published on the host and not reachable from outside the Caddy
  reverse proxy. The only externally reachable port is 443.

## Cross-cutting controls

- **Audit log** (`audit_logs` table): every authenticated request writes a
  row with `request_id, workspace_id, key_id, endpoint, command, sql_hash,
  params_hash, duration_ms, error_code, client_ip`. The full SQL/params go
  to the slog stream by default; `--audit-include-sql` enables full capture
  for compliance use cases.
- **Rate limiting**: per-key token bucket, default 60 req/s burst 120,
  configurable. Returns HTTP 429 with `Retry-After`.
- **Request size limits**: 1 MiB request body, 8 MiB response body default.
- **Timeouts**: read header 5s, read 10s, write 30s, idle 120s, statement
  timeout 5s (per request, configurable up to 60s).
- **Secrets**: file-backed in compose, `external: true` in swarm. No
  secrets in environment variables in production profiles.
- **Container hardening**: distroless final image, `USER 65532:65532`,
  read-only root filesystem, `cap_drop: [ALL]`, `no-new-privileges`.
- **Dependency scanning**: `govulncheck` and `pip-audit` in CI on every PR.
  CodeQL on the default branch.

## Reporting a vulnerability

Use [GitHub Security Advisories](https://github.com/fheinfling/aicoldb/security/advisories).
**Do not open a public issue.** Critical fixes get a CVE and a patch
release within 7 days of confirmed report.

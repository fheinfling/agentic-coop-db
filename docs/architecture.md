# Architecture

AIColDB is layered. Each layer depends only downward.

```
┌─────────────────────────────────────────────────────────┐
│  httpapi  (chi router, DTOs, error mapping)             │  transport
├─────────────────────────────────────────────────────────┤
│  auth │ sql │ rpc │ vector │ audit                      │  application
├─────────────────────────────────────────────────────────┤
│  tenant (RLS GUC)  │  db (pgxpool, tx helpers)          │  domain
├─────────────────────────────────────────────────────────┤
│  config │ observability │ version                       │  infrastructure
└─────────────────────────────────────────────────────────┘
```

`internal/` is unimportable from outside the module. Only `cmd/server/main.go`
wires the layers together.

## Request flow (POST /v1/sql/execute)

```
client ──HTTPS──► caddy ──HTTP──► api ──pgx──► postgres
                                  │
                                  │  1. parse Authorization header (auth.ParseBearer)
                                  │  2. resolve key (auth.Store + auth.VerifyCache)
                                  │  3. attach WorkspaceContext to ctx
                                  │  4. apply rate limit (httpapi.RateLimit)
                                  │  5. validate sql (sql.Validator)
                                  │  6. begin tx, SET LOCAL ROLE + workspace_id (tenant.Setup)
                                  │  7. execute (sql.Executor)
                                  │  8. commit, write audit row (audit.Writer)
                                  │  9. return JSON response
```

## Roles and the privilege boundary

The pool's login role is `aicoldb_gateway`. It has no privileges of its own
beyond `LOGIN` and `GRANT`-membership in the per-key roles (`dbadmin`,
`dbuser`, and any custom roles minted at runtime).

When a request lands, the executor opens a transaction and runs:

```sql
SET LOCAL statement_timeout = '5s';
SET LOCAL idle_in_transaction_session_timeout = '5s';
SELECT set_config('app.workspace_id', $1, true);
SET LOCAL ROLE "<key.role>";
```

`SET LOCAL ROLE` can only target roles the session's outer role is a member
of, so a `dbuser` key cannot escalate to `dbadmin` even if it controls the
SQL string. Postgres rejects `SET LOCAL ROLE dbadmin` from a `dbuser` session.

When the transaction commits or rolls back, the role and the GUC reset, so
the connection returned to the pool is back to its baseline.

## Why the validator is tiny

The validator only enforces what Postgres role grants cannot:

1. **Parseability** — `pg_query.Parse` must succeed.
2. **Single statement** — rejects `; DROP TABLE x` chains.
3. **Statement size** — 64 KiB cap by default.
4. **Placeholder count** — `$N` references in the AST must equal `len(params)`.
5. **Param count** — 100 max by default.

There is no statement-type allowlist. `SELECT`, `INSERT`, `CREATE TABLE`,
`CREATE USER`, `GRANT`, `VACUUM` are all forwarded. Whether the call
succeeds is decided by Postgres based on the role attached to the API key.

Filesystem/network escape functions (`pg_read_file`, `lo_import`,
`COPY ... FROM PROGRAM`, …) are revoked at the database level by migration
0004 — even an admin key cannot use them through the gateway.

## Where things live

| Concern                       | Path                              |
|-------------------------------|-----------------------------------|
| HTTP transport                | `internal/httpapi/`               |
| API key auth + middleware     | `internal/auth/`                  |
| SQL validator + executor      | `internal/sql/`                   |
| RPC registry + dispatcher     | `internal/rpc/`                   |
| Tenant context (`SET LOCAL`)  | `internal/tenant/`                |
| pgvector helpers              | `internal/vector/`                |
| Audit log writer              | `internal/audit/`                 |
| Pool + tx helpers             | `internal/db/`                    |
| Config (envconfig)            | `internal/config/`                |
| slog/prom/optional OTEL       | `internal/observability/`         |
| ldflags-injected build info   | `internal/version/`               |

Each package has a `doc.go` that describes its responsibilities and
collaboration boundaries.

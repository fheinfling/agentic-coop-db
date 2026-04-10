# API reference

All endpoints under `/v1` require an `Authorization: Bearer acd_<env>_<id>_<secret>`
header. Top-level endpoints (`/healthz`, `/readyz`, `/metrics`) are
unauthenticated.

> **MCP-compatible agents:** Instead of calling the HTTP API directly, you can
> use the [MCP server](mcp.md) which exposes these endpoints as MCP tools.

## POST /v1/sql/execute

Forward a parameterised SQL statement.

**Request body**

```json
{
  "sql":    "INSERT INTO notes(id, body) VALUES ($1, $2)",
  "params": ["1f4d...", "hi"]
}
```

**Headers**

- `Authorization: Bearer acd_...` (required)
- `Idempotency-Key: <uuid>` (optional — see below)

**Response (200)**

```json
{
  "command": "INSERT",
  "columns": [],
  "rows": [],
  "rows_affected": 1,
  "duration_ms": 4
}
```

For `SELECT`, `columns` and `rows` are populated. For DDL/DCL the response
shape is the same; `rows_affected` may be 0.

**Errors**

| HTTP | `title`                | When                                  |
|------|------------------------|---------------------------------------|
| 400  | `parse_error`          | the SQL did not parse                 |
| 400  | `params_mismatch`      | `len(params) != number of $N`         |
| 400  | `multiple_statements`  | more than one top-level statement     |
| 400  | `statement_too_large`  | bigger than `MaxStatementBytes`       |
| 401  | `invalid_api_key`      | missing/malformed/revoked key         |
| 403  | `permission_denied`    | Postgres `42501` (role grant blocks)  |
| 408  | `statement_timeout`    | Postgres `57014`                      |
| 409  | `unique_violation`     | Postgres `23505`                      |
| 429  | `rate_limited`         | per-key bucket exhausted              |
| 500  | `database_error`       | anything else from Postgres           |

Errors are `application/problem+json` (RFC 7807).

## POST /v1/rpc/call

Call a registered RPC by name.

```json
{ "procedure": "upsert_document", "args": { "id": "1f4d...", "body": "hi" } }
```

Same `Idempotency-Key` semantics as `/v1/sql/execute`.

## POST /v1/auth/keys

Create a new API key. Caller must have `pg_role = dbadmin`.

```json
{ "pg_role": "dbuser", "name": "frontend-prod" }
```

## POST /v1/auth/keys/rotate

Rotate the calling key. Returns the new token; the old key remains active
for the configured overlap window (default 24h).

## GET /v1/me

```json
{ "workspace_id": "...", "key_id": "...", "role": "dbadmin", "env": "live", "server": { "version": "0.1.0" } }
```

## GET /healthz · /readyz · /metrics

- `/healthz` — process is up
- `/readyz`  — pool is reachable AND migrations have run
- `/metrics` — prometheus

## Idempotency

Send `Idempotency-Key: <unique-string>` on any mutating request. The server
hashes the request body and stores `(workspace_id, key) → response`. A
replay returns the cached response unchanged. A different body with the
same key returns `409 idempotency_conflict`.

The replay window is 24 h by default. Expired rows are purged
automatically by a background sweep that runs every hour.

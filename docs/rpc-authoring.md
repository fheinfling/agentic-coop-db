# Authoring an RPC

Most users never need to register an RPC — `POST /v1/sql/execute` is enough.
RPCs exist for two cases:

1. You want a stable API surface for clients without giving them DDL.
2. You want a single-call multi-statement transaction (the gateway runs
   each RPC body in its own transaction; CTE-wrapped multi-writes are also
   supported via `db.transaction()` in the SDK).

## Steps

1. **Write the SQL body** under `sql/rpc/<name>.sql`. The body is one
   statement (use a `WITH` chain for multiple writes) that returns a single
   `jsonb` row. It receives the args object as a single text parameter `$1`.

   Example: `sql/rpc/upsert_document.sql`

   ```sql
   WITH args AS (
       SELECT (($1)::jsonb)->>'id'   AS id,
              (($1)::jsonb)->>'body' AS body
   ),
   upserted AS (
       INSERT INTO documents (id, body)
       SELECT id::uuid, body FROM args
       ON CONFLICT (id) DO UPDATE SET body = EXCLUDED.body
       RETURNING id, body
   )
   SELECT row_to_json(upserted)::jsonb FROM upserted;
   ```

2. **Register the procedure** in `internal/rpc/registry.go` (or, for runtime
   registration, write a helper that calls `Registry.Register`). Provide:
   - `Name`, `Version`, `Description`
   - `RequiredRole` — the Postgres role keys must have to call this RPC
   - `Schema` — a JSON Schema (draft 2020-12) that validates `args`

3. **Call it** from your client:

   ```python
   db.rpc_call("upsert_document", {"id": "1f4d...", "body": "hi"})
   ```

## Idempotency

Same as `/v1/sql/execute`. Clients may send `Idempotency-Key`; the
dispatcher will replay the cached response on a duplicate, return 409 on a
hash mismatch, and reclaim expired rows.

## When NOT to use an RPC

- For simple CRUD against a single table — just call `db.execute(...)`.
- For ad-hoc analytics queries — use `db.select(...)`.
- For DDL — DDL is for schema migrations or ad-hoc admin work; both go
  through `/v1/sql/execute`.

## When to register one

- The same multi-statement business operation is called from many places.
- You want to enforce a stable arg shape with JSON Schema validation
  before any SQL touches the database.
- The operation needs to run with a different role than the calling key
  (set `RequiredRole`).

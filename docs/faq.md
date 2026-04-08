# FAQ

### Why a gateway? Why not just expose Postgres on the public internet?

Because exposing Postgres on the public internet is bad. There is no rate
limiter on the wire protocol, the auth methods you actually want to use
(SCRAM with strong passwords, certificate auth) require client-side
configuration that is hard for application code to do correctly, and the
attack surface is enormous. AIColDB lets you keep all of that internal
while still letting any HTTPS client send SQL.

### Why not Supabase / Hasura / PostgREST?

Those are great projects. AIColDB is intentionally smaller. It does not
invent a new query language, does not require server-side procedures, has
no realtime layer, no object storage, no admin web UI. The point is to be
trivial to self-host on a Pi or a 4 GB cloud VM and stay out of the way.

### Why am I forced to use `$N` placeholders?

Because the alternative is f-strings, and f-strings are how you get SQL
injection. The validator counts placeholders found in the AST and rejects
any mismatch with `len(params)`. If you really need dynamic identifiers
(table names, column names), build a small allowlist in your application
and pass through validated identifiers.

### Can I use my own ORM?

Yes — AIColDB does not care. The endpoint is `{sql, params}`, so any layer
that produces parameterised SQL works. SQLAlchemy Core, Knex, sqlx,
sqlc-generated code — all of them. You will not get connection pooling
on the client side because every request is an HTTP call, but the gateway
runs its own pool against Postgres.

### How do I run schema migrations?

Migrations live in `migrations/` and use `golang-migrate`. The migrator is
embedded in the server (`AICOLDB_MIGRATE_ON_START=true` by default) and is
also shipped as a standalone binary `aicoldb-migrate`. For tenant tables,
make sure your migration includes the `ENABLE`/`FORCE`/policy block —
`scripts/lint-migrations` will fail your CI build otherwise.

### What happens if the gateway is down?

Reads return `NetworkError` and the caller decides what to do. Writes can
be queued in the local SQLite retry queue (`aicoldb.queue.Queue`) and
replayed on the next successful flush. The Idempotency-Key is generated
client-side so a replay never duplicates writes.

### How do I add a Postgres extension?

Write a migration: `CREATE EXTENSION IF NOT EXISTS <name>;`. The migrator
runs as `aicoldb_owner`, which is allowed to install extensions. Make
sure the extension is actually installed in the postgres image — for
custom extensions, build your own postgres image and reference it in the
compose file.

### Is this production-ready?

v1 is single-node and intentionally minimal. It is appropriate for
internal tools, side projects, small SaaS deployments, and edge servers.
For multi-region HA, see `docs/features/0013-ha-multi-region.md`.

### How does this work with pgvector?

pgvector is enabled by migration 0005. The Python SDK ships with
`db.vector_upsert()` / `db.vector_search()` helpers, but you can also write
the raw SQL yourself: `SELECT id, embedding <=> $1::vector AS distance
FROM docs ORDER BY embedding <=> $1::vector LIMIT $2`.

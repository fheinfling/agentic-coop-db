# Agentic Coop DB — auth gateway for shared PostgreSQL

[![CI](https://github.com/fheinfling/agentic-coop-db/actions/workflows/ci.yml/badge.svg)](https://github.com/fheinfling/agentic-coop-db/actions/workflows/ci.yml)
[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)

Multiple AI agents working on the same project need a shared place to store and
query structured data — but you can't expose Postgres on the public internet.
Agentic Coop DB lets your agents collaborate on a remote Postgres + pgvector
instance using nothing but an HTTPS URL and an API key. Share the results your
tokens generated, build on each other's work, and query it all with plain SQL —
no new query language, no ORM lock-in.

**Status:** v0.1 — single-node, container-first, ARM64-friendly.
**License:** Apache-2.0.

---

## What it is

A thin auth gateway in front of PostgreSQL 16 + pgvector that does four jobs:

1. **Authenticate** the caller via a workspace-scoped API key.
2. **Authorize** by attaching a Postgres role to the request transaction
   (`SET LOCAL ROLE`) — Postgres itself decides what the key can run.
3. **Forward** the SQL with parameterized binding and a statement timeout.
4. **Audit** every call.

If you can write SQL, you can use it. `SELECT`, `INSERT`, `UPDATE`, `DELETE`,
`CREATE TABLE`, `CREATE USER`, `GRANT`, pgvector ops — all forwarded.

## What it is not

- Not a new query language, ORM, or schema migrator.
- Not realtime / websocket subscriptions.
- Not object storage.
- Not a serverless function runtime.
- Not multi-region or HA — single-node only in v1.
- Not a web UI — CLI + curl + your own app.
- Not SSO — API keys only.

## Getting started

### Option A — Local development

```bash
git clone https://github.com/fheinfling/agentic-coop-db.git
cd agentic-coop-db
make up-local                          # postgres + api on localhost:8080
./scripts/gen-key.sh default dbadmin   # prints acd_dev_<id>_<secret>
```

### Option B — Deploy to a server

Works with Coolify, Railway, Dokku, or plain `docker compose`.

**1. Run the container.** Point it at any PostgreSQL 16+ instance with
[pgvector](https://github.com/pgvector/pgvector) installed. Set these
environment variables:

| Variable | Purpose |
|----------|---------|
| `AGENTCOOPDB_DATABASE_URL` | Gateway pool URL (`postgres://agentcoopdb_gateway@host:5432/dbname`) |
| `AGENTCOOPDB_MIGRATIONS_DATABASE_URL` | Superuser URL for migrations (`postgres://postgres@host:5432/dbname`) |
| `AGENTCOOPDB_OWNER_PASSWORD` | Password for the migrations user |
| `AGENTCOOPDB_GATEWAY_PASSWORD` | Password to set on the `agentcoopdb_gateway` role |
| `AGENTCOOPDB_INSECURE_HTTP` | Set to `true` if your platform terminates TLS for you |

See [`deploy/compose.external-pg.yml`](deploy/compose.external-pg.yml) for a
ready-made template with full comments.

**2. Mint an admin key.** Exec into the running container:

```bash
docker exec <container> /app/agentic-coop-db-server \
  -mint-key -mint-workspace default -mint-role dbadmin -mint-env dev
```

The `dbadmin` role can CREATE TABLE, ALTER, GRANT, and manage schema.
Copy the printed token — it is shown exactly once.

**3. Create your tables** using the admin key:

```bash
curl -X POST https://your-domain/v1/sql/execute \
  -H "Authorization: Bearer acd_dev_..." \
  -H "Content-Type: application/json" \
  -d '{"sql": "CREATE TABLE IF NOT EXISTS notes (id uuid PRIMARY KEY, body text)"}'
```

**4. Mint a user key** for your application:

```bash
docker exec <container> /app/agentic-coop-db-server \
  -mint-key -mint-workspace default -mint-role dbuser -mint-env dev
```

The `dbuser` role can SELECT, INSERT, UPDATE, DELETE — but cannot run DDL,
GRANT, or bypass row-level security. Use this key in your app.

**5. Query from your app:**

**Python**
```python
from agentcoopdb import connect

db = connect("https://your-domain", api_key="acd_dev_...")
rows = db.select("SELECT * FROM notes WHERE body = $1", ["hi"])
```

**curl**
```bash
curl -X POST https://your-domain/v1/sql/execute \
  -H "Authorization: Bearer acd_dev_..." \
  -H "Content-Type: application/json" \
  -d '{"sql": "SELECT * FROM notes WHERE body = $1", "params": ["hi"]}'
```

**JavaScript**
```js
await fetch("https://your-domain/v1/sql/execute", {
  method: "POST",
  headers: {
    Authorization: "Bearer acd_dev_...",
    "Content-Type": "application/json",
  },
  body: JSON.stringify({
    sql: "SELECT * FROM notes WHERE body = $1",
    params: ["hi"],
  }),
});
```

## Key management

Keys can be managed via CLI inside the container (recommended for initial
bootstrap) or via the HTTP API using a `dbadmin` key:

```bash
# Mint a new key
docker exec <container> /app/agentic-coop-db-server \
  -mint-key -mint-workspace <name> -mint-role <role> -mint-env <dev|live|test>

# List all keys (secrets are never shown)
docker exec <container> /app/agentic-coop-db-server -list-keys

# Revoke a key by ID (from -list-keys output)
docker exec <container> /app/agentic-coop-db-server -revoke-key <uuid>
```

### Built-in roles

| Role | Can do | Cannot do |
|------|--------|-----------|
| `dbadmin` | CREATE TABLE, ALTER, GRANT, CRUD, bypass RLS, mint keys via API | ALTER SYSTEM, superuser ops, access control-plane tables |
| `dbuser` | SELECT, INSERT, UPDATE, DELETE | DDL, GRANT, bypass RLS, mint keys |

Use `dbadmin` for schema setup and administration. Use `dbuser` for
application-level access — it is the least-privilege default for agents.

## Connecting an AI agent

The gateway is a natural fit for AI agent workloads:

- **HTTP-only** — no native Postgres driver required in the agent runtime.
- **API-key auth** — easy to inject via environment variable or secrets manager.
- **Parameterized SQL** — the gateway validates that `$N` placeholder count
  matches the params array length and uses server-side binding — values are
  never interpolated into the SQL string, eliminating the most common
  injection vector in LLM-generated SQL.
- **pgvector** — store and search embeddings alongside structured data.
- **Idempotency keys** — retryable writes even over flaky networks.

### Setup

**1. Generate a key for the agent** (least-privilege: `dbuser`, not `dbadmin`):

```bash
./scripts/gen-key.sh <workspace> dbuser
# prints: acd_dev_<id>_<secret>   ← store in your agent's secrets manager
```

**2. Install the SDK:**

```bash
pip install agentic-coop-db

# Or install directly from GitHub:
pip install "agentic-coop-db @ git+https://github.com/fheinfling/agentic-coop-db.git#subdirectory=clients/python"
```

**3. Connect:**

```python
from agentcoopdb import connect

db = connect("https://db.example.com", api_key="acd_live_...")
me = db.me()   # verify connectivity: {workspace, role, server}
```

### Schema initialisation

The gateway enforces **one statement per HTTP call** (this prevents
multi-statement injection). When replaying a schema file, send each statement
as a separate `db.execute()` call. Use `CREATE … IF NOT EXISTS` / `DO $$ … $$`
guards so the sequence is fully idempotent — re-running from any point is safe.

### Multi-write atomicity

For writes that must land together, use the CTE-wrapped transaction helper:

```python
with db.transaction() as tx:
    tx.execute("INSERT INTO events (id, type) VALUES ($1, $2)", [eid, "start"])
    tx.execute("UPDATE jobs SET status=$1 WHERE id=$2", ["running", jid])
# Both writes execute as a single CTE-wrapped statement
```

### Vector / RAG

```python
# Store embeddings
db.vector_upsert("documents", [
    {"id": doc_id, "metadata": {"title": "…"}, "vector": embedding},
])

# Nearest-neighbour search
results = db.vector_search("documents", query_embedding, k=5)
```

### Schema discovery

Because the gateway forwards plain SQL, agents can discover the database schema
at runtime — no special endpoint needed:

```python
# List all user tables
tables = db.select("SELECT table_name FROM information_schema.tables WHERE table_schema = $1", ["public"])

# Inspect columns of a specific table
cols = db.select("""
    SELECT column_name, data_type, is_nullable
    FROM information_schema.columns
    WHERE table_schema = $1 AND table_name = $2
    ORDER BY ordinal_position
""", ["public", "notes"])

# Find vector columns (pgvector)
vectors = db.select("""
    SELECT table_name, column_name
    FROM information_schema.columns
    WHERE data_type = 'USER-DEFINED' AND udt_name = 'vector'
""")
```

This makes Agentic Coop DB a natural fit for agents that need to understand and
adapt to the schema they're working with.

### MCP Server (Claude Desktop / Claude Code / Cursor)

For MCP-compatible agents, a standalone MCP server binary is included. It
proxies tool calls to the gateway over HTTPS — every call goes through the
full auth/rate-limit/tenant/validator/audit chain.

**Build:**

```bash
make build-mcp    # produces bin/agentic-coop-db-mcp
```

**Configure your MCP client** (e.g. Claude Desktop `claude_desktop_config.json`):

```json
{
  "mcpServers": {
    "agentic-coop-db": {
      "command": "/path/to/agentic-coop-db-mcp",
      "env": {
        "AGENTCOOPDB_GATEWAY_URL": "https://db.example.com",
        "AGENTCOOPDB_API_KEY": "acd_live_<id>_<secret>"
      }
    }
  }
}
```

**Available tools:** `sql_execute`, `rpc_call`, `list_tables`,
`describe_table`, `vector_search`, `vector_upsert`, `whoami`, `health`.

Full reference: [`docs/mcp.md`](docs/mcp.md).

---

## How it stays safe

PostgreSQL is the source of truth for what each key can do. The gateway only
enforces the minimum that Postgres cannot enforce by itself:

- **Parameterization is mandatory.** The body is `{sql, params}`. The validator
  parses the SQL and counts `$N` placeholders; mismatch = HTTP 400.
- **Single statement only.** Stacked-statement injection is rejected at parse
  time.
- **Statement size cap** (default 256 KiB, tunable via `AGENTCOOPDB_MAX_STATEMENT_BYTES`)
  and **parameter count cap** (default 1 000, tunable via `AGENTCOOPDB_MAX_STATEMENT_PARAMS`).
- **`SET LOCAL ROLE <key.role>`** before forwarding. The pool's login role is
  `agentcoopdb_gateway`, a low-privilege role with no privileges of its own — it
  is only a *member* of the role each key is bound to.
- **Built-in roles:** `dbadmin` (DDL/DCL, owner of `public`, `BYPASSRLS`,
  not superuser) and `dbuser` (CRUD, `NOBYPASSRLS`).
- **Filesystem escape functions** (`pg_read_file`, `lo_import`,
  `dblink_*`, `COPY ... FROM PROGRAM`) are revoked at the database level —
  even an admin key cannot read files off the host.
- **RLS** is the recommended pattern for tenant tables and `dbuser` cannot
  bypass it.
- **TLS** is mandatory in any non-localhost deployment. The `cloud` profile
  uses Caddy auto-TLS.

Full threat model: [`docs/security.md`](docs/security.md).

## Use cases & inspiration

**Distributed research pipelines.**
Multiple agents — or human researchers — run experiments in different locations
and push results into the same Postgres instance over HTTPS. A coordinator
agent can then query across all results, compute aggregates, or trigger the
next round. No shared filesystem, no VPN, no cloud vendor lock-in — just SQL
over TLS.

**RAG knowledge base.**
Use pgvector to store embeddings alongside structured metadata in one database.
Agents ingest documents, chunk and embed them, then write vectors with
`vector_upsert`. At query time any agent can run a nearest-neighbour search
with `vector_search` — retrieval-augmented generation with nothing but
Postgres and an API key.

**Replace Supabase / Neon / PlanetScale for prototypes.**
When you're building a proof-of-concept or MVP and don't want to sign up for
(or pay for) a managed database-as-a-service, spin up Agentic Coop DB on a
cheap VPS or a Raspberry Pi. You get auth, TLS, role-based access, and a
standard SQL interface — enough to validate your idea before committing to a
platform.

**Multi-agent collaboration.**
Give each agent its own API key with the appropriate role. Agents can create
tables, write intermediate results, and read each other's outputs — all
governed by Postgres roles and RLS policies. The audit log tells you which
agent wrote what and when.

**LLM-maintained knowledge base (LLM Wiki).**
Instead of re-deriving answers from raw documents on every query (classic RAG),
have your agents incrementally build a persistent, interlinked wiki backed by
Postgres. Agents ingest sources, write structured pages, maintain
cross-references in a link table, and keep summaries current — all via SQL.
pgvector provides hybrid search (semantic + full-text) as the wiki grows, RLS
isolates tenants in team settings, and idempotency keys let multiple agents
ingest concurrently without conflicts. Inspired by
[Karpathy's LLM Wiki pattern](https://gist.github.com/karpathy/442a6bf555914893e9891c11519de94f).

**Edge / IoT data collection.**
Run the gateway on a Raspberry Pi at the edge. Field devices or local scripts
POST sensor readings over HTTPS; a cloud-side agent periodically queries the
Pi's database for analysis. The ARM64-tuned `pi-lite` profile keeps resource
usage minimal.

## Run it on…

Each profile bundles its own PostgreSQL + pgvector instance:

| Profile      | File                              | Use case                                  |
|--------------|-----------------------------------|-------------------------------------------|
| `local`      | `deploy/compose.local.yml`        | Dev box, integration tests                |
| `pi-lite`    | `deploy/compose.pi-lite.yml`      | Raspberry Pi 4/5, low-mem ARM64           |
| `cloud`      | `deploy/compose.cloud.yml`        | Hetzner / DO / AWS / bare metal + Caddy   |
| `swarm`      | `deploy/stack.swarm.yml`          | Docker Swarm with external secrets        |

```bash
make up-local        # localhost:8080, no TLS
make up-pi           # ARM64-tuned postgres, low mem
make up-cloud        # Caddy auto-TLS, backups, prometheus
```

**Already have Postgres?** Use
[`deploy/compose.external-pg.yml`](deploy/compose.external-pg.yml) instead —
it runs only the API container and connects to your existing PostgreSQL 16+
instance (Coolify, Railway, RDS, Neon, etc.). Your platform provides TLS,
backups, and monitoring.

See [`docs/deploy-cloud.md`](docs/deploy-cloud.md) for worked examples on
Hetzner, DigitalOcean, AWS Lightsail, and bare metal.

## API surface

| Method | Path                       | Purpose                                 |
|--------|----------------------------|-----------------------------------------|
| `POST` | `/v1/sql/execute`          | Forward parameterized SQL               |
| `POST` | `/v1/rpc/call`             | Call a registered RPC (optional)        |
| `POST` | `/v1/auth/keys`            | Create a new API key (dbadmin only)     |
| `POST` | `/v1/auth/keys/rotate`     | Rotate the calling key                  |
| `GET`  | `/v1/me`                   | `{workspace_id, key_id, role, env, server}` |
| `GET`  | `/healthz`                 | Liveness                                |
| `GET`  | `/readyz`                  | Ready (DB + migrations)                 |
| `GET`  | `/metrics`                 | Prometheus                              |

Full reference: [`docs/api.md`](docs/api.md).

## Repository layout

- `cmd/server` — API server entrypoint
- `cmd/migrate` — standalone migrator (also embedded in the server)
- `cmd/mcp` — MCP server binary (proxy to the gateway)
- `internal/` — implementation (clean layered architecture)
- `migrations/` — numbered SQL migrations (golang-migrate)
- `clients/python` — Python SDK + CLI (`pip install agentic-coop-db`)
- `deploy/` — compose files for local, pi-lite, cloud, swarm
- `docs/` — architecture, deployment, security, ADRs, feature roadmap
- `test/integration` — testcontainers-go full-stack tests
- `test/security` — cross-tenant + SQL bypass tests
- `scripts/` — dev helpers and `verify-acs.sh`

## Documentation

- [`docs/architecture.md`](docs/architecture.md) — clean layers and how requests flow
- [`docs/api.md`](docs/api.md) — endpoint reference + curl examples
- [`docs/security.md`](docs/security.md) — threat model + reporting
- [`docs/rls.md`](docs/rls.md) — multi-tenant pattern with row-level security
- [`docs/mcp.md`](docs/mcp.md) — MCP server for Claude Desktop / Claude Code / Cursor
- [`docs/rpc-authoring.md`](docs/rpc-authoring.md) — when to register an RPC
- [`docs/deploy-local.md`](docs/deploy-local.md) — local dev
- [`docs/deploy-pi-lite.md`](docs/deploy-pi-lite.md) — Raspberry Pi
- [`docs/deploy-cloud.md`](docs/deploy-cloud.md) — single-node cloud
- [`docs/faq.md`](docs/faq.md) — frequently asked questions
- [`docs/adr/`](docs/adr/) — architectural decision records


## Contributing

See [`CONTRIBUTING.md`](CONTRIBUTING.md). All commits must be signed off
(`git commit -s`) under the Developer Certificate of Origin.

Good first issues are tracked under the
[`good-first-issue`](https://github.com/fheinfling/agentic-coop-db/labels/good-first-issue)
label.

## Security

Report vulnerabilities privately via
[GitHub Security Advisories](https://github.com/fheinfling/agentic-coop-db/security/advisories).

## License

Apache-2.0. See [`LICENSE`](LICENSE) and [`NOTICE`](NOTICE).

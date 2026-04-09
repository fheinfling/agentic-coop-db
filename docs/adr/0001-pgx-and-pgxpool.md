# ADR 0001 — pgx (and pgxpool) over database/sql

**Status:** accepted

## Decision

The gateway uses `github.com/jackc/pgx/v5` directly (not via `database/sql`)
and uses `pgxpool` for connection pooling.

## Why

- Native parameter binding without a wrapper layer.
- First-class `pgconn.PgError` exposes the SQLSTATE we need for the HTTP
  error mapper.
- `RowsScan(values...)` is significantly faster than `database/sql` for
  result-shape-unknown queries (every `/v1/sql/execute` call is one).
- `pgxpool` is what every active pgx user uses; it has the right knobs for
  per-connection lifetime, max conns, and concurrency.

## Tradeoffs

- We give up cross-driver portability (`lib/pq`, `mysql`). That is fine —
  Agentic Coop DB is Postgres-only by design.
- pgx's API surface is bigger than `database/sql`. We hide that behind
  `internal/db` so the rest of the codebase only sees a small set of
  helpers (`OpenPool`, `InTx`).

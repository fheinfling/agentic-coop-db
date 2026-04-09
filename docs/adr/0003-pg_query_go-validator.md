# ADR 0003 — pg_query_go for the SQL validator

**Status:** accepted

## Decision

The validator uses `github.com/pganalyze/pg_query_go/v5`, which embeds the
actual PostgreSQL parser as a static library.

## Why

The validator only needs to answer:

1. Is this valid SQL?
2. Is this exactly one statement?
3. How many `$N` placeholders does it reference?

A regex parser would fail (1) immediately and (2) and (3) on anything
non-trivial. We need the real parser. `pg_query_go` is the same parser
Postgres ships with, exposed via cgo, with a small Go wrapper.

## Tradeoffs

- The C source compiles in ~30s on first build. This is acceptable; we
  cache the build via Docker layers and Go's build cache.
- It is the only cgo dep in the tree. The Dockerfile uses
  `CGO_ENABLED=0` for the build because the package vendors the parser
  via go embed-friendly archives in v5.

## Alternatives considered

- A handwritten regex/state machine — fast but unsound.
- Send the SQL to Postgres for `EXPLAIN` and parse the response — adds a
  round trip per request and still does not give us the AST.
- `vitess/parser` — MySQL-flavoured.

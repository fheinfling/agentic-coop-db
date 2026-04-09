---
name: go-client
description: Go SDK with the same surface as the Python client
status: proposed
owner: ""
priority: p2
created: 2026-04-08
updated: 2026-04-08
---

## Problem

Go applications wanting to use Agentic Coop DB currently have to build their own
HTTP client. Most users will copy/paste a snippet that does it wrong (e.g.
no retry, no idempotency-key handling).

## Proposed solution

A `clients/go` package mirroring the Python SDK:

```go
db := agentcoopdb.Connect(ctx, "https://db.example.com", agentcoopdb.WithAPIKey("acd_..."))
res, err := db.Execute(ctx, "INSERT INTO notes(id, body) VALUES ($1, $2)", id, "hi")
rows, err := db.Select(ctx, "SELECT * FROM notes WHERE owner = $1", "alice")
```

Plus an offline retry queue (BoltDB or SQLite via mattn/go-sqlite3).

## Why deferred from v1

The Python SDK + curl cover the v1 quickstart. A Go SDK is high value but
not blocking, and we want to ship the gateway before fragmenting work
across SDKs.

## Acceptance criteria

- `go get github.com/fheinfling/agentic-coop-db/clients/go`
- The Python SDK's test matrix is replicated in Go
- `clients/go/README.md` matches the surface of `clients/python/README.md`

## Open questions

- BoltDB vs SQLite for the offline queue?
- Do we expose a `database/sql` driver? (Probably no — the surface is
  intentionally narrower than `database/sql`.)

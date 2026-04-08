---
name: pgbouncer-frontend
description: Stick PgBouncer in front of postgres for higher concurrency
status: proposed
owner: ""
priority: p3
created: 2026-04-08
updated: 2026-04-08
---

## Problem

The pgxpool tops out at `MaxConns` connections (default 20). For very
write-heavy workloads, that becomes the bottleneck.

## Proposed solution

Add a PgBouncer service to the cloud profile in transaction-pooling mode,
sized to multiplex many gateway connections onto a smaller postgres pool.

## Why deferred from v1

PgBouncer's transaction pooling is incompatible with `SET LOCAL` if the
session is reused mid-transaction. We use `SET LOCAL` heavily, which
requires session pooling — and session pooling does not give us the
multiplexing benefit. Either we redesign to a per-request connection
acquired-and-returned model, or we wait for users to actually need this.

## Acceptance criteria

- Either documented as a non-goal with the technical reason, or shipped
  with a clear "session pooling only" warning.

---
name: distributed-rate-limiting
description: Redis-backed token buckets for multi-replica deployments
status: proposed
owner: ""
priority: p2
created: 2026-04-08
updated: 2026-04-08
---

## Problem

The v1 rate limiter is in-memory per process. With multiple api replicas
the effective limit is `replicas × configured_limit`, which is wrong.

## Proposed solution

Pluggable backend interface in `internal/httpapi/ratelimit.go`. Default
remains the in-memory bucket; an opt-in Redis backend uses the
`redis-rate` Lua script for atomic per-key buckets.

## Why deferred from v1

v1 is single-node by design. Adding a second backend before there is a
real multi-replica use case is premature.

## Acceptance criteria

- Backend interface lives behind a feature flag (env var)
- Existing in-memory limiter behaviour is unchanged when the flag is off
- Integration test runs both backends through the same matrix

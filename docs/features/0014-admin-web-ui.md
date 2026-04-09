---
name: admin-web-ui
description: Read-only admin dashboard (audit log, key inventory)
status: proposed
owner: ""
priority: p2
created: 2026-04-08
updated: 2026-04-08
---

## Problem

Operators currently have to `psql` into the database to inspect the audit
log or rotate a key. A small read-only web UI would make daily ops less
painful without expanding the API surface significantly.

## Proposed solution

A static SPA served from `/admin/` (behind a separate `dbadmin`-only
auth check) that shows:

- Workspace inventory
- API key inventory (created/revoked/last_used)
- Audit log search (by request_id, key, time range)
- Health + queue depth

## Why deferred from v1

The README is explicit that v1 is "CLI + curl + your own app". A web UI
is a much larger maintenance surface (build pipeline, CSP, accessibility)
than the gateway itself.

## Acceptance criteria

- Built from `clients/admin-ui/` with vanilla TS + zero framework dep
- Bundle stays under 100 KB gzipped
- Same API surface as the CLI; no new server endpoints

---
name: sqlstate-http-mapping
description: Stabilise and document the SQLSTATE → HTTP status mapping
status: accepted
owner: ""
priority: p1
created: 2026-04-08
updated: 2026-04-08
---

## Problem

`internal/httpapi/errors.go` maps a handful of SQLSTATEs to specific HTTP
status codes (42501→403, 23505→409, 57014→408, …). The mapping is
documented in `docs/api.md` but not exhaustive, and we have no contract
test pinning the mapping.

## Proposed solution

1. Move the mapping out of code into a single Go map declared in
   `internal/httpapi/sqlstate_map.go` so it is grep-able.
2. Generate `docs/sqlstate.md` from the map at build time.
3. Add a contract test that validates every entry round-trips through the
   HTTP layer.

## Why deferred from v1

The current mapping covers the cases users will hit. Stabilisation is a
post-launch refinement.

## Acceptance criteria

- A single source of truth for SQLSTATE → HTTP
- `docs/sqlstate.md` exists and is generated
- A test asserts every map entry produces the expected HTTP status

# ADR 0002 — chi router

**Status:** accepted

## Decision

Use `github.com/go-chi/chi/v5` as the HTTP router.

## Why

- Stdlib-shaped: handlers are `http.HandlerFunc`, no framework lock-in.
- Pattern-aware: `chi.RouteContext(r.Context()).RoutePattern()` gives us
  bounded-cardinality prometheus labels (`/v1/sql/execute`, not the raw
  client URL).
- Minimal dependency tree.
- Native middleware composition; we already use `middleware.Recoverer`,
  `middleware.RealIP`, `middleware.RequestID`.

## Alternatives considered

- `gorilla/mux` — unmaintained.
- `gin` / `echo` — opinionated frameworks; force a custom Context type.
- `net/http.ServeMux` (1.22+) — sufficient for this surface, but the
  middleware story is still ad-hoc and the route-pattern API is awkward
  to wire into prometheus labels.

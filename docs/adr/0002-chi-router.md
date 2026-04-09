# ADR 0002 — HTTP routing

**Status:** superseded

## Original decision (pre-v0.1.0)

Use `github.com/go-chi/chi/v5` as the HTTP router.

## Superseded (v0.1.0)

Replaced chi with `net/http.ServeMux` (Go 1.22+). The reasons:

- Go 1.22 added method+path routing to the stdlib (`mux.Handle("POST /foo", h)`),
  eliminating the primary reason chi existed.
- This project's API surface has no path parameters, so `r.URL.Path` is
  already bounded-cardinality for prometheus labels — chi's `RoutePattern()`
  adds no value.
- The three chi middleware we used (`RealIP`, `RequestID`, `Recoverer`) are
  each < 20 lines and now live in `internal/httpapi/middleware.go`.
- Removing chi + its transitive deps simplifies the supply chain for
  open-source consumers.

## Alternatives still rejected

- `gorilla/mux` — unmaintained.
- `gin` / `echo` — opinionated frameworks; force a custom Context type.

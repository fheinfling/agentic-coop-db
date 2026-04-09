// Package httpapi is the HTTP transport layer.
//
// It owns the stdlib ServeMux router, the request/response DTOs, the access log,
// the rate limiter, the body-size middleware, and the RFC7807 error
// renderer. It does NOT contain any business logic; it delegates SQL
// execution to internal/sql, RPCs to internal/rpc, and auth to internal/auth.
//
// Endpoints (mounted under /v1):
//
//	POST /v1/sql/execute       — primary endpoint, parameterised SQL forwarding
//	POST /v1/rpc/call          — optional, registered procedures
//	POST /v1/auth/keys/rotate  — rotate the calling key
//	POST /v1/auth/keys         — create a new key (caller must be dbadmin)
//	GET  /v1/me                — { workspace, role, server_version }
//
// Top-level (NOT versioned, no auth):
//
//	GET /healthz   — process is up
//	GET /readyz    — DB + migrations are ready
//	GET /metrics   — prometheus
package httpapi

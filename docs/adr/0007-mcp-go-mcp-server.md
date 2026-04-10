# ADR 0007 — mcp-go for the standalone MCP server

**Status:** accepted
**Date:** 2026-04-10

## Context

We want AI agents using MCP-compatible clients (Claude Desktop, Claude Code,
Cursor) to connect to Agentic Coop DB without writing HTTP calls or installing
the Python SDK. The standard way to do this is to ship an MCP server binary.

## Decision

Add `github.com/mark3labs/mcp-go` as a direct dependency. It is the most widely
used Go implementation of the Model Context Protocol, supports the stdio
transport, and provides helpers for tool registration, input schema definition,
and protocol serialisation.

The dependency is used **exclusively** in `internal/mcp/` and `cmd/mcp/`. It is
never imported by the core server binary (`cmd/server/`), so it has zero impact
on the gateway's build, binary size, or attack surface.

## Alternatives considered

| Alternative | Why rejected |
|-------------|-------------|
| Hand-roll MCP protocol | Non-trivial (~1500 lines for JSON-RPC + stdio framing); the MCP spec is still evolving and tracking it manually is a maintenance burden |
| Embed MCP transport in the existing server | Increases core server complexity; harder to guarantee that MCP calls traverse the full HTTP middleware chain; violates the principle that `cmd/server` is the only wirer |

## Consequences

- The MCP binary (`agentic-coop-db-mcp`) does NOT require CGO — it can be built
  with `CGO_ENABLED=0` as a static binary, unlike the main server which needs
  `pg_query_go`.
- `govulncheck` in CI covers the new dependency automatically.
- The dependency is pinned via `go.sum`.

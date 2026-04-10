// Package mcp implements a standalone MCP (Model Context Protocol) server
// that proxies tool calls to the Agentic Coop DB HTTP gateway.
//
// The package is structured as a thin HTTP client adapter: it speaks MCP on
// one side (via github.com/mark3labs/mcp-go) and plain HTTPS on the other.
// Every tool call results in an authenticated HTTP request to the gateway,
// which enforces the full middleware chain — auth, rate limiting, tenant
// isolation, SQL validation, and audit logging.
//
// This package does NOT import any core server packages (internal/auth,
// internal/sql, internal/tenant, etc.). It depends only on internal/version
// (for the User-Agent header) and the mcp-go library.
package mcp

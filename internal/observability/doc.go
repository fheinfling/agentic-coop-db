// Package observability owns logging, metrics, and (optional) tracing for
// Agentic Coop DB. It is intentionally the only place where prometheus instruments
// are constructed; every other package takes a *Metrics value as a
// dependency. This keeps Prometheus' "must register exactly once" rule
// trivially satisfied.
package observability

// Package config defines the runtime configuration for the Agentic Coop DB gateway.
//
// All configuration is loaded from environment variables prefixed with
// AGENTCOOPDB_. The prefix exists so Agentic Coop DB does not collide with other
// services running in the same compose project (e.g. POSTGRES_*).
//
// `Usage()` renders the full env-var reference, which is what
// `agentic-coop-db-server -help-env` prints.
package config

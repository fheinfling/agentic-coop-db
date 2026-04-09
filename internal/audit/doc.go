// Package audit writes one row per authenticated request to the audit_logs
// table. SQL and params are hashed by default; AGENTCOOPDB_AUDIT_INCLUDE_SQL=true
// also writes the full text for compliance use cases.
package audit

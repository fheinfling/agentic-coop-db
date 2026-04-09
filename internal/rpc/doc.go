// Package rpc implements the optional /v1/rpc/call endpoint.
//
// An RPC is a server-side stored multi-statement business operation that:
//
//   - has a stable name + version
//   - declares its arguments via JSON Schema (draft 2020-12)
//   - executes a SQL body loaded from sql/rpc/<name>.sql at startup
//   - runs under a required Postgres role (e.g. dbadmin or a custom role)
//
// Most users never register an RPC — POST /v1/sql/execute is enough. The
// layer exists for two reasons:
//
//  1. Stable API surface: a frontend can call upsert_document(args) without
//     ever sending raw DDL, while admins still control the schema.
//  2. Multi-statement transactions: a single RPC body can contain
//     INSERT/UPDATE/DELETE in one transaction, with the same idempotency
//     guarantees as the plain SQL endpoint.
//
// Idempotency: every call may include an Idempotency-Key header. The
// dispatcher transitions a row in idempotency_keys through pending -> done
// (or failed). Replays return the cached response unchanged.
package rpc

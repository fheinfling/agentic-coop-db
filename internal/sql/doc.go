// Package sql is the data-plane core: a parser-backed validator and a
// pgxpool-backed executor that runs forwarded SQL inside a per-request
// transaction with the right Postgres role.
//
// The validator is intentionally tiny — it enforces only what Postgres role
// grants cannot:
//
//  1. pg_query.Parse must succeed (the input is valid SQL)
//  2. exactly one top-level statement (rejects ; DROP TABLE x chains)
//  3. statement size <= MaxStatementBytes
//  4. number of $N placeholders == len(params)
//  5. number of params <= MaxStatementParams
//
// There is no statement-type allowlist. SELECT, INSERT, CREATE TABLE,
// CREATE USER, GRANT, VACUUM are all forwarded. Whether the call succeeds
// is decided by Postgres based on the role attached to the API key.
package sql

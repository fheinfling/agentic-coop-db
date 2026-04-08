// Package db owns the pgxpool, the migration runner, and the small set of
// helpers (InTx, GUC setters) that every other domain package uses.
//
// Two distinct connection strings are in play:
//
//   - DATABASE_URL — used by the gateway pool. Login role is
//     aicoldb_gateway, which has no privileges of its own beyond LOGIN.
//   - MIGRATIONS_DATABASE_URL — used by cmd/migrate (and the optional
//     in-process migration runner). Login role is aicoldb_owner, the
//     superuser-equivalent role that owns the schema.
//
// The application server NEVER opens a connection as aicoldb_owner.
package db

// Package vector provides a few helpers around pgvector. The package is
// intentionally tiny — most callers can write their own pgvector SQL via
// /v1/sql/execute. The helpers exist to give the SDK a stable
// db.vector_upsert / db.vector_search shape that does not require the
// caller to know the index DDL.
package vector

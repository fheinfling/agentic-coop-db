-- 0005_pgvector.up.sql
-- Enable pgvector. Runs as agentcoopdb_owner so the CREATE EXTENSION is allowed.
--
-- Index strategy: leave it to the application. IVFFlat / HNSW are both
-- fine; the right choice depends on row count and query mix. internal/vector
-- creates an IVFFlat index above a configurable row threshold (default 10k);
-- below that, sequential scan is fine and cheaper.

BEGIN;
CREATE EXTENSION IF NOT EXISTS vector;
COMMIT;

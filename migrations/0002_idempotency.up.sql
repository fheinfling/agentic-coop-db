-- 0002_idempotency.up.sql
-- Server-side idempotency cache.
--
-- Clients may send an Idempotency-Key header on POST /v1/sql/execute or
-- POST /v1/rpc/call. The dispatcher transitions one row through:
--   pending --(success)--> done   ->   replay returns the cached response
--          --(failure)--> failed  ->   replay returns the cached error
--          --(crash)--> pending   ->   on next attempt, expired rows are
--                                       reclaimed and re-run.
--
-- The (workspace_id, idempotency_key) tuple is unique per workspace so two
-- different workspaces may use the same key string without colliding.

BEGIN;

CREATE TABLE IF NOT EXISTS idempotency_keys (
    id              uuid PRIMARY KEY,
    workspace_id    uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    key             text NOT NULL,
    request_hash    text NOT NULL,                 -- sha256(method|path|sql|params|rpc)
    state           text NOT NULL CHECK (state IN ('pending', 'done', 'failed')),
    status_code     integer,
    response_body   bytea,                          -- gzipped JSON, see internal/rpc/idempotency.go
    created_at      timestamptz NOT NULL DEFAULT now(),
    completed_at    timestamptz,
    expires_at      timestamptz NOT NULL,           -- caller-controlled (default: 24h)
    UNIQUE (workspace_id, key)
);

CREATE INDEX IF NOT EXISTS idempotency_keys_expires_idx ON idempotency_keys(expires_at);

COMMENT ON TABLE idempotency_keys IS
  'Server-side state machine for the Idempotency-Key header. See internal/rpc/idempotency.go.';

COMMIT;

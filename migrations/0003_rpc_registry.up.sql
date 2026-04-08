-- 0003_rpc_registry.up.sql
-- Optional RPC layer.
--
-- An RPC is a server-side stored multi-statement business operation that
-- the gateway exposes via POST /v1/rpc/call. The body of the RPC is loaded
-- from sql/rpc/<name>.sql at startup; the row in this table holds metadata
-- (name, version, JSON Schema for arguments, required Postgres role).
--
-- Most users will never register an RPC. The plain SQL endpoint is enough.
-- This table exists so projects that want to expose a stable API surface
-- (without giving clients DDL) have a place to put it.

BEGIN;

CREATE TABLE IF NOT EXISTS rpc_registry (
    id            uuid PRIMARY KEY,
    name          text NOT NULL,
    version       integer NOT NULL DEFAULT 1,
    args_schema   jsonb NOT NULL,                  -- JSON Schema (draft 2020-12)
    required_role text NOT NULL,                   -- e.g. dbadmin or a custom role
    description   text,
    body_path     text NOT NULL,                   -- relative path under sql/rpc/
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    UNIQUE (name, version)
);

CREATE INDEX IF NOT EXISTS rpc_registry_name_idx ON rpc_registry(name);

COMMIT;

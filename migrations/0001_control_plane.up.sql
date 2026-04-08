-- 0001_control_plane.up.sql
-- The minimum tables AIColDB needs to authenticate, authorize, and audit.
--
-- This migration runs as aicoldb_owner. It creates the schema in the public
-- schema for simplicity; if you want to namespace, change every CREATE TABLE
-- below and update internal/db/queries.go to match.
--
-- Conventions:
--   * Primary keys are uuid v7 minted by the application (ordered, ~1.6KB
--     of entropy per id).
--   * Created/updated timestamps are TIMESTAMPTZ NOT NULL DEFAULT now().
--   * Soft-delete columns are *_at TIMESTAMPTZ NULL (NULL means active).

BEGIN;

-- A workspace is the unit of multi-tenant isolation. Every API key belongs
-- to exactly one workspace, and every tenant table has a workspace_id column.
CREATE TABLE IF NOT EXISTS workspaces (
    id            uuid PRIMARY KEY,
    name          text NOT NULL,
    description   text,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    archived_at   timestamptz
);

CREATE INDEX IF NOT EXISTS workspaces_name_idx ON workspaces(name);

-- API keys.
--
-- The full key sent over the wire is `aic_<env>_<key_id>_<secret>`. The
-- gateway looks up the row by key_id, then verifies <secret> against
-- secret_hash using argon2id. The pg_role is the Postgres role the gateway
-- runs `SET LOCAL ROLE` to before forwarding any SQL — Postgres is the
-- authority for what the key can do.
CREATE TABLE IF NOT EXISTS api_keys (
    id              uuid PRIMARY KEY,
    workspace_id    uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    key_id          text NOT NULL UNIQUE,        -- the lookup component of the bearer token
    secret_hash     text NOT NULL,               -- argon2id (PHC string)
    env             text NOT NULL CHECK (env IN ('dev', 'live', 'test')),
    pg_role         text NOT NULL,               -- target role for SET LOCAL ROLE
    name            text,                        -- human label
    created_at      timestamptz NOT NULL DEFAULT now(),
    last_used_at    timestamptz,
    expires_at      timestamptz,
    revoked_at      timestamptz,
    replaces_key_id uuid REFERENCES api_keys(id) ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS api_keys_workspace_idx ON api_keys(workspace_id) WHERE revoked_at IS NULL;
CREATE INDEX IF NOT EXISTS api_keys_pg_role_idx   ON api_keys(pg_role);

COMMENT ON COLUMN api_keys.pg_role IS
  'Postgres role attached to this key via SET LOCAL ROLE before each request. Validated at insert time against pg_roles + pg_auth_members(aicoldb_gateway).';

-- Audit trail.
--
-- Every authenticated request writes one row. The full SQL/params are NOT
-- written here by default (they are in the slog stream); set
-- AICOLDB_AUDIT_INCLUDE_SQL=true to capture them on this table for compliance.
CREATE TABLE IF NOT EXISTS audit_logs (
    id            uuid PRIMARY KEY,
    request_id    text NOT NULL,
    workspace_id  uuid REFERENCES workspaces(id) ON DELETE SET NULL,
    key_id        uuid REFERENCES api_keys(id)   ON DELETE SET NULL,
    endpoint      text NOT NULL,                  -- e.g. POST /v1/sql/execute
    command       text,                           -- e.g. SELECT, CREATE TABLE
    sql_hash      text,                           -- sha256(sql)
    params_hash   text,                           -- sha256(canonicalized params)
    sql_text      text,                           -- only set if AUDIT_INCLUDE_SQL
    params_json   jsonb,                          -- only set if AUDIT_INCLUDE_SQL
    duration_ms   integer NOT NULL,
    status_code   integer NOT NULL,
    error_code    text,
    sqlstate      text,
    client_ip     inet,
    created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS audit_logs_workspace_created_idx ON audit_logs(workspace_id, created_at DESC);
CREATE INDEX IF NOT EXISTS audit_logs_key_created_idx       ON audit_logs(key_id, created_at DESC);
CREATE INDEX IF NOT EXISTS audit_logs_request_idx           ON audit_logs(request_id);

COMMIT;

-- 0007_split_control_plane_schema.up.sql
-- Move the gateway's own control-plane tables out of `public` and into a
-- dedicated `agentcoopdb` schema that NO API key role can read or write.
--
-- Why this exists:
--
-- Migrations 0001-0003 created the control-plane tables (workspaces,
-- api_keys, audit_logs, idempotency_keys, rpc_registry) in the `public`
-- schema. Migration 0004 made `dbadmin` the owner of `public` so admin
-- API keys could run DDL. The combination meant a `dbadmin` API key could
-- legitimately run `DROP TABLE api_keys` through /v1/sql/execute and erase
-- the gateway's auth state — including its own audit trail.
--
-- This migration enforces the right boundary:
--
--   * `agentcoopdb` schema  — owned by agentcoopdb_owner; only the gateway role
--                          (agentcoopdb_gateway) has CRUD on it. No API key
--                          role can touch it.
--   * `public` schema    — owned by dbadmin; full DDL/DCL allowed for
--                          dbadmin keys, CRUD for dbuser keys. This is
--                          where user data lives.
--
-- After this migration:
--
--   - dbadmin keys can DROP TABLE / CREATE TABLE / GRANT / etc. on any
--     table in `public` (the legitimate use case)
--   - dbadmin keys CANNOT touch agentcoopdb.api_keys, agentcoopdb.audit_logs, ...
--     (Postgres returns 42501 permission denied)
--   - the gateway pool's existing queries (`SELECT FROM api_keys`) keep
--     working unchanged because the gateway role's search_path is set to
--     `agentcoopdb, public` — Postgres resolves bare `api_keys` to
--     `agentcoopdb.api_keys` automatically.

BEGIN;

CREATE SCHEMA IF NOT EXISTS agentcoopdb AUTHORIZATION agentcoopdb_owner;

-- Move the control-plane tables. SET SCHEMA preserves ownership, indexes,
-- foreign keys, and existing grants — but we re-grant explicitly below to
-- be safe.
ALTER TABLE IF EXISTS public.workspaces       SET SCHEMA agentcoopdb;
ALTER TABLE IF EXISTS public.api_keys         SET SCHEMA agentcoopdb;
ALTER TABLE IF EXISTS public.audit_logs       SET SCHEMA agentcoopdb;
ALTER TABLE IF EXISTS public.idempotency_keys SET SCHEMA agentcoopdb;
ALTER TABLE IF EXISTS public.rpc_registry     SET SCHEMA agentcoopdb;

-- Lock down the new schema for API key roles. We revoke from dbadmin and
-- dbuser specifically (not PUBLIC) so that pre-existing database users
-- (e.g. the managed-PG admin role) keep their default access and can
-- still inspect control-plane tables via psql.
REVOKE ALL ON SCHEMA agentcoopdb FROM dbadmin;
REVOKE ALL ON SCHEMA agentcoopdb FROM dbuser;

-- The gateway role gets exactly what it needs: schema USAGE, CRUD on the
-- existing tables, USAGE+SELECT on sequences (in case any of these tables
-- ever uses a serial / identity column).
GRANT USAGE ON SCHEMA agentcoopdb TO agentcoopdb_gateway;
GRANT SELECT, INSERT, UPDATE, DELETE
    ON ALL TABLES IN SCHEMA agentcoopdb
    TO agentcoopdb_gateway;
GRANT USAGE, SELECT
    ON ALL SEQUENCES IN SCHEMA agentcoopdb
    TO agentcoopdb_gateway;

-- Future tables added by agentcoopdb_owner in this schema inherit the same
-- grants automatically — so a new control-plane table doesn't need a
-- one-off GRANT in its migration.
ALTER DEFAULT PRIVILEGES FOR ROLE agentcoopdb_owner IN SCHEMA agentcoopdb
    GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO agentcoopdb_gateway;
ALTER DEFAULT PRIVILEGES FOR ROLE agentcoopdb_owner IN SCHEMA agentcoopdb
    GRANT USAGE, SELECT ON SEQUENCES TO agentcoopdb_gateway;

-- Set search_path on the gateway role so existing Go queries (`FROM api_keys`)
-- resolve to the new schema without source code changes. Note that the
-- session search_path is inherited by SET LOCAL ROLE — dbadmin/dbuser will
-- see `agentcoopdb` in their path too, but Postgres still denies access at
-- the schema-USAGE level, so the privilege boundary holds.
--
-- ALTER ROLE ... SET search_path takes effect on the NEXT connection. The
-- gateway pool is opened by cmd/server AFTER migrations finish, so the
-- first request already sees the new search_path.
ALTER ROLE agentcoopdb_gateway SET search_path TO agentcoopdb, public;

-- dbadmin and dbuser get a `public` search_path so DDL they run lands in
-- the right place by default.
ALTER ROLE dbadmin SET search_path TO public;
ALTER ROLE dbuser  SET search_path TO public;

COMMIT;

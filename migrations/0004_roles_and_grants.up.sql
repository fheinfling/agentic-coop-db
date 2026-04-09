-- 0004_roles_and_grants.up.sql
-- The privilege boundary that makes the gateway safe.
--
-- Four roles are created:
--
--   agentcoopdb_owner   — superuser-equivalent, used ONLY by cmd/migrate.
--                     The application server NEVER opens a connection as
--                     this role. It is what runs DDL during migrations.
--   agentcoopdb_gateway — LOGIN role used by the API server pool. No privileges
--                     of its own beyond LOGIN; member of the per-key roles.
--                     SET LOCAL ROLE is the only privilege-change path and
--                     it is bounded by Postgres' role membership graph.
--   dbadmin         — built-in admin role for keys. CREATEROLE, CREATEDB,
--                     BYPASSRLS, owns the public schema. Not a Postgres
--                     superuser, so cannot ALTER SYSTEM, COPY ... FROM
--                     PROGRAM, or load arbitrary libraries.
--   dbuser          — built-in tenant role. SELECT/INSERT/UPDATE/DELETE on
--                     tenant tables, NOBYPASSRLS. Cannot create roles or
--                     escape RLS.
--
-- Filesystem/network escape functions are revoked from PUBLIC and from the
-- agentcoopdb_* roles below — even an admin key cannot read host files via SQL.

BEGIN;

-- Skip role creation if a role already exists (idempotent migration).
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'agentcoopdb_gateway') THEN
        CREATE ROLE agentcoopdb_gateway LOGIN
            NOCREATEDB NOCREATEROLE NOBYPASSRLS NOREPLICATION;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'dbadmin') THEN
        CREATE ROLE dbadmin
            CREATEDB CREATEROLE BYPASSRLS NOLOGIN;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'dbuser') THEN
        CREATE ROLE dbuser
            NOBYPASSRLS NOLOGIN;
    END IF;
END$$;

-- The gateway login role is a member of the built-in target roles. New
-- custom roles created later by a dbadmin key need an explicit
-- `GRANT custom_role TO agentcoopdb_gateway` (the agentcoopdb CLI does this for
-- you when minting a key for a custom role).
GRANT dbadmin TO agentcoopdb_gateway;
GRANT dbuser  TO agentcoopdb_gateway;

-- dbadmin owns the public schema and can grant on it. (The migration runs
-- as agentcoopdb_owner; the ALTER SCHEMA ... OWNER TO is therefore allowed.)
ALTER SCHEMA public OWNER TO dbadmin;

-- Default tenant CRUD privileges. dbadmin owns objects, so this only
-- matters for tables created via DDL run as dbadmin (the common case).
ALTER DEFAULT PRIVILEGES IN SCHEMA public
    GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO dbuser;
ALTER DEFAULT PRIVILEGES IN SCHEMA public
    GRANT USAGE, SELECT ON SEQUENCES TO dbuser;
ALTER DEFAULT PRIVILEGES IN SCHEMA public
    GRANT EXECUTE ON FUNCTIONS TO dbuser;

-- Filesystem / network escape functions: revoke from EVERYONE in scope.
-- Postgres binds these by oid, so we wrap each REVOKE in DO blocks that
-- skip silently when the function does not exist (e.g. the function was
-- renamed in a newer Postgres version).
DO $$
DECLARE
    func record;
BEGIN
    FOR func IN
        SELECT format('%I.%I(%s)', n.nspname, p.proname, pg_get_function_identity_arguments(p.oid)) AS sig
        FROM pg_proc p
        JOIN pg_namespace n ON n.oid = p.pronamespace
        WHERE p.proname IN (
            'pg_read_file', 'pg_read_binary_file', 'pg_ls_dir',
            'lo_import', 'lo_export'
        )
    LOOP
        EXECUTE format('REVOKE EXECUTE ON FUNCTION %s FROM PUBLIC', func.sig);
        EXECUTE format('REVOKE EXECUTE ON FUNCTION %s FROM dbadmin', func.sig);
        EXECUTE format('REVOKE EXECUTE ON FUNCTION %s FROM dbuser',  func.sig);
    END LOOP;
END$$;

-- COPY ... FROM PROGRAM requires pg_execute_server_program; we explicitly
-- ensure it is not granted. dblink / file_fdw / plpython3u / plperlu are
-- simply not installed (no CREATE EXTENSION migration creates them).
REVOKE pg_execute_server_program FROM dbadmin;
REVOKE pg_execute_server_program FROM dbuser;

-- 0005 will enable pgvector; nothing role-specific is needed for that.

COMMIT;

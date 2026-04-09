-- 0008_rename_identifiers.down.sql

BEGIN;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_namespace WHERE nspname = 'agentcoopdb') THEN
        ALTER SCHEMA agentcoopdb RENAME TO aicoopdb;
    END IF;

    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'agentcoopdb_gateway') THEN
        ALTER ROLE agentcoopdb_gateway RENAME TO aicoopdb_gateway;
    END IF;

    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'agentcoopdb_owner') THEN
        ALTER ROLE agentcoopdb_owner RENAME TO aicoopdb_owner;
    END IF;
END$$;

COMMIT;

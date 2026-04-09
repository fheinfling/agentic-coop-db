-- 0008_rename_identifiers.up.sql
-- Renames the control-plane schema and gateway/owner roles from the old
-- "aicoopdb" prefix to the new "agentcoopdb" prefix.
--
-- Safe to run on databases created by migrations 0001–0007 (old names) AND
-- idempotent for fresh installs created with new names.
--
-- Note: ALTER ROLE ... RENAME requires CREATEROLE or superuser.

BEGIN;

DO $$
BEGIN
    -- Rename the control-plane schema.
    IF EXISTS (SELECT 1 FROM pg_namespace WHERE nspname = 'aicoopdb') THEN
        ALTER SCHEMA aicoopdb RENAME TO agentcoopdb;
    END IF;

    -- Rename the gateway login role.
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'aicoopdb_gateway') THEN
        ALTER ROLE aicoopdb_gateway RENAME TO agentcoopdb_gateway;
    END IF;

    -- Rename the owner/migration role.
    -- EnsureOwnerRole (called before migrations run) may have already created
    -- a stub `agentcoopdb_owner` (NOLOGIN) if the legacy name check was absent
    -- in an older binary. Drop that stub first so the RENAME can succeed.
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'aicoopdb_owner') THEN
        IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'agentcoopdb_owner') THEN
            DROP ROLE agentcoopdb_owner;
        END IF;
        ALTER ROLE aicoopdb_owner RENAME TO agentcoopdb_owner;
    END IF;
END$$;

COMMIT;

-- 0010_reassign_public_ownership.up.sql
-- Transfer ownership of all objects in the public schema to dbadmin.
--
-- Migration 0004 made dbadmin the owner of the public SCHEMA, and
-- migration 0009 granted dbadmin ALL on existing tables. But ALTER TABLE
-- (adding columns, indexes, constraints) requires table OWNERSHIP, not
-- just ALL privileges. Tables created by the migrations role (postgres
-- or agentcoopdb_owner) are still owned by that role — so a dbadmin key
-- cannot ALTER them.
--
-- REASSIGN OWNED BY changes the owner of every object (tables,
-- sequences, functions, types) in all schemas. We scope it to the
-- public schema by running ALTER ... OWNER TO on individual objects
-- via a DO block instead, leaving the agentcoopdb schema untouched.

BEGIN;

DO $$
DECLARE
    obj record;
BEGIN
    -- Tables
    FOR obj IN
        SELECT tablename FROM pg_tables
        WHERE schemaname = 'public' AND tableowner <> 'dbadmin'
    LOOP
        EXECUTE format('ALTER TABLE public.%I OWNER TO dbadmin', obj.tablename);
    END LOOP;

    -- Sequences
    FOR obj IN
        SELECT s.relname FROM pg_class s
        JOIN pg_namespace n ON n.oid = s.relnamespace
        WHERE n.nspname = 'public' AND s.relkind = 'S'
          AND pg_catalog.pg_get_userbyid(s.relowner) <> 'dbadmin'
    LOOP
        EXECUTE format('ALTER SEQUENCE public.%I OWNER TO dbadmin', obj.relname);
    END LOOP;

    -- Views
    FOR obj IN
        SELECT viewname FROM pg_views
        WHERE schemaname = 'public'
          AND viewowner <> 'dbadmin'
    LOOP
        EXECUTE format('ALTER VIEW public.%I OWNER TO dbadmin', obj.viewname);
    END LOOP;
END$$;

COMMIT;

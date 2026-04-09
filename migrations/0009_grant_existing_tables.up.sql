-- 0009_grant_existing_tables.up.sql
-- Fix privilege gaps on tables in the public schema.
--
-- Migration 0004 set ALTER DEFAULT PRIVILEGES so that tables created by
-- the migration role (agentcoopdb_owner / postgres) automatically grant
-- CRUD to dbuser. But:
--
--   1. Tables that ALREADY EXIST in public at the time 0004 ran (or were
--      created by a different role) have no explicit grants — dbadmin and
--      dbuser can't even SELECT them.
--
--   2. Tables created via the API (by a dbadmin key, i.e. owned by the
--      dbadmin role) don't inherit the default privileges from 0004
--      because those defaults only apply to objects created by the role
--      that issued ALTER DEFAULT PRIVILEGES.
--
-- This migration closes both gaps:
--
--   a. Grant dbadmin full access to all EXISTING tables/sequences in
--      public (idempotent — harmless if dbadmin already owns them).
--
--   b. Grant dbuser CRUD on all EXISTING tables/sequences in public.
--
--   c. Set default privileges FOR ROLE dbadmin so that tables created
--      by dbadmin keys automatically grant CRUD to dbuser.

BEGIN;

-- (a) dbadmin gets full control of existing objects in public.
GRANT ALL ON ALL TABLES    IN SCHEMA public TO dbadmin;
GRANT ALL ON ALL SEQUENCES IN SCHEMA public TO dbadmin;

-- (b) dbuser gets CRUD on existing objects in public.
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES    IN SCHEMA public TO dbuser;
GRANT USAGE, SELECT                  ON ALL SEQUENCES IN SCHEMA public TO dbuser;

-- (c) Future tables/sequences created by dbadmin auto-grant to dbuser.
ALTER DEFAULT PRIVILEGES FOR ROLE dbadmin IN SCHEMA public
    GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO dbuser;
ALTER DEFAULT PRIVILEGES FOR ROLE dbadmin IN SCHEMA public
    GRANT USAGE, SELECT ON SEQUENCES TO dbuser;
ALTER DEFAULT PRIVILEGES FOR ROLE dbadmin IN SCHEMA public
    GRANT EXECUTE ON FUNCTIONS TO dbuser;

COMMIT;

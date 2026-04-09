-- 0009_grant_existing_tables.down.sql
-- Reverts the extra grants and default privileges from 0009.up.

BEGIN;

ALTER DEFAULT PRIVILEGES FOR ROLE dbadmin IN SCHEMA public
    REVOKE SELECT, INSERT, UPDATE, DELETE ON TABLES FROM dbuser;
ALTER DEFAULT PRIVILEGES FOR ROLE dbadmin IN SCHEMA public
    REVOKE USAGE, SELECT ON SEQUENCES FROM dbuser;
ALTER DEFAULT PRIVILEGES FOR ROLE dbadmin IN SCHEMA public
    REVOKE EXECUTE ON FUNCTIONS FROM dbuser;

-- Note: we do NOT revoke the per-table grants from (a)/(b) because
-- migration 0004 already set similar default privileges for the
-- migration role. Revoking them here could break tables that were
-- already working before 0009.

COMMIT;

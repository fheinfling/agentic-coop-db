BEGIN;

ALTER ROLE agentcoopdb_gateway RESET search_path;
ALTER ROLE dbadmin          RESET search_path;
ALTER ROLE dbuser           RESET search_path;

ALTER TABLE IF EXISTS agentcoopdb.rpc_registry     SET SCHEMA public;
ALTER TABLE IF EXISTS agentcoopdb.idempotency_keys SET SCHEMA public;
ALTER TABLE IF EXISTS agentcoopdb.audit_logs       SET SCHEMA public;
ALTER TABLE IF EXISTS agentcoopdb.api_keys         SET SCHEMA public;
ALTER TABLE IF EXISTS agentcoopdb.workspaces       SET SCHEMA public;

DROP SCHEMA IF EXISTS agentcoopdb;

COMMIT;

BEGIN;
-- Best-effort revert. Roles are NOT dropped because owning objects would
-- block the drop and cause a destructive cascade.
ALTER SCHEMA public OWNER TO postgres;
COMMIT;

-- 0010_reassign_public_ownership.down.sql
-- There is no safe generic rollback for ownership changes because we
-- don't know the original per-table owner. This is a no-op; if you
-- need to revert, manually ALTER ... OWNER TO the original role.

-- (intentionally empty)

-- sql/rpc/upsert_document.sql
--
-- Insert-or-update a row in the `documents` table by id and return the new
-- row as JSON.
--
-- Args (validated by the registry's JSON schema):
--   { "id": "<uuid>", "body": "<text>" }
--
-- The args object is passed as a single text parameter ($1) and parsed
-- inside Postgres so that the calling key only needs INSERT/UPDATE on
-- `documents` — not on json itself.
--
-- The dispatcher runs this body inside a transaction with SET LOCAL ROLE
-- and SET LOCAL app.workspace_id, so RLS policies on the table are still
-- enforced.

WITH args AS (
    SELECT
        (($1)::jsonb)->>'id'   AS id,
        (($1)::jsonb)->>'body' AS body
),
upserted AS (
    INSERT INTO documents (id, body)
    SELECT a.id::uuid, a.body FROM args a
    ON CONFLICT (id) DO UPDATE SET body = EXCLUDED.body
    RETURNING id, body
)
SELECT row_to_json(upserted)::jsonb FROM upserted;

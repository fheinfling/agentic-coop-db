-- 0006_example_tenant_table.up.sql
-- Canonical tenant table pattern: ENABLE + FORCE row-level security with a
-- policy keyed on app.workspace_id.
--
-- Every tenant table you add MUST follow this pattern. The migration linter
-- (scripts/lint-migrations) fails the build for any new CREATE TABLE that
-- has a workspace_id column but is missing ENABLE RLS / FORCE RLS / a
-- USING+WITH CHECK policy.

BEGIN;

-- Notes is intentionally trivial — it exists as a worked example for the
-- README quickstart. Real tables look the same shape.
CREATE TABLE IF NOT EXISTS notes (
    id            uuid PRIMARY KEY,
    workspace_id  uuid NOT NULL,
    body          text NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS notes_workspace_idx ON notes(workspace_id);

ALTER TABLE notes ENABLE ROW LEVEL SECURITY;
ALTER TABLE notes FORCE  ROW LEVEL SECURITY;

DROP POLICY IF EXISTS notes_workspace_isolation ON notes;
CREATE POLICY notes_workspace_isolation ON notes
    USING       (workspace_id = current_setting('app.workspace_id', true)::uuid)
    WITH CHECK  (workspace_id = current_setting('app.workspace_id', true)::uuid);

GRANT SELECT, INSERT, UPDATE, DELETE ON notes TO dbuser;

-- A second example: documents (used by sql/rpc/upsert_document.sql).
CREATE TABLE IF NOT EXISTS documents (
    id            uuid PRIMARY KEY,
    workspace_id  uuid NOT NULL,
    body          text NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS documents_workspace_idx ON documents(workspace_id);

ALTER TABLE documents ENABLE ROW LEVEL SECURITY;
ALTER TABLE documents FORCE  ROW LEVEL SECURITY;

DROP POLICY IF EXISTS documents_workspace_isolation ON documents;
CREATE POLICY documents_workspace_isolation ON documents
    USING       (workspace_id = current_setting('app.workspace_id', true)::uuid)
    WITH CHECK  (workspace_id = current_setting('app.workspace_id', true)::uuid);

GRANT SELECT, INSERT, UPDATE, DELETE ON documents TO dbuser;

COMMIT;

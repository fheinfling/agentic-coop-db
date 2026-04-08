# Multi-tenant isolation with RLS

Every tenant table has a `workspace_id` column and a row-level security
policy keyed on `current_setting('app.workspace_id')`. The gateway sets
that GUC inside the request transaction; if it is unset, the policy denies.

## The pattern

```sql
CREATE TABLE notes (
    id            uuid PRIMARY KEY,
    workspace_id  uuid NOT NULL,
    body          text NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE notes ENABLE ROW LEVEL SECURITY;
ALTER TABLE notes FORCE  ROW LEVEL SECURITY;

CREATE POLICY notes_workspace_isolation ON notes
    USING       (workspace_id = current_setting('app.workspace_id', true)::uuid)
    WITH CHECK  (workspace_id = current_setting('app.workspace_id', true)::uuid);

GRANT SELECT, INSERT, UPDATE, DELETE ON notes TO dbuser;
```

Three rules:

1. **Always `FORCE`** RLS — without it, the table owner (which is `dbadmin`)
   bypasses RLS. We want even admin keys to be subject to the policy on
   tenant tables.
2. **Use `current_setting('app.workspace_id', true)`** — the second arg is
   `missing_ok`, so it returns NULL instead of erroring. NULL = denied.
3. **Use `WITH CHECK`** — without it, an UPDATE could change `workspace_id`
   to another tenant.

## CI enforcement

`scripts/lint-migrations` walks every `*.up.sql` file under `migrations/`
and asserts that any `CREATE TABLE` with a `workspace_id` column is
accompanied by `ENABLE` + `FORCE` + a policy. The check is regex-based but
adequate for catching forgotten policies. CI fails on any violation.

## Trying it

After `make up-local`, mint two workspaces and a key for each:

```bash
./scripts/gen-key.sh ws-a dbuser
./scripts/gen-key.sh ws-b dbuser
```

Insert a row with the first key, then try to read it with the second key:

```bash
curl -X POST http://localhost:8080/v1/sql/execute \
  -H "Authorization: Bearer aic_dev_<ws-a-key>" \
  -d '{"sql": "INSERT INTO notes(id, workspace_id, body) VALUES ($1, $2, $3)",
       "params": ["00000000-0000-0000-0000-000000000001", "<ws-a-id>", "secret"]}'

curl -X POST http://localhost:8080/v1/sql/execute \
  -H "Authorization: Bearer aic_dev_<ws-b-key>" \
  -d '{"sql": "SELECT * FROM notes"}'
# -> { "rows": [], ... }
```

The cross-tenant denial is enforced by `test/security/cross_tenant_test.go`.

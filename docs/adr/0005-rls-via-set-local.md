# ADR 0005 — Row-level security via SET LOCAL app.workspace_id

**Status:** accepted

## Decision

Tenant isolation uses Postgres row-level security with policies keyed on
`current_setting('app.workspace_id', true)::uuid`. The gateway sets the
GUC inside the request transaction with
`SELECT set_config('app.workspace_id', $1, true)` (third arg = is_local),
then runs `SET LOCAL ROLE "<key.role>"` and forwards the SQL.

## Why

- Postgres enforces the policy on every read and write — there is no way
  for an application bug to bypass it.
- `set_config(..., true)` makes the GUC transaction-local; when the
  transaction commits or rolls back, the value resets, so the connection
  returned to the pool has no residual state.
- `current_setting(..., true)` returns NULL on miss instead of erroring;
  the comparison is NULL, the policy denies — fail closed.

## CI gate

`scripts/lint-migrations` walks every `*.up.sql` under `migrations/` and
fails the build if a `CREATE TABLE` with a `workspace_id` column lacks
`ENABLE` + `FORCE` RLS and a policy that mentions
`current_setting('app.workspace_id', true)`.

## Alternatives considered

- **Prepare different connections per tenant.** Defeats pooling; each
  tenant would need its own Postgres role.
- **Filter in application code.** Easy to forget; impossible to audit;
  failed-closed becomes failed-open.
- **One database per tenant.** Operationally heavy and breaks the
  shared-everything story.

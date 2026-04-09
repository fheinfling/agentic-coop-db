// Package tenant configures the per-request workspace and Postgres role
// inside the request transaction. RLS policies on tenant tables read
// `current_setting('app.workspace_id', true)::uuid`; the role attached to
// the API key is what `SET LOCAL ROLE` switches into.
package tenant

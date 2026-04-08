// Package tenant injects the per-request workspace context and Postgres role
// into the database session via SET LOCAL.
//
// Two GUCs are set on every request transaction:
//
//   - app.workspace_id   — used by RLS policies on tenant tables
//   - role               — set via SET LOCAL ROLE so subsequent statements
//                          run with the privileges Postgres has granted to
//                          the key's role (e.g. dbadmin / dbuser / custom)
//
// Both are scoped to the transaction, so a connection returned to the pool
// reverts to its baseline state (the aicoldb_gateway login role with no
// workspace context).
package tenant

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Setup runs the SET LOCAL statements for the given workspace + role and
// applies the per-request statement_timeout / idle_in_transaction_session_timeout.
func Setup(ctx context.Context, tx pgx.Tx, workspaceID, pgRole string, stmtTimeout, idleTimeout time.Duration) error {
	if workspaceID == "" {
		return errors.New("tenant.Setup: empty workspace_id")
	}
	if pgRole == "" {
		return errors.New("tenant.Setup: empty pg_role")
	}

	// SET LOCAL statement_timeout = '5s'
	if _, err := tx.Exec(ctx, fmt.Sprintf("SET LOCAL statement_timeout = '%dms'", stmtTimeout.Milliseconds())); err != nil {
		return fmt.Errorf("set statement_timeout: %w", err)
	}
	if _, err := tx.Exec(ctx, fmt.Sprintf("SET LOCAL idle_in_transaction_session_timeout = '%dms'", idleTimeout.Milliseconds())); err != nil {
		return fmt.Errorf("set idle_in_transaction_session_timeout: %w", err)
	}

	// SELECT set_config('app.workspace_id', $1, true)  — third arg is_local
	if _, err := tx.Exec(ctx, "SELECT set_config('app.workspace_id', $1, true)", workspaceID); err != nil {
		return fmt.Errorf("set app.workspace_id: %w", err)
	}

	// SET LOCAL ROLE — pg_role is validated at key creation time, but we
	// still treat it as identifier-only and use a quoted identifier in the
	// SQL we build. tx.Exec does not interpolate identifiers safely on its
	// own, so we use the pg_query format helper.
	if !isSafeRoleName(pgRole) {
		return fmt.Errorf("unsafe pg_role identifier: %q", pgRole)
	}
	if _, err := tx.Exec(ctx, `SET LOCAL ROLE "`+pgRole+`"`); err != nil {
		return fmt.Errorf("set local role: %w", err)
	}
	return nil
}

// isSafeRoleName returns true for identifiers consisting of lowercase
// letters, digits, and underscores. Postgres allows more (case-folding,
// quoted strings) but we deliberately restrict the surface to keep the
// `SET LOCAL ROLE "..."` construction injection-proof. The aicoldb CLI
// already enforces this same shape at key creation time.
func isSafeRoleName(s string) bool {
	if s == "" || len(s) > 63 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '_':
		default:
			return false
		}
	}
	return true
}

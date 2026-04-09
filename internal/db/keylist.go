package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// KeySummary is a single row returned by ListKeys. It deliberately omits
// secret_hash and any other sensitive field.
type KeySummary struct {
	ID            string
	WorkspaceName string
	PgRole        string
	Env           string
	Name          string
	Status        string // "active", "revoked", or "expired"
	CreatedAt     time.Time
}

// ListKeys returns every API key in the database with human-readable
// metadata. Secret hashes are never included. The connection is opened
// as the migrations/owner role (same as MintKey).
func ListKeys(ctx context.Context, migrationsURL, ownerPassword string) ([]KeySummary, error) {
	finalURL, err := injectPassword(migrationsURL, ownerPassword)
	if err != nil {
		return nil, fmt.Errorf("inject password: %w", err)
	}
	conn, err := pgx.Connect(ctx, finalURL)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", redactSecrets(err, ownerPassword, urlPassword(migrationsURL)))
	}
	defer func() { _ = conn.Close(ctx) }()

	const q = `
SELECT k.id, coalesce(w.name, ''), k.pg_role, k.env,
       coalesce(k.name, ''), k.revoked_at, k.expires_at, k.created_at
FROM agentcoopdb.api_keys k
LEFT JOIN agentcoopdb.workspaces w ON w.id = k.workspace_id
ORDER BY k.created_at`

	rows, err := conn.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("query: %w", redactSecrets(err, ownerPassword, urlPassword(migrationsURL)))
	}
	defer rows.Close()

	var keys []KeySummary
	for rows.Next() {
		var (
			s         KeySummary
			revokedAt *time.Time
			expiresAt *time.Time
		)
		if err := rows.Scan(&s.ID, &s.WorkspaceName, &s.PgRole, &s.Env,
			&s.Name, &revokedAt, &expiresAt, &s.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		s.Status = keyStatus(revokedAt, expiresAt)
		keys = append(keys, s)
	}
	return keys, rows.Err()
}

// keyStatus derives a human-readable status from the revoked/expires timestamps.
func keyStatus(revokedAt, expiresAt *time.Time) string {
	switch {
	case revokedAt != nil:
		return "revoked"
	case expiresAt != nil && !expiresAt.After(time.Now()):
		return "expired"
	default:
		return "active"
	}
}

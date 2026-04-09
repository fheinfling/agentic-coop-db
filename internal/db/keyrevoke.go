package db

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// RevokeKey marks the key with the given ID as revoked. The connection is
// opened as the migrations/owner role (same as MintKey / ListKeys).
func RevokeKey(ctx context.Context, migrationsURL, ownerPassword, keyID string) (err error) {
	defer func() {
		if err != nil {
			err = redactSecrets(err, ownerPassword, urlPassword(migrationsURL))
		}
	}()

	if _, err := uuid.Parse(keyID); err != nil {
		return fmt.Errorf("invalid key ID %q: %w", keyID, err)
	}

	finalURL, err := injectPassword(migrationsURL, ownerPassword)
	if err != nil {
		return fmt.Errorf("inject password: %w", err)
	}
	conn, err := pgx.Connect(ctx, finalURL)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	tag, err := conn.Exec(ctx,
		`UPDATE agentcoopdb.api_keys SET revoked_at = now() WHERE id = $1 AND revoked_at IS NULL`,
		keyID)
	if err != nil {
		return fmt.Errorf("revoke: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("key %s not found or already revoked", keyID)
	}
	return nil
}

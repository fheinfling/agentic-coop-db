package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrKeyNotFound means we did not find a row for the supplied key_id.
// Surfaced separately so the middleware can return 401 instead of 500.
var ErrKeyNotFound = errors.New("api key not found")

// Store reads and writes api_keys.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore returns a Store backed by the given pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// FindByKeyID looks up the row for the given key_id. The caller still has
// to verify the secret with VerifySecret.
func (s *Store) FindByKeyID(ctx context.Context, keyID string) (*KeyRecord, error) {
	const q = `
SELECT id, workspace_id, key_id, secret_hash, env, pg_role,
       coalesce(name, ''), created_at, last_used_at, expires_at, revoked_at, replaces_key_id
FROM api_keys
WHERE key_id = $1
LIMIT 1`
	row := s.pool.QueryRow(ctx, q, keyID)
	var rec KeyRecord
	var replaces *uuid.UUID
	var workspaceID, idVal uuid.UUID
	if err := row.Scan(
		&idVal,
		&workspaceID,
		&rec.KeyID,
		&rec.SecretHash,
		&rec.Env,
		&rec.PgRole,
		&rec.Name,
		&rec.CreatedAt,
		&rec.LastUsedAt,
		&rec.ExpiresAt,
		&rec.RevokedAt,
		&replaces,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrKeyNotFound
		}
		return nil, fmt.Errorf("api_keys lookup: %w", err)
	}
	rec.ID = idVal.String()
	rec.WorkspaceID = workspaceID.String()
	if replaces != nil {
		s := replaces.String()
		rec.ReplacesKeyID = &s
	}
	return &rec, nil
}

// TouchLastUsed updates last_used_at to now() for the given key id. Best
// effort — failures here should never break a request.
func (s *Store) TouchLastUsed(ctx context.Context, id string) error {
	idUUID, err := uuid.Parse(id)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `UPDATE api_keys SET last_used_at = now() WHERE id = $1`, idUUID)
	return err
}

// CreateKeyInput is the argument to Create.
type CreateKeyInput struct {
	WorkspaceID string
	Env         KeyEnvironment
	PgRole      string
	Name        string
	ExpiresAt   *time.Time
}

// CreatedKey is the return value of Create. FullToken is the only place the
// plaintext bearer token is ever exposed.
type CreatedKey struct {
	ID        string
	KeyID     string
	FullToken string
	Record    *KeyRecord
}

// Create mints a new API key, hashes its secret, validates the role
// against pg_roles + pg_auth_members, and inserts the row in a single tx.
func (s *Store) Create(ctx context.Context, in CreateKeyInput) (*CreatedKey, error) {
	if !in.Env.IsValid() {
		return nil, fmt.Errorf("auth.Create: invalid env %q", in.Env)
	}
	wsID, err := uuid.Parse(in.WorkspaceID)
	if err != nil {
		return nil, fmt.Errorf("workspace_id: %w", err)
	}

	keyID, secret, fullToken, err := Mint(in.Env)
	if err != nil {
		return nil, err
	}
	hash, err := HashSecret(secret)
	if err != nil {
		return nil, err
	}
	id := uuid.New()

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Validate that the target role exists and that aicoldb_gateway is a
	// member (recursively). If not, the key would be unusable.
	var roleOK bool
	if err := tx.QueryRow(ctx, `
SELECT EXISTS (
    SELECT 1
    FROM pg_roles r
    WHERE r.rolname = $1
)`, in.PgRole).Scan(&roleOK); err != nil {
		return nil, fmt.Errorf("role exists check: %w", err)
	}
	if !roleOK {
		return nil, fmt.Errorf("postgres role %q does not exist", in.PgRole)
	}
	var memberOK bool
	if err := tx.QueryRow(ctx, `
SELECT pg_has_role('aicoldb_gateway', $1, 'USAGE')`, in.PgRole).Scan(&memberOK); err != nil {
		return nil, fmt.Errorf("role membership check: %w", err)
	}
	if !memberOK {
		return nil, fmt.Errorf("aicoldb_gateway is not a member of %q — run: GRANT %q TO aicoldb_gateway", in.PgRole, in.PgRole)
	}

	if _, err := tx.Exec(ctx, `
INSERT INTO api_keys (id, workspace_id, key_id, secret_hash, env, pg_role, name, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, ''), $8)`,
		id, wsID, keyID, hash, string(in.Env), in.PgRole, in.Name, in.ExpiresAt,
	); err != nil {
		return nil, fmt.Errorf("insert api_keys: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	rec := &KeyRecord{
		ID:          id.String(),
		WorkspaceID: in.WorkspaceID,
		KeyID:       keyID,
		SecretHash:  hash,
		Env:         in.Env,
		PgRole:      in.PgRole,
		Name:        in.Name,
		CreatedAt:   time.Now().UTC(),
		ExpiresAt:   in.ExpiresAt,
	}
	return &CreatedKey{
		ID:        id.String(),
		KeyID:     keyID,
		FullToken: fullToken,
		Record:    rec,
	}, nil
}

// Revoke marks a key as revoked. Future requests with that key will fail
// at the database lookup.
func (s *Store) Revoke(ctx context.Context, id string) error {
	idUUID, err := uuid.Parse(id)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx,
		`UPDATE api_keys SET revoked_at = now() WHERE id = $1 AND revoked_at IS NULL`, idUUID)
	return err
}

// Rotate creates a fresh key in the same workspace + role and links the new
// row to the old one via replaces_key_id. The old key is left active for
// `overlap` so callers have time to swap their config.
func (s *Store) Rotate(ctx context.Context, oldID string, overlap time.Duration) (*CreatedKey, error) {
	oldUUID, err := uuid.Parse(oldID)
	if err != nil {
		return nil, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var (
		wsID    uuid.UUID
		env     KeyEnvironment
		pgRole  string
		nameVal string
	)
	if err := tx.QueryRow(ctx, `
SELECT workspace_id, env, pg_role, coalesce(name, '')
FROM api_keys
WHERE id = $1`, oldUUID,
	).Scan(&wsID, &env, &pgRole, &nameVal); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrKeyNotFound
		}
		return nil, err
	}

	keyID, secret, fullToken, err := Mint(env)
	if err != nil {
		return nil, err
	}
	hash, err := HashSecret(secret)
	if err != nil {
		return nil, err
	}
	newID := uuid.New()
	if _, err := tx.Exec(ctx, `
INSERT INTO api_keys (id, workspace_id, key_id, secret_hash, env, pg_role, name, replaces_key_id)
VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, ''), $8)`,
		newID, wsID, keyID, hash, string(env), pgRole, nameVal, oldUUID,
	); err != nil {
		return nil, err
	}
	// Schedule the old key to expire after the overlap window.
	if _, err := tx.Exec(ctx, `
UPDATE api_keys SET expires_at = now() + $2::interval
WHERE id = $1 AND revoked_at IS NULL`, oldUUID, overlap.String()); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	return &CreatedKey{
		ID:        newID.String(),
		KeyID:     keyID,
		FullToken: fullToken,
		Record: &KeyRecord{
			ID:          newID.String(),
			WorkspaceID: wsID.String(),
			KeyID:       keyID,
			SecretHash:  hash,
			Env:         env,
			PgRole:      pgRole,
			Name:        nameVal,
			CreatedAt:   time.Now().UTC(),
		},
	}, nil
}

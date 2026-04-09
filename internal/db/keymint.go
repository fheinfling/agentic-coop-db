package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/fheinfling/agentic-coop-db/internal/auth"
)

// MintKey creates a new API key in the database and returns the full
// bearer token. The caller is responsible for displaying the token to
// the user — only the argon2id hash of the secret is persisted, so the
// plaintext value is not recoverable after this function returns.
//
// The function uses the migration user (typically agentcoopdb_owner, or
// the managed-PG superuser in the external-PG profile) to insert into
// agentcoopdb.workspaces and agentcoopdb.api_keys. The workspace is created
// if it does not yet exist.
//
// pgRole must already exist as a Postgres role and the gateway login
// role must be a member of it (the api server enforces this at
// request time via SET LOCAL ROLE). Built-in choices created by
// migration 0004 are `dbadmin` and `dbuser`; custom roles can be added
// later via the API.
//
// env must be one of "dev" / "live" / "test" — these are the only
// values the api_keys.env CHECK constraint accepts.
//
// Errors are scrubbed of secrets (the migrations URL password and the
// owner password) before they bubble up.
func MintKey(
	ctx context.Context,
	migrationsURL, ownerPassword string,
	workspace, pgRole string,
	env auth.KeyEnvironment,
) (fullToken string, err error) {
	defer func() {
		if err != nil {
			err = redactSecrets(err, ownerPassword, urlPassword(migrationsURL))
		}
	}()

	if workspace == "" {
		return "", errors.New("MintKey: empty workspace")
	}
	if pgRole == "" {
		return "", errors.New("MintKey: empty pg_role")
	}
	if !env.IsValid() {
		return "", fmt.Errorf("MintKey: invalid env %q (must be dev / live / test)", env)
	}

	keyID, secret, token, err := auth.Mint(env)
	if err != nil {
		return "", fmt.Errorf("auth.Mint: %w", err)
	}
	hash, err := auth.HashSecret(secret)
	if err != nil {
		return "", fmt.Errorf("auth.HashSecret: %w", err)
	}

	finalURL, err := injectPassword(migrationsURL, ownerPassword)
	if err != nil {
		return "", fmt.Errorf("inject password: %w", err)
	}
	conn, err := pgx.Connect(ctx, finalURL)
	if err != nil {
		return "", fmt.Errorf("connect: %w", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	tx, err := conn.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Find the workspace by name; create it if it doesn't exist. We
	// deliberately do NOT use ON CONFLICT here because workspaces.name
	// has only a non-unique index — there's no constraint to conflict on.
	var wsID uuid.UUID
	err = tx.QueryRow(ctx,
		`SELECT id FROM agentcoopdb.workspaces WHERE name = $1`,
		workspace,
	).Scan(&wsID)
	if errors.Is(err, pgx.ErrNoRows) {
		wsID = uuid.New()
		if _, err = tx.Exec(ctx,
			`INSERT INTO agentcoopdb.workspaces (id, name) VALUES ($1, $2)`,
			wsID, workspace,
		); err != nil {
			return "", fmt.Errorf("create workspace: %w", err)
		}
	} else if err != nil {
		return "", fmt.Errorf("lookup workspace: %w", err)
	}

	keyPK := uuid.New()
	if _, err = tx.Exec(ctx,
		`INSERT INTO agentcoopdb.api_keys
		    (id, workspace_id, key_id, secret_hash, env, pg_role, name)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		keyPK, wsID, keyID, hash, string(env), pgRole, "mint-key",
	); err != nil {
		return "", fmt.Errorf("insert api_key: %w", err)
	}

	if err = tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit: %w", err)
	}

	return token, nil
}

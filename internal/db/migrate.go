package db

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5" // postgres driver
	_ "github.com/golang-migrate/migrate/v4/source/file"     // file:// source
	"github.com/jackc/pgx/v5"
)

// MigrationsDir returns the directory where migrations live.
//
// Resolution order:
//  1. AICOOPDB_MIGRATIONS_DIR if set
//  2. /app/migrations (the path baked into the docker image)
//  3. ./migrations relative to the working directory (dev)
func MigrationsDir() (string, error) {
	if d := os.Getenv("AICOOPDB_MIGRATIONS_DIR"); d != "" {
		return d, nil
	}
	for _, candidate := range []string{"/app/migrations", "migrations"} {
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			abs, err := filepath.Abs(candidate)
			if err != nil {
				return "", err
			}
			return abs, nil
		}
	}
	return "", errors.New("migrations directory not found (set AICOOPDB_MIGRATIONS_DIR)")
}

// RunMigrations applies every pending migration as the role described by
// migrationsURL (typically aicoopdb_owner). It is safe to call repeatedly;
// migrate.ErrNoChange is treated as a no-op.
//
// If `password` is non-empty, it is injected into the URL via net/url so
// the operator can keep the URL string in compose / env files
// password-free and supply the secret separately (e.g. via a docker
// secret file). golang-migrate's pgx/v5 driver only takes a URL, so we
// have to embed the password in the connection string.
//
// Operators write `postgres://...` (the standard scheme) but the
// golang-migrate pgx/v5 driver registers itself under `pgx5://`.
// We rewrite the scheme transparently so operators never need to know.
//
// Any password present in either the URL or the `password` argument is
// scrubbed from returned errors before they bubble up — golang-migrate
// happily embeds the full connection string in its error messages.
func RunMigrations(_ context.Context, migrationsURL, password string) (err error) {
	defer func() {
		err = redactSecrets(err, password, urlPassword(migrationsURL))
	}()
	if migrationsURL == "" {
		return errors.New("RunMigrations: empty migrations URL")
	}
	finalURL, err := injectPassword(migrationsURL, password)
	if err != nil {
		return fmt.Errorf("inject password: %w", err)
	}
	finalURL, err = rewriteSchemeForMigrate(finalURL)
	if err != nil {
		return fmt.Errorf("rewrite scheme: %w", err)
	}
	dir, err := MigrationsDir()
	if err != nil {
		return err
	}
	m, err := migrate.New("file://"+dir, finalURL)
	if err != nil {
		return fmt.Errorf("migrate.New: %w", err)
	}
	defer func() {
		_, _ = m.Close()
	}()
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrate.Up: %w", err)
	}
	return nil
}

// rewriteSchemeForMigrate normalizes the URL scheme to `pgx5`, which is
// the name golang-migrate's pgx/v5 driver registers under. Operators
// write the standard `postgres://` (or `postgresql://`) scheme; pgx
// itself accepts either, so we only need to rewrite when handing the
// URL to golang-migrate.
func rewriteSchemeForMigrate(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	switch u.Scheme {
	case "postgres", "postgresql":
		u.Scheme = "pgx5"
	case "pgx5":
		// already correct
	default:
		return "", fmt.Errorf("unsupported scheme %q (expected postgres / postgresql / pgx5)", u.Scheme)
	}
	return u.String(), nil
}

// urlPassword returns the password embedded in a URL's userinfo section,
// or "" if there is none / the URL fails to parse. Used by callers that
// want to redact the original URL's secret without re-parsing it.
func urlPassword(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.User == nil {
		return ""
	}
	pw, _ := u.User.Password()
	return pw
}

// redactSecrets returns a new error whose message has each non-empty
// secret literally replaced with "REDACTED". Errors are flattened to
// strings — callers that rely on errors.Is / errors.As against the
// underlying type should not use this; for boot-time errors that go
// straight to the logger this is the right tradeoff.
func redactSecrets(err error, secrets ...string) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	changed := false
	for _, s := range secrets {
		if s == "" {
			continue
		}
		if strings.Contains(msg, s) {
			msg = strings.ReplaceAll(msg, s, "REDACTED")
			changed = true
		}
	}
	if !changed {
		return err
	}
	return errors.New(msg)
}

// injectPassword returns rawURL with password set, leaving the rest of the
// URL unchanged. If password is empty, the URL is returned as-is.
func injectPassword(rawURL, password string) (string, error) {
	if password == "" {
		return rawURL, nil
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	if u.User == nil {
		return "", fmt.Errorf("URL %q has no user component to attach a password to", rawURL)
	}
	u.User = url.UserPassword(u.User.Username(), password)
	return u.String(), nil
}

// EnsureOwnerRole creates the `aicoopdb_owner` role if it does not yet
// exist. Migration 0007 references this role as the owner of the new
// `aicoopdb` schema (CREATE SCHEMA ... AUTHORIZATION aicoopdb_owner),
// so it must exist *before* migrations run.
//
// In the bundled-PG profiles (compose.local.yml, compose.cloud.yml,
// compose.pi-lite.yml), the postgres image creates this role at init
// time via POSTGRES_USER=aicoopdb_owner. In the external-PG profile
// the migration user is the managed-PG superuser (e.g. `postgres`) and
// nothing else creates the role — so this function is the bridge.
//
// The role is created as NOLOGIN: it exists only to own a schema and
// hold default privileges. Connection pooling still happens through
// `aicoopdb_gateway`. The function is idempotent — if the role already
// exists (bundled case), the IF NOT EXISTS guard makes it a no-op.
func EnsureOwnerRole(ctx context.Context, migrationsURL, ownerPassword string) (err error) {
	defer func() {
		err = redactSecrets(err, ownerPassword, urlPassword(migrationsURL))
	}()
	finalURL, err := injectPassword(migrationsURL, ownerPassword)
	if err != nil {
		return fmt.Errorf("inject password: %w", err)
	}
	conn, err := pgx.Connect(ctx, finalURL)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer func() { _ = conn.Close(ctx) }()
	const stmt = `
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'aicoopdb_owner') THEN
        CREATE ROLE aicoopdb_owner NOLOGIN CREATEDB CREATEROLE;
    END IF;
END$$;`
	if _, err := conn.Exec(ctx, stmt); err != nil {
		return fmt.Errorf("create aicoopdb_owner: %w", err)
	}
	return nil
}

// SetRolePassword opens a short-lived connection as the migrations role
// (typically aicoopdb_owner) and runs ALTER ROLE <role> WITH PASSWORD.
//
// This is the bridge between cloud deployments and the postgres
// `scram-sha-256` default: the gateway role created by migration 0004 has
// no password until this function is called. dev profiles run postgres
// with `POSTGRES_HOST_AUTH_METHOD=trust` and skip this step entirely.
//
// `role` is validated against a tight identifier whitelist before being
// interpolated. `password` is sent as a SQL string literal — the
// PASSWORD '...' clause is parsed as a literal, not a parameter, so $1
// binding does not apply here. Single quotes are escaped by doubling per
// the SQL standard, which is exactly how Postgres parses string literals.
func SetRolePassword(ctx context.Context, migrationsURL, ownerPassword, role, newPassword string) (err error) {
	defer func() {
		// Scrub both the owner password (used to connect) and the new
		// password (which appears in the ALTER ROLE statement and could
		// surface in a SQL error if the connection drops mid-statement).
		err = redactSecrets(err, ownerPassword, newPassword, urlPassword(migrationsURL))
	}()
	if !isSafeIdent(role) {
		return fmt.Errorf("SetRolePassword: unsafe role identifier %q", role)
	}
	if newPassword == "" {
		return errors.New("SetRolePassword: empty new password")
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
	escaped := strings.ReplaceAll(newPassword, "'", "''")
	stmt := fmt.Sprintf(`ALTER ROLE %q WITH PASSWORD '%s'`, role, escaped)
	if _, err := conn.Exec(ctx, stmt); err != nil {
		return fmt.Errorf("alter role: %w", err)
	}
	return nil
}

// isSafeIdent returns true for identifiers consisting of lowercase letters,
// digits, and underscores. Same restriction as internal/tenant.isSafeRoleName
// — we deliberately keep the surface narrow.
func isSafeIdent(s string) bool {
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

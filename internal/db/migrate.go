package db

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5" // postgres driver
	_ "github.com/golang-migrate/migrate/v4/source/file"    // file:// source
)

// MigrationsDir returns the directory where migrations live.
//
// Resolution order:
//  1. AICOLDB_MIGRATIONS_DIR if set
//  2. /app/migrations (the path baked into the docker image)
//  3. ./migrations relative to the working directory (dev)
func MigrationsDir() (string, error) {
	if d := os.Getenv("AICOLDB_MIGRATIONS_DIR"); d != "" {
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
	return "", errors.New("migrations directory not found (set AICOLDB_MIGRATIONS_DIR)")
}

// RunMigrations applies every pending migration as the role described by
// migrationsURL (typically aicoldb_owner). It is safe to call repeatedly;
// migrate.ErrNoChange is treated as a no-op.
func RunMigrations(_ context.Context, migrationsURL string) error {
	if migrationsURL == "" {
		return errors.New("RunMigrations: empty migrations URL")
	}
	dir, err := MigrationsDir()
	if err != nil {
		return err
	}
	m, err := migrate.New("file://"+dir, migrationsURL)
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

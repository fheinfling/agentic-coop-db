// Command aicoldb-migrate is the standalone migration runner.
//
// Usage:
//
//	AICOLDB_MIGRATIONS_DATABASE_URL=postgres://aicoldb_owner@host/db?sslmode=disable \
//	  aicoldb-migrate up
//
// The same logic is embedded in cmd/server when AICOLDB_MIGRATE_ON_START=true
// (the default), so this binary is only needed when you want to run migrations
// as a one-shot job — e.g. in a kubernetes init container or a CI step.
package main

import (
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	_ "github.com/golang-migrate/migrate/v4/source/file"

	"github.com/fheinfling/aicoldb/internal/db"
)

func main() {
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, `usage: aicoldb-migrate <up|down|version>

env:
  AICOLDB_MIGRATIONS_DATABASE_URL  postgres URL (login role: aicoldb_owner)
  AICOLDB_MIGRATIONS_DIR           override the migrations directory
`)
	}
	flag.Parse()

	cmd := "up"
	if flag.NArg() > 0 {
		cmd = flag.Arg(0)
	}

	url := os.Getenv("AICOLDB_MIGRATIONS_DATABASE_URL")
	if url == "" {
		fmt.Fprintln(os.Stderr, "AICOLDB_MIGRATIONS_DATABASE_URL is required")
		os.Exit(2)
	}

	dir, err := db.MigrationsDir()
	if err != nil {
		fail(err)
	}

	m, err := migrate.New("file://"+dir, url)
	if err != nil {
		fail(fmt.Errorf("migrate.New: %w", err))
	}
	defer m.Close()

	switch cmd {
	case "up":
		if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
			fail(err)
		}
		slog.Default().Info("migrations applied", "dir", dir)
	case "down":
		// down 1 step at a time on purpose; full down is destructive enough that
		// it should never be a single command.
		if err := m.Steps(-1); err != nil && !errors.Is(err, migrate.ErrNoChange) {
			fail(err)
		}
		slog.Default().Info("migration reverted", "dir", dir)
	case "version":
		v, dirty, err := m.Version()
		if err != nil && !errors.Is(err, migrate.ErrNilVersion) {
			fail(err)
		}
		fmt.Printf("version=%d dirty=%t\n", v, dirty)
	default:
		flag.Usage()
		os.Exit(2)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}

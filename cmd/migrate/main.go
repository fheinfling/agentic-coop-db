// Command agentic-coop-db-migrate is the standalone migration runner.
//
// Usage:
//
//	AGENTCOOPDB_MIGRATIONS_DATABASE_URL=postgres://agentcoopdb_owner@host/db?sslmode=disable \
//	AGENTCOOPDB_OWNER_PASSWORD=...                                                         \
//	  agentic-coop-db-migrate up
//
// The same logic is embedded in cmd/server when AGENTCOOPDB_MIGRATE_ON_START=true
// (the default), so this binary is only needed when you want to run migrations
// as a one-shot job — e.g. in a kubernetes init container or a CI step.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strconv"

	"github.com/fheinfling/agentic-coop-db/internal/db"
)

func main() {
	flag.Usage = func() {
		// Fprintln adds a final newline; the heredoc must NOT also end
		// with one or `go vet` flags it as a redundant newline error.
		fmt.Fprintln(os.Stderr, `usage: agentic-coop-db-migrate <up|force <version>>

commands:
  up              apply all pending migrations
  force <version> mark the database as being at <version> with dirty=false,
                  without running any migration files. Use to recover from a
                  dirty state: force to N-1, then run up again.

env:
  AGENTCOOPDB_MIGRATIONS_DATABASE_URL  postgres URL (login role: agentcoopdb_owner)
  AGENTCOOPDB_OWNER_PASSWORD           password for the agentcoopdb_owner role (optional; trust auth otherwise)
  AGENTCOOPDB_OWNER_PASSWORD_FILE      file containing the same (docker secret pattern)
  AGENTCOOPDB_MIGRATIONS_DIR           override the migrations directory`)
	}
	flag.Parse()

	cmd := "up"
	if flag.NArg() > 0 {
		cmd = flag.Arg(0)
	}

	url := os.Getenv("AGENTCOOPDB_MIGRATIONS_DATABASE_URL")
	if url == "" {
		fmt.Fprintln(os.Stderr, "AGENTCOOPDB_MIGRATIONS_DATABASE_URL is required")
		os.Exit(2)
	}
	password := os.Getenv("AGENTCOOPDB_OWNER_PASSWORD")
	if password == "" {
		if path := os.Getenv("AGENTCOOPDB_OWNER_PASSWORD_FILE"); path != "" {
			b, err := os.ReadFile(path)
			if err != nil {
				fail(fmt.Errorf("read AGENTCOOPDB_OWNER_PASSWORD_FILE: %w", err))
			}
			password = string(b)
		}
	}

	switch cmd {
	case "up":
		if err := db.RunMigrations(context.Background(), url, password); err != nil {
			fail(err)
		}
		slog.Default().Info("migrations applied")
	case "force":
		if flag.NArg() < 2 {
			fmt.Fprintln(os.Stderr, "error: force requires a version number (e.g. force 7)")
			flag.Usage()
			os.Exit(2)
		}
		v, err := strconv.Atoi(flag.Arg(1))
		if err != nil || v < 0 {
			fail(fmt.Errorf("force: invalid version %q (must be a non-negative integer)", flag.Arg(1)))
		}
		if err := db.ForceVersion(context.Background(), url, password, v); err != nil {
			fail(err)
		}
		slog.Default().Info("forced migration version", "version", v)
	default:
		flag.Usage()
		os.Exit(2)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}

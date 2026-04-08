//go:build integration

// Package integration contains tests that bring up a real Postgres via
// testcontainers-go and exercise the gateway end-to-end. They are
// build-tagged so `go test -short ./...` skips them.
package integration

import (
	"context"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	tcpg "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/fheinfling/ai-coop-db/internal/audit"
	"github.com/fheinfling/ai-coop-db/internal/auth"
	"github.com/fheinfling/ai-coop-db/internal/config"
	"github.com/fheinfling/ai-coop-db/internal/db"
	"github.com/fheinfling/ai-coop-db/internal/httpapi"
	"github.com/fheinfling/ai-coop-db/internal/observability"
	"github.com/fheinfling/ai-coop-db/internal/rpc"
	sqlpkg "github.com/fheinfling/ai-coop-db/internal/sql"
)

// repoMigrationsDir returns the absolute path of the repo's migrations
// directory, found by walking up from this source file until a go.mod
// is found. Used to set AICOOPDB_MIGRATIONS_DIR before db.RunMigrations
// runs in tests, since the test binary's CWD is test/integration where
// no migrations directory exists.
func repoMigrationsDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok, "runtime.Caller failed")
	dir := filepath.Dir(thisFile)
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return filepath.Join(dir, "migrations")
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatalf("could not find go.mod walking up from %s", thisFile)
	return ""
}

// Harness is the wired-up server bound to a testcontainers Postgres.
type Harness struct {
	T      *testing.T
	Pool   *pgxpool.Pool
	Server *httptest.Server
	Auth   *auth.Store
}

// StartHarness brings up Postgres, runs migrations, wires the API, and
// returns a *Harness with cleanup registered via t.Cleanup.
func StartHarness(t *testing.T) *Harness {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	// IMPORTANT: tcpg.Run does NOT add a default wait strategy. Without
	// tcpg.BasicWaitStrategies() the container is considered ready as
	// soon as it starts, before initdb finishes — and the first
	// migration call gets `connection reset by peer`. BasicWaitStrategies
	// waits for the "database system is ready to accept connections" log
	// to appear twice (postgres restarts itself between init and ready)
	// AND for port 5432/tcp to be reachable on localhost.
	pgC, err := tcpg.Run(ctx,
		"pgvector/pgvector:pg16",
		tcpg.WithDatabase("aicoopdb"),
		tcpg.WithUsername("aicoopdb_owner"),
		tcpg.WithPassword("test"),
		tcpg.BasicWaitStrategies(),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = pgC.Terminate(context.Background()) })

	dsn, err := pgC.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	// The migrations runner looks for ./migrations relative to CWD by
	// default, but tests run from test/integration where no such
	// directory exists. Point it at the repo root explicitly.
	t.Setenv("AICOOPDB_MIGRATIONS_DIR", repoMigrationsDir(t))

	// The testcontainers DSN already embeds the postgres password from
	// tcpg.WithPassword above, so we pass "" as the third arg to leave
	// the URL alone (injectPassword treats an empty password as a no-op).
	require.NoError(t, db.RunMigrations(ctx, dsn, ""))

	// In production the gateway pool's role (aicoopdb_gateway) has its
	// search_path set to "aicoopdb, public" by migration 0007, so bare
	// table names like `api_keys` resolve to `aicoopdb.api_keys`. In
	// these in-process tests we reuse the migration owner DSN as the
	// pool DSN (no separate gateway-role bootstrap), and aicoopdb_owner
	// has the default search_path of "$user", public — which does NOT
	// include aicoopdb. Patch it here so auth.Store and the rest of the
	// API code can keep using bare table names without test-specific
	// changes.
	patchConn, err := pgx.Connect(ctx, dsn)
	require.NoError(t, err)
	_, err = patchConn.Exec(ctx, `ALTER ROLE aicoopdb_owner SET search_path TO aicoopdb, public`)
	require.NoError(t, err)
	require.NoError(t, patchConn.Close(ctx))

	// Reconnect as the gateway role for the pool. The migrations create
	// aicoopdb_gateway and grant it dbadmin/dbuser membership.
	gatewayDSN := dsn // testcontainers gives us the owner DSN; for tests we
	// reuse it because the in-process tests are the only consumer.
	pool, err := db.OpenPool(ctx, db.PoolConfig{URL: gatewayDSN, MaxConns: 5, MinConns: 1})
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	cfg := &config.Config{
		StatementTimeout:   5 * time.Second,
		IdleInTxTimeout:    5 * time.Second,
		DefaultSelectLimit: 100,
		HardSelectLimit:    1000,
		MaxStatementBytes:  64 * 1024,
		MaxStatementParams: 100,
		AuthCacheSize: 32,
		// Tests revoke keys mid-test and immediately call /v1/me to
		// confirm the 401 — that path can only work if the cache TTL
		// is shorter than the test's sleep window. 1 minute (the
		// previous value here) made TestRevokedKeyRejected always
		// observe a stale cache hit.
		AuthCacheTTL:       10 * time.Millisecond,
		KeyRotateOverlap:   1 * time.Hour,
		RateLimitPerSecond: 1000,
		RateLimitBurst:     2000,
	}
	logger := observability.NewLogger("error", "text")
	store := auth.NewStore(pool)
	cache, err := auth.NewVerifyCache(cfg.AuthCacheSize, cfg.AuthCacheTTL)
	require.NoError(t, err)
	mw := auth.NewMiddleware(store, cache, logger)
	metrics := observability.NewMetrics(cache.Len)
	auditor := audit.NewWriter(pool, logger, false)
	validator := sqlpkg.NewValidator(sqlpkg.ValidatorConfig{
		MaxStatementBytes:  cfg.MaxStatementBytes,
		MaxStatementParams: cfg.MaxStatementParams,
	})
	executor := sqlpkg.NewExecutor(pool, sqlpkg.ExecutorConfig{
		StatementTimeout:   cfg.StatementTimeout,
		IdleInTxTimeout:    cfg.IdleInTxTimeout,
		DefaultSelectLimit: cfg.DefaultSelectLimit,
		HardSelectLimit:    cfg.HardSelectLimit,
	})
	registry := rpc.NewRegistry()
	dispatcher := rpc.NewDispatcher(pool, registry, logger)

	api := httpapi.New(httpapi.Deps{
		Config:         cfg,
		Logger:         logger,
		Metrics:        metrics,
		Pool:           pool,
		AuthMiddleware: mw,
		AuthStore:      store,
		Auditor:        auditor,
		Validator:      validator,
		Executor:       executor,
		RPCDispatcher:  dispatcher,
	})
	r := chi.NewRouter()
	r.Mount("/v1", api.Routes())
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	return &Harness{T: t, Pool: pool, Server: srv, Auth: store}
}

// MintWorkspaceAndKey is a tiny helper that creates a workspace + an
// API key with the given role and returns the bearer token.
func (h *Harness) MintWorkspaceAndKey(ctx context.Context, name, role string) (workspaceID, token string) {
	h.T.Helper()
	wsID := newUUID()
	// Migration 0007 moved control-plane tables from `public` to the
	// `aicoopdb` schema. The pool here logs in as aicoopdb_owner whose
	// default search_path is "$user, public", so we have to qualify
	// the schema explicitly. Also: workspaces.name has no UNIQUE
	// constraint so ON CONFLICT (name) won't bind — use a not-exists
	// guard instead.
	_, err := h.Pool.Exec(ctx, `
		INSERT INTO aicoopdb.workspaces (id, name)
		SELECT $1, $2
		WHERE NOT EXISTS (SELECT 1 FROM aicoopdb.workspaces WHERE name = $2)
	`, wsID, name)
	require.NoError(h.T, err)
	created, err := h.Auth.Create(ctx, auth.CreateKeyInput{
		WorkspaceID: wsID,
		Env:         auth.EnvTest,
		PgRole:      role,
		Name:        fmt.Sprintf("test-%s", role),
	})
	require.NoError(h.T, err)
	return wsID, created.FullToken
}

// newUUID returns a fresh uuid v4 string. Imported inline to avoid the
// google/uuid dep churn in the test files.
func newUUID() string {
	return uuidNew()
}

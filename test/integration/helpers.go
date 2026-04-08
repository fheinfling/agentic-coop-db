//go:build integration

// Package integration contains tests that bring up a real Postgres via
// testcontainers-go and exercise the gateway end-to-end. They are
// build-tagged so `go test -short ./...` skips them.
package integration

import (
	"context"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
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

	// Note: do NOT pass tc.WithWaitStrategy(nil) — newer testcontainers
	// (>= v0.34) panics when it tries to deref the nil strategy. Letting
	// the postgres module use its default wait strategy (pg_isready loop)
	// is what we want anyway.
	pgC, err := tcpg.Run(ctx,
		"pgvector/pgvector:pg16",
		tcpg.WithDatabase("aicoopdb"),
		tcpg.WithUsername("aicoopdb_owner"),
		tcpg.WithPassword("test"),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = pgC.Terminate(context.Background()) })

	dsn, err := pgC.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	// The testcontainers DSN already embeds the postgres password from
	// tcpg.WithPassword above, so we pass "" as the third arg to leave
	// the URL alone (injectPassword treats an empty password as a no-op).
	require.NoError(t, db.RunMigrations(ctx, dsn, ""))

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
		AuthCacheSize:      32,
		AuthCacheTTL:       1 * time.Minute,
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
	_, err := h.Pool.Exec(ctx,
		`INSERT INTO workspaces (id, name) VALUES ($1, $2) ON CONFLICT (name) DO NOTHING`,
		wsID, name,
	)
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

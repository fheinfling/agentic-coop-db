// Command ai-coop-db-server is the AI Coop DB API server entrypoint.
//
// All wiring lives here; internal/ packages are intentionally not aware of
// each other beyond the layered dependency direction documented in
// docs/architecture.md.
//
// Lifecycle:
//
//  1. Load config from AICOOPDB_* env vars.
//  2. Build the slog logger and the prometheus registry.
//  3. Open the pgxpool as the low-privilege login role aicoopdb_gateway.
//  4. Optionally run pending migrations (see internal/db.RunMigrations).
//  5. Build the http.Handler tree (chi router).
//  6. Serve until SIGTERM/SIGINT, then drain in-flight requests.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/fheinfling/ai-coop-db/internal/audit"
	"github.com/fheinfling/ai-coop-db/internal/auth"
	"github.com/fheinfling/ai-coop-db/internal/config"
	"github.com/fheinfling/ai-coop-db/internal/db"
	"github.com/fheinfling/ai-coop-db/internal/httpapi"
	"github.com/fheinfling/ai-coop-db/internal/observability"
	"github.com/fheinfling/ai-coop-db/internal/rpc"
	sqlpkg "github.com/fheinfling/ai-coop-db/internal/sql"
	"github.com/fheinfling/ai-coop-db/internal/version"
)

func main() {
	helpEnv := flag.Bool("help-env", false, "print the AICOOPDB_* env var reference and exit")
	showVersion := flag.Bool("version", false, "print version info and exit")
	hashSecret := flag.String("hash-secret", "", "argon2id-hash the given secret and print the PHC string (used by scripts/gen-key.sh)")
	flag.Parse()

	if *showVersion {
		v := version.Get()
		fmt.Printf("ai-coop-db-server %s (%s) built %s\n", v.Version, v.Commit, v.BuildDate)
		return
	}
	if *helpEnv {
		fmt.Println(config.Usage())
		return
	}
	if *hashSecret != "" {
		h, err := auth.HashSecret(*hashSecret)
		if err != nil {
			fmt.Fprintln(os.Stderr, "hash failed:", err)
			os.Exit(1)
		}
		fmt.Println(h)
		return
	}

	if err := run(); err != nil {
		slog.Default().Error("server exited with error", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	logger := observability.NewLogger(cfg.LogLevel, cfg.LogFormat)
	slog.SetDefault(logger)
	logger.Info("starting ai-coop-db-server",
		"version", version.Version,
		"commit", version.Commit,
		"build_date", version.BuildDate,
		"http_addr", cfg.HTTPAddr,
	)

	// Refuse to run plaintext HTTP outside of localhost unless the operator
	// has explicitly opted in.
	if !cfg.InsecureHTTP && !isLocalAddr(cfg.HTTPAddr) {
		return errors.New("plaintext HTTP on a non-localhost address requires AICOOPDB_INSECURE_HTTP=1")
	}

	rootCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Migrations FIRST. /readyz only goes green once schema is at the
	// version this binary expects, so we can't open the gateway pool until
	// the role exists and (when applicable) has its password set.
	var migrationsApplied atomic.Bool
	if cfg.MigrateOnStart {
		// Migration 0007 references aicoopdb_owner as the schema owner.
		// In bundled-PG profiles the role exists from POSTGRES_USER=
		// aicoopdb_owner; in the external-PG profile the migration user
		// is the managed-PG superuser and nothing else creates the role.
		// EnsureOwnerRole is idempotent — a no-op when the role exists.
		logger.Info("ensuring aicoopdb_owner role exists")
		if err := db.EnsureOwnerRole(rootCtx, cfg.MigrationsDatabaseURL, cfg.OwnerPassword); err != nil {
			return fmt.Errorf("ensure owner role: %w", err)
		}
		logger.Info("running pending migrations")
		if err := db.RunMigrations(rootCtx, cfg.MigrationsDatabaseURL, cfg.OwnerPassword); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}

	// If GATEWAY_PASSWORD is set, write it onto the role via the migrations
	// connection (which is the only role that can ALTER ROLE). This is the
	// path the cloud / swarm profiles use; local dev uses
	// POSTGRES_HOST_AUTH_METHOD=trust and leaves it unset.
	if cfg.GatewayPassword != "" {
		logger.Info("setting password on aicoopdb_gateway role")
		if err := db.SetRolePassword(rootCtx, cfg.MigrationsDatabaseURL, cfg.OwnerPassword, "aicoopdb_gateway", cfg.GatewayPassword); err != nil {
			return fmt.Errorf("set gateway password: %w", err)
		}
	}
	migrationsApplied.Store(true)

	// Now open the gateway pool. Login role is aicoopdb_gateway, which has
	// CRUD on the aicoopdb schema only — see migrations 0004 + 0007.
	pool, err := db.OpenPool(rootCtx, db.PoolConfig{
		URL:          cfg.DatabaseURL,
		Password:     cfg.GatewayPassword,
		MaxConns:     cfg.DatabaseMaxConns,
		MinConns:     cfg.DatabaseMinConns,
		ConnLifetime: cfg.DatabaseConnLifetime,
	})
	if err != nil {
		return fmt.Errorf("db: %w", err)
	}
	defer pool.Close()

	// Auth, audit, validator, executor, RPC dispatcher.
	authStore := auth.NewStore(pool)
	authCache, err := auth.NewVerifyCache(cfg.AuthCacheSize, cfg.AuthCacheTTL)
	if err != nil {
		return fmt.Errorf("auth cache: %w", err)
	}
	authMW := auth.NewMiddleware(authStore, authCache, logger)

	metrics := observability.NewMetrics(authCache.Len)

	auditor := audit.NewWriter(pool, logger, cfg.AuditIncludeSQL)

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

	rpcRegistry := rpc.NewRegistry()
	if err := rpc.LoadBuiltins(rpcRegistry); err != nil {
		return fmt.Errorf("rpc registry: %w", err)
	}
	rpcDispatcher := rpc.NewDispatcher(pool, rpcRegistry, logger)

	api := httpapi.New(httpapi.Deps{
		Config:        cfg,
		Logger:        logger,
		Metrics:       metrics,
		Pool:          pool,
		AuthMiddleware: authMW,
		AuthStore:     authStore,
		Auditor:       auditor,
		Validator:     validator,
		Executor:      executor,
		RPCDispatcher: rpcDispatcher,
	})

	router := chi.NewRouter()
	router.Use(middleware.RealIP)
	router.Use(middleware.RequestID)
	router.Use(middleware.Recoverer)
	router.Use(httpapi.AccessLog(logger))
	router.Use(httpapi.MaxBodyBytes(cfg.MaxRequestBodyBytes))
	router.Use(httpapi.MetricsMiddleware(metrics))

	// Liveness — process is up.
	router.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		httpapi.WriteJSON(w, http.StatusOK, map[string]any{
			"status":  "ok",
			"version": version.Get(),
		})
	})
	// Readiness — pool is up AND migrations have completed.
	router.Get("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if !migrationsApplied.Load() {
			httpapi.WriteJSON(w, http.StatusServiceUnavailable, map[string]any{
				"status": "starting",
				"reason": "migrations not yet applied",
			})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := pool.Ping(ctx); err != nil {
			httpapi.WriteJSON(w, http.StatusServiceUnavailable, map[string]any{
				"status": "degraded",
				"reason": "database ping failed",
				"error":  err.Error(),
			})
			return
		}
		httpapi.WriteJSON(w, http.StatusOK, map[string]any{"status": "ready"})
	})
	if cfg.MetricsEnabled {
		router.Handle("/metrics", metrics.Handler())
	}

	// Versioned API surface.
	router.Mount("/v1", api.Routes())

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           router,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		ReadTimeout:       cfg.ReadTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       cfg.IdleTimeout,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("listening", "addr", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-rootCtx.Done():
		logger.Info("signal received, draining")
	case err := <-errCh:
		if err != nil {
			return err
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	logger.Info("server stopped cleanly")
	return nil
}

// isLocalAddr returns true for ":<port>", "localhost:<port>", "127.0.0.1:<port>",
// and "[::1]:<port>". Used as part of the AICOOPDB_INSECURE_HTTP gate.
func isLocalAddr(addr string) bool {
	if addr == "" {
		return false
	}
	if addr[0] == ':' {
		return true
	}
	for _, prefix := range []string{"localhost:", "127.0.0.1:", "[::1]:"} {
		if len(addr) >= len(prefix) && addr[:len(prefix)] == prefix {
			return true
		}
	}
	return false
}

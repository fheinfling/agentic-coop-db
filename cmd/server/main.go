// Command agentic-coop-db-server is the Agentic Coop DB API server entrypoint.
//
// All wiring lives here; internal/ packages are intentionally not aware of
// each other beyond the layered dependency direction documented in
// docs/architecture.md.
//
// Lifecycle:
//
//  1. Load config from AGENTCOOPDB_* env vars.
//  2. Build the slog logger and the prometheus registry.
//  3. Open the pgxpool as the low-privilege login role agentcoopdb_gateway.
//  4. Optionally run pending migrations (see internal/db.RunMigrations).
//  5. Build the http.Handler tree (stdlib ServeMux).
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
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/fheinfling/agentic-coop-db/internal/audit"
	"github.com/fheinfling/agentic-coop-db/internal/auth"
	"github.com/fheinfling/agentic-coop-db/internal/config"
	"github.com/fheinfling/agentic-coop-db/internal/db"
	"github.com/fheinfling/agentic-coop-db/internal/httpapi"
	"github.com/fheinfling/agentic-coop-db/internal/observability"
	"github.com/fheinfling/agentic-coop-db/internal/rpc"
	sqlpkg "github.com/fheinfling/agentic-coop-db/internal/sql"
	"github.com/fheinfling/agentic-coop-db/internal/version"
)

func main() {
	helpEnv := flag.Bool("help-env", false, "print the AGENTCOOPDB_* env var reference and exit")
	showVersion := flag.Bool("version", false, "print version info and exit")
	hashSecret := flag.String("hash-secret", "", "argon2id-hash the given secret and print the PHC string (used by scripts/gen-key.sh)")
	mintKey := flag.Bool("mint-key", false, "mint a new API key, print it once, and exit (uses the migrations DB connection)")
	mintWorkspace := flag.String("mint-workspace", "default", "workspace name for -mint-key (created if missing)")
	mintRole := flag.String("mint-role", "dbadmin", "Postgres role attached to the minted key (must already exist; e.g. dbadmin or dbuser)")
	mintEnv := flag.String("mint-env", "dev", "env tag for the minted key (dev | live | test)")
	listKeys := flag.Bool("list-keys", false, "list all API keys (without secrets) and exit")
	revokeKey := flag.String("revoke-key", "", "revoke the API key with the given ID and exit")
	flag.Parse()

	if *showVersion {
		v := version.Get()
		fmt.Printf("agentic-coop-db-server %s (%s) built %s\n", v.Version, v.Commit, v.BuildDate)
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
	if *mintKey {
		if err := runMintKey(*mintWorkspace, *mintRole, *mintEnv); err != nil {
			fmt.Fprintln(os.Stderr, "mint-key failed:", err)
			os.Exit(1)
		}
		return
	}
	if *listKeys {
		if err := runListKeys(); err != nil {
			fmt.Fprintln(os.Stderr, "list-keys failed:", err)
			os.Exit(1)
		}
		return
	}
	if *revokeKey != "" {
		if err := runRevokeKey(*revokeKey); err != nil {
			fmt.Fprintln(os.Stderr, "revoke-key failed:", err)
			os.Exit(1)
		}
		return
	}

	if err := run(); err != nil {
		slog.Default().Error("server exited with error", "err", err)
		os.Exit(1)
	}
}

// runMintKey is the body of the -mint-key subcommand.
func runMintKey(workspace, pgRole, envTag string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	token, err := db.MintKey(
		ctx,
		cfg.MigrationsDatabaseURL,
		cfg.OwnerPassword,
		workspace,
		pgRole,
		auth.KeyEnvironment(envTag),
	)
	if err != nil {
		return err
	}
	fmt.Println()
	fmt.Println("===== API KEY MINTED (SHOWN ONCE — STORE IT NOW) =====")
	fmt.Println(token)
	fmt.Println("======================================================")
	fmt.Println()
	fmt.Println("Test with:")
	fmt.Println("  curl -H 'Authorization: Bearer " + token + "' https://<your-domain>/v1/me")
	return nil
}

func runListKeys() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	keys, err := db.ListKeys(ctx, cfg.MigrationsDatabaseURL, cfg.OwnerPassword)
	if err != nil {
		return err
	}
	if len(keys) == 0 {
		fmt.Println("no API keys found")
		return nil
	}
	fmt.Printf("%-36s  %-12s  %-7s  %-5s  %-8s  %-20s  %s\n",
		"ID", "WORKSPACE", "ROLE", "ENV", "STATUS", "CREATED", "NAME")
	for _, k := range keys {
		fmt.Printf("%-36s  %-12s  %-7s  %-5s  %-8s  %-20s  %s\n",
			k.ID, truncate(k.WorkspaceName, 12), k.PgRole, k.Env, k.Status, k.CreatedAt.Format("2006-01-02 15:04:05"), k.Name)
	}
	return nil
}

func runRevokeKey(keyID string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := db.RevokeKey(ctx, cfg.MigrationsDatabaseURL, cfg.OwnerPassword, keyID); err != nil {
		return err
	}
	fmt.Printf("key %s revoked\n", keyID)
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	logger := observability.NewLogger(cfg.LogLevel, cfg.LogFormat)
	slog.SetDefault(logger)
	logger.Info("starting agentic-coop-db-server",
		"version", version.Version,
		"commit", version.Commit,
		"build_date", version.BuildDate,
		"http_addr", cfg.HTTPAddr,
	)

	// Refuse to run plaintext HTTP outside of localhost unless the operator
	// has explicitly opted in.
	if !cfg.InsecureHTTP && !isLocalAddr(cfg.HTTPAddr) {
		return errors.New("plaintext HTTP on a non-localhost address requires AGENTCOOPDB_INSECURE_HTTP=1")
	}

	rootCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Migrations FIRST.
	var migrationsApplied atomic.Bool
	if cfg.MigrateOnStart {
		logger.Info("ensuring agentcoopdb_owner role exists")
		if err := db.EnsureOwnerRole(rootCtx, cfg.MigrationsDatabaseURL, cfg.OwnerPassword); err != nil {
			return fmt.Errorf("ensure owner role: %w", err)
		}
		logger.Info("running pending migrations")
		if err := db.RunMigrations(rootCtx, cfg.MigrationsDatabaseURL, cfg.OwnerPassword); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}

	if cfg.GatewayPassword != "" {
		logger.Info("setting password on agentcoopdb_gateway role")
		if err := db.SetRolePassword(rootCtx, cfg.MigrationsDatabaseURL, cfg.OwnerPassword, "agentcoopdb_gateway", cfg.GatewayPassword); err != nil {
			return fmt.Errorf("set gateway password: %w", err)
		}
	}
	migrationsApplied.Store(true)

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
		StatementTimeout: cfg.StatementTimeout,
		IdleInTxTimeout:  cfg.IdleInTxTimeout,
	})

	rpcRegistry := rpc.NewRegistry()
	if err := rpc.LoadBuiltins(rpcRegistry); err != nil {
		return fmt.Errorf("rpc registry: %w", err)
	}
	rpcDispatcher := rpc.NewDispatcher(pool, rpcRegistry, logger)

	api := httpapi.New(httpapi.Deps{
		Config:         cfg,
		Logger:         logger,
		Metrics:        metrics,
		Pool:           pool,
		AuthMiddleware: authMW,
		AuthStore:      authStore,
		Auditor:        auditor,
		Validator:      validator,
		Executor:       executor,
		RPCDispatcher:  rpcDispatcher,
	})

	mux := http.NewServeMux()

	// Liveness — process is up.
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		httpapi.WriteJSON(w, http.StatusOK, map[string]any{
			"status":  "ok",
			"version": version.Get(),
		})
	})
	// Readiness — pool is up AND migrations have completed.
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
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
		mux.Handle("GET /metrics", metrics.Handler())
	}

	// Versioned API surface — strip the /v1 prefix so the inner mux
	// sees paths like /sql/execute, /me, etc.
	mux.Handle("/v1/", http.StripPrefix("/v1", api.Routes()))

	// Middleware chain (outermost first).
	var handler http.Handler = mux
	handler = httpapi.MetricsMiddleware(metrics)(handler)
	handler = httpapi.MaxBodyBytes(cfg.MaxRequestBodyBytes)(handler)
	handler = httpapi.AccessLog(logger)(handler)
	handler = httpapi.Recoverer(logger)(handler)
	handler = httpapi.RequestID(handler)
	handler = httpapi.RealIP(handler)

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
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

// isLocalAddr returns true for "localhost:<port>", "127.0.0.1:<port>",
// and "[::1]:<port>". An address like ":<port>" (which binds 0.0.0.0) is
// NOT considered local because it accepts traffic from all interfaces.
func isLocalAddr(addr string) bool {
	if addr == "" {
		return false
	}
	for _, prefix := range []string{"localhost:", "127.0.0.1:", "[::1]:"} {
		if strings.HasPrefix(addr, prefix) {
			return true
		}
	}
	return false
}

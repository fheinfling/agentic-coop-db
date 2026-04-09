// Package config loads runtime configuration from environment variables.
//
// Every AGENTCOOPDB_* env var is documented in Usage(). Defaults are chosen
// so that the local profile works with no env vars set at all (except
// DATABASE_URL which is required).
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// EnvPrefix is the prefix for every Agentic Coop DB environment variable.
const EnvPrefix = "AGENTCOOPDB"

// Config is the full runtime configuration tree.
type Config struct {
	// HTTP server
	HTTPAddr             string
	ReadHeaderTimeout    time.Duration
	ReadTimeout          time.Duration
	WriteTimeout         time.Duration
	IdleTimeout          time.Duration
	MaxRequestBodyBytes  int64
	MaxResponseBodyBytes int64

	// Plaintext HTTP is allowed only when AGENTCOOPDB_INSECURE_HTTP=1.
	InsecureHTTP bool

	// Database (gateway pool — never connects as superuser)
	DatabaseURL           string
	MigrationsDatabaseURL string
	GatewayPassword       string
	OwnerPassword         string
	DatabaseMaxConns      int32
	DatabaseMinConns      int32
	DatabaseConnLifetime  time.Duration

	// SQL execution
	StatementTimeout   time.Duration
	IdleInTxTimeout    time.Duration
	MaxStatementBytes  int
	MaxStatementParams int

	// Auth
	AuthCacheSize    int
	AuthCacheTTL     time.Duration
	KeyRotateOverlap time.Duration

	// Rate limiting
	RateLimitPerSecond float64
	RateLimitBurst     int

	// Audit
	AuditIncludeSQL bool

	// Observability
	LogLevel       string
	LogFormat      string
	MetricsEnabled bool
	OTELEndpoint   string

	// Run-once flags
	MigrateOnStart bool
}

// Load reads AGENTCOOPDB_* env vars into a fresh Config and validates simple
// invariants. Anything that depends on cross-field state is checked here so
// the server fails fast at startup rather than mid-request.
func Load() (*Config, error) {
	c := &Config{
		HTTPAddr:             envOr("AGENTCOOPDB_HTTP_ADDR", ":8080"),
		InsecureHTTP:         envBool("AGENTCOOPDB_INSECURE_HTTP", false),
		MaxRequestBodyBytes:  1048576,
		MaxResponseBodyBytes: 8388608,

		DatabaseURL:           os.Getenv("AGENTCOOPDB_DATABASE_URL"),
		MigrationsDatabaseURL: os.Getenv("AGENTCOOPDB_MIGRATIONS_DATABASE_URL"),
		GatewayPassword:       os.Getenv("AGENTCOOPDB_GATEWAY_PASSWORD"),
		OwnerPassword:         os.Getenv("AGENTCOOPDB_OWNER_PASSWORD"),

		AuditIncludeSQL: envBool("AGENTCOOPDB_AUDIT_INCLUDE_SQL", false),
		MetricsEnabled:  envBool("AGENTCOOPDB_METRICS_ENABLED", true),
		MigrateOnStart:  envBool("AGENTCOOPDB_MIGRATE_ON_START", true),

		LogLevel:     envOr("AGENTCOOPDB_LOG_LEVEL", "info"),
		LogFormat:    envOr("AGENTCOOPDB_LOG_FORMAT", "json"),
		OTELEndpoint: os.Getenv("AGENTCOOPDB_OTEL_EXPORTER_OTLP_ENDPOINT"),
	}

	if c.DatabaseURL == "" {
		return nil, fmt.Errorf("AGENTCOOPDB_DATABASE_URL is required")
	}
	if c.MigrationsDatabaseURL == "" {
		c.MigrationsDatabaseURL = c.DatabaseURL
	}

	var err error
	if c.ReadHeaderTimeout, err = envDuration("AGENTCOOPDB_READ_HEADER_TIMEOUT", 5*time.Second); err != nil {
		return nil, err
	}
	if c.ReadTimeout, err = envDuration("AGENTCOOPDB_READ_TIMEOUT", 10*time.Second); err != nil {
		return nil, err
	}
	if c.WriteTimeout, err = envDuration("AGENTCOOPDB_WRITE_TIMEOUT", 30*time.Second); err != nil {
		return nil, err
	}
	if c.IdleTimeout, err = envDuration("AGENTCOOPDB_IDLE_TIMEOUT", 120*time.Second); err != nil {
		return nil, err
	}
	if c.StatementTimeout, err = envDuration("AGENTCOOPDB_STATEMENT_TIMEOUT", 5*time.Second); err != nil {
		return nil, err
	}
	if c.IdleInTxTimeout, err = envDuration("AGENTCOOPDB_IDLE_IN_TX_TIMEOUT", 5*time.Second); err != nil {
		return nil, err
	}
	if c.AuthCacheTTL, err = envDuration("AGENTCOOPDB_AUTH_CACHE_TTL", 5*time.Minute); err != nil {
		return nil, err
	}
	if c.KeyRotateOverlap, err = envDuration("AGENTCOOPDB_KEY_ROTATE_OVERLAP", 24*time.Hour); err != nil {
		return nil, err
	}
	if c.DatabaseConnLifetime, err = envDuration("AGENTCOOPDB_DATABASE_CONN_LIFETIME", 30*time.Minute); err != nil {
		return nil, err
	}
	if c.MaxRequestBodyBytes, err = envInt64("AGENTCOOPDB_MAX_REQUEST_BODY_BYTES", 1048576); err != nil {
		return nil, err
	}
	if c.MaxResponseBodyBytes, err = envInt64("AGENTCOOPDB_MAX_RESPONSE_BODY_BYTES", 8388608); err != nil {
		return nil, err
	}
	if c.MaxStatementBytes, err = envInt("AGENTCOOPDB_MAX_STATEMENT_BYTES", 262144); err != nil {
		return nil, err
	}
	if c.MaxStatementParams, err = envInt("AGENTCOOPDB_MAX_STATEMENT_PARAMS", 1000); err != nil {
		return nil, err
	}
	if c.AuthCacheSize, err = envInt("AGENTCOOPDB_AUTH_CACHE_SIZE", 1024); err != nil {
		return nil, err
	}
	if c.RateLimitBurst, err = envInt("AGENTCOOPDB_RATE_LIMIT_BURST", 120); err != nil {
		return nil, err
	}
	if c.RateLimitPerSecond, err = envFloat64("AGENTCOOPDB_RATE_LIMIT_PER_SECOND", 60); err != nil {
		return nil, err
	}

	var i32 int32
	if i32, err = envInt32("AGENTCOOPDB_DATABASE_MAX_CONNS", 20); err != nil {
		return nil, err
	}
	c.DatabaseMaxConns = i32
	if i32, err = envInt32("AGENTCOOPDB_DATABASE_MIN_CONNS", 2); err != nil {
		return nil, err
	}
	c.DatabaseMinConns = i32

	// Resolve `*_FILE` env vars for sensitive secrets (docker / swarm pattern).
	if v, ferr := loadSecretFromFile("AGENTCOOPDB_GATEWAY_PASSWORD", c.GatewayPassword); ferr != nil {
		return nil, ferr
	} else {
		c.GatewayPassword = v
	}
	if v, ferr := loadSecretFromFile("AGENTCOOPDB_OWNER_PASSWORD", c.OwnerPassword); ferr != nil {
		return nil, ferr
	} else {
		c.OwnerPassword = v
	}

	// Cross-field validation.
	if c.StatementTimeout > 60*time.Second {
		return nil, fmt.Errorf("statement_timeout must be <= 60s, got %s", c.StatementTimeout)
	}
	if c.MaxStatementParams <= 0 {
		return nil, fmt.Errorf("MAX_STATEMENT_PARAMS must be > 0")
	}
	return c, nil
}

// Usage returns the env var reference (used by `agentic-coop-db-server -help-env`).
func Usage() string {
	return `Environment variables (prefix: AGENTCOOPDB_):

  HTTP server
    AGENTCOOPDB_HTTP_ADDR                    address to bind (default ":8080")
    AGENTCOOPDB_READ_HEADER_TIMEOUT          (default "5s")
    AGENTCOOPDB_READ_TIMEOUT                 (default "10s")
    AGENTCOOPDB_WRITE_TIMEOUT                (default "30s")
    AGENTCOOPDB_IDLE_TIMEOUT                 (default "120s")
    AGENTCOOPDB_MAX_REQUEST_BODY_BYTES       (default 1048576)
    AGENTCOOPDB_MAX_RESPONSE_BODY_BYTES      (default 8388608)
    AGENTCOOPDB_INSECURE_HTTP                allow plaintext HTTP (default "false")

  Database
    AGENTCOOPDB_DATABASE_URL                 [required] gateway pool URL
    AGENTCOOPDB_MIGRATIONS_DATABASE_URL      superuser URL for migrations (defaults to DATABASE_URL)
    AGENTCOOPDB_GATEWAY_PASSWORD             password for agentcoopdb_gateway role
    AGENTCOOPDB_OWNER_PASSWORD               password for agentcoopdb_owner role
    AGENTCOOPDB_DATABASE_MAX_CONNS           (default 20)
    AGENTCOOPDB_DATABASE_MIN_CONNS           (default 2)
    AGENTCOOPDB_DATABASE_CONN_LIFETIME       (default "30m")

  SQL execution
    AGENTCOOPDB_STATEMENT_TIMEOUT            per-request timeout, max 60s (default "5s")
    AGENTCOOPDB_IDLE_IN_TX_TIMEOUT           (default "5s")
    AGENTCOOPDB_MAX_STATEMENT_BYTES          (default 262144)
    AGENTCOOPDB_MAX_STATEMENT_PARAMS         (default 1000)

  Auth
    AGENTCOOPDB_AUTH_CACHE_SIZE              LRU verify cache entries (default 1024)
    AGENTCOOPDB_AUTH_CACHE_TTL               (default "5m")
    AGENTCOOPDB_KEY_ROTATE_OVERLAP           (default "24h")

  Rate limiting
    AGENTCOOPDB_RATE_LIMIT_PER_SECOND        (default 60)
    AGENTCOOPDB_RATE_LIMIT_BURST             (default 120)

  Audit
    AGENTCOOPDB_AUDIT_INCLUDE_SQL            store full SQL in audit_logs (default "false")

  Observability
    AGENTCOOPDB_LOG_LEVEL                    (default "info")
    AGENTCOOPDB_LOG_FORMAT                   json or text (default "json")
    AGENTCOOPDB_METRICS_ENABLED              (default "true")
    AGENTCOOPDB_OTEL_EXPORTER_OTLP_ENDPOINT optional OTLP collector

  Run-once
    AGENTCOOPDB_MIGRATE_ON_START             (default "true")

  Secrets: every password variable also accepts a *_FILE variant
  (e.g. AGENTCOOPDB_GATEWAY_PASSWORD_FILE) for docker / swarm secrets.
`
}

// ---- env helpers (stdlib only) -------------------------------------------------

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}

func envInt(key string, fallback int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return n, nil
}

func envInt32(key string, fallback int32) (int32, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	n, err := strconv.ParseInt(v, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return int32(n), nil
}

func envInt64(key string, fallback int64) (int64, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return n, nil
}

func envFloat64(key string, fallback float64) (float64, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return f, nil
}

func envDuration(key string, fallback time.Duration) (time.Duration, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return d, nil
}

// loadSecretFromFile resolves the docker-style `<name>_FILE` env var.
func loadSecretFromFile(envName, current string) (string, error) {
	if current != "" {
		return current, nil
	}
	path := os.Getenv(envName + "_FILE")
	if path == "" {
		return "", nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s_FILE %q: %w", envName, path, err)
	}
	return strings.TrimSpace(string(b)), nil
}

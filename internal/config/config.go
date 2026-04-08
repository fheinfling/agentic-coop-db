// Package config loads runtime configuration from environment variables.
//
// Every option is documented inline so that `envconfig.Usage` can render a
// complete reference (used by `ai-coop-db-server -help-env`). Defaults are
// chosen so that the local profile works with no env vars set at all.
package config

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/kelseyhightower/envconfig"
)

// EnvPrefix is the prefix for every AI Coop DB environment variable.
const EnvPrefix = "AICOOPDB"

// Config is the full runtime configuration tree.
type Config struct {
	// HTTP server
	HTTPAddr             string        `envconfig:"HTTP_ADDR" default:":8080" desc:"address the HTTP server binds to"`
	ReadHeaderTimeout    time.Duration `envconfig:"READ_HEADER_TIMEOUT" default:"5s"`
	ReadTimeout          time.Duration `envconfig:"READ_TIMEOUT" default:"10s"`
	WriteTimeout         time.Duration `envconfig:"WRITE_TIMEOUT" default:"30s"`
	IdleTimeout          time.Duration `envconfig:"IDLE_TIMEOUT" default:"120s"`
	MaxRequestBodyBytes  int64         `envconfig:"MAX_REQUEST_BODY_BYTES" default:"1048576"`
	MaxResponseBodyBytes int64         `envconfig:"MAX_RESPONSE_BODY_BYTES" default:"8388608"`

	// Plaintext HTTP is allowed only when AICOOPDB_INSECURE_HTTP=1. Any other
	// deployment must terminate TLS in front of the gateway.
	InsecureHTTP bool `envconfig:"INSECURE_HTTP" default:"false"`

	// Database (gateway pool — never connects as superuser)
	DatabaseURL           string `envconfig:"DATABASE_URL" required:"true" desc:"postgres URL the gateway pool uses (login role: aicoopdb_gateway)"`
	MigrationsDatabaseURL string `envconfig:"MIGRATIONS_DATABASE_URL" desc:"postgres URL used by cmd/migrate (login role: aicoopdb_owner). defaults to DATABASE_URL"`
	// GatewayPassword is the password for the aicoopdb_gateway role. When
	// set, the server runs ALTER ROLE WITH PASSWORD on it after migrations
	// (so the role gets a password without baking it into a migration file)
	// and injects it into the gateway pool's connection config. When empty
	// (the local / pi-lite profiles, where postgres uses
	// POSTGRES_HOST_AUTH_METHOD=trust), no password is set.
	//
	// `*_FILE` variant: AICOOPDB_GATEWAY_PASSWORD_FILE points at a file
	// (typically /run/secrets/aicoopdb_gateway_password) whose contents are
	// used. This is the standard docker / swarm secret-mount pattern.
	GatewayPassword string `envconfig:"GATEWAY_PASSWORD" desc:"password for the aicoopdb_gateway role; required for cloud / swarm profiles, optional for local trust-auth dev. AICOOPDB_GATEWAY_PASSWORD_FILE is also accepted."`
	// OwnerPassword is the password for the aicoopdb_owner role. The server
	// uses it to (a) embed in the migrations URL when running migrations
	// and (b) reconnect as the owner to ALTER ROLE the gateway password.
	// Same `*_FILE` variant applies.
	OwnerPassword        string        `envconfig:"OWNER_PASSWORD" desc:"password for the aicoopdb_owner role; same dual GATEWAY_PASSWORD vs *_FILE rules"`
	DatabaseMaxConns     int32         `envconfig:"DATABASE_MAX_CONNS" default:"20"`
	DatabaseMinConns     int32         `envconfig:"DATABASE_MIN_CONNS" default:"2"`
	DatabaseConnLifetime time.Duration `envconfig:"DATABASE_CONN_LIFETIME" default:"30m"`

	// SQL execution
	StatementTimeout   time.Duration `envconfig:"STATEMENT_TIMEOUT" default:"5s" desc:"per-request statement_timeout (max 60s)"`
	IdleInTxTimeout    time.Duration `envconfig:"IDLE_IN_TX_TIMEOUT" default:"5s"`
	MaxStatementBytes  int           `envconfig:"MAX_STATEMENT_BYTES" default:"65536"`
	MaxStatementParams int           `envconfig:"MAX_STATEMENT_PARAMS" default:"100"`
	DefaultSelectLimit int           `envconfig:"DEFAULT_SELECT_LIMIT" default:"1000"`
	HardSelectLimit    int           `envconfig:"HARD_SELECT_LIMIT" default:"10000"`

	// Auth
	AuthCacheSize    int           `envconfig:"AUTH_CACHE_SIZE" default:"1024"`
	AuthCacheTTL     time.Duration `envconfig:"AUTH_CACHE_TTL" default:"5m"`
	KeyRotateOverlap time.Duration `envconfig:"KEY_ROTATE_OVERLAP" default:"24h"`

	// Rate limiting
	RateLimitPerSecond float64 `envconfig:"RATE_LIMIT_PER_SECOND" default:"60"`
	RateLimitBurst     int     `envconfig:"RATE_LIMIT_BURST" default:"120"`

	// Audit
	AuditIncludeSQL bool `envconfig:"AUDIT_INCLUDE_SQL" default:"false" desc:"if true, store full SQL+params in audit_logs (compliance use)"`

	// Observability
	LogLevel       string `envconfig:"LOG_LEVEL" default:"info"`
	LogFormat      string `envconfig:"LOG_FORMAT" default:"json"`
	MetricsEnabled bool   `envconfig:"METRICS_ENABLED" default:"true"`
	OTELEndpoint   string `envconfig:"OTEL_EXPORTER_OTLP_ENDPOINT" desc:"optional OTLP collector"`

	// Run-once flags
	MigrateOnStart bool `envconfig:"MIGRATE_ON_START" default:"true" desc:"run pending migrations at startup before serving traffic"`
}

// Load reads AICOOPDB_* env vars into a fresh Config and validates simple
// invariants. Anything that depends on cross-field state is checked here so
// the server fails fast at startup rather than mid-request.
func Load() (*Config, error) {
	var c Config
	if err := envconfig.Process(EnvPrefix, &c); err != nil {
		return nil, fmt.Errorf("envconfig: %w", err)
	}
	if c.MigrationsDatabaseURL == "" {
		c.MigrationsDatabaseURL = c.DatabaseURL
	}
	// Resolve `*_FILE` env vars for sensitive secrets. Docker / swarm
	// mount secrets as files at /run/secrets/<name>; this is how operators
	// pass them in without putting them in env-block strings.
	if v, err := loadSecretFromFile("AICOOPDB_GATEWAY_PASSWORD", c.GatewayPassword); err != nil {
		return nil, err
	} else {
		c.GatewayPassword = v
	}
	if v, err := loadSecretFromFile("AICOOPDB_OWNER_PASSWORD", c.OwnerPassword); err != nil {
		return nil, err
	} else {
		c.OwnerPassword = v
	}
	if c.StatementTimeout > 60*time.Second {
		return nil, fmt.Errorf("statement_timeout must be <= 60s, got %s", c.StatementTimeout)
	}
	if c.HardSelectLimit < c.DefaultSelectLimit {
		return nil, fmt.Errorf("HARD_SELECT_LIMIT (%d) must be >= DEFAULT_SELECT_LIMIT (%d)", c.HardSelectLimit, c.DefaultSelectLimit)
	}
	if c.MaxStatementParams <= 0 {
		return nil, fmt.Errorf("MAX_STATEMENT_PARAMS must be > 0")
	}
	return &c, nil
}

// Usage returns the env var reference (used by `ai-coop-db-server -help-env`).
//
// envconfig.Usage writes to os.Stderr and only returns an error, so we use
// Usagef with an in-memory buffer to capture the rendered text.
func Usage() string {
	var buf bytes.Buffer
	if err := envconfig.Usagef(EnvPrefix, &Config{}, &buf, envconfig.DefaultListFormat); err != nil {
		return fmt.Sprintf("(failed to render usage: %v)", err)
	}
	return buf.String()
}

// loadSecretFromFile resolves the docker-style `<name>_FILE` env var.
// If `current` is non-empty (the operator passed the value directly via
// the env var), it wins. Otherwise, if `<name>_FILE` is set, the file is
// read and its trimmed contents returned. If neither is set, returns
// the empty string.
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

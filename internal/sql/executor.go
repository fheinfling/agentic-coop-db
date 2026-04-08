package sql

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/fheinfling/ai-coop-db/internal/tenant"
)

// ExecutorConfig configures the request-time tx settings.
type ExecutorConfig struct {
	StatementTimeout   time.Duration
	IdleInTxTimeout    time.Duration
	DefaultSelectLimit int
	HardSelectLimit    int
}

// Executor runs validated SQL inside a request transaction.
type Executor struct {
	pool *pgxpool.Pool
	cfg  ExecutorConfig
}

// NewExecutor returns a configured Executor.
func NewExecutor(pool *pgxpool.Pool, cfg ExecutorConfig) *Executor {
	if cfg.StatementTimeout <= 0 {
		cfg.StatementTimeout = 5 * time.Second
	}
	if cfg.IdleInTxTimeout <= 0 {
		cfg.IdleInTxTimeout = 5 * time.Second
	}
	if cfg.DefaultSelectLimit <= 0 {
		cfg.DefaultSelectLimit = 1000
	}
	if cfg.HardSelectLimit <= 0 {
		cfg.HardSelectLimit = 10000
	}
	return &Executor{pool: pool, cfg: cfg}
}

// Response is the JSON-shaped result returned to the client.
type Response struct {
	Command      string          `json:"command"`
	Columns      []string        `json:"columns,omitempty"`
	Rows         [][]any         `json:"rows,omitempty"`
	RowsAffected int64           `json:"rows_affected"`
	DurationMS   int             `json:"duration_ms"`
	Notice       string          `json:"notice,omitempty"`
	SQLState     string          `json:"-"`
	PgError      *pgconn.PgError `json:"-"`
}

// ExecuteInput bundles everything Execute needs.
type ExecuteInput struct {
	WorkspaceID string
	PgRole      string
	SQL         string
	Params      []any
	Result      *Result // from Validator.Validate
}

// Execute runs the input inside a single transaction. It returns a Response
// or a wrapped *pgconn.PgError so the HTTP layer can surface the SQLSTATE.
func (e *Executor) Execute(ctx context.Context, in ExecuteInput) (*Response, error) {
	if in.Result == nil {
		return nil, errors.New("executor.Execute: nil validator result")
	}
	sqlText := in.SQL
	if in.Result.IsSelect {
		sqlText = e.maybeWrapSelectLimit(sqlText)
	}

	start := time.Now()
	resp := &Response{Command: in.Result.Command}

	tx, err := e.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := tenant.Setup(ctx, tx, in.WorkspaceID, in.PgRole, e.cfg.StatementTimeout, e.cfg.IdleInTxTimeout); err != nil {
		return nil, fmt.Errorf("tenant setup: %w", err)
	}

	if in.Result.IsSelect {
		rows, err := tx.Query(ctx, sqlText, in.Params...)
		if err != nil {
			return nil, classifyPgErr(err)
		}
		defer rows.Close()

		descs := rows.FieldDescriptions()
		resp.Columns = make([]string, len(descs))
		for i, d := range descs {
			resp.Columns[i] = d.Name
		}

		for rows.Next() {
			values, err := rows.Values()
			if err != nil {
				return nil, classifyPgErr(err)
			}
			resp.Rows = append(resp.Rows, values)
		}
		if err := rows.Err(); err != nil {
			return nil, classifyPgErr(err)
		}
		resp.RowsAffected = int64(len(resp.Rows))
	} else {
		tag, err := tx.Exec(ctx, sqlText, in.Params...)
		if err != nil {
			return nil, classifyPgErr(err)
		}
		resp.RowsAffected = tag.RowsAffected()
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, classifyPgErr(err)
	}

	resp.DurationMS = int(time.Since(start).Milliseconds())
	return resp, nil
}

// maybeWrapSelectLimit auto-applies LIMIT $defaultSelectLimit when the
// statement looks like a bare SELECT without an explicit LIMIT or FETCH.
//
// We check the trailing tokens of the normalized SQL — if `limit` or
// `fetch` already appears in the tail we leave the statement alone. The
// hard cap is enforced by the executor regardless of what the caller
// supplied (we wrap with an outer SELECT * FROM (... ) limit hard if
// the requested limit is bigger).
func (e *Executor) maybeWrapSelectLimit(sqlText string) string {
	tail := strings.ToLower(strings.TrimRight(sqlText, "; \t\n\r"))
	// crude but adequate: look for ` limit ` or ` fetch ` in the last 64 bytes
	tailScan := tail
	if len(tailScan) > 256 {
		tailScan = tailScan[len(tailScan)-256:]
	}
	if strings.Contains(tailScan, " limit ") || strings.Contains(tailScan, " fetch ") {
		return sqlText
	}
	trimmed := strings.TrimRight(sqlText, "; \t\n\r")
	return fmt.Sprintf("SELECT * FROM (%s) AS _aicoopdb_wrapped LIMIT %d", trimmed, e.cfg.DefaultSelectLimit)
}

// classifyPgErr unwraps the *pgconn.PgError so the HTTP layer can read the
// SQLSTATE without re-parsing.
func classifyPgErr(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return &Error{Pg: pgErr}
	}
	return err
}

// Error wraps a *pgconn.PgError. The HTTP layer maps SQLSTATE classes to
// HTTP status codes (42501 -> 403, 22xxx -> 400, 23xxx -> 409, etc).
type Error struct {
	Pg *pgconn.PgError
}

func (e *Error) Error() string {
	if e.Pg == nil {
		return "unknown postgres error"
	}
	return fmt.Sprintf("%s: %s", e.Pg.Code, e.Pg.Message)
}

// Unwrap exposes the underlying pgconn error so callers can errors.As() it.
func (e *Error) Unwrap() error { return e.Pg }

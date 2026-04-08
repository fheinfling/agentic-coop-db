package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/fheinfling/aicoldb/internal/tenant"
)

// Dispatcher invokes a registered Procedure. It is the only entry point
// the HTTP layer talks to.
type Dispatcher struct {
	pool     *pgxpool.Pool
	registry *Registry
	idem     *IdempotencyStore
	logger   *slog.Logger
}

// NewDispatcher constructs a Dispatcher.
func NewDispatcher(pool *pgxpool.Pool, registry *Registry, logger *slog.Logger) *Dispatcher {
	if logger == nil {
		logger = slog.Default()
	}
	return &Dispatcher{
		pool:     pool,
		registry: registry,
		idem:     NewIdempotencyStore(pool),
		logger:   logger,
	}
}

// CallInput is what the HTTP handler hands the dispatcher.
type CallInput struct {
	WorkspaceID    string
	PgRole         string                 // the calling key's role
	Name           string                 // procedure name
	Args           map[string]any         // raw decoded JSON args
	IdempotencyKey string                 // optional Idempotency-Key header value
	StatementTimeout time.Duration
	IdleInTxTimeout  time.Duration
}

// CallResult is the dispatcher's response.
type CallResult struct {
	Procedure  string          `json:"procedure"`
	Result     json.RawMessage `json:"result"`
	DurationMS int             `json:"duration_ms"`

	// idempotency: if Replay is true, the HTTP handler should serve
	// CachedStatus + CachedBody verbatim.
	Replay       bool
	CachedStatus int
	CachedBody   []byte
}

// ErrUnknownProcedure is returned when name is not in the registry.
var ErrUnknownProcedure = errors.New("unknown procedure")

// ErrRoleNotPermitted is returned when the calling key cannot run this RPC
// because the procedure declares a required role and the key's role is
// not the same. Postgres still has the final say (via SET LOCAL ROLE),
// this is just an early reject.
var ErrRoleNotPermitted = errors.New("calling key cannot run this RPC")

// Call dispatches an RPC end-to-end.
func (d *Dispatcher) Call(ctx context.Context, in CallInput) (*CallResult, error) {
	proc, ok := d.registry.Get(in.Name)
	if !ok {
		return nil, ErrUnknownProcedure
	}
	if proc.RequiredRole != "" && proc.RequiredRole != in.PgRole {
		// Soft early reject — keeps the audit log clean. Postgres would
		// also block it via the role grants, but the error message would
		// reference the underlying role rather than the RPC.
		return nil, fmt.Errorf("%w: rpc %q requires role %q, key has %q", ErrRoleNotPermitted, in.Name, proc.RequiredRole, in.PgRole)
	}

	// Validate args against the procedure's JSON schema.
	if err := proc.Schema.Validate(in.Args); err != nil {
		return nil, fmt.Errorf("rpc args validation: %w", err)
	}

	// Idempotency layer (optional).
	requestHash := HashRequest("RPC", in.Name, "", []any{in.Args})
	if in.IdempotencyKey != "" {
		res, err := d.idem.BeginOrReplay(ctx, in.WorkspaceID, in.IdempotencyKey, requestHash, 24*time.Hour)
		if err != nil {
			return nil, err
		}
		if res.Replay {
			return &CallResult{Replay: true, CachedStatus: res.StatusCode, CachedBody: res.Body}, nil
		}
	}

	start := time.Now()
	result, err := d.run(ctx, proc, in)
	durationMS := int(time.Since(start).Milliseconds())

	if in.IdempotencyKey != "" {
		body, _ := json.Marshal(map[string]any{
			"procedure":   proc.Name,
			"result":      result,
			"duration_ms": durationMS,
		})
		status := 200
		ok := err == nil
		if !ok {
			status = 500
		}
		if cerr := d.idem.Complete(ctx, in.WorkspaceID, in.IdempotencyKey, status, body, ok); cerr != nil {
			d.logger.Warn("idempotency complete failed", "err", cerr)
		}
	}

	if err != nil {
		return nil, err
	}
	return &CallResult{
		Procedure:  proc.Name,
		Result:     result,
		DurationMS: durationMS,
	}, nil
}

// run executes the procedure body inside a transaction with SET LOCAL.
func (d *Dispatcher) run(ctx context.Context, proc *Procedure, in CallInput) (json.RawMessage, error) {
	tx, err := d.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := tenant.Setup(ctx, tx, in.WorkspaceID, in.PgRole, in.StatementTimeout, in.IdleInTxTimeout); err != nil {
		return nil, err
	}

	argsJSON, err := json.Marshal(in.Args)
	if err != nil {
		return nil, err
	}

	// The procedure body is expected to be a single statement that returns
	// json (e.g. SELECT json_build_object(...)). It receives one parameter:
	// the args object.
	var raw []byte
	if err := tx.QueryRow(ctx, proc.Body, string(argsJSON)).Scan(&raw); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return raw, nil
}

package rpc

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// IdempotencyState is the row state in idempotency_keys.
type IdempotencyState string

const (
	StatePending IdempotencyState = "pending"
	StateDone    IdempotencyState = "done"
	StateFailed  IdempotencyState = "failed"
)

// IdempotencyResult is what BeginOrReplay returns when the row is already
// in a terminal state. The Replay flag tells the caller to skip execution
// and serve StatusCode/Body verbatim.
type IdempotencyResult struct {
	Replay     bool
	StatusCode int
	Body       []byte
}

// IdempotencyConflict is returned when a key was already used for a
// different request hash (the same key with different content).
var IdempotencyConflict = errors.New("idempotency_key reused with different request body")

// IdempotencyStore is the persistence layer behind the dispatcher.
type IdempotencyStore struct {
	pool *pgxpool.Pool
}

// NewIdempotencyStore returns a Store backed by pool.
func NewIdempotencyStore(pool *pgxpool.Pool) *IdempotencyStore {
	return &IdempotencyStore{pool: pool}
}

// HashRequest produces a stable hash of the request payload. Used to detect
// conflict (same key, different body).
func HashRequest(method, path, sql string, params []any) string {
	h := sha256.New()
	_, _ = io.WriteString(h, method)
	_, _ = io.WriteString(h, "\x00")
	_, _ = io.WriteString(h, path)
	_, _ = io.WriteString(h, "\x00")
	_, _ = io.WriteString(h, sql)
	_, _ = io.WriteString(h, "\x00")
	if params != nil {
		_ = json.NewEncoder(h).Encode(params)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// BeginOrReplay either inserts a new pending row or returns a cached
// terminal-state response. If a different request hash is found for the
// same (workspace, key), it returns IdempotencyConflict.
func (s *IdempotencyStore) BeginOrReplay(ctx context.Context, workspaceID, key, requestHash string, ttl time.Duration) (*IdempotencyResult, error) {
	if key == "" {
		return nil, errors.New("BeginOrReplay: empty key")
	}
	wsID, err := uuid.Parse(workspaceID)
	if err != nil {
		return nil, fmt.Errorf("workspace_id: %w", err)
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Try to insert a fresh pending row. If a row already exists, we read
	// it and decide whether to replay or conflict.
	id := uuid.New()
	tag, err := tx.Exec(ctx, `
INSERT INTO idempotency_keys (id, workspace_id, key, request_hash, state, created_at, expires_at)
VALUES ($1, $2, $3, $4, 'pending', now(), now() + $5::interval)
ON CONFLICT (workspace_id, key) DO NOTHING`,
		id, wsID, key, requestHash, ttl.String(),
	)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 1 {
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return &IdempotencyResult{Replay: false}, nil
	}

	var (
		state    IdempotencyState
		hashGot  string
		statusCD *int
		body     []byte
		expires  time.Time
	)
	if err := tx.QueryRow(ctx, `
SELECT state, request_hash, status_code, response_body, expires_at
FROM idempotency_keys
WHERE workspace_id = $1 AND key = $2`,
		wsID, key,
	).Scan(&state, &hashGot, &statusCD, &body, &expires); err != nil {
		return nil, err
	}

	if expires.Before(time.Now()) {
		// Stale row — reclaim it.
		if _, err := tx.Exec(ctx,
			`DELETE FROM idempotency_keys WHERE workspace_id = $1 AND key = $2`,
			wsID, key,
		); err != nil {
			return nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return s.BeginOrReplay(ctx, workspaceID, key, requestHash, ttl)
	}

	if hashGot != requestHash {
		_ = tx.Commit(ctx)
		return nil, IdempotencyConflict
	}
	if state == StatePending {
		// In flight — caller should retry shortly. We surface this as a
		// conflict-shaped error so the HTTP layer can return 409.
		_ = tx.Commit(ctx)
		return nil, errors.New("idempotency_key request still in progress")
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	decoded, err := gunzip(body)
	if err != nil {
		return nil, err
	}
	status := 200
	if statusCD != nil {
		status = *statusCD
	}
	return &IdempotencyResult{Replay: true, StatusCode: status, Body: decoded}, nil
}

// Complete marks a row as done/failed and stores the gzipped response body.
func (s *IdempotencyStore) Complete(ctx context.Context, workspaceID, key string, status int, body []byte, ok bool) error {
	wsID, err := uuid.Parse(workspaceID)
	if err != nil {
		return err
	}
	state := StateDone
	if !ok {
		state = StateFailed
	}
	gz, err := gzipBytes(body)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
UPDATE idempotency_keys
SET state = $1, status_code = $2, response_body = $3, completed_at = now()
WHERE workspace_id = $4 AND key = $5`,
		string(state), status, gz, wsID, key,
	)
	return err
}

func gzipBytes(p []byte) ([]byte, error) {
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write(p); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func gunzip(p []byte) ([]byte, error) {
	if len(p) == 0 {
		return nil, nil
	}
	r, err := gzip.NewReader(bytes.NewReader(p))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}

// Package audit writes one structured row per authenticated request to the
// audit_logs table. The full SQL/params live in the slog stream by default;
// set AICOLDB_AUDIT_INCLUDE_SQL=true to also persist them on the row.
package audit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Entry is a single audit record. The fields map 1:1 to audit_logs columns.
type Entry struct {
	RequestID   string
	WorkspaceID string
	KeyDBID     string
	Endpoint    string
	Command     string
	SQL         string
	Params      []any
	DurationMS  int
	StatusCode  int
	ErrorCode   string
	SQLState    string
	ClientIP    string
}

// Writer writes audit rows. Failures are logged but never propagated to the
// caller — auditing is best-effort.
type Writer struct {
	pool       *pgxpool.Pool
	logger     *slog.Logger
	includeSQL bool
}

// NewWriter constructs a Writer.
func NewWriter(pool *pgxpool.Pool, logger *slog.Logger, includeSQL bool) *Writer {
	if logger == nil {
		logger = slog.Default()
	}
	return &Writer{pool: pool, logger: logger, includeSQL: includeSQL}
}

// Write inserts the entry. Always logs the structured form to slog as well.
func (w *Writer) Write(ctx context.Context, e Entry) {
	w.logger.LogAttrs(ctx, slog.LevelInfo, "request",
		slog.String("request_id", e.RequestID),
		slog.String("workspace_id", e.WorkspaceID),
		slog.String("key_db_id", e.KeyDBID),
		slog.String("endpoint", e.Endpoint),
		slog.String("command", e.Command),
		slog.Int("duration_ms", e.DurationMS),
		slog.Int("status_code", e.StatusCode),
		slog.String("error_code", e.ErrorCode),
		slog.String("sqlstate", e.SQLState),
		slog.String("client_ip", e.ClientIP),
	)
	if w.pool == nil {
		return
	}

	id := uuid.New()
	var (
		wsID    *uuid.UUID
		keyID   *uuid.UUID
		clipIP  *net.IP
		sqlText *string
		params  []byte
	)
	if e.WorkspaceID != "" {
		if u, err := uuid.Parse(e.WorkspaceID); err == nil {
			wsID = &u
		}
	}
	if e.KeyDBID != "" {
		if u, err := uuid.Parse(e.KeyDBID); err == nil {
			keyID = &u
		}
	}
	if e.ClientIP != "" {
		if ip := net.ParseIP(e.ClientIP); ip != nil {
			clipIP = &ip
		}
	}
	if w.includeSQL {
		s := e.SQL
		sqlText = &s
		if e.Params != nil {
			b, _ := json.Marshal(e.Params)
			params = b
		}
	}

	const q = `
INSERT INTO audit_logs (
    id, request_id, workspace_id, key_id, endpoint, command,
    sql_hash, params_hash, sql_text, params_json,
    duration_ms, status_code, error_code, sqlstate, client_ip
) VALUES (
    $1, $2, $3, $4, $5, $6,
    $7, $8, $9, $10,
    $11, $12, $13, $14, $15
)`
	insertCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if _, err := w.pool.Exec(insertCtx, q,
		id, e.RequestID, wsID, keyID, e.Endpoint, e.Command,
		hashOrEmpty(e.SQL), paramsHash(e.Params), sqlText, params,
		e.DurationMS, e.StatusCode, e.ErrorCode, e.SQLState, clipIP,
	); err != nil {
		w.logger.Warn("audit write failed", "err", err, "request_id", e.RequestID)
	}
}

func hashOrEmpty(s string) string {
	if s == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func paramsHash(params []any) string {
	if len(params) == 0 {
		return ""
	}
	b, err := json.Marshal(params)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

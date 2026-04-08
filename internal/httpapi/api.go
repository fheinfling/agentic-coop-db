package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/fheinfling/aicoldb/internal/audit"
	"github.com/fheinfling/aicoldb/internal/auth"
	"github.com/fheinfling/aicoldb/internal/config"
	"github.com/fheinfling/aicoldb/internal/observability"
	"github.com/fheinfling/aicoldb/internal/rpc"
	sqlpkg "github.com/fheinfling/aicoldb/internal/sql"
	"github.com/fheinfling/aicoldb/internal/version"
)

// Deps is the wiring bag passed to New. Every dependency is required.
type Deps struct {
	Config         *config.Config
	Logger         *slog.Logger
	Metrics        *observability.Metrics
	Pool           *pgxpool.Pool
	AuthMiddleware *auth.Middleware
	AuthStore      *auth.Store
	Auditor        *audit.Writer
	Validator      *sqlpkg.Validator
	Executor       *sqlpkg.Executor
	RPCDispatcher  *rpc.Dispatcher
}

// API holds the wiring needed by the route handlers.
type API struct {
	deps      Deps
	rateLimit *RateLimit
}

// New constructs an API.
func New(deps Deps) *API {
	return &API{
		deps:      deps,
		rateLimit: NewRateLimit(deps.Config.RateLimitPerSecond, deps.Config.RateLimitBurst),
	}
}

// Routes returns the http.Handler for the /v1 subtree. The caller is
// responsible for mounting top-level routes (/healthz, /readyz, /metrics).
func (a *API) Routes() http.Handler {
	r := chi.NewRouter()

	r.Group(func(g chi.Router) {
		g.Use(a.deps.AuthMiddleware.Authenticate)
		g.Use(a.rateLimit.Middleware)

		g.Post("/sql/execute", a.handleSQLExecute)
		g.Post("/rpc/call", a.handleRPCCall)
		g.Post("/auth/keys/rotate", a.handleKeyRotate)
		g.Post("/auth/keys", a.handleKeyCreate)
		g.Get("/me", a.handleMe)
	})

	return r
}

// ---- DTOs --------------------------------------------------------------------

type sqlExecuteRequest struct {
	SQL    string `json:"sql"`
	Params []any  `json:"params"`
}

type rpcCallRequest struct {
	Procedure string         `json:"procedure"`
	Args      map[string]any `json:"args"`
}

type rotateKeyResponse struct {
	NewKeyID string `json:"new_key_id"`
	Token    string `json:"token"`
	Notice   string `json:"notice"`
}

type createKeyRequest struct {
	WorkspaceID string `json:"workspace_id"`
	Env         string `json:"env"`
	PgRole      string `json:"pg_role"`
	Name        string `json:"name"`
}

type meResponse struct {
	WorkspaceID string       `json:"workspace_id"`
	KeyID       string       `json:"key_id"`
	Role        string       `json:"role"`
	Env         string       `json:"env"`
	Server      version.Info `json:"server"`
}

// ---- handlers ----------------------------------------------------------------

func (a *API) handleSQLExecute(w http.ResponseWriter, r *http.Request) {
	ws := auth.MustFromContext(r.Context())
	start := time.Now()

	var req sqlExecuteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteProblem(w, Problem{Title: "invalid_json", Status: http.StatusBadRequest, Detail: err.Error()})
		return
	}
	res, err := a.deps.Validator.Validate(req.SQL, req.Params)
	if err != nil {
		WriteProblem(w, MapError(err))
		a.audit(r, ws, "POST /v1/sql/execute", "", req.SQL, req.Params, start, http.StatusBadRequest, err)
		return
	}
	resp, err := a.deps.Executor.Execute(r.Context(), sqlpkg.ExecuteInput{
		WorkspaceID: ws.WorkspaceID,
		PgRole:      ws.PgRole,
		SQL:         req.SQL,
		Params:      req.Params,
		Result:      res,
	})
	if err != nil {
		problem := MapError(err)
		WriteProblem(w, problem)
		a.audit(r, ws, "POST /v1/sql/execute", res.Command, req.SQL, req.Params, start, problem.Status, err)
		a.deps.Metrics.SQLStatements.WithLabelValues(res.Command, problem.SQLState).Inc()
		return
	}
	WriteJSON(w, http.StatusOK, resp)
	a.audit(r, ws, "POST /v1/sql/execute", res.Command, req.SQL, req.Params, start, http.StatusOK, nil)
	a.deps.Metrics.SQLStatements.WithLabelValues(res.Command, "00000").Inc()
}

func (a *API) handleRPCCall(w http.ResponseWriter, r *http.Request) {
	ws := auth.MustFromContext(r.Context())
	start := time.Now()

	var req rpcCallRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteProblem(w, Problem{Title: "invalid_json", Status: http.StatusBadRequest, Detail: err.Error()})
		return
	}
	res, err := a.deps.RPCDispatcher.Call(r.Context(), rpc.CallInput{
		WorkspaceID:      ws.WorkspaceID,
		PgRole:           ws.PgRole,
		Name:             req.Procedure,
		Args:             req.Args,
		IdempotencyKey:   r.Header.Get("Idempotency-Key"),
		StatementTimeout: a.deps.Config.StatementTimeout,
		IdleInTxTimeout:  a.deps.Config.IdleInTxTimeout,
	})
	if err != nil {
		switch {
		case errors.Is(err, rpc.ErrUnknownProcedure):
			WriteProblem(w, Problem{Title: "unknown_procedure", Status: http.StatusNotFound, Detail: err.Error()})
		case errors.Is(err, rpc.ErrRoleNotPermitted):
			WriteProblem(w, Problem{Title: "permission_denied", Status: http.StatusForbidden, Detail: err.Error()})
		case errors.Is(err, rpc.IdempotencyConflict):
			WriteProblem(w, Problem{Title: "idempotency_conflict", Status: http.StatusConflict, Detail: err.Error()})
		default:
			WriteProblem(w, MapError(err))
		}
		a.audit(r, ws, "POST /v1/rpc/call", "RPC", req.Procedure, []any{req.Args}, start, http.StatusInternalServerError, err)
		a.deps.Metrics.RPCInvocations.WithLabelValues(req.Procedure, "error").Inc()
		return
	}
	if res.Replay {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Idempotent-Replayed", "true")
		w.WriteHeader(res.CachedStatus)
		_, _ = w.Write(res.CachedBody)
		a.deps.Metrics.IdempotencyHits.Inc()
		a.deps.Metrics.RPCInvocations.WithLabelValues(req.Procedure, "ok").Inc()
		return
	}
	a.deps.Metrics.IdempotencyMisses.Inc()
	a.deps.Metrics.RPCInvocations.WithLabelValues(req.Procedure, "ok").Inc()
	WriteJSON(w, http.StatusOK, res)
	a.audit(r, ws, "POST /v1/rpc/call", "RPC", req.Procedure, []any{req.Args}, start, http.StatusOK, nil)
}

func (a *API) handleKeyRotate(w http.ResponseWriter, r *http.Request) {
	ws := auth.MustFromContext(r.Context())
	created, err := a.deps.AuthStore.Rotate(r.Context(), ws.KeyDBID, a.deps.Config.KeyRotateOverlap)
	if err != nil {
		WriteProblem(w, MapError(err))
		return
	}
	WriteJSON(w, http.StatusOK, rotateKeyResponse{
		NewKeyID: created.KeyID,
		Token:    created.FullToken,
		Notice:   "old key remains active for the configured overlap window",
	})
}

func (a *API) handleKeyCreate(w http.ResponseWriter, r *http.Request) {
	ws := auth.MustFromContext(r.Context())
	if ws.PgRole != "dbadmin" {
		WriteProblem(w, Problem{
			Title:  "permission_denied",
			Status: http.StatusForbidden,
			Detail: "only keys with role=dbadmin may create new keys",
		})
		return
	}
	var req createKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteProblem(w, Problem{Title: "invalid_json", Status: http.StatusBadRequest, Detail: err.Error()})
		return
	}
	if req.WorkspaceID == "" {
		req.WorkspaceID = ws.WorkspaceID
	}
	if req.Env == "" {
		req.Env = string(ws.Env)
	}
	created, err := a.deps.AuthStore.Create(r.Context(), auth.CreateKeyInput{
		WorkspaceID: req.WorkspaceID,
		Env:         auth.KeyEnvironment(req.Env),
		PgRole:      req.PgRole,
		Name:        req.Name,
	})
	if err != nil {
		WriteProblem(w, MapError(err))
		return
	}
	WriteJSON(w, http.StatusCreated, rotateKeyResponse{
		NewKeyID: created.KeyID,
		Token:    created.FullToken,
		Notice:   "this token is shown exactly once — store it now",
	})
}

func (a *API) handleMe(w http.ResponseWriter, r *http.Request) {
	ws := auth.MustFromContext(r.Context())
	WriteJSON(w, http.StatusOK, meResponse{
		WorkspaceID: ws.WorkspaceID,
		KeyID:       ws.KeyID,
		Role:        ws.PgRole,
		Env:         string(ws.Env),
		Server:      version.Get(),
	})
}

// ---- audit helper ------------------------------------------------------------

func (a *API) audit(r *http.Request, ws *auth.WorkspaceContext, endpoint, command, sql string, params []any, start time.Time, status int, err error) {
	if a.deps.Auditor == nil {
		return
	}
	var (
		errCode  string
		sqlState string
	)
	if err != nil {
		problem := MapError(err)
		errCode = problem.Title
		sqlState = problem.SQLState
	}
	a.deps.Auditor.Write(r.Context(), audit.Entry{
		RequestID:   chimw.GetReqID(r.Context()),
		WorkspaceID: ws.WorkspaceID,
		KeyDBID:     ws.KeyDBID,
		Endpoint:    endpoint,
		Command:     command,
		SQL:         sql,
		Params:      params,
		DurationMS:  int(time.Since(start).Milliseconds()),
		StatusCode:  status,
		ErrorCode:   errCode,
		SQLState:    sqlState,
		ClientIP:    clientIP(r),
	})
}

func clientIP(r *http.Request) string {
	if v := r.Header.Get("X-Forwarded-For"); v != "" {
		// take the leftmost
		if i := strings.IndexByte(v, ','); i >= 0 {
			return strings.TrimSpace(v[:i])
		}
		return strings.TrimSpace(v)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

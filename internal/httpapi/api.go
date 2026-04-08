package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/fheinfling/ai-coop-db/internal/audit"
	"github.com/fheinfling/ai-coop-db/internal/auth"
	"github.com/fheinfling/ai-coop-db/internal/config"
	"github.com/fheinfling/ai-coop-db/internal/observability"
	"github.com/fheinfling/ai-coop-db/internal/rpc"
	sqlpkg "github.com/fheinfling/ai-coop-db/internal/sql"
	"github.com/fheinfling/ai-coop-db/internal/version"
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
	idem      *rpc.IdempotencyStore
}

// New constructs an API.
func New(deps Deps) *API {
	return &API{
		deps:      deps,
		rateLimit: NewRateLimit(deps.Config.RateLimitPerSecond, deps.Config.RateLimitBurst),
		// Both /v1/sql/execute and /v1/rpc/call go through the same
		// idempotency table; we instantiate one store at the API level so
		// the SQL handler does not have to reach into the dispatcher.
		idem: rpc.NewIdempotencyStore(deps.Pool),
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

	// Read the body once. The MaxBodyBytes middleware caps it. We hash the
	// raw bytes for idempotency BEFORE JSON-parsing so the hash is
	// deterministic across re-encodings of nested JSON.
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		WriteProblem(w, Problem{Title: "body_too_large", Status: http.StatusRequestEntityTooLarge, Detail: err.Error()})
		return
	}

	var req sqlExecuteRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		WriteProblem(w, Problem{Title: "invalid_json", Status: http.StatusBadRequest, Detail: err.Error()})
		return
	}
	res, err := a.deps.Validator.Validate(req.SQL, req.Params)
	if err != nil {
		WriteProblem(w, MapError(err))
		a.audit(r, ws, "POST /v1/sql/execute", "", req.SQL, req.Params, start, http.StatusBadRequest, err)
		return
	}

	// Optional idempotency layer. The same state machine the RPC dispatcher
	// uses, sharing the same idempotency_keys table.
	idemKey := r.Header.Get("Idempotency-Key")
	if idemKey != "" {
		hash := rpc.HashRequest(r.Method, r.URL.Path, bodyBytes)
		ir, ierr := a.idem.BeginOrReplay(r.Context(), ws.WorkspaceID, idemKey, hash, 24*time.Hour)
		if ierr != nil {
			if errors.Is(ierr, rpc.ErrIdempotencyConflict) {
				WriteProblem(w, Problem{Title: "idempotency_conflict", Status: http.StatusConflict, Detail: ierr.Error()})
				return
			}
			WriteProblem(w, Problem{Title: "idempotency_internal", Status: http.StatusInternalServerError, Detail: ierr.Error()})
			return
		}
		if ir.Replay {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Idempotent-Replayed", "true")
			w.WriteHeader(ir.StatusCode)
			_, _ = w.Write(ir.Body)
			a.deps.Metrics.IdempotencyHits.Inc()
			return
		}
		a.deps.Metrics.IdempotencyMisses.Inc()
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
		// Persist the failure as `failed` so a replay returns the same error
		// rather than re-running the statement.
		if idemKey != "" {
			body, _ := json.Marshal(problem)
			_ = a.idem.Complete(r.Context(), ws.WorkspaceID, idemKey, problem.Status, body, false)
		}
		return
	}
	body, _ := json.Marshal(resp)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
	if idemKey != "" {
		_ = a.idem.Complete(r.Context(), ws.WorkspaceID, idemKey, http.StatusOK, body, true)
	}
	a.audit(r, ws, "POST /v1/sql/execute", res.Command, req.SQL, req.Params, start, http.StatusOK, nil)
	a.deps.Metrics.SQLStatements.WithLabelValues(res.Command, "00000").Inc()
}

func (a *API) handleRPCCall(w http.ResponseWriter, r *http.Request) {
	ws := auth.MustFromContext(r.Context())
	start := time.Now()

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		WriteProblem(w, Problem{Title: "body_too_large", Status: http.StatusRequestEntityTooLarge, Detail: err.Error()})
		return
	}
	var req rpcCallRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		WriteProblem(w, Problem{Title: "invalid_json", Status: http.StatusBadRequest, Detail: err.Error()})
		return
	}
	res, err := a.deps.RPCDispatcher.Call(r.Context(), rpc.CallInput{
		WorkspaceID:      ws.WorkspaceID,
		PgRole:           ws.PgRole,
		Name:             req.Procedure,
		Args:             req.Args,
		IdempotencyKey:   r.Header.Get("Idempotency-Key"),
		RequestHash:      rpc.HashRequest(r.Method, r.URL.Path, bodyBytes),
		StatementTimeout: a.deps.Config.StatementTimeout,
		IdleInTxTimeout:  a.deps.Config.IdleInTxTimeout,
	})
	if err != nil {
		switch {
		case errors.Is(err, rpc.ErrUnknownProcedure):
			WriteProblem(w, Problem{Title: "unknown_procedure", Status: http.StatusNotFound, Detail: err.Error()})
		case errors.Is(err, rpc.ErrRoleNotPermitted):
			WriteProblem(w, Problem{Title: "permission_denied", Status: http.StatusForbidden, Detail: err.Error()})
		case errors.Is(err, rpc.ErrIdempotencyConflict):
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
	// Evict the OLD key from the in-memory verify cache so a stolen
	// token cannot continue to authenticate until the cache TTL expires.
	// The new key is not yet in the cache; the next request that uses it
	// will populate it via the normal argon2id-verify path.
	a.deps.AuthMiddleware.RevokeFromCache(ws.KeyDBID)

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
	// dbadmin keys are scoped to ONE workspace. They may not mint keys for
	// any other workspace, even by passing a different workspace_id in the
	// request body. Cross-workspace minting would defeat the entire
	// multi-tenant story: a compromised dbadmin key for tenant A would
	// instantly become admin of every other tenant in the database.
	if req.WorkspaceID != "" && req.WorkspaceID != ws.WorkspaceID {
		WriteProblem(w, Problem{
			Title:  "permission_denied",
			Status: http.StatusForbidden,
			Detail: "dbadmin keys can only mint keys for their own workspace",
		})
		return
	}
	req.WorkspaceID = ws.WorkspaceID
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

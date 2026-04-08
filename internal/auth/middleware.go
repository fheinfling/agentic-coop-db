package auth

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"
)

// WorkspaceContext is the auth result attached to every authenticated
// request. Downstream handlers read it from context via FromContext.
type WorkspaceContext struct {
	WorkspaceID string
	KeyID       string
	KeyDBID     string // primary key of the api_keys row
	PgRole      string
	Env         KeyEnvironment
}

type ctxKey int

const wsCtxKey ctxKey = 0

// FromContext returns the WorkspaceContext attached by the middleware, or
// nil if the request was not authenticated.
func FromContext(ctx context.Context) *WorkspaceContext {
	v, _ := ctx.Value(wsCtxKey).(*WorkspaceContext)
	return v
}

// MustFromContext panics if the request was not authenticated. Handlers
// mounted under the auth middleware can use this safely; never call it
// from public routes like /healthz.
func MustFromContext(ctx context.Context) *WorkspaceContext {
	v := FromContext(ctx)
	if v == nil {
		panic("auth: WorkspaceContext missing from context — was the middleware applied?")
	}
	return v
}

// Middleware is the http.Handler middleware that converts an
// `Authorization: Bearer aic_...` header into a WorkspaceContext.
type Middleware struct {
	store  *Store
	cache  *VerifyCache
	logger *slog.Logger
}

// NewMiddleware constructs a Middleware. None of the args may be nil.
func NewMiddleware(store *Store, cache *VerifyCache, logger *slog.Logger) *Middleware {
	if store == nil || cache == nil {
		panic("auth.NewMiddleware: store and cache are required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Middleware{store: store, cache: cache, logger: logger}
}

// Authenticate is the chi/middleware-compatible function. Failed auth
// returns 401 with a tiny RFC7807-shaped JSON body.
func (m *Middleware) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("Authorization")
		parsed, err := ParseBearer(token)
		if err != nil {
			writeAuthError(w, http.StatusUnauthorized, "missing_or_invalid_api_key", "Authorization header must be 'Bearer aic_<env>_<id>_<secret>'")
			return
		}
		rec, err := m.resolve(r.Context(), parsed)
		if err != nil {
			switch {
			case errors.Is(err, ErrKeyNotFound), errors.Is(err, ErrInvalidKey):
				writeAuthError(w, http.StatusUnauthorized, "invalid_api_key", "the supplied API key is not valid")
			default:
				m.logger.Error("auth resolve failed", "err", err)
				writeAuthError(w, http.StatusInternalServerError, "auth_internal", "internal authentication error")
			}
			return
		}
		if !rec.Active(time.Now()) {
			writeAuthError(w, http.StatusUnauthorized, "key_inactive", "this key is revoked or expired")
			return
		}
		// Best-effort touch (non-blocking, non-fatal).
		go func(id string) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if err := m.store.TouchLastUsed(ctx, id); err != nil {
				m.logger.Warn("touch last_used_at failed", "err", err)
			}
		}(rec.ID)

		ws := &WorkspaceContext{
			WorkspaceID: rec.WorkspaceID,
			KeyID:       rec.KeyID,
			KeyDBID:     rec.ID,
			PgRole:      rec.PgRole,
			Env:         rec.Env,
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), wsCtxKey, ws)))
	})
}

// resolve checks the cache and falls back to the database + argon2id verify.
func (m *Middleware) resolve(ctx context.Context, p *ParsedKey) (*KeyRecord, error) {
	cacheKey := p.CacheKey()
	if rec, ok := m.cache.Get(cacheKey); ok {
		return rec, nil
	}
	rec, err := m.store.FindByKeyID(ctx, p.KeyID)
	if err != nil {
		return nil, err
	}
	if err := VerifySecret(p.Secret, rec.SecretHash); err != nil {
		return nil, err
	}
	m.cache.Put(cacheKey, rec)
	return rec, nil
}

// writeAuthError writes a small RFC7807-shaped JSON error to w.
func writeAuthError(w http.ResponseWriter, status int, code, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.Header().Set("WWW-Authenticate", `Bearer realm="aicoldb"`)
	w.WriteHeader(status)
	_, _ = w.Write([]byte(
		`{"type":"about:blank","title":"` + code + `","status":` + itoa(status) + `,"detail":"` + detail + `"}`,
	))
}

// itoa avoids pulling strconv just for this small helper.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

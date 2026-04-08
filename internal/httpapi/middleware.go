package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"golang.org/x/time/rate"

	"github.com/fheinfling/aicoldb/internal/auth"
	"github.com/fheinfling/aicoldb/internal/observability"
)

// chiRouteContext returns the bound chi route pattern, or "" if none.
func chiRouteContext(r *http.Request) string {
	if rc := chi.RouteContext(r.Context()); rc != nil {
		return rc.RoutePattern()
	}
	return ""
}

// MaxBodyBytes returns a middleware that caps the request body to n bytes.
// Requests bigger than n short-circuit with HTTP 413 before any handler runs.
func MaxBodyBytes(n int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, n)
			next.ServeHTTP(w, r)
		})
	}
}

// MetricsMiddleware records prometheus stats for every request.
func MetricsMiddleware(m *observability.Metrics) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			duration := time.Since(start).Seconds()
			route := chiRoute(r)
			status := strconv.Itoa(ww.Status())
			m.RequestDuration.WithLabelValues(route, r.Method, status).Observe(duration)
			m.RequestsTotal.WithLabelValues(route, r.Method, status).Inc()
		})
	}
}

// AccessLog logs one structured line per request at INFO level.
func AccessLog(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			logger.LogAttrs(r.Context(), slog.LevelInfo, "http",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", ww.Status()),
				slog.Int("bytes_written", ww.BytesWritten()),
				slog.Duration("duration", time.Since(start)),
				slog.String("remote", r.RemoteAddr),
				slog.String("request_id", middleware.GetReqID(r.Context())),
			)
		})
	}
}

// RateLimit applies a per-key token bucket. Unauthenticated requests share
// a single bucket so they cannot starve authenticated traffic.
type RateLimit struct {
	mu       sync.Mutex
	limiters map[string]*rate.Limiter
	rps      rate.Limit
	burst    int
}

// NewRateLimit constructs a per-key rate limiter.
func NewRateLimit(rps float64, burst int) *RateLimit {
	if rps <= 0 {
		rps = 60
	}
	if burst <= 0 {
		burst = 120
	}
	return &RateLimit{
		limiters: make(map[string]*rate.Limiter),
		rps:      rate.Limit(rps),
		burst:    burst,
	}
}

// Middleware returns the chi-compatible handler.
func (r *RateLimit) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		key := "anon"
		if ws := auth.FromContext(req.Context()); ws != nil {
			key = ws.KeyDBID
		}
		lim := r.limiterFor(key)
		if !lim.Allow() {
			w.Header().Set("Retry-After", "1")
			WriteProblem(w, Problem{
				Title:  "rate_limited",
				Status: http.StatusTooManyRequests,
				Detail: "this key is sending requests too fast — slow down or raise the limit",
			})
			return
		}
		next.ServeHTTP(w, req)
	})
}

func (r *RateLimit) limiterFor(key string) *rate.Limiter {
	r.mu.Lock()
	defer r.mu.Unlock()
	if l, ok := r.limiters[key]; ok {
		return l
	}
	l := rate.NewLimiter(r.rps, r.burst)
	r.limiters[key] = l
	return l
}

// chiRoute returns the chi route pattern for r if available, otherwise the
// raw path. The pattern is what we want for prometheus labels because it
// keeps cardinality bounded.
func chiRoute(r *http.Request) string {
	if rctx := chiRouteContext(r); rctx != "" {
		return rctx
	}
	if r.URL.Path != "" {
		return r.URL.Path
	}
	return "unknown"
}

// WriteJSON serialises v as JSON with the given status code. Errors are
// logged but cannot be reported because the response has already started.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

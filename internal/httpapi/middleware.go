package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"

	"github.com/fheinfling/agentic-coop-db/internal/auth"
	"github.com/fheinfling/agentic-coop-db/internal/observability"
)

// ---- request ID ----------------------------------------------------------------

type requestIDKey struct{}

// GetRequestID returns the request ID from context, or "".
func GetRequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

// RequestID assigns a unique ID to each request. If the client sent an
// X-Request-Id header it is reused; otherwise a random hex string is generated.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-Id")
		if id == "" {
			var b [16]byte
			_, _ = rand.Read(b[:])
			id = hex.EncodeToString(b[:])
		}
		w.Header().Set("X-Request-Id", id)
		ctx := context.WithValue(r.Context(), requestIDKey{}, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// ---- real IP -------------------------------------------------------------------

// RealIP overwrites r.RemoteAddr from X-Real-Ip or X-Forwarded-For when
// the request comes through a reverse proxy.
func RealIP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if rip := r.Header.Get("X-Real-Ip"); rip != "" {
			r.RemoteAddr = rip
		} else if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
			if i := strings.IndexByte(fwd, ','); i > 0 {
				r.RemoteAddr = strings.TrimSpace(fwd[:i])
			} else {
				r.RemoteAddr = strings.TrimSpace(fwd)
			}
		}
		next.ServeHTTP(w, r)
	})
}

// ---- recoverer -----------------------------------------------------------------

// Recoverer catches panics and returns a 500. http.ErrAbortHandler is
// re-panicked so net/http can abort the connection.
func Recoverer(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rvr := recover(); rvr != nil {
					if rvr == http.ErrAbortHandler { //nolint:errorlint // sentinel comparison
						panic(rvr)
					}
					logger.Error("panic recovered",
						"panic", rvr,
						"method", r.Method,
						"path", r.URL.Path,
					)
					http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// ---- response writer wrapper ---------------------------------------------------

// responseWriter captures the status code and bytes written so middleware
// can record them after the handler returns.
type responseWriter struct {
	http.ResponseWriter
	status       int
	bytesWritten int
	wroteHeader  bool
}

func wrapResponseWriter(w http.ResponseWriter) *responseWriter {
	return &responseWriter{ResponseWriter: w, status: http.StatusOK}
}

func (w *responseWriter) WriteHeader(code int) {
	if w.wroteHeader {
		return
	}
	w.status = code
	w.wroteHeader = true
	w.ResponseWriter.WriteHeader(code)
}

func (w *responseWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.wroteHeader = true
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytesWritten += n
	return n, err
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (w *responseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

// ---- MaxBodyBytes --------------------------------------------------------------

// MaxBodyBytes returns a middleware that caps the request body to n bytes.
func MaxBodyBytes(n int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, n)
			next.ServeHTTP(w, r)
		})
	}
}

// ---- metrics -------------------------------------------------------------------

// MetricsMiddleware records prometheus stats for every request.
func MetricsMiddleware(m *observability.Metrics) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := wrapResponseWriter(w)
			next.ServeHTTP(ww, r)
			duration := time.Since(start).Seconds()
			route := r.URL.Path
			if route == "" {
				route = "unknown"
			}
			status := strconv.Itoa(ww.status)
			m.RequestDuration.WithLabelValues(route, r.Method, status).Observe(duration)
			m.RequestsTotal.WithLabelValues(route, r.Method, status).Inc()
		})
	}
}

// ---- access log ----------------------------------------------------------------

// AccessLog logs one structured line per request at INFO level.
func AccessLog(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := wrapResponseWriter(w)
			next.ServeHTTP(ww, r)
			logger.LogAttrs(r.Context(), slog.LevelInfo, "http",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", ww.status),
				slog.Int("bytes_written", ww.bytesWritten),
				slog.Duration("duration", time.Since(start)),
				slog.String("remote", r.RemoteAddr),
				slog.String("request_id", GetRequestID(r.Context())),
			)
		})
	}
}

// ---- token bucket (replaces golang.org/x/time/rate) ----------------------------

// tokenBucket is a standard token-bucket rate limiter.
type tokenBucket struct {
	mu       sync.Mutex
	tokens   float64
	max      float64
	rate     float64 // tokens per second
	lastTime time.Time
}

func newTokenBucket(rps float64, burst int) *tokenBucket {
	return &tokenBucket{
		tokens:   float64(burst),
		max:      float64(burst),
		rate:     rps,
		lastTime: time.Now(),
	}
}

func (b *tokenBucket) refill() {
	now := time.Now()
	elapsed := now.Sub(b.lastTime).Seconds()
	b.lastTime = now
	b.tokens = math.Min(b.tokens+elapsed*b.rate, b.max)
}

// Allow consumes one token and returns true, or returns false if empty.
func (b *tokenBucket) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.refill()
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// Tokens returns the current number of available tokens without consuming any.
func (b *tokenBucket) Tokens() float64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.refill()
	return b.tokens
}

// Rate returns the refill rate in tokens per second.
func (b *tokenBucket) Rate() float64 {
	return b.rate
}

// ---- per-key rate limiter ------------------------------------------------------

// RateLimit applies a per-key token bucket. Unauthenticated requests share
// a single "anon" bucket so they cannot starve authenticated traffic.
// Buckets are stored in a bounded LRU cache to prevent unbounded memory growth.
type RateLimit struct {
	mu       sync.Mutex
	limiters *lru.Cache[string, *tokenBucket]
	rps      float64
	burst    int
}

// NewRateLimit constructs a per-key rate limiter backed by an LRU cache.
func NewRateLimit(rps float64, burst int) *RateLimit {
	if rps <= 0 {
		rps = 60
	}
	if burst <= 0 {
		burst = 120
	}
	const maxBuckets = 4096
	cache, _ := lru.New[string, *tokenBucket](maxBuckets)
	return &RateLimit{
		limiters: cache,
		rps:      rps,
		burst:    burst,
	}
}

// Middleware returns the rate-limiting http middleware.
func (r *RateLimit) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		key := "anon"
		if ws := auth.FromContext(req.Context()); ws != nil {
			key = ws.KeyDBID
		}
		lim := r.limiterFor(key)
		w.Header().Set("X-RateLimit-Limit", strconv.Itoa(int(lim.Rate())))
		if !lim.Allow() {
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.Header().Set("Retry-After", "1")
			WriteProblem(w, Problem{
				Title:  "rate_limited",
				Status: http.StatusTooManyRequests,
				Detail: "this key is sending requests too fast — slow down or raise the limit",
			})
			return
		}
		w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(int(lim.Tokens())))
		next.ServeHTTP(w, req)
	})
}

func (r *RateLimit) limiterFor(key string) *tokenBucket {
	r.mu.Lock()
	defer r.mu.Unlock()
	if l, ok := r.limiters.Get(key); ok {
		return l
	}
	l := newTokenBucket(r.rps, r.burst)
	r.limiters.Add(key, l)
	return l
}

// WriteJSON serialises v as JSON with the given status code.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

package httpapi_test

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/fheinfling/agentic-coop-db/internal/auth"
	"github.com/fheinfling/agentic-coop-db/internal/httpapi"
)

// readBodyHandler simulates what every real handler does: read the request
// body and return 413 if the read fails (body too large).
func readBodyHandler(w http.ResponseWriter, r *http.Request) {
	_, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusRequestEntityTooLarge)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// ---- MaxBodyBytes -----------------------------------------------------------

func TestMaxBodyBytes_UnderLimit(t *testing.T) {
	mw := httpapi.MaxBodyBytes(100)
	handler := mw(http.HandlerFunc(readBodyHandler))
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(bytes.Repeat([]byte("x"), 50)))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200", w.Code)
	}
}

func TestMaxBodyBytes_OverLimit(t *testing.T) {
	mw := httpapi.MaxBodyBytes(10)
	handler := mw(http.HandlerFunc(readBodyHandler))
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(bytes.Repeat([]byte("x"), 20)))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status: got %d, want 413", w.Code)
	}
}

// ---- RateLimit --------------------------------------------------------------

func okHandler(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }

func TestRateLimit_SetsHeaders(t *testing.T) {
	rl := httpapi.NewRateLimit(100, 100)
	handler := rl.Middleware(http.HandlerFunc(okHandler))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	if got := w.Header().Get("X-RateLimit-Limit"); got == "" {
		t.Error("X-RateLimit-Limit header missing")
	}
	if got := w.Header().Get("X-RateLimit-Remaining"); got == "" {
		t.Error("X-RateLimit-Remaining header missing")
	}
}

func TestRateLimit_RemainingDecreases(t *testing.T) {
	rl := httpapi.NewRateLimit(100, 10)
	handler := rl.Middleware(http.HandlerFunc(okHandler))

	do := func() int {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		v, _ := strconv.Atoi(w.Header().Get("X-RateLimit-Remaining"))
		return v
	}
	first := do()
	second := do()
	if second > first {
		t.Errorf("X-RateLimit-Remaining should not increase: first=%d second=%d", first, second)
	}
}

func TestRateLimit_Returns429WhenExhausted(t *testing.T) {
	// burst=1, very low rps so tokens don't refill during the test.
	rl := httpapi.NewRateLimit(0.001, 1)
	handler := rl.Middleware(http.HandlerFunc(okHandler))

	do := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w
	}

	first := do()
	if first.Code != http.StatusOK {
		t.Fatalf("first request: got %d, want 200", first.Code)
	}
	second := do()
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("second request (burst exhausted): got %d, want 429", second.Code)
	}
	if got := second.Header().Get("X-RateLimit-Remaining"); got != "0" {
		t.Errorf("X-RateLimit-Remaining on 429: got %q, want 0", got)
	}
	if got := second.Header().Get("Retry-After"); got == "" {
		t.Error("Retry-After header missing on 429")
	}
}

func TestRateLimit_PerKeyBuckets(t *testing.T) {
	// burst=1 per key — two different authenticated keys each get their own
	// independent bucket, so both first requests should succeed.
	rl := httpapi.NewRateLimit(0.001, 1)
	handler := rl.Middleware(http.HandlerFunc(okHandler))

	doAs := func(keyDBID string) int {
		ws := &auth.WorkspaceContext{KeyDBID: keyDBID}
		ctx := auth.NewContext(httptest.NewRequest(http.MethodGet, "/", nil).Context(), ws)
		req := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w.Code
	}

	if code := doAs("key-alice"); code != http.StatusOK {
		t.Errorf("key-alice first request: got %d, want 200", code)
	}
	if code := doAs("key-bob"); code != http.StatusOK {
		t.Errorf("key-bob first request: got %d, want 200 (buckets must be independent)", code)
	}
	// Second request from alice must be rate-limited (her bucket is exhausted).
	if code := doAs("key-alice"); code != http.StatusTooManyRequests {
		t.Errorf("key-alice second request: got %d, want 429", code)
	}
	// Bob still has his full bucket — but his second request should also be limited.
	if code := doAs("key-bob"); code != http.StatusTooManyRequests {
		t.Errorf("key-bob second request: got %d, want 429", code)
	}
	// Verify the 429 body mentions "rate_limited". The anon bucket is fresh
	// (burst=1), so consume it with the first call, then assert on the 429.
	anonDo := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w
	}
	anonDo() // consume the anon token
	if got := anonDo().Body.String(); !strings.Contains(got, "rate_limited") {
		t.Errorf("429 body should mention rate_limited, got: %s", got)
	}
}

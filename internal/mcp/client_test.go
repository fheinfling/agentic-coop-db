package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// fakeGateway records the last request and returns a canned response.
type fakeGateway struct {
	t *testing.T

	// Captured from last request
	method  string
	path    string
	headers http.Header
	body    []byte

	// Canned response
	statusCode   int
	responseBody any
}

func (fg *fakeGateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	fg.method = r.Method
	fg.path = r.URL.Path
	fg.headers = r.Header.Clone()
	if r.Body != nil {
		fg.body, _ = io.ReadAll(r.Body)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(fg.statusCode)
	if fg.responseBody != nil {
		_ = json.NewEncoder(w).Encode(fg.responseBody)
	}
}

func TestClient_SQLExecute_Success(t *testing.T) {
	fg := &fakeGateway{
		t:          t,
		statusCode: 200,
		responseBody: map[string]any{
			"command":       "SELECT",
			"columns":       []string{"id", "body"},
			"rows":          [][]any{{"1", "hello"}},
			"rows_affected": 1,
			"duration_ms":   4,
		},
	}
	srv := httptest.NewServer(fg)
	defer srv.Close()

	c := NewClient(ClientConfig{GatewayURL: srv.URL, APIKey: "acd_test_abc123_secret"})
	result, err := c.SQLExecute(context.Background(), "SELECT id, body FROM notes WHERE id = $1", []any{"1"}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify request shape
	if fg.method != "POST" {
		t.Errorf("method = %q, want POST", fg.method)
	}
	if fg.path != "/v1/sql/execute" {
		t.Errorf("path = %q, want /v1/sql/execute", fg.path)
	}

	var reqBody map[string]any
	if err := json.Unmarshal(fg.body, &reqBody); err != nil {
		t.Fatalf("unmarshal request body: %v", err)
	}
	if reqBody["sql"] != "SELECT id, body FROM notes WHERE id = $1" {
		t.Errorf("sql = %v, want SELECT query", reqBody["sql"])
	}

	// Verify response parsing
	if result.Command != "SELECT" {
		t.Errorf("Command = %q, want SELECT", result.Command)
	}
	if len(result.Columns) != 2 {
		t.Errorf("len(Columns) = %d, want 2", len(result.Columns))
	}
}

func TestClient_SQLExecute_WithIdempotencyKey(t *testing.T) {
	fg := &fakeGateway{
		t:            t,
		statusCode:   200,
		responseBody: map[string]any{"command": "INSERT", "rows_affected": 1},
	}
	srv := httptest.NewServer(fg)
	defer srv.Close()

	c := NewClient(ClientConfig{GatewayURL: srv.URL, APIKey: "acd_test_abc123_secret"})
	_, err := c.SQLExecute(context.Background(), "INSERT INTO t(id) VALUES ($1)", []any{"x"}, "my-idemp-key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := fg.headers.Get("Idempotency-Key"); got != "my-idemp-key" {
		t.Errorf("Idempotency-Key = %q, want %q", got, "my-idemp-key")
	}
}

func TestClient_SQLExecute_ErrorMapping(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		body       map[string]any
		wantTitle  string
		wantStatus int
	}{
		{
			name:   "400 validation",
			status: 400,
			body:   map[string]any{"title": "parse_error", "detail": "syntax error"},
			wantTitle:  "parse_error",
			wantStatus: 400,
		},
		{
			name:   "401 unauthorized",
			status: 401,
			body:   map[string]any{"title": "unauthorized", "detail": "invalid key"},
			wantTitle:  "unauthorized",
			wantStatus: 401,
		},
		{
			name:   "403 forbidden",
			status: 403,
			body:   map[string]any{"title": "permission_denied", "detail": "no access", "sqlstate": "42501"},
			wantTitle:  "permission_denied",
			wantStatus: 403,
		},
		{
			name:   "429 rate limited",
			status: 429,
			body:   map[string]any{"title": "rate_limited", "detail": "slow down"},
			wantTitle:  "rate_limited",
			wantStatus: 429,
		},
		{
			name:   "500 server error",
			status: 500,
			body:   map[string]any{"title": "database_error", "detail": "internal"},
			wantTitle:  "database_error",
			wantStatus: 500,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fg := &fakeGateway{t: t, statusCode: tc.status, responseBody: tc.body}
			srv := httptest.NewServer(fg)
			defer srv.Close()

			c := NewClient(ClientConfig{GatewayURL: srv.URL, APIKey: "acd_test_abc123_secret"})
			_, err := c.SQLExecute(context.Background(), "SELECT 1", nil, "")
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			ge, ok := err.(*GatewayError)
			if !ok {
				t.Fatalf("error type = %T, want *GatewayError", err)
			}
			if ge.Title != tc.wantTitle {
				t.Errorf("Title = %q, want %q", ge.Title, tc.wantTitle)
			}
			if ge.Status != tc.wantStatus {
				t.Errorf("Status = %d, want %d", ge.Status, tc.wantStatus)
			}
		})
	}
}

func TestClient_RPCCall_Success(t *testing.T) {
	fg := &fakeGateway{
		t:          t,
		statusCode: 200,
		responseBody: map[string]any{
			"result": map[string]any{"id": "doc-1", "body": "updated"},
		},
	}
	srv := httptest.NewServer(fg)
	defer srv.Close()

	c := NewClient(ClientConfig{GatewayURL: srv.URL, APIKey: "acd_test_abc123_secret"})
	result, err := c.RPCCall(context.Background(), "upsert_document", map[string]any{"id": "doc-1", "body": "updated"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fg.path != "/v1/rpc/call" {
		t.Errorf("path = %q, want /v1/rpc/call", fg.path)
	}

	var reqBody map[string]any
	if err := json.Unmarshal(fg.body, &reqBody); err != nil {
		t.Fatalf("unmarshal request body: %v", err)
	}
	if reqBody["procedure"] != "upsert_document" {
		t.Errorf("procedure = %v, want upsert_document", reqBody["procedure"])
	}

	if result == nil {
		t.Fatal("result is nil")
	}
}

func TestClient_Me_Success(t *testing.T) {
	fg := &fakeGateway{
		t:          t,
		statusCode: 200,
		responseBody: map[string]any{
			"workspace_id": "ws-123",
			"key_id":       "k-456",
			"role":         "dbuser",
			"env":          "test",
		},
	}
	srv := httptest.NewServer(fg)
	defer srv.Close()

	c := NewClient(ClientConfig{GatewayURL: srv.URL, APIKey: "acd_test_abc123_secret"})
	me, err := c.Me(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fg.method != "GET" {
		t.Errorf("method = %q, want GET", fg.method)
	}
	if fg.path != "/v1/me" {
		t.Errorf("path = %q, want /v1/me", fg.path)
	}
	if me.WorkspaceID != "ws-123" {
		t.Errorf("WorkspaceID = %q, want ws-123", me.WorkspaceID)
	}
	if me.Role != "dbuser" {
		t.Errorf("Role = %q, want dbuser", me.Role)
	}
}

func TestClient_Health_Success(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		switch r.URL.Path {
		case "/healthz":
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		case "/readyz":
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		}
	}))
	defer srv.Close()

	c := NewClient(ClientConfig{GatewayURL: srv.URL, APIKey: "acd_test_abc123_secret"})
	result, err := c.Health(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if callCount != 2 {
		t.Errorf("callCount = %d, want 2 (healthz + readyz)", callCount)
	}
	if result == nil {
		t.Fatal("result is nil")
	}
}

func TestClient_AuthHeaderSet(t *testing.T) {
	fg := &fakeGateway{
		t:            t,
		statusCode:   200,
		responseBody: map[string]any{"command": "SELECT"},
	}
	srv := httptest.NewServer(fg)
	defer srv.Close()

	key := "acd_test_abc123_secret"
	c := NewClient(ClientConfig{GatewayURL: srv.URL, APIKey: key})
	_, _ = c.SQLExecute(context.Background(), "SELECT 1", nil, "")

	if got := fg.headers.Get("Authorization"); got != "Bearer "+key {
		t.Errorf("Authorization = %q, want %q", got, "Bearer "+key)
	}
}

func TestClient_UserAgentSet(t *testing.T) {
	fg := &fakeGateway{
		t:            t,
		statusCode:   200,
		responseBody: map[string]any{"command": "SELECT"},
	}
	srv := httptest.NewServer(fg)
	defer srv.Close()

	c := NewClient(ClientConfig{GatewayURL: srv.URL, APIKey: "acd_test_abc123_secret"})
	_, _ = c.SQLExecute(context.Background(), "SELECT 1", nil, "")

	ua := fg.headers.Get("User-Agent")
	if ua == "" {
		t.Error("User-Agent header not set")
	}
	if len(ua) < 10 {
		t.Errorf("User-Agent = %q, expected agentic-coop-db-mcp/<version>", ua)
	}
}

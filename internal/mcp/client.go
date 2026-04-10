package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/fheinfling/agentic-coop-db/internal/version"
)

// Doer is the interface tool handlers depend on. It is the test seam —
// production code uses *Client; tests use a fakeDoer.
type Doer interface {
	SQLExecute(ctx context.Context, sql string, params []any, idempotencyKey string) (*SQLResult, error)
	RPCCall(ctx context.Context, procedure string, args map[string]any) (map[string]any, error)
	Me(ctx context.Context) (*MeResult, error)
	Health(ctx context.Context) (*HealthResult, error)
}

// SQLResult mirrors the gateway's /v1/sql/execute response.
type SQLResult struct {
	Command      string   `json:"command"`
	Columns      []string `json:"columns"`
	Rows         [][]any  `json:"rows"`
	RowsAffected int64    `json:"rows_affected"`
	DurationMS   int      `json:"duration_ms"`
}

// MeResult mirrors the gateway's /v1/me response.
type MeResult struct {
	WorkspaceID string `json:"workspace_id"`
	KeyID       string `json:"key_id"`
	Role        string `json:"role"`
	Env         string `json:"env"`
}

// HealthResult combines /healthz and /readyz responses.
type HealthResult struct {
	Healthy bool   `json:"healthy"`
	Ready   bool   `json:"ready"`
	Detail  string `json:"detail,omitempty"`
}

// GatewayError carries the RFC 7807 Problem detail from the gateway.
type GatewayError struct {
	Title    string `json:"title"`
	Detail   string `json:"detail"`
	Status   int    `json:"status"`
	SQLState string `json:"sqlstate,omitempty"`
}

func (e *GatewayError) Error() string {
	if e.Detail != "" {
		return fmt.Sprintf("%s: %s", e.Title, e.Detail)
	}
	return e.Title
}

// ClientConfig holds the gateway connection parameters.
type ClientConfig struct {
	GatewayURL string
	APIKey     string
	HTTPClient *http.Client
}

// Client is a thin HTTP client for the Agentic Coop DB gateway.
type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
	userAgent  string
}

// NewClient constructs a Client. Panics if GatewayURL or APIKey is empty.
func NewClient(cfg ClientConfig) *Client {
	if cfg.GatewayURL == "" {
		panic("mcp.NewClient: GatewayURL is required")
	}
	if cfg.APIKey == "" {
		panic("mcp.NewClient: APIKey is required")
	}
	hc := cfg.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{
		baseURL:    strings.TrimRight(cfg.GatewayURL, "/"),
		apiKey:     cfg.APIKey,
		httpClient: hc,
		userAgent:  "agentic-coop-db-mcp/" + version.Version,
	}
}

// SQLExecute calls POST /v1/sql/execute.
func (c *Client) SQLExecute(ctx context.Context, sql string, params []any, idempotencyKey string) (*SQLResult, error) {
	body := map[string]any{"sql": sql}
	if params != nil {
		body["params"] = params
	}

	headers := make(http.Header)
	if idempotencyKey != "" {
		headers.Set("Idempotency-Key", idempotencyKey)
	}

	var result SQLResult
	if err := c.post(ctx, "/v1/sql/execute", body, headers, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// RPCCall calls POST /v1/rpc/call.
func (c *Client) RPCCall(ctx context.Context, procedure string, args map[string]any) (map[string]any, error) {
	body := map[string]any{"procedure": procedure}
	if args != nil {
		body["args"] = args
	}

	var result map[string]any
	if err := c.post(ctx, "/v1/rpc/call", body, nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// Me calls GET /v1/me.
func (c *Client) Me(ctx context.Context) (*MeResult, error) {
	var result MeResult
	if err := c.get(ctx, "/v1/me", &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// Health calls GET /healthz and GET /readyz.
func (c *Client) Health(ctx context.Context) (*HealthResult, error) {
	healthErr := c.get(ctx, "/healthz", &json.RawMessage{})
	readyErr := c.get(ctx, "/readyz", &json.RawMessage{})

	result := &HealthResult{
		Healthy: healthErr == nil,
		Ready:   readyErr == nil,
	}
	if healthErr != nil {
		result.Detail = fmt.Sprintf("healthz: %v", healthErr)
	} else if readyErr != nil {
		result.Detail = fmt.Sprintf("readyz: %v", readyErr)
	}
	return result, nil
}

// post sends a POST request to the gateway and decodes the response.
func (c *Client) post(ctx context.Context, path string, body any, extraHeaders http.Header, dest any) error {
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(jsonBody))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	c.setCommonHeaders(req)
	for k, vals := range extraHeaders {
		for _, v := range vals {
			req.Header.Set(k, v)
		}
	}

	return c.doAndDecode(req, dest)
}

// get sends a GET request to the gateway and decodes the response.
func (c *Client) get(ctx context.Context, path string, dest any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	c.setCommonHeaders(req)
	return c.doAndDecode(req, dest)
}

func (c *Client) setCommonHeaders(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("User-Agent", c.userAgent)
}

func (c *Client) doAndDecode(req *http.Request, dest any) error {
	resp, err := c.httpClient.Do(req) //nolint:gosec // URL is constructed from trusted GatewayURL config, not user input
	if err != nil {
		return fmt.Errorf("http request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	const maxResponseSize = 10 << 20 // 10 MB
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		ge := &GatewayError{Status: resp.StatusCode}
		if len(respBody) > 0 {
			_ = json.Unmarshal(respBody, ge)
		}
		if ge.Title == "" {
			ge.Title = http.StatusText(resp.StatusCode)
		}
		return ge
	}

	if dest != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, dest); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

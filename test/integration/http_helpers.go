//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// NewReq builds an authenticated http.Request with the given JSON body.
// Exported so tests in sibling packages (test/security/*) can reuse it.
func NewReq(t *testing.T, url, token string, body any) *http.Request {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		require.NoError(t, err)
		r = bytes.NewReader(b)
	}
	method := http.MethodGet
	if body != nil {
		method = http.MethodPost
	}
	req, err := http.NewRequest(method, url, r)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req
}

// postJSON sends a POST and decodes the response into a map.
func postJSON(t *testing.T, h *Harness, token, path string, body any) map[string]any {
	t.Helper()
	req := NewReq(t, h.Server.URL+path, token, body)
	if body == nil {
		req.Method = http.MethodGet
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	out := map[string]any{}
	if resp.ContentLength != 0 {
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	}
	out["__status"] = resp.StatusCode
	return out
}

// keyIDFromToken parses the token and looks up the api_keys.id PK.
func keyIDFromToken(t *testing.T, h *Harness, token string) string {
	t.Helper()
	// token shape: aic_<env>_<key_id>_<secret>
	parts := splitN(token, "_", 4)
	require.Len(t, parts, 4)
	keyID := parts[2]
	var dbID string
	require.NoError(t, h.Pool.QueryRow(context.Background(),
		`SELECT id FROM api_keys WHERE key_id = $1`, keyID).Scan(&dbID))
	return dbID
}

func splitN(s, sep string, n int) []string {
	out := []string{}
	for i := 0; i < n-1; i++ {
		idx := indexOf(s, sep)
		if idx < 0 {
			break
		}
		out = append(out, s[:idx])
		s = s[idx+len(sep):]
	}
	out = append(out, s)
	return out
}

func indexOf(s, sep string) int {
	for i := 0; i+len(sep) <= len(s); i++ {
		if s[i:i+len(sep)] == sep {
			return i
		}
	}
	return -1
}

//go:build integration

package security

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/fheinfling/ai-coop-db/test/integration"
)

// TestSQLBypassAttempts walks through the canonical injection / escape
// attempts and asserts each one is blocked at the validator OR by Postgres
// role grants — never at runtime by the application.
func TestSQLBypassAttempts(t *testing.T) {
	h := integration.StartHarness(t)
	ctx := context.Background()
	_, token := h.MintWorkspaceAndKey(ctx, "sec-bypass", "dbuser")

	cases := []struct {
		name string
		sql  string
	}{
		{"stacked statements", "SELECT 1; DROP TABLE workspaces;"},
		{"pg_read_file", "SELECT pg_read_file('/etc/passwd')"},
		{"copy from program", "COPY notes FROM PROGRAM 'cat /etc/passwd'"},
		{"select pg_authid", "SELECT * FROM pg_authid"},
		{"cte delete", "WITH d AS (DELETE FROM workspaces RETURNING *) SELECT * FROM d"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := integration.NewReq(t, h.Server.URL+"/v1/sql/execute", token, map[string]any{
				"sql":    tc.sql,
				"params": []any{},
			})
			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()
			require.NotEqual(t, http.StatusOK, resp.StatusCode, "bypass attempt %q should NOT return 200", tc.name)
		})
	}
}

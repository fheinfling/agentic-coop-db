//go:build integration

package integration

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUnparseableSQLRejected(t *testing.T) {
	h := StartHarness(t)
	ctx := context.Background()
	_, token := h.MintWorkspaceAndKey(ctx, "ws-parse", "dbadmin")

	resp := postJSON(t, h, token, "/v1/sql/execute", map[string]any{
		"sql":    "NOT VALID SQL!!!",
		"params": []any{},
	})
	require.Equal(t, http.StatusBadRequest, resp["__status"])
	require.Equal(t, "parse_error", resp["title"])
}

func TestParamsMismatchRejected(t *testing.T) {
	h := StartHarness(t)
	ctx := context.Background()
	_, token := h.MintWorkspaceAndKey(ctx, "ws-params", "dbadmin")

	resp := postJSON(t, h, token, "/v1/sql/execute", map[string]any{
		"sql":    "SELECT $1, $2",
		"params": []any{"only-one"},
	})
	require.Equal(t, http.StatusBadRequest, resp["__status"])
	require.Equal(t, "params_mismatch", resp["title"])
}

func TestMultipleStatementsRejected(t *testing.T) {
	h := StartHarness(t)
	ctx := context.Background()
	_, token := h.MintWorkspaceAndKey(ctx, "ws-multi", "dbadmin")

	resp := postJSON(t, h, token, "/v1/sql/execute", map[string]any{
		"sql":    "SELECT 1; DROP TABLE workspaces;",
		"params": []any{},
	})
	require.Equal(t, http.StatusBadRequest, resp["__status"])
	require.Equal(t, "multiple_statements", resp["title"])
}

func TestSelectRoundTrip(t *testing.T) {
	h := StartHarness(t)
	ctx := context.Background()
	_, token := h.MintWorkspaceAndKey(ctx, "ws-select", "dbadmin")

	resp := postJSON(t, h, token, "/v1/sql/execute", map[string]any{
		"sql":    "SELECT 1 AS one, $1::text AS hello",
		"params": []any{"world"},
	})
	require.Equal(t, http.StatusOK, resp["__status"])
	require.Equal(t, "SELECT", resp["command"])
}

//go:build integration

package integration

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestCrossWorkspaceIsolation(t *testing.T) {
	h := StartHarness(t)
	ctx := context.Background()

	wsA, tokA := h.MintWorkspaceAndKey(ctx, "ws-rls-a", "dbuser")
	_, tokB := h.MintWorkspaceAndKey(ctx, "ws-rls-b", "dbuser")

	// Workspace A inserts a row.
	rowID := uuid.New().String()
	resp := postJSON(t, h, tokA, "/v1/sql/execute", map[string]any{
		"sql":    "INSERT INTO notes (id, workspace_id, body) VALUES ($1, $2, $3)",
		"params": []any{rowID, wsA, "secret-a"},
	})
	require.Equal(t, http.StatusOK, resp["__status"], resp)

	// Workspace B reads — should see zero rows.
	respB := postJSON(t, h, tokB, "/v1/sql/execute", map[string]any{
		"sql":    "SELECT body FROM notes WHERE id = $1",
		"params": []any{rowID},
	})
	require.Equal(t, http.StatusOK, respB["__status"])
	rows, _ := respB["rows"].([]any)
	require.Empty(t, rows, "workspace B must not see workspace A's rows")
}

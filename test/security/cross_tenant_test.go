//go:build integration

// Package security contains the cross-tenant denial matrix and the
// SQL bypass attempt tests. They share the integration harness so the
// container only starts once per package.
package security

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/fheinfling/agentic-coop-db/test/integration"
)

func TestCrossTenantWriteBlocked(t *testing.T) {
	h := integration.StartHarness(t)
	ctx := context.Background()

	wsA, tokA := h.MintWorkspaceAndKey(ctx, "sec-a", "dbuser")
	_, tokB := h.MintWorkspaceAndKey(ctx, "sec-b", "dbuser")
	_ = wsA

	// Workspace B tries to insert a row claiming workspace A's id.
	rowID := uuid.New().String()
	req := integration.NewReq(t, h.Server.URL+"/v1/sql/execute", tokB, map[string]any{
		"sql":    "INSERT INTO notes (id, workspace_id, body) VALUES ($1, $2, $3)",
		"params": []any{rowID, wsA, "stolen"},
	})
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	// Either a 403 (RLS WITH CHECK violation surfaced as permission denied)
	// or a 409 (integrity error). Anything 2xx is a security failure.
	require.NotEqual(t, http.StatusOK, resp.StatusCode)
	_ = tokA
}

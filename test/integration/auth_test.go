//go:build integration

package integration

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRevokedKeyRejected(t *testing.T) {
	h := StartHarness(t)
	ctx := context.Background()

	_, token := h.MintWorkspaceAndKey(ctx, "ws-revoke", "dbadmin")

	// First call works.
	req := NewReq(t, h.Server.URL+"/v1/me", token, nil)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	// Revoke the key directly via the store.
	keyDBID := keyIDFromToken(t, h, token)
	require.NoError(t, h.Auth.Revoke(ctx, keyDBID))

	// Cache TTL is short in tests; force a fresh resolve by waiting.
	time.Sleep(50 * time.Millisecond)

	req2 := NewReq(t, h.Server.URL+"/v1/me", token, nil)
	resp2, err := http.DefaultClient.Do(req2)
	require.NoError(t, err)
	defer resp2.Body.Close()
	require.Equal(t, http.StatusUnauthorized, resp2.StatusCode)
}

func TestSingleWorkspaceAndRoleContext(t *testing.T) {
	h := StartHarness(t)
	ctx := context.Background()

	wsID, token := h.MintWorkspaceAndKey(ctx, "ws-context", "dbuser")

	body := postJSON(t, h, token, "/v1/me", nil)
	require.Equal(t, wsID, body["workspace_id"])
	require.Equal(t, "dbuser", body["role"])
}

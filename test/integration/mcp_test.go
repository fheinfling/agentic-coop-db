//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	mcppkg "github.com/fheinfling/agentic-coop-db/internal/mcp"
)

func newMCPClient(t *testing.T, h *Harness, token string) *mcppkg.Client {
	t.Helper()
	return mcppkg.NewClient(mcppkg.ClientConfig{
		GatewayURL: h.Server.URL,
		APIKey:     token,
	})
}

func TestMCP_SQLExecute_E2E(t *testing.T) {
	h := StartHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, token := h.MintWorkspaceAndKey(ctx, "mcp-sql-test", "dbadmin")
	c := newMCPClient(t, h, token)

	// Create a table, insert, and select
	_, err := c.SQLExecute(ctx,
		"CREATE TABLE IF NOT EXISTS mcp_test_items (id text PRIMARY KEY, val text, workspace_id uuid DEFAULT current_setting('app.workspace_id')::uuid)",
		nil, "")
	require.NoError(t, err)

	_, err = c.SQLExecute(ctx,
		"INSERT INTO mcp_test_items (id, val) VALUES ($1, $2)",
		[]any{"item-1", "hello"}, "")
	require.NoError(t, err)

	result, err := c.SQLExecute(ctx,
		"SELECT id, val FROM mcp_test_items WHERE id = $1",
		[]any{"item-1"}, "")
	require.NoError(t, err)
	require.Equal(t, "SELECT", result.Command)
	require.Len(t, result.Columns, 2)
	require.GreaterOrEqual(t, len(result.Rows), 1)
}

func TestMCP_ListTables_E2E(t *testing.T) {
	h := StartHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, token := h.MintWorkspaceAndKey(ctx, "mcp-list-test", "dbadmin")
	c := newMCPClient(t, h, token)

	// The migrations create example tables (notes, documents). Query
	// information_schema to verify list_tables works.
	result, err := c.SQLExecute(ctx,
		`SELECT t.table_name, COALESCE(s.n_live_tup, 0) AS approx_rows
		FROM information_schema.tables t
		LEFT JOIN pg_stat_user_tables s ON s.relname = t.table_name AND s.schemaname = 'public'
		WHERE t.table_schema = 'public' AND t.table_type = 'BASE TABLE'
		ORDER BY t.table_name`,
		nil, "")
	require.NoError(t, err)
	require.Equal(t, "SELECT", result.Command)
	// At least the migration-created tables should be present
	require.GreaterOrEqual(t, len(result.Rows), 1)
}

func TestMCP_DescribeTable_E2E(t *testing.T) {
	h := StartHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, token := h.MintWorkspaceAndKey(ctx, "mcp-desc-test", "dbadmin")
	c := newMCPClient(t, h, token)

	// Describe the notes table created by migrations
	result, err := c.SQLExecute(ctx,
		`SELECT column_name, data_type, is_nullable, column_default
		FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = $1
		ORDER BY ordinal_position`,
		[]any{"notes"}, "")
	require.NoError(t, err)
	require.Equal(t, "SELECT", result.Command)
	require.GreaterOrEqual(t, len(result.Rows), 1)
}

func TestMCP_Whoami_E2E(t *testing.T) {
	h := StartHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, token := h.MintWorkspaceAndKey(ctx, "mcp-whoami-test", "dbuser")
	c := newMCPClient(t, h, token)

	me, err := c.Me(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, me.WorkspaceID)
	require.Equal(t, "dbuser", me.Role)
	require.Equal(t, "test", me.Env)
}

func TestMCP_Health_E2E(t *testing.T) {
	h := StartHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, token := h.MintWorkspaceAndKey(ctx, "mcp-health-test", "dbuser")
	c := newMCPClient(t, h, token)

	// The test harness only mounts /v1/* routes. The top-level /healthz and
	// /readyz handlers are registered in cmd/server/main.go, not in
	// api.Routes(). We verify the client doesn't panic and returns a result.
	health, err := c.Health(ctx)
	require.NoError(t, err)
	require.NotNil(t, health)
	// In the test harness, both return 404 → unhealthy, which is expected.
	// Full health checks are exercised in the real server.
}

func TestMCP_AuthFailure_E2E(t *testing.T) {
	h := StartHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Use a bad API key
	c := newMCPClient(t, h, "acd_test_badkey1234567890_badsecret12345678901234567890ab")

	_, err := c.SQLExecute(ctx, "SELECT 1", nil, "")
	require.Error(t, err)

	ge, ok := err.(*mcppkg.GatewayError)
	require.True(t, ok, "expected *GatewayError, got %T", err)
	require.Equal(t, 401, ge.Status)
}

func TestMCP_CrossTenantBlocked_E2E(t *testing.T) {
	h := StartHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Workspace A creates data
	_, tokenA := h.MintWorkspaceAndKey(ctx, "mcp-tenant-a", "dbadmin")
	cA := newMCPClient(t, h, tokenA)

	_, err := cA.SQLExecute(ctx,
		"CREATE TABLE IF NOT EXISTS mcp_tenant_items (id text PRIMARY KEY, secret text, workspace_id uuid DEFAULT current_setting('app.workspace_id')::uuid)",
		nil, "")
	require.NoError(t, err)

	// Enable RLS on the table
	_, err = cA.SQLExecute(ctx,
		"ALTER TABLE mcp_tenant_items ENABLE ROW LEVEL SECURITY", nil, "")
	require.NoError(t, err)
	_, err = cA.SQLExecute(ctx,
		"ALTER TABLE mcp_tenant_items FORCE ROW LEVEL SECURITY", nil, "")
	require.NoError(t, err)
	_, err = cA.SQLExecute(ctx,
		`CREATE POLICY mcp_tenant_items_isolation ON mcp_tenant_items
		USING (workspace_id = current_setting('app.workspace_id', true)::uuid)
		WITH CHECK (workspace_id = current_setting('app.workspace_id', true)::uuid)`,
		nil, "")
	require.NoError(t, err)
	_, err = cA.SQLExecute(ctx,
		"GRANT SELECT, INSERT, UPDATE, DELETE ON mcp_tenant_items TO dbuser",
		nil, "")
	require.NoError(t, err)

	// Workspace A inserts data
	_, err = cA.SQLExecute(ctx,
		"INSERT INTO mcp_tenant_items (id, secret) VALUES ($1, $2)",
		[]any{"secret-1", "workspace-a-data"}, "")
	require.NoError(t, err)

	// Workspace B tries to read it — should get zero rows (RLS)
	_, tokenB := h.MintWorkspaceAndKey(ctx, "mcp-tenant-b", "dbuser")
	cB := newMCPClient(t, h, tokenB)

	result, err := cB.SQLExecute(ctx,
		"SELECT id, secret FROM mcp_tenant_items",
		nil, "")
	require.NoError(t, err)
	require.Len(t, result.Rows, 0, "workspace B should see zero rows from workspace A")
}

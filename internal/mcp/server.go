package mcp

import (
	"context"
	"os"

	gomcp "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/fheinfling/agentic-coop-db/internal/version"
)

// NewServer creates a configured MCP server with all tools registered.
// Panics if client is nil.
func NewServer(client Doer) *server.MCPServer {
	if client == nil {
		panic("mcp.NewServer: client must not be nil")
	}

	srv := server.NewMCPServer(
		"agentic-coop-db",
		version.Version,
		server.WithToolCapabilities(false),
		server.WithRecovery(),
	)

	srv.AddTool(
		gomcp.NewTool("sql_execute",
			gomcp.WithDescription("Execute a parameterized SQL statement against the database. "+
				"Use $1, $2, ... placeholders and pass values in the params array. "+
				"Single statement only; multi-statement chains are rejected."),
			gomcp.WithString("sql",
				gomcp.Description("The SQL statement with $N placeholders"),
				gomcp.Required(),
			),
			gomcp.WithArray("params",
				gomcp.Description("Positional parameter values for $1, $2, ... placeholders"),
			),
			gomcp.WithString("idempotency_key",
				gomcp.Description("Optional key for safely retrying the same write operation"),
			),
		),
		handleSQLExecute(client),
	)

	srv.AddTool(
		gomcp.NewTool("rpc_call",
			gomcp.WithDescription("Call a registered RPC procedure by name. "+
				"RPCs are multi-statement transactions defined server-side."),
			gomcp.WithString("procedure",
				gomcp.Description("Name of the registered procedure"),
				gomcp.Required(),
			),
			gomcp.WithObject("args",
				gomcp.Description("Arguments to pass to the procedure"),
			),
		),
		handleRPCCall(client),
	)

	srv.AddTool(
		gomcp.NewTool("list_tables",
			gomcp.WithDescription("List all user tables in the public schema with approximate row counts. "+
				"Use this to discover the database structure before writing queries."),
		),
		handleListTables(client),
	)

	srv.AddTool(
		gomcp.NewTool("describe_table",
			gomcp.WithDescription("Show column names, types, nullability, and defaults for a table. "+
				"Use this to understand the schema before writing queries."),
			gomcp.WithString("table",
				gomcp.Description("Table name (lowercase, alphanumeric/underscore only)"),
				gomcp.Required(),
			),
		),
		handleDescribeTable(client),
	)

	srv.AddTool(
		gomcp.NewTool("vector_search",
			gomcp.WithDescription("Run a top-k cosine similarity search on a pgvector column. "+
				"Returns matching rows ordered by distance."),
			gomcp.WithString("table",
				gomcp.Description("Table name containing the vector column"),
				gomcp.Required(),
			),
			gomcp.WithString("vector_column",
				gomcp.Description("Name of the vector column"),
				gomcp.Required(),
			),
			gomcp.WithArray("query_embedding",
				gomcp.Description("Query vector as an array of numbers"),
				gomcp.WithNumberItems(),
				gomcp.Required(),
			),
			gomcp.WithNumber("k",
				gomcp.Description("Maximum number of results to return (default 5, max 1000)"),
			),
		),
		handleVectorSearch(client),
	)

	srv.AddTool(
		gomcp.NewTool("vector_upsert",
			gomcp.WithDescription("Insert or update embedding rows (max 100 per call). Each row must have id, metadata (object), and vector (array of floats)."),
			gomcp.WithString("table",
				gomcp.Description("Table name"),
				gomcp.Required(),
			),
			gomcp.WithString("id_column",
				gomcp.Description("Name of the primary key column"),
				gomcp.Required(),
			),
			gomcp.WithString("vector_column",
				gomcp.Description("Name of the vector column"),
				gomcp.Required(),
			),
			gomcp.WithArray("rows",
				gomcp.Description("Rows to upsert, each with id (string), metadata (object), and vector (array of numbers)"),
				gomcp.Required(),
			),
		),
		handleVectorUpsert(client),
	)

	srv.AddTool(
		gomcp.NewTool("whoami",
			gomcp.WithDescription("Show the current workspace, role, and environment for the API key in use."),
		),
		handleWhoami(client),
	)

	srv.AddTool(
		gomcp.NewTool("health",
			gomcp.WithDescription("Check if the database gateway is healthy and ready to accept requests."),
		),
		handleHealth(client),
	)

	return srv
}

// RunStdio starts the MCP server on stdin/stdout. Blocks until the
// connection closes or ctx is cancelled.
func RunStdio(ctx context.Context, srv *server.MCPServer) error {
	stdio := server.NewStdioServer(srv)

	// Use a goroutine to handle context cancellation
	done := make(chan error, 1)
	go func() {
		done <- stdio.Listen(ctx, os.Stdin, os.Stdout)
	}()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

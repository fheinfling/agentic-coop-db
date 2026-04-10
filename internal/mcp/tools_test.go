package mcp

import (
	"context"
	"fmt"
	"strings"
	"testing"

	gomcp "github.com/mark3labs/mcp-go/mcp"
)

// fakeDoer is a hand-written test double for the Doer interface.
type fakeDoer struct {
	sqlResult    *SQLResult
	sqlErr       error
	rpcResult    map[string]any
	rpcErr       error
	meResult     *MeResult
	meErr        error
	healthResult *HealthResult
	healthErr    error

	// Call tracking
	sqlCalls   int
	lastSQL    string
	lastParams []any
	lastIdemp  string

	rpcCalls      int
	lastProcedure string
	lastArgs      map[string]any
}

func (f *fakeDoer) SQLExecute(_ context.Context, sql string, params []any, idempotencyKey string) (*SQLResult, error) {
	f.sqlCalls++
	f.lastSQL = sql
	f.lastParams = params
	f.lastIdemp = idempotencyKey
	return f.sqlResult, f.sqlErr
}

func (f *fakeDoer) RPCCall(_ context.Context, procedure string, args map[string]any) (map[string]any, error) {
	f.rpcCalls++
	f.lastProcedure = procedure
	f.lastArgs = args
	return f.rpcResult, f.rpcErr
}

func (f *fakeDoer) Me(_ context.Context) (*MeResult, error) {
	return f.meResult, f.meErr
}

func (f *fakeDoer) Health(_ context.Context) (*HealthResult, error) {
	return f.healthResult, f.healthErr
}

func callTool(t *testing.T, h toolHandler, args map[string]any) *gomcp.CallToolResult {
	t.Helper()
	req := gomcp.CallToolRequest{}
	req.Params.Arguments = args
	result, err := h(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return result
}

func callToolExpectErr(t *testing.T, h toolHandler, args map[string]any) *gomcp.CallToolResult {
	t.Helper()
	req := gomcp.CallToolRequest{}
	req.Params.Arguments = args
	result, err := h(context.Background(), req)
	if err != nil {
		// Protocol-level errors are also acceptable
		return nil
	}
	if result != nil && !result.IsError {
		t.Fatal("expected error result, got success")
	}
	return result
}

func resultText(t *testing.T, result *gomcp.CallToolResult) string {
	t.Helper()
	if result == nil || len(result.Content) == 0 {
		t.Fatal("result has no content")
	}
	tc, ok := result.Content[0].(gomcp.TextContent)
	if !ok {
		t.Fatalf("content type = %T, want TextContent", result.Content[0])
	}
	return tc.Text
}

func TestToolSQLExecute_CallsClient(t *testing.T) {
	fd := &fakeDoer{
		sqlResult: &SQLResult{
			Command: "SELECT",
			Columns: []string{"id"},
			Rows:    [][]any{{"1"}},
		},
	}
	h := handleSQLExecute(fd)
	result := callTool(t, h, map[string]any{
		"sql":    "SELECT id FROM t",
		"params": []any{},
	})

	if fd.sqlCalls != 1 {
		t.Errorf("sqlCalls = %d, want 1", fd.sqlCalls)
	}
	if fd.lastSQL != "SELECT id FROM t" {
		t.Errorf("lastSQL = %q, want SELECT query", fd.lastSQL)
	}
	text := resultText(t, result)
	if !strings.Contains(text, "SELECT") {
		t.Errorf("result text = %q, should contain SELECT", text)
	}
}

func TestToolSQLExecute_MissingSQL(t *testing.T) {
	fd := &fakeDoer{}
	h := handleSQLExecute(fd)
	result := callToolExpectErr(t, h, map[string]any{})
	if result != nil {
		text := resultText(t, result)
		if !strings.Contains(strings.ToLower(text), "sql") {
			t.Errorf("error should mention sql, got: %s", text)
		}
	}
}

func TestToolSQLExecute_ErrorPropagation(t *testing.T) {
	fd := &fakeDoer{
		sqlErr: &GatewayError{Title: "parse_error", Detail: "bad sql", Status: 400},
	}
	h := handleSQLExecute(fd)
	result := callTool(t, h, map[string]any{"sql": "BAD SQL"})
	if !result.IsError {
		t.Error("expected IsError = true")
	}
	text := resultText(t, result)
	if !strings.Contains(text, "parse_error") {
		t.Errorf("error text = %q, should contain parse_error", text)
	}
}

func TestToolSQLExecute_IdempotencyKey(t *testing.T) {
	fd := &fakeDoer{
		sqlResult: &SQLResult{Command: "INSERT", RowsAffected: 1},
	}
	h := handleSQLExecute(fd)
	callTool(t, h, map[string]any{
		"sql":             "INSERT INTO t(id) VALUES ($1)",
		"params":          []any{"x"},
		"idempotency_key": "my-key-123",
	})
	if fd.lastIdemp != "my-key-123" {
		t.Errorf("idempotency key = %q, want my-key-123", fd.lastIdemp)
	}
}

func TestToolRPCCall_CallsClient(t *testing.T) {
	fd := &fakeDoer{
		rpcResult: map[string]any{"id": "doc-1"},
	}
	h := handleRPCCall(fd)
	result := callTool(t, h, map[string]any{
		"procedure": "upsert_document",
		"args":      map[string]any{"id": "doc-1"},
	})

	if fd.rpcCalls != 1 {
		t.Errorf("rpcCalls = %d, want 1", fd.rpcCalls)
	}
	if fd.lastProcedure != "upsert_document" {
		t.Errorf("lastProcedure = %q, want upsert_document", fd.lastProcedure)
	}
	text := resultText(t, result)
	if !strings.Contains(text, "doc-1") {
		t.Errorf("result text = %q, should contain doc-1", text)
	}
}

func TestToolListTables_BuildsCorrectSQL(t *testing.T) {
	fd := &fakeDoer{
		sqlResult: &SQLResult{
			Command: "SELECT",
			Columns: []string{"table_name", "approx_rows"},
			Rows:    [][]any{{"notes", float64(42)}},
		},
	}
	h := handleListTables(fd)
	result := callTool(t, h, map[string]any{})

	if fd.sqlCalls != 1 {
		t.Errorf("sqlCalls = %d, want 1", fd.sqlCalls)
	}
	if !strings.Contains(fd.lastSQL, "information_schema.tables") {
		t.Errorf("SQL should query information_schema.tables, got: %s", fd.lastSQL)
	}
	if !strings.Contains(fd.lastSQL, "table_schema = 'public'") {
		t.Errorf("SQL should filter to public schema, got: %s", fd.lastSQL)
	}
	text := resultText(t, result)
	if !strings.Contains(text, "notes") {
		t.Errorf("result text = %q, should contain table name", text)
	}
}

func TestToolDescribeTable_ValidatesTableName(t *testing.T) {
	tests := []struct {
		name  string
		table string
	}{
		{"injection semicolon", "notes; DROP TABLE notes--"},
		{"injection quote", `notes"`},
		{"uppercase", "NOTES"},
		{"empty", ""},
		{"too long", strings.Repeat("a", 64)},
		{"special chars", "notes$table"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fd := &fakeDoer{}
			h := handleDescribeTable(fd)
			result := callToolExpectErr(t, h, map[string]any{"table": tc.table})
			if fd.sqlCalls != 0 {
				t.Error("SQL should not be called for unsafe identifiers")
			}
			_ = result // may be nil if protocol error
		})
	}
}

func TestToolDescribeTable_BuildsCorrectSQL(t *testing.T) {
	fd := &fakeDoer{
		sqlResult: &SQLResult{
			Command: "SELECT",
			Columns: []string{"column_name", "data_type", "is_nullable", "column_default"},
			Rows:    [][]any{{"id", "uuid", "NO", nil}, {"body", "text", "YES", nil}},
		},
	}
	h := handleDescribeTable(fd)
	result := callTool(t, h, map[string]any{"table": "notes"})

	if fd.sqlCalls != 1 {
		t.Errorf("sqlCalls = %d, want 1", fd.sqlCalls)
	}
	if !strings.Contains(fd.lastSQL, "information_schema.columns") {
		t.Errorf("SQL should query information_schema.columns, got: %s", fd.lastSQL)
	}
	if !strings.Contains(fd.lastSQL, "$1") {
		t.Errorf("SQL should use parameterized $1, got: %s", fd.lastSQL)
	}
	if len(fd.lastParams) != 1 || fd.lastParams[0] != "notes" {
		t.Errorf("params = %v, want [notes]", fd.lastParams)
	}
	text := resultText(t, result)
	if !strings.Contains(text, "uuid") {
		t.Errorf("result text = %q, should contain column type", text)
	}
}

func TestToolVectorSearch_BuildsCorrectSQL(t *testing.T) {
	fd := &fakeDoer{
		sqlResult: &SQLResult{
			Command: "SELECT",
			Columns: []string{"id", "distance"},
			Rows:    [][]any{{"doc-1", 0.1}},
		},
	}
	h := handleVectorSearch(fd)
	result := callTool(t, h, map[string]any{
		"table":           "documents",
		"vector_column":   "embedding",
		"query_embedding": []any{0.1, 0.2, 0.3},
		"k":               float64(5),
	})

	if fd.sqlCalls != 1 {
		t.Errorf("sqlCalls = %d, want 1", fd.sqlCalls)
	}
	if !strings.Contains(fd.lastSQL, "documents") {
		t.Errorf("SQL should reference table, got: %s", fd.lastSQL)
	}
	if !strings.Contains(fd.lastSQL, "<=>") {
		t.Errorf("SQL should use cosine distance operator, got: %s", fd.lastSQL)
	}
	text := resultText(t, result)
	if !strings.Contains(text, "doc-1") {
		t.Errorf("result text = %q, should contain result id", text)
	}
}

func TestToolVectorSearch_ValidatesIdentifiers(t *testing.T) {
	tests := []struct {
		name   string
		table  string
		column string
	}{
		{"bad table", "bad;table", "embedding"},
		{"bad column", "documents", "bad;col"},
		{"both bad", "A", "B"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fd := &fakeDoer{}
			h := handleVectorSearch(fd)
			callToolExpectErr(t, h, map[string]any{
				"table":           tc.table,
				"vector_column":   tc.column,
				"query_embedding": []any{0.1},
			})
			if fd.sqlCalls != 0 {
				t.Error("SQL should not be called for unsafe identifiers")
			}
		})
	}
}

func TestToolVectorUpsert_BuildsCorrectSQL(t *testing.T) {
	fd := &fakeDoer{
		sqlResult: &SQLResult{Command: "INSERT", RowsAffected: 2},
	}
	h := handleVectorUpsert(fd)

	rows := []any{
		map[string]any{"id": "doc-1", "metadata": map[string]any{}, "vector": []any{0.1, 0.2}},
		map[string]any{"id": "doc-2", "metadata": map[string]any{"k": "v"}, "vector": []any{0.3, 0.4}},
	}
	result := callTool(t, h, map[string]any{
		"table":         "documents",
		"id_column":     "id",
		"vector_column": "embedding",
		"rows":          rows,
	})

	if fd.sqlCalls != 1 {
		t.Errorf("sqlCalls = %d, want 1", fd.sqlCalls)
	}
	if !strings.Contains(fd.lastSQL, `INSERT INTO "documents"`) {
		t.Errorf("SQL should INSERT INTO quoted table, got: %s", fd.lastSQL)
	}
	if !strings.Contains(fd.lastSQL, "ON CONFLICT") {
		t.Errorf("SQL should have ON CONFLICT, got: %s", fd.lastSQL)
	}
	text := resultText(t, result)
	if !strings.Contains(text, "2") {
		t.Errorf("result text = %q, should contain rows affected", text)
	}
}

func TestToolWhoami_CallsMe(t *testing.T) {
	fd := &fakeDoer{
		meResult: &MeResult{
			WorkspaceID: "ws-1",
			Role:        "dbuser",
			Env:         "test",
		},
	}
	h := handleWhoami(fd)
	result := callTool(t, h, map[string]any{})
	text := resultText(t, result)
	if !strings.Contains(text, "dbuser") {
		t.Errorf("result text = %q, should contain role", text)
	}
}

func TestToolHealth_CallsBothEndpoints(t *testing.T) {
	fd := &fakeDoer{
		healthResult: &HealthResult{Healthy: true, Ready: true},
	}
	h := handleHealth(fd)
	result := callTool(t, h, map[string]any{})
	text := resultText(t, result)
	if !strings.Contains(text, "healthy") || !strings.Contains(text, "ready") {
		t.Errorf("result text = %q, should contain health status", text)
	}
}

func TestIsSafeIdent(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"notes", true},
		{"my_table_1", true},
		{"a", true},
		{"", false},
		{"UPPER", false},
		{"semi;colon", false},
		{`quo"te`, false},
		{"spa ce", false},
		{strings.Repeat("a", 63), true},
		{strings.Repeat("a", 64), false},
		{"$dollar", false},
		{"tab\tnewline", false},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			if got := isSafeIdent(tc.input); got != tc.want {
				t.Errorf("isSafeIdent(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestToolVectorSearch_KLimit(t *testing.T) {
	fd := &fakeDoer{}
	h := handleVectorSearch(fd)
	result := callToolExpectErr(t, h, map[string]any{
		"table":           "documents",
		"vector_column":   "embedding",
		"query_embedding": []any{0.1},
		"k":               float64(1001),
	})
	if fd.sqlCalls != 0 {
		t.Error("SQL should not be called when k > 1000")
	}
	if result != nil {
		text := resultText(t, result)
		if !strings.Contains(text, "1000") {
			t.Errorf("error should mention limit, got: %s", text)
		}
	}
}

func TestToolVectorUpsert_BatchLimit(t *testing.T) {
	fd := &fakeDoer{}
	h := handleVectorUpsert(fd)

	rows := make([]any, 101)
	for i := range rows {
		rows[i] = map[string]any{"id": fmt.Sprintf("d-%d", i), "metadata": map[string]any{}, "vector": []any{0.1}}
	}
	result := callToolExpectErr(t, h, map[string]any{
		"table":         "documents",
		"id_column":     "id",
		"vector_column": "embedding",
		"rows":          rows,
	})
	if fd.sqlCalls != 0 {
		t.Error("SQL should not be called when rows > 100")
	}
	_ = result
}

func TestToolVectorUpsert_MetadataValidation(t *testing.T) {
	fd := &fakeDoer{}
	h := handleVectorUpsert(fd)

	// Missing metadata
	result := callToolExpectErr(t, h, map[string]any{
		"table":         "documents",
		"id_column":     "id",
		"vector_column": "embedding",
		"rows": []any{
			map[string]any{"id": "doc-1", "vector": []any{0.1}},
		},
	})
	if fd.sqlCalls != 0 {
		t.Error("SQL should not be called for missing metadata")
	}
	if result != nil {
		text := resultText(t, result)
		if !strings.Contains(text, "metadata") {
			t.Errorf("error should mention metadata, got: %s", text)
		}
	}

	// Non-object metadata
	result2 := callToolExpectErr(t, h, map[string]any{
		"table":         "documents",
		"id_column":     "id",
		"vector_column": "embedding",
		"rows": []any{
			map[string]any{"id": "doc-1", "metadata": "not-an-object", "vector": []any{0.1}},
		},
	})
	if fd.sqlCalls != 0 {
		t.Error("SQL should not be called for non-object metadata")
	}
	if result2 != nil {
		text := resultText(t, result2)
		if !strings.Contains(text, "metadata") {
			t.Errorf("error should mention metadata, got: %s", text)
		}
	}
}

// formatVector is tested indirectly through vector tool tests,
// but we verify the output format explicitly here.
func TestFormatVector(t *testing.T) {
	v := []float64{1.0, 2.5, 3.0}
	got := formatVector(v)
	want := "[1,2.5,3]"
	if got != want {
		t.Errorf("formatVector = %q, want %q", got, want)
	}
}

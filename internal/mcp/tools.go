package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	gomcp "github.com/mark3labs/mcp-go/mcp"
)

// toolHandler is the function signature for MCP tool handlers.
type toolHandler = func(ctx context.Context, request gomcp.CallToolRequest) (*gomcp.CallToolResult, error)

// isSafeIdent validates a Postgres identifier. Same restriction as
// internal/vector.isSafeIdent and internal/tenant.isSafeRoleName:
// lowercase letters, digits, underscores, max 63 chars.
func isSafeIdent(s string) bool {
	if s == "" || len(s) > 63 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '_':
		default:
			return false
		}
	}
	return true
}

// formatVector formats a float slice as a pgvector literal: [1,2.5,3]
func formatVector(v []float64) string {
	var sb strings.Builder
	sb.WriteByte('[')
	for i, f := range v {
		if i > 0 {
			sb.WriteByte(',')
		}
		fmt.Fprintf(&sb, "%g", f)
	}
	sb.WriteByte(']')
	return sb.String()
}

func errResult(msg string) *gomcp.CallToolResult {
	return &gomcp.CallToolResult{
		Content: []gomcp.Content{gomcp.NewTextContent(msg)},
		IsError: true,
	}
}

func textResult(text string) *gomcp.CallToolResult {
	return &gomcp.CallToolResult{
		Content: []gomcp.Content{gomcp.NewTextContent(text)},
	}
}

func jsonResult(v any) *gomcp.CallToolResult {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return errResult(fmt.Sprintf("marshal result: %v", err))
	}
	return textResult(string(b))
}

// --- Tool handlers ---

func handleSQLExecute(d Doer) toolHandler {
	return func(ctx context.Context, req gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
		args := req.GetArguments()

		sql, _ := args["sql"].(string)
		if sql == "" {
			return errResult("required parameter 'sql' is missing or empty"), nil
		}

		var params []any
		if rawParams, ok := args["params"]; ok {
			if arr, ok := rawParams.([]any); ok {
				params = arr
			}
		}

		idempotencyKey, _ := args["idempotency_key"].(string)

		result, err := d.SQLExecute(ctx, sql, params, idempotencyKey)
		if err != nil {
			return errResult(err.Error()), nil
		}
		return jsonResult(result), nil
	}
}

func handleRPCCall(d Doer) toolHandler {
	return func(ctx context.Context, req gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
		args := req.GetArguments()

		procedure, _ := args["procedure"].(string)
		if procedure == "" {
			return errResult("required parameter 'procedure' is missing or empty"), nil
		}

		var rpcArgs map[string]any
		if raw, ok := args["args"]; ok {
			if m, ok := raw.(map[string]any); ok {
				rpcArgs = m
			}
		}

		result, err := d.RPCCall(ctx, procedure, rpcArgs)
		if err != nil {
			return errResult(err.Error()), nil
		}
		return jsonResult(result), nil
	}
}

func handleListTables(d Doer) toolHandler {
	const listTablesSQL = `SELECT t.table_name,
       COALESCE(s.n_live_tup, 0) AS approx_rows
FROM information_schema.tables t
LEFT JOIN pg_stat_user_tables s
  ON s.relname = t.table_name AND s.schemaname = 'public'
WHERE t.table_schema = 'public'
  AND t.table_type = 'BASE TABLE'
ORDER BY t.table_name`

	return func(ctx context.Context, _ gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
		result, err := d.SQLExecute(ctx, listTablesSQL, nil, "")
		if err != nil {
			return errResult(err.Error()), nil
		}
		return jsonResult(result), nil
	}
}

func handleDescribeTable(d Doer) toolHandler {
	const describeSQL = `SELECT column_name, data_type, is_nullable, column_default
FROM information_schema.columns
WHERE table_schema = 'public' AND table_name = $1
ORDER BY ordinal_position`

	return func(ctx context.Context, req gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
		args := req.GetArguments()
		table, _ := args["table"].(string)
		if !isSafeIdent(table) {
			return errResult(fmt.Sprintf("invalid table name: %q (must be lowercase alphanumeric/underscore, max 63 chars)", table)), nil
		}

		result, err := d.SQLExecute(ctx, describeSQL, []any{table}, "")
		if err != nil {
			return errResult(err.Error()), nil
		}
		return jsonResult(result), nil
	}
}

func handleVectorSearch(d Doer) toolHandler {
	return func(ctx context.Context, req gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
		args := req.GetArguments()

		table, _ := args["table"].(string)
		if !isSafeIdent(table) {
			return errResult(fmt.Sprintf("invalid table name: %q", table)), nil
		}
		vectorCol, _ := args["vector_column"].(string)
		if !isSafeIdent(vectorCol) {
			return errResult(fmt.Sprintf("invalid vector_column: %q", vectorCol)), nil
		}

		rawEmb, ok := args["query_embedding"]
		if !ok {
			return errResult("required parameter 'query_embedding' is missing"), nil
		}
		embArr, ok := rawEmb.([]any)
		if !ok || len(embArr) == 0 {
			return errResult("query_embedding must be a non-empty array of numbers"), nil
		}
		embedding := make([]float64, len(embArr))
		for i, v := range embArr {
			f, ok := v.(float64)
			if !ok {
				return errResult(fmt.Sprintf("query_embedding[%d] is not a number", i)), nil
			}
			embedding[i] = f
		}

		k := 5
		if kv, ok := args["k"].(float64); ok && kv > 0 {
			k = int(kv)
		}
		if k > 1000 {
			return errResult("k must be <= 1000"), nil
		}

		sql := fmt.Sprintf(
			`SELECT *, "%s" <=> $1::vector AS distance FROM "%s" ORDER BY "%s" <=> $1::vector LIMIT $2`,
			vectorCol, table, vectorCol,
		)
		result, err := d.SQLExecute(ctx, sql, []any{formatVector(embedding), k}, "")
		if err != nil {
			return errResult(err.Error()), nil
		}
		return jsonResult(result), nil
	}
}

func handleVectorUpsert(d Doer) toolHandler {
	return func(ctx context.Context, req gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
		args := req.GetArguments()

		table, _ := args["table"].(string)
		if !isSafeIdent(table) {
			return errResult(fmt.Sprintf("invalid table name: %q", table)), nil
		}
		idCol, _ := args["id_column"].(string)
		if !isSafeIdent(idCol) {
			return errResult(fmt.Sprintf("invalid id_column: %q", idCol)), nil
		}
		vectorCol, _ := args["vector_column"].(string)
		if !isSafeIdent(vectorCol) {
			return errResult(fmt.Sprintf("invalid vector_column: %q", vectorCol)), nil
		}

		rawRows, ok := args["rows"]
		if !ok {
			return errResult("required parameter 'rows' is missing"), nil
		}
		rows, ok := rawRows.([]any)
		if !ok || len(rows) == 0 {
			return errResult("rows must be a non-empty array"), nil
		}
		if len(rows) > 100 {
			return errResult("rows must contain at most 100 entries"), nil
		}

		// Build parameterized INSERT ... ON CONFLICT
		var valuesClauses []string
		var params []any
		for i, rawRow := range rows {
			row, ok := rawRow.(map[string]any)
			if !ok {
				return errResult(fmt.Sprintf("rows[%d] must be an object", i)), nil
			}
			id, _ := row["id"].(string)
			if id == "" {
				return errResult(fmt.Sprintf("rows[%d].id is required", i)), nil
			}
			rawMeta, hasMeta := row["metadata"]
			if !hasMeta {
				return errResult(fmt.Sprintf("rows[%d].metadata is required", i)), nil
			}
			meta, ok := rawMeta.(map[string]any)
			if !ok {
				return errResult(fmt.Sprintf("rows[%d].metadata must be a JSON object", i)), nil
			}
			metaJSON, err := json.Marshal(meta)
			if err != nil {
				return errResult(fmt.Sprintf("rows[%d].metadata: %v", i, err)), nil
			}
			rawVec, ok := row["vector"].([]any)
			if !ok || len(rawVec) == 0 {
				return errResult(fmt.Sprintf("rows[%d].vector must be a non-empty array", i)), nil
			}
			vec := make([]float64, len(rawVec))
			for j, v := range rawVec {
				f, ok := v.(float64)
				if !ok {
					return errResult(fmt.Sprintf("rows[%d].vector[%d] is not a number", i, j)), nil
				}
				vec[j] = f
			}

			base := i * 3
			valuesClauses = append(valuesClauses, fmt.Sprintf("($%d, $%d::jsonb, $%d::vector)", base+1, base+2, base+3))
			params = append(params, id, string(metaJSON), formatVector(vec))
		}

		sql := fmt.Sprintf(
			`INSERT INTO "%s" ("%s", "metadata", "%s") VALUES %s ON CONFLICT ("%s") DO UPDATE SET "metadata" = EXCLUDED."metadata", "%s" = EXCLUDED."%s"`,
			table, idCol, vectorCol,
			strings.Join(valuesClauses, ", "),
			idCol, vectorCol, vectorCol,
		)

		result, err := d.SQLExecute(ctx, sql, params, "")
		if err != nil {
			return errResult(err.Error()), nil
		}
		return jsonResult(map[string]any{
			"rows_affected": result.RowsAffected,
			"command":       result.Command,
		}), nil
	}
}

func handleWhoami(d Doer) toolHandler {
	return func(ctx context.Context, _ gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
		me, err := d.Me(ctx)
		if err != nil {
			return errResult(err.Error()), nil
		}
		return jsonResult(me), nil
	}
}

func handleHealth(d Doer) toolHandler {
	return func(ctx context.Context, _ gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
		h, err := d.Health(ctx)
		if err != nil {
			return errResult(err.Error()), nil
		}
		return jsonResult(map[string]any{
			"healthy": h.Healthy,
			"ready":   h.Ready,
			"detail":  h.Detail,
		}), nil
	}
}

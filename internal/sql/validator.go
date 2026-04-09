package sql

import (
	"fmt"
	"strconv"

	pg_query "github.com/pganalyze/pg_query_go/v5"
)

// ValidatorConfig configures the small set of static checks the validator runs.
type ValidatorConfig struct {
	MaxStatementBytes  int
	MaxStatementParams int
}

// Validator runs the parser-backed checks. It is goroutine-safe.
type Validator struct {
	cfg ValidatorConfig
}

// NewValidator returns a Validator with sane defaults if cfg is zero.
func NewValidator(cfg ValidatorConfig) *Validator {
	if cfg.MaxStatementBytes <= 0 {
		cfg.MaxStatementBytes = 256 * 1024
	}
	if cfg.MaxStatementParams <= 0 {
		cfg.MaxStatementParams = 1000
	}
	return &Validator{cfg: cfg}
}

// ValidationError is the typed error returned by Validate. The Code is
// surfaced through the HTTP layer as the RFC7807 `title`.
type ValidationError struct {
	Code    string
	Message string
}

func (e *ValidationError) Error() string { return e.Code + ": " + e.Message }

// Result is the parsed-and-classified output of Validate.
type Result struct {
	// Command is the top-level statement tag (e.g. "SELECT", "CREATE TABLE",
	// "INSERT") — used for the audit row, the prometheus label, and the
	// SDK response payload.
	Command string

	// IsSelect is true for a bare SELECT (used to decide whether the
	// executor should auto-wrap with LIMIT). It is false for SELECT inside
	// a CTE that performs a write, for VALUES, EXPLAIN, etc.
	IsSelect bool

	// PlaceholderCount is the number of distinct $N placeholders found in
	// the AST. Equal to len(params) by construction.
	PlaceholderCount int
}

// Validate runs every static check on (sql, params).
func (v *Validator) Validate(sqlText string, params []any) (*Result, error) {
	if sqlText == "" {
		return nil, &ValidationError{Code: "empty_statement", Message: "sql is empty"}
	}
	if len(sqlText) > v.cfg.MaxStatementBytes {
		return nil, &ValidationError{
			Code:    "statement_too_large",
			Message: fmt.Sprintf("statement is %d bytes (max %d)", len(sqlText), v.cfg.MaxStatementBytes),
		}
	}
	if len(params) > v.cfg.MaxStatementParams {
		return nil, &ValidationError{
			Code:    "too_many_params",
			Message: fmt.Sprintf("got %d params (max %d)", len(params), v.cfg.MaxStatementParams),
		}
	}

	tree, err := pg_query.Parse(sqlText)
	if err != nil {
		return nil, &ValidationError{Code: "parse_error", Message: err.Error()}
	}
	if tree == nil || len(tree.Stmts) == 0 {
		return nil, &ValidationError{Code: "empty_statement", Message: "no statement found"}
	}
	if len(tree.Stmts) != 1 {
		return nil, &ValidationError{
			Code:    "multiple_statements",
			Message: fmt.Sprintf("expected 1 statement, got %d (use one POST per statement)", len(tree.Stmts)),
		}
	}

	stmt := tree.Stmts[0].Stmt
	cmd := classify(stmt)
	isSelect := cmd == "SELECT"

	placeholders, err := countPlaceholders(sqlText)
	if err != nil {
		return nil, &ValidationError{Code: "parse_error", Message: err.Error()}
	}
	if placeholders != len(params) {
		return nil, &ValidationError{
			Code:    "params_mismatch",
			Message: fmt.Sprintf("statement has %d $N placeholders but %d params were supplied", placeholders, len(params)),
		}
	}

	return &Result{
		Command:          cmd,
		IsSelect:         isSelect,
		PlaceholderCount: placeholders,
	}, nil
}

// classify maps a parsed top-level node to a command tag.
//
// pg_query exposes Stmts via a oneof; we only need a stable string for the
// audit row + metrics label, so we walk the proto with a switch over the
// generated Node types.
func classify(node *pg_query.Node) string {
	if node == nil {
		return "UNKNOWN"
	}
	switch n := node.Node.(type) {
	case *pg_query.Node_SelectStmt:
		return "SELECT"
	case *pg_query.Node_InsertStmt:
		return "INSERT"
	case *pg_query.Node_UpdateStmt:
		return "UPDATE"
	case *pg_query.Node_DeleteStmt:
		return "DELETE"
	case *pg_query.Node_CreateStmt:
		return "CREATE TABLE"
	case *pg_query.Node_CreateRoleStmt:
		return "CREATE ROLE"
	case *pg_query.Node_CreatedbStmt:
		return "CREATE DATABASE"
	case *pg_query.Node_DropStmt:
		return "DROP"
	case *pg_query.Node_AlterTableStmt:
		return "ALTER TABLE"
	case *pg_query.Node_AlterRoleStmt:
		return "ALTER ROLE"
	case *pg_query.Node_GrantStmt:
		return "GRANT"
	case *pg_query.Node_GrantRoleStmt:
		return "GRANT ROLE"
	case *pg_query.Node_VacuumStmt:
		return "VACUUM"
	case *pg_query.Node_ExplainStmt:
		return "EXPLAIN"
	case *pg_query.Node_TruncateStmt:
		return "TRUNCATE"
	case *pg_query.Node_CopyStmt:
		return "COPY"
	case *pg_query.Node_TransactionStmt:
		return "TRANSACTION"
	case *pg_query.Node_IndexStmt:
		return "CREATE INDEX"
	case *pg_query.Node_ViewStmt:
		return "CREATE VIEW"
	case *pg_query.Node_CreateExtensionStmt:
		return "CREATE EXTENSION"
	case *pg_query.Node_RenameStmt:
		return "RENAME"
	case *pg_query.Node_VariableSetStmt:
		return "SET"
	case *pg_query.Node_VariableShowStmt:
		return "SHOW"
	default:
		_ = n
		return "OTHER"
	}
}

// countPlaceholders returns the largest $N referenced in sqlText.
//
// We use pg_query.Scan to tokenise the SQL with the real PostgreSQL
// scanner. This is the only way to distinguish a `$1` placeholder from a
// `$1` substring inside a string literal (`'$1 off'`) or a dollar-quoted
// block (`$tag$ ... $1 ... $tag$`). Earlier versions of this function
// used pg_query.Normalize, which REPLACED literal values with new
// placeholders and so falsely inflated the count for any query that
// contained a literal — making practically every real-world request fail
// validation with `params_mismatch`.
//
// We do not look at the token's enum value, only its text: anything that
// the scanner emits as a single token starting with `$` followed by ASCII
// digits is a placeholder. Dollar-quoted strings are emitted as a single
// SCONST-shaped token whose text starts with `$<tag>$` (not all-digits
// after the `$`), so they are skipped automatically.
func countPlaceholders(sqlText string) (int, error) {
	result, err := pg_query.Scan(sqlText)
	if err != nil {
		return 0, err
	}
	max := 0
	for _, tok := range result.Tokens {
		start, end := int(tok.Start), int(tok.End)
		if start < 0 || end > len(sqlText) || start >= end {
			continue
		}
		text := sqlText[start:end]
		if len(text) < 2 || text[0] != '$' {
			continue
		}
		n, err := strconv.Atoi(text[1:])
		if err != nil {
			continue // not a $N placeholder (likely $tag$ or $$)
		}
		if n > max {
			max = n
		}
	}
	return max, nil
}

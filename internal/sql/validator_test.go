package sql

import (
	"strings"
	"testing"
)

func TestNewValidator_AppliesDefaults(t *testing.T) {
	v := NewValidator(ValidatorConfig{})
	if v.cfg.MaxStatementBytes != 256*1024 {
		t.Errorf("MaxStatementBytes default: got %d, want %d", v.cfg.MaxStatementBytes, 256*1024)
	}
	if v.cfg.MaxStatementParams != 1000 {
		t.Errorf("MaxStatementParams default: got %d, want %d", v.cfg.MaxStatementParams, 1000)
	}
}

func TestValidate_Empty(t *testing.T) {
	v := NewValidator(ValidatorConfig{})
	if _, err := v.Validate("", nil); err == nil {
		t.Fatal("Validate(\"\"): expected error, got nil")
	} else if ve, ok := err.(*ValidationError); !ok || ve.Code != "empty_statement" {
		t.Errorf("Validate(\"\"): expected empty_statement, got %v", err)
	}
}

func TestValidate_TooLarge(t *testing.T) {
	v := NewValidator(ValidatorConfig{MaxStatementBytes: 32})
	huge := "SELECT '" + strings.Repeat("x", 64) + "'"
	_, err := v.Validate(huge, nil)
	ve, ok := err.(*ValidationError)
	if !ok || ve.Code != "statement_too_large" {
		t.Errorf("Validate(huge): expected statement_too_large, got %v", err)
	}
}

func TestValidate_TooManyParams(t *testing.T) {
	v := NewValidator(ValidatorConfig{MaxStatementParams: 2})
	_, err := v.Validate("SELECT 1", []any{1, 2, 3})
	ve, ok := err.(*ValidationError)
	if !ok || ve.Code != "too_many_params" {
		t.Errorf("Validate(too many params): expected too_many_params, got %v", err)
	}
}

func TestValidate_ParseError(t *testing.T) {
	v := NewValidator(ValidatorConfig{})
	_, err := v.Validate("SELEC FROM nope where", nil)
	ve, ok := err.(*ValidationError)
	if !ok || ve.Code != "parse_error" {
		t.Errorf("Validate(garbage): expected parse_error, got %v", err)
	}
}

func TestValidate_RejectsMultipleStatements(t *testing.T) {
	v := NewValidator(ValidatorConfig{})
	_, err := v.Validate("SELECT 1; SELECT 2", nil)
	ve, ok := err.(*ValidationError)
	if !ok || ve.Code != "multiple_statements" {
		t.Errorf("Validate(multi): expected multiple_statements, got %v", err)
	}
}

func TestValidate_PlaceholderMismatch(t *testing.T) {
	v := NewValidator(ValidatorConfig{})

	t.Run("more placeholders than params", func(t *testing.T) {
		_, err := v.Validate("SELECT $1, $2, $3", []any{"only-one"})
		ve, ok := err.(*ValidationError)
		if !ok || ve.Code != "params_mismatch" {
			t.Errorf("expected params_mismatch, got %v", err)
		}
	})

	t.Run("more params than placeholders", func(t *testing.T) {
		_, err := v.Validate("SELECT 1", []any{"unused"})
		ve, ok := err.(*ValidationError)
		if !ok || ve.Code != "params_mismatch" {
			t.Errorf("expected params_mismatch, got %v", err)
		}
	})
}

// TestValidate_LiteralNotMistakenForPlaceholder is the regression
// guard for the bug called out in countPlaceholders' doc comment:
// pg_query.Normalize used to inflate the placeholder count by
// rewriting literals into new placeholders. The Scan-based
// implementation must NOT count `$1` inside a string literal.
func TestValidate_LiteralNotMistakenForPlaceholder(t *testing.T) {
	v := NewValidator(ValidatorConfig{})

	cases := []struct {
		name   string
		sql    string
		params []any
	}{
		{"placeholder substring in single-quoted literal", "SELECT '$1 off'", nil},
		{"placeholder substring in dollar-quoted literal", "SELECT $tag$ price is $1 today $tag$", nil},
		{"mixed: real placeholder + literal that looks like one", "SELECT '$2 off' WHERE id = $1", []any{42}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := v.Validate(tc.sql, tc.params)
			if err != nil {
				t.Fatalf("Validate(%q): %v", tc.sql, err)
			}
			if res.PlaceholderCount != len(tc.params) {
				t.Errorf("PlaceholderCount: got %d, want %d (sql=%q)", res.PlaceholderCount, len(tc.params), tc.sql)
			}
		})
	}
}

func TestValidate_HappyPath(t *testing.T) {
	v := NewValidator(ValidatorConfig{})

	cases := []struct {
		name       string
		sql        string
		params     []any
		wantCmd    string
		wantSelect bool
		wantPlaceN int
	}{
		{"bare select", "SELECT 1", nil, "SELECT", true, 0},
		{"select with placeholder", "SELECT $1", []any{42}, "SELECT", true, 1},
		{"insert", "INSERT INTO t (a) VALUES ($1)", []any{42}, "INSERT", false, 1},
		{"update", "UPDATE t SET a=$1 WHERE id=$2", []any{1, 2}, "UPDATE", false, 2},
		{"delete", "DELETE FROM t WHERE id=$1", []any{1}, "DELETE", false, 1},
		{"create table", "CREATE TABLE t (id int)", nil, "CREATE TABLE", false, 0},
		{"drop", "DROP TABLE t", nil, "DROP", false, 0},
		{"alter table", "ALTER TABLE t ADD COLUMN x int", nil, "ALTER TABLE", false, 0},
		{"grant", "GRANT SELECT ON t TO dbuser", nil, "GRANT", false, 0},
		{"create index", "CREATE INDEX idx ON t (a)", nil, "CREATE INDEX", false, 0},
		{"explain", "EXPLAIN SELECT 1", nil, "EXPLAIN", false, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := v.Validate(tc.sql, tc.params)
			if err != nil {
				t.Fatalf("Validate(%q): %v", tc.sql, err)
			}
			if res.Command != tc.wantCmd {
				t.Errorf("Command: got %q, want %q", res.Command, tc.wantCmd)
			}
			if res.IsSelect != tc.wantSelect {
				t.Errorf("IsSelect: got %v, want %v", res.IsSelect, tc.wantSelect)
			}
			if res.PlaceholderCount != tc.wantPlaceN {
				t.Errorf("PlaceholderCount: got %d, want %d", res.PlaceholderCount, tc.wantPlaceN)
			}
		})
	}
}

func TestValidate_ClassifyAdditionalStatements(t *testing.T) {
	v := NewValidator(ValidatorConfig{})

	cases := []struct {
		name       string
		sql        string
		params     []any
		wantCmd    string
		wantSelect bool
		wantPlaceN int
	}{
		{"create role", "CREATE ROLE testrole", nil, "CREATE ROLE", false, 0},
		{"alter role", "ALTER ROLE dbuser WITH LOGIN", nil, "ALTER ROLE", false, 0},
		{"grant role", "GRANT dbuser TO someone", nil, "GRANT ROLE", false, 0},
		{"vacuum", "VACUUM", nil, "VACUUM", false, 0},
		{"truncate", "TRUNCATE t", nil, "TRUNCATE", false, 0},
		{"copy to stdout", "COPY t TO STDOUT", nil, "COPY", false, 0},
		{"begin", "BEGIN", nil, "TRANSACTION", false, 0},
		{"commit", "COMMIT", nil, "TRANSACTION", false, 0},
		{"create view", "CREATE VIEW v AS SELECT 1", nil, "CREATE VIEW", false, 0},
		{"create extension", "CREATE EXTENSION IF NOT EXISTS pgcrypto", nil, "CREATE EXTENSION", false, 0},
		{"alter table rename", "ALTER TABLE t RENAME TO t2", nil, "RENAME", false, 0},
		{"set", "SET statement_timeout = '5s'", nil, "SET", false, 0},
		{"show", "SHOW statement_timeout", nil, "SHOW", false, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := v.Validate(tc.sql, tc.params)
			if err != nil {
				t.Fatalf("Validate(%q): %v", tc.sql, err)
			}
			if res.Command != tc.wantCmd {
				t.Errorf("Command: got %q, want %q", res.Command, tc.wantCmd)
			}
			if res.IsSelect != tc.wantSelect {
				t.Errorf("IsSelect: got %v, want %v", res.IsSelect, tc.wantSelect)
			}
			if res.PlaceholderCount != tc.wantPlaceN {
				t.Errorf("PlaceholderCount: got %d, want %d", res.PlaceholderCount, tc.wantPlaceN)
			}
		})
	}
}

func TestValidate_HasReturning(t *testing.T) {
	v := NewValidator(ValidatorConfig{})

	cases := []struct {
		name          string
		sql           string
		params        []any
		wantCmd       string
		wantReturning bool
	}{
		{"insert returning", "INSERT INTO t (a) VALUES ($1) RETURNING id", []any{1}, "INSERT", true},
		{"insert returning star", "INSERT INTO t (a) VALUES ($1) RETURNING *", []any{1}, "INSERT", true},
		{"insert no returning", "INSERT INTO t (a) VALUES ($1)", []any{1}, "INSERT", false},
		{"update returning", "UPDATE t SET a=$1 WHERE id=$2 RETURNING a", []any{1, 2}, "UPDATE", true},
		{"update no returning", "UPDATE t SET a=$1 WHERE id=$2", []any{1, 2}, "UPDATE", false},
		{"delete returning", "DELETE FROM t WHERE id=$1 RETURNING *", []any{1}, "DELETE", true},
		{"delete no returning", "DELETE FROM t WHERE id=$1", []any{1}, "DELETE", false},
		{"select is not returning", "SELECT 1", nil, "SELECT", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := v.Validate(tc.sql, tc.params)
			if err != nil {
				t.Fatalf("Validate(%q): %v", tc.sql, err)
			}
			if res.Command != tc.wantCmd {
				t.Errorf("Command: got %q, want %q", res.Command, tc.wantCmd)
			}
			if res.HasReturning != tc.wantReturning {
				t.Errorf("HasReturning: got %v, want %v", res.HasReturning, tc.wantReturning)
			}
		})
	}
}

func TestValidationError_Error(t *testing.T) {
	e := &ValidationError{Code: "parse_error", Message: "syntax error"}
	want := "parse_error: syntax error"
	if got := e.Error(); got != want {
		t.Errorf("Error(): got %q, want %q", got, want)
	}
}

package httpapi_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/fheinfling/ai-coop-db/internal/httpapi"
	sqlpkg "github.com/fheinfling/ai-coop-db/internal/sql"
)

func TestMapError_Nil(t *testing.T) {
	p := httpapi.MapError(nil)
	if p.Status != http.StatusOK || p.Title != "ok" {
		t.Errorf("nil error: got {%d %s}, want {200 ok}", p.Status, p.Title)
	}
}

func TestMapError_ValidationError(t *testing.T) {
	err := &sqlpkg.ValidationError{Code: "params_mismatch", Message: "got 2 params, want 1"}
	p := httpapi.MapError(err)
	if p.Status != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", p.Status, http.StatusBadRequest)
	}
	if p.Title != "params_mismatch" {
		t.Errorf("title: got %q, want %q", p.Title, "params_mismatch")
	}
	if p.Detail != "got 2 params, want 1" {
		t.Errorf("detail: got %q", p.Detail)
	}
}

func TestMapError_SqlErrorWrapped(t *testing.T) {
	pg := &pgconn.PgError{Code: "42501", Message: "permission denied for table foo"}
	wrapped := &sqlpkg.Error{Pg: pg}
	p := httpapi.MapError(wrapped)
	if p.Status != http.StatusForbidden {
		t.Errorf("status: got %d, want %d", p.Status, http.StatusForbidden)
	}
	if p.Title != "permission_denied" {
		t.Errorf("title: got %q", p.Title)
	}
	if p.SQLState != "42501" {
		t.Errorf("sqlstate: got %q", p.SQLState)
	}
}

func TestMapError_PlainError(t *testing.T) {
	p := httpapi.MapError(errors.New("something broke"))
	if p.Status != http.StatusInternalServerError {
		t.Errorf("status: got %d, want 500", p.Status)
	}
	if p.Title != "internal" {
		t.Errorf("title: got %q, want internal", p.Title)
	}
}

// Table-driven tests for every SQLSTATE branch in mapPgError.
func TestMapPgError(t *testing.T) {
	cases := []struct {
		code       string
		wantStatus int
		wantTitle  string
	}{
		{"42501", http.StatusForbidden, "permission_denied"},
		{"42P01", http.StatusBadRequest, "undefined_object"},
		{"42883", http.StatusBadRequest, "undefined_object"},
		{"23505", http.StatusConflict, "unique_violation"},
		{"23502", http.StatusConflict, "integrity_violation"},
		{"23503", http.StatusConflict, "integrity_violation"},
		{"23514", http.StatusConflict, "integrity_violation"},
		{"22001", http.StatusBadRequest, "invalid_input"},
		{"22003", http.StatusBadRequest, "invalid_input"},
		{"22P02", http.StatusBadRequest, "invalid_input"},
		{"22008", http.StatusBadRequest, "invalid_input"},
		{"57014", http.StatusRequestTimeout, "statement_timeout"},
		{"53300", http.StatusServiceUnavailable, "too_many_connections"},
		{"40001", http.StatusConflict, "serialization_failure"},
		{"40P01", http.StatusConflict, "deadlock_detected"},
		// Unknown code falls back to 500 database_error and preserves the code.
		{"XX000", http.StatusInternalServerError, "database_error"},
	}
	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			pgErr := &pgconn.PgError{Code: tc.code, Message: "test message"}
			p := httpapi.MapError(pgErr)
			if p.Status != tc.wantStatus {
				t.Errorf("status: got %d, want %d", p.Status, tc.wantStatus)
			}
			if p.Title != tc.wantTitle {
				t.Errorf("title: got %q, want %q", p.Title, tc.wantTitle)
			}
			if p.SQLState != tc.code {
				t.Errorf("sqlstate: got %q, want %q", p.SQLState, tc.code)
			}
		})
	}
}

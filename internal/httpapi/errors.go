package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5/pgconn"

	sqlpkg "github.com/fheinfling/agentic-coop-db/internal/sql"
)

// Problem is the RFC7807 problem-details payload.
type Problem struct {
	Type     string `json:"type,omitempty"`
	Title    string `json:"title"`
	Status   int    `json:"status"`
	Detail   string `json:"detail,omitempty"`
	Instance string `json:"instance,omitempty"`
	SQLState string `json:"sqlstate,omitempty"`
}

// WriteProblem writes a Problem as application/problem+json.
func WriteProblem(w http.ResponseWriter, p Problem) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(p.Status)
	_ = json.NewEncoder(w).Encode(p)
}

// MapError converts an error from the validator/executor into the right
// Problem. Validation errors map to 400; pg permission errors to 403;
// integrity violations to 409; statement-timeouts to 408; everything else
// to 500.
func MapError(err error) Problem {
	if err == nil {
		return Problem{Title: "ok", Status: 200}
	}

	var ve *sqlpkg.ValidationError
	if errors.As(err, &ve) {
		return Problem{Title: ve.Code, Status: http.StatusBadRequest, Detail: ve.Message}
	}
	var sqlErr *sqlpkg.Error
	if errors.As(err, &sqlErr) && sqlErr.Pg != nil {
		return mapPgError(sqlErr.Pg)
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return mapPgError(pgErr)
	}
	return Problem{Title: "internal", Status: http.StatusInternalServerError, Detail: err.Error()}
}

func mapPgError(pg *pgconn.PgError) Problem {
	status := http.StatusInternalServerError
	title := "database_error"
	switch pg.Code {
	case "42501":
		status, title = http.StatusForbidden, "permission_denied"
	case "42P01", "42883":
		status, title = http.StatusBadRequest, "undefined_object"
	case "23505":
		status, title = http.StatusConflict, "unique_violation"
	case "23502", "23503", "23514":
		status, title = http.StatusConflict, "integrity_violation"
	case "22001", "22003", "22P02", "22008":
		status, title = http.StatusBadRequest, "invalid_input"
	case "57014":
		status, title = http.StatusRequestTimeout, "statement_timeout"
	case "53300":
		status, title = http.StatusServiceUnavailable, "too_many_connections"
	case "40001":
		status, title = http.StatusConflict, "serialization_failure"
	case "40P01":
		status, title = http.StatusConflict, "deadlock_detected"
	}
	return Problem{
		Title:    title,
		Status:   status,
		Detail:   pg.Message,
		SQLState: pg.Code,
	}
}

package vector

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Row is a single embedding row.
type Row struct {
	ID       string
	Metadata map[string]any
	Vector   []float32
}

// Format formats a []float32 in the textual form pgvector accepts.
func Format(v []float32) string {
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

// EnsureIVFFlatIndex creates an IVFFlat index over the embedding column if
// the table has at least minRows rows. Below that threshold sequential
// scan is cheaper. Returns true if the index was created on this call.
//
// Caller is expected to hold a transaction in which the gateway role
// already has CREATE INDEX permission on the table.
func EnsureIVFFlatIndex(ctx context.Context, tx pgx.Tx, table, column string, minRows, lists int) (bool, error) {
	if !isSafeIdent(table) || !isSafeIdent(column) {
		return false, errors.New("vector.EnsureIVFFlatIndex: unsafe identifier")
	}
	indexName := fmt.Sprintf("%s_%s_ivfflat_idx", table, column)

	var exists bool
	if err := tx.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM pg_indexes WHERE indexname = $1)`,
		indexName,
	).Scan(&exists); err != nil {
		return false, err
	}
	if exists {
		return false, nil
	}

	var rowCount int
	if err := tx.QueryRow(ctx,
		fmt.Sprintf(`SELECT count(*) FROM %q`, table),
	).Scan(&rowCount); err != nil {
		return false, err
	}
	if rowCount < minRows {
		return false, nil
	}

	if lists <= 0 {
		// pgvector recommends sqrt(n) lists; clamp to a sensible range.
		lists = 100
		if rowCount > 1_000_000 {
			lists = 1000
		}
	}
	stmt := fmt.Sprintf(
		`CREATE INDEX %q ON %q USING ivfflat (%q vector_cosine_ops) WITH (lists = %d)`,
		indexName, table, column, lists,
	)
	if _, err := tx.Exec(ctx, stmt); err != nil {
		return false, err
	}
	return true, nil
}

// Search runs a top-k cosine similarity search. The caller is responsible
// for the actual table schema; this is just the boilerplate.
func Search(ctx context.Context, tx pgx.Tx, table, column string, query []float32, k int) ([]map[string]any, error) {
	if !isSafeIdent(table) || !isSafeIdent(column) {
		return nil, errors.New("vector.Search: unsafe identifier")
	}
	if k <= 0 {
		k = 5
	}
	stmt := fmt.Sprintf(
		`SELECT id, metadata, %q <=> $1::vector AS distance
         FROM %q
         ORDER BY %q <=> $1::vector
         LIMIT $2`,
		column, table, column,
	)
	rows, err := tx.Query(ctx, stmt, Format(query), k)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []map[string]any
	for rows.Next() {
		var (
			id       string
			metadata map[string]any
			distance float64
		)
		if err := rows.Scan(&id, &metadata, &distance); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{
			"id":       id,
			"metadata": metadata,
			"distance": distance,
		})
	}
	return out, rows.Err()
}

// isSafeIdent guards the dynamic-SQL helpers above. Same restriction as
// internal/tenant.isSafeRoleName: lowercase letters, digits, underscores.
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

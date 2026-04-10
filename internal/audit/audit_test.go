package audit

import (
	"context"
	"encoding/hex"
	"log/slog"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestHashOrEmpty(t *testing.T) {
	cases := []struct {
		name  string
		input string
		check func(t *testing.T, got string)
	}{
		{
			name:  "empty string returns empty",
			input: "",
			check: func(t *testing.T, got string) {
				t.Helper()
				if got != "" {
					t.Errorf("hashOrEmpty(%q) = %q, want empty", "", got)
				}
			},
		},
		{
			name:  "hello returns 64-char hex",
			input: "hello",
			check: func(t *testing.T, got string) {
				t.Helper()
				if len(got) != 64 {
					t.Errorf("hashOrEmpty(\"hello\") length = %d, want 64", len(got))
				}
			},
		},
		{
			name:  "deterministic output",
			input: "hello",
			check: func(t *testing.T, got string) {
				t.Helper()
				again := hashOrEmpty("hello")
				if got != again {
					t.Errorf("hashOrEmpty not deterministic: %q != %q", got, again)
				}
			},
		},
		{
			name:  "different inputs produce different hashes",
			input: "hello",
			check: func(t *testing.T, got string) {
				t.Helper()
				other := hashOrEmpty("world")
				if got == other {
					t.Error("hashOrEmpty(\"hello\") == hashOrEmpty(\"world\"), want different")
				}
			},
		},
		{
			name:  "result is valid hex",
			input: "hello",
			check: func(t *testing.T, got string) {
				t.Helper()
				if _, err := hex.DecodeString(got); err != nil {
					t.Errorf("hashOrEmpty(\"hello\") is not valid hex: %v", err)
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := hashOrEmpty(tc.input)
			tc.check(t, got)
		})
	}
}

func TestParamsHash(t *testing.T) {
	cases := []struct {
		name  string
		input []any
		check func(t *testing.T, got string)
	}{
		{
			name:  "nil returns empty",
			input: nil,
			check: func(t *testing.T, got string) {
				t.Helper()
				if got != "" {
					t.Errorf("paramsHash(nil) = %q, want empty", got)
				}
			},
		},
		{
			name:  "empty slice returns empty",
			input: []any{},
			check: func(t *testing.T, got string) {
				t.Helper()
				if got != "" {
					t.Errorf("paramsHash([]any{}) = %q, want empty", got)
				}
			},
		},
		{
			name:  "non-empty params returns 64-char hex",
			input: []any{1, "two", 3.0},
			check: func(t *testing.T, got string) {
				t.Helper()
				if len(got) != 64 {
					t.Errorf("paramsHash length = %d, want 64", len(got))
				}
			},
		},
		{
			name:  "deterministic output",
			input: []any{1, "two", 3.0},
			check: func(t *testing.T, got string) {
				t.Helper()
				again := paramsHash([]any{1, "two", 3.0})
				if got != again {
					t.Errorf("paramsHash not deterministic: %q != %q", got, again)
				}
			},
		},
		{
			name:  "order matters",
			input: []any{1, 2},
			check: func(t *testing.T, got string) {
				t.Helper()
				other := paramsHash([]any{2, 1})
				if got == other {
					t.Error("paramsHash([1,2]) == paramsHash([2,1]), want different")
				}
			},
		},
		{
			name:  "result is valid hex",
			input: []any{1, "two", 3.0},
			check: func(t *testing.T, got string) {
				t.Helper()
				if _, err := hex.DecodeString(got); err != nil {
					t.Errorf("paramsHash result is not valid hex: %v", err)
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := paramsHash(tc.input)
			tc.check(t, got)
		})
	}
}

func TestNewWriter_NilLogger(t *testing.T) {
	w := NewWriter(nil, nil, false, false)
	if w == nil {
		t.Fatal("NewWriter(nil, nil, false, false) returned nil")
	}
}

func TestNewWriter_IncludeSQL(t *testing.T) {
	w := NewWriter(nil, nil, false, true)
	if !w.includeSQL {
		t.Error("NewWriter(nil, nil, false, true).includeSQL = false, want true")
	}
}

func TestNewWriter_Disabled(t *testing.T) {
	w := NewWriter(nil, nil, true, false)
	if !w.disabled {
		t.Error("NewWriter(nil, nil, true, false).disabled = false, want true")
	}
}

// panicPool implements dbPool and panics if Exec is ever called, making it
// observable when the disabled short-circuit is bypassed in tests.
type panicPool struct{}

func (panicPool) Exec(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
	panic("audit: Exec called on a disabled Writer — disabled short-circuit is broken")
}

func TestWrite_Disabled(t *testing.T) {
	// Use a non-nil pool that panics on Exec so the test fails if the disabled
	// flag does not short-circuit before the DB insert.
	w := &Writer{pool: panicPool{}, disabled: true, logger: slog.Default()}
	w.Write(context.Background(), Entry{
		RequestID:  "req-disabled",
		Endpoint:   "/query",
		Command:    "SELECT",
		SQL:        "SELECT 1",
		DurationMS: 5,
		StatusCode: 200,
	})
}

func TestWrite_NilPool(t *testing.T) {
	w := NewWriter(nil, nil, false, false)
	// Should not panic; Write logs and returns early when pool is nil.
	w.Write(context.Background(), Entry{
		RequestID:   "req-1",
		WorkspaceID: "ws-1",
		KeyDBID:     "key-1",
		Endpoint:    "/query",
		Command:     "SELECT",
		SQL:         "SELECT 1",
		Params:      []any{42},
		DurationMS:  10,
		StatusCode:  200,
		ClientIP:    "127.0.0.1",
	})
}

func TestWrite_NilPool_VariousInputs(t *testing.T) {
	w := NewWriter(nil, nil, false, false)

	cases := []struct {
		name  string
		entry Entry
	}{
		{
			name:  "empty fields",
			entry: Entry{},
		},
		{
			name: "valid UUIDs and IP",
			entry: Entry{
				WorkspaceID: "550e8400-e29b-41d4-a716-446655440000",
				KeyDBID:     "6ba7b810-9dad-11d1-80b4-00c04fd430c8",
				ClientIP:    "1.2.3.4",
			},
		},
		{
			name: "invalid UUID and IP",
			entry: Entry{
				WorkspaceID: "not-a-uuid",
				KeyDBID:     "not-a-uuid",
				ClientIP:    "not-an-ip",
			},
		},
		{
			name: "IPv6 address",
			entry: Entry{
				ClientIP: "::1",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Should not panic for any input.
			w.Write(context.Background(), tc.entry)
		})
	}
}

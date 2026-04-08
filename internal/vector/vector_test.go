package vector

import (
	"strings"
	"testing"
)

func TestFormat_Empty(t *testing.T) {
	got := Format(nil)
	if got != "[]" {
		t.Errorf("Format(nil) = %q, want %q", got, "[]")
	}
}

func TestFormat_Single(t *testing.T) {
	got := Format([]float32{1.5})
	if got != "[1.5]" {
		t.Errorf("Format([1.5]) = %q, want %q", got, "[1.5]")
	}
}

func TestFormat_Multiple(t *testing.T) {
	got := Format([]float32{1, 2.5, -3})
	if got != "[1,2.5,-3]" {
		t.Errorf("Format([1,2.5,-3]) = %q, want %q", got, "[1,2.5,-3]")
	}
}

func TestFormat_NoBracketedSpaces(t *testing.T) {
	got := Format([]float32{0.1, 0.2})
	if strings.Contains(got, " ") {
		t.Errorf("Format output must not contain spaces, got %q", got)
	}
}

func TestIsSafeIdent_Accepts(t *testing.T) {
	cases := []string{
		"documents",
		"embedding_v2",
		"t123",
		"a",
		strings.Repeat("a", 63), // exactly 63 chars
	}
	for _, s := range cases {
		if !isSafeIdent(s) {
			t.Errorf("isSafeIdent(%q) = false, want true", s)
		}
	}
}

func TestIsSafeIdent_Rejects(t *testing.T) {
	cases := []struct {
		name  string
		ident string
	}{
		{"empty", ""},
		{"too long (64 chars)", strings.Repeat("a", 64)},
		{"uppercase", "Documents"},
		{"dash", "my-table"},
		{"space", "my table"},
		{"semicolon", "table;drop"},
		{"dot", "a.b"},
		{"dollar", "$field"},
		{"quote", `"quoted"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if isSafeIdent(tc.ident) {
				t.Errorf("isSafeIdent(%q) = true, want false", tc.ident)
			}
		})
	}
}

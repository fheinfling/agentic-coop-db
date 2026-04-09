package tenant

import (
	"strings"
	"testing"
)

// isSafeRoleName is the only privilege-boundary check that runs on the
// pg_role string before it gets interpolated into a `SET LOCAL ROLE
// "..."` statement. The same shape is enforced at key creation time,
// but defense in depth is the whole point — these tests lock the
// allowlist down so accidentally widening it gets caught in CI.

func TestIsSafeRoleName_Accepts(t *testing.T) {
	good := []string{
		"a",
		"abc",
		"dbadmin",
		"dbuser",
		"custom_role",
		"role_with_digits_123",
		"_leading_underscore",
		"a1",
		strings.Repeat("a", 63), // exactly the 63-char Postgres limit
	}
	for _, s := range good {
		t.Run(s, func(t *testing.T) {
			if !isSafeRoleName(s) {
				t.Errorf("isSafeRoleName(%q) = false, want true", s)
			}
		})
	}
}

func TestIsSafeRoleName_Rejects(t *testing.T) {
	cases := []struct {
		name string
		role string
	}{
		{"empty", ""},
		{"too long (64 chars)", strings.Repeat("a", 64)},
		{"way too long", strings.Repeat("a", 200)},
		{"uppercase only", "DBADMIN"},
		{"mixed case", "DbAdmin"},
		{"with space", "db admin"},
		{"with hyphen", "db-admin"},
		{"with dot", "db.admin"},
		{"with semicolon", "dbadmin;DROP"},
		{"with double quote", `db"admin`},
		{"with single quote", "db'admin"},
		{"with backslash", `db\admin`},
		{"with newline", "dbadmin\n"},
		{"with NUL byte", "dbadmin\x00"},
		{"non-ASCII", "rôle"},
		{"emoji", "db🔥"},
		{"sql injection attempt", `dbadmin"; DROP TABLE api_keys; --`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if isSafeRoleName(tc.role) {
				t.Errorf("isSafeRoleName(%q) = true, want false (this would have allowed SQL injection via SET LOCAL ROLE)", tc.role)
			}
		})
	}
}

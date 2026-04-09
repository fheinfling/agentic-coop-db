package db

import (
	"errors"
	"strings"
	"testing"
)

// These tests cover the pure helpers in migrate.go that don't need a
// running Postgres. The functions that DO need a real connection
// (RunMigrations, SetRolePassword, EnsureOwnerRole, MintKey) are
// covered by the testcontainers-backed suite under test/integration.

func TestInjectPassword(t *testing.T) {
	cases := []struct {
		name     string
		url      string
		password string
		want     string
		wantErr  bool
	}{
		{
			name:     "empty password is no-op",
			url:      "postgres://alice@host:5432/db",
			password: "",
			want:     "postgres://alice@host:5432/db",
		},
		{
			name:     "injects on userinfo without existing password",
			url:      "postgres://alice@host:5432/db",
			password: "secret",
			want:     "postgres://alice:secret@host:5432/db",
		},
		{
			name:     "replaces existing password",
			url:      "postgres://alice:old@host:5432/db",
			password: "new",
			want:     "postgres://alice:new@host:5432/db",
		},
		{
			name:     "url-encodes special chars in the password",
			url:      "postgres://alice@host/db",
			password: "p@ss/word",
			// net/url percent-encodes only chars that are illegal in
			// userinfo. '@' and '/' are both illegal there, so they
			// MUST be escaped.
			want: "postgres://alice:p%40ss%2Fword@host/db",
		},
		{
			name:     "preserves query params",
			url:      "postgres://alice@host/db?sslmode=require",
			password: "secret",
			want:     "postgres://alice:secret@host/db?sslmode=require",
		},
		{
			name:     "rejects URL without user component",
			url:      "postgres://host:5432/db",
			password: "secret",
			wantErr:  true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := injectPassword(tc.url, tc.password)
			if (err != nil) != tc.wantErr {
				t.Fatalf("injectPassword: err=%v, wantErr=%v", err, tc.wantErr)
			}
			if err != nil {
				return
			}
			if got != tc.want {
				t.Errorf("got  %q\nwant %q", got, tc.want)
			}
		})
	}
}

func TestUrlPassword(t *testing.T) {
	cases := []struct {
		name string
		url  string
		want string
	}{
		{"with password", "postgres://alice:secret@host/db", "secret"},
		{"without password", "postgres://alice@host/db", ""},
		{"no userinfo", "postgres://host/db", ""},
		{"empty url", "", ""},
		{"malformed url", "::not a url::", ""},
		{"percent-encoded password is decoded", "postgres://alice:p%40ss@host/db", "p@ss"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := urlPassword(tc.url); got != tc.want {
				t.Errorf("urlPassword(%q) = %q, want %q", tc.url, got, tc.want)
			}
		})
	}
}

func TestRedactSecrets(t *testing.T) {
	t.Run("nil error returns nil", func(t *testing.T) {
		if got := redactSecrets(nil, "secret"); got != nil {
			t.Errorf("got %v, want nil", got)
		}
	})

	t.Run("error without any secret matches is unchanged", func(t *testing.T) {
		orig := errors.New("connection refused")
		got := redactSecrets(orig, "unrelated", "alsounrelated")
		if got != orig {
			t.Errorf("expected the original error to pass through unchanged when no secret matches; got a new error %T", got)
		}
	})

	t.Run("single secret is replaced", func(t *testing.T) {
		err := errors.New(`failed to connect: postgres://alice:hunter2@host/db: timeout`)
		got := redactSecrets(err, "hunter2")
		if strings.Contains(got.Error(), "hunter2") {
			t.Errorf("redactSecrets did not strip the secret: %q", got.Error())
		}
		if !strings.Contains(got.Error(), "REDACTED") {
			t.Errorf("expected REDACTED marker in %q", got.Error())
		}
	})

	t.Run("multiple secrets all replaced", func(t *testing.T) {
		err := errors.New("a=ownerpw, b=gatewaypw, c=urlpw")
		got := redactSecrets(err, "ownerpw", "gatewaypw", "urlpw")
		msg := got.Error()
		for _, s := range []string{"ownerpw", "gatewaypw", "urlpw"} {
			if strings.Contains(msg, s) {
				t.Errorf("secret %q still present in %q", s, msg)
			}
		}
		if strings.Count(msg, "REDACTED") != 3 {
			t.Errorf("expected 3 REDACTED markers in %q", msg)
		}
	})

	t.Run("empty secrets are skipped", func(t *testing.T) {
		err := errors.New("nothing to hide")
		got := redactSecrets(err, "", "")
		if got.Error() != err.Error() {
			t.Errorf("empty secrets should be a no-op; got %q", got.Error())
		}
	})

	t.Run("does not double-redact", func(t *testing.T) {
		// If "REDACTED" itself were treated as a secret, we'd loop
		// forever. Just sanity-check the output is finite.
		err := errors.New("REDACTED is a token")
		got := redactSecrets(err, "REDACTED")
		if got == nil {
			t.Fatal("got nil")
		}
	})
}

func TestRewriteSchemeForMigrate(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{
			name: "postgres scheme is rewritten",
			in:   "postgres://alice:secret@host/db?sslmode=require",
			want: "pgx5://alice:secret@host/db?sslmode=require",
		},
		{
			name: "postgresql scheme is rewritten",
			in:   "postgresql://alice@host/db",
			want: "pgx5://alice@host/db",
		},
		{
			name: "pgx5 scheme is left as-is",
			in:   "pgx5://alice@host/db",
			want: "pgx5://alice@host/db",
		},
		{
			name:    "rejects unsupported scheme",
			in:      "mysql://alice@host/db",
			wantErr: true,
		},
		{
			name:    "rejects malformed URL",
			in:      "://nope",
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := rewriteSchemeForMigrate(tc.in)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err=%v, wantErr=%v", err, tc.wantErr)
			}
			if err != nil {
				return
			}
			if got != tc.want {
				t.Errorf("got  %q\nwant %q", got, tc.want)
			}
		})
	}
}

func TestIsSafeIdent(t *testing.T) {
	good := []string{
		"a",
		"abc",
		"abc_def",
		"abc123",
		"_underscore",
		"a_b_c_1_2_3",
		strings.Repeat("a", 63), // exactly the 63-char Postgres limit
	}
	for _, s := range good {
		if !isSafeIdent(s) {
			t.Errorf("isSafeIdent(%q) = false, want true", s)
		}
	}

	bad := []string{
		"",                      // empty
		strings.Repeat("a", 64), // too long
		"ABC",                   // uppercase
		"with space",            // space
		"with-hyphen",           // hyphen
		"with.dot",              // dot
		`with"quote`,            // quote
		`with;semicolon`,        // semicolon
		"unicodé",               // non-ascii
		"abc\x00def",            // NUL byte
	}
	for _, s := range bad {
		if isSafeIdent(s) {
			t.Errorf("isSafeIdent(%q) = true, want false", s)
		}
	}
}

package auth_test

import (
	"strings"
	"testing"

	"github.com/fheinfling/ai-coop-db/internal/auth"
)

func TestParseBearer_RoundTrip(t *testing.T) {
	for _, env := range []auth.KeyEnvironment{auth.EnvDev, auth.EnvLive, auth.EnvTest} {
		t.Run(string(env), func(t *testing.T) {
			keyID, secret, full, err := auth.Mint(env)
			if err != nil {
				t.Fatalf("Mint: %v", err)
			}
			parsed, err := auth.ParseBearer("Bearer " + full)
			if err != nil {
				t.Fatalf("ParseBearer: %v", err)
			}
			if parsed.Env != env {
				t.Errorf("env: got %q, want %q", parsed.Env, env)
			}
			if parsed.KeyID != keyID {
				t.Errorf("keyID: got %q, want %q", parsed.KeyID, keyID)
			}
			if parsed.Secret != secret {
				t.Errorf("secret: got %q, want %q", parsed.Secret, secret)
			}
		})
	}
}

// TestParseBearer_UnderscoreInPayload locks in the fix for the parser
// regression: base64url's alphabet includes "_", so generated keyIDs
// and secrets can contain underscores. The original split-on-"_"
// parser rejected ~60% of generated keys with ErrInvalidKey. The
// position-based parser must accept them.
func TestParseBearer_UnderscoreInPayload(t *testing.T) {
	cases := []struct {
		name   string
		keyID  string
		secret string
	}{
		// All four combinations of clean / dirty in each field. The
		// keyID is exactly 16 chars and the secret is exactly 32 chars
		// — what auth.Mint would produce — but with `_` substituted at
		// representative positions in each.
		{"both_clean", "abcdefghijklmnop", "ABCDEFGHIJKLMNOPabcdefghijklmnop"},
		{"underscore_in_keyID", "abc_efghijklmnop", "ABCDEFGHIJKLMNOPabcdefghijklmnop"},
		{"underscore_in_secret", "abcdefghijklmnop", "ABCD_FGHIJKLMNOPabcdefghijklmnop"},
		{"underscore_at_end_of_secret", "abcdefghijklmnop", "ABCDEFGHIJKLMNOPabcdefghijklmno_"},
		{"underscore_at_start_of_keyID", "_bcdefghijklmnop", "ABCDEFGHIJKLMNOPabcdefghijklmnop"},
		{"underscore_in_both", "_bc_efghijklmnop", "ABCD_FGHIJKLMNOPabcdefghijklmno_"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			full := "acd_dev_" + tc.keyID + "_" + tc.secret
			parsed, err := auth.ParseBearer("Bearer " + full)
			if err != nil {
				t.Fatalf("ParseBearer(%q): %v", full, err)
			}
			if parsed.Env != auth.EnvDev {
				t.Errorf("env: got %q, want dev", parsed.Env)
			}
			if parsed.KeyID != tc.keyID {
				t.Errorf("keyID: got %q, want %q", parsed.KeyID, tc.keyID)
			}
			if parsed.Secret != tc.secret {
				t.Errorf("secret: got %q, want %q", parsed.Secret, tc.secret)
			}
		})
	}
}

func TestParseBearer_AcceptsBareToken(t *testing.T) {
	// Same parser must accept the value with or without the "Bearer "
	// prefix (the middleware sometimes strips it before calling).
	full := "acd_dev_abcdefghijklmnop_ABCDEFGHIJKLMNOPabcdefghijklmnop"
	for _, header := range []string{full, "Bearer " + full, "  Bearer  " + full + "  "} {
		t.Run(header, func(t *testing.T) {
			if _, err := auth.ParseBearer(header); err != nil {
				t.Errorf("ParseBearer(%q): %v", header, err)
			}
		})
	}
}

func TestParseBearer_Rejects(t *testing.T) {
	cases := []struct {
		name   string
		header string
	}{
		{"empty", ""},
		{"just_bearer", "Bearer "},
		{"wrong_prefix", "Bearer xyz_dev_abcdefghijklmnop_ABCDEFGHIJKLMNOPabcdefghijklmnop"},
		{"unknown_env", "Bearer acd_prod_abcdefghijklmnop_ABCDEFGHIJKLMNOPabcdefghijklmnop"},
		{"keyID_too_short", "Bearer acd_dev_abc_ABCDEFGHIJKLMNOPabcdefghijklmnop"},
		{"keyID_too_long", "Bearer acd_dev_abcdefghijklmnopX_ABCDEFGHIJKLMNOPabcdefghijklmnop"},
		{"secret_too_short", "Bearer acd_dev_abcdefghijklmnop_short"},
		{"secret_too_long", "Bearer acd_dev_abcdefghijklmnop_" + strings.Repeat("X", 33)},
		{"missing_keyid_secret_separator", "Bearer acd_dev_abcdefghijklmnopABCDEFGHIJKLMNOPabcdefghijklmnop"},
		{"missing_env_separator", "Bearer acd_devabcdefghijklmnop_ABCDEFGHIJKLMNOPabcdefghijklmnop"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := auth.ParseBearer(tc.header); err == nil {
				t.Errorf("ParseBearer(%q): expected ErrInvalidKey, got nil", tc.header)
			}
		})
	}
}

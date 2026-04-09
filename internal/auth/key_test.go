package auth_test

import (
	"strings"
	"testing"

	"github.com/fheinfling/agentic-coop-db/internal/auth"
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

func TestHashSecret_ReturnsPHC(t *testing.T) {
	phc, err := auth.HashSecret("x")
	if err != nil {
		t.Fatalf("HashSecret: %v", err)
	}
	if !strings.HasPrefix(phc, "$argon2id$") {
		t.Errorf("expected $argon2id$ prefix, got %q", phc)
	}
}

func TestVerifySecret_Correct(t *testing.T) {
	phc, err := auth.HashSecret("correct-secret")
	if err != nil {
		t.Fatalf("HashSecret: %v", err)
	}
	if err := auth.VerifySecret("correct-secret", phc); err != nil {
		t.Errorf("VerifySecret with correct secret: %v", err)
	}
}

func TestVerifySecret_Wrong(t *testing.T) {
	phc, err := auth.HashSecret("original")
	if err != nil {
		t.Fatalf("HashSecret: %v", err)
	}
	if err := auth.VerifySecret("wrong", phc); err == nil {
		t.Error("VerifySecret with wrong secret: expected error, got nil")
	}
}

func TestVerifySecret_MalformedPHC(t *testing.T) {
	cases := []struct {
		name string
		phc  string
	}{
		{"empty", ""},
		{"truncated", "$argon2id$v=19"},
		{"random_string", "not-a-phc-string"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := auth.VerifySecret("secret", tc.phc); err == nil {
				t.Errorf("VerifySecret(%q): expected error, got nil", tc.phc)
			}
		})
	}
}

func TestCacheKey_Deterministic(t *testing.T) {
	p := &auth.ParsedKey{FullToken: "acd_dev_abcdefghijklmnop_ABCDEFGHIJKLMNOPabcdefghijklmnop"}
	k1 := p.CacheKey()
	k2 := p.CacheKey()
	if k1 != k2 {
		t.Errorf("CacheKey not deterministic: %q != %q", k1, k2)
	}
	if k1 == "" {
		t.Error("CacheKey returned empty string")
	}
}

func TestCacheKey_Distinct(t *testing.T) {
	a := &auth.ParsedKey{FullToken: "acd_dev_aaaaaaaaaaaaaaaa_BBBBBBBBBBBBBBBBbbbbbbbbbbbbbbbb"}
	b := &auth.ParsedKey{FullToken: "acd_dev_bbbbbbbbbbbbbbbb_BBBBBBBBBBBBBBBBbbbbbbbbbbbbbbbb"}
	if a.CacheKey() == b.CacheKey() {
		t.Error("different tokens produced the same CacheKey")
	}
}

func TestKeyEnvironment_IsValid(t *testing.T) {
	valid := []auth.KeyEnvironment{auth.EnvDev, auth.EnvLive, auth.EnvTest}
	for _, e := range valid {
		if !e.IsValid() {
			t.Errorf("IsValid(%q) = false, want true", e)
		}
	}
	invalid := []auth.KeyEnvironment{"prod", "staging", ""}
	for _, e := range invalid {
		if e.IsValid() {
			t.Errorf("IsValid(%q) = true, want false", e)
		}
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

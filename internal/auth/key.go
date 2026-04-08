package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/argon2"
)

// KeyEnvironment is the env tag baked into a key string.
type KeyEnvironment string

const (
	EnvLive KeyEnvironment = "live"
	EnvDev  KeyEnvironment = "dev"
	EnvTest KeyEnvironment = "test"
)

// IsValid returns true if env is one of the recognised tags.
func (e KeyEnvironment) IsValid() bool {
	switch e {
	case EnvLive, EnvDev, EnvTest:
		return true
	}
	return false
}

// Argon2id parameters. Picked once and pinned: changing them invalidates
// existing hashes.
const (
	argonTime    uint32 = 2
	argonMemory  uint32 = 64 * 1024 // 64 MiB
	argonThreads uint8  = 2
	argonKeyLen  uint32 = 32
	argonSaltLen uint32 = 16

	keyIDRawBytes  = 12 // -> 16 base64 chars
	secretRawBytes = 24 // -> 32 base64 chars (192 bits of entropy)
)

// ErrInvalidKey is returned by ParseBearer for any malformed token.
var ErrInvalidKey = errors.New("invalid api key")

// ParsedKey is the structured form of a bearer token.
type ParsedKey struct {
	Env       KeyEnvironment
	KeyID     string // 16 url-safe base64 chars
	Secret    string // 32 url-safe base64 chars
	FullToken string // the original full token (used for cache key only)
}

// Encoded lengths of keyID and secret. Both are base64url encodings of
// keyIDRawBytes / secretRawBytes respectively (no padding because both
// are multiples of 3 bytes), so they are fixed-length and we can parse
// the token by position rather than by splitting on "_".
const (
	keyIDEncodedLen  = 16 // base64url(12 raw bytes)
	secretEncodedLen = 32 // base64url(24 raw bytes)
)

// ParseBearer accepts the value of the `Authorization` header (with or
// without the leading "Bearer ") and returns a ParsedKey or ErrInvalidKey.
//
// The token format is `acd_<env>_<keyID>_<secret>`. We can NOT parse it
// by splitting on "_" because both keyID and secret are base64url-encoded
// random bytes, and the base64url alphabet includes "_". With 16-char
// keyIDs and 32-char secrets, ~25% of keyIDs and ~50% of secrets contain
// at least one "_" — splitting overcounts the parts and rejects the
// token. The original `strings.Split(_, "_")` parser broke ~60% of
// generated keys.
//
// Instead we parse by position. The structure is fully determined:
//
//	"acd_"          (4 literal chars)
//	<env>           (3 or 4 chars: dev / live / test, validated below)
//	"_"             (1 literal char)
//	<keyID>         (exactly keyIDEncodedLen chars)
//	"_"             (1 literal char)
//	<secret>        (exactly secretEncodedLen chars)
//
// env is the only variable-length component before keyID, and we find
// it by locating the first "_" after the "acd_" prefix. Once env is
// known, the remaining slice has exactly two fixed-length fields.
func ParseBearer(header string) (*ParsedKey, error) {
	header = strings.TrimSpace(header)
	if header == "" {
		return nil, ErrInvalidKey
	}
	header = strings.TrimPrefix(header, "Bearer ")
	header = strings.TrimSpace(header)

	if !strings.HasPrefix(header, "acd_") {
		return nil, ErrInvalidKey
	}
	rest := header[len("acd_"):]

	sep := strings.IndexByte(rest, '_')
	if sep < 1 {
		return nil, ErrInvalidKey
	}
	env := KeyEnvironment(rest[:sep])
	if !env.IsValid() {
		return nil, ErrInvalidKey
	}
	rest = rest[sep+1:]

	// Remaining slice must be exactly: keyID + "_" + secret.
	if len(rest) != keyIDEncodedLen+1+secretEncodedLen {
		return nil, ErrInvalidKey
	}
	if rest[keyIDEncodedLen] != '_' {
		return nil, ErrInvalidKey
	}
	keyID := rest[:keyIDEncodedLen]
	secret := rest[keyIDEncodedLen+1:]

	return &ParsedKey{
		Env:       env,
		KeyID:     keyID,
		Secret:    secret,
		FullToken: header,
	}, nil
}

// Mint generates a fresh (key_id, secret, full_token) triple.
func Mint(env KeyEnvironment) (keyID, secret, fullToken string, err error) {
	if !env.IsValid() {
		return "", "", "", fmt.Errorf("auth.Mint: invalid env %q", env)
	}
	idBytes := make([]byte, keyIDRawBytes)
	if _, err := rand.Read(idBytes); err != nil {
		return "", "", "", err
	}
	secBytes := make([]byte, secretRawBytes)
	if _, err := rand.Read(secBytes); err != nil {
		return "", "", "", err
	}
	keyID = base64.RawURLEncoding.EncodeToString(idBytes)
	secret = base64.RawURLEncoding.EncodeToString(secBytes)
	fullToken = fmt.Sprintf("acd_%s_%s_%s", env, keyID, secret)
	return keyID, secret, fullToken, nil
}

// HashSecret returns a PHC-encoded argon2id hash of the secret string.
//
// Format: $argon2id$v=19$m=65536,t=2,p=2$<saltB64>$<hashB64>
func HashSecret(secret string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	hash := argon2.IDKey([]byte(secret), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash),
	), nil
}

// VerifySecret runs argon2id over secret with the parameters embedded in
// phc and returns nil if the result equals the stored hash. Comparison is
// constant-time.
func VerifySecret(secret, phc string) error {
	parts := strings.Split(phc, "$")
	// "", "argon2id", "v=19", "m=...,t=...,p=...", saltB64, hashB64
	if len(parts) != 6 || parts[1] != "argon2id" {
		return ErrInvalidKey
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return ErrInvalidKey
	}
	if version != argon2.Version {
		return ErrInvalidKey
	}
	var m, t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return ErrInvalidKey
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return ErrInvalidKey
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return ErrInvalidKey
	}
	got := argon2.IDKey([]byte(secret), salt, t, m, p, uint32(len(want)))
	if subtleConstantTimeEq(got, want) {
		return nil
	}
	return ErrInvalidKey
}

// subtleConstantTimeEq is a tiny wrapper so we can keep the dependency tree
// flat (avoid pulling crypto/subtle into doc tests).
func subtleConstantTimeEq(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var v byte
	for i := range a {
		v |= a[i] ^ b[i]
	}
	return v == 0
}

// CacheKey returns a stable, cheap-to-compute key for the verify cache.
// We hash the full token rather than store it directly so a memory dump
// of a running process never reveals plaintext tokens.
func (p *ParsedKey) CacheKey() string {
	sum := sha256.Sum256([]byte(p.FullToken))
	return hex.EncodeToString(sum[:])
}

// dummyHash is a precomputed argon2id PHC string used by the middleware to
// equalise response times when a key_id lookup misses. Without this, an
// attacker could measure response time to enumerate valid key_id values
// (a fast 401 means "key not found"; a slow 401 means "key found, secret
// wrong"). Running VerifySecret against the dummy hash on miss makes both
// paths take roughly the same time.
//
// Initialised lazily on first use so the cost is paid by the first request,
// not by package init (which would slow down `aicoldb-server -version`).
var (
	dummyHash     string
	dummyHashOnce sync.Once
)

// DummyHash returns the precomputed dummy argon2id PHC string. It is
// idempotent and goroutine-safe.
func DummyHash() string {
	dummyHashOnce.Do(func() {
		// Hash a fixed sentinel. The plaintext doesn't matter; only the
		// time-cost of running argon2id matters.
		h, err := HashSecret("aicoldb_dummy_secret_for_timing_safety")
		if err == nil {
			dummyHash = h
		}
	})
	return dummyHash
}

// KeyRecord is the row stored in api_keys, plus the workspace metadata
// the middleware needs to populate WorkspaceContext.
type KeyRecord struct {
	ID            string
	WorkspaceID   string
	KeyID         string
	SecretHash    string
	Env           KeyEnvironment
	PgRole        string
	Name          string
	CreatedAt     time.Time
	LastUsedAt    *time.Time
	ExpiresAt     *time.Time
	RevokedAt     *time.Time
	ReplacesKeyID *string
}

// Active reports whether the record is currently usable.
func (r *KeyRecord) Active(now time.Time) bool {
	if r.RevokedAt != nil {
		return false
	}
	if r.ExpiresAt != nil && !r.ExpiresAt.After(now) {
		return false
	}
	return true
}

package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
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

// ParseBearer accepts the value of the `Authorization` header (with or
// without the leading "Bearer ") and returns a ParsedKey or ErrInvalidKey.
func ParseBearer(header string) (*ParsedKey, error) {
	header = strings.TrimSpace(header)
	if header == "" {
		return nil, ErrInvalidKey
	}
	header = strings.TrimPrefix(header, "Bearer ")
	header = strings.TrimSpace(header)

	parts := strings.Split(header, "_")
	// "aic", env, keyID, secret
	if len(parts) != 4 || parts[0] != "aic" {
		return nil, ErrInvalidKey
	}
	env := KeyEnvironment(parts[1])
	if !env.IsValid() {
		return nil, ErrInvalidKey
	}
	if len(parts[2]) == 0 || len(parts[3]) == 0 {
		return nil, ErrInvalidKey
	}
	return &ParsedKey{
		Env:       env,
		KeyID:     parts[2],
		Secret:    parts[3],
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
	fullToken = fmt.Sprintf("aic_%s_%s_%s", env, keyID, secret)
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

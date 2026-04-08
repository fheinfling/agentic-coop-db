// Package auth implements API key parsing, hashing, lookup, and the HTTP
// middleware that authenticates requests.
//
// Key format on the wire:
//
//	aic_<env>_<key_id>_<secret>
//
// where:
//
//   - "aic"      — fixed prefix so the token is greppable in logs
//   - <env>      — "live" | "dev" | "test"; matches api_keys.env
//   - <key_id>   — 16 url-safe base64 chars (12 raw bytes), used as the
//                  unique lookup column on api_keys
//   - <secret>   — 32 url-safe base64 chars (24 raw bytes, ~192 bits of
//                  entropy), verified against secret_hash with argon2id
//
// The full token is shown to the caller exactly once at creation time. Only
// secret_hash is stored in the database; the plaintext is never logged.
//
// Verification flow on every request:
//
//  1. Parse the bearer token, fail fast if it does not match the format.
//  2. Look up the row by key_id. Fail-closed on revoked / expired.
//  3. Check the in-memory LRU cache (sha256(full_token) -> bool, 5-min TTL).
//  4. If miss, run argon2id verify, populate the cache.
//  5. Attach a WorkspaceContext to the request context.
//
// The cache exists because argon2id is intentionally slow (~50 ms / verify).
// At 60 req/s per key, the cache lets us spend ~one verify per 5 minutes
// rather than 60 per second.
package auth

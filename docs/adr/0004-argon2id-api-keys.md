# ADR 0004 — Argon2id for API key hashing

**Status:** accepted

## Decision

API key secrets are hashed at rest with Argon2id, parameters
`time=2, memory=64 MiB, threads=2`, with a per-key 16-byte salt and a
32-byte derived key. The hash is stored in PHC string format.

## Why

- Memory-hard, side-channel-resistant; the OWASP-recommended choice.
- The cost parameters are tuned to ~50 ms per verify on a Pi 5 — slow
  enough to deter bulk verification, fast enough that the auth cache
  amortises the cost over the request stream.
- The PHC format encodes parameters in the hash, so we can ratchet the
  cost in the future without a migration: a new hash is written on the
  next successful verify.

## Cache

A pure verification cost of 50 ms per request is unacceptable. We add an
in-memory LRU+TTL cache keyed on `sha256(full_token)` so argon2id runs at
most once per 5 minutes per server per key. The cache value is invalidated
explicitly on rotation/revocation.

## Alternatives considered

- bcrypt — older, no memory hardness, no parameter ratchet.
- scrypt — solid but argon2id is the modern winner.
- HMAC-SHA256 with a server-side pepper — fast but provides no defence in
  depth against database compromise (the pepper is the only secret).

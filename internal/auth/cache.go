package auth

import (
	"sync"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
)

// VerifyCache is a small TTL+LRU cache that lets us skip argon2id verification
// for keys we have recently seen. The cache key is sha256(full bearer token);
// the cache value is the verified KeyRecord plus a verifiedAt timestamp.
//
// Eviction is twofold:
//   - LRU bound on size
//   - Per-entry TTL checked on every Get
//
// On a successful POST /v1/auth/keys/rotate the middleware calls Invalidate
// for the old key, so a freshly rotated key never serves a stale identity.
type VerifyCache struct {
	lru *lru.Cache[string, *cachedKey]
	ttl time.Duration

	mu       sync.RWMutex
	stamps   map[string]time.Time
	invalids map[string]struct{}
}

type cachedKey struct {
	rec        *KeyRecord
	verifiedAt time.Time
}

// NewVerifyCache constructs a cache of the given size with the given TTL.
func NewVerifyCache(size int, ttl time.Duration) (*VerifyCache, error) {
	if size <= 0 {
		size = 1024
	}
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	c, err := lru.New[string, *cachedKey](size)
	if err != nil {
		return nil, err
	}
	return &VerifyCache{
		lru:      c,
		ttl:      ttl,
		stamps:   make(map[string]time.Time, size),
		invalids: make(map[string]struct{}),
	}, nil
}

// Get returns the cached record if present and fresh.
func (c *VerifyCache) Get(token string) (*KeyRecord, bool) {
	c.mu.RLock()
	if _, dead := c.invalids[token]; dead {
		c.mu.RUnlock()
		return nil, false
	}
	c.mu.RUnlock()

	v, ok := c.lru.Get(token)
	if !ok {
		return nil, false
	}
	if time.Since(v.verifiedAt) > c.ttl {
		c.lru.Remove(token)
		return nil, false
	}
	return v.rec, true
}

// Put inserts a verified record.
func (c *VerifyCache) Put(token string, rec *KeyRecord) {
	c.mu.Lock()
	delete(c.invalids, token)
	c.mu.Unlock()
	c.lru.Add(token, &cachedKey{rec: rec, verifiedAt: time.Now()})
}

// Invalidate evicts a token immediately. Used after rotation/revocation.
func (c *VerifyCache) Invalidate(token string) {
	c.lru.Remove(token)
	c.mu.Lock()
	c.invalids[token] = struct{}{}
	c.mu.Unlock()
}

// Len returns the current cache size, used as a prometheus gauge.
func (c *VerifyCache) Len() int {
	return c.lru.Len()
}

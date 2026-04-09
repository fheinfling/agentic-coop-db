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
// Eviction is threefold:
//   - LRU bound on size
//   - Per-entry TTL checked on every Get
//   - Per-key-DB-ID revocation set checked on every Get
//
// The third path is what makes /v1/auth/keys/rotate and Store.Revoke take
// effect immediately rather than waiting for the LRU TTL: the rotate /
// revoke handler calls RevokeByDBID(keyDBID), and the next cache lookup
// for any token bound to that row falls through to the database (which
// returns either ErrKeyNotFound or a row whose Active() check fails).
type VerifyCache struct {
	lru *lru.Cache[string, *cachedKey]
	ttl time.Duration

	mu      sync.RWMutex
	revoked map[string]struct{} // key DB IDs that have been forcibly evicted
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
		lru:     c,
		ttl:     ttl,
		revoked: make(map[string]struct{}),
	}, nil
}

// Get returns the cached record if present, fresh, and not flagged as
// revoked since insertion.
func (c *VerifyCache) Get(token string) (*KeyRecord, bool) {
	v, ok := c.lru.Get(token)
	if !ok {
		return nil, false
	}
	if time.Since(v.verifiedAt) > c.ttl {
		c.lru.Remove(token)
		return nil, false
	}
	c.mu.RLock()
	_, revoked := c.revoked[v.rec.ID]
	c.mu.RUnlock()
	if revoked {
		c.lru.Remove(token)
		return nil, false
	}
	return v.rec, true
}

// Put inserts a verified record. If the row was previously marked revoked
// for this key DB id, clear the flag — Put is only called after a fresh
// argon2id verify against the database, so the record is known good.
func (c *VerifyCache) Put(token string, rec *KeyRecord) {
	c.mu.Lock()
	delete(c.revoked, rec.ID)
	c.mu.Unlock()
	c.lru.Add(token, &cachedKey{rec: rec, verifiedAt: time.Now()})
}

// RevokeByDBID forces every cache entry whose KeyRecord.ID matches keyDBID
// to be treated as a miss on the next Get. This is the path the HTTP layer
// uses after a successful rotation or revocation, so a stolen token cannot
// continue working until the LRU TTL expires (the previous behaviour gave
// attackers up to 5 minutes after revocation).
func (c *VerifyCache) RevokeByDBID(keyDBID string) {
	if keyDBID == "" {
		return
	}
	c.mu.Lock()
	c.revoked[keyDBID] = struct{}{}
	c.mu.Unlock()
}

// Len returns the current cache size, used as a prometheus gauge.
func (c *VerifyCache) Len() int {
	return c.lru.Len()
}

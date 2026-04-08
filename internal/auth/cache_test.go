package auth

import (
	"testing"
	"time"
)

func newTestCache(t *testing.T, size int, ttl time.Duration) *VerifyCache {
	t.Helper()
	c, err := NewVerifyCache(size, ttl)
	if err != nil {
		t.Fatalf("NewVerifyCache: %v", err)
	}
	return c
}

func TestVerifyCache_Miss(t *testing.T) {
	c := newTestCache(t, 8, time.Minute)
	if rec, ok := c.Get("no-such-token"); ok || rec != nil {
		t.Errorf("expected miss, got ok=%v rec=%v", ok, rec)
	}
}

func TestVerifyCache_HitAfterPut(t *testing.T) {
	c := newTestCache(t, 8, time.Minute)
	rec := &KeyRecord{ID: "key-1", PgRole: "dbuser"}
	c.Put("tok-abc", rec)
	got, ok := c.Get("tok-abc")
	if !ok {
		t.Fatal("expected hit, got miss")
	}
	if got != rec {
		t.Errorf("got different record pointer: %+v", got)
	}
}

func TestVerifyCache_TTLExpiry(t *testing.T) {
	c := newTestCache(t, 8, time.Millisecond)
	rec := &KeyRecord{ID: "key-2", PgRole: "dbuser"}
	c.Put("tok-ttl", rec)
	time.Sleep(2 * time.Millisecond)
	if _, ok := c.Get("tok-ttl"); ok {
		t.Error("expected TTL expiry miss, got hit")
	}
}

func TestVerifyCache_RevokeByDBID(t *testing.T) {
	c := newTestCache(t, 8, time.Minute)
	rec := &KeyRecord{ID: "key-3", PgRole: "dbuser"}
	c.Put("tok-rev", rec)
	c.RevokeByDBID("key-3")
	if _, ok := c.Get("tok-rev"); ok {
		t.Error("expected miss after revocation, got hit")
	}
}

func TestVerifyCache_PutAfterRevoke(t *testing.T) {
	// Simulates the post-rotation path: revoke the old entry, then Put after
	// a fresh argon2id verify — the revocation flag must be cleared so the
	// new entry is accessible.
	c := newTestCache(t, 8, time.Minute)
	rec := &KeyRecord{ID: "key-4", PgRole: "dbuser"}
	c.Put("tok-old", rec)
	c.RevokeByDBID("key-4")
	// Fresh DB verify succeeded → Put again.
	c.Put("tok-new", rec)
	if _, ok := c.Get("tok-new"); !ok {
		t.Error("expected hit after re-Put, got miss")
	}
}

func TestVerifyCache_Len(t *testing.T) {
	c := newTestCache(t, 8, time.Minute)
	if c.Len() != 0 {
		t.Errorf("Len before any Put: got %d, want 0", c.Len())
	}
	c.Put("t1", &KeyRecord{ID: "k1"})
	c.Put("t2", &KeyRecord{ID: "k2"})
	if c.Len() != 2 {
		t.Errorf("Len after 2 Puts: got %d, want 2", c.Len())
	}
}

func TestNewVerifyCache_ZeroDefaults(t *testing.T) {
	// size=0 and ttl=0 should fall back to the built-in defaults, not error.
	c, err := NewVerifyCache(0, 0)
	if err != nil {
		t.Fatalf("NewVerifyCache(0,0): unexpected error: %v", err)
	}
	if c.ttl <= 0 {
		t.Errorf("expected positive default TTL, got %v", c.ttl)
	}
}

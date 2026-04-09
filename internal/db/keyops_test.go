package db

import (
	"context"
	"testing"
	"time"
)

func TestKeyStatus(t *testing.T) {
	now := time.Now()
	past := now.Add(-24 * time.Hour)
	future := now.Add(24 * time.Hour)

	cases := []struct {
		name      string
		revokedAt *time.Time
		expiresAt *time.Time
		want      string
	}{
		{"nil/nil is active", nil, nil, "active"},
		{"revoked", &past, nil, "revoked"},
		{"revoked takes precedence over expired", &past, &past, "revoked"},
		{"expired in the past", nil, &past, "expired"},
		{"expired exactly now", nil, &now, "expired"},
		{"future expiry is active", nil, &future, "active"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := keyStatus(tc.revokedAt, tc.expiresAt)
			if got != tc.want {
				t.Errorf("keyStatus() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRevokeKeyValidatesUUID(t *testing.T) {
	bad := []string{"", "not-a-uuid", "'; DROP TABLE api_keys;--"}
	for _, id := range bad {
		t.Run(id, func(t *testing.T) {
			err := RevokeKey(context.Background(), "postgres://localhost/test", "", id)
			if err == nil {
				t.Fatal("expected error for invalid UUID")
			}
		})
	}
}

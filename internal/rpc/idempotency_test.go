package rpc

import (
	"bytes"
	"testing"
)

// ---- HashRequest ------------------------------------------------------------

func TestHashRequest_Deterministic(t *testing.T) {
	body := []byte(`{"sql":"SELECT 1"}`)
	h1 := HashRequest("POST", "/v1/sql/execute", body)
	h2 := HashRequest("POST", "/v1/sql/execute", body)
	if h1 != h2 {
		t.Errorf("HashRequest not deterministic: %q != %q", h1, h2)
	}
	if h1 == "" {
		t.Error("HashRequest returned empty string")
	}
}

func TestHashRequest_DiffersByMethod(t *testing.T) {
	body := []byte(`{}`)
	if HashRequest("GET", "/v1/me", body) == HashRequest("POST", "/v1/me", body) {
		t.Error("different methods must produce different hashes")
	}
}

func TestHashRequest_DiffersByPath(t *testing.T) {
	body := []byte(`{}`)
	if HashRequest("POST", "/v1/sql/execute", body) == HashRequest("POST", "/v1/rpc/call", body) {
		t.Error("different paths must produce different hashes")
	}
}

func TestHashRequest_DiffersByBody(t *testing.T) {
	h1 := HashRequest("POST", "/v1/sql/execute", []byte(`{"sql":"SELECT 1"}`))
	h2 := HashRequest("POST", "/v1/sql/execute", []byte(`{"sql":"SELECT 2"}`))
	if h1 == h2 {
		t.Error("different bodies must produce different hashes")
	}
}

func TestHashRequest_EmptyBody(t *testing.T) {
	h := HashRequest("GET", "/v1/me", nil)
	if h == "" {
		t.Error("HashRequest with nil body returned empty string")
	}
	// Nil and empty slice must hash identically.
	if HashRequest("GET", "/v1/me", nil) != HashRequest("GET", "/v1/me", []byte{}) {
		t.Error("nil body and empty body must produce the same hash")
	}
}

// ---- gzip helpers -----------------------------------------------------------

func TestGzipRoundTrip(t *testing.T) {
	cases := [][]byte{
		[]byte("hello world"),
		[]byte(`{"sql":"SELECT * FROM t WHERE id = $1","params":[42]}`),
		bytes.Repeat([]byte("repeat"), 1000), // compressible payload
	}
	for _, original := range cases {
		gz, err := gzipBytes(original)
		if err != nil {
			t.Fatalf("gzipBytes: %v", err)
		}
		got, err := gunzip(gz)
		if err != nil {
			t.Fatalf("gunzip: %v", err)
		}
		if !bytes.Equal(got, original) {
			t.Errorf("round-trip mismatch: got %q, want %q", got, original)
		}
	}
}

func TestGunzip_Empty(t *testing.T) {
	got, err := gunzip(nil)
	if err != nil {
		t.Errorf("gunzip(nil): unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("gunzip(nil): got %v, want nil", got)
	}
}

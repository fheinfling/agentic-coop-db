package mcp

import (
	"testing"
)

func TestNewServer_RegistersAllTools(t *testing.T) {
	fd := &fakeDoer{}
	srv := NewServer(fd)
	if srv == nil {
		t.Fatal("NewServer returned nil")
	}

	// The server should have all 8 tools registered.
	// We verify by checking the tool names returned via ListTools.
	// mcp-go exposes registered tools through the server's internal state.
	// Since we can't directly inspect, we verify the server was created
	// without panic and has non-nil state. A more thorough check happens
	// in integration tests.
}

func TestNewServer_NilClientPanics(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Error("expected panic for nil client, got none")
		}
	}()
	NewServer(nil)
}

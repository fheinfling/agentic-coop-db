package mcp

import (
	"sort"
	"testing"
)

func TestNewServer_RegistersAllTools(t *testing.T) {
	fd := &fakeDoer{}
	srv := NewServer(fd)
	if srv == nil {
		t.Fatal("NewServer returned nil")
	}

	tools := srv.ListTools()
	if len(tools) != 8 {
		t.Errorf("registered tools = %d, want 8", len(tools))
	}

	want := []string{
		"describe_table", "health", "list_tables", "rpc_call",
		"sql_execute", "vector_search", "vector_upsert", "whoami",
	}
	var got []string
	for name := range tools {
		got = append(got, name)
	}
	sort.Strings(got)

	if len(got) != len(want) {
		t.Fatalf("tool names = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("tool[%d] = %q, want %q", i, got[i], want[i])
		}
	}
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

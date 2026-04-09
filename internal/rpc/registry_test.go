package rpc

import (
	"testing"
)

func TestRegistry_RegisterNil(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(nil); err == nil {
		t.Error("Register(nil): expected error, got nil")
	}
}

func TestRegistry_RegisterEmptyName(t *testing.T) {
	r := NewRegistry()
	s, _ := compileSchema(`{"type":"object"}`)
	err := r.Register(&Procedure{Name: "", Body: "SELECT 1", Schema: s})
	if err == nil {
		t.Error("Register with empty name: expected error, got nil")
	}
}

func TestRegistry_RegisterNilSchema(t *testing.T) {
	r := NewRegistry()
	err := r.Register(&Procedure{Name: "test_proc", Body: "SELECT 1", Schema: nil})
	if err == nil {
		t.Error("Register with nil schema: expected error, got nil")
	}
}

func TestRegistry_RegisterEmptyBody(t *testing.T) {
	r := NewRegistry()
	s, _ := compileSchema(`{"type":"object"}`)
	err := r.Register(&Procedure{Name: "test_proc", Body: "", Schema: s})
	if err == nil {
		t.Error("Register with empty body: expected error, got nil")
	}
}

func TestRegistry_GetMiss(t *testing.T) {
	r := NewRegistry()
	p, ok := r.Get("nonexistent")
	if ok || p != nil {
		t.Errorf("Get on empty registry: got ok=%v p=%v, want miss", ok, p)
	}
}

func TestRegistry_RegisterAndGet(t *testing.T) {
	r := NewRegistry()
	s, err := compileSchema(`{"type":"object"}`)
	if err != nil {
		t.Fatalf("compileSchema: %v", err)
	}
	proc := &Procedure{
		Name:         "do_thing",
		Body:         "SELECT $1::text",
		Schema:       s,
		RequiredRole: "dbuser",
	}
	if err := r.Register(proc); err != nil {
		t.Fatalf("Register: %v", err)
	}
	got, ok := r.Get("do_thing")
	if !ok {
		t.Fatal("Get after Register: expected hit, got miss")
	}
	if got.Name != "do_thing" || got.RequiredRole != "dbuser" {
		t.Errorf("unexpected procedure: %+v", got)
	}
}

func TestRegistry_Overwrite(t *testing.T) {
	r := NewRegistry()
	s, _ := compileSchema(`{"type":"object"}`)
	first := &Procedure{Name: "p", Body: "SELECT 1", Schema: s, Version: 1}
	second := &Procedure{Name: "p", Body: "SELECT 2", Schema: s, Version: 2}
	if err := r.Register(first); err != nil {
		t.Fatalf("Register first: %v", err)
	}
	if err := r.Register(second); err != nil {
		t.Fatalf("Register second: %v", err)
	}
	got, _ := r.Get("p")
	if got.Version != 2 || got.Body != "SELECT 2" {
		t.Errorf("second registration should overwrite first, got version=%d body=%q", got.Version, got.Body)
	}
}

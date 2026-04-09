package rpc

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// Procedure is one registered RPC.
type Procedure struct {
	Name         string
	Version      int
	Description  string
	RequiredRole string
	BodyPath     string // relative to sql/rpc/
	Body         string // loaded once at startup
	Schema       *jsonschema.Schema
}

// Registry holds the in-memory map of registered procedures.
type Registry struct {
	mu    sync.RWMutex
	procs map[string]*Procedure // key: name (latest version wins)
}

// NewRegistry constructs an empty Registry.
func NewRegistry() *Registry {
	return &Registry{procs: make(map[string]*Procedure)}
}

// Register adds (or overwrites) a procedure.
func (r *Registry) Register(p *Procedure) error {
	if p == nil || p.Name == "" {
		return errors.New("rpc.Register: nil procedure or empty name")
	}
	if p.Schema == nil {
		return errors.New("rpc.Register: procedure has nil JSON schema")
	}
	if p.Body == "" {
		return errors.New("rpc.Register: procedure has empty body")
	}
	r.mu.Lock()
	r.procs[p.Name] = p
	r.mu.Unlock()
	return nil
}

// Get returns the procedure for the given name.
func (r *Registry) Get(name string) (*Procedure, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.procs[name]
	return p, ok
}

// LoadBuiltins registers the procedures shipped under sql/rpc/. The set is
// deliberately tiny: a single upsert_document RPC that doubles as a usage
// example. New procedures are added by writing a sql/rpc/<name>.sql file
// and a corresponding registry entry below.
func LoadBuiltins(reg *Registry) error {
	root, err := bodiesRoot()
	if err != nil {
		return err
	}
	upsertDocPath := filepath.Join(root, "upsert_document.sql")
	body, err := os.ReadFile(upsertDocPath)
	if err != nil {
		// The file is optional — if it is missing, log and continue.
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}

	schemaJSON := `{
        "$schema": "https://json-schema.org/draft/2020-12/schema",
        "type": "object",
        "additionalProperties": false,
        "required": ["id", "body"],
        "properties": {
            "id":   {"type": "string"},
            "body": {"type": "string"}
        }
    }`
	schema, err := compileSchema(schemaJSON)
	if err != nil {
		return fmt.Errorf("compile upsert_document schema: %w", err)
	}
	return reg.Register(&Procedure{
		Name:         "upsert_document",
		Version:      1,
		Description:  "Insert or update a document by id, returning the new row.",
		RequiredRole: "dbuser",
		BodyPath:     "upsert_document.sql",
		Body:         string(body),
		Schema:       schema,
	})
}

func bodiesRoot() (string, error) {
	if d := os.Getenv("AGENTCOOPDB_RPC_DIR"); d != "" {
		return d, nil
	}
	for _, c := range []string{"/app/sql/rpc", "sql/rpc"} {
		if info, err := os.Stat(c); err == nil && info.IsDir() {
			return filepath.Abs(c)
		}
	}
	return "", errors.New("rpc bodies directory not found")
}

func compileSchema(jsonText string) (*jsonschema.Schema, error) {
	var doc any
	if err := json.Unmarshal([]byte(jsonText), &doc); err != nil {
		return nil, fmt.Errorf("schema decode: %w", err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource("inline.json", doc); err != nil {
		return nil, err
	}
	return c.Compile("inline.json")
}

// Command lint-migrations is a CI gate that fails the build for any
// migration that adds a tenant table without an RLS policy.
//
// Heuristic:
//
//  1. Walk every *.up.sql under migrations/.
//  2. Identify CREATE TABLE statements that have a `workspace_id` column.
//  3. For each such table T, the same migration MUST contain:
//     - ALTER TABLE T ENABLE ROW LEVEL SECURITY
//     - ALTER TABLE T FORCE  ROW LEVEL SECURITY
//     - CREATE POLICY ... ON T ... USING (workspace_id = current_setting('app.workspace_id'...
//
// The check is intentionally regex-based: it does not need a full SQL
// parser to be useful, and it stays runnable from a `go run` invocation
// without external dependencies.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var (
	createTableRE  = regexp.MustCompile(`(?is)CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?([a-z_][a-z0-9_]*)\s*\((.*?)\)\s*;`)
	workspaceColRE = regexp.MustCompile(`(?im)^\s*workspace_id\b`)
	enableRLSRE    = regexp.MustCompile(`(?i)ALTER\s+TABLE\s+%s\s+ENABLE\s+ROW\s+LEVEL\s+SECURITY`)
	forceRLSRE     = regexp.MustCompile(`(?i)ALTER\s+TABLE\s+%s\s+FORCE\s+ROW\s+LEVEL\s+SECURITY`)
	policyRE       = regexp.MustCompile(`(?is)CREATE\s+POLICY\s+\S+\s+ON\s+%s\s+.*?current_setting\(\s*'app.workspace_id'`)
)

// controlPlaneTables lists tables that the linter must NOT require RLS
// on, even though they have a workspace_id column. They live in the
// `aicoopdb` schema (moved by migration 0007) and are unreachable from
// any API key role at all — schema-level isolation supersedes RLS for
// these. RLS would be redundant defense against a threat model that
// doesn't apply, and the migrations linter shouldn't enforce it.
//
// Add new control-plane tables here as they are introduced. Tenant
// tables (anything a `dbadmin` or `dbuser` key can reach) are NOT
// allowed in this list — those must always have RLS.
var controlPlaneTables = map[string]bool{
	"api_keys":         true,
	"audit_logs":       true,
	"idempotency_keys": true,
	"rpc_registry":     true,
}

func main() {
	dir := "migrations"
	if len(os.Args) > 1 {
		dir = os.Args[1]
	}
	files, err := upMigrations(dir)
	if err != nil {
		fail(err)
	}
	var problems []string
	for _, f := range files {
		body, err := os.ReadFile(f)
		if err != nil {
			fail(err)
		}
		ps := lintFile(f, string(body))
		problems = append(problems, ps...)
	}
	if len(problems) > 0 {
		for _, p := range problems {
			fmt.Fprintln(os.Stderr, p)
		}
		fmt.Fprintf(os.Stderr, "\n%d migration lint problem(s)\n", len(problems))
		os.Exit(1)
	}
	fmt.Println("migrations OK")
}

func upMigrations(dir string) ([]string, error) {
	var out []string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, ".up.sql") {
			out = append(out, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

func lintFile(path, body string) []string {
	var problems []string
	for _, m := range createTableRE.FindAllStringSubmatch(body, -1) {
		table := m[1]
		colsBlock := m[2]
		if !workspaceColRE.MatchString(colsBlock) {
			continue // not a tenant table
		}
		// Skip the control-plane workspaces table itself — its rows ARE the
		// tenants, so it has no workspace_id column.
		if table == "workspaces" {
			continue
		}
		// Skip other control-plane tables (api_keys, audit_logs, …).
		// They live in the aicoopdb schema after migration 0007 and are
		// unreachable from any API key role, so RLS is unnecessary.
		if controlPlaneTables[table] {
			continue
		}
		if !regexp.MustCompile(fmt.Sprintf(enableRLSRE.String(), regexp.QuoteMeta(table))).MatchString(body) {
			problems = append(problems, fmt.Sprintf("%s: table %q is missing 'ALTER TABLE ... ENABLE ROW LEVEL SECURITY'", path, table))
		}
		if !regexp.MustCompile(fmt.Sprintf(forceRLSRE.String(), regexp.QuoteMeta(table))).MatchString(body) {
			problems = append(problems, fmt.Sprintf("%s: table %q is missing 'ALTER TABLE ... FORCE ROW LEVEL SECURITY'", path, table))
		}
		if !regexp.MustCompile(fmt.Sprintf(policyRE.String(), regexp.QuoteMeta(table))).MatchString(body) {
			problems = append(problems, fmt.Sprintf("%s: table %q is missing a workspace isolation policy keyed on current_setting('app.workspace_id', true)::uuid", path, table))
		}
	}
	return problems
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}

// Package version exposes build-time identifiers injected via -ldflags.
//
// The Makefile sets these from `git describe`, the short commit, and the
// build date. They are surfaced through the /v1/me and /healthz responses
// and the prometheus build_info metric.
package version

// Version is the semantic version (e.g. "0.1.0", "0.1.0-dev", "v0.1.0-3-gabc").
var Version = "0.0.0-dev"

// Commit is the short git SHA the binary was built from.
var Commit = "unknown"

// BuildDate is the UTC RFC3339 timestamp of the build.
var BuildDate = "unknown"

// Info bundles the build identifiers in one struct, useful for JSON responses.
type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildDate string `json:"build_date"`
}

// Get returns a populated Info value.
func Get() Info {
	return Info{Version: Version, Commit: Commit, BuildDate: BuildDate}
}

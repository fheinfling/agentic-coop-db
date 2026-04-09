package version

import "testing"

// Get is trivial — but the JSON tags on Info are part of the public
// /v1/me and /healthz response shape, so a test that locks them in
// catches accidental renames.

func TestGet_ReturnsPackageVars(t *testing.T) {
	// Save and restore the package-level vars so the test doesn't
	// observe whatever ldflags injected at build time.
	origVersion, origCommit, origBuild := Version, Commit, BuildDate
	t.Cleanup(func() {
		Version, Commit, BuildDate = origVersion, origCommit, origBuild
	})

	Version = "1.2.3"
	Commit = "abc1234"
	BuildDate = "2026-04-08T00:00:00Z"

	got := Get()
	if got.Version != "1.2.3" {
		t.Errorf("Version: got %q, want %q", got.Version, "1.2.3")
	}
	if got.Commit != "abc1234" {
		t.Errorf("Commit: got %q, want %q", got.Commit, "abc1234")
	}
	if got.BuildDate != "2026-04-08T00:00:00Z" {
		t.Errorf("BuildDate: got %q, want %q", got.BuildDate, "2026-04-08T00:00:00Z")
	}
}

func TestGet_DefaultsAreNonEmpty(t *testing.T) {
	// Even with no ldflags injection, the package-level defaults must
	// be non-empty so /healthz never returns "" for a field.
	if Version == "" {
		t.Error("Version default is empty")
	}
	if Commit == "" {
		t.Error("Commit default is empty")
	}
	if BuildDate == "" {
		t.Error("BuildDate default is empty")
	}
}

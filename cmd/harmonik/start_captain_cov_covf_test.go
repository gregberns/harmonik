package main

// start_captain_cov_covf_test.go — coverage-drain chunk F: two small pure-logic
// resolvers the existing start_test.go / captain_*_test.go leave partly uncovered:
//
//   - startResolveProjectDir  --project= (equals form) + the no-flag cwd fallback
//   - captainTmuxSessionName   the explicit --tmux override branch (no hashing)
//
// The hashed-namespace / EvalSymlinks-fallback legs of captainTmuxSessionName
// depend on lifecycle.ComputeProjectHash over a real dir and are covered by the
// captain_launch_* tests; only the explicit-override short-circuit is added here.

import (
	"os"
	"testing"
)

// TestStartResolveProjectDir covers the space form, the equals form, and the
// cwd fallback when no --project is present.
func TestStartResolveProjectDir(t *testing.T) {
	if got := startResolveProjectDir([]string{"crew", "--project", "/space/dir"}); got != "/space/dir" {
		t.Errorf("space form: got %q, want /space/dir", got)
	}
	if got := startResolveProjectDir([]string{"crew", "--project=/eq/dir"}); got != "/eq/dir" {
		t.Errorf("equals form: got %q, want /eq/dir", got)
	}

	// No --project → cwd.
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if got := startResolveProjectDir([]string{"captain"}); got != wd {
		t.Errorf("no --project: got %q, want cwd %q", got, wd)
	}
}

// TestCaptainTmuxSessionName_ExplicitOverride: an explicit --tmux value wins and
// is returned verbatim without hashing the project dir.
func TestCaptainTmuxSessionName_ExplicitOverride(t *testing.T) {
	got, err := captainTmuxSessionName("my-session", "/whatever/project")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "my-session" {
		t.Errorf("explicit override: got %q, want my-session", got)
	}
}

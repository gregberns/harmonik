package main

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

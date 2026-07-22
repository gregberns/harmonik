package main

// captain_gocache_hk137y6_test.go — the captain/oversight tmux launch must
// carry the fixed per-agent GOCACHE pin (bead hk-137y6).
//
// The crew path and the captain path are separate launchers. Fixing one and not
// the other leaves every oversight session — captain, commodore, admiral — still
// inheriting Go's default cache and still following the `GOCACHE=$(mktemp -d)`
// guidance that leaked 7.3 GiB in a night. This is the wiring test for the half
// that is easy to forget.

import (
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/daemon"
)

// TestBuildCaptainTmuxCmd_PinsGoCache asserts the assembled tmux argv passes the
// agent's fixed cache path via `-e GOCACHE=...`.
func TestBuildCaptainTmuxCmd_PinsGoCache(t *testing.T) {
	t.Parallel()

	project := t.TempDir()
	cmd := buildCaptainTmuxCmd("captain", "test-session", "11111111-2222-4333-8444-555555555555", "", project)

	want := "GOCACHE=" + daemon.GoCacheDirFor(project, "captain")
	for _, a := range cmd.Args {
		if a == want {
			return
		}
	}
	t.Errorf("captain tmux argv = %v; want it to contain %q — without the pin an oversight session inherits Go's "+
		"default cache and either leaks a fresh one per command or gets wiped mid-build (hk-137y6)",
		cmd.Args, want)
}

// TestBuildCaptainTmuxCmd_GoCacheIsNotPurgeable guards the property that made
// hk-pgtbr's merge gate fail with errors inside Go's own standard library: a
// cache under ~/Library/Caches is reclaimed wholesale by macOS under disk
// pressure, mid-build, with no warning.
func TestBuildCaptainTmuxCmd_GoCacheIsNotPurgeable(t *testing.T) {
	t.Parallel()

	project := t.TempDir()
	cmd := buildCaptainTmuxCmd("captain", "test-session", "11111111-2222-4333-8444-555555555555", "", project)

	for _, a := range cmd.Args {
		if strings.HasPrefix(a, "GOCACHE=") && strings.Contains(a, "Library/Caches") {
			t.Errorf("captain tmux argv pins %q — that path is macOS-purgeable and vanishes mid-build (hk-pgtbr)", a)
		}
	}
}

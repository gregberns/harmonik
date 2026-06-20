package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/keeper"
)

// keeper_cli_hardening_hkx7s_test.go — W5/W7 keeper CLI hardening (hk-x7s).
//
// Covers:
//   W5 — marker verbs Abs-normalize --project (relative resolves to the SAME
//        .harmonik/keeper/ dir as absolute), and set-dispatching emits a non-fatal
//        WARNING (exit stays 0) when no live keeper is watching the resolved dir.
//   W7 — an unknown keeper subcommand exits non-zero with an "unknown keeper
//        subcommand" message rather than the misleading "flag-only" fall-through.

// TestSetDispatching_RelativeProjectResolvesLikeAbsolute pins the W5 Abs-normalize
// fix: a marker verb invoked with a RELATIVE --project writes the same marker file
// an ABSOLUTE --project would, so two commands that "agree" on --project touch the
// same dir as the os.Getwd()-absolute watcher.
func TestSetDispatching_RelativeProjectResolvesLikeAbsolute(t *testing.T) {
	// Not parallel: chdir mutates process-global CWD.
	absProject := t.TempDir()
	agent := "rel-abs-agent"

	origWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWd) })

	// CD to the parent so a relative "<base>" path names absProject.
	parent := filepath.Dir(absProject)
	base := filepath.Base(absProject)
	if err := os.Chdir(parent); err != nil {
		t.Fatalf("Chdir(%q): %v", parent, err)
	}

	// Invoke with a RELATIVE --project.
	if code := runKeeperSetDispatching([]string{"--project", base, "--agent", agent}); code != 0 {
		t.Fatalf("set-dispatching with relative --project: want exit 0, got %d", code)
	}

	// The marker must land under the ABSOLUTE project dir (Abs-normalized), i.e.
	// exactly where the watcher (which uses os.Getwd()-absolute) would look.
	markerPath := filepath.Join(absProject, ".harmonik", "keeper", agent+".dispatching")
	if _, statErr := os.Stat(markerPath); statErr != nil {
		t.Fatalf("relative --project did not resolve to the absolute dir: marker not at %q: %v", markerPath, statErr)
	}

	// And HoldingDispatch (which the watcher consults with the absolute dir) sees it.
	if !keeper.HoldingDispatch(absProject, agent) {
		t.Error("HoldingDispatch(absolute): want true — relative --project must Abs-normalize to the same dir")
	}
}

// TestSetDispatching_WarnsWhenNoLiveKeeper pins the W5 fail-open WARNING: writing
// the marker still succeeds (exit 0), but stderr carries an advisory warning when
// no live keeper holds the <agent>.lock for the resolved project+agent.
func TestSetDispatching_WarnsWhenNoLiveKeeper(t *testing.T) {
	projectDir := t.TempDir()
	agent := "unwatched-agent"

	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	origStderr := os.Stderr
	os.Stderr = pw

	code := runKeeperSetDispatching([]string{"--project", projectDir, "--agent", agent})

	pw.Close()
	os.Stderr = origStderr

	buf := make([]byte, 8192)
	n, _ := pr.Read(buf)
	pr.Close()
	output := string(buf[:n])

	// Fail-OPEN: exit stays 0 — a keeper may legitimately start later.
	if code != 0 {
		t.Fatalf("set-dispatching with no live keeper: want exit 0 (fail-open), got %d", code)
	}
	if !strings.Contains(output, "WARNING") || !strings.Contains(output, "no live keeper") {
		t.Errorf("expected a 'no live keeper' WARNING on stderr; got:\n%s", output)
	}
}

// TestUnknownKeeperSubcommand_ExitsNonZero pins the W7 default: case: a typo'd
// verb exits non-zero with a clear "unknown keeper subcommand" message instead of
// degrading to the watcher-mode "flag-only" fall-through.
func TestUnknownKeeperSubcommand_ExitsNonZero(t *testing.T) {
	mainFixtureResetFlags(t)
	mainFixtureSaveRestoreArgs(t, []string{"harmonik", "keeper", "restrt-now", "--agent", "captain"})

	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	origStderr := os.Stderr
	os.Stderr = pw

	code := run()

	pw.Close()
	os.Stderr = origStderr

	buf := make([]byte, 16384)
	n, _ := pr.Read(buf)
	pr.Close()
	output := string(buf[:n])

	if code == 0 {
		t.Fatalf("unknown keeper subcommand: want non-zero exit, got 0")
	}
	if !strings.Contains(output, "unknown keeper subcommand") {
		t.Errorf("expected 'unknown keeper subcommand' message; got:\n%s", output)
	}
	if strings.Contains(output, "flag-only") {
		t.Errorf("unknown verb must NOT print the misleading 'flag-only' message; got:\n%s", output)
	}
}

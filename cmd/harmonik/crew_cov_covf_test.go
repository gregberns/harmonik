package main

// crew_cov_covf_test.go — coverage-drain chunk F: pure-logic + offline surface of
// crew.go that the existing crew_*_test.go files leave uncovered:
//
//   - runCrewSubcommand         verb routing (help / unknown)
//   - runCrewListSubcommand     the fully-offline read path (empty / records /
//                               --json / arg errors), which needs no daemon
//   - crewResolveSockPath       socket-flag override vs. project-join resolution
//   - crewUsage / crewListUsage banners (reached via the help routing)
//
// The daemon-RPC verbs (start/stop) are exercised in crew_stop_rpc_covf_test.go.
// The actual tmux/keeper spawn on `crew start` is NOT covered here (out of scope:
// it shells to a real tmux server + claude session).
//
// These helpers mutate os.Stdout/os.Stderr, so NONE of these tests use t.Parallel.

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/crew"
)

// silenceStderr redirects os.Stderr to /dev/null for the test, restoring it on
// cleanup. Unlike vgSilenceStd it leaves os.Stdout live, so a test can still
// capture stdout via captureStdoutDuring while suppressing usage-banner noise on
// stderr. Distinct from — not a re-implementation of — the daemon toolkit.
func silenceStderr(t *testing.T) {
	t.Helper()
	devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open /dev/null: %v", err)
	}
	old := os.Stderr
	os.Stderr = devnull
	t.Cleanup(func() {
		os.Stderr = old
		_ = devnull.Close()
	})
}

// TestRunCrewSubcommand_Routing covers the verb switch in runCrewSubcommand:
// the empty/help verbs print usage and exit 0; an unrecognised verb exits 2.
func TestRunCrewSubcommand_Routing(t *testing.T) {
	silenceStderr(t)

	// Empty verb → crewUsage on stdout, exit 0.
	var code int
	out := captureStdoutDuring(t, func() { code = runCrewSubcommand(nil) })
	if code != 0 {
		t.Fatalf("empty verb: exit = %d, want 0", code)
	}
	if !strings.Contains(out, "harmonik crew") {
		t.Errorf("empty verb: expected usage banner on stdout, got %q", out)
	}

	// --help → exit 0.
	out = captureStdoutDuring(t, func() { code = runCrewSubcommand([]string{"--help"}) })
	if code != 0 {
		t.Fatalf("--help: exit = %d, want 0", code)
	}
	if !strings.Contains(out, "VERBS") {
		t.Errorf("--help: expected usage banner, got %q", out)
	}

	// Unknown verb → exit 2 (stderr silenced).
	if code = runCrewSubcommand([]string{"frobnicate"}); code != 2 {
		t.Fatalf("unknown verb: exit = %d, want 2", code)
	}
}

// TestRunCrewListSubcommand_Empty: an absent .harmonik/crew/ dir yields an empty
// list and exit 0 (a note goes to stderr in the non-JSON case).
func TestRunCrewListSubcommand_Empty(t *testing.T) {
	silenceStderr(t)
	dir := t.TempDir()
	if code := runCrewListSubcommand([]string{"--project", dir}); code != 0 {
		t.Fatalf("empty list: exit = %d, want 0", code)
	}
}

// TestRunCrewListSubcommand_Records plants two crew records and asserts both the
// human-readable and --json (NDJSON) renderings list them sorted by name.
func TestRunCrewListSubcommand_Records(t *testing.T) {
	silenceStderr(t)
	dir := t.TempDir()

	for _, r := range []crew.Record{
		{Name: "beta", Queue: "beta-q", SessionID: "sid-beta", Handle: "hbeta", StartedAt: time.Unix(1700000000, 0).UTC()},
		{Name: "alpha", Queue: "alpha-q", SessionID: "sid-alpha", StartedAt: time.Unix(1700000100, 0).UTC()},
	} {
		if err := crew.Write(dir, r); err != nil {
			t.Fatalf("plant record %q: %v", r.Name, err)
		}
	}

	// Human-readable: sorted alpha before beta; the empty handle renders "(no handle)".
	out := captureStdoutDuring(t, func() {
		if code := runCrewListSubcommand([]string{"--project", dir}); code != 0 {
			t.Fatalf("list: exit = %d, want 0", code)
		}
	})
	ai, bi := strings.Index(out, "alpha"), strings.Index(out, "beta")
	if ai < 0 || bi < 0 {
		t.Fatalf("list output missing a crew name: %q", out)
	}
	if ai > bi {
		t.Errorf("records not sorted by name (alpha should precede beta): %q", out)
	}
	if !strings.Contains(out, "(no handle)") {
		t.Errorf("empty handle should render as (no handle): %q", out)
	}

	// --json: one JSON object per line, each decodes and carries its name.
	out = captureStdoutDuring(t, func() {
		if code := runCrewListSubcommand([]string{"--json", "--project", dir}); code != 0 {
			t.Fatalf("list --json: exit = %d, want 0", code)
		}
	})
	var names []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		var rec crew.Record
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("decode NDJSON line %q: %v", line, err)
		}
		names = append(names, rec.Name)
	}
	if len(names) != 2 || names[0] != "alpha" || names[1] != "beta" {
		t.Errorf("NDJSON names = %v, want [alpha beta]", names)
	}
}

// TestRunCrewListSubcommand_ArgErrors covers the arg-validation exits: help (0),
// an unknown flag (1), and an unexpected positional (1).
func TestRunCrewListSubcommand_ArgErrors(t *testing.T) {
	silenceStderr(t)

	var code int
	out := captureStdoutDuring(t, func() { code = runCrewListSubcommand([]string{"--help"}) })
	if code != 0 {
		t.Fatalf("--help: exit = %d, want 0", code)
	}
	if !strings.Contains(out, "harmonik crew list") {
		t.Errorf("--help: expected list usage banner, got %q", out)
	}

	if code = runCrewListSubcommand([]string{"--bogus"}); code != 1 {
		t.Fatalf("unknown flag: exit = %d, want 1", code)
	}
	if code = runCrewListSubcommand([]string{"stray"}); code != 1 {
		t.Fatalf("unexpected positional: exit = %d, want 1", code)
	}
}

// TestCrewResolveSockPath covers both resolution branches: an explicit --socket
// override is returned verbatim; otherwise the path is joined under the project.
func TestCrewResolveSockPath(t *testing.T) {
	silenceStderr(t)

	if got := crewResolveSockPath("/custom/daemon.sock", ""); got != "/custom/daemon.sock" {
		t.Errorf("socket override: got %q, want /custom/daemon.sock", got)
	}

	got := crewResolveSockPath("", "/proj/dir")
	if !strings.HasSuffix(got, "/.harmonik/daemon.sock") {
		t.Errorf("project-join: got %q, want a .harmonik/daemon.sock suffix", got)
	}
	if !strings.HasPrefix(got, "/proj/dir") {
		t.Errorf("project-join: got %q, want it rooted at /proj/dir", got)
	}
}

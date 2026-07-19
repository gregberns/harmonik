package main

// wave4_cli_fixes_test.go — regression tests for the Wave-4 mega-review §c
// CLI correctness fixes:
//
//   - >64KB valid event lines must not abort NDJSON scans (scanner buffer).
//   - smoke Signal 3 must pass on the commit the smoke task actually writes.
//   - queue-already-active append fallback attributes exit to the CALLER's beads.
//   - init / gc branches reject unknown flags (fail closed).
//   - eval collect re-run does not double-count (run_id dedup).
//   - SH-002 rejects uppercase .YML too; digest short-ID does not panic.

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
)

// injectAndWatchBeads mirrors injectAndWatch but passes a watchBeads set
// (append-fallback attribution mode).
func injectAndWatchBeads(t *testing.T, ndjsonLines []string, queueID string, beads []core.BeadID) int {
	t.Helper()
	server, client := net.Pipe()
	defer func() { _ = server.Close() }() //nolint:errcheck // test teardown; pipe close error is non-actionable

	go func() {
		defer func() { _ = client.Close() }() //nolint:errcheck // test teardown; pipe close error is non-actionable
		for _, line := range ndjsonLines {
			if _, err := fmt.Fprintln(client, line); err != nil {
				return
			}
		}
	}()

	return viaWatchGroupCompletion(server, queueID, 0, beads, nil)
}

// TestViaWatchGroupCompletion_LargeEventLine verifies that a valid event line
// far beyond bufio.Scanner's 64KB default (and the old 512KB cap) does not
// abort the scan before the completion event arrives.
func TestViaWatchGroupCompletion_LargeEventLine(t *testing.T) {
	t.Parallel()

	big := strings.Repeat("x", 1024*1024) // 1MB payload field
	lines := []string{
		`{"type":"heartbeat","payload":{"junk":"` + big + `"}}`,
		`{"type":"queue_group_completed","payload":{"queue_id":"q1","group_index":0,"final_status":"complete-success"}}`,
	}
	if code := injectAndWatch(t, lines, "q1", 0); code != 0 {
		t.Fatalf("exit code = %d, want 0 (large line must not abort the scan)", code)
	}
}

// TestViaWatchGroupCompletion_AppendFallbackAttribution verifies that on the
// append-fallback path the exit code reflects the caller's OWN beads, not the
// outcome of unrelated beads sharing group 0.
func TestViaWatchGroupCompletion_AppendFallbackAttribution(t *testing.T) {
	t.Parallel()

	// Our bead succeeds; an unrelated bead in the same group fails.
	lines := []string{
		`{"type":"run_started","payload":{"run_id":"r-ours","bead_id":"hk-ours"}}`,
		`{"type":"run_started","payload":{"run_id":"r-other","bead_id":"hk-other"}}`,
		`{"type":"run_failed","payload":{"run_id":"r-other"}}`,
		`{"type":"run_completed","payload":{"run_id":"r-ours"}}`,
	}
	if code := injectAndWatchBeads(t, lines, "q1", []core.BeadID{"hk-ours"}); code != 0 {
		t.Fatalf("exit code = %d, want 0 (unrelated bead's failure must not be attributed to us)", code)
	}

	// Converse: our bead fails while the rest of the group succeeds.
	lines = []string{
		`{"type":"run_started","payload":{"run_id":"r-ours","bead_id":"hk-ours"}}`,
		`{"type":"run_failed","payload":{"run_id":"r-ours"}}`,
		`{"type":"queue_group_completed","payload":{"queue_id":"q1","group_index":0,"final_status":"complete-with-failures"}}`,
	}
	if code := injectAndWatchBeads(t, lines, "q1", []core.BeadID{"hk-ours"}); code != 1 {
		t.Fatalf("exit code = %d, want 1 (our bead failed)", code)
	}

	// No run events for our bead at all: fall back to group outcome.
	lines = []string{
		`{"type":"queue_group_completed","payload":{"queue_id":"q1","group_index":0,"final_status":"complete-success"}}`,
	}
	if code := injectAndWatchBeads(t, lines, "q1", []core.BeadID{"hk-ours"}); code != 0 {
		t.Fatalf("exit code = %d, want 0 (group-success fallback for unobserved bead)", code)
	}
}

// TestSmokeCheckCommitOnBranch_MatchesActualSmokeCommit verifies Signal 3
// passes on the commit message the smoke task is instructed to write —
// with or without the Refs: trailer.
func TestSmokeCheckCommitOnBranch_MatchesActualSmokeCommit(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir}, args...)...) //nolint:gosec // G204: git args are test-controlled
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "smoke-log.md"), []byte("smoke\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	// The commit the smoke task actually writes: subject only, no Refs trailer.
	run("commit", "-m", "smoke(hk-smoke1): 5-signal verification")

	var stderr bytes.Buffer
	ok, sha := smokeCheckCommitOnBranch(dir, "main", "hk-smoke1", &stderr)
	if !ok || sha == "" {
		t.Fatalf("smokeCheckCommitOnBranch = (%v, %q), want match on subject-form commit; stderr: %s",
			ok, sha, stderr.String())
	}

	// A commit that only carries the Refs: trailer must also match.
	run("commit", "--allow-empty", "-m", "chore: something\n\nRefs: hk-smoke2")
	if ok, _ := smokeCheckCommitOnBranch(dir, "main", "hk-smoke2", &stderr); !ok {
		t.Fatal("smokeCheckCommitOnBranch: want match on Refs:-trailer commit")
	}

	// And an absent bead must not match.
	if ok, _ := smokeCheckCommitOnBranch(dir, "main", "hk-absent", &stderr); ok {
		t.Fatal("smokeCheckCommitOnBranch: matched a bead with no commit")
	}
}

// TestInit_UnknownFlagRejected verifies `harmonik init` fails closed on a
// mistyped flag instead of silently bootstrapping against defaults.
func TestInit_UnknownFlagRejected(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	if code := runInit([]string{"--target-branc", "integration"}, &stdout, &stderr); code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "unknown argument") {
		t.Fatalf("stderr = %q, want unknown-argument error", stderr.String())
	}
}

// TestBranchReap_UnknownFlagRejected verifies `harmonik gc branches` fails
// closed on a mistyped flag (e.g. --dryrun) instead of running a live reap.
func TestBranchReap_UnknownFlagRejected(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	if code := runBranchReapSubcommand([]string{"--dryrun"}, &stdout, &stderr); code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "unknown argument") {
		t.Fatalf("stderr = %q, want unknown-argument error", stderr.String())
	}
}

// TestEvalCollect_RerunDoesNotDoubleCount verifies that running
// `harmonik eval collect` twice over the same events file emits each eval
// run's record exactly once.
func TestEvalCollect_RerunDoesNotDoubleCount(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	outputPath := filepath.Join(dir, "eval-results.jsonl")

	events := strings.Join([]string{
		`{"type":"run_started","run_id":"r1","payload":{"started_at":"2026-07-17T00:00:00Z"}}`,
		`{"type":"node_dispatch_requested","run_id":"r1","payload":{"node_id":"grade"}}`,
		`{"type":"run_completed","run_id":"r1","timestamp_wall":"2026-07-17T00:05:00Z","payload":{"ended_at":"2026-07-17T00:05:00Z"}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(eventsPath, []byte(events), 0o600); err != nil {
		t.Fatal(err)
	}

	getwd := func() (string, error) { return dir, nil }
	args := []string{"--project", dir, "--events-file", eventsPath, "--output", outputPath}

	var out1, err1 bytes.Buffer
	if code := runEvalCollect(args, &out1, &err1, getwd); code != 0 {
		t.Fatalf("first collect: exit %d; stderr: %s", code, err1.String())
	}
	var out2, err2 bytes.Buffer
	if code := runEvalCollect(args, &out2, &err2, getwd); code != 0 {
		t.Fatalf("second collect: exit %d; stderr: %s", code, err2.String())
	}

	data, err := os.ReadFile(outputPath) //nolint:gosec // G304: outputPath is a test-controlled temp path
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Count(string(data), `"run_id":"r1"`)
	if got != 1 {
		t.Fatalf("run r1 appears %d time(s) in output after re-run, want exactly 1\noutput:\n%s", got, data)
	}
}

// TestHarnessDiscoverScenarios_UppercaseYMLRejected_SH002 verifies the
// uppercase .YML variant is also rejected (previously only .yml and .YAML).
func TestHarnessDiscoverScenarios_UppercaseYMLRejected_SH002(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	scenariosDir := filepath.Join(root, "scenarios")
	if err := os.MkdirAll(scenariosDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scenariosDir, "bad.YML"), []byte("name: x\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, errs := harnessDiscoverScenarios(root, nil, "all", false, nil)
	found := false
	for _, e := range errs {
		if strings.Contains(e.Error(), "scenario-load-failure") && strings.Contains(e.Error(), ".YML") {
			found = true
		}
	}
	if !found {
		t.Fatalf("want SH-002 scenario-load-failure for .YML, got errors: %v", errs)
	}
}

// TestDigestShortID_NoPanicOnShortIDs guards the human digest against
// slicing IDs shorter than 8 characters.
func TestDigestShortID_NoPanicOnShortIDs(t *testing.T) {
	t.Parallel()

	if got := digestShortID("abc"); got != "abc" {
		t.Fatalf("digestShortID(short) = %q, want %q", got, "abc")
	}
	if got := digestShortID("0123456789"); got != "01234567" {
		t.Fatalf("digestShortID(long) = %q, want first 8", got)
	}
}

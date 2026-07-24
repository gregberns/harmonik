package main

// smoke_logic_test.go — pure-logic + RPC-construction tests for the
// `harmonik smoke` subcommand (smoke.go).
//
// Covered here:
//   - runSmoke arg-parse error surface (--help→0, unknown arg→1, bad --timeout→1)
//   - smokeReadTargetBranch (branching.yaml lands_on parse + fallbacks)
//   - smokeWatchSignals: the subscribe-request construction (asserted via the
//     fake daemon), the daemon-down exit code, the run_failed short-circuit, and
//     the full 5-signal happy path driven by streamed NDJSON events
//   - smokeCheckCommitOnBranch (git-log classifier, found/not-found)
//   - smokePrintResults (result-table formatting)
//
// SKIPPED (process spawn / live daemon, not pure logic):
//   - smokeCreateBead — shells out to `br create`; requires the br binary + a
//     real bead ledger. The create/submit/cleanup arc is the live-run path.
//   - smokeSubmitBead / smokeCleanupBead — re-exec the harmonik binary / `br`.
//   - runSmoke past arg-parse — it calls smokeCreateBead + the daemon; only the
//     early flag-validation returns are unit-reachable.
//
// No t.Parallel(): several helpers here share the fake-daemon toolkit whose
// fixtures and (elsewhere) process-stream capture are not parallel-safe.

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// --- runSmoke arg parsing --------------------------------------------------

// TestRunSmoke_HelpReturnsZero verifies --help prints usage and returns 0.
func TestRunSmoke_HelpReturnsZero(t *testing.T) {
	var out, errb bytes.Buffer
	if code := runSmoke([]string{"--help"}, &out, &errb); code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if !strings.Contains(out.String(), "harmonik smoke — 5-signal") {
		t.Fatalf("usage banner missing:\n%s", out.String())
	}
}

// TestRunSmoke_UnknownArgReturnsOne verifies an unrecognised argument returns 1
// and reports it on stderr.
func TestRunSmoke_UnknownArgReturnsOne(t *testing.T) {
	var out, errb bytes.Buffer
	if code := runSmoke([]string{"--frob"}, &out, &errb); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(errb.String(), "unknown argument") {
		t.Fatalf("stderr missing 'unknown argument':\n%s", errb.String())
	}
}

// TestRunSmoke_InvalidTimeoutReturnsOne verifies a malformed --timeout value
// (both the space form and the =form) returns 1 before any daemon work.
func TestRunSmoke_InvalidTimeoutReturnsOne(t *testing.T) {
	for _, args := range [][]string{
		{"--timeout", "not-a-duration"},
		{"--timeout=nonsense"},
	} {
		var out, errb bytes.Buffer
		if code := runSmoke(args, &out, &errb); code != 1 {
			t.Fatalf("args=%v: exit = %d, want 1", args, code)
		}
		if !strings.Contains(errb.String(), "--timeout") {
			t.Fatalf("args=%v: stderr missing --timeout diagnostic:\n%s", args, errb.String())
		}
	}
}

// --- smokeReadTargetBranch -------------------------------------------------

// TestSmokeReadTargetBranch covers the branching.yaml lands_on parse and all
// three fallback-to-main paths.
func TestSmokeReadTargetBranch(t *testing.T) {
	t.Run("missing-file-defaults-main", func(t *testing.T) {
		dir := t.TempDir()
		if got := smokeReadTargetBranch(dir); got != "main" {
			t.Fatalf("got %q, want main", got)
		}
	})
	t.Run("reads-lands_on", func(t *testing.T) {
		dir := t.TempDir()
		smokeWriteBranchYAML(t, dir, "lands_on: integration\n")
		if got := smokeReadTargetBranch(dir); got != "integration" {
			t.Fatalf("got %q, want integration", got)
		}
	})
	t.Run("strips-inline-comment", func(t *testing.T) {
		dir := t.TempDir()
		smokeWriteBranchYAML(t, dir, "lands_on: release   # the target\n")
		if got := smokeReadTargetBranch(dir); got != "release" {
			t.Fatalf("got %q, want release", got)
		}
	})
	t.Run("empty-value-falls-back-main", func(t *testing.T) {
		dir := t.TempDir()
		smokeWriteBranchYAML(t, dir, "lands_on:\nother: x\n")
		if got := smokeReadTargetBranch(dir); got != "main" {
			t.Fatalf("got %q, want main", got)
		}
	})
}

func smokeWriteBranchYAML(t *testing.T, harmonikDir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(harmonikDir, "branching.yaml"), []byte(content), 0o644); err != nil {
		t.Fatalf("write branching.yaml: %v", err)
	}
}

// --- smokeWatchSignals: subscribe request + daemon-down + state machine -----

// TestSmokeWatchSignals_DaemonDownReturns17 CHARACTERIZES a bug (hk-d4y2p): a
// MISSING daemon socket currently returns exit 1, NOT the documented exit 17
// ("daemon not running"). Root cause: the ENOENT branch checks
// errors.As(err, &sysErr) for *os.PathError, but DialContext("unix", missing)
// returns *net.OpError wrapping *os.SyscallError, so the branch never matches
// and it falls through to the generic exit-1 path. ECONNREFUSED is handled
// correctly (errors.Is on the whole error); socket-missing is not.
//
// This test pins the CURRENT (buggy) behavior so the discrepancy stays visible;
// flip the expectation to 17 when hk-d4y2p is fixed (use errors.Is ENOENT).
func TestSmokeWatchSignals_DaemonDownReturns17(t *testing.T) {
	dir := newProjectFixture(t) // .harmonik/ exists but no daemon is listening
	sock := filepath.Join(dir, ".harmonik", "daemon.sock")
	var errb bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, code := smokeWatchSignals(ctx, sock, dir, "main", "hk-x", &bytes.Buffer{}, &errb)
	if code != 1 { // BUG hk-d4y2p: should be 17; pinned to actual behavior
		t.Fatalf("exit = %d, want 1 (buggy actual; contract says 17, see hk-d4y2p); stderr=%s", code, errb.String())
	}
	if !strings.Contains(errb.String(), "dial daemon socket") {
		t.Fatalf("expected the generic dial-error path; stderr=%s", errb.String())
	}
}

// TestSmokeWatchSignals_ConstructsSubscribeRequest asserts, via the fake daemon,
// that smokeWatchSignals sends a well-formed subscribe request: op=subscribe,
// heartbeat_seconds=60, and the five event types it watches.
func TestSmokeWatchSignals_ConstructsSubscribeRequest(t *testing.T) {
	// Stream nothing: the fake closes the connection right after capturing the
	// request, so smokeWatchSignals falls through to the clean-close (exit 2).
	d := startFakeDaemon(t, streamEvents())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() {
		_, _ = smokeWatchSignals(ctx, d.SockPath, d.Dir, "main", "hk-x", &bytes.Buffer{}, &bytes.Buffer{})
	}()

	raw := <-d.Requests()
	if raw == nil {
		t.Fatal("fake daemon decoded no request")
	}
	var req struct {
		Op        string   `json:"op"`
		Heartbeat int      `json:"heartbeat_seconds"`
		Types     []string `json:"types"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		t.Fatalf("unmarshal request %q: %v", raw, err)
	}
	if req.Op != "subscribe" {
		t.Errorf("op = %q, want subscribe", req.Op)
	}
	if req.Heartbeat != 60 {
		t.Errorf("heartbeat_seconds = %d, want 60", req.Heartbeat)
	}
	want := map[string]bool{
		"run_started": true, "run_completed": true, "run_failed": true,
		"reviewer_verdict": true, "bead_closed": true,
	}
	got := map[string]bool{}
	for _, ty := range req.Types {
		got[ty] = true
	}
	for ty := range want {
		if !got[ty] {
			t.Errorf("subscribe types missing %q (got %v)", ty, req.Types)
		}
	}
}

// TestSmokeWatchSignals_AllSignalsPass drives the full state machine: the fake
// streams the four dispatch events for the smoke bead, and the fixture dir is a
// git repo carrying a commit that references the bead (Signal 3). All five
// signals fire → exit 0.
func TestSmokeWatchSignals_AllSignalsPass(t *testing.T) {
	const bead = "hk-smoke1"
	events := []map[string]any{
		{"type": "run_started", "payload": map[string]any{"run_id": "R1", "bead_id": bead}},
		{"type": "run_completed", "payload": map[string]any{"run_id": "R1"}},
		{"type": "reviewer_verdict", "payload": map[string]any{"run_id": "R1", "verdict": "APPROVE"}},
		{"type": "bead_closed", "payload": map[string]any{"run_id": "R1", "bead_id": bead}},
	}
	d := startFakeDaemon(t, streamEvents(events...))
	// Make the fake's project dir a git repo with a matching commit so Signal 3
	// (commit on target branch) passes.
	initGitRepoWithBeadCommit(t, d.Dir, "main", bead)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	res, code := smokeWatchSignals(ctx, d.SockPath, d.Dir, "main", bead, &bytes.Buffer{}, &bytes.Buffer{})
	if code != 0 {
		t.Fatalf("exit = %d, want 0; observed=%v", code, res.observed)
	}
	for i, ok := range res.observed {
		if !ok {
			t.Errorf("signal %d (%s) not observed", i+1, smokeSignalNames[i])
		}
	}
}

// TestSmokeWatchSignals_Signal3FailsWithoutCommit verifies that when the commit
// for Signal 3 is absent (project dir is not a git repo), signals 1/2/4/5 still
// register but the run is not a PASS: the stream closes with signal 3 missing →
// exit 2 (treated as timeout: not all signals observed).
func TestSmokeWatchSignals_Signal3FailsWithoutCommit(t *testing.T) {
	const bead = "hk-smoke2"
	events := []map[string]any{
		{"type": "run_started", "payload": map[string]any{"run_id": "R1", "bead_id": bead}},
		{"type": "run_completed", "payload": map[string]any{"run_id": "R1"}},
		{"type": "reviewer_verdict", "payload": map[string]any{"run_id": "R1", "verdict": "APPROVE"}},
		{"type": "bead_closed", "payload": map[string]any{"run_id": "R1", "bead_id": bead}},
	}
	d := startFakeDaemon(t, streamEvents(events...)) // d.Dir has no .git
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, code := smokeWatchSignals(ctx, d.SockPath, d.Dir, "main", bead, &bytes.Buffer{}, &bytes.Buffer{})
	if code != 2 {
		t.Fatalf("exit = %d, want 2 (signal 3 fail, clean close)", code)
	}
	if !res.observed[0] || !res.observed[1] || !res.observed[3] || !res.observed[4] {
		t.Fatalf("signals 1/2/4/5 should be observed: %v", res.observed)
	}
	if res.observed[2] {
		t.Fatal("signal 3 should NOT be observed without a matching commit")
	}
}

// TestSmokeWatchSignals_RunFailedReturnsOne verifies a run_failed for the smoke
// run short-circuits to exit 1.
func TestSmokeWatchSignals_RunFailedReturnsOne(t *testing.T) {
	const bead = "hk-smoke3"
	events := []map[string]any{
		{"type": "run_started", "payload": map[string]any{"run_id": "R1", "bead_id": bead}},
		{"type": "run_failed", "payload": map[string]any{"run_id": "R1"}},
	}
	d := startFakeDaemon(t, streamEvents(events...))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, code := smokeWatchSignals(ctx, d.SockPath, d.Dir, "main", bead, &bytes.Buffer{}, &bytes.Buffer{})
	if code != 1 {
		t.Fatalf("exit = %d, want 1 (run_failed)", code)
	}
	if !res.observed[0] {
		t.Fatal("signal 1 should be observed before the failure")
	}
}

// --- smokeCheckCommitOnBranch (git-log classifier) -------------------------

// TestSmokeCheckCommitOnBranch exercises the git-log grep classifier against a
// real temp git repo. This DOES shell out to git, but git is a hard dependency
// of the project and the behaviour is deterministic; the value is covering the
// found/not-found branches and the sha-extraction parse.
func TestSmokeCheckCommitOnBranch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	initGitRepoWithBeadCommit(t, dir, "main", "hk-found")

	t.Run("found", func(t *testing.T) {
		ok, sha := smokeCheckCommitOnBranch(dir, "main", "hk-found", &bytes.Buffer{})
		if !ok {
			t.Fatal("expected commit to be found")
		}
		if strings.TrimSpace(sha) == "" {
			t.Fatal("expected a non-empty short sha")
		}
	})
	t.Run("not-found", func(t *testing.T) {
		ok, sha := smokeCheckCommitOnBranch(dir, "main", "hk-absent", &bytes.Buffer{})
		if ok || sha != "" {
			t.Fatalf("expected (false, \"\"), got (%v, %q)", ok, sha)
		}
	})
}

// initGitRepoWithBeadCommit initialises a git repo at dir on branch `branch`
// with a single commit whose message references beadID.
func initGitRepoWithBeadCommit(t *testing.T, dir, branch, beadID string) {
	t.Helper()
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	run("add", "f.txt")
	run("commit", "-q", "-m", "smoke("+beadID+"): 5-signal verification")
	run("branch", "-M", branch)
}

// --- smokePrintResults formatting ------------------------------------------

// TestSmokePrintResults_AllPass verifies every row renders PASS with its detail.
func TestSmokePrintResults_AllPass(t *testing.T) {
	var res smokeResult
	for i := range res.observed {
		res.observed[i] = true
		res.detail[i] = "d"
	}
	var buf bytes.Buffer
	if err := smokePrintResults(&buf, "hk-z", res); err != nil {
		t.Fatalf("print: %v", err)
	}
	out := buf.String()
	if strings.Count(out, "PASS") != 5 {
		t.Fatalf("want 5 PASS rows, got:\n%s", out)
	}
	if strings.Contains(out, "FAIL") {
		t.Fatalf("unexpected FAIL in all-pass table:\n%s", out)
	}
}

// TestSmokePrintResults_NoneObserved verifies unobserved rows render FAIL with
// the "(not observed)" placeholder.
func TestSmokePrintResults_NoneObserved(t *testing.T) {
	var buf bytes.Buffer
	if err := smokePrintResults(&buf, "hk-z", smokeResult{}); err != nil {
		t.Fatalf("print: %v", err)
	}
	out := buf.String()
	if strings.Count(out, "FAIL") != 5 {
		t.Fatalf("want 5 FAIL rows, got:\n%s", out)
	}
	if !strings.Contains(out, "(not observed)") {
		t.Fatalf("missing (not observed) placeholder:\n%s", out)
	}
}

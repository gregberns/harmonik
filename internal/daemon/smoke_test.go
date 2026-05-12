package daemon_test

// smoke_test.go — MVH end-to-end smoke test (hk-wql33).
//
// TestMVHSmoke is the proof-of-life integration test: one ready bead →
// workspace → handler run → bead closed → run_completed event in JSONL.
//
// This test uses:
//   - A REAL beads SQLite DB seeded via `br init` + `br create`.
//   - A REAL daemon.Start call (not stub deps) with HandlerBinary pointing at
//     a tiny /bin/sh wrapper script that exits 0 immediately.
//   - Real brcli.Adapter calls through daemon.Start → newWorkLoopDeps.
//
// The test passes a cancellable context to daemon.Start (hk-7oz2f) so that
// the work loop can be stopped cleanly without sending SIGINT to the test
// process.  Once the bead is confirmed closed, the cancel function is called
// and the goroutine exits.
//
// Helper prefix: smokeFixture (per implementer-protocol §Helper-prefix discipline; bead hk-wql33).
//
// Config.HandlerArgs is not yet present in daemon.Config (post-MVH gap filed
// as hk-4e5b5).  The test works around this by writing a baked /bin/sh
// wrapper script to t.TempDir() and pointing HandlerBinary at it.
//
// Worktree cleanup is NOT performed by the work loop at MVH (gap filed as
// hk-fgdgz).  The test accepts worktrees remaining after the run and just
// verifies the happy path through bead close + JSONL events.

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// smokeFixtureProjectDir creates the minimal project directory tree for the
// smoke test: .harmonik/events/, .harmonik/beads-intents/.  Returns the
// project dir and the JSONL events log path.
func smokeFixtureProjectDir(t *testing.T) (projectDir, jsonlPath string) {
	t.Helper()
	projectDir = t.TempDir()
	eventsDir := filepath.Join(projectDir, ".harmonik", "events")
	//nolint:gosec // G301: test-only temp directory; not production
	if err := os.MkdirAll(eventsDir, 0o755); err != nil {
		t.Fatalf("smokeFixtureProjectDir: mkdir events: %v", err)
	}
	intentsDir := filepath.Join(projectDir, ".harmonik", "beads-intents")
	//nolint:gosec // G301: test-only temp directory; not production
	if err := os.MkdirAll(intentsDir, 0o755); err != nil {
		t.Fatalf("smokeFixtureProjectDir: mkdir beads-intents: %v", err)
	}
	jsonlPath = filepath.Join(eventsDir, "events.jsonl")
	return projectDir, jsonlPath
}

// smokeFixtureGitRepo initialises a bare git repository with a single initial
// commit in dir.  Required because CreateWorktree calls `git worktree add` and
// needs an existing git repo with a resolvable HEAD.
func smokeFixtureGitRepo(t *testing.T, dir string) {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
		//nolint:gosec // G204: git args are test-internal literals; not user input
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("smokeFixtureGitRepo: git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "--initial-branch=main")
	run("config", "user.email", "test@harmonik.local")
	run("config", "user.name", "Harmonik Test")
	initFile := filepath.Join(dir, "README")
	if err := os.WriteFile(initFile, []byte("harmonik smoke test repo\n"), 0o644); err != nil {
		t.Fatalf("smokeFixtureGitRepo: WriteFile: %v", err)
	}
	run("add", "README")
	run("commit", "-m", "Initial commit")
}

// smokeFixtureBrPath locates the real `br` binary via exec.LookPath.
// If br is not on PATH, the test is skipped.
func smokeFixtureBrPath(t *testing.T) string {
	t.Helper()
	brPath, err := exec.LookPath("br")
	if err != nil {
		t.Skip("br required for smoke test (not on PATH); CI sets br on PATH")
	}
	return brPath
}

// smokeFixtureBrWrapperScript writes a /bin/sh wrapper script to t.TempDir()
// that invokes realBrPath with --db <dbPath> prepended to all args.  Returns
// the absolute path to the wrapper script.
//
// This wrapper is required because brcli.Adapter does not pass --db to br
// invocations; br normally discovers the DB by upward-traversal from CWD.
// Pointing it explicitly at the test DB via a wrapper is the cleanest
// isolation strategy for a test that runs inside a Go test binary.
func smokeFixtureBrWrapperScript(t *testing.T, realBrPath, dbPath string) string {
	t.Helper()
	scriptDir := t.TempDir()
	scriptPath := filepath.Join(scriptDir, "br")
	content := "#!/bin/sh\nexec " + realBrPath + " --db " + dbPath + " \"$@\"\n"
	//nolint:gosec // G306: script is test-only, chmod 0755 required for execution
	if err := os.WriteFile(scriptPath, []byte(content), 0o755); err != nil {
		t.Fatalf("smokeFixtureBrWrapperScript: WriteFile: %v", err)
	}
	return scriptPath
}

// smokeFixtureHandlerScript writes a /bin/sh script to t.TempDir() that exits
// immediately with code 0. This is the happy-path handler for the smoke test.
// It is used instead of Config.HandlerArgs (which does not exist on daemon.Config
// at MVH — see follow-up bead filed in TestMVHSmoke).
func smokeFixtureHandlerScript(t *testing.T) string {
	t.Helper()
	scriptDir := t.TempDir()
	scriptPath := filepath.Join(scriptDir, "handler.sh")
	content := "#!/bin/sh\nexit 0\n"
	//nolint:gosec // G306: script is test-only, chmod 0755 required for execution
	if err := os.WriteFile(scriptPath, []byte(content), 0o755); err != nil {
		t.Fatalf("smokeFixtureHandlerScript: WriteFile: %v", err)
	}
	return scriptPath
}

// smokeFixtureInitBr initialises a beads workspace in projectDir, creates one
// ready bead, and returns its ID.
//
// br init is run via the real binary with cmd.Dir = projectDir so that br
// discovers the correct workspace and creates .beads/ there.  Subsequent br
// create calls use the wrapper script (which passes --db) so they work from
// any working directory.
func smokeFixtureInitBr(t *testing.T, realBrPath, projectDir, brWrapperPath string) string {
	t.Helper()

	// Step 1: br init — run in projectDir so br creates .beads/ there.
	//nolint:gosec // G204: br args are test-internal literals; not user input
	initCmd := exec.CommandContext(t.Context(), realBrPath, "init", "--prefix", "sm")
	initCmd.Dir = projectDir
	initOut, initErr := initCmd.CombinedOutput()
	if initErr != nil {
		t.Fatalf("smokeFixtureInitBr: br init in %s: %v\n%s", projectDir, initErr, initOut)
	}

	// Step 2: br create via wrapper (--db is now valid since .beads/ exists).
	//nolint:gosec // G204: br args are test-internal literals; not user input
	createCmd := exec.CommandContext(t.Context(), brWrapperPath, "create", "smoke test bead", "--status", "open", "--silent")
	createOut, createErr := createCmd.CombinedOutput()
	if createErr != nil {
		t.Fatalf("smokeFixtureInitBr: br create: %v\n%s", createErr, createOut)
	}
	id := strings.TrimSpace(string(createOut))
	if id == "" {
		t.Fatal("smokeFixtureInitBr: br create returned empty ID")
	}
	return id
}

// smokeFixtureReadJSONLLines reads all non-empty JSONL lines from path.
func smokeFixtureReadJSONLLines(t *testing.T, path string) []string {
	t.Helper()
	//nolint:gosec // G304: path is t.TempDir()-based; not user input
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("smokeFixtureReadJSONLLines: open %s: %v", path, err)
	}
	defer func() { _ = f.Close() }()
	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if line := strings.TrimSpace(scanner.Text()); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// smokeFixturePollBeadClosed polls `br show <id>` at 10 ms intervals for up
// to budget.  Returns true if the bead reaches "closed" status within budget.
func smokeFixturePollBeadClosed(t *testing.T, brWrapperPath, beadID string, budget time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		//nolint:gosec // G204: br args are test-internal literals; not user input
		cmd := exec.CommandContext(t.Context(), brWrapperPath, "show", beadID, "--format", "json")
		out, err := cmd.Output()
		if err == nil {
			var items []struct {
				Status string `json:"status"`
			}
			if jsonErr := json.Unmarshal(out, &items); jsonErr == nil && len(items) == 1 {
				if items[0].Status == "closed" {
					return true
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// ─────────────────────────────────────────────────────────────────────────────
// TestMVHSmoke — end-to-end happy path
// ─────────────────────────────────────────────────────────────────────────────

// TestMVHSmoke is the MVH_ROADMAP row #11 proof-of-life integration test.
// It exercises the full daemon work loop against a real beads SQLite DB:
//
//  1. Seed one ready bead.
//  2. daemon.Start (real br adapter, real git worktree, real handler subprocess).
//  3. Poll until the bead is closed.
//  4. Assert JSONL contains run_started and run_completed events.
//
// The test passes a cancellable context to daemon.Start (hk-7oz2f) and calls
// cancel once the bead is confirmed closed, avoiding SIGINT to the test process.
//
// Known MVH gaps (follow-up beads filed inline):
//   - Config.HandlerArgs not present → workaround: baked handler.sh script.
//   - Worktree cleanup not wired in R10 → workaround: accept worktree present.
func TestMVHSmoke(t *testing.T) {
	t.Parallel()

	realBrPath := smokeFixtureBrPath(t)

	projectDir, jsonlPath := smokeFixtureProjectDir(t)
	smokeFixtureGitRepo(t, projectDir)

	// Build the br DB path: br init will place the DB at <projectDir>/.beads/beads.db.
	dbPath := filepath.Join(projectDir, ".beads", "beads.db")
	brWrapper := smokeFixtureBrWrapperScript(t, realBrPath, dbPath)
	handlerScript := smokeFixtureHandlerScript(t)

	beadID := smokeFixtureInitBr(t, realBrPath, projectDir, brWrapper)
	t.Logf("smoke: seeded bead ID = %s", beadID)

	cfg := daemon.Config{
		ProjectDir:    projectDir,
		JSONLLogPath:  jsonlPath,
		BrPath:        brWrapper,
		HandlerBinary: handlerScript,
		HandlerEnv:    nil,
	}

	// MVH gap (hk-4e5b5): daemon.Config.HandlerArgs — Config currently lacks a
	// HandlerArgs field so the work loop always invokes HandlerBinary with no
	// extra args.  The smoke test works around this by using a baked handler.sh
	// that exits 0 without args.

	// Build a cancellable context to drive a clean shutdown (hk-7oz2f).
	// Cancelling loopCancel replaces the previous SIGINT-to-self workaround.
	loopCtx, loopCancel := context.WithCancel(context.Background())
	defer loopCancel()

	// Launch daemon.Start in a goroutine.  It blocks until loopCtx is cancelled.
	startDone := make(chan error, 1)
	go func() {
		startDone <- daemon.Start(loopCtx, cfg)
	}()

	// Poll until the bead is closed (10 ms interval, 20 s budget).
	// The work loop poll cadence is 2 s; budget gives 10× headroom.
	const pollBudget = 20 * time.Second
	closed := smokeFixturePollBeadClosed(t, brWrapper, beadID, pollBudget)

	// Stop the work loop by cancelling the context.
	loopCancel()

	// Wait for daemon.Start to return (up to 5 s after cancel).
	select {
	case err := <-startDone:
		if err != nil {
			t.Errorf("daemon.Start returned error after context cancel: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Error("daemon.Start did not return within 5 s after context cancel")
	}

	// Assert bead was closed within the polling budget.
	if !closed {
		// Re-check one more time to handle the case where closure happened
		// during the SIGINT→return window.
		closed = smokeFixturePollBeadClosed(t, brWrapper, beadID, 2*time.Second)
	}
	if !closed {
		t.Errorf("bead %s was not closed within %s; MVH work loop did not complete the dispatch cycle", beadID, pollBudget)
	}

	// Assert JSONL log contains run_started and run_completed events.
	lines := smokeFixtureReadJSONLLines(t, jsonlPath)
	if len(lines) == 0 {
		t.Fatal("JSONL log is empty; expected daemon_started, run_started, run_completed")
	}

	// Check for run_started event (keyed to any run_id).
	foundRunStarted := false
	for _, line := range lines {
		if strings.Contains(line, string(core.EventTypeRunStarted)) ||
			strings.Contains(line, `"workspace_path"`) {
			foundRunStarted = true
			break
		}
	}
	if !foundRunStarted {
		t.Errorf("run_started event not found in JSONL log; lines: %v", lines)
	}

	// Check for run_completed event.
	foundRunCompleted := false
	for _, line := range lines {
		if strings.Contains(line, string(core.EventTypeRunCompleted)) ||
			strings.Contains(line, `"auto-close: exit=0"`) {
			foundRunCompleted = true
			break
		}
	}
	if !foundRunCompleted {
		t.Errorf("run_completed event not found in JSONL log; lines: %v", lines)
	}

	// Note: worktree cleanup is not asserted here.  The work loop does not
	// clean up worktrees at MVH.  Follow-up bead hk-fgdgz tracks this gap.

	t.Logf("smoke: JSONL line count = %d; bead closed = %v; run_started = %v; run_completed = %v",
		len(lines), closed, foundRunStarted, foundRunCompleted)
}

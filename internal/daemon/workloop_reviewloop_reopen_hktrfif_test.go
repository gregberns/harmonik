package daemon_test

// workloop_reviewloop_reopen_hktrfif_test.go — regression test for 5adcdcf fix (hk-trfif).
//
// Converted from ExportedWorkLoopDeps to daemon.Start composition root per hk-u0h9v.
//
// Root cause (pre-fix): when runReviewLoop returned success=false (e.g. no_commit,
// error), beadRunOne called CloseBead, incorrectly marking the bead as done with
// a failed status. This caused beads to be closed-without-impl.
//
// Fix (5adcdcf): beadRunOne now calls ReopenBead on the review-loop failure path
// so failed beads remain available for retry.
//
// This test exercises the full daemon stack via daemon.Start in review-loop mode.
// The handler exits 0 without committing anything, which triggers the failure path
// in beadRunOne. The test asserts:
//
//	(a) run_failed fires in the JSONL event log (handler exited without advancing HEAD)
//	(b) bead_closed does NOT appear in the JSONL log (CloseBead was NOT called —
//	    the pre-fix regression would emit bead_closed here)
//	(c) the bead is NOT "closed" in br after the daemon stops (ReopenBead was called)
//
// # Production composition root
//
// daemon.Start is used directly; no internal seams below daemon.Start are used.
//
// # Handler
//
// A plain shell wrapper script (exit 0) is used as the handler binary. It ignores
// the daemon-appended flags (--session-id etc.) and exits immediately without
// making a git commit. This triggers the agent_ready_timeout or no-commit failure
// path in beadRunOne.
//
// # Helper prefix: rlReopenSc (per implementer-protocol.md §Helper-prefix
// discipline; bead hk-u0h9v conversion of hk-trfif).
//
// Bead: hk-u0h9v. Regression guard: hk-trfif (commit 5adcdcf).

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
	"github.com/gregberns/harmonik/internal/daemon/scenariotest"
)

// ─────────────────────────────────────────────────────────────────────────────
// rlReopenSc fixture helpers
// ─────────────────────────────────────────────────────────────────────────────

// rlReopenScBrPath returns the path to the real `br` binary, skipping the test
// when br is not on PATH.
func rlReopenScBrPath(t *testing.T) string {
	t.Helper()
	brPath, err := exec.LookPath("br")
	if err != nil {
		t.Skip("br required for scenario test (not on PATH)")
	}
	return brPath
}

// rlReopenScBrWrapperScript writes a /bin/sh wrapper that invokes realBrPath
// with --db <dbPath> prepended to all args. Returns the wrapper path.
func rlReopenScBrWrapperScript(t *testing.T, realBrPath, dbPath string) string {
	t.Helper()
	raw := t.TempDir()
	dir, resolveErr := filepath.EvalSymlinks(raw)
	if resolveErr != nil {
		t.Fatalf("rlReopenScBrWrapperScript: EvalSymlinks %q: %v", raw, resolveErr)
	}
	path := filepath.Join(dir, "br")
	content := "#!/bin/sh\nexec " + realBrPath + " --db " + dbPath + " \"$@\"\n"
	//nolint:gosec // G306: script is test-only; chmod 0755 required for execution
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("rlReopenScBrWrapperScript: WriteFile: %v", err)
	}
	return path
}

// rlReopenScInitBr initialises a beads workspace in projectDir, creates one
// open bead, and returns its ID.
func rlReopenScInitBr(t *testing.T, realBrPath, projectDir, brWrapper string) string {
	t.Helper()
	initCmd := exec.CommandContext(t.Context(), realBrPath, "init", "--prefix", "rlrs")
	initCmd.Dir = projectDir
	initOut, initErr := initCmd.CombinedOutput()
	if initErr != nil {
		t.Fatalf("rlReopenScInitBr: br init: %v\n%s", initErr, initOut)
	}
	createCmd := exec.CommandContext(t.Context(), brWrapper, "create",
		"review-loop reopen regression test", "--status", "open", "--silent")
	createOut, createErr := createCmd.CombinedOutput()
	if createErr != nil {
		t.Fatalf("rlReopenScInitBr: br create: %v\n%s", createErr, createOut)
	}
	id := strings.TrimSpace(string(createOut))
	if id == "" {
		t.Fatal("rlReopenScInitBr: br create returned empty ID")
	}
	return id
}

// rlReopenScHandlerScript writes a shell script that exits 0 without committing.
// The daemon-appended flags (--session-id etc.) are positional params and ignored.
func rlReopenScHandlerScript(t *testing.T) string {
	t.Helper()
	raw := t.TempDir()
	dir, resolveErr := filepath.EvalSymlinks(raw)
	if resolveErr != nil {
		t.Fatalf("rlReopenScHandlerScript: EvalSymlinks %q: %v", raw, resolveErr)
	}
	path := filepath.Join(dir, "no-commit-handler.sh")
	// Script exits 0 without making a git commit. Daemon-appended flags like
	// --session-id are passed as positional parameters; they are ignored.
	content := "#!/bin/sh\n# exits 0 without advancing HEAD — triggers the review-loop failure path\nexit 0\n"
	//nolint:gosec // G306: script is test-only; chmod 0755 required for execution
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("rlReopenScHandlerScript: WriteFile: %v", err)
	}
	return path
}

// rlReopenScBeadStatus returns the br status string for beadID, or "" on error.
func rlReopenScBeadStatus(t *testing.T, brWrapper, beadID string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), brWrapper, "show", beadID, "--format", "json")
	out, err := cmd.Output()
	if err != nil {
		t.Logf("rlReopenScBeadStatus: br show %s: %v", beadID, err)
		return ""
	}
	var records []struct {
		Status string `json:"status"`
	}
	if jsonErr := json.Unmarshal(out, &records); jsonErr != nil || len(records) == 0 {
		t.Logf("rlReopenScBeadStatus: parse %s: %v", beadID, jsonErr)
		return ""
	}
	return records[0].Status
}

// rlReopenScAssertNoBeadClosed fails the test if the JSONL log contains any
// bead_closed event. A bead_closed event indicates CloseBead was called, which
// is the pre-fix regression this test guards against (5adcdcf).
func rlReopenScAssertNoBeadClosed(t *testing.T, jsonlPath string) {
	t.Helper()
	//nolint:gosec // G304: path is t.TempDir()-based; not user input
	f, err := os.Open(jsonlPath)
	if err != nil {
		if os.IsNotExist(err) {
			return // no JSONL file → no bead_closed
		}
		t.Fatalf("rlReopenScAssertNoBeadClosed: open %s: %v", jsonlPath, err)
	}
	defer func() { _ = f.Close() }()
	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if strings.Contains(line, string(core.EventTypeBeadClosed)) {
			t.Errorf(
				"rlReopenScAssertNoBeadClosed: bead_closed event at JSONL line %d — "+
					"CloseBead was called on review-loop failure path; want ReopenBead "+
					"(regression for commit 5adcdcf / hk-trfif)\nLine: %s",
				lineNum, line,
			)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// TestWorkLoop_ReviewLoopFailure_ReopensBeadNotCloses_Hktrfif
// ─────────────────────────────────────────────────────────────────────────────

// TestWorkLoop_ReviewLoopFailure_ReopensBeadNotCloses_Hktrfif verifies that when
// the review loop fails (handler exits 0 without advancing HEAD), beadRunOne
// calls ReopenBead and does NOT call CloseBead.
//
// Regression guard for commit 5adcdcf: before the fix, the failure path called
// CloseBead, incorrectly terminating the bead as done.
//
// Converted from ExportedWorkLoopDeps to daemon.Start per hk-u0h9v.
//
// Setup:
//  1. TempDir project with git repo and br DB.
//  2. One open bead seeded via br create.
//  3. daemon.Start wired with a shell handler that exits 0 (no commit).
//
// Assertions:
//  1. run_failed fires in JSONL (review-loop failure path ran).
//  2. bead_closed NOT in JSONL (CloseBead was NOT called — regression guard).
//  3. Bead status is NOT "closed" after daemon stops (ReopenBead was called).
//  4. Causality invariant: run_started → run_failed within 60 s.
//
// Bead ref: hk-trfif.
func TestWorkLoop_ReviewLoopFailure_ReopensBeadNotCloses_Hktrfif(t *testing.T) {
	// Not parallel: uses os.Setenv(HARMONIK_CLAUDE_CONFIG_PATH) to isolate
	// EnsureWorktreeTrust from the running harmonik daemon's ~/.claude.json.lock.

	// Locate br binary; skip when absent.
	realBrPath := rlReopenScBrPath(t)

	// Create project directory with git repo.
	projectDir, jsonlPath := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	// Initialise br DB and seed one open bead.
	dbPath := filepath.Join(projectDir, ".beads", "beads.db")
	brWrapper := rlReopenScBrWrapperScript(t, realBrPath, dbPath)
	beadID := rlReopenScInitBr(t, realBrPath, projectDir, brWrapper)
	t.Logf("rlReopenSc: seeded bead ID = %s", beadID)

	// Handler script that exits 0 without committing — triggers the review-loop
	// failure path in beadRunOne.
	handlerScript := rlReopenScHandlerScript(t)

	// Redirect EnsureWorktreeTrust to a test-local config path so the test
	// does not contend with a running harmonik daemon's ~/.claude.json.lock.
	// Each test gets a unique t.TempDir() path so that if tests run in separate
	// processes, there is no race on the env var value.
	claudeConfigPath := filepath.Join(t.TempDir(), ".claude.json")
	prevClaudeCfg, hadClaudeCfg := os.LookupEnv("HARMONIK_CLAUDE_CONFIG_PATH")
	if err := os.Setenv("HARMONIK_CLAUDE_CONFIG_PATH", claudeConfigPath); err != nil {
		t.Fatalf("rlReopenSc: Setenv HARMONIK_CLAUDE_CONFIG_PATH: %v", err)
	}
	t.Cleanup(func() {
		// hk-1o0cc: restore prior value (TestMain package default) — see scenario_happypath_n1.
		if hadClaudeCfg {
			_ = os.Setenv("HARMONIK_CLAUDE_CONFIG_PATH", prevClaudeCfg)
		} else {
			_ = os.Unsetenv("HARMONIK_CLAUDE_CONFIG_PATH")
		}
	})

	// Wire daemon.Config — production composition root.
	loopCtx, loopCancel := context.WithCancel(context.Background())
	defer loopCancel()

	cfg := daemon.Config{
		ProjectDir:    projectDir,
		JSONLLogPath:  jsonlPath,
		BrPath:        brWrapper,
		HandlerBinary: handlerScript,
		// WorkflowModeReviewLoop: the daemon dispatches the bead in review-loop
		// mode. The handler exits 0 without committing, triggering the failure
		// path in beadRunOne (agent_ready_timeout or no-commit detection).
		WorkflowModeDefault: core.WorkflowModeReviewLoop,
		// Short timeout: handler exits immediately without emitting agent_ready.
		// The failure fires at agent_ready_timeout (5 s) or earlier if the
		// no-commit exit is detected before the timeout.
		AgentReadyTimeout:     5 * time.Second,
		SkipWALCheckpoint:     true,
		SkipBrHistoryRotation: true,
		LogWriter:             testLogWriter{t: t},
	}

	startDone := make(chan error, 1)
	go func() {
		startDone <- daemon.Start(loopCtx, cfg)
	}()

	// Poll until run_failed appears in the JSONL log.
	//
	// Budget: AgentReadyTimeout (5 s) + overhead for br calls and daemon startup.
	// The failure fires when the handler exits without committing (no_commit or
	// agent_ready_timeout path in beadRunOne). Note: ReopenBead is called BEFORE
	// run_failed is emitted, so when we detect run_failed the bead is already
	// reopened.
	const runFailedPollBudget = 20 * time.Second
	if !scenariotest.WaitForEvent(t, jsonlPath, string(core.EventTypeRunFailed), "", runFailedPollBudget) {
		t.Fatalf("rlReopenSc: run_failed not found in JSONL within %s", runFailedPollBudget)
	}

	// Stop the work loop now that the run has reached a terminal JSONL event.
	// ReopenBead was already called (it precedes run_failed emission), so the
	// bead is already "open" at this point.
	loopCancel()

	// Wait for daemon.Start to return — ensures all goroutines (wg.Wait) have
	// completed before we inspect JSONL and bead status.
	scenariotest.MustCompleteWithin(t, jsonlPath, "", nil, 5*time.Second, func() {
		if err := <-startDone; err != nil {
			t.Errorf("daemon.Start returned error after context cancel: %v", err)
		}
	})

	// ── Assertion 1: bead_closed MUST NOT appear in JSONL ────────────────────
	//
	// The pre-fix regression (before 5adcdcf): beadRunOne called CloseBead on
	// the review-loop failure path, emitting bead_closed. The fix replaces that
	// CloseBead call with ReopenBead. If bead_closed appears here, the regression
	// has returned.

	rlReopenScAssertNoBeadClosed(t, jsonlPath)

	// ── Assertion 2: bead is NOT closed in br ────────────────────────────────
	//
	// After ReopenBead, the bead status is "open" (ready for retry).
	// If CloseBead was called (regression), the status would be "closed".
	// Status "in_progress" is acceptable — it means the daemon was cancelled
	// during a second dispatch triggered by the reopened bead; CloseBead was
	// still not called on the first failure path.

	finalStatus := rlReopenScBeadStatus(t, brWrapper, string(beadID))
	if finalStatus == "closed" {
		t.Errorf(
			"rlReopenSc: bead %s status = %q after review-loop failure — "+
				"CloseBead was called; want bead reopened (regression for 5adcdcf / hk-trfif). "+
				"JSONL: %s",
			beadID, finalStatus, jsonlPath,
		)
	} else {
		t.Logf("rlReopenSc: bead %s status = %q (not closed — PASS)", beadID, finalStatus)
	}

	// ── Assertion 3: causality invariant ─────────────────────────────────────
	// run_started must precede a terminal run event.

	scenariotest.AssertEventCausality(t, jsonlPath,
		"run_started",
		[]string{"run_completed", "run_failed", "run_cancelled"},
		60*time.Second,
	)

	t.Logf("rlReopenSc: PASS bead=%s", beadID)
}

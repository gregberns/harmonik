package daemon_test

// mergetomain_beadsynccall_hkzgt4u_test.go — integration tests for the
// post-merge `br sync --import-only` call and bead_sync_failed event emission
// introduced by BL-MRG-004/005 (bead hk-zgt4u).
//
// Test assertions:
//   (r1) When the merge touches .beads/issues.jsonl AND brPath is set, the
//        daemon calls `br sync --import-only` in the project directory.
//   (r2) When `br sync --import-only` exits non-zero, a bead_sync_failed event
//        is emitted (F-class: run_id, error, timestamp all present).
//   (r3) The merge still returns success even when `br sync --import-only` fails;
//        the bead is closed, not reopened.
//   (r4) When .beads/issues.jsonl is NOT in the diff, `br sync --import-only`
//        is NOT called (the marker file must not exist).
//   (r5) When brPath == "" (work-loop unit-test mode), `br sync --import-only`
//        is NOT called regardless of whether the ledger was touched.
//
// Helper prefix: beadSyncCall (per implementer-protocol.md §Helper-prefix
// discipline; bead hk-zgt4u).
//
// Spec refs:
//   - event-model.md §8.15.1 bead_sync_failed (BL-MRG-004)
//   - plans/2026-06-22-beads-integration-conformance-audit.md BL-MRG-005
//
// Bead: hk-zgt4u.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// ─────────────────────────────────────────────────────────────────────────────
// Fixture helpers
// ─────────────────────────────────────────────────────────────────────────────

// beadSyncCallGit runs a git command in dir and fails the test on error.
func beadSyncCallGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s (dir=%s): %v\n%s", strings.Join(args, " "), dir, err, out)
	}
	return strings.TrimRight(string(out), "\n")
}

// beadSyncCallSetupRepo builds a minimal git repo with:
//   - .beads/issues.jsonl committed on main
//   - A bare remote (origin) for push
//   - The .harmonik/ directories needed by ExportedWorkLoopDeps
//
// Returns projectDir.
func beadSyncCallSetupRepo(t *testing.T) string {
	t.Helper()
	projectDir := mergeToMainFixtureProjectDir(t)
	mergeToMainFixtureGitRepo(t, projectDir)

	// Commit .beads/issues.jsonl on main.
	mergeToMainFixtureInitBeadsLedger(t, projectDir)

	// Create a bare remote so git push succeeds.
	originDir := t.TempDir()
	initBareCmd := exec.CommandContext(t.Context(), "git", "init", "--bare", "--initial-branch=main", originDir)
	if out, err := initBareCmd.CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v\n%s", err, out)
	}
	addRemoteCmd := exec.CommandContext(t.Context(), "git", "remote", "add", "origin", originDir)
	addRemoteCmd.Dir = projectDir
	if out, err := addRemoteCmd.CombinedOutput(); err != nil {
		t.Fatalf("git remote add origin: %v\n%s", err, out)
	}
	pushInitCmd := exec.CommandContext(t.Context(), "git", "push", "origin", "main")
	pushInitCmd.Dir = projectDir
	if out, err := pushInitCmd.CombinedOutput(); err != nil {
		t.Fatalf("git push origin main (initial): %v\n%s", err, out)
	}

	return projectDir
}

// beadSyncCallWriteMockBr writes a mock `br` script to dir that records
// invocations and exits with exitCode. Returns the path to the script.
//
// When called as `br sync --import-only`, the script appends a line to
// <markerPath> so the test can observe whether it was invoked.
func beadSyncCallWriteMockBr(t *testing.T, dir string, exitCode int, markerPath string) string {
	t.Helper()
	scriptPath := filepath.Join(dir, "mock-br")
	var scriptContent string
	if exitCode == 0 {
		scriptContent = fmt.Sprintf("#!/bin/sh\necho invoked \"$@\" >> %q\nexit 0\n", markerPath)
	} else {
		scriptContent = fmt.Sprintf("#!/bin/sh\necho invoked \"$@\" >> %q\necho 'br sync: import failed' >&2\nexit %d\n", markerPath, exitCode)
	}
	//nolint:gosec // G306: 0755 required for executable script
	if err := os.WriteFile(scriptPath, []byte(scriptContent), 0o755); err != nil {
		t.Fatalf("beadSyncCallWriteMockBr: WriteFile: %v", err)
	}
	return scriptPath
}

// beadSyncCallLedgerTouchingFactory returns a worktree factory that commits
// work.txt AND a modified .beads/issues.jsonl on the run-branch, so the
// merge diff will include the ledger.
func beadSyncCallLedgerTouchingFactory(t *testing.T) func(ctx context.Context, projectDir, runID, headSHA string) (string, func(), error) {
	t.Helper()
	return func(ctx context.Context, projectDir, runID, headSHA string) (string, func(), error) {
		wtPath, cleanup, err := daemon.ExportedProductionWorktreeFactory(ctx, projectDir, runID, headSHA)
		if err != nil {
			return "", nil, err
		}

		workFile := filepath.Join(wtPath, "work.txt")
		//nolint:gosec // G306: 0644 is fine for a test fixture file
		if err2 := os.WriteFile(workFile, []byte("agent work\n"), 0o644); err2 != nil {
			cleanup()
			return "", nil, &testSetupError{"beadSyncCallLedgerTouchingFactory: WriteFile work.txt: " + err2.Error()}
		}

		// Modify .beads/issues.jsonl on the run-branch.
		beadsDir := filepath.Join(wtPath, ".beads")
		//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
		if err2 := os.MkdirAll(beadsDir, 0o755); err2 != nil {
			cleanup()
			return "", nil, &testSetupError{"beadSyncCallLedgerTouchingFactory: MkdirAll: " + err2.Error()}
		}
		ledgerPath := filepath.Join(beadsDir, "issues.jsonl")
		//nolint:gosec // G306: 0644 is fine for a test fixture file
		if err2 := os.WriteFile(ledgerPath, []byte(`{"id":"bead-init","status":"open"}`+"\n"+`{"id":"agent-bead","status":"closed"}`+"\n"), 0o644); err2 != nil {
			cleanup()
			return "", nil, &testSetupError{"beadSyncCallLedgerTouchingFactory: WriteFile ledger: " + err2.Error()}
		}

		addCmd := exec.CommandContext(ctx, "git", "add", "work.txt", ".beads/issues.jsonl")
		addCmd.Dir = wtPath
		if out, err2 := addCmd.CombinedOutput(); err2 != nil {
			cleanup()
			return "", nil, &testSetupError{"beadSyncCallLedgerTouchingFactory: git add: " + string(out)}
		}
		commitCmd := exec.CommandContext(ctx, "git", "commit", "-m", "feat: touch ledger",
			"--trailer", "Harmonik-Run-ID: "+runID,
		)
		commitCmd.Dir = wtPath
		if out, err2 := commitCmd.CombinedOutput(); err2 != nil {
			cleanup()
			return "", nil, &testSetupError{"beadSyncCallLedgerTouchingFactory: git commit: " + string(out)}
		}

		return wtPath, cleanup, nil
	}
}

// beadSyncCallCodeOnlyFactory returns a worktree factory that commits only
// work.txt (NOT .beads/issues.jsonl) on the run-branch.
func beadSyncCallCodeOnlyFactory(t *testing.T) func(ctx context.Context, projectDir, runID, headSHA string) (string, func(), error) {
	t.Helper()
	return func(ctx context.Context, projectDir, runID, headSHA string) (string, func(), error) {
		wtPath, cleanup, err := daemon.ExportedProductionWorktreeFactory(ctx, projectDir, runID, headSHA)
		if err != nil {
			return "", nil, err
		}

		workFile := filepath.Join(wtPath, "work.txt")
		//nolint:gosec // G306: 0644 is fine for a test fixture file
		if err2 := os.WriteFile(workFile, []byte("agent work\n"), 0o644); err2 != nil {
			cleanup()
			return "", nil, &testSetupError{"beadSyncCallCodeOnlyFactory: WriteFile work.txt: " + err2.Error()}
		}
		addCmd := exec.CommandContext(ctx, "git", "add", "work.txt")
		addCmd.Dir = wtPath
		if out, err2 := addCmd.CombinedOutput(); err2 != nil {
			cleanup()
			return "", nil, &testSetupError{"beadSyncCallCodeOnlyFactory: git add: " + string(out)}
		}
		commitCmd := exec.CommandContext(ctx, "git", "commit", "-m", "feat: code only",
			"--trailer", "Harmonik-Run-ID: "+runID,
		)
		commitCmd.Dir = wtPath
		if out, err2 := commitCmd.CombinedOutput(); err2 != nil {
			cleanup()
			return "", nil, &testSetupError{"beadSyncCallCodeOnlyFactory: git commit: " + string(out)}
		}

		return wtPath, cleanup, nil
	}
}

// beadSyncCallRunLoop runs the work-loop until the ledger signals doneCh, then
// cancels and waits for the loop to exit.  It returns the event collector.
func beadSyncCallRunLoop(
	t *testing.T,
	beadID core.BeadID,
	projectDir string,
	brPath string,
	factory func(ctx context.Context, projectDir, runID, headSHA string) (string, func(), error),
) (ledger *mergeToMainRecordingLedger, collector *stubEventCollector) {
	t.Helper()

	ledger = newMergeToMainRecordingLedger(beadID)
	collector = &stubEventCollector{}

	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:        ledger,
		Bus:              collector,
		ProjectDir:       projectDir,
		HandlerBinary:    "/bin/sh",
		HandlerArgs:      []string{"-c", "exit 0"},
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		WorktreeFactory:  factory,
		BrPath:           brPath,
	})

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)

	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	select {
	case <-ledger.doneCh:
		cancel()
	case <-ctx.Done():
		cancel()
		t.Error("timed out waiting for bead close/reopen")
	}

	select {
	case <-loopDone:
	case <-time.After(5 * time.Second):
		t.Error("work loop did not exit within 5s")
	}

	return ledger, collector
}

// ─────────────────────────────────────────────────────────────────────────────
// Test (r1)+(r3): ledger touched, br succeeds → sync called, bead closes
// ─────────────────────────────────────────────────────────────────────────────

// TestMergeToMain_BeadSyncCall_LedgerTouched_SyncCalled verifies that when the
// merge diff includes .beads/issues.jsonl AND brPath is set, the daemon runs
// `br sync --import-only` in the project directory (r1), and the bead is still
// closed on success (r3).
func TestMergeToMain_BeadSyncCall_LedgerTouched_SyncCalled(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("beadsync-ledger-touched-sync-called-001")

	projectDir := beadSyncCallSetupRepo(t)

	markerPath := filepath.Join(t.TempDir(), "br-invocations.txt")
	brPath := beadSyncCallWriteMockBr(t, t.TempDir(), 0, markerPath)

	ledger, collector := beadSyncCallRunLoop(t, beadID, projectDir, brPath, beadSyncCallLedgerTouchingFactory(t))

	// ── (r1): br sync --import-only was called. ──────────────────────────────
	markerContent, readErr := os.ReadFile(markerPath)
	if readErr != nil {
		t.Fatalf("marker file not created — br sync was never called: %v", readErr)
	}
	if !strings.Contains(string(markerContent), "sync") || !strings.Contains(string(markerContent), "import-only") {
		t.Errorf("br was called but not with 'sync --import-only'; invocations:\n%s", markerContent)
	}

	// ── (r3): bead closed, not reopened. ─────────────────────────────────────
	if got := ledger.getClosedCount(); got != 1 {
		t.Errorf("CloseBead call count = %d; want 1 (br sync succeeded, merge ok)", got)
	}
	if got := ledger.getReopenedCount(); got != 0 {
		t.Errorf("ReopenBead call count = %d; want 0", got)
	}

	// No bead_sync_failed event expected on a successful sync.
	syncFailEvs := mergeToMainFindEvents(collector, string(core.EventTypeBeadSyncFailed))
	if len(syncFailEvs) != 0 {
		t.Errorf("bead_sync_failed emitted unexpectedly (%d events)", len(syncFailEvs))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test (r2)+(r3): ledger touched, br fails → bead_sync_failed emitted, bead
//                 still closes
// ─────────────────────────────────────────────────────────────────────────────

// TestMergeToMain_BeadSyncCall_SyncFails_EmitsBadSync verifies that when
// `br sync --import-only` exits non-zero:
//
//	(r2) a bead_sync_failed event is emitted with run_id, error, timestamp,
//	(r3) the bead is still closed (merge was already durable).
func TestMergeToMain_BeadSyncCall_SyncFails_EmitsBadSync(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("beadsync-sync-fails-emit-001")

	projectDir := beadSyncCallSetupRepo(t)

	markerPath := filepath.Join(t.TempDir(), "br-invocations.txt")
	brPath := beadSyncCallWriteMockBr(t, t.TempDir(), 1, markerPath) // exits 1

	ledger, collector := beadSyncCallRunLoop(t, beadID, projectDir, brPath, beadSyncCallLedgerTouchingFactory(t))

	// ── (r2): bead_sync_failed emitted. ──────────────────────────────────────
	syncFailEvs := mergeToMainFindEvents(collector, string(core.EventTypeBeadSyncFailed))
	if len(syncFailEvs) == 0 {
		t.Fatalf("no bead_sync_failed event emitted; all events: %v", mergeToMainEventOrder(collector))
	}

	// Validate payload fields (run_id, error, timestamp all required per §8.15.1).
	var pl core.BeadSyncFailedPayload
	if err := json.Unmarshal(syncFailEvs[0].Payload, &pl); err != nil {
		t.Fatalf("bead_sync_failed payload unmarshal: %v", err)
	}
	if pl.RunID == "" {
		t.Errorf("bead_sync_failed: run_id is empty")
	}
	if pl.Error == "" {
		t.Errorf("bead_sync_failed: error is empty")
	}
	if pl.Timestamp == "" {
		t.Errorf("bead_sync_failed: timestamp is empty")
	}
	if _, parseErr := time.Parse(time.RFC3339, pl.Timestamp); parseErr != nil {
		t.Errorf("bead_sync_failed: timestamp %q not RFC3339: %v", pl.Timestamp, parseErr)
	}

	// ── (r3): bead still closed despite sync failure. ────────────────────────
	if got := ledger.getClosedCount(); got != 1 {
		t.Errorf("CloseBead call count = %d; want 1 (merge succeeded even though br sync failed)", got)
	}
	if got := ledger.getReopenedCount(); got != 0 {
		t.Errorf("ReopenBead call count = %d; want 0", got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test (r4): ledger NOT in diff → br sync NOT called
// ─────────────────────────────────────────────────────────────────────────────

// TestMergeToMain_BeadSyncCall_LedgerNotTouched_SyncNotCalled verifies that
// when the merge diff does NOT include .beads/issues.jsonl, `br sync
// --import-only` is not called even when brPath is configured.
func TestMergeToMain_BeadSyncCall_LedgerNotTouched_SyncNotCalled(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("beadsync-ledger-not-touched-001")

	projectDir := beadSyncCallSetupRepo(t)

	markerPath := filepath.Join(t.TempDir(), "br-invocations.txt")
	brPath := beadSyncCallWriteMockBr(t, t.TempDir(), 0, markerPath)

	// Use the code-only factory: no .beads/issues.jsonl in the diff.
	ledger, _ := beadSyncCallRunLoop(t, beadID, projectDir, brPath, beadSyncCallCodeOnlyFactory(t))

	// br sync must NOT have been called.
	if _, readErr := os.ReadFile(markerPath); readErr == nil {
		invocations, _ := os.ReadFile(markerPath)
		t.Errorf("br was called but ledger was not in the diff; invocations:\n%s", invocations)
	}

	// Bead still closes normally.
	if got := ledger.getClosedCount(); got != 1 {
		t.Errorf("CloseBead call count = %d; want 1", got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test (r5): brPath == "" → br sync NOT called even when ledger is touched
// ─────────────────────────────────────────────────────────────────────────────

// TestMergeToMain_BeadSyncCall_NoBrPath_SyncNotCalled verifies that when
// brPath is empty (unit-test mode / no br binary configured), `br sync
// --import-only` is not invoked even when .beads/issues.jsonl was touched.
func TestMergeToMain_BeadSyncCall_NoBrPath_SyncNotCalled(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("beadsync-no-brpath-001")

	projectDir := beadSyncCallSetupRepo(t)

	// Run with brPath == "".
	ledger, collector := beadSyncCallRunLoop(t, beadID, projectDir, "", beadSyncCallLedgerTouchingFactory(t))

	// No bead_sync_failed event expected — the step is disabled when brPath == "".
	syncFailEvs := mergeToMainFindEvents(collector, string(core.EventTypeBeadSyncFailed))
	if len(syncFailEvs) != 0 {
		t.Errorf("bead_sync_failed emitted with brPath=%q; got %d events", "", len(syncFailEvs))
	}

	// Bead still closes normally.
	if got := ledger.getClosedCount(); got != 1 {
		t.Errorf("CloseBead call count = %d; want 1", got)
	}
}

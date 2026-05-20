package daemon_test

// scenario_orphan_sweep_queue_owned_tutqk_test.go — integration test for the
// daemon.Start + queue.json + orphan-sweep bead-reset path (hk-tutqk).
//
// # What is tested
//
// TestScenario_OrphanSweep_QueueOwnedBeadReset boots the full daemon.Start
// composition root with:
//
//   - A real br DB seeded with one bead in `in_progress` status (simulating
//     a crash-left-behind bead from a prior SIGKILL recovery).
//   - A pre-written queue.json whose single item carries the same bead_id with
//     status=pending (queue-owned but NOT dispatched). This is the SIGKILL-
//     recovery scenario where the claim intent was drained but queue.json still
//     records ownership.
//
// daemon.Start runs the orphan sweep (step 3 of PL-005) synchronously before
// the work loop. The sweep reads queue.json, builds QueueOwnedSet, detects the
// bead in QueueOwnedSet but NOT in QueueDispatchedSet, establishes provenance,
// and resets the bead to `open` via br update.
//
// The test cancels the daemon context once daemon_orphan_sweep_completed appears
// in the JSONL log and asserts the bead status via AssertBeadStatus.
//
// # Helper prefix
//
// Helpers in this file use the prefix "sweepQO" (sweep queue-owned).
// Per implementer-protocol.md §Helper-prefix discipline.
//
// # Spec refs
//
//   - specs/process-lifecycle.md §4.5 PL-006 sixth bullet — queue-owned provenance.
//   - specs/queue-model.md §2.7 — ItemStatus values.
//
// Bead: hk-tutqk.

import (
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

// sweepQOEvalSymlinks resolves all symlinks in path so that br — which rejects
// paths containing symlinks outside the beads directory — receives a canonical
// path. On macOS, t.TempDir() returns /var/folders/... which is a symlink to
// /private/var/folders/..., triggering br's symlink guard.
func sweepQOEvalSymlinks(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("sweepQOEvalSymlinks: EvalSymlinks %q: %v", path, err)
	}
	return resolved
}

// ─────────────────────────────────────────────────────────────────────────────
// sweepQO fixture helpers
// ─────────────────────────────────────────────────────────────────────────────

// sweepQOProjectDir creates the minimal project directory for the scenario:
// .harmonik/events/ and .harmonik/beads-intents/. Returns the project dir
// and the JSONL events log path.
//
// The directory path is resolved via filepath.EvalSymlinks so that br —
// which rejects paths whose components are symlinks outside the beads directory
// — receives a canonical path. On macOS, t.TempDir() returns a path under
// /var/folders/ which is a symlink to /private/var/folders/.
func sweepQOProjectDir(t *testing.T) (projectDir, jsonlPath string) {
	t.Helper()
	projectDir = sweepQOEvalSymlinks(t, t.TempDir())
	for _, sub := range []string{
		filepath.Join(".harmonik", "events"),
		filepath.Join(".harmonik", "beads-intents"),
	} {
		//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
		if err := os.MkdirAll(filepath.Join(projectDir, sub), 0o755); err != nil {
			t.Fatalf("sweepQOProjectDir: mkdir %s: %v", sub, err)
		}
	}
	jsonlPath = filepath.Join(projectDir, ".harmonik", "events", "events.jsonl")
	return projectDir, jsonlPath
}

// sweepQOBrPath returns the path to the real `br` binary, skipping the test
// when br is not on PATH.
func sweepQOBrPath(t *testing.T) string {
	t.Helper()
	brPath, err := exec.LookPath("br")
	if err != nil {
		t.Skip("br required for scenario test (not on PATH)")
	}
	return brPath
}

// sweepQOBrWrapperScript writes a /bin/sh wrapper that invokes realBrPath
// with --db <dbPath> prepended to all args. Returns the wrapper path.
func sweepQOBrWrapperScript(t *testing.T, realBrPath, dbPath string) string {
	t.Helper()
	dir := sweepQOEvalSymlinks(t, t.TempDir())
	path := filepath.Join(dir, "br")
	content := "#!/bin/sh\nexec " + realBrPath + " --db " + dbPath + " \"$@\"\n"
	//nolint:gosec // G306: script is test-only; chmod 0755 required for execution
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("sweepQOBrWrapperScript: WriteFile: %v", err)
	}
	return path
}

// sweepQOInitBrWithInProgress initialises a beads workspace in projectDir,
// creates one bead, and sets it to in_progress status. Returns the bead ID.
func sweepQOInitBrWithInProgress(t *testing.T, realBrPath, projectDir, brWrapper string) string {
	t.Helper()

	// br init — creates .beads/ and .beads/beads.db.
	//nolint:gosec // G204: br args are test-internal literals; not user input
	initCmd := exec.CommandContext(t.Context(), realBrPath, "init", "--prefix", "sqo")
	initCmd.Dir = projectDir
	initOut, initErr := initCmd.CombinedOutput()
	if initErr != nil {
		t.Fatalf("sweepQOInitBrWithInProgress: br init: %v\n%s", initErr, initOut)
	}

	// br create — produces a bead in open status.
	//nolint:gosec // G204: br args are test-internal literals; not user input
	createCmd := exec.CommandContext(t.Context(), brWrapper, "create",
		"orphan sweep queue-owned test bead", "--status", "open", "--silent")
	createOut, createErr := createCmd.CombinedOutput()
	if createErr != nil {
		t.Fatalf("sweepQOInitBrWithInProgress: br create: %v\n%s", createErr, createOut)
	}
	beadID := strings.TrimSpace(string(createOut))
	if beadID == "" {
		t.Fatal("sweepQOInitBrWithInProgress: br create returned empty ID")
	}

	// br update --status in_progress — simulate a crash-left-behind bead.
	//nolint:gosec // G204: br args are test-internal literals; not user input
	updateCmd := exec.CommandContext(t.Context(), brWrapper, "update", beadID,
		"--status", "in_progress")
	updateOut, updateErr := updateCmd.CombinedOutput()
	if updateErr != nil {
		t.Fatalf("sweepQOInitBrWithInProgress: br update in_progress: %v\n%s", updateErr, updateOut)
	}

	return beadID
}

// sweepQOWriteQueueJSON writes a queue.json file to .harmonik/queue.json under
// projectDir. The queue has one active group with a single item whose bead_id
// is beadID and status is "pending" (queue-owned, not dispatched).
func sweepQOWriteQueueJSON(t *testing.T, projectDir, beadID string) {
	t.Helper()

	harmonikDir := filepath.Join(projectDir, ".harmonik")
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(harmonikDir, 0o755); err != nil {
		t.Fatalf("sweepQOWriteQueueJSON: MkdirAll .harmonik: %v", err)
	}

	// Construct a minimal valid queue.json envelope (schema_version=1, status=active,
	// one wave group, one pending item).
	queue := map[string]interface{}{
		"schema_version": 1,
		"queue_id":       "00000000-0000-7000-8000-000000000001",
		"submitted_at":   time.Now().UTC().Format(time.RFC3339),
		"status":         "active",
		"groups": []map[string]interface{}{
			{
				"group_index": 0,
				"kind":        "wave",
				"status":      "active",
				"created_at":  time.Now().UTC().Format(time.RFC3339),
				"items": []map[string]interface{}{
					{
						"bead_id": beadID,
						"status":  "pending",
					},
				},
			},
		},
	}

	data, err := json.Marshal(queue)
	if err != nil {
		t.Fatalf("sweepQOWriteQueueJSON: marshal: %v", err)
	}

	queuePath := filepath.Join(harmonikDir, "queue.json")
	if err := os.WriteFile(queuePath, data, 0o600); err != nil {
		t.Fatalf("sweepQOWriteQueueJSON: WriteFile: %v", err)
	}
}

// sweepQOPollOrphanSweepCompleted polls the JSONL log for a
// daemon_orphan_sweep_completed event for up to budget. Returns true when found.
func sweepQOPollOrphanSweepCompleted(t *testing.T, jsonlPath string, budget time.Duration) bool {
	t.Helper()
	return scenariotest.WaitForEvent(t, jsonlPath,
		string(core.EventTypeDaemonOrphanSweepCompleted), "", budget)
}

// ─────────────────────────────────────────────────────────────────────────────
// TestScenario_OrphanSweep_QueueOwnedBeadReset
// ─────────────────────────────────────────────────────────────────────────────

// TestScenario_OrphanSweep_QueueOwnedBeadReset is the integration test for the
// daemon.Start + queue.json + orphan-sweep queue-owned bead-reset path.
//
// Setup:
//  1. TempDir project with br DB seeded with one bead in `in_progress` status.
//  2. queue.json written with that bead_id at status=pending (queue-owned, not
//     dispatched).
//  3. daemon.Start wired with BrPath but no HandlerBinary needed for the sweep.
//
// Assertions:
//  1. daemon_orphan_sweep_completed event appears in the JSONL log.
//  2. Bead status == "open" in the br DB (reset by queue-owned provenance path).
//
// Bead: hk-tutqk.
func TestScenario_OrphanSweep_QueueOwnedBeadReset(t *testing.T) {
	// Not parallel: uses os.Setenv(HARMONIK_CLAUDE_CONFIG_PATH) to isolate
	// EnsureWorktreeTrust — same rationale as TestScenario_HappyPath_N1.

	// Locate br binary; skip when absent.
	realBrPath := sweepQOBrPath(t)

	// Create project directory.
	projectDir, jsonlPath := sweepQOProjectDir(t)

	// Initialise br DB and seed one bead in in_progress status.
	dbPath := filepath.Join(projectDir, ".beads", "beads.db")
	brWrapper := sweepQOBrWrapperScript(t, realBrPath, dbPath)
	beadID := sweepQOInitBrWithInProgress(t, realBrPath, projectDir, brWrapper)
	t.Logf("sweepQO: seeded bead ID = %s (in_progress)", beadID)

	// Verify initial state: bead must be in_progress before the sweep.
	scenariotest.AssertBeadStatus(t, brWrapper, beadID, "in_progress")

	// Write queue.json: bead is queue-owned (status=pending) but not dispatched.
	sweepQOWriteQueueJSON(t, projectDir, beadID)

	// Redirect EnsureWorktreeTrust to a test-local config path.
	claudeConfigPath := filepath.Join(sweepQOEvalSymlinks(t, t.TempDir()), ".claude.json")
	if err := os.Setenv("HARMONIK_CLAUDE_CONFIG_PATH", claudeConfigPath); err != nil {
		t.Fatalf("sweepQO: Setenv HARMONIK_CLAUDE_CONFIG_PATH: %v", err)
	}
	t.Cleanup(func() { _ = os.Unsetenv("HARMONIK_CLAUDE_CONFIG_PATH") })

	// Wire daemon.Config for the orphan-sweep integration test.
	// No HandlerBinary: with no ready beads (the in_progress bead is reset to
	// open by the sweep, but there are no beads the work loop dispatches since
	// BrPath is set and the queue item is pending; the work loop will claim it —
	// but we cancel the context after the sweep completes, before any dispatch).
	// We use a no-op twin wrapper so any accidental dispatch fails harmlessly.
	loopCtx, loopCancel := context.WithCancel(context.Background())
	defer loopCancel()

	cfg := daemon.Config{
		ProjectDir:            projectDir,
		JSONLLogPath:          jsonlPath,
		BrPath:                brWrapper,
		SkipWALCheckpoint:     true,
		SkipBrHistoryRotation: true,
		// No HandlerBinary: work loop will not be able to dispatch. We cancel
		// before any dispatch attempt anyway.
		// AgentReadyTimeout left at zero (= default 30 s); irrelevant since we
		// cancel before any dispatch.
		LogWriter: testLogWriter{t: t},
	}

	// Launch daemon.Start in a goroutine.
	startDone := make(chan error, 1)
	go func() {
		startDone <- daemon.Start(loopCtx, cfg)
	}()

	// ── Wait for orphan sweep to complete ────────────────────────────────────
	//
	// The sweep runs synchronously in daemon.Start BEFORE the work loop goroutine
	// is spawned (PL-005 step 3). We poll the JSONL log for
	// daemon_orphan_sweep_completed, which is emitted immediately after the sweep.
	// Budget: 10 s is generous; the sweep itself is sub-second in CI.
	const sweepPollBudget = 10 * time.Second
	if !sweepQOPollOrphanSweepCompleted(t, jsonlPath, sweepPollBudget) {
		t.Error("sweepQO: daemon_orphan_sweep_completed not found within budget")
	}

	// Cancel the daemon context to stop the work loop.
	loopCancel()

	// Wait for daemon.Start to return (up to 5 s).
	select {
	case err := <-startDone:
		if err != nil {
			t.Errorf("daemon.Start returned error after context cancel: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Error("daemon.Start did not return within 5 s after context cancel")
	}

	// ── Assertion: bead reset to open ────────────────────────────────────────
	//
	// The orphan sweep detected bead in in_progress, established ownership via
	// QueueOwnedSet (bead_id appears in queue.json), saw QueueDispatched is empty
	// (status=pending, not dispatched), and called ResetBead → br update --status open.
	scenariotest.AssertBeadStatus(t, brWrapper, beadID, "open")

	t.Logf("sweepQO: PASS bead=%s reset to open by queue-owned provenance path", beadID)
}

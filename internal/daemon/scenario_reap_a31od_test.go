package daemon_test

// scenario_reap_a31od_test.go — integration test for the reap scenario:
// boot orphan-sweep remediates multiple orphaned resource types and the
// daemon_orphan_sweep_completed JSONL payload counts reconcile exactly.
//
// # What is tested
//
// TestScenario_Reap_BootOrphanSweepCounts boots the full daemon.Start
// composition root with:
//
//   - A real br DB seeded with one bead in `in_progress` status.
//   - A pre-written queue.json whose single item carries the same bead_id with
//     status=pending (queue-owned, not dispatched) — the SIGKILL-recovery
//     scenario where the bead was claimed but the queue still records ownership.
//   - Two stale intent files under .harmonik/beads-intents/ (mtime 15 min ago).
//   - Two stale reconciliation lock files under .harmonik/reconciliation-locks/
//     with dead PIDs.
//
// daemon.Start runs the orphan sweep synchronously before the work loop.
// The sweep:
//   - Detects the bead in in_progress, resolves provenance via QueueOwnedSet
//     (bead appears in queue.json as pending, not dispatched), and resets it to
//     open via br update.
//   - Enumerates the two stale intent files (does NOT remove them — left for
//     Cat 3a reconciliation detector).
//   - Removes the two stale reconciliation lock files (dead creator PIDs).
//
// The test asserts that the daemon_orphan_sweep_completed event in the JSONL
// has exactly the expected count values — "reconciles to terminal JSONL counts"
// — i.e. the payload accurately reflects what was remediated:
//
//	bead_in_progress_reset   == 1
//	stale_intents_observed   == 2
//	reconciliation_locks_removed == 2
//
// # Helper prefix
//
// Helpers in this file use the prefix "reapScen" (reap scenario).
// Per implementer-protocol.md §Helper-prefix discipline.
//
// # Spec refs
//
//   - specs/process-lifecycle.md §4.2 PL-006 — orphan sweep mandate.
//   - specs/process-lifecycle.md §4.5 PL-006 sixth bullet — bead-reset sweep.
//   - specs/event-model.md §8.7.14 — daemon_orphan_sweep_completed payload.
//   - specs/queue-model.md §2.7 — ItemStatus values.
//
// Bead: hk-a31od.

import (
	"bufio"
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
	"github.com/gregberns/harmonik/internal/daemon/scenariotest"
)

// ─────────────────────────────────────────────────────────────────────────────
// reapScen fixture helpers
// ─────────────────────────────────────────────────────────────────────────────

// reapScenEvalSymlinks resolves all symlinks in path so that br — which rejects
// paths containing symlinks outside the beads directory — receives a canonical
// path. On macOS, t.TempDir() returns /var/folders/... which is a symlink to
// /private/var/folders/..., triggering br's symlink guard.
func reapScenEvalSymlinks(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("reapScenEvalSymlinks: EvalSymlinks %q: %v", path, err)
	}
	return resolved
}

// reapScenProjectDir creates the minimal project directory for the scenario:
// .harmonik/events/, .harmonik/beads-intents/, and
// .harmonik/reconciliation-locks/. Returns the project dir and the JSONL
// events log path.
func reapScenProjectDir(t *testing.T) (projectDir, jsonlPath string) {
	t.Helper()
	projectDir = reapScenEvalSymlinks(t, t.TempDir())
	for _, sub := range []string{
		filepath.Join(".harmonik", "events"),
		filepath.Join(".harmonik", "beads-intents"),
		filepath.Join(".harmonik", "reconciliation-locks"),
	} {
		//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
		if err := os.MkdirAll(filepath.Join(projectDir, sub), 0o755); err != nil {
			t.Fatalf("reapScenProjectDir: mkdir %s: %v", sub, err)
		}
	}
	jsonlPath = filepath.Join(projectDir, ".harmonik", "events", "events.jsonl")
	return projectDir, jsonlPath
}

// reapScenBrPath returns the path to the real `br` binary, skipping the test
// when br is not on PATH.
func reapScenBrPath(t *testing.T) string {
	t.Helper()
	brPath, err := exec.LookPath("br")
	if err != nil {
		t.Skip("br required for scenario test (not on PATH)")
	}
	return brPath
}

// reapScenBrWrapperScript writes a /bin/sh wrapper that invokes realBrPath
// with --db <dbPath> prepended to all args. Returns the wrapper path.
func reapScenBrWrapperScript(t *testing.T, realBrPath, dbPath string) string {
	t.Helper()
	dir := reapScenEvalSymlinks(t, t.TempDir())
	path := filepath.Join(dir, "br")
	content := "#!/bin/sh\nexec " + realBrPath + " --db " + dbPath + " \"$@\"\n"
	//nolint:gosec // G306: script is test-only; chmod 0755 required for execution
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("reapScenBrWrapperScript: WriteFile: %v", err)
	}
	return path
}

// reapScenInitBrWithInProgress initialises a beads workspace in projectDir,
// creates one bead, sets it to in_progress status. Returns the bead ID.
func reapScenInitBrWithInProgress(t *testing.T, realBrPath, projectDir, brWrapper string) string {
	t.Helper()

	initCmd := exec.CommandContext(t.Context(), realBrPath, "init", "--prefix", "rsp")
	initCmd.Dir = projectDir
	initOut, initErr := initCmd.CombinedOutput()
	if initErr != nil {
		t.Fatalf("reapScenInitBrWithInProgress: br init: %v\n%s", initErr, initOut)
	}

	createCmd := exec.CommandContext(t.Context(), brWrapper, "create",
		"reap scenario orphan-sweep test bead", "--status", "open", "--silent")
	createOut, createErr := createCmd.CombinedOutput()
	if createErr != nil {
		t.Fatalf("reapScenInitBrWithInProgress: br create: %v\n%s", createErr, createOut)
	}
	beadID := strings.TrimSpace(string(createOut))
	if beadID == "" {
		t.Fatal("reapScenInitBrWithInProgress: br create returned empty ID")
	}

	updateCmd := exec.CommandContext(t.Context(), brWrapper, "update", beadID,
		"--status", "in_progress")
	updateOut, updateErr := updateCmd.CombinedOutput()
	if updateErr != nil {
		t.Fatalf("reapScenInitBrWithInProgress: br update in_progress: %v\n%s", updateErr, updateOut)
	}

	return beadID
}

// reapScenWriteQueueJSON writes the "main" queue file to
// .harmonik/queues/main.json under projectDir with a single pending item for
// beadID (queue-owned, not dispatched).
//
// The path is the NQ-A2 named-queues layout (.harmonik/queues/<name>.json, per
// specs/queue-model.md §2.9) that queue.Load reads. The pre-fix legacy path
// (.harmonik/queue.json) is no longer loaded, so the orphan sweep's queue-owned
// provenance check (queue.Load(QueueNameMain)) found an empty set and never
// reset the bead (hk-4f5ua).
func reapScenWriteQueueJSON(t *testing.T, projectDir, beadID string) {
	t.Helper()

	harmonikDir := filepath.Join(projectDir, ".harmonik", "queues")
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(harmonikDir, 0o755); err != nil {
		t.Fatalf("reapScenWriteQueueJSON: MkdirAll .harmonik/queues: %v", err)
	}

	queue := map[string]interface{}{
		"schema_version": 1,
		"queue_id":       "00000000-0000-7000-8000-000000000002",
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
		t.Fatalf("reapScenWriteQueueJSON: marshal: %v", err)
	}

	queuePath := filepath.Join(harmonikDir, "main.json")
	if err := os.WriteFile(queuePath, data, 0o600); err != nil {
		t.Fatalf("reapScenWriteQueueJSON: WriteFile: %v", err)
	}
}

// reapScenSeedStaleIntentFile creates a stale intent file under
// .harmonik/beads-intents/ with mtime set 15 minutes in the past.
func reapScenSeedStaleIntentFile(t *testing.T, projectDir, intentID string) {
	t.Helper()

	intentsDir := filepath.Join(projectDir, ".harmonik", "beads-intents")
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(intentsDir, 0o755); err != nil {
		t.Fatalf("reapScenSeedStaleIntentFile: MkdirAll: %v", err)
	}

	intentPath := filepath.Join(intentsDir, intentID+".json")
	content := fmt.Sprintf(`{"intent_id":%q,"bead_id":"rsp-0001","created_at":%q}`,
		intentID, time.Now().Add(-15*time.Minute).Format(time.RFC3339))
	if err := os.WriteFile(intentPath, []byte(content), 0o600); err != nil {
		t.Fatalf("reapScenSeedStaleIntentFile: WriteFile: %v", err)
	}
	past := time.Now().Add(-15 * time.Minute)
	if err := os.Chtimes(intentPath, past, past); err != nil {
		t.Fatalf("reapScenSeedStaleIntentFile: Chtimes: %v", err)
	}
}

// reapScenSeedReconciliationLock creates a stale reconciliation lock file
// under .harmonik/reconciliation-locks/ with a dead creator PID (99999).
func reapScenSeedReconciliationLock(t *testing.T, projectDir, runID string) {
	t.Helper()

	lockDir := filepath.Join(projectDir, ".harmonik", "reconciliation-locks")
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(lockDir, 0o755); err != nil {
		t.Fatalf("reapScenSeedReconciliationLock: MkdirAll: %v", err)
	}

	lockPath := filepath.Join(lockDir, runID+".lock")
	const deadPID = 99999
	content := fmt.Sprintf("creator_pid=%d\nrun_id=%s\n", deadPID, runID)
	if err := os.WriteFile(lockPath, []byte(content), 0o600); err != nil {
		t.Fatalf("reapScenSeedReconciliationLock: WriteFile: %v", err)
	}
}

// reapScenOrphanSweepPayload is the decoded payload of a
// daemon_orphan_sweep_completed JSONL event.
type reapScenOrphanSweepPayload struct {
	TmuxSessionsKilled         int    `json:"tmux_sessions_killed"`
	LocksCleared               int    `json:"locks_cleared"`
	SubprocessesKilled         int    `json:"subprocesses_killed"`
	BrSubprocessesKilled       int    `json:"br_subprocesses_killed"`
	ReconciliationLocksRemoved int    `json:"reconciliation_locks_removed"`
	StaleIntentsObserved       int    `json:"stale_intents_observed"`
	BeadInProgressReset        int    `json:"bead_in_progress_reset"`
	BeadCat3cClosed            int    `json:"bead_cat3c_closed"`
	SweptAt                    string `json:"swept_at"`
}

// reapScenExtractOrphanSweepPayload reads the JSONL log and returns the decoded
// payload of the first daemon_orphan_sweep_completed event. Fails the test if
// the event is not found or the payload is malformed.
func reapScenExtractOrphanSweepPayload(t *testing.T, jsonlPath string) reapScenOrphanSweepPayload {
	t.Helper()

	//nolint:gosec // G304: path is t.TempDir()-based; not user input
	f, err := os.Open(jsonlPath)
	if err != nil {
		t.Fatalf("reapScenExtractOrphanSweepPayload: open %s: %v", jsonlPath, err)
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			t.Logf("reapScenExtractOrphanSweepPayload: close: %v", closeErr)
		}
	}()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		// Decode envelope to check type.
		var env struct {
			Type    string          `json:"type"`
			Payload json.RawMessage `json:"payload"`
		}
		if decErr := json.Unmarshal([]byte(line), &env); decErr != nil {
			continue
		}
		if env.Type != string(core.EventTypeDaemonOrphanSweepCompleted) {
			continue
		}
		// Decode the payload.
		var p reapScenOrphanSweepPayload
		if decErr := json.Unmarshal(env.Payload, &p); decErr != nil {
			t.Fatalf("reapScenExtractOrphanSweepPayload: decode payload: %v\npayload: %s", decErr, env.Payload)
		}
		return p
	}

	t.Fatalf("reapScenExtractOrphanSweepPayload: %s event not found in %s",
		core.EventTypeDaemonOrphanSweepCompleted, jsonlPath)
	return reapScenOrphanSweepPayload{} // unreachable
}

// ─────────────────────────────────────────────────────────────────────────────
// TestScenario_Reap_BootOrphanSweepCounts
// ─────────────────────────────────────────────────────────────────────────────

// TestScenario_Reap_BootOrphanSweepCounts is the integration test for the
// reap scenario: daemon boot orphan-sweep remediates multiple orphaned resource
// types and the daemon_orphan_sweep_completed JSONL payload counts reconcile.
//
// Setup:
//  1. TempDir project with br DB seeded with one bead in `in_progress` status.
//  2. queue.json with the bead as status=pending (queue-owned, not dispatched).
//  3. Two stale intent files under .harmonik/beads-intents/ (mtime −15 min).
//  4. Two stale reconciliation lock files with dead creator PIDs.
//  5. daemon.Start wired with BrPath; no HandlerBinary needed for sweep path.
//
// Assertions:
//  1. daemon_orphan_sweep_completed event appears in the JSONL log.
//  2. Payload counts reconcile:
//     - bead_in_progress_reset   == 1 (queue-owned provenance → reset to open)
//     - stale_intents_observed   == 2 (two intent files enumerated)
//     - reconciliation_locks_removed == 2 (two dead-PID lock files removed)
//  3. Bead status == "open" in the br DB (reset by queue-owned provenance path).
//
// Bead: hk-a31od.
func TestScenario_Reap_BootOrphanSweepCounts(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	// Not parallel: uses os.Setenv(HARMONIK_CLAUDE_CONFIG_PATH) to isolate
	// EnsureWorktreeTrust — same rationale as TestScenario_HappyPath_N1.

	// Locate br binary; skip when absent.
	realBrPath := reapScenBrPath(t)

	// Create project directory.
	projectDir, jsonlPath := reapScenProjectDir(t)

	// Initialise br DB and seed one bead in in_progress status.
	dbPath := filepath.Join(projectDir, ".beads", "beads.db")
	brWrapper := reapScenBrWrapperScript(t, realBrPath, dbPath)
	beadID := reapScenInitBrWithInProgress(t, realBrPath, projectDir, brWrapper)
	t.Logf("reapScen: seeded bead ID = %s (in_progress)", beadID)

	// Verify initial bead state.
	scenariotest.AssertBeadStatus(t, brWrapper, beadID, "in_progress")

	// Write queue.json: bead is queue-owned (pending) but not dispatched.
	reapScenWriteQueueJSON(t, projectDir, beadID)

	// Seed two stale intent files.
	reapScenSeedStaleIntentFile(t, projectDir, "intent-reap-001")
	reapScenSeedStaleIntentFile(t, projectDir, "intent-reap-002")

	// Seed two stale reconciliation lock files with dead PIDs.
	reapScenSeedReconciliationLock(t, projectDir, "run-reap-lock-a")
	reapScenSeedReconciliationLock(t, projectDir, "run-reap-lock-b")

	// Redirect EnsureWorktreeTrust to a test-local config path.
	claudeConfigPath := filepath.Join(reapScenEvalSymlinks(t, t.TempDir()), ".claude.json")
	prevClaudeCfg, hadClaudeCfg := os.LookupEnv("HARMONIK_CLAUDE_CONFIG_PATH")
	if err := os.Setenv("HARMONIK_CLAUDE_CONFIG_PATH", claudeConfigPath); err != nil {
		t.Fatalf("reapScen: Setenv HARMONIK_CLAUDE_CONFIG_PATH: %v", err)
	}
	// hk-1o0cc: restore prior value (TestMain package default) — see scenario_happypath_n1.
	t.Cleanup(func() {
		if hadClaudeCfg {
			_ = os.Setenv("HARMONIK_CLAUDE_CONFIG_PATH", prevClaudeCfg)
		} else {
			_ = os.Unsetenv("HARMONIK_CLAUDE_CONFIG_PATH")
		}
	})

	// Wire daemon.Config: orphan-sweep-only integration test.
	// No HandlerBinary: we cancel context after the sweep completes, before
	// any work-loop dispatch.
	loopCtx, loopCancel := context.WithCancel(context.Background())
	defer loopCancel()

	cfg := daemon.Config{
		ProjectDir:            projectDir,
		JSONLLogPath:          jsonlPath,
		BrPath:                brWrapper,
		SkipWALCheckpoint:     true,
		SkipBrHistoryRotation: true,
		LogWriter:             testLogWriter{t: t},
		// WorkflowModeDefault is required by daemon.Start since hk-81n9r
		// (9835491b). This sweep-only test cancels before any dispatch, so the
		// value is never exercised by a workloop run; review-loop matches the
		// daemon's documented default. Without it, daemon.Start returns the
		// "WorkflowModeDefault must be set (PL-004a)" error (hk-4f5ua).
		WorkflowModeDefault: core.WorkflowModeReviewLoop,
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
	// daemon_orphan_sweep_completed. Budget: 10 s is generous; the sweep itself
	// is sub-second in CI.
	const sweepPollBudget = 10 * time.Second
	scenariotest.MustCompleteWithin(t, jsonlPath, "", nil, sweepPollBudget, func() {
		for {
			if scenariotest.WaitForEvent(t, jsonlPath, "daemon_orphan_sweep_completed", "", 50*time.Millisecond) {
				return
			}
		}
	})

	// Cancel the daemon context to stop the work loop.
	loopCancel()

	// Wait for daemon.Start to return (up to 5 s).
	scenariotest.MustCompleteWithin(t, jsonlPath, "", nil, 5*time.Second, func() {
		if err := <-startDone; err != nil {
			t.Errorf("daemon.Start returned error after context cancel: %v", err)
		}
	})

	// ── Assertion 1: extract and verify payload counts ────────────────────────
	//
	// The daemon_orphan_sweep_completed payload must reflect exactly what was
	// seeded: 1 bead reset, 2 stale intents observed, 2 recon locks removed.
	payload := reapScenExtractOrphanSweepPayload(t, jsonlPath)

	if payload.BeadInProgressReset != 1 {
		t.Errorf("reapScen: bead_in_progress_reset = %d, want 1", payload.BeadInProgressReset)
	}
	if payload.StaleIntentsObserved != 2 {
		t.Errorf("reapScen: stale_intents_observed = %d, want 2", payload.StaleIntentsObserved)
	}
	if payload.ReconciliationLocksRemoved != 2 {
		t.Errorf("reapScen: reconciliation_locks_removed = %d, want 2", payload.ReconciliationLocksRemoved)
	}

	// ── Assertion 2: bead reset to open ──────────────────────────────────────
	//
	// The orphan sweep detected the bead in in_progress, established ownership
	// via QueueOwnedSet (bead_id appears in queue.json as pending, not dispatched),
	// and called ResetBead → br update --status open.
	scenariotest.AssertBeadStatus(t, brWrapper, beadID, "open")

	// ── Causality invariants (hk-xegej) ──────────────────────────────────────
	// run_started is absent in this sweep-only scenario; both checks pass vacuously.
	scenariotest.AssertEventCausality(t, jsonlPath,
		"run_started",
		[]string{"run_completed", "run_failed", "run_cancelled"},
		60*time.Second,
	)
	scenariotest.AssertEventCausality(t, jsonlPath,
		"implementer_commit",
		[]string{"reviewer_launched", "run_completed"},
		30*time.Second,
	)

	t.Logf("reapScen: PASS bead=%s reset=1 intents=2 recon-locks=2", beadID)
}

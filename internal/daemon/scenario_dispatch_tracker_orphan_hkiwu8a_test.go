package daemon_test

// scenario_dispatch_tracker_orphan_hkiwu8a_test.go — integration test for the
// hk-iwu8a fix: reconcileOrphanedRunsOnResume sourcing orphans from the live
// dispatch-tracker (queueDispatched), not just an observed run_started event or
// a .harmonik/runs/ record.
//
// # What is tested
//
// TestScenario_DispatchTrackerOrphan_ClearsWedgedDispatchLock boots the full
// daemon.Start composition root with:
//
//   - A real br DB seeded with one bead in `in_progress` status.
//   - A pre-written queue.json whose single item carries that bead_id at
//     status=dispatched (the queue believes a run is executing it).
//   - An EMPTY events.jsonl (no run_started was ever recorded) and no
//     .harmonik/runs/ record — reproducing the crash window where the daemon
//     was killed after the queue-claim write and the bead claim, but before it
//     wrote run_started or the runs/ record.
//
// Before the hk-iwu8a fix, reconcileOrphanedRunsOnResume had no way to see this
// bead (it only enumerates via an observed run_started or a runs/ record), so
// it never reset the bead to open. QM-002a's reconcileDispatchedItems only
// reverts a dispatched queue item when Beads confirms the bead as open — so
// with the bead stuck at in_progress, the queue item stayed dispatched and the
// bead's -32015 (bead_already_dispatched) lock survived every restart. After
// the fix, the dispatch-tracker pass resets the bead to open BEFORE
// LoadQueueAtStartup runs, letting QM-002a revert the queue item to pending.
//
// # Helper prefix
//
// Helpers in this file use the prefix "dtOrphan" (dispatch-tracker orphan).
// Per implementer-protocol.md §Helper-prefix discipline.
//
// # Spec refs
//
//   - specs/process-lifecycle.md §4.5 PL-006 (hk-r73qr).
//   - specs/queue-model.md §3.2a QM-002a (hk-mdus1).
//
// Bead: hk-iwu8a.

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

// dtOrphanEvalSymlinks resolves all symlinks in path so that br — which
// rejects paths containing symlinks outside the beads directory — receives a
// canonical path.
func dtOrphanEvalSymlinks(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("dtOrphanEvalSymlinks: EvalSymlinks %q: %v", path, err)
	}
	return resolved
}

// dtOrphanProjectDir creates the minimal project directory for the scenario.
func dtOrphanProjectDir(t *testing.T) (projectDir, jsonlPath string) {
	t.Helper()
	projectDir = dtOrphanEvalSymlinks(t, t.TempDir())
	for _, sub := range []string{
		filepath.Join(".harmonik", "events"),
		filepath.Join(".harmonik", "beads-intents"),
	} {
		//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
		if err := os.MkdirAll(filepath.Join(projectDir, sub), 0o755); err != nil {
			t.Fatalf("dtOrphanProjectDir: mkdir %s: %v", sub, err)
		}
	}
	jsonlPath = filepath.Join(projectDir, ".harmonik", "events", "events.jsonl")
	// Create an empty events.jsonl — no run_started was ever written for the
	// bead under test; this is the crash window the fix covers.
	if err := os.WriteFile(jsonlPath, nil, 0o600); err != nil {
		t.Fatalf("dtOrphanProjectDir: create empty events.jsonl: %v", err)
	}
	return projectDir, jsonlPath
}

// dtOrphanBrPath returns the path to the real `br` binary, skipping the test
// when br is not on PATH.
func dtOrphanBrPath(t *testing.T) string {
	t.Helper()
	brPath, err := exec.LookPath("br")
	if err != nil {
		t.Skip("br required for scenario test (not on PATH)")
	}
	return brPath
}

// dtOrphanBrWrapperScript writes a /bin/sh wrapper that invokes realBrPath
// with --db <dbPath> prepended to all args.
func dtOrphanBrWrapperScript(t *testing.T, realBrPath, dbPath string) string {
	t.Helper()
	dir := dtOrphanEvalSymlinks(t, t.TempDir())
	path := filepath.Join(dir, "br")
	content := "#!/bin/sh\nexec " + realBrPath + " --db " + dbPath + " \"$@\"\n"
	//nolint:gosec // G306: script is test-only; chmod 0755 required for execution
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("dtOrphanBrWrapperScript: WriteFile: %v", err)
	}
	return path
}

// dtOrphanInitBrWithInProgress initialises a beads workspace in projectDir,
// creates one bead, and sets it to in_progress status (simulating a bead
// whose claim landed but whose run never got as far as writing run_started).
// Returns the bead ID.
func dtOrphanInitBrWithInProgress(t *testing.T, realBrPath, projectDir, brWrapper string) string {
	t.Helper()

	//nolint:gosec // G204: br args are test-internal literals; not user input
	initCmd := exec.CommandContext(t.Context(), realBrPath, "init", "--prefix", "dto")
	initCmd.Dir = projectDir
	initOut, initErr := initCmd.CombinedOutput()
	if initErr != nil {
		t.Fatalf("dtOrphanInitBrWithInProgress: br init: %v\n%s", initErr, initOut)
	}

	//nolint:gosec // G204: br args are test-internal literals; not user input
	createCmd := exec.CommandContext(t.Context(), brWrapper, "create",
		"dispatch-tracker orphan test bead", "--status", "open", "--silent")
	createOut, createErr := createCmd.CombinedOutput()
	if createErr != nil {
		t.Fatalf("dtOrphanInitBrWithInProgress: br create: %v\n%s", createErr, createOut)
	}
	beadID := strings.TrimSpace(string(createOut))
	if beadID == "" {
		t.Fatal("dtOrphanInitBrWithInProgress: br create returned empty ID")
	}

	//nolint:gosec // G204: br args are test-internal literals; not user input
	updateCmd := exec.CommandContext(t.Context(), brWrapper, "update", beadID,
		"--status", "in_progress")
	updateOut, updateErr := updateCmd.CombinedOutput()
	if updateErr != nil {
		t.Fatalf("dtOrphanInitBrWithInProgress: br update in_progress: %v\n%s", updateErr, updateOut)
	}

	return beadID
}

// dtOrphanQueuePath returns the path queue.Load reads for the "main" queue.
func dtOrphanQueuePath(projectDir string) string {
	return filepath.Join(projectDir, ".harmonik", "queues", "main.json")
}

// dtOrphanWriteQueueJSON writes the "main" queue file with one dispatched item
// for beadID — the durable "live dispatch-tracker" believes a run is currently
// executing it, even though no run_started event and no runs/ record exist.
func dtOrphanWriteQueueJSON(t *testing.T, projectDir, beadID string) {
	t.Helper()

	harmonikDir := filepath.Join(projectDir, ".harmonik", "queues")
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(harmonikDir, 0o755); err != nil {
		t.Fatalf("dtOrphanWriteQueueJSON: MkdirAll .harmonik/queues: %v", err)
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
						"status":  "dispatched",
					},
				},
			},
		},
	}

	data, err := json.Marshal(queue)
	if err != nil {
		t.Fatalf("dtOrphanWriteQueueJSON: marshal: %v", err)
	}

	if err := os.WriteFile(dtOrphanQueuePath(projectDir), data, 0o600); err != nil {
		t.Fatalf("dtOrphanWriteQueueJSON: WriteFile: %v", err)
	}
}

// dtOrphanFindReconciledPayload scans jsonlPath for the queue_item_reconciled
// event for beadID and returns its decoded payload. Reading the durable event
// log (rather than re-reading the live, mutable queue.json — which the work
// loop may rewrite the instant the reverted item goes pending) is what makes
// this assertion race-free: the event is an immutable record of what QM-002a
// actually did, independent of anything that happens afterwards.
func dtOrphanFindReconciledPayload(t *testing.T, jsonlPath, beadID string) core.QueueItemReconciledPayload {
	t.Helper()
	data, err := os.ReadFile(jsonlPath) //nolint:gosec // G304: test-constructed path
	if err != nil {
		t.Fatalf("dtOrphanFindReconciledPayload: ReadFile: %v", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if line == "" {
			continue
		}
		var env struct {
			Type    string          `json:"type"`
			Payload json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal([]byte(line), &env); err != nil {
			continue
		}
		if env.Type != string(core.EventTypeQueueItemReconciled) {
			continue
		}
		var pl core.QueueItemReconciledPayload
		if err := json.Unmarshal(env.Payload, &pl); err != nil {
			continue
		}
		if pl.BeadID == beadID {
			return pl
		}
	}
	t.Fatalf("dtOrphanFindReconciledPayload: no queue_item_reconciled event found for bead %s", beadID)
	return core.QueueItemReconciledPayload{}
}

// ─────────────────────────────────────────────────────────────────────────────
// TestScenario_DispatchTrackerOrphan_ClearsWedgedDispatchLock
// ─────────────────────────────────────────────────────────────────────────────

// TestScenario_DispatchTrackerOrphan_ClearsWedgedDispatchLock reproduces the
// hk-iwu8a bug end-to-end through the real daemon.Start composition root and
// verifies the fix clears it.
//
// Setup:
//  1. TempDir project with br DB seeded with one bead in `in_progress` status.
//  2. queue.json written with that bead_id at status=dispatched (the live
//     dispatch-tracker), an EMPTY events.jsonl, and no runs/ record.
//  3. daemon.Start wired with BrPath; no HandlerBinary needed.
//
// Assertions:
//  1. queue_item_reconciled fires for the item (QM-002a reverted it).
//  2. Bead status == "open" in the br DB (reset by the dispatch-tracker pass).
//  3. The queue item status == "pending" (reverted from "dispatched").
//
// Bead: hk-iwu8a.
func TestScenario_DispatchTrackerOrphan_ClearsWedgedDispatchLock(t *testing.T) {
	skipRealDaemonE2EInShort(t)

	realBrPath := dtOrphanBrPath(t)

	projectDir, jsonlPath := dtOrphanProjectDir(t)

	dbPath := filepath.Join(projectDir, ".beads", "beads.db")
	brWrapper := dtOrphanBrWrapperScript(t, realBrPath, dbPath)
	beadID := dtOrphanInitBrWithInProgress(t, realBrPath, projectDir, brWrapper)
	t.Logf("dtOrphan: seeded bead ID = %s (in_progress)", beadID)

	scenariotest.AssertBeadStatus(t, brWrapper, beadID, "in_progress")

	// Queue believes the bead is dispatched (the live dispatch-tracker), but
	// there is no run_started event and no runs/ record for it anywhere.
	dtOrphanWriteQueueJSON(t, projectDir, beadID)

	claudeConfigPath := filepath.Join(dtOrphanEvalSymlinks(t, t.TempDir()), ".claude.json")
	prevClaudeCfg, hadClaudeCfg := os.LookupEnv("HARMONIK_CLAUDE_CONFIG_PATH")
	if err := os.Setenv("HARMONIK_CLAUDE_CONFIG_PATH", claudeConfigPath); err != nil {
		t.Fatalf("dtOrphan: Setenv HARMONIK_CLAUDE_CONFIG_PATH: %v", err)
	}
	t.Cleanup(func() {
		if hadClaudeCfg {
			_ = os.Setenv("HARMONIK_CLAUDE_CONFIG_PATH", prevClaudeCfg)
		} else {
			_ = os.Unsetenv("HARMONIK_CLAUDE_CONFIG_PATH")
		}
	})

	loopCtx, loopCancel := context.WithCancel(context.Background())
	defer loopCancel()

	cfg := daemon.Config{
		ProjectDir:            projectDir,
		JSONLLogPath:          jsonlPath,
		BrPath:                brWrapper,
		SkipWALCheckpoint:     true,
		SkipBrHistoryRotation: true,
		LogWriter:             testLogWriter{t: t},
		WorkflowModeDefault:   core.WorkflowModeDot,
	}

	startDone := make(chan error, 1)
	go func() {
		startDone <- daemon.Start(loopCtx, cfg)
	}()

	// ── Wait for QM-002a to revert the wedged queue item ─────────────────────
	//
	// queue_item_reconciled is emitted by LoadQueueAtStartup's QM-002a pass,
	// which runs AFTER reconcileOrphanedRunsOnResume. It only fires here if the
	// dispatch-tracker pass already reset the bead to open — pre-fix, the bead
	// stays in_progress and reconcileDispatchedItems' ShowBead-open check never
	// fires, so this event never appears within the budget and the test fails
	// (t.Fatalf below), proving the test is diagnostic.
	const reconcilePollBudget = 10 * time.Second
	found := false
	scenariotest.MustCompleteWithin(t, jsonlPath, "", nil, reconcilePollBudget, func() {
		for {
			if scenariotest.WaitForEvent(t, jsonlPath, string(core.EventTypeQueueItemReconciled), "", 50*time.Millisecond) {
				found = true
				return
			}
		}
	})
	loopCancel()

	scenariotest.MustCompleteWithin(t, jsonlPath, "", nil, 5*time.Second, func() {
		if err := <-startDone; err != nil {
			t.Errorf("daemon.Start returned error after context cancel: %v", err)
		}
	})

	if !found {
		t.Fatalf("dtOrphan: queue_item_reconciled never observed within %s — the dispatch-tracker orphan's -32015 lock was not cleared", reconcilePollBudget)
	}

	// ── Assertions ────────────────────────────────────────────────────────────
	scenariotest.AssertBeadStatus(t, brWrapper, beadID, "open")

	pl := dtOrphanFindReconciledPayload(t, jsonlPath, beadID)
	if pl.Reason != "claim_write_lost" {
		t.Fatalf("dtOrphan: queue_item_reconciled reason = %q, want %q", pl.Reason, "claim_write_lost")
	}
	if pl.GroupIndex != 0 {
		t.Fatalf("dtOrphan: queue_item_reconciled group_index = %d, want 0", pl.GroupIndex)
	}

	t.Logf("dtOrphan: PASS bead=%s reset to open, queue item reverted to pending (event: %+v)", beadID, pl)
}

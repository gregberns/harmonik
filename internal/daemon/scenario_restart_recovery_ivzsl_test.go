//go:build scenario

package daemon_test

// scenario_restart_recovery_ivzsl_test.go — live restart-recovery harness for the
// dispatched+bead-closed deadlock combination (hk-ivzsl).
//
// # What is tested
//
// TestScenario_RestartRecovery_QM002bDeadlock boots the full daemon.Start
// composition root with a pre-seeded "stuck" queue that reproduces the exact
// deadlock that hk-z0pmi and hk-5pg37 target:
//
//   - bead A: ItemStatus=dispatched in queue.json + CoarseStatus=closed in br
//     (simulates a bead that landed via another queue after the first daemon
//     was killed mid-run, leaving the claim goroutine abandoned).
//
//   - bead B: ItemStatus=failed with LastFailureReason="cross_queue_duplicate"
//     (QM-034 — a failed sibling that must not interrupt the dispatched item).
//
// Before the Class A' fix (f82c051e), the group could NEVER advance to terminal
// because QM-034 bars advancement while any item is in dispatched (non-terminal)
// state.  The group stays active → QM-027 blocks all subsequent submits.
//
// After the Class A' fix:
//   - daemon.Start calls LoadQueueAtStartup → reconcileThreeWay detects
//     bead A is dispatched+closed → advances item to completed.
//   - Both items are now terminal (completed + failed).
//   - daemon.Start emits reconciliation_mismatch_observed with
//     mismatch_class=bead_closed_queue_dispatched.
//   - On context cancel the work loop calls drainCancelledQueue, which
//     renames .harmonik/queues/main.json to *.cancelled-<ts> so the NEXT
//     daemon start does NOT see a blocking active queue.
//
// # Assertions
//
//  1. reconciliation_mismatch_observed with mismatch_class=bead_closed_queue_dispatched fires.
//  2. On disk, item A's status is "completed" (Class A' persisted the correction).
//  3. After daemon exits, .harmonik/queues/main.json is absent (drainCancelledQueue renamed it).
//  4. queue.Load returns nil (no active queue → QM-027 would not block).
//  5. A NEW queue can be written and loaded without error (proving the wedge is gone).
//
// # Helper prefix
//
// Helpers in this file use the prefix "rrRecov" (restart-recovery).
// Per implementer-protocol.md §Helper-prefix discipline.
//
// # Spec refs
//
//   - specs/queue-model.md §3.2b QM-002b Class A' — dispatched+closed advance.
//   - specs/queue-model.md §5 QM-034 — failed items must not interrupt siblings.
//   - specs/queue-model.md §6 QM-027 — single-active-queue guard.
//   - specs/process-lifecycle.md §4.2 PL-005 step 8a.
//
// Bead: hk-ivzsl.

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
	"github.com/gregberns/harmonik/internal/queue"
)

// ─────────────────────────────────────────────────────────────────────────────
// rrRecov fixture helpers
// ─────────────────────────────────────────────────────────────────────────────

// rrRecovEvalSymlinks resolves all symlinks in path so that br — which rejects
// paths containing symlinks outside the beads directory — receives a canonical
// path. On macOS, t.TempDir() returns /var/folders/... which is a symlink to
// /private/var/folders/..., triggering br's symlink guard.
func rrRecovEvalSymlinks(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("rrRecovEvalSymlinks: EvalSymlinks %q: %v", path, err)
	}
	return resolved
}

// rrRecovProjectDir creates the minimal project directory for the scenario.
// Returns the project dir and the JSONL events log path.
func rrRecovProjectDir(t *testing.T) (projectDir, jsonlPath string) {
	t.Helper()
	projectDir = rrRecovEvalSymlinks(t, t.TempDir())
	for _, sub := range []string{
		filepath.Join(".harmonik", "events"),
		filepath.Join(".harmonik", "beads-intents"),
		filepath.Join(".harmonik", "queues"),
	} {
		//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
		if err := os.MkdirAll(filepath.Join(projectDir, sub), 0o755); err != nil {
			t.Fatalf("rrRecovProjectDir: mkdir %s: %v", sub, err)
		}
	}
	jsonlPath = filepath.Join(projectDir, ".harmonik", "events", "events.jsonl")
	return projectDir, jsonlPath
}

// rrRecovBrPath returns the path to the real `br` binary, skipping the test
// when br is not on PATH.
func rrRecovBrPath(t *testing.T) string {
	t.Helper()
	brPath, err := exec.LookPath("br")
	if err != nil {
		t.Skip("br required for scenario test (not on PATH)")
	}
	return brPath
}

// rrRecovBrWrapperScript writes a /bin/sh wrapper that invokes realBrPath
// with --db <dbPath> prepended to all args. Returns the wrapper path.
func rrRecovBrWrapperScript(t *testing.T, realBrPath, dbPath string) string {
	t.Helper()
	dir := rrRecovEvalSymlinks(t, t.TempDir())
	path := filepath.Join(dir, "br")
	content := "#!/bin/sh\nexec " + realBrPath + " --db " + dbPath + " \"$@\"\n"
	//nolint:gosec // G306: script is test-only; chmod 0755 required for execution
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("rrRecovBrWrapperScript: WriteFile: %v", err)
	}
	return path
}

// rrRecovInitBrWithBeads initialises a beads workspace and creates two beads:
//   - beadA is created open then closed (simulates "landed via another path").
//   - beadB is created open (will have queue status=failed but br status=open;
//     the pre-claim guard on the queue path already failed it).
//
// Returns (beadAID, beadBID).
func rrRecovInitBrWithBeads(t *testing.T, realBrPath, projectDir, brWrapper string) (beadAID, beadBID string) {
	t.Helper()

	//nolint:gosec // G204: br args are test-internal literals; not user input
	initCmd := exec.CommandContext(t.Context(), realBrPath, "init", "--prefix", "rrr")
	initCmd.Dir = projectDir
	initOut, initErr := initCmd.CombinedOutput()
	if initErr != nil {
		t.Fatalf("rrRecovInitBrWithBeads: br init: %v\n%s", initErr, initOut)
	}

	// Create bead A — simulates the bead that was dispatched and landed elsewhere.
	//nolint:gosec // G204: br args are test-internal literals; not user input
	createA := exec.CommandContext(t.Context(), brWrapper, "create",
		"restart-recovery test bead A (dispatched+closed)", "--status", "open", "--silent")
	outA, errA := createA.CombinedOutput()
	if errA != nil {
		t.Fatalf("rrRecovInitBrWithBeads: br create A: %v\n%s", errA, outA)
	}
	beadAID = strings.TrimSpace(string(outA))
	if beadAID == "" {
		t.Fatal("rrRecovInitBrWithBeads: br create A returned empty ID")
	}

	// Close bead A — models "landed via another queue or direct br close".
	//nolint:gosec // G204: br args are test-internal literals; not user input
	closeA := exec.CommandContext(t.Context(), brWrapper, "close", beadAID, "--reason", "landed-via-other-path")
	closeAOut, closeAErr := closeA.CombinedOutput()
	if closeAErr != nil {
		t.Fatalf("rrRecovInitBrWithBeads: br close A: %v\n%s", closeAErr, closeAOut)
	}

	// Create bead B — will be represented as failed in the queue (cross_queue_duplicate).
	//nolint:gosec // G204: br args are test-internal literals; not user input
	createB := exec.CommandContext(t.Context(), brWrapper, "create",
		"restart-recovery test bead B (failed sibling)", "--status", "open", "--silent")
	outB, errB := createB.CombinedOutput()
	if errB != nil {
		t.Fatalf("rrRecovInitBrWithBeads: br create B: %v\n%s", errB, outB)
	}
	beadBID = strings.TrimSpace(string(outB))
	if beadBID == "" {
		t.Fatal("rrRecovInitBrWithBeads: br create B returned empty ID")
	}

	return beadAID, beadBID
}

// rrRecovWriteStuckQueueJSON writes .harmonik/queues/main.json under projectDir
// with the "deadlock combination" that blocked groups before Class A':
//   - item 0 (beadAID): status=dispatched + run_id set (stuck from crash)
//   - item 1 (beadBID): status=failed + last_failure_reason=cross_queue_duplicate
//
// This reproduces the exact combination described in hk-z0pmi: a dispatched
// item whose bead landed elsewhere paired with a failed sibling.  Before Class
// A', QM-034's "dispatched blocks advance" invariant kept this group stuck forever.
func rrRecovWriteStuckQueueJSON(t *testing.T, projectDir, beadAID, beadBID string) {
	t.Helper()

	queuesDir := filepath.Join(projectDir, ".harmonik", "queues")
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(queuesDir, 0o755); err != nil {
		t.Fatalf("rrRecovWriteStuckQueueJSON: MkdirAll queues: %v", err)
	}

	runID := "rrr-run-0001-aaaa-bbbb-cccc-000000000001"
	now := time.Now().UTC().Format(time.RFC3339)
	q := map[string]interface{}{
		"schema_version": 1,
		"queue_id":       "00000000-0000-7000-8000-aaaa000000001",
		"submitted_at":   now,
		"status":         "active",
		"groups": []map[string]interface{}{
			{
				"group_index": 0,
				"kind":        "wave",
				"status":      "active",
				"created_at":  now,
				"started_at":  now,
				"items": []map[string]interface{}{
					{
						"bead_id": beadAID,
						"status":  "dispatched",
						"run_id":  runID,
					},
					{
						"bead_id":             beadBID,
						"status":              "failed",
						"last_failure_reason": "cross_queue_duplicate",
					},
				},
			},
		},
	}

	data, err := json.Marshal(q)
	if err != nil {
		t.Fatalf("rrRecovWriteStuckQueueJSON: marshal: %v", err)
	}

	queuePath := filepath.Join(queuesDir, "main.json")
	if err := os.WriteFile(queuePath, data, 0o600); err != nil {
		t.Fatalf("rrRecovWriteStuckQueueJSON: WriteFile: %v", err)
	}
}

// rrRecovMismatchPayload is the decoded payload of a
// reconciliation_mismatch_observed JSONL event.
type rrRecovMismatchPayload struct {
	QueueID       string `json:"queue_id"`
	GroupIndex    int    `json:"group_index"`
	BeadID        string `json:"bead_id"`
	MismatchClass string `json:"mismatch_class"`
	LedgerStatus  string `json:"ledger_status"`
	QueueStatus   string `json:"queue_status"`
	ObservedAt    string `json:"observed_at"`
}

// rrRecovExtractMismatchPayload reads the JSONL log and returns the decoded
// payload of the first reconciliation_mismatch_observed event whose
// mismatch_class equals wantClass. Fails the test if not found.
func rrRecovExtractMismatchPayload(t *testing.T, jsonlPath, wantClass string) rrRecovMismatchPayload {
	t.Helper()

	//nolint:gosec // G304: path is t.TempDir()-based; not user input
	f, err := os.Open(jsonlPath)
	if err != nil {
		t.Fatalf("rrRecovExtractMismatchPayload: open %s: %v", jsonlPath, err)
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			t.Logf("rrRecovExtractMismatchPayload: close: %v", closeErr)
		}
	}()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var env struct {
			Type    string          `json:"type"`
			Payload json.RawMessage `json:"payload"`
		}
		if json.Unmarshal([]byte(line), &env) != nil {
			continue
		}
		if env.Type != "reconciliation_mismatch_observed" {
			continue
		}
		var p rrRecovMismatchPayload
		if json.Unmarshal(env.Payload, &p) != nil {
			continue
		}
		if p.MismatchClass == wantClass {
			return p
		}
	}

	t.Fatalf("rrRecovExtractMismatchPayload: event type=reconciliation_mismatch_observed mismatch_class=%q not found in %s",
		wantClass, jsonlPath)
	return rrRecovMismatchPayload{} // unreachable
}

// rrRecovReadGroupItemStatuses reads the on-disk queue file and returns the
// item statuses in group 0 in order.
func rrRecovReadGroupItemStatuses(t *testing.T, projectDir string) []string {
	t.Helper()

	queuePath := filepath.Join(projectDir, ".harmonik", "queues", "main.json")
	data, err := os.ReadFile(queuePath) //nolint:gosec // G304: path is t.TempDir()-based; not user input
	if err != nil {
		t.Fatalf("rrRecovReadGroupItemStatuses: ReadFile: %v", err)
	}

	var q struct {
		Groups []struct {
			Items []struct {
				Status string `json:"status"`
			} `json:"items"`
		} `json:"groups"`
	}
	if err := json.Unmarshal(data, &q); err != nil {
		t.Fatalf("rrRecovReadGroupItemStatuses: unmarshal: %v", err)
	}
	if len(q.Groups) == 0 {
		t.Fatal("rrRecovReadGroupItemStatuses: no groups in queue")
	}

	statuses := make([]string, len(q.Groups[0].Items))
	for i, item := range q.Groups[0].Items {
		statuses[i] = item.Status
	}
	return statuses
}

// ─────────────────────────────────────────────────────────────────────────────
// TestScenario_RestartRecovery_QM002bDeadlock
// ─────────────────────────────────────────────────────────────────────────────

// TestScenario_RestartRecovery_QM002bDeadlock is the scenario-level complement
// to the unit tests in internal/lifecycle/startup_pl005_qm002_test.go.  It
// exercises the LIVE restart-recovery path through the full daemon.Start
// composition root.
//
// The test pre-seeds the exact "deadlock combination" from hk-z0pmi:
//   - One bead stuck at ItemStatus=dispatched in queue.json (crashed daemon
//     left its goroutine abandoned) while its br status is already closed
//     (landed via another path).  Without QM-002b Class A', this item is
//     stuck forever because no goroutine owns it to call
//     evaluateGroupAdvanceWithOutcome.
//   - One failed sibling (cross_queue_duplicate, QM-034): QM-034 requires all
//     items to be terminal before the group advances.  The dispatched sibling
//     blocks this invariant.
//
// The combination meant the group could NEVER reach complete-with-failures,
// so queue.json remained active across daemon restarts and QM-027 refused all
// new submits.
//
// After the Class A' fix (f82c051e, landed in reconcileThreeWay):
//
//  1. daemon.Start → LoadQueueAtStartup → reconcileThreeWay detects bead A
//     is dispatched + closed → advances item to completed, persists queue.
//     Emits reconciliation_mismatch_observed{mismatch_class=bead_closed_queue_dispatched}.
//
//  2. Both items are now terminal (completed + failed); the group is
//     all-terminal.  The work loop does NOT auto-advance the group to
//     complete-with-failures (evaluateGroupAdvanceWithOutcome only fires on
//     run completion, not on startup).
//
//  3. On daemon context cancel, drainCancelledQueue transitions the
//     still-active queue to cancelled and renames main.json →
//     main.json.cancelled-<ts>.
//
//  4. The renamed file means queue.Load returns nil on the next startup.
//     QM-027 sees ActiveQueue == nil → accepts the new submit.
//
// Run: go test -race -tags=scenario ./internal/daemon/... -run TestScenario_RestartRecovery_QM002bDeadlock
//
// Refs: hk-z0pmi, hk-5pg37, QM-002b Class A', QM-034, QM-027.
// Bead: hk-ivzsl.
func TestScenario_RestartRecovery_QM002bDeadlock(t *testing.T) {
	// Not parallel: uses os.Setenv(HARMONIK_CLAUDE_CONFIG_PATH) to isolate
	// EnsureWorktreeTrust — same rationale as TestScenario_HappyPath_N1.

	// Locate br binary; skip when absent.
	realBrPath := rrRecovBrPath(t)

	// Create project directory with required subdirs.
	projectDir, jsonlPath := rrRecovProjectDir(t)

	// Initialise br DB and seed two beads:
	//   bead A = dispatched in queue + closed in br  (Class A' target)
	//   bead B = failed sibling (cross_queue_duplicate)
	dbPath := filepath.Join(projectDir, ".beads", "beads.db")
	brWrapper := rrRecovBrWrapperScript(t, realBrPath, dbPath)
	beadAID, beadBID := rrRecovInitBrWithBeads(t, realBrPath, projectDir, brWrapper)
	t.Logf("rrRecov: beadA=%s (dispatched+closed), beadB=%s (failed sibling)", beadAID, beadBID)

	// Verify initial br state.
	scenariotest.AssertBeadStatus(t, brWrapper, beadAID, "closed")
	scenariotest.AssertBeadStatus(t, brWrapper, beadBID, "open")

	// Write the stuck queue.json simulating the state left by a crashed daemon:
	//   group 0, active, wave
	//   item 0: beadA dispatched (stuck claim goroutine from prior crash)
	//   item 1: beadB failed (cross_queue_duplicate — QM-034 failed sibling)
	rrRecovWriteStuckQueueJSON(t, projectDir, beadAID, beadBID)

	// Redirect EnsureWorktreeTrust to a test-local config path so the daemon
	// does not try to read ~/.claude.json and fail in CI.
	claudeConfigPath := filepath.Join(rrRecovEvalSymlinks(t, t.TempDir()), ".claude.json")
	if err := os.Setenv("HARMONIK_CLAUDE_CONFIG_PATH", claudeConfigPath); err != nil {
		t.Fatalf("rrRecov: Setenv HARMONIK_CLAUDE_CONFIG_PATH: %v", err)
	}
	t.Cleanup(func() { _ = os.Unsetenv("HARMONIK_CLAUDE_CONFIG_PATH") })

	// Wire daemon.Config.  No HandlerBinary: we cancel after reconciliation
	// fires, well before any dispatch attempt.
	loopCtx, loopCancel := context.WithCancel(context.Background())
	defer loopCancel()

	cfg := daemon.Config{
		ProjectDir:            projectDir,
		JSONLLogPath:          jsonlPath,
		BrPath:                brWrapper,
		NoAutoPull:            true, // queue-only mode; no br-ready fallback
		SkipWALCheckpoint:     true,
		SkipBrHistoryRotation: true,
		SkipRestartBackoff:    true,
		LogWriter:             testLogWriter{t: t},
		WorkflowModeDefault:   core.WorkflowModeReviewLoop,
	}

	// Launch daemon.Start in a goroutine.
	startDone := make(chan error, 1)
	go func() {
		startDone <- daemon.Start(loopCtx, cfg)
	}()

	// ── Phase 1: wait for Class A' to fire ────────────────────────────────────
	//
	// LoadQueueAtStartup runs synchronously in daemon.Start BEFORE the work
	// loop goroutine starts.  Within that call, reconcileThreeWay detects
	// beadA (dispatched + closed) and emits reconciliation_mismatch_observed
	// with mismatch_class=bead_closed_queue_dispatched.
	//
	// Budget: 15 s — the reconciliation itself is sub-second; extra headroom
	// for CI and slow br calls.
	const reconcileBudget = 15 * time.Second
	scenariotest.MustCompleteWithin(t, jsonlPath, "", nil, reconcileBudget, func() {
		for {
			if scenariotest.WaitForEvent(t, jsonlPath, "reconciliation_mismatch_observed", "", 50*time.Millisecond) {
				return
			}
		}
	})

	// ── Phase 2: assert Class A' payload ─────────────────────────────────────
	//
	// The reconciliation_mismatch_observed event must carry the correct
	// bead_id and mismatch_class for the Class A' advance.
	mismatch := rrRecovExtractMismatchPayload(t, jsonlPath, "bead_closed_queue_dispatched")
	if mismatch.BeadID != beadAID {
		t.Errorf("rrRecov: Class A' mismatch.BeadID = %q, want %q", mismatch.BeadID, beadAID)
	}
	if mismatch.QueueStatus != "dispatched" {
		t.Errorf("rrRecov: Class A' mismatch.QueueStatus = %q, want \"dispatched\"", mismatch.QueueStatus)
	}
	if mismatch.LedgerStatus == "" {
		t.Error("rrRecov: Class A' mismatch.LedgerStatus is empty")
	}
	if mismatch.ObservedAt == "" {
		t.Error("rrRecov: Class A' mismatch.ObservedAt is empty")
	}

	// ── Phase 3: assert on-disk queue correction ──────────────────────────────
	//
	// Class A' persists the corrected queue before emitting the event
	// (QM-063 persist-before-emit).  Read the queue file from disk and assert:
	//   item 0 (beadA): advanced from dispatched → completed.
	//   item 1 (beadB): unchanged at failed (QM-034 sibling integrity preserved).
	itemStatuses := rrRecovReadGroupItemStatuses(t, projectDir)
	if len(itemStatuses) != 2 {
		t.Fatalf("rrRecov: expected 2 items in group 0, got %d", len(itemStatuses))
	}
	if itemStatuses[0] != "completed" {
		t.Errorf("rrRecov: item A status = %q, want \"completed\" (Class A' advance)", itemStatuses[0])
	}
	if itemStatuses[1] != "failed" {
		t.Errorf("rrRecov: item B status = %q, want \"failed\" (sibling unchanged)", itemStatuses[1])
	}

	// ── Phase 4: cancel daemon and wait for clean exit ────────────────────────
	//
	// Cancelling loopCtx causes runWorkLoop to call exitClean → drainCancelledQueue.
	// drainCancelledQueue sees the queue is still active (the work loop did not
	// advance the group — no run goroutine fired evaluateGroupAdvanceWithOutcome)
	// and renames main.json → main.json.cancelled-<ts>.
	loopCancel()

	scenariotest.MustCompleteWithin(t, jsonlPath, "", nil, 10*time.Second, func() {
		if err := <-startDone; err != nil {
			t.Errorf("rrRecov: daemon.Start returned error after context cancel: %v", err)
		}
	})

	// ── Phase 5: assert queue file absent (QM-027 wedge is gone) ─────────────
	//
	// After drainCancelledQueue, .harmonik/queues/main.json must not exist.
	// queue.Load (which reads that exact path) must return nil, meaning a
	// subsequent queue.Validate would see ActiveQueue==nil and QM-027 would
	// NOT fire.
	queueMainPath := filepath.Join(projectDir, ".harmonik", "queues", "main.json")
	if _, statErr := os.Stat(queueMainPath); !os.IsNotExist(statErr) {
		t.Errorf("rrRecov: .harmonik/queues/main.json must be absent after daemon exit (drainCancelledQueue renames it); statErr=%v", statErr)
	}

	// Confirm via queue.Load — the canonical path used by daemon.Start on the
	// next startup.  A nil result means QM-027 would not block a new submit.
	loadedQ, loadErr := queue.Load(context.Background(), projectDir, queue.QueueNameMain)
	if loadErr != nil {
		t.Errorf("rrRecov: queue.Load after daemon exit: %v", loadErr)
	}
	if loadedQ != nil {
		t.Errorf("rrRecov: queue.Load after daemon exit = non-nil (queueID=%s); want nil (no active queue)", loadedQ.QueueID)
	}

	// ── Phase 6: prove subsequent submit is accepted ──────────────────────────
	//
	// Write a fresh queue to disk and load it.  The old *.cancelled-<ts> file
	// does NOT conflict because queue.EnumerateQueueNames only collects *.json
	// files without a dot-suffix, so the cancelled archive is invisible to
	// the next startup.  A new main.json written here would be picked up by
	// daemon.Start without hitting QM-027.
	freshQueue := &queue.Queue{
		SchemaVersion: 1,
		QueueID:       "00000000-0000-7000-8000-bbbb000000002",
		SubmittedAt:   time.Now().UTC(),
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindWave,
				Status:     queue.GroupStatusPending,
				CreatedAt:  time.Now().UTC(),
				Items: []queue.Item{
					{BeadID: "rrr-fresh-bead-01", Status: queue.ItemStatusPending},
				},
			},
		},
	}
	if err := queue.Persist(context.Background(), projectDir, freshQueue); err != nil {
		t.Fatalf("rrRecov: queue.Persist fresh queue: %v (proves QM-027 not wedged)", err)
	}
	loadedFresh, freshLoadErr := queue.Load(context.Background(), projectDir, queue.QueueNameMain)
	if freshLoadErr != nil {
		t.Errorf("rrRecov: queue.Load fresh queue: %v", freshLoadErr)
	}
	if loadedFresh == nil {
		t.Error("rrRecov: queue.Load fresh queue = nil; want non-nil")
	} else if loadedFresh.QueueID != freshQueue.QueueID {
		t.Errorf("rrRecov: loaded fresh queue ID = %q, want %q", loadedFresh.QueueID, freshQueue.QueueID)
	}

	// ── Causality invariants (hk-xegej) ──────────────────────────────────────
	//
	// No run goroutines fired in this test (no HandlerBinary → no dispatch),
	// so run_started is absent.  Both invariants pass vacuously.
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

	t.Logf("rrRecov: PASS beadA=%s Class-A'-advanced=completed beadB=%s sibling-unchanged=failed queue-unlinked=true subsequent-submit-accepted=true",
		beadAID, beadBID)
}

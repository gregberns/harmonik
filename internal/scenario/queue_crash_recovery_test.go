package scenario

// queue_crash_recovery_test.go — scenario contract tests for queue.json
// crash-recovery per specs/queue-model.md §3.2 QM-002 and §3.2a QM-002a.
//
// This file tests the persistence foundation of crash recovery: that a queue
// written atomically before a simulated daemon death (SIGKILL) is loaded intact
// by the next daemon startup, with the correct status and group_index values.
//
// QM-002 (§3.2): daemon reads .harmonik/queue.json at PL-005 step 8a; v0.1
// supports schema_version == 1; forward-incompatible refuses with exit code 2;
// corrupt is treated as absent (warn + proceed).
//
// QM-002a (§3.2a): after loading, daemon cross-checks dispatched items against
// the Beads ledger; if Beads shows open, the item reverts to pending via
// QM-001 atomic write and emits queue_item_reconciled{reason: claim_write_lost}.
// The cross-check completes before the first dispatch-loop tick.
// NOTE: QM-002a cross-check end-to-end coverage (revert + event ordering + payload
// + persistence round-trip + no-revert-when-not-open) lives in
// internal/lifecycle/startup_pl005_qm002_test.go via LoadQueueAtStartup
// (landed at 9b15f62 for hk-fwpc0 / T31). This file covers the QM-002 persistence
// foundation and the QM-002a terminal-state invariant only.
//
// Helper prefix: queueCrashRecovery (bead hk-30wgn, implementer-protocol §Helper-prefix).
//
// Spec refs:
//   - specs/queue-model.md §3.2 QM-002, §3.2a QM-002a
//   - specs/process-lifecycle.md §4.2 PL-005 step 8a
//
// Bead ref: hk-30wgn (T82).

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/queue"
)

// ---------------------------------------------------------------------------
// queueCrashRecovery fixture helpers
// ---------------------------------------------------------------------------

// queueCrashRecoveryProjectDir creates a temporary directory that acts as the
// projectDir for a single crash-recovery scenario. Pre-creates .harmonik/ so
// that queue.Persist / queue.Load can write and read queue.json.
// All created paths are registered for t.Cleanup removal.
func queueCrashRecoveryProjectDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	harmonikDir := filepath.Join(dir, ".harmonik")
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(harmonikDir, 0o755); err != nil {
		t.Fatalf("queueCrashRecoveryProjectDir: MkdirAll .harmonik: %v", err)
	}
	return dir
}

// queueCrashRecoveryTimestamp returns a deterministic UTC timestamp for
// crash-recovery fixture queues (millisecond precision, no monotonic component).
func queueCrashRecoveryTimestamp() time.Time {
	return time.Date(2026, 5, 14, 22, 0, 0, 0, time.UTC)
}

// queueCrashRecoveryTimestampPtr returns a pointer to queueCrashRecoveryTimestamp.
func queueCrashRecoveryTimestampPtr() *time.Time {
	ts := queueCrashRecoveryTimestamp()
	return &ts
}

// queueCrashRecoveryActiveQueue builds a Queue in the active / in-progress
// state: group 0 is active, one item is dispatched (simulating a crash while
// dispatch was in-flight), and the queue itself is active.
//
// This is the fixture state written to disk before the simulated SIGKILL.
// On restart the daemon must load this exact envelope per QM-002.
func queueCrashRecoveryActiveQueue() queue.Queue {
	runID := "0190b3c4-9001-7000-8000-000000000001"
	return queue.Queue{
		SchemaVersion: 1,
		QueueID:       "0190b3c4-8f12-7c4e-9a82-2bf0d4ee0099",
		SubmittedAt:   queueCrashRecoveryTimestamp(),
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindWave,
				Status:     queue.GroupStatusActive,
				Items: []queue.Item{
					{
						BeadID: core.BeadID("hk-test01"),
						Status: queue.ItemStatusDispatched,
						RunID:  &runID,
					},
					{
						BeadID: core.BeadID("hk-test02"),
						Status: queue.ItemStatusPending,
					},
				},
				CreatedAt: queueCrashRecoveryTimestamp(),
				StartedAt: queueCrashRecoveryTimestampPtr(),
			},
			{
				GroupIndex: 1,
				Kind:       queue.GroupKindWave,
				Status:     queue.GroupStatusPending,
				Items: []queue.Item{
					{
						BeadID: core.BeadID("hk-test03"),
						Status: queue.ItemStatusPending,
					},
				},
				CreatedAt: queueCrashRecoveryTimestamp(),
			},
		},
	}
}

// queueCrashRecoveryQueuePath returns the expected path of the per-queue file
// for the "main" queue under projectDir. After NQ-A2 (hk-tigaf.3) queues live
// at .harmonik/queues/<name>.json, not at the legacy .harmonik/queue.json.
func queueCrashRecoveryQueuePath(projectDir string) string {
	return filepath.Join(projectDir, ".harmonik", "queues", "main.json")
}

// queueCrashRecoveryEnsureQueuesDir ensures .harmonik/queues/ exists under
// projectDir. Required for tests that write queue files directly (without
// going through queue.Persist, which creates the directory automatically).
func queueCrashRecoveryEnsureQueuesDir(t *testing.T, projectDir string) {
	t.Helper()
	dir := filepath.Join(projectDir, ".harmonik", "queues")
	//nolint:gosec // G301: 0755 matches .harmonik dir conventions
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("queueCrashRecoveryEnsureQueuesDir: MkdirAll: %v", err)
	}
}

// ---------------------------------------------------------------------------
// QM-002 — queue.json loaded on restart with active status + correct group_index
// ---------------------------------------------------------------------------

// TestQueueCrashRecovery_QM002_LoadsActiveQueueAfterRestart verifies that after
// a simulated daemon crash (SIGKILL), a subsequent daemon restart loads
// queue.json per QM-002 with the persisted active status and the correct
// group_index values intact.
//
// Scenario:
//  1. Persist a Queue with status=active, group[0].status=active,
//     group[1].status=pending.
//  2. Simulate SIGKILL: no graceful shutdown, file left as-is.
//  3. Restart: call queue.Load (the QM-002 read path at PL-005 step 8a).
//  4. Assert: queue is non-nil; status==active; group[0].group_index==0,
//     group[0].status==active; group[1].group_index==1, group[1].status==pending.
//
// Spec refs: queue-model.md §3.2 QM-002; process-lifecycle.md §4.2 PL-005 step 8a.
func TestQueueCrashRecovery_QM002_LoadsActiveQueueAfterRestart(t *testing.T) {
	t.Parallel()

	projectDir := queueCrashRecoveryProjectDir(t)
	ctx := context.Background()

	// Step 1: write queue.json before simulated crash (QM-001 atomic write).
	original := queueCrashRecoveryActiveQueue()
	if err := queue.Persist(ctx, projectDir, &original); err != nil {
		t.Fatalf("Persist (pre-crash): %v", err)
	}

	// Verify queue.json exists on disk before proceeding.
	qPath := queueCrashRecoveryQueuePath(projectDir)
	if _, statErr := os.Stat(qPath); os.IsNotExist(statErr) {
		t.Fatal("queue.json must exist after Persist (pre-crash)")
	}

	// Step 2: simulate SIGKILL — no cleanup, no graceful shutdown.
	// The file is left exactly as written. Nothing to do in the test.

	// Step 3: daemon restart — read queue.json per QM-002 (PL-005 step 8a).
	loaded, err := queue.Load(ctx, projectDir, queue.QueueNameMain)
	if err != nil {
		t.Fatalf("Load (post-restart): %v", err)
	}
	if loaded == nil {
		t.Fatal("Load (post-restart): got nil Queue; expected active queue to survive crash")
	}

	// Step 4: assert persisted state is fully recovered.

	// QM-002: queue status must be active (not reset, not cleared).
	if loaded.Status != queue.QueueStatusActive {
		t.Errorf("post-restart queue.Status = %q, want %q",
			loaded.Status, queue.QueueStatusActive)
	}

	// QueueID must be preserved across restart (QM-005 identity discipline).
	if loaded.QueueID != original.QueueID {
		t.Errorf("post-restart queue.QueueID = %q, want %q",
			loaded.QueueID, original.QueueID)
	}

	// Group count must be unchanged.
	if len(loaded.Groups) != len(original.Groups) {
		t.Fatalf("post-restart len(Groups) = %d, want %d",
			len(loaded.Groups), len(original.Groups))
	}

	// group_index 0: status must be active (was active at crash time).
	g0 := loaded.Groups[0]
	if g0.GroupIndex != 0 {
		t.Errorf("Groups[0].GroupIndex = %d, want 0", g0.GroupIndex)
	}
	if g0.Status != queue.GroupStatusActive {
		t.Errorf("Groups[0].Status = %q, want %q",
			g0.Status, queue.GroupStatusActive)
	}

	// group_index 1: status must be pending (was pending at crash time).
	g1 := loaded.Groups[1]
	if g1.GroupIndex != 1 {
		t.Errorf("Groups[1].GroupIndex = %d, want 1", g1.GroupIndex)
	}
	if g1.Status != queue.GroupStatusPending {
		t.Errorf("Groups[1].Status = %q, want %q",
			g1.Status, queue.GroupStatusPending)
	}

	// Item statuses in group 0 must be unchanged: first item dispatched,
	// second item pending (QM-002 loads the on-disk envelope verbatim;
	// QM-002a cross-check is a separate post-load step).
	if len(g0.Items) != 2 {
		t.Fatalf("Groups[0] len(Items) = %d, want 2", len(g0.Items))
	}
	if g0.Items[0].Status != queue.ItemStatusDispatched {
		t.Errorf("Groups[0].Items[0].Status = %q, want %q",
			g0.Items[0].Status, queue.ItemStatusDispatched)
	}
	if g0.Items[1].Status != queue.ItemStatusPending {
		t.Errorf("Groups[0].Items[1].Status = %q, want %q",
			g0.Items[1].Status, queue.ItemStatusPending)
	}
}

// TestQueueCrashRecovery_QM002_FileAbsentMeansNoQueue verifies that when
// queue.json does not exist at restart time, Load returns (nil, nil) per
// QM-002 "File absent" case — the daemon starts with no active queue.
//
// Spec ref: queue-model.md §3.2 QM-002.
func TestQueueCrashRecovery_QM002_FileAbsentMeansNoQueue(t *testing.T) {
	t.Parallel()

	projectDir := queueCrashRecoveryProjectDir(t)
	ctx := context.Background()

	// No prior Persist call — simulate a clean restart with no persisted queue.
	loaded, err := queue.Load(ctx, projectDir, queue.QueueNameMain)
	if err != nil {
		t.Fatalf("Load on absent queue.json: unexpected error: %v", err)
	}
	if loaded != nil {
		t.Errorf("Load on absent queue.json: got non-nil Queue %+v; want nil", loaded)
	}
}

// TestQueueCrashRecovery_QM002_CorruptFileWarnAndProceed verifies that when
// queue.json is present but corrupt, Load returns (nil, ErrCorrupt) per
// QM-002 "File present but unparseable" case. The daemon proceeds with no
// active queue; the file is NOT auto-deleted (operator must inspect).
//
// Spec ref: queue-model.md §3.2 QM-002.
func TestQueueCrashRecovery_QM002_CorruptFileWarnAndProceed(t *testing.T) {
	t.Parallel()

	projectDir := queueCrashRecoveryProjectDir(t)
	ctx := context.Background()

	// Write a corrupt queue file — as if a mid-write crash left a partial file
	// that survived as the canonical path (impossible under WM-026 atomic-write
	// but defensively handled per QM-002 for file-system corruption scenarios).
	queueCrashRecoveryEnsureQueuesDir(t, projectDir)
	qPath := queueCrashRecoveryQueuePath(projectDir)
	//nolint:gosec // G306: 0600 is appropriate for .harmonik test fixture
	if err := os.WriteFile(qPath, []byte(`{"schema_version":1,"corrupted":`), 0o600); err != nil {
		t.Fatalf("setup: write corrupt queue file: %v", err)
	}

	loaded, err := queue.Load(ctx, projectDir, queue.QueueNameMain)
	if loaded != nil {
		t.Errorf("Load on corrupt file: got non-nil Queue %+v; want nil", loaded)
	}
	if err == nil {
		t.Fatal("Load on corrupt file: expected ErrCorrupt, got nil error")
	}
	if !errors.Is(err, queue.ErrCorrupt) {
		t.Errorf("Load on corrupt file: error %v does not wrap ErrCorrupt", err)
	}

	// QM-002 mandates the file is NOT auto-deleted: operator must inspect.
	if _, statErr := os.Stat(qPath); os.IsNotExist(statErr) {
		t.Error("Load on corrupt file: queue.json was auto-deleted; must be left for operator inspection (QM-002)")
	}
}

// TestQueueCrashRecovery_QM002_ForwardIncompatibleSchemaVersion verifies that
// a queue.json with schema_version != 1 is rejected as forward-incompatible
// per QM-002. The daemon must refuse startup with exit code 2 in the
// production path; at the persistence layer this surfaces as ErrSchemaVersion
// wrapped by ErrCorrupt (the "file present but unparseable" branch).
//
// Spec ref: queue-model.md §3.2 QM-002.
func TestQueueCrashRecovery_QM002_ForwardIncompatibleSchemaVersion(t *testing.T) {
	t.Parallel()

	projectDir := queueCrashRecoveryProjectDir(t)
	ctx := context.Background()

	// Write queue file with schema_version=99 (future, unsupported).
	queueCrashRecoveryEnsureQueuesDir(t, projectDir)
	qPath := queueCrashRecoveryQueuePath(projectDir)
	const futureSchema = `{"schema_version":99,"queue_id":"0190b3c4-8f12-7c4e-9a82-000000000099",` +
		`"submitted_at":"2026-05-14T22:00:00Z","status":"active","groups":[]}`
	//nolint:gosec // G306: 0600 is appropriate for .harmonik test fixture
	if err := os.WriteFile(qPath, []byte(futureSchema), 0o600); err != nil {
		t.Fatalf("setup: write future-schema queue file: %v", err)
	}

	loaded, err := queue.Load(ctx, projectDir, queue.QueueNameMain)
	if loaded != nil {
		t.Errorf("Load on future schema: got non-nil Queue; want nil (forward-incompatible refusal)")
	}
	if err == nil {
		t.Fatal("Load on future schema: expected error, got nil")
	}
	// The forward-incompatible case is parsed as corrupt (schema_version check
	// is part of UnmarshalQueue), so ErrCorrupt should wrap the schema error.
	if !errors.Is(err, queue.ErrCorrupt) {
		t.Errorf("Load on future schema: error %v does not wrap ErrCorrupt; want ErrCorrupt(ErrSchemaVersion)", err)
	}
}

// ---------------------------------------------------------------------------
// QM-002a — dispatched items reverted to pending when Beads cross-check shows open
// ---------------------------------------------------------------------------

// Note: TestQueueCrashRecovery_QM002a_DispatchedItemRevertedToPendingWhenBeadsOpen
// previously lived here as a skipped placeholder. Full QM-002a end-to-end coverage
// (revert + run_id clear + event emission + ordering + persistence round-trip)
// now lives in internal/lifecycle/startup_pl005_qm002_test.go (landed at 9b15f62
// for hk-fwpc0 / T31). Removed by hk-zixbp.

// TestQueueCrashRecovery_QM002a_NoRevertForCompletedItem verifies that
// the QM-002a cross-check does NOT revert items that are already in terminal
// states (completed, failed). Only dispatched items are subject to the
// cross-check; terminal items are immutable per QM-032.
//
// This test exercises the persistence layer only — the cross-check API itself
// is gated on hk-fwpc0.
//
// Spec ref: queue-model.md §3.2a QM-002a, §5 QM-032 (terminal states absorbing).
func TestQueueCrashRecovery_QM002a_NoRevertForCompletedItem(t *testing.T) {
	t.Parallel()

	projectDir := queueCrashRecoveryProjectDir(t)
	ctx := context.Background()

	runID := "0190b3c4-9001-7000-8000-000000000002"
	q := queue.Queue{
		SchemaVersion: 1,
		QueueID:       "0190b3c4-8f12-7c4e-9a82-2bf0d4ee0100",
		SubmittedAt:   queueCrashRecoveryTimestamp(),
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindWave,
				Status:     queue.GroupStatusActive,
				Items: []queue.Item{
					{
						BeadID: core.BeadID("hk-done01"),
						Status: queue.ItemStatusCompleted,
						RunID:  &runID,
					},
					{
						BeadID: core.BeadID("hk-pend01"),
						Status: queue.ItemStatusPending,
					},
				},
				CreatedAt: queueCrashRecoveryTimestamp(),
				StartedAt: queueCrashRecoveryTimestampPtr(),
			},
		},
	}

	if err := queue.Persist(ctx, projectDir, &q); err != nil {
		t.Fatalf("Persist: %v", err)
	}

	loaded, err := queue.Load(ctx, projectDir, queue.QueueNameMain)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded == nil {
		t.Fatal("Load: got nil; want queue")
	}

	// Terminal item (completed) must load with its status intact.
	// QM-002a cross-check only applies to dispatched items; completed items
	// are absorbing per QM-032 and must not be reverted.
	if loaded.Groups[0].Items[0].Status != queue.ItemStatusCompleted {
		t.Errorf("Items[0].Status = %q, want %q (terminal completed must not be reverted by QM-002a)",
			loaded.Groups[0].Items[0].Status, queue.ItemStatusCompleted)
	}

	// Pending item must also load unchanged.
	if loaded.Groups[0].Items[1].Status != queue.ItemStatusPending {
		t.Errorf("Items[1].Status = %q, want %q",
			loaded.Groups[0].Items[1].Status, queue.ItemStatusPending)
	}
}

// TestQueueCrashRecovery_DispatchResumedFromCorrectGroupIndex verifies that
// after a crash, the loaded queue allows dispatch to resume from the active
// group (group_index=0 in the fixture) without skipping or re-running groups.
//
// This tests the "dispatch continues without losing items" acceptance criterion
// from the bead body at the persistence layer: EligibleItems on the loaded
// active group returns the expected pending item (hk-test02; hk-test01 was
// dispatched and is eligible for the QM-002a revert in the full cross-check).
//
// Spec ref: queue-model.md §3.2 QM-002; §5.2 EligibleItems (QM-035, QM-036).
func TestQueueCrashRecovery_DispatchResumedFromCorrectGroupIndex(t *testing.T) {
	t.Parallel()

	projectDir := queueCrashRecoveryProjectDir(t)
	ctx := context.Background()

	original := queueCrashRecoveryActiveQueue()
	if err := queue.Persist(ctx, projectDir, &original); err != nil {
		t.Fatalf("Persist (pre-crash): %v", err)
	}

	loaded, err := queue.Load(ctx, projectDir, queue.QueueNameMain)
	if err != nil {
		t.Fatalf("Load (post-restart): %v", err)
	}
	if loaded == nil {
		t.Fatal("Load: got nil; want active queue")
	}

	// Identify the active group: must be group_index 0.
	var activeGroup *queue.Group
	for i := range loaded.Groups {
		if loaded.Groups[i].Status == queue.GroupStatusActive {
			g := &loaded.Groups[i]
			activeGroup = g
			break
		}
	}
	if activeGroup == nil {
		t.Fatal("no active group found after restart; want Groups[0].Status==active")
	}
	if activeGroup.GroupIndex != 0 {
		t.Errorf("active group has group_index=%d, want 0 (dispatch must resume from group 0)",
			activeGroup.GroupIndex)
	}

	// EligibleItems on the active wave group: hk-test01 is dispatched (in-flight,
	// NOT eligible for re-dispatch until QM-002a reverts it), hk-test02 is pending
	// (eligible). EligibleItems must return exactly hk-test02.
	eligible := queue.EligibleItems(activeGroup)
	if len(eligible) != 1 {
		t.Errorf("EligibleItems after restart: got %d items, want 1 (hk-test02 pending)",
			len(eligible))
	} else if eligible[0].BeadID != core.BeadID("hk-test02") {
		t.Errorf("EligibleItems after restart: got %q, want %q",
			eligible[0].BeadID, core.BeadID("hk-test02"))
	}

	// Group 1 must remain pending — dispatch must not skip to a non-active group.
	if loaded.Groups[1].Status != queue.GroupStatusPending {
		t.Errorf("Groups[1].Status = %q, want %q (must not advance past active group on restart)",
			loaded.Groups[1].Status, queue.GroupStatusPending)
	}
}

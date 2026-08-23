package scenario

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

func queueCrashRecoveryTimestamp() time.Time {
	return time.Date(2026, 5, 14, 22, 0, 0, 0, time.UTC)
}

func queueCrashRecoveryTimestampPtr() *time.Time {
	ts := queueCrashRecoveryTimestamp()
	return &ts
}

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

func queueCrashRecoveryQueuePath(projectDir string) string {
	return filepath.Join(projectDir, ".harmonik", "queues", "main.json")
}

func queueCrashRecoveryEnsureQueuesDir(t *testing.T, projectDir string) {
	t.Helper()
	dir := filepath.Join(projectDir, ".harmonik", "queues")
	//nolint:gosec // G301: 0755 matches .harmonik dir conventions
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("queueCrashRecoveryEnsureQueuesDir: MkdirAll: %v", err)
	}
}

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

	original := queueCrashRecoveryActiveQueue()
	if err := queue.Persist(ctx, projectDir, &original); err != nil {
		t.Fatalf("Persist (pre-crash): %v", err)
	}

	qPath := queueCrashRecoveryQueuePath(projectDir)
	if _, statErr := os.Stat(qPath); os.IsNotExist(statErr) {
		t.Fatal("queue.json must exist after Persist (pre-crash)")
	}

	loaded, err := queue.Load(ctx, projectDir, queue.QueueNameMain)
	if err != nil {
		t.Fatalf("Load (post-restart): %v", err)
	}
	if loaded == nil {
		t.Fatal("Load (post-restart): got nil Queue; expected active queue to survive crash")
	}

	if loaded.Status != queue.QueueStatusActive {
		t.Errorf("post-restart queue.Status = %q, want %q",
			loaded.Status, queue.QueueStatusActive)
	}

	if loaded.QueueID != original.QueueID {
		t.Errorf("post-restart queue.QueueID = %q, want %q",
			loaded.QueueID, original.QueueID)
	}

	if len(loaded.Groups) != len(original.Groups) {
		t.Fatalf("post-restart len(Groups) = %d, want %d",
			len(loaded.Groups), len(original.Groups))
	}

	g0 := loaded.Groups[0]
	if g0.GroupIndex != 0 {
		t.Errorf("Groups[0].GroupIndex = %d, want 0", g0.GroupIndex)
	}
	if g0.Status != queue.GroupStatusActive {
		t.Errorf("Groups[0].Status = %q, want %q",
			g0.Status, queue.GroupStatusActive)
	}

	g1 := loaded.Groups[1]
	if g1.GroupIndex != 1 {
		t.Errorf("Groups[1].GroupIndex = %d, want 1", g1.GroupIndex)
	}
	if g1.Status != queue.GroupStatusPending {
		t.Errorf("Groups[1].Status = %q, want %q",
			g1.Status, queue.GroupStatusPending)
	}

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

	queueCrashRecoveryEnsureQueuesDir(t, projectDir)
	qPath := queueCrashRecoveryQueuePath(projectDir)
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

	queueCrashRecoveryEnsureQueuesDir(t, projectDir)
	qPath := queueCrashRecoveryQueuePath(projectDir)
	const futureSchema = `{"schema_version":99,"queue_id":"0190b3c4-8f12-7c4e-9a82-000000000099",` +
		`"submitted_at":"2026-05-14T22:00:00Z","status":"active","groups":[]}`
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
	if !errors.Is(err, queue.ErrCorrupt) {
		t.Errorf("Load on future schema: error %v does not wrap ErrCorrupt; want ErrCorrupt(ErrSchemaVersion)", err)
	}
}

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

	if loaded.Groups[0].Items[0].Status != queue.ItemStatusCompleted {
		t.Errorf("Items[0].Status = %q, want %q (terminal completed must not be reverted by QM-002a)",
			loaded.Groups[0].Items[0].Status, queue.ItemStatusCompleted)
	}

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

	eligible := queue.EligibleItems(activeGroup)
	if len(eligible) != 1 {
		t.Errorf("EligibleItems after restart: got %d items, want 1 (hk-test02 pending)",
			len(eligible))
	} else if eligible[0].BeadID != core.BeadID("hk-test02") {
		t.Errorf("EligibleItems after restart: got %q, want %q",
			eligible[0].BeadID, core.BeadID("hk-test02"))
	}

	if loaded.Groups[1].Status != queue.GroupStatusPending {
		t.Errorf("Groups[1].Status = %q, want %q (must not advance past active group on restart)",
			loaded.Groups[1].Status, queue.GroupStatusPending)
	}
}

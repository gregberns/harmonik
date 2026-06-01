package daemon_test

// workloop_perqueue_completion_hktigaf4_test.go — NQ-B1 regression test for the
// run-COMPLETION path on NON-"main" named queues (hk-tigaf.4).
//
// NQ-B1 made non-"main" named queues dispatchable for the first time, but the
// completion path in evaluateGroupAdvanceWithOutcome was still hard-wired to the
// "main" queue (it resolved the queue via the main-only lq.Queue() shim, then
// tripped a QueueID guard and returned early). For a run dispatched from a
// non-"main" queue this meant the item was NEVER marked terminal and the group
// NEVER advanced — that queue stalled forever.
//
// The fix threads the dispatching queue's name (capturedQueueName) through to
// evaluateGroupAdvanceWithOutcome, which now resolves the queue BY NAME
// (LockedQueueByName) instead of the main-only shim.
//
// This test drives the completion path directly (mirroring
// TestQueueDispatch_EM015f_GroupAdvanceGate) against a queue installed under a
// NON-"main" slot. WITHOUT the fix the assertions fail (item left dispatched,
// group left active, queue never unlinked); WITH the fix they pass.
//
// Helper prefix: perQueueComplete (implementer-protocol.md §Helper-prefix).
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent
//
// Spec ref: specs/execution-model.md §4.3.EM-015f; specs/queue-model.md §2.1 QM-003.
// Bead ref: hk-tigaf.4, hk-45ude.

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/queue"
)

// perQueueCompleteWaveQueue builds a single-group wave queue under the given
// name with the given bead IDs as items. The group is active; all items start
// dispatched (simulating an in-flight run that is about to complete).
func perQueueCompleteWaveQueue(t *testing.T, name, queueID string, beadIDs ...core.BeadID) *queue.Queue {
	t.Helper()
	items := make([]queue.Item, len(beadIDs))
	for i, id := range beadIDs {
		items[i] = queue.Item{BeadID: id, Status: queue.ItemStatusDispatched}
	}
	now := time.Now()
	return &queue.Queue{
		SchemaVersion: 1,
		QueueID:       queueID,
		Name:          name,
		SubmittedAt:   now,
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindWave,
				Status:     queue.GroupStatusActive,
				Items:      items,
				CreatedAt:  now,
			},
		},
	}
}

// TestPerQueueComplete_NonMainQueue_MarksItemTerminalAndAdvances is the
// load-bearing NQ-B1 completion-path regression: a run dispatched from a
// NON-"main" named queue ("investigate"), on completion, must mark its item
// terminal AND advance/unlink its group — not leave the item dispatched forever.
//
// Pre-fix behaviour (evaluateGroupAdvanceWithOutcome resolving via the main-only
// lq.Queue() shim): the queueName="investigate" run resolves nil for the "main"
// slot (or the wrong queue), trips the QueueID guard, and returns early. The
// item stays dispatched, the group stays active, the queue is never unlinked —
// every assertion below fails.
//
// Post-fix behaviour: the completion path resolves the queue by name, marks the
// item completed, advances the single group to complete-success, and (all groups
// complete-success) CompleteAndUnlinks the queue → the "investigate" slot is
// cleared.
func TestPerQueueComplete_NonMainQueue_MarksItemTerminalAndAdvances(t *testing.T) {
	t.Parallel()

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	const (
		queueName = "investigate" // NON-"main" — the whole point of the test
		queueID   = "qid-investigate-completion"
		beadID    = core.BeadID("hk-tigaf4-completion-bead")
	)

	q := perQueueCompleteWaveQueue(t, queueName, queueID, beadID)

	qs := daemon.ExportedNewQueueStore()
	// Install ONLY under the non-"main" slot. The main slot stays empty, so a
	// completion path that resolves via lq.Queue() (main-only) gets nil and
	// returns early — exactly the bug under test.
	qs.SetQueueByName(queueName, q)

	bus := &stubEventCollector{}
	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:        &stubBeadLedger{},
		Bus:              bus,
		ProjectDir:       projectDir,
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		QueueStore:       qs,
	})

	// Sanity: the main slot is empty; the queue lives under "investigate".
	if qs.Queue() != nil {
		t.Fatal("precondition: main slot should be empty (queue installed under \"investigate\")")
	}
	if qs.QueueByName(queueName) == nil {
		t.Fatalf("precondition: queue should be installed under %q slot", queueName)
	}

	// Drive the completion path for the single item (success). queueName is the
	// non-"main" name the run was dispatched from — the fix must use it to
	// resolve the right queue.
	daemon.ExportedEvaluateGroupAdvanceWithOutcome(context.Background(), deps, queueName, queueID, 0 /*groupIndex*/, 0 /*itemIdx*/, true /*success*/)

	// All groups reached complete-success → the queue is CompleteAndUnlinked,
	// which clears the in-memory slot (ClearQueueByName). So the "investigate"
	// slot must now be empty. WITHOUT the fix the completion path returned early,
	// the item stayed dispatched, the group stayed active, and the queue was
	// NEVER unlinked — so the slot would still hold an active queue with a
	// non-terminal item.
	if got := qs.QueueByName(queueName); got != nil {
		// Not unlinked → inspect why: the item should at minimum be terminal.
		if len(got.Groups) > 0 && len(got.Groups[0].Items) > 0 {
			item := got.Groups[0].Items[0]
			if item.Status == queue.ItemStatusDispatched {
				t.Fatalf("NQ-B1 BUG: item still %q (never marked terminal) — completion path did not resolve the non-\"main\" queue %q",
					item.Status, queueName)
			}
		}
		if got.Groups[0].Status == queue.GroupStatusActive {
			t.Fatalf("NQ-B1 BUG: group still active (never advanced) on non-\"main\" queue %q", queueName)
		}
		t.Fatalf("NQ-B1 BUG: queue %q not unlinked after sole item completed; group status=%q",
			queueName, got.Groups[0].Status)
	}
}

// TestPerQueueComplete_NonMainQueue_TwoItemGroupAdvances is the multi-item
// companion: it proves the completion path on a non-"main" queue marks each item
// terminal and only advances the group once ALL items are terminal (EM-015f),
// without relying on CompleteAndUnlink masking an early-return bug.
//
// A two-item group is used so that after the FIRST item completes the group is
// still active and the queue is still loaded (not unlinked) — letting us assert
// directly that the first item was marked terminal on a non-"main" queue.
func TestPerQueueComplete_NonMainQueue_TwoItemGroupAdvances(t *testing.T) {
	t.Parallel()

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	const (
		queueName = "investigate"
		queueID   = "qid-investigate-twoitem"
		beadA     = core.BeadID("hk-tigaf4-twoitem-a")
		beadB     = core.BeadID("hk-tigaf4-twoitem-b")
	)

	q := perQueueCompleteWaveQueue(t, queueName, queueID, beadA, beadB)

	qs := daemon.ExportedNewQueueStore()
	qs.SetQueueByName(queueName, q)

	bus := &stubEventCollector{}
	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:        &stubBeadLedger{},
		Bus:              bus,
		ProjectDir:       projectDir,
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		QueueStore:       qs,
	})

	// Complete item 0 only. Group must stay active (item 1 still in flight) AND
	// item 0 must be marked terminal — on the NON-"main" queue.
	daemon.ExportedEvaluateGroupAdvanceWithOutcome(context.Background(), deps, queueName, queueID, 0, 0, true)

	q1 := qs.QueueByName(queueName)
	if q1 == nil {
		t.Fatal("queue unexpectedly unlinked after only 1 of 2 items completed")
	}
	if q1.Groups[0].Items[0].Status != queue.ItemStatusCompleted {
		t.Fatalf("NQ-B1 BUG: item 0 status = %q after completion on non-\"main\" queue; want completed (completion path did not resolve %q)",
			q1.Groups[0].Items[0].Status, queueName)
	}
	if q1.Groups[0].Status != queue.GroupStatusActive {
		t.Fatalf("group 0 status = %q after 1 of 2 items terminal; want active (EM-015f gate)",
			q1.Groups[0].Status)
	}

	// Complete item 1 — now all terminal; group → complete-success → queue
	// CompleteAndUnlinked → slot cleared.
	daemon.ExportedEvaluateGroupAdvanceWithOutcome(context.Background(), deps, queueName, queueID, 0, 1, true)

	if got := qs.QueueByName(queueName); got != nil {
		if got.Groups[0].Status == queue.GroupStatusActive {
			t.Fatalf("NQ-B1 BUG: group still active after both items terminal on non-\"main\" queue %q", queueName)
		}
		t.Fatalf("queue %q not unlinked after both items completed; group status=%q",
			queueName, got.Groups[0].Status)
	}
}

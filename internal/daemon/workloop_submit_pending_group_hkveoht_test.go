package daemon_test

// workloop_submit_pending_group_hkveoht_test.go — freshly-submitted queue with a
// PENDING first group must bootstrap to active and dispatch (hk-veoht).
//
// Regression: when a queue is submitted via `harmonik queue submit` (or loaded
// on boot), its first group is persisted GroupStatusPending. The only caller of
// AdvanceGroup, evaluateGroupAdvanceWithOutcome, fires only on a PRIOR run's
// completion — so a freshly-submitted queue's group 0 never transitions
// pending → active. The work loop's "no active group" branch then idle-waits
// forever and EligibleItems returns nil for the non-active group, so the items
// never dispatch.
//
// This test reproduces that: it submits a single kind=wave group whose status
// is PENDING (exactly as the submit/boot path persists it) into an idle work
// loop, and asserts the group advances to active and every item gets claimed
// (dispatched). Without the activateFirstPendingGroup fix the loop idle-waits
// and no claim is ever made — the test fails on the timeout / unclaimed asserts.
//
// Spec ref: specs/queue-model.md §5 QM-031 (pending → active); §8 QM-063.
// Bead ref: hk-veoht.

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/queue"
)

// TestWorkLoop_SubmittedPendingGroupBootstrapsToActive verifies that a queue
// whose first group is persisted PENDING (the submit/boot path) is advanced to
// active by the work loop and its items get dispatched — not stuck pending.
//
// The handler exits 0 with no commit, so each dispatched item is marked Failed
// by the no-commit guard. That is fine: the property under test is
// group-bootstrap + dispatch (claim), not handler success. The group reaches
// complete-with-failures, the queue becomes paused-by-failure, and
// CancelOnQueueExit fires so the loop exits cleanly.
//
// Bead ref: hk-veoht.
func TestWorkLoop_SubmittedPendingGroupBootstrapsToActive(t *testing.T) {
	t.Parallel()

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	const itemCount = 3
	beadIDs := make([]core.BeadID, itemCount)
	items := make([]queue.Item, itemCount)
	for i := range itemCount {
		id := core.BeadID("hk-veoht-" + string(rune('a'+i)))
		beadIDs[i] = id
		items[i] = queue.Item{BeadID: id, Status: queue.ItemStatusPending}
	}

	now := time.Now()
	// THE BUG CONDITION: group 0 is PENDING, exactly as `harmonik queue submit`
	// (and boot-from-queue.json) persists it. Queue status is active (queues are
	// minted active by HandleQueueSubmit). Pre-fix, nothing advances group 0.
	q := &queue.Queue{
		SchemaVersion: 1,
		QueueID:       "veoht-pending-group-test",
		SubmittedAt:   now,
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindWave,
				Status:     queue.GroupStatusPending, // ← not active
				Items:      items,
				CreatedAt:  now,
			},
		},
	}

	qs := daemon.ExportedNewQueueStore()
	qs.SetQueue(q)

	ledger := newStuckHeadLedger("hk-veoht-no-stuck-bead") // none of our beads is "stuck"
	bus := &stubEventCollector{}

	exitCtx, cancelExit := context.WithCancel(context.Background())

	p := daemon.WorkLoopDepsParams{
		BrAdapter:         ledger,
		Bus:               bus,
		ProjectDir:        projectDir,
		HandlerBinary:     "/bin/sh",
		HandlerArgs:       []string{"-c", "exit 0"},
		IntentLogDir:      filepath.Join(projectDir, ".harmonik", "beads-intents"),
		QueueStore:        qs,
		MaxConcurrent:     itemCount,
		AdapterRegistry2:  NewSealedAdapterRegistryForTest(t),
		CancelOnQueueExit: cancelExit,
	}
	deps := daemon.ExportedWorkLoopDeps(p)

	testCtx, testCancel := context.WithTimeout(exitCtx, 60*time.Second)
	defer testCancel()

	loopDone := make(chan error, 1)
	go func() {
		loopDone <- daemon.ExportedRunWorkLoop(testCtx, deps)
	}()

	select {
	case err := <-loopDone:
		if err != nil {
			t.Errorf("runWorkLoop returned non-nil error: %v", err)
		}
	case <-time.After(55 * time.Second):
		// Pre-fix failure mode: the loop idle-waits forever because group 0 is
		// never advanced to active, so it never exits.
		t.Fatal("runWorkLoop did not exit — submitted PENDING group never advanced to active; items stuck pending (hk-veoht regression)")
	}

	// ── Assert every item was claimed (i.e. dispatched, not stuck pending) ───
	claimed := ledger.claimedSet()
	for _, id := range beadIDs {
		if _, ok := claimed[id]; !ok {
			t.Errorf("bead %s was never claimed — group 0 did not bootstrap to active (hk-veoht regression)", id)
		}
	}

	// ── Assert the group reached a terminal, NON-pending status ──────────────
	finalQ := daemon.ExportedQueueStoreOf(deps).Queue()
	if finalQ == nil {
		t.Fatal("queue is nil after work loop exit")
	}
	if len(finalQ.Groups) == 0 {
		t.Fatal("queue has no groups")
	}
	g := finalQ.Groups[0]
	if g.Status == queue.GroupStatusPending {
		t.Errorf("group 0 status = %q; expected it to have advanced past pending (hk-veoht regression)", g.Status)
	}

	// ── Assert a queue_group_started event was emitted for the bootstrap ─────
	sawGroupStarted := false
	for _, et := range bus.eventTypes() {
		if et == "queue_group_started" {
			sawGroupStarted = true
			break
		}
	}
	if !sawGroupStarted {
		t.Errorf("no queue_group_started event emitted; expected one when the pending group bootstrapped to active (hk-veoht)")
	}

	t.Logf("hk-veoht: claimed=%d/%d, group_status=%s, group_started_emitted=%v",
		len(claimed), itemCount, g.Status, sawGroupStarted)
}

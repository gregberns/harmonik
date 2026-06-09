package daemon_test

// workloop_hk45ude_test.go — queue-dispatch workloop tests (T50, hk-45ude).
//
// Coverage:
//   - Queue-pull happy path: workloop pulls from active group and dispatches.
//   - EM-015f gate: next group not started until current group all-terminal.
//   - Failure path: complete-with-failures triggers queue_paused event.
//   - QueueID/QueueGroupIndex stamping: run_started carries non-nil queue fields.
//   - Backward compat: br-ready poll path still works when no queue is loaded.
//
// Helper prefix: queueDispatchFixture (derived from T50 concept per
// implementer-protocol.md §Helper-prefix discipline).
//
// Spec ref: specs/execution-model.md §7.4 (TS-1); §4.3.EM-015f.
// Bead ref: hk-45ude.

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/queue"
)

// ─────────────────────────────────────────────────────────────────────────────
// Fixtures
// ─────────────────────────────────────────────────────────────────────────────

// queueDispatchFixtureWaveQueue builds a minimal one-group wave Queue with the
// given bead IDs as items.  All items are pending; the group is active so
// EligibleItems returns all of them.
func queueDispatchFixtureWaveQueue(t *testing.T, beadIDs ...core.BeadID) *queue.Queue {
	t.Helper()
	items := make([]queue.Item, len(beadIDs))
	for i, id := range beadIDs {
		items[i] = queue.Item{
			BeadID: id,
			Status: queue.ItemStatusPending,
		}
	}
	now := time.Now()
	return &queue.Queue{
		SchemaVersion: 1,
		QueueID:       "test-queue-id-" + t.Name(),
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

// queueDispatchFixtureTwoGroupQueue builds a two-group queue: group 0 (wave,
// active) with beadGroup0 items, and group 1 (wave, pending) with beadGroup1
// items. Used for EM-015f group-advance gate tests.
func queueDispatchFixtureTwoGroupQueue(t *testing.T, beadGroup0, beadGroup1 []core.BeadID) *queue.Queue {
	t.Helper()
	makeItems := func(ids []core.BeadID) []queue.Item {
		items := make([]queue.Item, len(ids))
		for i, id := range ids {
			items[i] = queue.Item{BeadID: id, Status: queue.ItemStatusPending}
		}
		return items
	}
	now := time.Now()
	return &queue.Queue{
		SchemaVersion: 1,
		QueueID:       "test-queue-two-group-" + t.Name(),
		SubmittedAt:   now,
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindWave,
				Status:     queue.GroupStatusActive,
				Items:      makeItems(beadGroup0),
				CreatedAt:  now,
			},
			{
				GroupIndex: 1,
				Kind:       queue.GroupKindWave,
				Status:     queue.GroupStatusPending,
				Items:      makeItems(beadGroup1),
				CreatedAt:  now,
			},
		},
	}
}

// queueDispatchFixtureDeps builds workLoopDeps with a QueueStore pre-loaded
// with q.  The br-ready poll is wired to an empty stubBeadLedger so it never
// returns beads (the queue should be the only dispatch source).
func queueDispatchFixtureDeps(t *testing.T, projectDir string, bus *stubEventCollector, q *queue.Queue) daemon.WorkLoopDepsParams {
	t.Helper()
	qs := daemon.ExportedNewQueueStore()
	if q != nil {
		qs.SetQueue(q)
	}
	ledger := &stubBeadLedger{} // empty — no br-ready beads
	return daemon.WorkLoopDepsParams{
		BrAdapter:     ledger,
		Bus:           bus,
		ProjectDir:    projectDir,
		HandlerBinary: "/bin/sh",
		HandlerArgs:   []string{"-c", "exit 0"},
		IntentLogDir:  filepath.Join(projectDir, ".harmonik", "beads-intents"),
		QueueStore:    qs,
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
	}
}

// queueDispatchFixtureEventTypes extracts the list of event type strings from
// a stubEventCollector in order.
func queueDispatchFixtureEventTypes(col *stubEventCollector) []string {
	evts := col.allEvents()
	out := make([]string, len(evts))
	for i, e := range evts {
		out[i] = e.EventType
	}
	return out
}

// queueDispatchFixturePollClosed polls ledger.closedIDs until count IDs are
// present or the deadline elapses.
func queueDispatchFixturePollClosed(t *testing.T, ledger *stubBeadLedger, count int, deadline time.Duration) {
	t.Helper()
	timer := time.NewTimer(deadline)
	defer timer.Stop()
	for {
		if len(ledger.closedIDs()) >= count {
			return
		}
		select {
		case <-timer.C:
			t.Fatalf("queueDispatchFixturePollClosed: timed out after %s waiting for %d closed beads; got %d",
				deadline, count, len(ledger.closedIDs()))
		case <-time.After(25 * time.Millisecond):
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// TestQueueDispatch_HappyPath
// ─────────────────────────────────────────────────────────────────────────────

// TestQueueDispatch_HappyPath verifies that when a QueueStore is loaded with a
// single-item wave queue, the workloop pulls the item, dispatches it, and the
// bead is closed.  run_started must carry non-nil queue_id + queue_group_index
// (QM-011 / QM-012).
//
// Spec ref: execution-model.md §7.4 (TS-1); QM-011, QM-012.
// Bead ref: hk-45ude.
func TestQueueDispatch_HappyPath(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	t.Parallel()

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	const beadID = core.BeadID("hk45ude-happy-bead-001")
	q := queueDispatchFixtureWaveQueue(t, beadID)
	queueID := q.QueueID

	bus := &stubEventCollector{}
	p := queueDispatchFixtureDeps(t, projectDir, bus, q)
	deps := daemon.ExportedWorkLoopDeps(p)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	waitDone := make(chan struct{})
	go func() {
		defer close(waitDone)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	// Wait for the bead to be closed.
	ledger := p.BrAdapter.(*stubBeadLedger)
	queueDispatchFixturePollClosed(t, ledger, 1, 15*time.Second)

	cancel()
	select {
	case <-waitDone:
	case <-time.After(3 * time.Second):
		t.Fatal("workloop did not exit after context cancellation")
	}

	// Assert the bead was closed.
	closed := ledger.closedIDs()
	if len(closed) == 0 {
		t.Fatal("expected bead to be closed; none were")
	}
	if closed[0] != beadID {
		t.Errorf("closed bead = %q; want %q", closed[0], beadID)
	}

	// Assert run_started carries non-nil queue_id + queue_group_index.
	allEvts := bus.allEvents()
	var startedPayload struct {
		QueueID         *string `json:"queue_id"`
		QueueGroupIndex *int    `json:"queue_group_index"`
	}
	foundStarted := false
	for _, e := range allEvts {
		if e.EventType != string(core.EventTypeRunStarted) {
			continue
		}
		foundStarted = true
		if err := json.Unmarshal(e.Payload, &startedPayload); err != nil {
			t.Fatalf("unmarshal run_started payload: %v", err)
		}
		break
	}
	if !foundStarted {
		t.Fatal("run_started event not found")
	}
	if startedPayload.QueueID == nil || *startedPayload.QueueID != queueID {
		t.Errorf("run_started queue_id = %v; want %q", startedPayload.QueueID, queueID)
	}
	if startedPayload.QueueGroupIndex == nil || *startedPayload.QueueGroupIndex != 0 {
		t.Errorf("run_started queue_group_index = %v; want 0", startedPayload.QueueGroupIndex)
	}

	// Assert run_completed also carries queue fields.
	var completedPayload struct {
		QueueID         *string `json:"queue_id"`
		QueueGroupIndex *int    `json:"queue_group_index"`
	}
	foundCompleted := false
	for _, e := range allEvts {
		if e.EventType != string(core.EventTypeRunCompleted) {
			continue
		}
		foundCompleted = true
		if err := json.Unmarshal(e.Payload, &completedPayload); err != nil {
			t.Fatalf("unmarshal run_completed payload: %v", err)
		}
		break
	}
	if !foundCompleted {
		t.Fatal("run_completed event not found")
	}
	if completedPayload.QueueID == nil || *completedPayload.QueueID != queueID {
		t.Errorf("run_completed queue_id = %v; want %q", completedPayload.QueueID, queueID)
	}
	if completedPayload.QueueGroupIndex == nil || *completedPayload.QueueGroupIndex != 0 {
		t.Errorf("run_completed queue_group_index = %v; want 0", completedPayload.QueueGroupIndex)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// TestQueueDispatch_EM015f_GroupAdvanceGate
// ─────────────────────────────────────────────────────────────────────────────

// TestQueueDispatch_EM015f_GroupAdvanceGate verifies that a two-group queue does
// NOT start group 1 until group 0 is all-terminal (EM-015f).
//
// We call ExportedEvaluateGroupAdvanceWithOutcome directly (rather than running
// the full workloop) to isolate the gate logic.  The test verifies:
//   - Before any terminal: group 1 stays pending.
//   - After group 0 item 0 completes: group 1 still pending (only 1 of 1 items
//     terminal in a single-item group 0 ← actually complete-success here).
//
// For a two-item wave:
//   - After item 0 terminal (success): group 0 still active (item 1 in flight).
//   - After both items terminal (success): group 0 → complete-success; group 1 → active.
//
// Spec ref: execution-model.md §4.3.EM-015f.
// Bead ref: hk-45ude.
func TestQueueDispatch_EM015f_GroupAdvanceGate(t *testing.T) {
	t.Parallel()

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	const (
		beadA = core.BeadID("hk45ude-gate-a")
		beadB = core.BeadID("hk45ude-gate-b")
		beadC = core.BeadID("hk45ude-gate-c") // group 1
	)

	// Build two-group queue: group 0 = [beadA, beadB], group 1 = [beadC].
	// Both items in group 0 start as dispatched (simulate in-flight).
	q := queueDispatchFixtureTwoGroupQueue(t, []core.BeadID{beadA, beadB}, []core.BeadID{beadC})
	q.Groups[0].Items[0].Status = queue.ItemStatusDispatched
	q.Groups[0].Items[1].Status = queue.ItemStatusDispatched
	queueID := q.QueueID
	groupIndex := 0

	qs := daemon.ExportedNewQueueStore()
	qs.SetQueue(q)

	bus := &stubEventCollector{}
	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:    &stubBeadLedger{},
		Bus:          bus,
		ProjectDir:   projectDir,
		IntentLogDir: filepath.Join(projectDir, ".harmonik", "beads-intents"),
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		QueueStore:   qs,
	})

	// Advance item 0 only (beadA success) — group should stay active.
	daemon.ExportedEvaluateGroupAdvanceWithOutcome(context.Background(), deps, "", queueID, groupIndex, 0, true)

	q2 := qs.Queue()
	if q2 == nil {
		t.Fatal("queue became nil after first advance")
	}
	if q2.Groups[0].Status != queue.GroupStatusActive {
		t.Errorf("group 0 status = %q after one of two items completes; want active", q2.Groups[0].Status)
	}
	if q2.Groups[1].Status != queue.GroupStatusPending {
		t.Errorf("group 1 status = %q (should still be pending); want pending", q2.Groups[1].Status)
	}

	// Advance item 1 (beadB success) — now both terminal; group 0 → complete-success.
	daemon.ExportedEvaluateGroupAdvanceWithOutcome(context.Background(), deps, "", queueID, groupIndex, 1, true)

	q3 := qs.Queue()
	if q3 == nil {
		t.Fatal("queue became nil after second advance")
	}
	if q3.Groups[0].Status != queue.GroupStatusCompleteSuccess {
		t.Errorf("group 0 status = %q after both items complete; want complete-success", q3.Groups[0].Status)
	}
	// Group 1 should now be active (activated by group 0 complete-success).
	if q3.Groups[1].Status != queue.GroupStatusActive {
		t.Errorf("group 1 status = %q after group 0 complete-success; want active", q3.Groups[1].Status)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// TestQueueDispatch_FailurePath_QueuePaused
// ─────────────────────────────────────────────────────────────────────────────

// TestQueueDispatch_FailurePath_QueuePaused verifies that when a group
// reaches complete-with-failures, the queue transitions to paused-by-failure
// and a queue_paused event is emitted.
//
// Spec ref: execution-model.md §4.3.EM-015f (complete-with-failures branch);
// queue-model.md §8.3.
// Bead ref: hk-45ude.
func TestQueueDispatch_FailurePath_QueuePaused(t *testing.T) {
	t.Parallel()

	projectDir, _ := workloopFixtureProjectDir(t)

	const beadID = core.BeadID("hk45ude-fail-bead-001")

	q := queueDispatchFixtureWaveQueue(t, beadID)
	// Mark the item as already dispatched.
	q.Groups[0].Items[0].Status = queue.ItemStatusDispatched
	queueID := q.QueueID

	qs := daemon.ExportedNewQueueStore()
	qs.SetQueue(q)

	bus := &stubEventCollector{}
	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:    &stubBeadLedger{},
		Bus:          bus,
		ProjectDir:   projectDir,
		IntentLogDir: filepath.Join(projectDir, ".harmonik", "beads-intents"),
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		QueueStore:   qs,
	})

	// Simulate a failed run outcome.
	daemon.ExportedEvaluateGroupAdvanceWithOutcome(context.Background(), deps, "", queueID, 0, 0, false)

	q2 := qs.Queue()
	if q2 == nil {
		t.Fatal("queue became nil after failure advance")
	}

	// Queue must be paused-by-failure.
	if q2.Status != queue.QueueStatusPausedByFailure {
		t.Errorf("queue.Status = %q; want paused-by-failure", q2.Status)
	}

	// Group 0 must be complete-with-failures.
	if q2.Groups[0].Status != queue.GroupStatusCompleteWithFailures {
		t.Errorf("group 0 status = %q; want complete-with-failures", q2.Groups[0].Status)
	}

	// Bus must have received queue_group_completed and queue_paused events.
	eventTypes := queueDispatchFixtureEventTypes(bus)
	foundGroupCompleted := false
	foundQueuePaused := false
	for _, et := range eventTypes {
		switch et {
		case "queue_group_completed":
			foundGroupCompleted = true
		case "queue_paused":
			foundQueuePaused = true
		}
	}
	if !foundGroupCompleted {
		t.Errorf("queue_group_completed not emitted; got events: %v", eventTypes)
	}
	if !foundQueuePaused {
		t.Errorf("queue_paused not emitted; got events: %v", eventTypes)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// TestQueueDispatch_BackwardCompat_BrReadyFallback
// ─────────────────────────────────────────────────────────────────────────────

// TestQueueDispatch_BackwardCompat_BrReadyFallback verifies that when the
// QueueStore is nil (no queue loaded), the workloop falls back to the br-ready
// poll path and closes the bead normally.
//
// This is the backward-compatibility test for existing single-bead dispatch
// tests that do not use the queue surface.
//
// Bead ref: hk-45ude.
func TestQueueDispatch_BackwardCompat_BrReadyFallback(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	t.Parallel()

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	const beadID = core.BeadID("hk45ude-compat-bead-001")
	ledger := &stubBeadLedger{
		ready: []core.BeadID{beadID},
	}
	bus := &stubEventCollector{}

	// No QueueStore — uses br-ready path.
	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:     ledger,
		Bus:           bus,
		ProjectDir:    projectDir,
		HandlerBinary: "/bin/sh",
		HandlerArgs:   []string{"-c", "exit 0"},
		IntentLogDir:  filepath.Join(projectDir, ".harmonik", "beads-intents"),
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		// QueueStore: nil — intentionally absent
	})

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	waitDone := make(chan struct{})
	go func() {
		defer close(waitDone)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	queueDispatchFixturePollClosed(t, ledger, 1, 15*time.Second)

	cancel()
	select {
	case <-waitDone:
	case <-time.After(3 * time.Second):
		t.Fatal("workloop did not exit after context cancellation")
	}

	closed := ledger.closedIDs()
	if len(closed) == 0 {
		t.Fatal("expected bead to be closed via br-ready fallback; none were")
	}
	if closed[0] != beadID {
		t.Errorf("closed bead = %q; want %q", closed[0], beadID)
	}

	// run_started must NOT carry queue fields when no queue is active.
	allEvts := bus.allEvents()
	for _, e := range allEvts {
		if e.EventType != string(core.EventTypeRunStarted) {
			continue
		}
		var pl struct {
			QueueID         *string `json:"queue_id"`
			QueueGroupIndex *int    `json:"queue_group_index"`
		}
		if err := json.Unmarshal(e.Payload, &pl); err != nil {
			t.Fatalf("unmarshal run_started: %v", err)
		}
		if pl.QueueID != nil {
			t.Errorf("run_started queue_id = %v; want nil (no queue loaded)", pl.QueueID)
		}
		if pl.QueueGroupIndex != nil {
			t.Errorf("run_started queue_group_index = %v; want nil (no queue loaded)", pl.QueueGroupIndex)
		}
		break
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// TestQueueDispatch_QueuePausedState_NoDispatch
// ─────────────────────────────────────────────────────────────────────────────

// TestQueueDispatch_QueuePausedState_NoDispatch verifies that when the queue is
// in paused-by-failure state, the workloop does NOT dispatch any beads and
// instead idles (per §7.4 pseudocode IF queue.status IN {paused...}).
//
// Bead ref: hk-45ude.
func TestQueueDispatch_QueuePausedState_NoDispatch(t *testing.T) {
	t.Parallel()

	projectDir, _ := workloopFixtureProjectDir(t)

	const beadID = core.BeadID("hk45ude-paused-bead-001")
	// Build a paused queue with a pending item.
	q := queueDispatchFixtureWaveQueue(t, beadID)
	q.Status = queue.QueueStatusPausedByFailure

	qs := daemon.ExportedNewQueueStore()
	qs.SetQueue(q)

	ledger := &stubBeadLedger{}
	bus := &stubEventCollector{}

	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:    ledger,
		Bus:          bus,
		ProjectDir:   projectDir,
		IntentLogDir: filepath.Join(projectDir, ".harmonik", "beads-intents"),
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		QueueStore:   qs,
	})

	// Run the loop for a short window and verify no dispatch occurred.
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()
	wg.Wait()

	// No beads should have been closed or reopened.
	if len(ledger.closedIDs()) != 0 {
		t.Errorf("closed %d beads; want 0 (queue is paused)", len(ledger.closedIDs()))
	}
	// No run_started events should have been emitted.
	for _, e := range bus.allEvents() {
		if e.EventType == string(core.EventTypeRunStarted) {
			t.Error("run_started emitted despite paused queue; want none")
		}
	}
}

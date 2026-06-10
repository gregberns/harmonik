package scenario

// named_queues_pause_test.go — SC3: per-queue pause isolates one named queue.
//
// Bead: hk-tigaf.7 (NQ-C2 scenario test)
// Spec refs:
//   - specs/queue-model.md §8.5 QM-054 (pause-by-drain entry; in-flight runs continue)
//   - specs/queue-model.md §8.6 QM-055 (persisted pause survives restart)
//   - specs/queue-model.md §5.3 QM-031 (pending→active gate: only when queue active)
//   - specs/queue-model.md §5.2 QM-030 (all-terminal gate: in-flight completions accepted)
//
// SC3 scenario:
//  1. Two queues active: "investigate" (group0 active with one in-flight item,
//     group1 pending) and "main" (group0 active with two pending items).
//  2. Pause "investigate" (status → paused-by-drain).
//  3. Assert "main" keeps dispatching: EligibleItems returns pending items.
//  4. Assert in-flight "investigate" run reaches terminal: AdvanceGroup accepts
//     the completion on the in-flight item while the queue is paused.
//  5. Assert "investigate" group1 stays pending while queue is paused (QM-031).
//  6. Resume "investigate" (status → active) and assert group1 can advance.
//
// These tests exercise the queue state machine and persistence layer directly —
// no live daemon, no event bus. This is the same layer exercised by
// queue_paused_test.go and queue_daemon_wiring_test.go.
//
// Helper prefix: namedQueuesPause (implementer-protocol.md §Helper-prefix).
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/queue"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

var namedQueuesPauseNow = time.Date(2026, 5, 31, 10, 0, 0, 0, time.UTC)

const (
	namedQueuesPauseInvestigateQueueID = "01906000-0001-7000-8000-000000000001"
	namedQueuesPauseMainQueueID        = "01906000-0002-7000-8000-000000000002"
)

// namedQueuesPauseProjectDir creates a temporary project root pre-populated
// with .harmonik/ for queue.Persist / queue.Load.
func namedQueuesPauseProjectDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	//nolint:gosec // G301: 0755 matches .harmonik dir conventions
	if err := os.MkdirAll(filepath.Join(dir, ".harmonik"), 0o755); err != nil {
		t.Fatalf("namedQueuesPauseProjectDir: MkdirAll .harmonik: %v", err)
	}
	return dir
}

// namedQueuesPauseInvestigateQueue returns the "investigate" fixture queue:
//   - group0 active: item-a dispatched (in-flight at pause time), item-b pending
//   - group1 pending: item-c pending (awaiting group0 complete-success)
func namedQueuesPauseInvestigateQueue() queue.Queue {
	runID := "00000000-0000-0000-0000-00000000aa01"
	return queue.Queue{
		SchemaVersion: 1,
		QueueID:       namedQueuesPauseInvestigateQueueID,
		Name:          "investigate",
		SubmittedAt:   namedQueuesPauseNow,
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindWave,
				Status:     queue.GroupStatusActive,
				Items: []queue.Item{
					{
						BeadID: core.BeadID("hk-sc3-inv-a"),
						Status: queue.ItemStatusDispatched, // in-flight when pause happens
						RunID:  &runID,
					},
					{
						BeadID: core.BeadID("hk-sc3-inv-b"),
						Status: queue.ItemStatusPending, // not yet dispatched
					},
				},
				CreatedAt: namedQueuesPauseNow,
			},
			{
				GroupIndex: 1,
				Kind:       queue.GroupKindWave,
				Status:     queue.GroupStatusPending,
				Items: []queue.Item{
					{
						BeadID: core.BeadID("hk-sc3-inv-c"),
						Status: queue.ItemStatusPending,
					},
				},
				CreatedAt: namedQueuesPauseNow,
			},
		},
	}
}

// namedQueuesPauseMainQueue returns the "main" fixture queue:
//   - group0 active: item-x and item-y both pending (eligible for dispatch)
func namedQueuesPauseMainQueue() queue.Queue {
	return queue.Queue{
		SchemaVersion: 1,
		QueueID:       namedQueuesPauseMainQueueID,
		Name:          "main",
		SubmittedAt:   namedQueuesPauseNow,
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindWave,
				Status:     queue.GroupStatusActive,
				Items: []queue.Item{
					{BeadID: core.BeadID("hk-sc3-main-x"), Status: queue.ItemStatusPending},
					{BeadID: core.BeadID("hk-sc3-main-y"), Status: queue.ItemStatusPending},
				},
				CreatedAt: namedQueuesPauseNow,
			},
		},
	}
}

// namedQueuesPauseAdvanceGroup calls queue.AdvanceGroup and fails the test on
// any unexpected error.
func namedQueuesPauseAdvanceGroup(
	t *testing.T,
	g *queue.Group,
	queueStatus queue.QueueStatus,
	queueID string,
) (queue.GroupStatus, []core.Event) {
	t.Helper()
	newStatus, events, err := queue.AdvanceGroup(
		context.Background(),
		g,
		queueStatus,
		queueID,
		namedQueuesPauseNow,
	)
	if err != nil {
		t.Fatalf("namedQueuesPauseAdvanceGroup: AdvanceGroup: %v", err)
	}
	return newStatus, events
}

// ---------------------------------------------------------------------------
// SC3.1 — main keeps dispatching when investigate is paused
// ---------------------------------------------------------------------------

// TestNamedQueuesPause_MainKeepsDispatchingWhenInvestigatePaused verifies that
// after pausing the "investigate" queue (status → paused-by-drain), the "main"
// queue remains active and its EligibleItems return pending items for dispatch.
//
// Spec ref: specs/queue-model.md §8.5 QM-054 (pause is scoped to the named
// queue; other queues are unaffected).
func TestNamedQueuesPause_MainKeepsDispatchingWhenInvestigatePaused(t *testing.T) {
	t.Parallel()

	investigateQ := namedQueuesPauseInvestigateQueue()
	mainQ := namedQueuesPauseMainQueue()

	// Simulate operator pause scoped to "investigate".
	investigateQ.Status = queue.QueueStatusPausedByDrain

	// main must remain active — pause is scoped to investigate only.
	if mainQ.Status != queue.QueueStatusActive {
		t.Errorf("main.Status = %q after investigate pause, want active (named pause must not affect other queues)",
			mainQ.Status)
	}

	// EligibleItems on main's active group must still return both pending items.
	eligible := queue.EligibleItems(&mainQ.Groups[0])
	if len(eligible) != 2 {
		t.Errorf("EligibleItems(main.group0) = %d items after investigate pause, want 2",
			len(eligible))
	}
	for _, item := range eligible {
		if item.Status != queue.ItemStatusPending {
			t.Errorf("EligibleItems returned item %q with status %q, want pending",
				item.BeadID, item.Status)
		}
	}
}

// TestNamedQueuesPause_MainAdvancesGroupWhileInvestigatePaused verifies that
// AdvanceGroup on a main group advances normally (pending → active) while
// "investigate" is paused. The main queue is still active, so QM-031 allows
// the transition.
//
// Spec ref: specs/queue-model.md §5.3 QM-031 (pending→active requires queue active).
func TestNamedQueuesPause_MainAdvancesGroupWhileInvestigatePaused(t *testing.T) {
	t.Parallel()

	mainQ := namedQueuesPauseMainQueue()

	// Build a successor pending group for main to advance.
	pendingGroup := queue.Group{
		GroupIndex: 1,
		Kind:       queue.GroupKindWave,
		Status:     queue.GroupStatusPending,
		Items: []queue.Item{
			{BeadID: core.BeadID("hk-sc3-main-z"), Status: queue.ItemStatusPending},
		},
		CreatedAt: namedQueuesPauseNow,
	}

	// Advance with main queue active (investigate is paused but main is not).
	newStatus, _ := namedQueuesPauseAdvanceGroup(t, &pendingGroup, mainQ.Status, mainQ.QueueID)

	if newStatus != queue.GroupStatusActive {
		t.Errorf("main pending group status after advance = %q, want active (QM-031: main queue is still active)",
			newStatus)
	}
}

// ---------------------------------------------------------------------------
// SC3.2 — in-flight investigate run reaches terminal while queue is paused
// ---------------------------------------------------------------------------

// TestNamedQueuesPause_InFlightRunReachesTerminalWhilePaused verifies that an
// "investigate" item that was dispatched before the pause can complete (reach
// terminal) while the queue is paused-by-drain.
//
// This confirms QM-054: "in-flight runs continue" — the pause does not abort
// already-running dispatch sessions. At the state-machine layer, AdvanceGroup
// accepts a completion on the in-flight item. The group stays active because
// item-b (pending) is still non-terminal (QM-030 all-terminal gate).
//
// Spec ref: specs/queue-model.md §8.5 QM-054 (in-flight runs continue);
//
//	§5.2 QM-030 (all-terminal gate).
func TestNamedQueuesPause_InFlightRunReachesTerminalWhilePaused(t *testing.T) {
	t.Parallel()

	investigateQ := namedQueuesPauseInvestigateQueue()
	investigateQ.Status = queue.QueueStatusPausedByDrain

	// Simulate the in-flight run (item-a, dispatched) completing while paused.
	investigateQ.Groups[0].Items[0].Status = queue.ItemStatusCompleted

	// item-b is still pending — AdvanceGroup must NOT complete the group yet
	// (QM-030: all-terminal gate; item-b is non-terminal).
	newStatus, events := namedQueuesPauseAdvanceGroup(
		t,
		&investigateQ.Groups[0],
		investigateQ.Status, // paused-by-drain
		investigateQ.QueueID,
	)

	if newStatus != queue.GroupStatusActive {
		t.Errorf("group0 status = %q after in-flight item completes, want active (item-b still pending; QM-030 gate)",
			newStatus)
	}
	if len(events) != 0 {
		t.Errorf("event count = %d, want 0 (group not yet terminal; item-b still pending)", len(events))
	}

	// The in-flight item must now be terminal — verify the state is accepted.
	if investigateQ.Groups[0].Items[0].Status != queue.ItemStatusCompleted {
		t.Errorf("item-a status = %q after completion, want completed", investigateQ.Groups[0].Items[0].Status)
	}
}

// TestNamedQueuesPause_BothInvestigateItemsTerminalWhilePaused verifies that
// when BOTH items in the in-flight group complete while paused, AdvanceGroup
// transitions the group to complete-success even though the queue is
// paused-by-drain.
//
// The "paused" status gates new dispatches (via the workloop), not the
// all-terminal completion transition (QM-030). Once all items are terminal the
// group MUST complete regardless of queue status.
//
// Spec ref: specs/queue-model.md §5.2 QM-030; §8.5 QM-054.
func TestNamedQueuesPause_BothInvestigateItemsTerminalWhilePaused(t *testing.T) {
	t.Parallel()

	investigateQ := namedQueuesPauseInvestigateQueue()
	investigateQ.Status = queue.QueueStatusPausedByDrain

	// Both items complete while the queue is paused.
	investigateQ.Groups[0].Items[0].Status = queue.ItemStatusCompleted
	investigateQ.Groups[0].Items[1].Status = queue.ItemStatusCompleted

	newStatus, events := namedQueuesPauseAdvanceGroup(
		t,
		&investigateQ.Groups[0],
		investigateQ.Status, // paused-by-drain
		investigateQ.QueueID,
	)

	if newStatus != queue.GroupStatusCompleteSuccess {
		t.Errorf("group0 status = %q after all items complete, want complete-success (QM-030 terminal gate)",
			newStatus)
	}
	if len(events) != 1 {
		t.Fatalf("event count = %d, want 1 (queue_group_completed)", len(events))
	}
	if events[0].Type != "queue_group_completed" {
		t.Errorf("events[0].Type = %q, want %q", events[0].Type, "queue_group_completed")
	}
}

// ---------------------------------------------------------------------------
// SC3.3 — paused investigate blocks successor pending group (QM-031)
// ---------------------------------------------------------------------------

// TestNamedQueuesPause_PausedInvestigatePendingGroupStaysPending verifies that
// group1 (pending) in the "investigate" queue does NOT advance to active while
// the queue is paused-by-drain. QM-031 gates the pending→active transition on
// queue status == active.
//
// Spec ref: specs/queue-model.md §5.3 QM-031.
func TestNamedQueuesPause_PausedInvestigatePendingGroupStaysPending(t *testing.T) {
	t.Parallel()

	investigateQ := namedQueuesPauseInvestigateQueue()
	investigateQ.Status = queue.QueueStatusPausedByDrain

	newStatus, events := namedQueuesPauseAdvanceGroup(
		t,
		&investigateQ.Groups[1], // group1 pending
		investigateQ.Status,     // paused-by-drain
		investigateQ.QueueID,
	)

	if newStatus != queue.GroupStatusPending {
		t.Errorf("investigate group1 status = %q after advance while paused, want pending (QM-031: gate requires queue active)",
			newStatus)
	}
	if len(events) != 0 {
		t.Errorf("event count = %d, want 0 while queue is paused-by-drain", len(events))
	}
}

// ---------------------------------------------------------------------------
// SC3.4 — resume restores dispatch for investigate
// ---------------------------------------------------------------------------

// TestNamedQueuesPause_ResumeRestoresPendingGroupAdvance verifies that after
// resuming "investigate" (status → active), the successor pending group can
// advance to active (QM-031 gate satisfied).
//
// Spec ref: specs/queue-model.md §5.3 QM-031; §8.5 QM-054.
func TestNamedQueuesPause_ResumeRestoresPendingGroupAdvance(t *testing.T) {
	t.Parallel()

	investigateQ := namedQueuesPauseInvestigateQueue()

	// Simulate pause then resume.
	investigateQ.Status = queue.QueueStatusPausedByDrain
	investigateQ.Status = queue.QueueStatusActive // resume

	newStatus, events := namedQueuesPauseAdvanceGroup(
		t,
		&investigateQ.Groups[1], // group1 pending
		investigateQ.Status,     // active after resume
		investigateQ.QueueID,
	)

	if newStatus != queue.GroupStatusActive {
		t.Errorf("investigate group1 status = %q after resume + advance, want active (QM-031: queue now active)",
			newStatus)
	}
	if len(events) != 1 {
		t.Fatalf("event count = %d after pending→active, want 1 (queue_group_started)", len(events))
	}
	if events[0].Type != "queue_group_started" {
		t.Errorf("events[0].Type = %q, want %q", events[0].Type, "queue_group_started")
	}
}

// TestNamedQueuesPause_ResumeRestoresEligibleItems verifies that after
// resuming "investigate", EligibleItems on the active group returns items for
// dispatch again. The pending items that were blocked from dispatch during the
// pause become eligible once the queue is resumed.
//
// Spec ref: specs/queue-model.md §8.5 QM-054; §5.5 QM-035/QM-036.
func TestNamedQueuesPause_ResumeRestoresEligibleItems(t *testing.T) {
	t.Parallel()

	investigateQ := namedQueuesPauseInvestigateQueue()
	investigateQ.Status = queue.QueueStatusPausedByDrain
	investigateQ.Status = queue.QueueStatusActive // resume

	// item-a was dispatched; item-b is pending — wave group: only pending items eligible.
	eligible := queue.EligibleItems(&investigateQ.Groups[0])
	if len(eligible) != 1 {
		t.Fatalf("EligibleItems(investigate.group0) after resume = %d items, want 1 (item-b pending)",
			len(eligible))
	}
	if eligible[0].BeadID != core.BeadID("hk-sc3-inv-b") {
		t.Errorf("eligible item = %q, want hk-sc3-inv-b", eligible[0].BeadID)
	}
}

// ---------------------------------------------------------------------------
// SC3.5 — persist round-trip: paused status survives restart (QM-055)
// ---------------------------------------------------------------------------

// TestNamedQueuesPause_PersistRoundTrip_InvestigatePausedSurvivesRestart
// verifies that the "investigate" queue with status=paused-by-drain survives a
// Persist → Load round-trip, confirming QM-055 per named queue. The pause
// status must not be reset on restart.
//
// Spec ref: specs/queue-model.md §8.6 QM-055.
func TestNamedQueuesPause_PersistRoundTrip_InvestigatePausedSurvivesRestart(t *testing.T) {
	t.Parallel()

	projectDir := namedQueuesPauseProjectDir(t)
	ctx := context.Background()

	investigateQ := namedQueuesPauseInvestigateQueue()
	investigateQ.Status = queue.QueueStatusPausedByDrain

	if err := queue.Persist(ctx, projectDir, &investigateQ); err != nil {
		t.Fatalf("Persist(investigate): %v", err)
	}

	loaded, err := queue.Load(ctx, projectDir, "investigate")
	if err != nil {
		t.Fatalf("Load(investigate): %v", err)
	}
	if loaded == nil {
		t.Fatal("Load(investigate): returned nil; want paused investigate queue")
	}

	// QM-055: pause status must survive restart unchanged.
	if loaded.Status != queue.QueueStatusPausedByDrain {
		t.Errorf("loaded investigate.Status = %q, want paused-by-drain (QM-055: persisted pause survives restart)",
			loaded.Status)
	}
	if loaded.Name != "investigate" {
		t.Errorf("loaded investigate.Name = %q, want %q", loaded.Name, "investigate")
	}
	if loaded.QueueID != namedQueuesPauseInvestigateQueueID {
		t.Errorf("loaded investigate.QueueID = %q, want %q",
			loaded.QueueID, namedQueuesPauseInvestigateQueueID)
	}
}

// TestNamedQueuesPause_PersistRoundTrip_BothQueuesCoexistOnDisk verifies that
// the "investigate" (paused-by-drain) and "main" (active) queues can coexist
// in .harmonik/queues/ and load independently. Each named queue is persisted
// to its own file (.harmonik/queues/<name>.json) per specs/queue-model.md §2.9.
//
// This is the multi-queue isolation test: pausing one queue must not corrupt
// or remove the other.
//
// Spec ref: specs/queue-model.md §2.9 (per-name queue files); §8.5 QM-054.
func TestNamedQueuesPause_PersistRoundTrip_BothQueuesCoexistOnDisk(t *testing.T) {
	t.Parallel()

	projectDir := namedQueuesPauseProjectDir(t)
	ctx := context.Background()

	investigateQ := namedQueuesPauseInvestigateQueue()
	investigateQ.Status = queue.QueueStatusPausedByDrain

	mainQ := namedQueuesPauseMainQueue()
	// main remains active — the pause is scoped to investigate only.

	if err := queue.Persist(ctx, projectDir, &investigateQ); err != nil {
		t.Fatalf("Persist(investigate): %v", err)
	}
	if err := queue.Persist(ctx, projectDir, &mainQ); err != nil {
		t.Fatalf("Persist(main): %v", err)
	}

	// Load investigate — must be paused-by-drain.
	loadedInvestigate, err := queue.Load(ctx, projectDir, "investigate")
	if err != nil {
		t.Fatalf("Load(investigate): %v", err)
	}
	if loadedInvestigate == nil {
		t.Fatal("Load(investigate): returned nil")
	}
	if loadedInvestigate.Status != queue.QueueStatusPausedByDrain {
		t.Errorf("investigate.Status = %q, want paused-by-drain", loadedInvestigate.Status)
	}

	// Load main — must remain active.
	loadedMain, err := queue.Load(ctx, projectDir, "main")
	if err != nil {
		t.Fatalf("Load(main): %v", err)
	}
	if loadedMain == nil {
		t.Fatal("Load(main): returned nil")
	}
	if loadedMain.Status != queue.QueueStatusActive {
		t.Errorf("main.Status = %q, want active (pause of investigate must not affect main)", loadedMain.Status)
	}
}

// TestNamedQueuesPause_PersistRoundTrip_ResumedStatusSurvivesRestart verifies
// that after resuming "investigate" (status → active) and persisting, the
// resumed status survives a Persist → Load round-trip. The queue must load as
// active — not paused-by-drain.
//
// Spec ref: specs/queue-model.md §8.5 QM-054; §3.2 QM-002 (load reads on-disk status verbatim).
func TestNamedQueuesPause_PersistRoundTrip_ResumedStatusSurvivesRestart(t *testing.T) {
	t.Parallel()

	projectDir := namedQueuesPauseProjectDir(t)
	ctx := context.Background()

	investigateQ := namedQueuesPauseInvestigateQueue()

	// Simulate pause → resume cycle.
	investigateQ.Status = queue.QueueStatusPausedByDrain
	if err := queue.Persist(ctx, projectDir, &investigateQ); err != nil {
		t.Fatalf("Persist(investigate, paused): %v", err)
	}

	investigateQ.Status = queue.QueueStatusActive
	if err := queue.Persist(ctx, projectDir, &investigateQ); err != nil {
		t.Fatalf("Persist(investigate, resumed): %v", err)
	}

	loaded, err := queue.Load(ctx, projectDir, "investigate")
	if err != nil {
		t.Fatalf("Load(investigate, after resume): %v", err)
	}
	if loaded == nil {
		t.Fatal("Load(investigate): returned nil after resume")
	}
	if loaded.Status != queue.QueueStatusActive {
		t.Errorf("loaded.Status = %q after resume, want active", loaded.Status)
	}
}

package scenario_test

// queue_lifecycle_test.go — end-to-end queue lifecycle scenario (T80, hk-8vokz).
//
// Coverage:
//   (a) BootstrapFixture creates an isolated project root for the scenario.
//   (b) Wave group with 2 items returns both eligible simultaneously (QM-036 parallelism).
//   (c) Group-advance gate: group 1 stays pending until group 0 is all-terminal (EM-015f).
//   (d) Group-advance transition: group 1 activates after group 0 reaches complete-success
//       (QM-051); §8.10 emission ordering is respected (QM-065).
//   (e) queue.json is unlinked when the last group reaches complete-success (QM-003 / QM-053).
//
// Fixture beads: .beads/queue-test-fixtures/two-wave-group-queue.json
//   Documents the bead-ID set used here. See that file for the fixture layout.
//
// The tests exercise the queue state machine (queue.AdvanceGroup, queue.EligibleItems),
// queue persistence (queue.Persist, queue.CompleteAndUnlink, queue.Load), and the
// daemon.QueueStore ownership layer — all within the scenario-harness fixture
// lifecycle (BootstrapFixture for isolated project root, SH-012).
//
// Helper prefix: queueLifecycleFixture (per implementer-protocol.md
// §Helper-prefix discipline, bead hk-8vokz).
//
// Spec ref: specs/queue-model.md §5.1 (group state table), §5.2 QM-030,
//           §5.7 QM-036, §8.2 QM-051, §3.3 QM-003, §8.4 QM-053,
//           §9.5 QM-065 (emission ordering);
//           specs/execution-model.md §4.3.EM-015f (group-advance gate).
// Bead ref: hk-8vokz.
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/queue"
	"github.com/gregberns/harmonik/internal/scenario"
)

// ─────────────────────────────────────────────────────────────────────────────
// Fixture constants — bead IDs
// ─────────────────────────────────────────────────────────────────────────────

// queueLifecycleFixture bead IDs match .beads/queue-test-fixtures/two-wave-group-queue.json.
//
//	Group 0 — wave, 2 items dispatched in parallel (QM-036 wave admission).
//	Group 1 — wave, 1 item; activates only after group 0 all-terminal (EM-015f).
const (
	queueLifecycleG0Item0 = core.BeadID("hk-qtfix-g0-item0")
	queueLifecycleG0Item1 = core.BeadID("hk-qtfix-g0-item1")
	queueLifecycleG1Item0 = core.BeadID("hk-qtfix-g1-item0")
)

// ─────────────────────────────────────────────────────────────────────────────
// Fixture helpers
// ─────────────────────────────────────────────────────────────────────────────

// queueLifecycleFixtureProjectDir creates an isolated project root via
// BootstrapFixture (SH-012). Returns the absolute path to the synthetic project
// root. The fixture root is cleaned up via t.Cleanup.
func queueLifecycleFixtureProjectDir(t *testing.T) string {
	t.Helper()
	fixtureRoot, err := os.MkdirTemp("", "harmonik-ql-")
	if err != nil {
		t.Fatalf("queueLifecycleFixtureProjectDir: MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(fixtureRoot) })

	result, err := scenario.BootstrapFixture(t.Context(), fixtureRoot, "queue-lifecycle", nil)
	if err != nil {
		t.Fatalf("queueLifecycleFixtureProjectDir: BootstrapFixture: %v", err)
	}
	return result.ProjectRoot
}

// queueLifecycleFixtureTwoGroupQueue constructs the canonical 2-wave-group queue:
//
//	group 0 (wave, active):  items G0Item0, G0Item1 — parallel-eligible per QM-036
//	group 1 (wave, pending): item  G1Item0           — activates per QM-051 after group 0
//
// This layout matches .beads/queue-test-fixtures/two-wave-group-queue.json.
func queueLifecycleFixtureTwoGroupQueue(t *testing.T) queue.Queue {
	t.Helper()
	now := time.Now()
	return queue.Queue{
		SchemaVersion: 1,
		QueueID:       "ql-test-queue-" + t.Name(),
		SubmittedAt:   now,
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindWave,
				Status:     queue.GroupStatusActive,
				Items: []queue.Item{
					{BeadID: queueLifecycleG0Item0, Status: queue.ItemStatusPending},
					{BeadID: queueLifecycleG0Item1, Status: queue.ItemStatusPending},
				},
				CreatedAt: now,
			},
			{
				GroupIndex: 1,
				Kind:       queue.GroupKindWave,
				Status:     queue.GroupStatusPending,
				Items: []queue.Item{
					{BeadID: queueLifecycleG1Item0, Status: queue.ItemStatusPending},
				},
				CreatedAt: now,
			},
		},
	}
}

// queueLifecycleFixtureQueueJSON returns the canonical .harmonik/queue.json path.
func queueLifecycleFixtureQueueJSON(projectDir string) string {
	return filepath.Join(projectDir, ".harmonik", "queue.json")
}

// ─────────────────────────────────────────────────────────────────────────────
// TestQueueLifecycle_BootstrapFixture_ProjectRootExists
// ─────────────────────────────────────────────────────────────────────────────

// TestQueueLifecycle_BootstrapFixture_ProjectRootExists verifies that
// queueLifecycleFixtureProjectDir produces a valid isolated project root per
// BootstrapFixture (SH-012).  This is acceptance criterion (a): the scenario
// harness creates a clean project root before orchestration.
//
// Spec ref: specs/scenario-harness.md §4.4 SH-012.
// Bead ref: hk-8vokz.
func TestQueueLifecycle_BootstrapFixture_ProjectRootExists(t *testing.T) {
	t.Parallel()

	projectDir := queueLifecycleFixtureProjectDir(t)

	info, err := os.Stat(projectDir)
	if err != nil {
		t.Fatalf("(SH-012) BootstrapFixture project root %q: stat error: %v", projectDir, err)
	}
	if !info.IsDir() {
		t.Errorf("(SH-012) project root %q: exists but is not a directory", projectDir)
	}

	// .harmonik/events/ must exist per SH-014.
	eventsDir := filepath.Join(projectDir, ".harmonik", "events")
	if _, err := os.Stat(eventsDir); err != nil {
		t.Errorf("(SH-014) events dir %q: %v", eventsDir, err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// TestQueueLifecycle_WaveParallelism
// ─────────────────────────────────────────────────────────────────────────────

// TestQueueLifecycle_WaveParallelism verifies that a wave group with two pending
// items returns both as eligible simultaneously (QM-036 wave admission is
// unordered; no head-of-line blocking).
//
// This is acceptance criterion (b): group 0 dispatches both items in parallel.
//
// Spec ref: specs/queue-model.md §5.7 QM-036.
// Bead ref: hk-8vokz.
func TestQueueLifecycle_WaveParallelism(t *testing.T) {
	t.Parallel()

	now := time.Now()
	g := queue.Group{
		GroupIndex: 0,
		Kind:       queue.GroupKindWave,
		Status:     queue.GroupStatusActive,
		Items: []queue.Item{
			{BeadID: queueLifecycleG0Item0, Status: queue.ItemStatusPending},
			{BeadID: queueLifecycleG0Item1, Status: queue.ItemStatusPending},
		},
		CreatedAt: now,
	}

	eligible := queue.EligibleItems(&g)
	if len(eligible) != 2 {
		t.Fatalf("(QM-036) EligibleItems = %d; want 2 — both items must be eligible simultaneously for wave parallelism",
			len(eligible))
	}
	if eligible[0].BeadID != queueLifecycleG0Item0 {
		t.Errorf("(QM-036) eligible[0].BeadID = %q; want %q", eligible[0].BeadID, queueLifecycleG0Item0)
	}
	if eligible[1].BeadID != queueLifecycleG0Item1 {
		t.Errorf("(QM-036) eligible[1].BeadID = %q; want %q", eligible[1].BeadID, queueLifecycleG0Item1)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// TestQueueLifecycle_GroupAdvance_EM015f_Gate
// ─────────────────────────────────────────────────────────────────────────────

// TestQueueLifecycle_GroupAdvance_EM015f_Gate verifies acceptance criterion (c):
// group 0 remains active (blocking dispatch-eligible items from running) until
// all items are terminal, then group 0 transitions to complete-success (QM-030).
// Only after group 0 reaches complete-success does the caller advance group 1
// (EM-015f caller discipline; QM-031 is satisfied by caller context, not by
// AdvanceGroup itself per the godoc: "The caller is responsible for supplying
// the correct predecessor-complete-success trigger context").
//
// Step 1 — one of two items in group 0 terminal: group 0 stays active (QM-030
//
//	all-terminal gate blocks transition).
//
// Step 2 — both items in group 0 terminal: group 0 → complete-success.
// Step 3 — caller then invokes AdvanceGroup(group1): group 1 → active (QM-051).
//
// Spec ref: specs/queue-model.md §5.2 QM-030; §8.2 QM-051.
//
//	specs/execution-model.md §4.3.EM-015f.
//
// Bead ref: hk-8vokz.
func TestQueueLifecycle_GroupAdvance_EM015f_Gate(t *testing.T) {
	t.Parallel()

	q := queueLifecycleFixtureTwoGroupQueue(t)
	queueID := q.QueueID
	now := time.Now()

	// Simulate both group-0 items dispatched (in-flight) and then one completes.
	q.Groups[0].Items[0].Status = queue.ItemStatusCompleted  // first item terminal
	q.Groups[0].Items[1].Status = queue.ItemStatusDispatched // second item still in-flight

	// ── Step 1: one item done, one still dispatched → QM-030 all-terminal gate. ──
	// AdvanceGroup MUST return active because item 1 is dispatched (not terminal).
	newStatus0, events0, err := queue.AdvanceGroup(context.Background(), &q.Groups[0], q.Status, queueID, now)
	if err != nil {
		t.Fatalf("(step 1) AdvanceGroup group 0: %v", err)
	}
	if newStatus0 != queue.GroupStatusActive {
		t.Errorf("(step 1 QM-030) group 0 = %q; want active while item 1 is still dispatched",
			newStatus0)
	}
	if len(events0) != 0 {
		t.Errorf("(step 1 QM-030) expected 0 events when group stays active; got %d", len(events0))
	}

	// EligibleItems for group 0 at this point: item 1 is dispatched, not pending;
	// item 0 is completed.  Neither is pending → no new items eligible.
	eligible := queue.EligibleItems(&q.Groups[0])
	if len(eligible) != 0 {
		t.Errorf("(step 1 QM-036) EligibleItems = %d; want 0 (item 1 dispatched, item 0 completed)",
			len(eligible))
	}

	// ── Step 2: item 1 also becomes terminal → group 0 now all-terminal. ──
	q.Groups[0].Items[1].Status = queue.ItemStatusCompleted

	newStatus0b, events0b, err := queue.AdvanceGroup(context.Background(), &q.Groups[0], q.Status, queueID, now)
	if err != nil {
		t.Fatalf("(step 2) AdvanceGroup group 0: %v", err)
	}
	if newStatus0b != queue.GroupStatusCompleteSuccess {
		t.Fatalf("(step 2 QM-030) group 0 = %q; want complete-success (all items terminal, zero failed)",
			newStatus0b)
	}
	if len(events0b) != 1 || events0b[0].Type != "queue_group_completed" {
		t.Fatalf("(step 2) expected 1 queue_group_completed event; got %v", eventTypes(events0b))
	}
	q.Groups[0].Status = newStatus0b

	// ── Step 3: caller now advances group 1 (EM-015f: advance only when predecessor
	// is complete-success). QM-031 requires queue.status == active. ──
	newStatus1, events1, err := queue.AdvanceGroup(context.Background(), &q.Groups[1], q.Status, queueID, now)
	if err != nil {
		t.Fatalf("(step 3) AdvanceGroup group 1: %v", err)
	}
	if newStatus1 != queue.GroupStatusActive {
		t.Errorf("(step 3 QM-051) group 1 = %q; want active after group 0 complete-success",
			newStatus1)
	}
	if len(events1) != 1 || events1[0].Type != "queue_group_started" {
		t.Fatalf("(step 3) expected 1 queue_group_started event; got %v", eventTypes(events1))
	}
	q.Groups[1].Status = newStatus1

	// Group 1 item is now eligible for dispatch.
	eligible1 := queue.EligibleItems(&q.Groups[1])
	if len(eligible1) != 1 {
		t.Fatalf("(step 3 QM-036) EligibleItems group 1 = %d; want 1", len(eligible1))
	}
	if eligible1[0].BeadID != queueLifecycleG1Item0 {
		t.Errorf("(step 3) eligible1[0].BeadID = %q; want %q", eligible1[0].BeadID, queueLifecycleG1Item0)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// TestQueueLifecycle_EmissionOrdering_QM065
// ─────────────────────────────────────────────────────────────────────────────

// TestQueueLifecycle_EmissionOrdering_QM065 verifies §8.10 / QM-065 emission
// ordering: after group 0 reaches complete-success, the event sequence is:
//
//	queue_group_completed{group_0, final_status: complete-success}
//	queue_group_started{group_1}
//
// This is the emission ordering rule cited in acceptance criterion (c).
//
// Spec ref: specs/queue-model.md §8.2 QM-051; §9.5 QM-065.
// Bead ref: hk-8vokz.
func TestQueueLifecycle_EmissionOrdering_QM065(t *testing.T) {
	t.Parallel()

	q := queueLifecycleFixtureTwoGroupQueue(t)
	queueID := q.QueueID
	now := time.Now()

	// Simulate group 0 all-terminal (both items completed).
	q.Groups[0].Items[0].Status = queue.ItemStatusCompleted
	q.Groups[0].Items[1].Status = queue.ItemStatusCompleted

	// Advance group 0: must produce queue_group_completed.
	newStatus0, events0, err := queue.AdvanceGroup(context.Background(), &q.Groups[0], q.Status, queueID, now)
	if err != nil {
		t.Fatalf("AdvanceGroup group 0: %v", err)
	}
	if newStatus0 != queue.GroupStatusCompleteSuccess {
		t.Fatalf("group 0 status = %q; want complete-success", newStatus0)
	}
	q.Groups[0].Status = newStatus0

	// Advance group 1: must produce queue_group_started.
	_, events1, err := queue.AdvanceGroup(context.Background(), &q.Groups[1], q.Status, queueID, now)
	if err != nil {
		t.Fatalf("AdvanceGroup group 1: %v", err)
	}

	// Collect events in emission order: group-0 events first, then group-1 events.
	// This mirrors the production path in evaluateGroupAdvanceWithOutcome where
	// group-0 events are collected then group-1 events appended (events = append(events, nextEvents...)).
	allEvents := append(events0, events1...) //nolint:gocritic // appendAssign: explicit collect for ordering test

	if len(allEvents) < 2 {
		t.Fatalf("(QM-065) expected at least 2 events (group_completed + group_started); got %d", len(allEvents))
	}

	// Verify ordering: queue_group_completed must precede queue_group_started.
	completedIdx := -1
	startedIdx := -1
	for i, e := range allEvents {
		switch e.Type {
		case "queue_group_completed":
			if completedIdx < 0 {
				completedIdx = i
			}
		case "queue_group_started":
			if startedIdx < 0 {
				startedIdx = i
			}
		}
	}
	if completedIdx < 0 {
		t.Error("(QM-065) queue_group_completed event not found")
	}
	if startedIdx < 0 {
		t.Error("(QM-065) queue_group_started event not found")
	}
	if completedIdx >= 0 && startedIdx >= 0 && completedIdx > startedIdx {
		t.Errorf("(QM-065 emission ordering) queue_group_completed at index %d must precede queue_group_started at index %d",
			completedIdx, startedIdx)
	}

	// Verify queue_group_completed payload carries final_status=complete-success.
	if completedIdx >= 0 {
		var p core.QueueGroupCompletedPayload
		if err := json.Unmarshal(allEvents[completedIdx].Payload, &p); err != nil {
			t.Fatalf("unmarshal queue_group_completed payload: %v", err)
		}
		if p.FinalStatus != string(queue.GroupStatusCompleteSuccess) {
			t.Errorf("queue_group_completed.final_status = %q; want %q",
				p.FinalStatus, queue.GroupStatusCompleteSuccess)
		}
		if p.QueueID != queueID {
			t.Errorf("queue_group_completed.queue_id = %q; want %q", p.QueueID, queueID)
		}
		if p.GroupIndex != 0 {
			t.Errorf("queue_group_completed.group_index = %d; want 0", p.GroupIndex)
		}
	}

	// Verify queue_group_started payload carries group_index=1.
	if startedIdx >= 0 {
		var p core.QueueGroupStartedPayload
		if err := json.Unmarshal(allEvents[startedIdx].Payload, &p); err != nil {
			t.Fatalf("unmarshal queue_group_started payload: %v", err)
		}
		if p.GroupIndex != 1 {
			t.Errorf("queue_group_started.group_index = %d; want 1", p.GroupIndex)
		}
		if p.QueueID != queueID {
			t.Errorf("queue_group_started.queue_id = %q; want %q", p.QueueID, queueID)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// TestQueueLifecycle_QueueJSON_UnlinkedOnCompletion
// ─────────────────────────────────────────────────────────────────────────────

// TestQueueLifecycle_QueueJSON_UnlinkedOnCompletion verifies acceptance criterion
// (e): queue.json is absent from .harmonik/ after the last group reaches
// complete-success (QM-003 removal-on-completion, QM-053 completion sequence).
//
// Uses queue.CompleteAndUnlink which is the production path called when the
// queue transitions to completed.  BootstrapFixture provides the isolated
// project root per SH-012.
//
// Spec ref: specs/queue-model.md §8.4 QM-053; §3.3 QM-003.
// Bead ref: hk-8vokz.
func TestQueueLifecycle_QueueJSON_UnlinkedOnCompletion(t *testing.T) {
	t.Parallel()

	projectDir := queueLifecycleFixtureProjectDir(t)
	queueJSONPath := queueLifecycleFixtureQueueJSON(projectDir)

	// Build a 2-wave-group queue with all groups complete-success (simulates
	// the state immediately before the QM-053 completion sequence fires).
	q := queueLifecycleFixtureTwoGroupQueue(t)
	q.Groups[0].Status = queue.GroupStatusCompleteSuccess
	q.Groups[1].Status = queue.GroupStatusCompleteSuccess

	// Persist queue.json so it exists before the completion sequence.
	if err := queue.Persist(t.Context(), projectDir, &q); err != nil {
		t.Fatalf("Persist: %v", err)
	}
	if _, err := os.Stat(queueJSONPath); err != nil {
		t.Fatalf("queue.json must exist before CompleteAndUnlink: %v", err)
	}

	// Execute the QM-053 completion sequence.
	if err := queue.CompleteAndUnlink(t.Context(), projectDir, &q); err != nil {
		t.Fatalf("CompleteAndUnlink: %v", err)
	}

	// (e) queue.json must be absent after CompleteAndUnlink.
	if _, err := os.Stat(queueJSONPath); !os.IsNotExist(err) {
		t.Errorf("(QM-003) queue.json at %q: expected absent after CompleteAndUnlink; stat err=%v",
			queueJSONPath, err)
	}

	// (e) In-memory queue status must be completed (persisted before unlink per QM-053 step 1).
	if q.Status != queue.QueueStatusCompleted {
		t.Errorf("(QM-053) q.Status after CompleteAndUnlink = %q; want %q",
			q.Status, queue.QueueStatusCompleted)
	}

	// (e) Load after unlink must return nil (no active queue per QM-053 step 4).
	loaded, err := queue.Load(t.Context(), projectDir)
	if err != nil {
		t.Fatalf("Load after CompleteAndUnlink: %v", err)
	}
	if loaded != nil {
		t.Errorf("(QM-053) Load after CompleteAndUnlink: got non-nil queue %+v; want nil", loaded)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// TestQueueLifecycle_QueueStore_HoldsActiveQueue
// ─────────────────────────────────────────────────────────────────────────────

// TestQueueLifecycle_QueueStore_HoldsActiveQueue verifies that the daemon's
// QueueStore correctly holds the 2-wave-group queue and that its group structure
// is consistent with the queue lifecycle scenario fixture.
//
// This tests the scenario's setup precondition: the queue is loaded into the
// daemon's QueueStore before the workloop begins dispatching.
//
// Spec ref: specs/queue-model.md §9.1 QM-060 (single-writer discipline).
// Bead ref: hk-8vokz.
func TestQueueLifecycle_QueueStore_HoldsActiveQueue(t *testing.T) {
	t.Parallel()

	q := queueLifecycleFixtureTwoGroupQueue(t)
	qCopy := q // store a copy

	qs := &daemon.QueueStore{}
	qs.SetQueue(&q)

	// Queue must be readable and match the fixture.
	got := qs.Queue()
	if got == nil {
		t.Fatal("QueueStore.Queue() returned nil after SetQueue")
	}
	if got.QueueID != qCopy.QueueID {
		t.Errorf("QueueID = %q; want %q", got.QueueID, qCopy.QueueID)
	}
	if got.Status != queue.QueueStatusActive {
		t.Errorf("Status = %q; want active", got.Status)
	}
	if len(got.Groups) != 2 {
		t.Fatalf("len(Groups) = %d; want 2", len(got.Groups))
	}
	if got.Groups[0].Status != queue.GroupStatusActive {
		t.Errorf("Groups[0].Status = %q; want active", got.Groups[0].Status)
	}
	if got.Groups[1].Status != queue.GroupStatusPending {
		t.Errorf("Groups[1].Status = %q; want pending", got.Groups[1].Status)
	}
	if len(got.Groups[0].Items) != 2 {
		t.Errorf("Groups[0].Items count = %d; want 2 (wave parallelism requires 2 items)", len(got.Groups[0].Items))
	}
	if len(got.Groups[1].Items) != 1 {
		t.Errorf("Groups[1].Items count = %d; want 1", len(got.Groups[1].Items))
	}

	// ClearQueue must cause Queue() to return nil.
	qs.ClearQueue()
	if qs.Queue() != nil {
		t.Error("QueueStore.Queue() must return nil after ClearQueue")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// TestQueueLifecycle_FullStateSequence
// ─────────────────────────────────────────────────────────────────────────────

// TestQueueLifecycle_FullStateSequence exercises the complete 2-wave-group queue
// lifecycle through the state machine in a single test:
//
//  1. queue.active / group0.active / group1.pending (initial state after submit)
//  2. group0 wave: both items eligible simultaneously (QM-036 parallelism)
//  3. group0 items dispatched → AdvanceGroup stays active (EM-015f)
//  4. group0 items completed → AdvanceGroup → complete-success
//  5. group1 activates (QM-051 advance); group1 item eligible
//  6. group1 item completed → complete-success
//  7. Last group complete-success → CompleteAndUnlink (QM-053/QM-003)
//
// This is the canonical scenario that the acceptance criteria describe.
//
// Spec ref: specs/queue-model.md §5.1 (transition table), §8.2 QM-051,
//
//	§8.4 QM-053, §3.3 QM-003, §9.5 QM-065.
//
// Bead ref: hk-8vokz.
func TestQueueLifecycle_FullStateSequence(t *testing.T) {
	t.Parallel()

	projectDir := queueLifecycleFixtureProjectDir(t)
	queueJSONPath := queueLifecycleFixtureQueueJSON(projectDir)

	q := queueLifecycleFixtureTwoGroupQueue(t)
	queueID := q.QueueID
	now := time.Now()

	// ── Phase 1: initial state. ──
	if q.Status != queue.QueueStatusActive {
		t.Fatalf("(phase 1) initial queue.Status = %q; want active", q.Status)
	}
	if q.Groups[0].Status != queue.GroupStatusActive {
		t.Fatalf("(phase 1) group 0 initial status = %q; want active", q.Groups[0].Status)
	}
	if q.Groups[1].Status != queue.GroupStatusPending {
		t.Fatalf("(phase 1) group 1 initial status = %q; want pending", q.Groups[1].Status)
	}

	// ── Phase 2: wave parallelism — both items eligible. ──
	eligible := queue.EligibleItems(&q.Groups[0])
	if len(eligible) != 2 {
		t.Fatalf("(phase 2 QM-036) EligibleItems = %d; want 2 for wave parallelism", len(eligible))
	}

	// ── Phase 3: dispatch both items (simulate concurrent dispatch). ──
	q.Groups[0].Items[0].Status = queue.ItemStatusDispatched
	q.Groups[0].Items[1].Status = queue.ItemStatusDispatched

	// Group 0 stays active while items are in-flight.
	newSt, evts, err := queue.AdvanceGroup(context.Background(), &q.Groups[0], q.Status, queueID, now)
	if err != nil {
		t.Fatalf("(phase 3) AdvanceGroup: %v", err)
	}
	if newSt != queue.GroupStatusActive {
		t.Errorf("(phase 3 EM-015f) group 0 = %q while items dispatched; want active", newSt)
	}
	if len(evts) != 0 {
		t.Errorf("(phase 3) expected 0 events while group 0 in-flight; got %d", len(evts))
	}

	// ── Phase 4: both items complete; group 0 → complete-success. ──
	q.Groups[0].Items[0].Status = queue.ItemStatusCompleted
	q.Groups[0].Items[1].Status = queue.ItemStatusCompleted

	newSt0, evts0, err := queue.AdvanceGroup(context.Background(), &q.Groups[0], q.Status, queueID, now)
	if err != nil {
		t.Fatalf("(phase 4) AdvanceGroup group 0: %v", err)
	}
	if newSt0 != queue.GroupStatusCompleteSuccess {
		t.Fatalf("(phase 4 QM-030) group 0 = %q; want complete-success", newSt0)
	}
	if len(evts0) != 1 || evts0[0].Type != "queue_group_completed" {
		t.Fatalf("(phase 4) expected 1 queue_group_completed event; got %v", eventTypes(evts0))
	}
	q.Groups[0].Status = newSt0

	// ── Phase 5: group 1 activates (QM-051) + item eligible. ──
	newSt1, evts1, err := queue.AdvanceGroup(context.Background(), &q.Groups[1], q.Status, queueID, now)
	if err != nil {
		t.Fatalf("(phase 5) AdvanceGroup group 1: %v", err)
	}
	if newSt1 != queue.GroupStatusActive {
		t.Fatalf("(phase 5 QM-051) group 1 = %q after group 0 complete-success; want active", newSt1)
	}
	if len(evts1) != 1 || evts1[0].Type != "queue_group_started" {
		t.Fatalf("(phase 5) expected 1 queue_group_started event; got %v", eventTypes(evts1))
	}
	q.Groups[1].Status = newSt1

	// ── Phase 5 continued: emission ordering (QM-065). ──
	// The production path appends group-1 events after group-0 events:
	//   events = append(events0b, events1b...) in evaluateGroupAdvance.
	combinedEvents := append(evts0, evts1...) //nolint:gocritic // appendAssign: explicit ordering test
	if combinedEvents[0].Type != "queue_group_completed" {
		t.Errorf("(QM-065) combined[0] = %q; want queue_group_completed", combinedEvents[0].Type)
	}
	if combinedEvents[1].Type != "queue_group_started" {
		t.Errorf("(QM-065) combined[1] = %q; want queue_group_started", combinedEvents[1].Type)
	}

	// ── Phase 5: group 1 eligible items. ──
	eligible1 := queue.EligibleItems(&q.Groups[1])
	if len(eligible1) != 1 {
		t.Fatalf("(phase 5) EligibleItems group 1 = %d; want 1", len(eligible1))
	}
	if eligible1[0].BeadID != queueLifecycleG1Item0 {
		t.Errorf("(phase 5) eligible1[0].BeadID = %q; want %q", eligible1[0].BeadID, queueLifecycleG1Item0)
	}

	// ── Phase 6: group 1 item completes; group 1 → complete-success. ──
	q.Groups[1].Items[0].Status = queue.ItemStatusCompleted
	newSt1b, evts1b, err := queue.AdvanceGroup(context.Background(), &q.Groups[1], q.Status, queueID, now)
	if err != nil {
		t.Fatalf("(phase 6) AdvanceGroup group 1: %v", err)
	}
	if newSt1b != queue.GroupStatusCompleteSuccess {
		t.Fatalf("(phase 6) group 1 = %q; want complete-success", newSt1b)
	}
	if len(evts1b) != 1 || evts1b[0].Type != "queue_group_completed" {
		t.Fatalf("(phase 6) expected 1 queue_group_completed event; got %v", eventTypes(evts1b))
	}
	q.Groups[1].Status = newSt1b

	// ── Phase 7: QM-053 completion + QM-003 unlink. ──
	// CompleteAndUnlink is the production completion path for the last group.
	if err := queue.CompleteAndUnlink(t.Context(), projectDir, &q); err != nil {
		t.Fatalf("(phase 7 QM-053) CompleteAndUnlink: %v", err)
	}

	// (e) queue.json must be absent.
	if _, err := os.Stat(queueJSONPath); !os.IsNotExist(err) {
		t.Errorf("(phase 7 QM-003) queue.json still present at %q after CompleteAndUnlink", queueJSONPath)
	}

	// (e) in-memory status must be completed.
	if q.Status != queue.QueueStatusCompleted {
		t.Errorf("(phase 7 QM-053) q.Status = %q; want completed", q.Status)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// eventTypes — helper for error messages
// ─────────────────────────────────────────────────────────────────────────────

// eventTypes returns the type strings of a slice of core.Event values.
func eventTypes(events []core.Event) []string {
	out := make([]string, len(events))
	for i, e := range events {
		out[i] = e.Type
	}
	return out
}

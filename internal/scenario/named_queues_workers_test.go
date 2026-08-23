package scenario

// named_queues_workers_test.go — SC2: per-queue workers honor caps + global ceiling.
//
// Bead: hk-tigaf.5 (NQ-B2 scenario test)
// Spec refs:
//   - specs/queue-model.md §9.3 QM-062 (capacity composition: min(pending, max_concurrent - running))
//   - specs/queue-model.md §5.7 QM-036 (wave group: all pending items eligible simultaneously)
//   - specs/queue-model.md §9.1 QM-060 (single-writer discipline on QueueStore)
//   - specs/execution-model.md §4.11 EM-049 (capacity gate)
//
// SC2 scenario:
//  1. Two queues active: "main" (wave group with 3 pending items) and
//     "investigate" (wave group with 1 pending item). maxConcurrent = 4.
//  2. EligibleItems on each group returns the correct pending-item count.
//  3. QM-062 cap simulation: dispatching items from both queues, the total
//     concurrently dispatched items never exceeds maxConcurrent (4).
//  4. At capacity (running = maxConcurrent): the QM-062 formula yields 0 for
//     any further admit request — neither queue contributes new dispatches.
//  5. Completion of one item frees a slot; one new dispatch is admitted.
//  6. QueueStore.AllQueues() correctly holds both named queues.
//
// These tests exercise the queue state machine and QueueStore ownership layer
// directly — no live daemon, no event bus. This is the same layer exercised by
// queue_lifecycle_test.go and queue_paused_test.go.
//
// Helper prefix: namedQueuesWorkers (implementer-protocol.md §Helper-prefix).
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

import (
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/queue"
	"github.com/gregberns/harmonik/internal/queuewiring"
)

const (
	namedQueuesWorkersMaxConcurrent = 4

	namedQueuesWorkersMainQueueID        = "01906000-0010-7000-8000-000000000010"
	namedQueuesWorkersInvestigateQueueID = "01906000-0010-7000-8000-000000000011"
)

var namedQueuesWorkersNow = time.Date(2026, 5, 31, 11, 0, 0, 0, time.UTC)

func namedQueuesWorkersMainQueue() queue.Queue {
	return queue.Queue{
		SchemaVersion: 1,
		QueueID:       namedQueuesWorkersMainQueueID,
		Name:          "main",
		SubmittedAt:   namedQueuesWorkersNow,
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindWave,
				Status:     queue.GroupStatusActive,
				Items: []queue.Item{
					{BeadID: core.BeadID("hk-sc2-main-a"), Status: queue.ItemStatusPending},
					{BeadID: core.BeadID("hk-sc2-main-b"), Status: queue.ItemStatusPending},
					{BeadID: core.BeadID("hk-sc2-main-c"), Status: queue.ItemStatusPending},
				},
				CreatedAt: namedQueuesWorkersNow,
			},
		},
	}
}

func namedQueuesWorkersInvestigateQueue() queue.Queue {
	return queue.Queue{
		SchemaVersion: 1,
		QueueID:       namedQueuesWorkersInvestigateQueueID,
		Name:          "investigate",
		SubmittedAt:   namedQueuesWorkersNow,
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindWave,
				Status:     queue.GroupStatusActive,
				Items: []queue.Item{
					{BeadID: core.BeadID("hk-sc2-inv-a"), Status: queue.ItemStatusPending},
				},
				CreatedAt: namedQueuesWorkersNow,
			},
		},
	}
}

// namedQueuesWorkersAdmitItems simulates the QM-062 workloop capacity gate for
// a single queue group. It returns the count of items that can be admitted from
// group g given (maxConcurrent, currentlyRunning) and marks those items as
// dispatched in-place.
//
// Admission count = min(eligible_count, maxConcurrent - currentlyRunning).
// Returns 0 when running >= maxConcurrent (capacity full).
//
// Spec ref: specs/queue-model.md §9.3 QM-062.
//
//nolint:unparam // The helper models the configurable production capacity gate.
func namedQueuesWorkersAdmitItems(g *queue.Group, maxConcurrent, currentlyRunning int) int {
	available := maxConcurrent - currentlyRunning
	if available <= 0 {
		return 0
	}
	eligible := queue.EligibleItems(g)
	admit := len(eligible)
	if admit > available {
		admit = available
	}
	for i := 0; i < admit; i++ {
		eligible[i].Status = queue.ItemStatusDispatched
	}
	return admit
}

// TestNamedQueuesWorkers_MainEligibleItemsCount verifies that the "main" queue's
// wave group returns all 3 pending items as eligible simultaneously (QM-036).
//
// Spec ref: specs/queue-model.md §5.7 QM-036.
func TestNamedQueuesWorkers_MainEligibleItemsCount(t *testing.T) {
	t.Parallel()

	mainQ := namedQueuesWorkersMainQueue()
	eligible := queue.EligibleItems(&mainQ.Groups[0])

	if len(eligible) != 3 {
		t.Errorf("EligibleItems(main.group0) = %d items, want 3 (QM-036: all pending items eligible in a wave group)",
			len(eligible))
	}
	for _, item := range eligible {
		if item.Status != queue.ItemStatusPending {
			t.Errorf("EligibleItems returned item %q with status %q, want pending",
				item.BeadID, item.Status)
		}
	}
}

// TestNamedQueuesWorkers_InvestigateEligibleItemsCount verifies that the
// "investigate" queue's wave group returns its 1 pending item as eligible.
//
// Spec ref: specs/queue-model.md §5.7 QM-036.
func TestNamedQueuesWorkers_InvestigateEligibleItemsCount(t *testing.T) {
	t.Parallel()

	invQ := namedQueuesWorkersInvestigateQueue()
	eligible := queue.EligibleItems(&invQ.Groups[0])

	if len(eligible) != 1 {
		t.Fatalf("EligibleItems(investigate.group0) = %d items, want 1", len(eligible))
	}
	if eligible[0].BeadID != core.BeadID("hk-sc2-inv-a") {
		t.Errorf("eligible[0].BeadID = %q, want hk-sc2-inv-a", eligible[0].BeadID)
	}
}

// TestNamedQueuesWorkers_QM062_TotalAdmittedEqualsGlobalCap verifies that when
// both queues dispatch concurrently, the total admitted items equals the global
// cap (4) and is not exceeded.
//
// Admission order: "main" dispatches first (3 items admitted), then "investigate"
// dispatches from the remaining 1 slot. Total = 4 = maxConcurrent.
//
// Spec ref: specs/queue-model.md §9.3 QM-062.
func TestNamedQueuesWorkers_QM062_TotalAdmittedEqualsGlobalCap(t *testing.T) {
	t.Parallel()

	mainQ := namedQueuesWorkersMainQueue()
	invQ := namedQueuesWorkersInvestigateQueue()

	currentlyRunning := 0

	admittedMain := namedQueuesWorkersAdmitItems(
		&mainQ.Groups[0], namedQueuesWorkersMaxConcurrent, currentlyRunning,
	)
	if admittedMain != 3 {
		t.Errorf("admitted from main = %d, want 3 (QM-062: min(3, 4-0)=3)", admittedMain)
	}
	currentlyRunning += admittedMain

	admittedInvestigate := namedQueuesWorkersAdmitItems(
		&invQ.Groups[0], namedQueuesWorkersMaxConcurrent, currentlyRunning,
	)
	if admittedInvestigate != 1 {
		t.Errorf("admitted from investigate = %d, want 1 (QM-062: min(1, 4-3)=1)", admittedInvestigate)
	}
	currentlyRunning += admittedInvestigate

	if currentlyRunning != namedQueuesWorkersMaxConcurrent {
		t.Errorf("total running = %d, want %d (all cap slots filled by main+investigate)",
			currentlyRunning, namedQueuesWorkersMaxConcurrent)
	}
}

// TestNamedQueuesWorkers_QM062_TotalNeverExceedsCap verifies that regardless of
// the order in which queues are evaluated, the total admitted items across both
// queues never exceeds maxConcurrent.
//
// "investigate" dispatches first (1 item), then "main" dispatches from the
// remaining 3 slots. Total is still 4 = maxConcurrent.
//
// Spec ref: specs/queue-model.md §9.3 QM-062.
func TestNamedQueuesWorkers_QM062_TotalNeverExceedsCap(t *testing.T) {
	t.Parallel()

	mainQ := namedQueuesWorkersMainQueue()
	invQ := namedQueuesWorkersInvestigateQueue()

	currentlyRunning := 0

	admittedInvestigate := namedQueuesWorkersAdmitItems(
		&invQ.Groups[0], namedQueuesWorkersMaxConcurrent, currentlyRunning,
	)
	if admittedInvestigate != 1 {
		t.Errorf("admitted from investigate = %d, want 1 (QM-062: min(1, 4-0)=1)", admittedInvestigate)
	}
	currentlyRunning += admittedInvestigate

	admittedMain := namedQueuesWorkersAdmitItems(
		&mainQ.Groups[0], namedQueuesWorkersMaxConcurrent, currentlyRunning,
	)
	if admittedMain != 3 {
		t.Errorf("admitted from main = %d, want 3 (QM-062: min(3, 4-1)=3)", admittedMain)
	}
	currentlyRunning += admittedMain

	if currentlyRunning > namedQueuesWorkersMaxConcurrent {
		t.Errorf("total running = %d, exceeds maxConcurrent %d (QM-062 violated)",
			currentlyRunning, namedQueuesWorkersMaxConcurrent)
	}
	if currentlyRunning != namedQueuesWorkersMaxConcurrent {
		t.Errorf("total running = %d, want %d (all 4 slots should be filled)",
			currentlyRunning, namedQueuesWorkersMaxConcurrent)
	}
}

// TestNamedQueuesWorkers_QM062_AtCapNoFurtherDispatch verifies that when
// currently_running equals maxConcurrent, the QM-062 capacity gate admits 0
// additional items from either queue.
//
// Spec ref: specs/queue-model.md §9.3 QM-062.
// Spec ref: specs/execution-model.md §4.11 EM-049.
func TestNamedQueuesWorkers_QM062_AtCapNoFurtherDispatch(t *testing.T) {
	t.Parallel()

	mainQ := namedQueuesWorkersMainQueue()
	invQ := namedQueuesWorkersInvestigateQueue()

	currentlyRunning := namedQueuesWorkersMaxConcurrent

	admittedMain := namedQueuesWorkersAdmitItems(
		&mainQ.Groups[0], namedQueuesWorkersMaxConcurrent, currentlyRunning,
	)
	if admittedMain != 0 {
		t.Errorf("admitted from main at capacity = %d, want 0 (QM-062: available slots = 0)", admittedMain)
	}

	admittedInvestigate := namedQueuesWorkersAdmitItems(
		&invQ.Groups[0], namedQueuesWorkersMaxConcurrent, currentlyRunning,
	)
	if admittedInvestigate != 0 {
		t.Errorf("admitted from investigate at capacity = %d, want 0 (QM-062: available slots = 0)", admittedInvestigate)
	}

	for _, item := range mainQ.Groups[0].Items {
		if item.Status != queue.ItemStatusPending {
			t.Errorf("main item %q status = %q after at-cap gate, want pending (no dispatch occurred)",
				item.BeadID, item.Status)
		}
	}
	for _, item := range invQ.Groups[0].Items {
		if item.Status != queue.ItemStatusPending {
			t.Errorf("investigate item %q status = %q after at-cap gate, want pending (no dispatch occurred)",
				item.BeadID, item.Status)
		}
	}
}

// TestNamedQueuesWorkers_QM062_CompletionFreesSlot verifies that when one of the
// dispatched items completes, the freed slot allows one new pending item to be
// admitted (QM-062: available = maxConcurrent - currently_running > 0).
//
// Spec ref: specs/queue-model.md §9.3 QM-062.
func TestNamedQueuesWorkers_QM062_CompletionFreesSlot(t *testing.T) {
	t.Parallel()

	mainQ := namedQueuesWorkersMainQueue()

	for i := range mainQ.Groups[0].Items {
		mainQ.Groups[0].Items[i].Status = queue.ItemStatusDispatched
	}
	currentlyRunning := namedQueuesWorkersMaxConcurrent // 4

	mainQ.Groups[0].Items = append(mainQ.Groups[0].Items, queue.Item{
		BeadID: core.BeadID("hk-sc2-main-d"),
		Status: queue.ItemStatusPending,
	})

	admitted := namedQueuesWorkersAdmitItems(
		&mainQ.Groups[0], namedQueuesWorkersMaxConcurrent, currentlyRunning,
	)
	if admitted != 0 {
		t.Errorf("admitted before completion = %d, want 0 (at capacity)", admitted)
	}

	mainQ.Groups[0].Items[0].Status = queue.ItemStatusCompleted
	currentlyRunning-- // 3

	admitted = namedQueuesWorkersAdmitItems(
		&mainQ.Groups[0], namedQueuesWorkersMaxConcurrent, currentlyRunning,
	)
	if admitted != 1 {
		t.Errorf("admitted after 1 completion = %d, want 1 (QM-062: min(1 pending, 4-3)=1)", admitted)
	}
	currentlyRunning += admitted
	if currentlyRunning > namedQueuesWorkersMaxConcurrent {
		t.Errorf("running after re-dispatch = %d, exceeds maxConcurrent %d",
			currentlyRunning, namedQueuesWorkersMaxConcurrent)
	}
}

// TestNamedQueuesWorkers_QueueStore_HoldsBothNamedQueues verifies that the
// daemon's QueueStore correctly stores and retrieves both "main" and "investigate"
// named queues independently. AllQueues returns both; each is accessible by name.
//
// Spec ref: specs/queue-model.md §9.1 QM-060 (single-writer discipline).
func TestNamedQueuesWorkers_QueueStore_HoldsBothNamedQueues(t *testing.T) {
	t.Parallel()

	mainQ := namedQueuesWorkersMainQueue()
	invQ := namedQueuesWorkersInvestigateQueue()

	qs := queuewiring.NewQueueStore()
	qs.SetQueue(&mainQ)
	qs.SetQueueByName("investigate", &invQ)

	all := qs.AllQueues()
	if len(all) != 2 {
		t.Fatalf("AllQueues() len = %d, want 2 (main + investigate)", len(all))
	}

	gotMain := qs.QueueByName("main")
	if gotMain == nil {
		t.Fatal("QueueByName(\"main\") returned nil")
	}
	if gotMain.QueueID != namedQueuesWorkersMainQueueID {
		t.Errorf("main.QueueID = %q, want %q", gotMain.QueueID, namedQueuesWorkersMainQueueID)
	}
	if len(gotMain.Groups[0].Items) != 3 {
		t.Errorf("main.Groups[0].Items count = %d, want 3", len(gotMain.Groups[0].Items))
	}

	gotInv := qs.QueueByName("investigate")
	if gotInv == nil {
		t.Fatal("QueueByName(\"investigate\") returned nil")
	}
	if gotInv.QueueID != namedQueuesWorkersInvestigateQueueID {
		t.Errorf("investigate.QueueID = %q, want %q", gotInv.QueueID, namedQueuesWorkersInvestigateQueueID)
	}
	if len(gotInv.Groups[0].Items) != 1 {
		t.Errorf("investigate.Groups[0].Items count = %d, want 1", len(gotInv.Groups[0].Items))
	}

	qs.ClearQueueByName("investigate")
	if qs.QueueByName("investigate") != nil {
		t.Error("QueueByName(\"investigate\") must return nil after ClearQueueByName")
	}
	if qs.QueueByName("main") == nil {
		t.Error("QueueByName(\"main\") must still return non-nil after clearing only investigate")
	}
}

// TestNamedQueuesWorkers_QueueStore_EligibleFromBothQueues verifies that after
// loading both queues into a QueueStore, EligibleItems on each queue's active
// group returns the expected item count. This confirms the QueueStore's
// multi-queue registry is consistent with the state-machine's eligible-item view.
//
// Spec ref: specs/queue-model.md §9.1 QM-060; §5.7 QM-036.
func TestNamedQueuesWorkers_QueueStore_EligibleFromBothQueues(t *testing.T) {
	t.Parallel()

	mainQ := namedQueuesWorkersMainQueue()
	invQ := namedQueuesWorkersInvestigateQueue()

	qs := queuewiring.NewQueueStore()
	qs.SetQueue(&mainQ)
	qs.SetQueueByName("investigate", &invQ)

	totalEligible := 0
	for name, q := range qs.AllQueues() {
		if q.Status != queue.QueueStatusActive {
			continue
		}
		var activeGroup *queue.Group
		for i := range q.Groups {
			if q.Groups[i].Status == queue.GroupStatusActive {
				activeGroup = &q.Groups[i]
				break
			}
		}
		if activeGroup == nil {
			t.Errorf("queue %q: no active group found", name)
			continue
		}
		eligible := queue.EligibleItems(activeGroup)
		totalEligible += len(eligible)
	}

	if totalEligible != 4 {
		t.Errorf("total eligible across all queues = %d, want 4 (main:3 + investigate:1)", totalEligible)
	}

	if totalEligible > namedQueuesWorkersMaxConcurrent {
		t.Errorf("total eligible = %d, exceeds maxConcurrent %d (would violate QM-062 if all dispatched at once)",
			totalEligible, namedQueuesWorkersMaxConcurrent)
	}
}

package queue_test

// resume_test.go — coverage for the paused-by-failure recovery transitions
// (ResumeFromFailure / RearmFailedItems) and the QM-027 resubmit relaxation
// that together unwedge a queue parked at paused-by-failure.
//
// Bead ref: hk-fkpb7. Spec ref: specs/queue-model.md §8.3 QM-052, §6.8 QM-027,
// §A.3.
//
// Helper prefix: resumeFixture (derived from "resume.go" per
// implementer-protocol.md §Helper-prefix discipline).

import (
	"context"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/queue"
)

// resumeFixtureFailedItem returns a failed Item that has burned its retry
// budget (Attempts at MaxItemAttempts), mirroring the workloop's hk-6pspu
// fail-on-max-attempts path.
func resumeFixtureFailedItem(beadID string) queue.Item {
	return queue.Item{
		BeadID:            core.BeadID(beadID),
		Status:            queue.ItemStatusFailed,
		Attempts:          queue.MaxItemAttempts,
		LastFailureReason: "rebase_conflict",
	}
}

// resumeFixtureItem returns an Item with the given BeadID and status.
func resumeFixtureItem(beadID string, status queue.ItemStatus) queue.Item {
	return queue.Item{BeadID: core.BeadID(beadID), Status: status}
}

// resumeFixtureFailedQueue builds a single-group queue parked at
// paused-by-failure: the group is complete-with-failures with one completed and
// one failed item (the hk-fkpb7 repro shape — one bead merged, a sibling lost
// the merge race and failed).
func resumeFixtureFailedQueue(kind queue.GroupKind) *queue.Queue {
	return &queue.Queue{
		SchemaVersion: 1,
		QueueID:       "0190b3c4-8f12-7c4e-9a82-2bf0d4ee9001",
		Name:          "fwkeeper",
		Status:        queue.QueueStatusPausedByFailure,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       kind,
				Status:     queue.GroupStatusCompleteWithFailures,
				Items: []queue.Item{
					resumeFixtureItem("hk-i0sgl", queue.ItemStatusCompleted),
					resumeFixtureFailedItem("hk-kzqml"),
				},
			},
		},
	}
}

// -----------------------------------------------------------------------
// (a) resume clears paused-by-failure
// -----------------------------------------------------------------------

// TestResumeFromFailure_ClearsPausedByFailure verifies that ResumeFromFailure
// flips a queue parked at paused-by-failure back to active, re-opens the
// complete-with-failures group, and re-arms the failed item — the core
// "resume does not clear paused-by-failure" bug (hk-fkpb7).
func TestResumeFromFailure_ClearsPausedByFailure(t *testing.T) {
	t.Parallel()

	q := resumeFixtureFailedQueue(queue.GroupKindStream)

	rearmed, ok := queue.ResumeFromFailure(q)

	if !ok {
		t.Fatalf("ResumeFromFailure: ok = false, want true for a paused-by-failure queue")
	}
	if q.Status != queue.QueueStatusActive {
		t.Errorf("queue status = %q, want %q (paused-by-failure must be cleared)", q.Status, queue.QueueStatusActive)
	}
	if got := q.Groups[0].Status; got != queue.GroupStatusActive {
		t.Errorf("group status = %q, want %q (failed group must re-open)", got, queue.GroupStatusActive)
	}
	if q.Groups[0].CompletedAt != nil {
		t.Errorf("group CompletedAt = %v, want nil after re-open", q.Groups[0].CompletedAt)
	}
	if len(rearmed) != 1 || rearmed[0] != core.BeadID("hk-kzqml") {
		t.Errorf("rearmed = %v, want [hk-kzqml]", rearmed)
	}
}

// TestResumeFromFailure_NoOpWhenNotPausedByFailure verifies the idempotent
// no-op contract: a resume against a queue that is active, paused-by-drain,
// completed, or nil leaves the queue untouched and returns ok=false. This keeps
// a duplicate operator-resume harmless and preserves paused-by-drain's separate
// active↔drain recovery path.
func TestResumeFromFailure_NoOpWhenNotPausedByFailure(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		status queue.QueueStatus
	}{
		{"active", queue.QueueStatusActive},
		{"paused_by_drain", queue.QueueStatusPausedByDrain},
		{"completed", queue.QueueStatusCompleted},
		{"cancelled", queue.QueueStatusCancelled},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			q := resumeFixtureFailedQueue(queue.GroupKindStream)
			q.Status = tc.status

			rearmed, ok := queue.ResumeFromFailure(q)

			if ok {
				t.Errorf("ResumeFromFailure: ok = true, want false for status %q", tc.status)
			}
			if rearmed != nil {
				t.Errorf("rearmed = %v, want nil for status %q", rearmed, tc.status)
			}
			if q.Status != tc.status {
				t.Errorf("queue status mutated to %q, want unchanged %q", q.Status, tc.status)
			}
		})
	}

	t.Run("nil_queue", func(t *testing.T) {
		t.Parallel()
		rearmed, ok := queue.ResumeFromFailure(nil)
		if ok || rearmed != nil {
			t.Errorf("ResumeFromFailure(nil) = (%v, %v), want (nil, false)", rearmed, ok)
		}
	})
}

// TestResumeFromFailure_LeavesNonTerminalGroupsAlone verifies that a multi-group
// queue parked at paused-by-failure only re-opens the group that actually
// reached complete-with-failures; a still-pending successor group stays pending.
func TestResumeFromFailure_LeavesNonTerminalGroupsAlone(t *testing.T) {
	t.Parallel()

	q := &queue.Queue{
		SchemaVersion: 1,
		QueueID:       "0190b3c4-8f12-7c4e-9a82-2bf0d4ee9002",
		Name:          "fwkeeper",
		Status:        queue.QueueStatusPausedByFailure,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindWave,
				Status:     queue.GroupStatusCompleteWithFailures,
				Items: []queue.Item{
					resumeFixtureFailedItem("hk-aaa01"),
				},
			},
			{
				GroupIndex: 1,
				Kind:       queue.GroupKindWave,
				Status:     queue.GroupStatusPending,
				Items: []queue.Item{
					resumeFixtureItem("hk-bbb01", queue.ItemStatusPending),
				},
			},
		},
	}

	rearmed, ok := queue.ResumeFromFailure(q)

	if !ok {
		t.Fatalf("ResumeFromFailure: ok = false, want true")
	}
	if q.Groups[0].Status != queue.GroupStatusActive {
		t.Errorf("group 0 status = %q, want %q", q.Groups[0].Status, queue.GroupStatusActive)
	}
	if q.Groups[1].Status != queue.GroupStatusPending {
		t.Errorf("group 1 status = %q, want unchanged %q", q.Groups[1].Status, queue.GroupStatusPending)
	}
	if len(rearmed) != 1 || rearmed[0] != core.BeadID("hk-aaa01") {
		t.Errorf("rearmed = %v, want [hk-aaa01]", rearmed)
	}
}

// -----------------------------------------------------------------------
// (b) the retry / re-arm path
// -----------------------------------------------------------------------

// TestRearmFailedItems_ResetsFailedToPending verifies the retry primitive:
// failed items go failed → pending with Attempts reset to 0 (clearing the
// MaxItemAttempts skip-gate) and LastFailureReason cleared, while non-failed
// siblings are untouched.
func TestRearmFailedItems_ResetsFailedToPending(t *testing.T) {
	t.Parallel()

	g := &queue.Group{
		GroupIndex: 0,
		Kind:       queue.GroupKindStream,
		Status:     queue.GroupStatusCompleteWithFailures,
		Items: []queue.Item{
			resumeFixtureItem("hk-done0", queue.ItemStatusCompleted),
			resumeFixtureFailedItem("hk-fail1"),
			resumeFixtureItem("hk-pend2", queue.ItemStatusPending),
			resumeFixtureFailedItem("hk-fail2"),
		},
	}

	rearmed := queue.RearmFailedItems(g)

	wantRearmed := []core.BeadID{"hk-fail1", "hk-fail2"}
	if len(rearmed) != len(wantRearmed) {
		t.Fatalf("rearmed = %v, want %v", rearmed, wantRearmed)
	}
	for i, want := range wantRearmed {
		if rearmed[i] != want {
			t.Errorf("rearmed[%d] = %q, want %q", i, rearmed[i], want)
		}
	}

	for _, it := range g.Items {
		switch it.BeadID {
		case "hk-fail1", "hk-fail2":
			if it.Status != queue.ItemStatusPending {
				t.Errorf("%s status = %q, want %q", it.BeadID, it.Status, queue.ItemStatusPending)
			}
			if it.Attempts != 0 {
				t.Errorf("%s Attempts = %d, want 0 (must clear MaxItemAttempts gate)", it.BeadID, it.Attempts)
			}
			if it.LastFailureReason != "" {
				t.Errorf("%s LastFailureReason = %q, want cleared", it.BeadID, it.LastFailureReason)
			}
		case "hk-done0":
			if it.Status != queue.ItemStatusCompleted {
				t.Errorf("hk-done0 status = %q, want unchanged %q", it.Status, queue.ItemStatusCompleted)
			}
		case "hk-pend2":
			if it.Status != queue.ItemStatusPending {
				t.Errorf("hk-pend2 status = %q, want unchanged %q", it.Status, queue.ItemStatusPending)
			}
		}
	}
	// The group status is the queue-level resume's concern, not RearmFailedItems'.
	if g.Status != queue.GroupStatusCompleteWithFailures {
		t.Errorf("RearmFailedItems must not touch group status; got %q", g.Status)
	}
}

// TestRearmFailedItems_NoFailedItemsIsNoOp verifies that a group with no failed
// items (and a nil group) is a no-op returning nil.
func TestRearmFailedItems_NoFailedItemsIsNoOp(t *testing.T) {
	t.Parallel()

	g := &queue.Group{
		GroupIndex: 0,
		Kind:       queue.GroupKindStream,
		Status:     queue.GroupStatusActive,
		Items: []queue.Item{
			resumeFixtureItem("hk-aaa01", queue.ItemStatusCompleted),
			resumeFixtureItem("hk-bbb01", queue.ItemStatusDispatched),
		},
	}
	if rearmed := queue.RearmFailedItems(g); rearmed != nil {
		t.Errorf("RearmFailedItems with no failed items = %v, want nil", rearmed)
	}
	if rearmed := queue.RearmFailedItems(nil); rearmed != nil {
		t.Errorf("RearmFailedItems(nil) = %v, want nil", rearmed)
	}
}

// TestRearmedItemBecomesEligible verifies the end-to-end retry intent: after
// re-arming, the previously-failed item is dispatch-eligible again via
// EligibleItems (the MaxItemAttempts skip-gate no longer hides it).
func TestRearmedItemBecomesEligible(t *testing.T) {
	t.Parallel()

	q := resumeFixtureFailedQueue(queue.GroupKindStream)
	// Before resume: the only non-terminal candidate is the failed item, and the
	// group is terminal, so nothing is eligible.
	if got := queue.EligibleItems(&q.Groups[0]); got != nil {
		t.Fatalf("pre-resume EligibleItems = %v, want nil (group terminal)", got)
	}

	if _, ok := queue.ResumeFromFailure(q); !ok {
		t.Fatalf("ResumeFromFailure: ok = false, want true")
	}

	eligible := queue.EligibleItems(&q.Groups[0])
	if len(eligible) != 1 {
		t.Fatalf("post-resume EligibleItems count = %d, want 1", len(eligible))
	}
	if eligible[0].BeadID != core.BeadID("hk-kzqml") {
		t.Errorf("eligible item = %q, want hk-kzqml", eligible[0].BeadID)
	}
}

// -----------------------------------------------------------------------
// (c) re-submit to a previously-stuck (paused-by-failure) name succeeds
// -----------------------------------------------------------------------

// TestValidate_ResubmitToPausedByFailureName verifies the QM-027 relaxation:
// a queue-submit targeting a name whose queue is parked at paused-by-failure is
// accepted (the failure-pause no longer wedges the name), while paused-by-drain
// and active still reject with queue_already_active.
//
// Spec ref: specs/queue-model.md §6.8 QM-027. Bead ref: hk-fkpb7.
func TestValidate_ResubmitToPausedByFailureName(t *testing.T) {
	t.Parallel()

	const idA = core.BeadID("hk-resub1")

	submitReq := func(active *queue.Queue) queue.ValidationRequest {
		return queue.ValidationRequest{
			Groups: []queue.Group{
				{
					GroupIndex: 0,
					Kind:       queue.GroupKindStream,
					Status:     queue.GroupStatusPending,
					Items:      []queue.Item{{BeadID: idA, Status: queue.ItemStatusPending}},
				},
			},
			ActiveQueue: active,
			QueueName:   "fwkeeper",
			IsAppend:    false,
		}
	}

	t.Run("pass_paused_by_failure", func(t *testing.T) {
		t.Parallel()
		active := &queue.Queue{
			SchemaVersion: 1,
			QueueID:       "stuck-queue-id",
			Name:          "fwkeeper",
			Status:        queue.QueueStatusPausedByFailure,
		}
		ledger := validFixtureOpenLedger(idA)
		errs, _, err := queue.Validate(context.Background(), submitReq(active), ledger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(errs) != 0 {
			t.Fatalf("expected resubmit to paused-by-failure name to pass; got errors: %v", errs)
		}
	})

	t.Run("fail_paused_by_drain", func(t *testing.T) {
		t.Parallel()
		active := &queue.Queue{
			SchemaVersion: 1,
			QueueID:       "drained-queue-id",
			Name:          "fwkeeper",
			Status:        queue.QueueStatusPausedByDrain,
		}
		ledger := validFixtureOpenLedger(idA)
		errs, _, err := queue.Validate(context.Background(), submitReq(active), ledger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(errs) != 1 || errs[0].Reason != queue.ReasonQueueAlreadyActive {
			t.Fatalf("expected queue_already_active for paused-by-drain; got %v", errs)
		}
	})

	t.Run("fail_active", func(t *testing.T) {
		t.Parallel()
		active := &queue.Queue{
			SchemaVersion: 1,
			QueueID:       "active-queue-id",
			Name:          "fwkeeper",
			Status:        queue.QueueStatusActive,
		}
		ledger := validFixtureOpenLedger(idA)
		errs, _, err := queue.Validate(context.Background(), submitReq(active), ledger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(errs) != 1 || errs[0].Reason != queue.ReasonQueueAlreadyActive {
			t.Fatalf("expected queue_already_active for active queue; got %v", errs)
		}
	})
}

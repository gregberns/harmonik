package queue_test

// validation_em065_hkxizhl_test.go — binding tests for the EM-065
// submit/append double-queue guard.
//
// EM-065 prevents double-queuing a bead that is already non-terminally present
// in any active queue at submit or append time. It extends QM-022 (Beads-ledger
// in_progress check) to cover the pre-claim window where a bead is still "open"
// in the Beads ledger but has already been accepted into a queue slot.
//
// Cases covered:
//   - Cross-queue dup on submit: bead X pending in queue "alpha", submit to
//     queue "beta" → ReasonBeadAlreadyDispatched.
//   - Cross-queue dup on append: bead X pending in queue "alpha", append to
//     queue "beta" → ReasonBeadAlreadyDispatched.
//   - Cross-group dup on append: bead X pending in group 0 of a queue, append
//     bead X to group 1 of the same queue → ReasonBeadAlreadyDispatched.
//   - Terminal items allowed: bead X completed/failed in another queue →
//     submit/append succeeds (terminal items are not active slots).
//   - No OtherQueues: validation without other-queue context passes (caller
//     omits cross-queue context for legacy / unit-test paths).
//
// Spec ref: specs/execution-model.md §4.14 EM-065.
// Bead ref: hk-xizhl.

import (
	"context"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/queue"
)

// em065Ledger is a minimal BeadLedger that reports all beads as open.
type em065Ledger struct{}

func (l *em065Ledger) LookupStatus(_ context.Context, _ core.BeadID) (queue.BeadStatus, error) {
	return queue.BeadStatusOpen, nil
}

func (l *em065Ledger) BlocksEdge(_ context.Context, _, _ core.BeadID) (bool, error) {
	return false, nil
}

// em065MakeQueue builds a minimal active Queue with a single stream group
// containing one item at the given status.
func em065MakeQueue(name, queueID string, beadID core.BeadID, status queue.ItemStatus) *queue.Queue {
	return &queue.Queue{
		SchemaVersion: 1,
		QueueID:       queueID,
		Name:          name,
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindStream,
				Status:     queue.GroupStatusActive,
				Items: []queue.Item{
					{BeadID: beadID, Status: status},
				},
			},
		},
	}
}

// TestValidate_EM065_CrossQueueDuplicate_Submit verifies that submitting a bead
// that is already pending in another named queue returns ReasonBeadAlreadyDispatched.
//
// Setup: queue "alpha" has bead X pending. Submit bead X to queue "beta"
// (a new, not-yet-active queue). OtherQueues carries alpha.
func TestValidate_EM065_CrossQueueDuplicate_Submit(t *testing.T) {
	t.Parallel()

	const beadX core.BeadID = "hk-xizhl-bead-x"

	alphaQ := em065MakeQueue("alpha", "qid-alpha", beadX, queue.ItemStatusPending)

	vreq := queue.ValidationRequest{
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindStream,
				Status:     queue.GroupStatusPending,
				Items:      []queue.Item{{BeadID: beadX, Status: queue.ItemStatusPending}},
			},
		},
		ActiveQueue: nil, // "beta" queue does not exist yet
		QueueName:   "beta",
		IsAppend:    false,
		OtherQueues: []*queue.Queue{alphaQ},
	}

	verrs, _, err := queue.Validate(context.Background(), vreq, &em065Ledger{})
	if err != nil {
		t.Fatalf("Validate returned unexpected error: %v", err)
	}
	if len(verrs) == 0 {
		t.Fatal("expected ReasonBeadAlreadyDispatched; got no validation errors")
	}
	if verrs[0].Reason != queue.ReasonBeadAlreadyDispatched {
		t.Errorf("got reason %q, want %q", verrs[0].Reason, queue.ReasonBeadAlreadyDispatched)
	}
	if got, ok := verrs[0].Detail["bead_id"]; !ok || got != string(beadX) {
		t.Errorf("detail bead_id = %v, want %q", got, beadX)
	}
}

// TestValidate_EM065_CrossQueueDuplicate_Append verifies that appending a bead
// already pending in another named queue returns ReasonBeadAlreadyDispatched.
//
// Setup: queue "alpha" has bead X pending. Append bead X to queue "beta".
func TestValidate_EM065_CrossQueueDuplicate_Append(t *testing.T) {
	t.Parallel()

	const beadX core.BeadID = "hk-xizhl-bead-y"

	alphaQ := em065MakeQueue("alpha", "qid-alpha-2", beadX, queue.ItemStatusPending)
	betaQ := em065MakeQueue("beta", "qid-beta-2", "hk-xizhl-other", queue.ItemStatusPending)

	vreq := queue.ValidationRequest{
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindStream,
				Status:     queue.GroupStatusPending,
				Items:      []queue.Item{{BeadID: beadX, Status: queue.ItemStatusPending}},
			},
		},
		ActiveQueue:      betaQ,
		IsAppend:         true,
		AppendGroupIndex: 0,
		OtherQueues:      []*queue.Queue{alphaQ},
	}

	verrs, _, err := queue.Validate(context.Background(), vreq, &em065Ledger{})
	if err != nil {
		t.Fatalf("Validate returned unexpected error: %v", err)
	}
	if len(verrs) == 0 {
		t.Fatal("expected ReasonBeadAlreadyDispatched; got no validation errors")
	}
	if verrs[0].Reason != queue.ReasonBeadAlreadyDispatched {
		t.Errorf("got reason %q, want %q", verrs[0].Reason, queue.ReasonBeadAlreadyDispatched)
	}
	if got, ok := verrs[0].Detail["bead_id"]; !ok || got != string(beadX) {
		t.Errorf("detail bead_id = %v, want %q", got, beadX)
	}
}

// TestValidate_EM065_CrossGroupDuplicate_Append verifies that appending a bead
// already pending in a DIFFERENT group of the SAME queue (cross-group dup)
// returns ReasonBeadAlreadyDispatched.
//
// QM-023 only guards the target group; EM-065 covers all other groups.
func TestValidate_EM065_CrossGroupDuplicate_Append(t *testing.T) {
	t.Parallel()

	const beadX core.BeadID = "hk-xizhl-bead-z"

	// Queue with two groups: group 0 has beadX pending; group 1 is the append target.
	q := &queue.Queue{
		SchemaVersion: 1,
		QueueID:       "qid-crossgroup",
		Name:          "main",
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindStream,
				Status:     queue.GroupStatusActive,
				Items:      []queue.Item{{BeadID: beadX, Status: queue.ItemStatusPending}},
			},
			{
				GroupIndex: 1,
				Kind:       queue.GroupKindStream,
				Status:     queue.GroupStatusPending,
				Items:      []queue.Item{},
			},
		},
	}

	// Append beadX to group 1; beadX is already non-terminal in group 0.
	vreq := queue.ValidationRequest{
		Groups: []queue.Group{
			{
				GroupIndex: 1,
				Kind:       queue.GroupKindStream,
				Status:     queue.GroupStatusPending,
				Items:      []queue.Item{{BeadID: beadX, Status: queue.ItemStatusPending}},
			},
		},
		ActiveQueue:      q,
		IsAppend:         true,
		AppendGroupIndex: 1,
	}

	verrs, _, err := queue.Validate(context.Background(), vreq, &em065Ledger{})
	if err != nil {
		t.Fatalf("Validate returned unexpected error: %v", err)
	}
	if len(verrs) == 0 {
		t.Fatal("expected ReasonBeadAlreadyDispatched (cross-group); got no validation errors")
	}
	if verrs[0].Reason != queue.ReasonBeadAlreadyDispatched {
		t.Errorf("got reason %q, want %q", verrs[0].Reason, queue.ReasonBeadAlreadyDispatched)
	}
	if got, ok := verrs[0].Detail["bead_id"]; !ok || got != string(beadX) {
		t.Errorf("detail bead_id = %v, want %q", got, beadX)
	}
}

// TestValidate_EM065_TerminalItems_NotBlocked verifies that completed or failed
// items in OtherQueues do NOT trigger the EM-065 guard. A terminal bead is no
// longer an active slot; it should be re-queueable.
func TestValidate_EM065_TerminalItems_NotBlocked(t *testing.T) {
	t.Parallel()

	const beadX core.BeadID = "hk-xizhl-bead-terminal"

	for _, termStatus := range []queue.ItemStatus{queue.ItemStatusCompleted, queue.ItemStatusFailed} {
		termStatus := termStatus
		t.Run(string(termStatus), func(t *testing.T) {
			t.Parallel()

			alphaQ := em065MakeQueue("alpha", "qid-alpha-term", beadX, termStatus)

			vreq := queue.ValidationRequest{
				Groups: []queue.Group{
					{
						GroupIndex: 0,
						Kind:       queue.GroupKindStream,
						Status:     queue.GroupStatusPending,
						Items:      []queue.Item{{BeadID: beadX, Status: queue.ItemStatusPending}},
					},
				},
				ActiveQueue: nil,
				QueueName:   "beta",
				IsAppend:    false,
				OtherQueues: []*queue.Queue{alphaQ},
			}

			verrs, _, err := queue.Validate(context.Background(), vreq, &em065Ledger{})
			if err != nil {
				t.Fatalf("Validate returned unexpected error: %v", err)
			}
			if len(verrs) != 0 {
				t.Errorf("expected no validation errors for terminal status %q; got %v", termStatus, verrs)
			}
		})
	}
}

// TestValidate_EM065_DispatchedInOtherQueue_Blocked verifies that a bead in
// ItemStatusDispatched (in-flight) in another queue is also blocked.
func TestValidate_EM065_DispatchedInOtherQueue_Blocked(t *testing.T) {
	t.Parallel()

	const beadX core.BeadID = "hk-xizhl-bead-dispatched"

	alphaQ := em065MakeQueue("alpha", "qid-alpha-disp", beadX, queue.ItemStatusDispatched)

	vreq := queue.ValidationRequest{
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindStream,
				Status:     queue.GroupStatusPending,
				Items:      []queue.Item{{BeadID: beadX, Status: queue.ItemStatusPending}},
			},
		},
		ActiveQueue: nil,
		QueueName:   "beta",
		IsAppend:    false,
		OtherQueues: []*queue.Queue{alphaQ},
	}

	verrs, _, err := queue.Validate(context.Background(), vreq, &em065Ledger{})
	if err != nil {
		t.Fatalf("Validate returned unexpected error: %v", err)
	}
	if len(verrs) == 0 {
		t.Fatal("expected ReasonBeadAlreadyDispatched for dispatched-in-other-queue; got no errors")
	}
	if verrs[0].Reason != queue.ReasonBeadAlreadyDispatched {
		t.Errorf("got reason %q, want %q", verrs[0].Reason, queue.ReasonBeadAlreadyDispatched)
	}
}

// TestValidate_EM065_NoOtherQueues_Submit verifies that when OtherQueues is
// nil (the caller didn't supply cross-queue context), validation still passes
// for beads that would otherwise be clean. This is the legacy/unit-test compat
// path where callers omit the optional OtherQueues field.
func TestValidate_EM065_NoOtherQueues_Submit(t *testing.T) {
	t.Parallel()

	const beadA core.BeadID = "hk-xizhl-bead-a"

	vreq := queue.ValidationRequest{
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindStream,
				Status:     queue.GroupStatusPending,
				Items:      []queue.Item{{BeadID: beadA, Status: queue.ItemStatusPending}},
			},
		},
		ActiveQueue: nil,
		QueueName:   "main",
		IsAppend:    false,
		OtherQueues: nil, // no cross-queue context supplied
	}

	verrs, _, err := queue.Validate(context.Background(), vreq, &em065Ledger{})
	if err != nil {
		t.Fatalf("Validate returned unexpected error: %v", err)
	}
	if len(verrs) != 0 {
		t.Errorf("expected no validation errors; got %v", verrs)
	}
}

// TestValidate_EM065_CrossGroupDuplicate_TargetGroupCompleted verifies that a
// bead that appears in the TARGET group as a completed item does not trigger
// the EM-065 cross-group guard. QM-023 handles non-terminal dups in the target
// group; EM-065 must not double-count the target group.
func TestValidate_EM065_CrossGroupDuplicate_TargetGroupCompleted(t *testing.T) {
	t.Parallel()

	const beadX core.BeadID = "hk-xizhl-bead-target-completed"

	// Queue: group 0 is the append target; beadX is already completed there.
	q := &queue.Queue{
		SchemaVersion: 1,
		QueueID:       "qid-target-completed",
		Name:          "main",
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindStream,
				Status:     queue.GroupStatusActive,
				Items:      []queue.Item{{BeadID: beadX, Status: queue.ItemStatusCompleted}},
			},
		},
	}

	vreq := queue.ValidationRequest{
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindStream,
				Status:     queue.GroupStatusPending,
				Items:      []queue.Item{{BeadID: beadX, Status: queue.ItemStatusPending}},
			},
		},
		ActiveQueue:      q,
		IsAppend:         true,
		AppendGroupIndex: 0,
	}

	verrs, _, err := queue.Validate(context.Background(), vreq, &em065Ledger{})
	if err != nil {
		t.Fatalf("Validate returned unexpected error: %v", err)
	}
	// EM-065 cross-group scan skips the target group; completed items in the
	// target group are not active slots. No EM-065 error expected.
	// (QM-023 non-terminal check does not block re-appending completed items.)
	if len(verrs) != 0 {
		t.Errorf("expected no validation errors for completed-in-target-group; got %v", verrs)
	}
}

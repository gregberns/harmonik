package queue

import (
	"errors"
	"testing"
	"time"
)

const pausedCompletionQueueID = "0197c454-0000-7000-8000-000000000009"

var pausedCompletionStamp = time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)

func pausedCompletionInput(outcome GroupCompletionOutcome) GroupCompletionInput {
	return GroupCompletionInput{
		ExpectedQueueID: pausedCompletionQueueID,
		Location:        GroupCompletionLocation{GroupIndex: 0, ItemIndex: 0},
		Outcome:         outcome,
		CompletedAt:     pausedCompletionStamp,
	}
}

func pausedCompletionQueue(successorCount int, statuses ...ItemStatus) Queue {
	items := make([]Item, len(statuses))
	for i, status := range statuses {
		items[i] = Item{BeadID: "hk-paused-item", Status: status}
	}
	groups := []Group{{GroupIndex: 0, Kind: GroupKindWave, Status: GroupStatusActive, Items: items}}
	for i := 1; i <= successorCount; i++ {
		groups = append(groups, Group{
			GroupIndex: i, Kind: GroupKindWave, Status: GroupStatusPending,
			Items: []Item{{BeadID: "hk-paused-successor", Status: ItemStatusPending}},
		})
	}
	return Queue{SchemaVersion: 1, QueueID: pausedCompletionQueueID, Name: QueueNameMain, Status: QueueStatusActive, Groups: groups}
}

// The disposition that exists so no other one lies. On a paused queue the
// successor correctly stays pending, and reporting that as successor-activated
// told an operator the opposite of what the queue file says.
func TestDecideGroupCompletionHoldsTheSuccessorOnAPausedQueue(t *testing.T) {
	for _, tc := range []struct {
		name  string
		pause func(*Queue) error
	}{
		{"drain", PauseQueueForDrain},
		{"restart", PauseQueueForRestart},
		// The budget pause has no transition function: the per-queue spend
		// meter assigns the status directly, so the fixture does the same.
		{"budget", func(q *Queue) error { setQueueStatus(q, QueueStatusPausedByBudget); return nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prior := pausedCompletionQueue(1, ItemStatusDispatched)
			if err := tc.pause(&prior); err != nil {
				t.Fatalf("pause: %v", err)
			}
			pausedStatus := prior.Status

			result, err := DecideGroupCompletion(prior, pausedCompletionInput(GroupCompletionOutcomeCompleted))
			if err != nil {
				t.Fatalf("decide: %v — a paused queue refused the outcome of a run it dispatched itself", err)
			}
			if err := result.Validate(); err != nil {
				t.Fatalf("validate result: %v", err)
			}
			if result.Disposition != GroupCompletionDispositionSuccessorHeld {
				t.Fatalf("disposition = %q; want %q — the successor stayed pending, so any other name misreports it",
					result.Disposition, GroupCompletionDispositionSuccessorHeld)
			}
			if result.NextQueue.Groups[0].Items[0].Status != ItemStatusCompleted {
				t.Errorf("item status = %q; want %q", result.NextQueue.Groups[0].Items[0].Status, ItemStatusCompleted)
			}
			if result.NextQueue.Groups[0].Status != GroupStatusCompleteSuccess {
				t.Errorf("group status = %q; want %q", result.NextQueue.Groups[0].Status, GroupStatusCompleteSuccess)
			}
			if result.NextQueue.Groups[1].Status != GroupStatusPending {
				t.Errorf("successor status = %q; want %q — a paused queue must not start a new group",
					result.NextQueue.Groups[1].Status, GroupStatusPending)
			}
			if result.NextQueue.Status != pausedStatus {
				t.Errorf("queue status = %q; want %q — recording an outcome must not resume the queue",
					result.NextQueue.Status, pausedStatus)
			}
		})
	}
}

// An active queue still activates its successor. The held disposition must not
// swallow the ordinary case.
func TestDecideGroupCompletionStillActivatesTheSuccessorOnAnActiveQueue(t *testing.T) {
	result, err := DecideGroupCompletion(pausedCompletionQueue(1, ItemStatusDispatched), pausedCompletionInput(GroupCompletionOutcomeCompleted))
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	if result.Disposition != GroupCompletionDispositionSuccessorActivated {
		t.Fatalf("disposition = %q; want %q", result.Disposition, GroupCompletionDispositionSuccessorActivated)
	}
	if result.NextQueue.Groups[1].Status != GroupStatusActive {
		t.Errorf("successor status = %q; want %q", result.NextQueue.Groups[1].Status, GroupStatusActive)
	}
}

// The first load-bearing consequence. A group can fail DURING a drain. Leaving
// the queue paused-by-drain would let the resume-on-start bit auto-resume it on
// the next daemon start, and the queue would then start the successor group
// PAST a group that failed. Failure outranks drain, and the bit is cleared.
func TestDecideGroupCompletionFailureOutranksARestartPause(t *testing.T) {
	prior := pausedCompletionQueue(1, ItemStatusDispatched)
	if err := PauseQueueForRestart(&prior); err != nil {
		t.Fatalf("pause for restart: %v", err)
	}
	if !prior.ResumeOnStart {
		t.Fatal("fixture: the restart pause did not set the resume bit, so this case proves nothing")
	}

	result, err := DecideGroupCompletion(prior, pausedCompletionInput(GroupCompletionOutcomeFailed))
	if err != nil {
		t.Fatalf("decide: %v — a group that failed during a drain could not park its queue", err)
	}
	if result.Disposition != GroupCompletionDispositionPausedByFailure {
		t.Fatalf("disposition = %q; want %q", result.Disposition, GroupCompletionDispositionPausedByFailure)
	}
	if result.NextQueue.Status != QueueStatusPausedByFailure {
		t.Errorf("queue status = %q; want %q — a queue left paused-by-drain auto-resumes and then runs the successor "+
			"of a failed group", result.NextQueue.Status, QueueStatusPausedByFailure)
	}
	if result.NextQueue.ResumeOnStart {
		t.Error("resume-on-start is still set — the next daemon start would resume a queue an operator has to look at")
	}
	if result.NextQueue.Groups[1].Status != GroupStatusPending {
		t.Errorf("successor status = %q; want %q", result.NextQueue.Groups[1].Status, GroupStatusPending)
	}
}

// The second load-bearing consequence. A drain that lands on the LAST group's
// last item must still complete the queue. Refusing it left all the work done,
// the canonical file never unlinked and the name never released, and the boot
// reconciliation pass re-derives the same refusal, so nothing heals it.
func TestDecideGroupCompletionCompletesTheQueueOnADrainingQueue(t *testing.T) {
	prior := pausedCompletionQueue(0, ItemStatusDispatched)
	if err := PauseQueueForRestart(&prior); err != nil {
		t.Fatalf("pause for restart: %v", err)
	}

	input := pausedCompletionInput(GroupCompletionOutcomeCompleted)
	first, err := DecideGroupCompletion(prior, input)
	if err != nil {
		t.Fatalf("decide: %v — the last item of the last group could not finish during a drain", err)
	}
	if first.Disposition != GroupCompletionDispositionReceiptRequired {
		t.Fatalf("disposition = %q; want %q", first.Disposition, GroupCompletionDispositionReceiptRequired)
	}

	input.CompletionReceiptID = "0197c454-0000-7000-8000-00000000000a"
	result, err := DecideGroupCompletion(prior, input)
	if err != nil {
		t.Fatalf("decide with receipt: %v", err)
	}
	if result.Disposition != GroupCompletionDispositionQueueCompleted {
		t.Fatalf("disposition = %q; want %q", result.Disposition, GroupCompletionDispositionQueueCompleted)
	}
	if result.NextQueue.Status != QueueStatusCompleted {
		t.Errorf("queue status = %q; want %q — the queue file is never unlinked and the name never released",
			result.NextQueue.Status, QueueStatusCompleted)
	}
	if result.NextQueue.ResumeOnStart {
		t.Error("resume-on-start is still set on a completed queue, which no longer exists to resume")
	}
}

// The gate is widened, not opened. A queue that holds no in-flight item, or
// that is gone, still refuses — a write to a released name would resurrect it.
func TestQueueAcceptsItemCompletionExcludesTheStatusesWithNothingInFlight(t *testing.T) {
	for status, want := range map[QueueStatus]bool{
		QueueStatusActive:          true,
		QueueStatusPausedByDrain:   true,
		QueueStatusPausedByBudget:  true,
		QueueStatusPausedByFailure: false,
		QueueStatusCompleted:       false,
		QueueStatusCancelled:       false,
		QueueStatus("nonsense"):    false,
	} {
		if got := queueAcceptsItemCompletion(status); got != want {
			t.Errorf("queueAcceptsItemCompletion(%q) = %v; want %v", status, got, want)
		}
	}
}

// A cancelled queue refuses the completion outright rather than reaching the
// transitions above.
func TestDecideGroupCompletionRefusesACancelledQueue(t *testing.T) {
	prior := pausedCompletionQueue(1, ItemStatusDispatched)
	setQueueStatus(&prior, QueueStatusCancelled)

	result, err := DecideGroupCompletion(prior, pausedCompletionInput(GroupCompletionOutcomeCompleted))
	if err == nil {
		t.Fatalf("decide returned %+v; want a refusal — a write to a released name resurrects it", result)
	}
	var stateErr *GroupCompletionStateError
	if !errors.As(err, &stateErr) || stateErr.Reason != GroupCompletionErrorInvalidQueueStatus {
		t.Fatalf("err = %v; want reason %q", err, GroupCompletionErrorInvalidQueueStatus)
	}
}

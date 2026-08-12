package dispatch

import (
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/queue"
)

func TestClassifyQueueObservationItemStates(t *testing.T) {
	for _, tc := range []struct {
		name      string
		phase     Phase
		status    queue.ItemStatus
		group     queue.GroupStatus
		run       bool
		refusal   ClaimRefusalCause
		wantQueue QueueFact
		wantPre   PreclaimFact
	}{
		{name: "prepared before reservation", phase: PhasePrepared, status: queue.ItemStatusPending, group: queue.GroupStatusActive, wantQueue: QueueOfferable, wantPre: PreclaimAbsent},
		{name: "exact reservation", phase: PhasePrepared, status: queue.ItemStatusDispatched, group: queue.GroupStatusActive, run: true, wantQueue: QueueReserved, wantPre: PreclaimAbsent},
		{name: "successful item in active group", phase: PhaseHandoffDurable, status: queue.ItemStatusCompleted, group: queue.GroupStatusActive, run: true, wantQueue: QueueTerminalSuccess, wantPre: PreclaimAbsent},
		{name: "failed item in durable group", phase: PhaseHandoffDurable, status: queue.ItemStatusFailed, group: queue.GroupStatusCompleteWithFailures, run: true, wantQueue: QueueTerminalUnreopened, wantPre: PreclaimAbsent},
		{name: "supported refusal released", phase: PhaseClaimRefused, refusal: ClaimRefusalSupportedNonOpen, status: queue.ItemStatusPending, group: queue.GroupStatusActive, wantQueue: QueueOfferable, wantPre: PreclaimReleased},
		{name: "dependency refusal cannot release", phase: PhaseClaimRefused, refusal: ClaimRefusalDependency, status: queue.ItemStatusPending, group: queue.GroupStatusActive, wantQueue: QueueConflict, wantPre: PreclaimConflict},
		{name: "deferred is not the bound dispatch", phase: PhasePrepared, status: queue.ItemStatusDeferredForLedgerDep, group: queue.GroupStatusActive, wantQueue: QueueConflict, wantPre: PreclaimConflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			intent := queueFactIntent(tc.phase, tc.refusal)
			snapshot := queueFactSnapshot(intent, tc.status, tc.group, tc.run)
			got, err := ClassifyQueueObservation(intent, snapshot)
			if err != nil {
				t.Fatal(err)
			}
			want := QueueObservation{Queue: tc.wantQueue, Preclaim: tc.wantPre}
			if got != want {
				t.Fatalf("observation = %+v, want %+v", got, want)
			}
		})
	}
}

func TestClassifyQueueObservationPreclaimTerminalProgress(t *testing.T) {
	for _, tc := range []struct {
		name    string
		phase   Phase
		refusal ClaimRefusalCause
		cause   queue.PreclaimTerminalCause
		group   queue.GroupStatus
		want    PreclaimFact
	}{
		{name: "max attempts item", phase: PhasePrepared, cause: queue.PreclaimTerminalMaxAttempts, group: queue.GroupStatusActive, want: PreclaimMaxAttemptsItemTerminal},
		{name: "max attempts group", phase: PhasePrepared, cause: queue.PreclaimTerminalMaxAttempts, group: queue.GroupStatusCompleteWithFailures, want: PreclaimMaxAttemptsGroupDurable},
		{name: "cross queue item", phase: PhasePrepared, cause: queue.PreclaimTerminalCrossQueue, group: queue.GroupStatusActive, want: PreclaimCrossQueueItemTerminal},
		{name: "dependency item", phase: PhaseClaimRefused, refusal: ClaimRefusalDependency, cause: queue.PreclaimTerminalDependencyRefusal, group: queue.GroupStatusActive, want: PreclaimDependencyItemTerminal},
		{name: "dependency group", phase: PhaseClaimRefused, refusal: ClaimRefusalDependency, cause: queue.PreclaimTerminalDependencyRefusal, group: queue.GroupStatusCompleteWithFailures, want: PreclaimDependencyGroupDurable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			intent := queueFactIntent(tc.phase, tc.refusal)
			snapshot := queueFactSnapshot(intent, queue.ItemStatusFailed, tc.group, tc.cause == queue.PreclaimTerminalDependencyRefusal)
			item := &snapshot.Groups[0].Items[0]
			item.PreclaimTerminal = &queue.PreclaimTerminalBinding{
				RunID: intent.Binding.RunID.String(), ClaimTransitionID: intent.Binding.ClaimTransitionID.String(), Cause: tc.cause,
			}
			got, err := ClassifyQueueObservation(intent, snapshot)
			if err != nil {
				t.Fatal(err)
			}
			want := QueueObservation{Queue: QueueTerminalUnreopened, Preclaim: tc.want}
			if got != want {
				t.Fatalf("observation = %+v, want %+v", got, want)
			}
		})
	}
}

func TestClassifyQueueObservationIdentityAndStateConflicts(t *testing.T) {
	baseIntent := queueFactIntent(PhasePrepared, "")
	mutations := []struct {
		name   string
		mutate func(*queue.Queue)
	}{
		{name: "queue id", mutate: func(q *queue.Queue) { q.QueueID = testRunID }},
		{name: "queue name", mutate: func(q *queue.Queue) { q.Name = "other" }},
		{name: "schema", mutate: func(q *queue.Queue) { q.SchemaVersion = 2 }},
		{name: "queue status", mutate: func(q *queue.Queue) { q.Status = "unknown" }},
		{name: "duplicate group", mutate: func(q *queue.Queue) { q.Groups = append(q.Groups, q.Groups[0]) }},
		{name: "wrong bead", mutate: func(q *queue.Queue) { q.Groups[0].Items[0].BeadID = "hk-other" }},
		{name: "wrong run", mutate: func(q *queue.Queue) {
			other := testQueueID
			q.Groups[0].Items[0].Status = queue.ItemStatusDispatched
			q.Groups[0].Items[0].RunID = &other
		}},
		{name: "pending group", mutate: func(q *queue.Queue) { q.Groups[0].Status = queue.GroupStatusPending }},
		{name: "unknown sibling shape", mutate: func(q *queue.Queue) {
			q.Groups = append(q.Groups, queue.Group{GroupIndex: 1, Kind: "unknown", Status: queue.GroupStatusPending})
		}},
	}
	for _, tc := range mutations {
		t.Run(tc.name, func(t *testing.T) {
			snapshot := queueFactSnapshot(baseIntent, queue.ItemStatusPending, queue.GroupStatusActive, false)
			tc.mutate(snapshot)
			got, err := ClassifyQueueObservation(baseIntent, snapshot)
			if err != nil {
				t.Fatal(err)
			}
			if want := (QueueObservation{Queue: QueueConflict, Preclaim: PreclaimConflict}); got != want {
				t.Fatalf("observation = %+v, want %+v", got, want)
			}
		})
	}
}

func TestClassifyQueueObservationRejectsCorruptAggregateState(t *testing.T) {
	intent := queueFactIntent(PhasePrepared, "")
	terminalRun := intent.Binding.RunID.String()
	mutations := []struct {
		name   string
		mutate func(*queue.Queue)
	}{
		{name: "completed queue with active group", mutate: func(q *queue.Queue) { q.Status = queue.QueueStatusCompleted }},
		{name: "cancelled queue with active group", mutate: func(q *queue.Queue) { q.Status = queue.QueueStatusCancelled }},
		{name: "failed queue status without failed group", mutate: func(q *queue.Queue) { q.Status = queue.QueueStatusPausedByFailure }},
		{name: "failed group under active queue", mutate: func(q *queue.Queue) {
			q.Groups[0].Status = queue.GroupStatusCompleteWithFailures
			q.Groups[0].Items[0] = queue.Item{BeadID: intent.Binding.BeadID, Status: queue.ItemStatusFailed}
		}},
		{name: "terminal failed group with pending sibling", mutate: func(q *queue.Queue) {
			q.Status = queue.QueueStatusPausedByFailure
			q.Groups[0].Status = queue.GroupStatusCompleteWithFailures
			q.Groups[0].Items = append(q.Groups[0].Items, queue.Item{BeadID: "hk-pending", Status: queue.ItemStatusPending})
			q.Groups[0].Items[0] = queue.Item{BeadID: intent.Binding.BeadID, Status: queue.ItemStatusFailed}
		}},
		{name: "success group with failed item", mutate: func(q *queue.Queue) {
			q.Status = queue.QueueStatusCompleted
			q.Groups[0].Status = queue.GroupStatusCompleteSuccess
			q.Groups[0].Items[0] = queue.Item{BeadID: intent.Binding.BeadID, Status: queue.ItemStatusFailed}
		}},
		{name: "failure group with no failure", mutate: func(q *queue.Queue) {
			q.Status = queue.QueueStatusPausedByFailure
			q.Groups[0].Status = queue.GroupStatusCompleteWithFailures
			q.Groups[0].Items[0] = queue.Item{BeadID: intent.Binding.BeadID, Status: queue.ItemStatusCompleted, RunID: &terminalRun}
		}},
		{name: "non-target group has unknown item", mutate: func(q *queue.Queue) {
			q.Groups = append(q.Groups, queue.Group{GroupIndex: 1, Kind: queue.GroupKindStream, Status: queue.GroupStatusPending, Items: []queue.Item{{BeadID: "hk-sibling", Status: "unknown"}}})
		}},
		{name: "non-target terminal group has pending item", mutate: func(q *queue.Queue) {
			q.Groups[0] = queue.Group{GroupIndex: 0, Kind: queue.GroupKindStream, Status: queue.GroupStatusCompleteSuccess, Items: []queue.Item{{BeadID: "hk-prior", Status: queue.ItemStatusCompleted, RunID: &terminalRun}}}
			q.Groups = append(q.Groups, queue.Group{GroupIndex: 1, Kind: queue.GroupKindStream, Status: queue.GroupStatusCompleteWithFailures, Items: []queue.Item{{BeadID: "hk-sibling", Status: queue.ItemStatusPending}}})
			q.Status = queue.QueueStatusPausedByFailure
		}},
	}
	for _, tc := range mutations {
		t.Run(tc.name, func(t *testing.T) {
			snapshot := queueFactSnapshot(intent, queue.ItemStatusPending, queue.GroupStatusActive, false)
			tc.mutate(snapshot)
			got, err := ClassifyQueueObservation(intent, snapshot)
			if err != nil {
				t.Fatal(err)
			}
			if want := (QueueObservation{Queue: QueueConflict, Preclaim: PreclaimConflict}); got != want {
				t.Fatalf("observation = %+v, want %+v", got, want)
			}
		})
	}
}

func TestClassifyQueueObservationRejectsCorruptCompletedSibling(t *testing.T) {
	intent := queueFactIntent(PhasePrepared, "")
	intent.Binding.GroupIndex = 1
	otherRun := testQueueID
	snapshot := queueFactSnapshot(intent, queue.ItemStatusPending, queue.GroupStatusActive, false)
	snapshot.Groups[0].GroupIndex = 1
	snapshot.Groups = append([]queue.Group{{
		GroupIndex: 0,
		Kind:       queue.GroupKindStream,
		Status:     queue.GroupStatusCompleteSuccess,
		Items:      []queue.Item{{BeadID: "hk-corrupt-prior", Status: queue.ItemStatusFailed, RunID: &otherRun}},
	}}, snapshot.Groups...)
	got, err := ClassifyQueueObservation(intent, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if want := (QueueObservation{Queue: QueueConflict, Preclaim: PreclaimConflict}); got != want {
		t.Fatalf("observation = %+v, want %+v", got, want)
	}
}

func TestClassifyQueueObservationAcceptsReconciledCompletedSiblingWithoutRun(t *testing.T) {
	intent := queueFactIntent(PhasePrepared, "")
	intent.Binding.GroupIndex = 1
	snapshot := queueFactSnapshot(intent, queue.ItemStatusPending, queue.GroupStatusActive, false)
	snapshot.Groups[0].GroupIndex = 1
	snapshot.Groups = append([]queue.Group{{
		GroupIndex: 0,
		Kind:       queue.GroupKindStream,
		Status:     queue.GroupStatusCompleteSuccess,
		Items:      []queue.Item{{BeadID: "hk-reconciled", Status: queue.ItemStatusCompleted}},
	}}, snapshot.Groups...)
	got, err := ClassifyQueueObservation(intent, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if want := (QueueObservation{Queue: QueueOfferable, Preclaim: PreclaimAbsent}); got != want {
		t.Fatalf("observation = %+v, want %+v", got, want)
	}
}

func TestClassifyQueueObservationRejectsPendingGroupAndDuplicateIdentityCorruption(t *testing.T) {
	intent := queueFactIntent(PhasePrepared, "")
	for _, tc := range []struct {
		name   string
		mutate func(*queue.Queue)
	}{
		{name: "pending group has dispatched item", mutate: func(q *queue.Queue) {
			runID := testQueueID
			q.Groups = append(q.Groups, queue.Group{GroupIndex: 1, Kind: queue.GroupKindStream, Status: queue.GroupStatusPending, Items: []queue.Item{{BeadID: "hk-later", Status: queue.ItemStatusDispatched, RunID: &runID}}})
		}},
		{name: "pending group has deferred item", mutate: func(q *queue.Queue) {
			q.Groups = append(q.Groups, queue.Group{GroupIndex: 1, Kind: queue.GroupKindStream, Status: queue.GroupStatusPending, Items: []queue.Item{{BeadID: "hk-later", Status: queue.ItemStatusDeferredForLedgerDep}}})
		}},
		{name: "duplicate bead", mutate: func(q *queue.Queue) {
			q.Groups[0].Items = append(q.Groups[0].Items, queue.Item{BeadID: intent.Binding.BeadID, Status: queue.ItemStatusPending})
		}},
		{name: "duplicate run", mutate: func(q *queue.Queue) {
			runID := testRunID
			q.Groups[0].Items[0].Status = queue.ItemStatusDispatched
			q.Groups[0].Items[0].RunID = &runID
			q.Groups[0].Items = append(q.Groups[0].Items, queue.Item{BeadID: "hk-other", Status: queue.ItemStatusDispatched, RunID: &runID})
		}},
		{name: "noncanonical dispatched run", mutate: func(q *queue.Queue) {
			badRunID := "not-a-run"
			q.Groups[0].Items = append(q.Groups[0].Items, queue.Item{BeadID: "hk-other", Status: queue.ItemStatusDispatched, RunID: &badRunID})
		}},
		{name: "duplicate preclaim run", mutate: func(q *queue.Queue) {
			q.Groups[0].Items = append(q.Groups[0].Items,
				preclaimQueueFactItem("hk-preclaim-one", testRunID, testTransitionID),
				preclaimQueueFactItem("hk-preclaim-two", testRunID, "0197d100-0000-7000-8000-000000000004"))
		}},
		{name: "duplicate preclaim transition", mutate: func(q *queue.Queue) {
			q.Groups[0].Items = append(q.Groups[0].Items,
				preclaimQueueFactItem("hk-preclaim-one", testRunID, testTransitionID),
				preclaimQueueFactItem("hk-preclaim-two", "0197d100-0000-7000-8000-000000000004", testTransitionID))
		}},
		{name: "preclaim run duplicates live run", mutate: func(q *queue.Queue) {
			runID := testRunID
			q.Groups[0].Items = append(q.Groups[0].Items,
				queue.Item{BeadID: "hk-live", Status: queue.ItemStatusDispatched, RunID: &runID},
				preclaimQueueFactItem("hk-preclaim", testRunID, testTransitionID))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			snapshot := queueFactSnapshot(intent, queue.ItemStatusPending, queue.GroupStatusActive, false)
			tc.mutate(snapshot)
			got, err := ClassifyQueueObservation(intent, snapshot)
			if err != nil {
				t.Fatal(err)
			}
			if want := (QueueObservation{Queue: QueueConflict, Preclaim: PreclaimConflict}); got != want {
				t.Fatalf("observation = %+v, want %+v", got, want)
			}
		})
	}
}

func preclaimQueueFactItem(beadID, runID, transitionID string) queue.Item {
	return queue.Item{
		BeadID: core.BeadID(beadID),
		Status: queue.ItemStatusFailed,
		PreclaimTerminal: &queue.PreclaimTerminalBinding{
			RunID: runID, ClaimTransitionID: transitionID, Cause: queue.PreclaimTerminalMaxAttempts,
		},
	}
}

func TestClassifyQueueObservationRejectsMismatchedPreclaimAuthority(t *testing.T) {
	intent := queueFactIntent(PhasePrepared, "")
	for _, mutate := range []func(*queue.PreclaimTerminalBinding){
		func(binding *queue.PreclaimTerminalBinding) { binding.RunID = testQueueID },
		func(binding *queue.PreclaimTerminalBinding) { binding.ClaimTransitionID = testQueueID },
		func(binding *queue.PreclaimTerminalBinding) { binding.Cause = queue.PreclaimTerminalDependencyRefusal },
	} {
		snapshot := queueFactSnapshot(intent, queue.ItemStatusFailed, queue.GroupStatusActive, false)
		binding := &queue.PreclaimTerminalBinding{RunID: testRunID, ClaimTransitionID: testTransitionID, Cause: queue.PreclaimTerminalMaxAttempts}
		mutate(binding)
		snapshot.Groups[0].Items[0].PreclaimTerminal = binding
		got, err := ClassifyQueueObservation(intent, snapshot)
		if err != nil {
			t.Fatal(err)
		}
		if got.Queue != QueueConflict || got.Preclaim != PreclaimConflict {
			t.Fatalf("observation = %+v", got)
		}
	}
}

func TestClassifyQueueObservationRejectsInvalidIntent(t *testing.T) {
	intent := queueFactIntent(PhasePrepared, "")
	intent.Binding.ItemIndex = -1
	if _, err := ClassifyQueueObservation(intent, nil); err == nil {
		t.Fatal("ClassifyQueueObservation() error = nil")
	}
}

func queueFactIntent(phase Phase, refusal ClaimRefusalCause) Intent {
	intent := testIntent(phase)
	intent.Binding.GroupIndex = 0
	intent.Binding.ItemIndex = 0
	if phase == PhaseClaimRefused {
		intent.Refusal.Cause = refusal
	}
	return intent
}

func queueFactSnapshot(intent Intent, status queue.ItemStatus, groupStatus queue.GroupStatus, withRun bool) *queue.Queue {
	item := queue.Item{BeadID: intent.Binding.BeadID, Status: status}
	if withRun {
		runID := intent.Binding.RunID.String()
		item.RunID = &runID
	}
	queueStatus := queue.QueueStatusActive
	if groupStatus == queue.GroupStatusCompleteWithFailures {
		queueStatus = queue.QueueStatusPausedByFailure
	}
	return &queue.Queue{
		SchemaVersion: 1,
		QueueID:       intent.Binding.QueueID,
		Name:          intent.Binding.QueueName,
		Status:        queueStatus,
		Groups: []queue.Group{{
			GroupIndex: 0,
			Kind:       queue.GroupKindStream,
			Status:     groupStatus,
			Items:      []queue.Item{item},
		}},
	}
}

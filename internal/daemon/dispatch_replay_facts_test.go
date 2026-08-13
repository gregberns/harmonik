package daemon

import (
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/dispatch"
	"github.com/gregberns/harmonik/internal/queue"
	runpkg "github.com/gregberns/harmonik/internal/run"
)

func TestBuildDispatchReplayFactsJoinsPreparedAuthorities(t *testing.T) {
	intent := replayOwnershipIntent(t, dispatch.PhasePrepared)
	item := queue.Item{BeadID: intent.Binding.BeadID, Status: queue.ItemStatusPending}
	snapshot := replayFactQueue(intent, item)
	bead := replayFactBead(intent.Binding.BeadID, core.CoarseStatusOpen)

	facts, err := buildDispatchReplayFacts(intent, dispatchReplayObservations{
		Queue: &snapshot, Bead: &bead,
		Session: dispatch.SessionTargetObservation{Status: dispatch.SessionTargetSessionAbsent},
		Claim:   dispatch.ClaimNone, Git: dispatch.GitAbsent,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := replayPlanPreparedFacts(intent, dispatch.QueueOfferable)
	if facts != want {
		t.Fatalf("facts = %+v, want %+v", facts, want)
	}
}

func TestBuildDispatchReplayFactsJoinsExactHandoffAuthorities(t *testing.T) {
	intent := replayOwnershipIntent(t, dispatch.PhaseHandoffDurable)
	runID := intent.Binding.RunID.String()
	item := queue.Item{BeadID: intent.Binding.BeadID, Status: queue.ItemStatusDispatched, RunID: &runID}
	snapshot := replayFactQueue(intent, item)
	bead := replayFactBead(intent.Binding.BeadID, core.CoarseStatusInProgress)
	record := replayFactRunRecord(t, intent)
	receipt, err := dispatch.NewSessionStartReceipt(intent)
	if err != nil {
		t.Fatal(err)
	}
	worktree := dispatch.WorktreeObservation{
		RunID: runID, Path: "/tmp/" + runID, Registered: true,
		LeasePresent: true, LeaseReadable: true, LeaseRunID: runID,
		LeasePID: 42, LeaseCreatedAt: "2026-08-13T01:02:03Z", LeaseTTLSec: 60,
	}
	session := dispatch.SessionTargetObservation{
		Status: dispatch.SessionTargetExact, RunID: runID,
		ClaimTransitionID: intent.Binding.ClaimTransitionID.String(),
		SessionName:       intent.Handoff.SessionName, WindowName: intent.Handoff.WindowName,
		PanePID: "42", PaneDead: "0",
	}

	facts, err := buildDispatchReplayFacts(intent, dispatchReplayObservations{
		Queue: &snapshot, Bead: &bead, RunRecord: &record,
		Worktrees: []dispatch.WorktreeObservation{worktree}, Session: session, Receipt: &receipt,
		Claim: dispatch.ClaimNone, Git: dispatch.GitAbsent,
	})
	if err != nil {
		t.Fatal(err)
	}
	if facts.Queue != dispatch.QueueReserved || facts.Bead != dispatch.BeadInProgress ||
		facts.RunRecord != dispatch.RunRecordSession || facts.Worktree != dispatch.WorktreeLeased ||
		facts.Session != dispatch.SessionLive || facts.SessionReceipt != dispatch.SessionReceiptExact {
		t.Fatalf("joined facts = %+v", facts)
	}
}

func TestBuildDispatchReplayFactsFailsClosedOnEachIdentityMismatch(t *testing.T) {
	intent := replayOwnershipIntent(t, dispatch.PhaseHandoffDurable)
	runID := intent.Binding.RunID.String()
	item := queue.Item{BeadID: intent.Binding.BeadID, Status: queue.ItemStatusDispatched, RunID: &runID}
	snapshot := replayFactQueue(intent, item)
	bead := replayFactBead(intent.Binding.BeadID, core.CoarseStatusInProgress)
	record := replayFactRunRecord(t, intent)
	receipt, err := dispatch.NewSessionStartReceipt(intent)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		mutate func(*dispatchReplayObservations)
		check  func(dispatch.ReplayFacts) bool
	}{
		{name: "queue", mutate: func(o *dispatchReplayObservations) { o.Queue.QueueID = "other" }, check: func(f dispatch.ReplayFacts) bool { return f.Queue == dispatch.QueueConflict }},
		{name: "bead", mutate: func(o *dispatchReplayObservations) { o.Bead.BeadID = "other" }, check: func(f dispatch.ReplayFacts) bool { return f.Bead == dispatch.BeadConflict }},
		{name: "run record", mutate: func(o *dispatchReplayObservations) { o.RunRecord.BeadID = "other" }, check: func(f dispatch.ReplayFacts) bool { return f.RunRecord == dispatch.RunRecordConflict }},
		{name: "receipt", mutate: func(o *dispatchReplayObservations) { o.Receipt.WindowName = "other" }, check: func(f dispatch.ReplayFacts) bool { return f.SessionReceipt == dispatch.SessionReceiptConflict }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			queueCopy := queue.CloneQueue(&snapshot)
			beadCopy := bead
			recordCopy := record
			receiptCopy := receipt
			observations := dispatchReplayObservations{
				Queue: queueCopy, Bead: &beadCopy, RunRecord: &recordCopy, Receipt: &receiptCopy,
				Session: dispatch.SessionTargetObservation{Status: dispatch.SessionTargetSessionAbsent},
				Claim:   dispatch.ClaimNone, Git: dispatch.GitAbsent,
			}
			tc.mutate(&observations)
			facts, buildErr := buildDispatchReplayFacts(intent, observations)
			if buildErr != nil {
				t.Fatal(buildErr)
			}
			if !tc.check(facts) {
				t.Fatalf("facts = %+v", facts)
			}
		})
	}
}

func TestBuildDispatchReplayFactsPreservesExplicitTerminalFacts(t *testing.T) {
	intent := replayOwnershipIntent(t, dispatch.PhaseHandoffDurable)
	runID := intent.Binding.RunID.String()
	item := queue.Item{BeadID: intent.Binding.BeadID, Status: queue.ItemStatusCompleted, RunID: &runID}
	snapshot := replayFactQueue(intent, item)
	snapshot.Groups[0].Status = queue.GroupStatusCompleteSuccess
	snapshot.Status = queue.QueueStatusCompleted
	bead := replayFactBead(intent.Binding.BeadID, core.CoarseStatusClosed)
	record := replayFactRunRecord(t, intent)
	receipt, err := dispatch.NewSessionStartReceipt(intent)
	if err != nil {
		t.Fatal(err)
	}
	facts, err := buildDispatchReplayFacts(intent, dispatchReplayObservations{
		Queue: &snapshot, Bead: &bead, RunRecord: &record, Receipt: &receipt,
		Session: dispatch.SessionTargetObservation{Status: dispatch.SessionTargetSessionAbsent},
		Claim:   dispatch.ClaimMatching, Git: dispatch.GitMatching, RunOutcome: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if facts.Claim != dispatch.ClaimMatching || facts.Git != dispatch.GitMatching || !facts.RunOutcomeDurable {
		t.Fatalf("explicit terminal facts = %+v", facts)
	}
}

func TestBuildDispatchReplayFactsJoinsClaimRefusalPreclaim(t *testing.T) {
	intent := replayOwnershipIntent(t, dispatch.PhaseClaimRefused)
	runID := intent.Binding.RunID.String()
	item := queue.Item{
		BeadID: intent.Binding.BeadID, Status: queue.ItemStatusFailed, RunID: &runID,
		PreclaimTerminal: &queue.PreclaimTerminalBinding{
			RunID: runID, ClaimTransitionID: intent.Binding.ClaimTransitionID.String(),
			Cause: queue.PreclaimTerminalDependencyRefusal,
		},
	}
	snapshot := replayFactQueue(intent, item)
	bead := replayFactBead(intent.Binding.BeadID, core.CoarseStatusOpen)
	facts, err := buildDispatchReplayFacts(intent, dispatchReplayObservations{
		Queue: &snapshot, Bead: &bead,
		Session: dispatch.SessionTargetObservation{Status: dispatch.SessionTargetSessionAbsent},
		Claim:   dispatch.ClaimNone, Git: dispatch.GitAbsent,
	})
	if err != nil {
		t.Fatal(err)
	}
	if facts.RefusalCause != dispatch.ClaimRefusalDependency || facts.Preclaim != dispatch.PreclaimDependencyItemTerminal {
		t.Fatalf("claim-refused facts = %+v", facts)
	}
}

func replayFactQueue(intent dispatch.Intent, item queue.Item) queue.Queue {
	return queue.Queue{
		SchemaVersion: 1, QueueID: intent.Binding.QueueID, Name: intent.Binding.QueueName,
		Status: queue.QueueStatusActive,
		Groups: []queue.Group{{GroupIndex: intent.Binding.GroupIndex, Kind: queue.GroupKindWave, Status: queue.GroupStatusActive, Items: []queue.Item{item}}},
	}
}

func replayFactBead(id core.BeadID, status core.CoarseStatus) core.BeadRecord {
	return core.BeadRecord{BeadID: id, Title: "replay", BeadType: "task", Status: status, AuditTrailRef: "audit"}
}

func replayFactRunRecord(t *testing.T, intent dispatch.Intent) runpkg.DispatchRecord {
	t.Helper()
	record, err := runpkg.NewDispatchRecord(intent.Binding, time.Date(2026, 8, 13, 1, 2, 3, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	location := runpkg.ExecutionLocation{Kind: runpkg.ExecutionLocalIndependent}
	record, err = record.BindLocation(location)
	if err != nil {
		t.Fatal(err)
	}
	record, err = record.BindSession(intent.Handoff.SessionName, intent.Handoff.WindowName)
	if err != nil {
		t.Fatal(err)
	}
	return record
}

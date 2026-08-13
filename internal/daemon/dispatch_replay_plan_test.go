package daemon

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/dispatch"
)

type replayPlanFactReader struct {
	facts  map[string]dispatch.ReplayFacts
	err    map[string]error
	reads  []string
	mutate func(*dispatch.Intent)
}

func (r *replayPlanFactReader) ReadDispatchReplayFacts(
	_ context.Context,
	intent dispatch.Intent,
) (dispatch.ReplayFacts, error) {
	runID := intent.Binding.RunID.String()
	r.reads = append(r.reads, runID)
	if r.mutate != nil {
		r.mutate(&intent)
	}
	return r.facts[runID], r.err[runID]
}

func TestPlanDispatchReplayReadsAllFactsBeforeReturningActions(t *testing.T) {
	first := replayOwnershipIntent(t, dispatch.PhasePrepared)
	second := replayOwnershipIntent(t, dispatch.PhasePrepared)
	second.Binding.RunID = mustReplayRunID(t, "0197d100-0000-7000-8000-000000000041")
	second.Binding.ClaimTransitionID = mustReplayTransitionID(t, "0197d100-0000-7000-8000-000000000042")
	second.Binding.ItemIndex = 1
	second.Binding.BeadID = "hk-replay-second"

	reader := &replayPlanFactReader{facts: map[string]dispatch.ReplayFacts{
		first.Binding.RunID.String():  replayPlanPreparedFacts(first, dispatch.QueueOfferable),
		second.Binding.RunID.String(): replayPlanPreparedFacts(second, dispatch.QueueReserved),
	}}
	steps, err := planDispatchReplay(t.Context(), []dispatch.Intent{second, first}, reader)
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 2 {
		t.Fatalf("steps = %d, want 2", len(steps))
	}
	if steps[0].Intent.Binding.RunID != first.Binding.RunID || steps[0].Action != dispatch.ReplayReservation {
		t.Fatalf("first step = %+v", steps[0])
	}
	if steps[1].Intent.Binding.RunID != second.Binding.RunID || steps[1].Action != dispatch.ReplayClaim {
		t.Fatalf("second step = %+v", steps[1])
	}
	if got := strings.Join(reader.reads, ","); got != first.Binding.RunID.String()+","+second.Binding.RunID.String() {
		t.Fatalf("fact read order = %q", got)
	}
}

func TestPlanDispatchReplayReturnsNoActionsWhenAnyIntentNeedsRepair(t *testing.T) {
	first := replayOwnershipIntent(t, dispatch.PhasePrepared)
	second := replayOwnershipIntent(t, dispatch.PhasePrepared)
	second.Binding.RunID = mustReplayRunID(t, "0197d100-0000-7000-8000-000000000041")
	second.Binding.ClaimTransitionID = mustReplayTransitionID(t, "0197d100-0000-7000-8000-000000000042")
	second.Binding.ItemIndex = 1
	second.Binding.BeadID = "hk-replay-second"
	third := replayOwnershipIntent(t, dispatch.PhasePrepared)
	third.Binding.RunID = mustReplayRunID(t, "0197d100-0000-7000-8000-000000000051")
	third.Binding.ClaimTransitionID = mustReplayTransitionID(t, "0197d100-0000-7000-8000-000000000052")
	third.Binding.ItemIndex = 2
	third.Binding.BeadID = "hk-replay-third"

	reader := &replayPlanFactReader{facts: map[string]dispatch.ReplayFacts{
		first.Binding.RunID.String():  replayPlanPreparedFacts(first, dispatch.QueueOfferable),
		second.Binding.RunID.String(): replayPlanPreparedFacts(second, dispatch.QueueConflict),
		third.Binding.RunID.String():  replayPlanPreparedFacts(third, dispatch.QueueOfferable),
	}}
	steps, err := planDispatchReplay(t.Context(), []dispatch.Intent{first, second, third}, reader)
	if err == nil || !strings.Contains(err.Error(), "requires repair") {
		t.Fatalf("planDispatchReplay() error = %v", err)
	}
	if steps != nil {
		t.Fatalf("steps = %+v, want nil", steps)
	}
	if len(reader.reads) != 3 {
		t.Fatalf("fact reads = %v, want every intent read", reader.reads)
	}
}

func TestPlanDispatchReplayDetachesIntentPointers(t *testing.T) {
	intent := replayOwnershipIntent(t, dispatch.PhaseHandoffDurable)
	wantRunID := intent.Run.RecordRunID
	wantSession := intent.Handoff.SessionName
	reader := &replayPlanFactReader{facts: map[string]dispatch.ReplayFacts{
		intent.Binding.RunID.String(): {
			IntentPhase: intent.Phase,
			Queue:       dispatch.QueueReserved, Bead: dispatch.BeadInProgress,
			RunRecord: dispatch.RunRecordSession, Worktree: dispatch.WorktreeLeased,
			Session: dispatch.SessionAbsent, SessionReceipt: dispatch.SessionReceiptAbsent,
			Git: dispatch.GitAbsent, Claim: dispatch.ClaimNone, Preclaim: dispatch.PreclaimAbsent,
		},
	}, mutate: func(got *dispatch.Intent) {
		got.Run.RecordRunID = mustReplayRunID(t, "0197d100-0000-7000-8000-000000000071")
		got.Handoff.SessionName = "reader-changed"
	}}
	steps, err := planDispatchReplay(t.Context(), []dispatch.Intent{intent}, reader)
	if err != nil {
		t.Fatal(err)
	}
	if intent.Run.RecordRunID != wantRunID || intent.Handoff.SessionName != wantSession {
		t.Fatal("fact reader mutated caller-owned intent pointers")
	}
	if steps[0].Intent.Run.RecordRunID != wantRunID || steps[0].Intent.Handoff.SessionName != wantSession {
		t.Fatal("plan step contains fact-reader pointer mutations")
	}
	intent.Run.RecordRunID = mustReplayRunID(t, "0197d100-0000-7000-8000-000000000061")
	intent.Handoff.SessionName = "changed"
	if steps[0].Intent.Run.RecordRunID == intent.Run.RecordRunID || steps[0].Intent.Handoff.SessionName == intent.Handoff.SessionName {
		t.Fatal("plan step aliases input intent pointers")
	}
}

func TestPlanDispatchReplayDetachesRefusalPointer(t *testing.T) {
	intent := replayOwnershipIntent(t, dispatch.PhaseClaimRefused)
	wantCause := intent.Refusal.Cause
	facts := replayPlanPreparedFacts(intent, dispatch.QueueReserved)
	facts.RefusalCause = wantCause
	reader := &replayPlanFactReader{
		facts: map[string]dispatch.ReplayFacts{intent.Binding.RunID.String(): facts},
		mutate: func(got *dispatch.Intent) {
			got.Refusal.Cause = dispatch.ClaimRefusalSupportedNonOpen
		},
	}
	steps, err := planDispatchReplay(t.Context(), []dispatch.Intent{intent}, reader)
	if err != nil {
		t.Fatal(err)
	}
	if intent.Refusal.Cause != wantCause || steps[0].Intent.Refusal.Cause != wantCause {
		t.Fatal("refusal pointer was not detached from fact reader")
	}
}

func TestPlanDispatchReplayRejectsFactsForDifferentIntentPhase(t *testing.T) {
	intent := replayOwnershipIntent(t, dispatch.PhaseHandoffDurable)
	facts := replayPlanPreparedFacts(intent, dispatch.QueueOfferable)
	facts.IntentPhase = dispatch.PhasePrepared
	reader := &replayPlanFactReader{facts: map[string]dispatch.ReplayFacts{intent.Binding.RunID.String(): facts}}
	if steps, err := planDispatchReplay(t.Context(), []dispatch.Intent{intent}, reader); err == nil || steps != nil {
		t.Fatalf("planDispatchReplay() = (%+v, %v), want refused mismatch", steps, err)
	}
}

func TestPlanDispatchReplayRejectsFactsForDifferentRefusalCause(t *testing.T) {
	intent := replayOwnershipIntent(t, dispatch.PhaseClaimRefused)
	facts := replayPlanPreparedFacts(intent, dispatch.QueueReserved)
	facts.Bead = dispatch.BeadOpen
	facts.RefusalCause = dispatch.ClaimRefusalSupportedNonOpen
	reader := &replayPlanFactReader{facts: map[string]dispatch.ReplayFacts{intent.Binding.RunID.String(): facts}}
	if steps, err := planDispatchReplay(t.Context(), []dispatch.Intent{intent}, reader); err == nil || steps != nil {
		t.Fatalf("planDispatchReplay() = (%+v, %v), want refused mismatch", steps, err)
	}
}

func TestPlanDispatchReplayPropagatesFactReadFailure(t *testing.T) {
	first := replayOwnershipIntent(t, dispatch.PhasePrepared)
	second := replayOwnershipIntent(t, dispatch.PhasePrepared)
	second.Binding.RunID = mustReplayRunID(t, "0197d100-0000-7000-8000-000000000041")
	second.Binding.ClaimTransitionID = mustReplayTransitionID(t, "0197d100-0000-7000-8000-000000000042")
	second.Binding.ItemIndex = 1
	second.Binding.BeadID = "hk-replay-second"
	third := replayOwnershipIntent(t, dispatch.PhasePrepared)
	third.Binding.RunID = mustReplayRunID(t, "0197d100-0000-7000-8000-000000000051")
	third.Binding.ClaimTransitionID = mustReplayTransitionID(t, "0197d100-0000-7000-8000-000000000052")
	third.Binding.ItemIndex = 2
	third.Binding.BeadID = "hk-replay-third"
	diagnostic := errors.New("ledger unavailable")
	reader := &replayPlanFactReader{
		facts: map[string]dispatch.ReplayFacts{
			first.Binding.RunID.String(): replayPlanPreparedFacts(first, dispatch.QueueOfferable),
			third.Binding.RunID.String(): replayPlanPreparedFacts(third, dispatch.QueueOfferable),
		},
		err: map[string]error{second.Binding.RunID.String(): diagnostic},
	}
	steps, err := planDispatchReplay(t.Context(), []dispatch.Intent{first, second, third}, reader)
	if !errors.Is(err, diagnostic) {
		t.Fatalf("planDispatchReplay() error = %v", err)
	}
	if steps != nil || len(reader.reads) != 3 {
		t.Fatalf("planDispatchReplay() = (%+v, %v reads), want nil plan after all reads", steps, reader.reads)
	}
}

func replayPlanPreparedFacts(intent dispatch.Intent, queueFact dispatch.QueueFact) dispatch.ReplayFacts {
	return dispatch.ReplayFacts{
		IntentPhase: intent.Phase,
		Queue:       queueFact, Bead: dispatch.BeadOpen,
		RunRecord: dispatch.RunRecordAbsent, Worktree: dispatch.WorktreeAbsent,
		Session: dispatch.SessionAbsent, SessionReceipt: dispatch.SessionReceiptAbsent,
		Git: dispatch.GitAbsent, Claim: dispatch.ClaimNone, Preclaim: dispatch.PreclaimAbsent,
	}
}

func mustReplayRunID(t *testing.T, value string) core.RunID {
	t.Helper()
	return core.RunID(uuid.MustParse(value))
}

func mustReplayTransitionID(t *testing.T, value string) core.TransitionID {
	t.Helper()
	return core.TransitionID(uuid.MustParse(value))
}

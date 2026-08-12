package run

import (
	"testing"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/dispatch"
)

func TestClassifyDispatchRecordPhases(t *testing.T) {
	base := testDispatchRecord()
	located := base
	located.Location = &ExecutionLocation{Kind: ExecutionLocalIndependent}
	session := located
	session.SessionName = "harmonik-run-test"
	for _, tc := range []struct {
		name   string
		intent dispatch.Intent
		record *DispatchRecord
		want   dispatch.RunRecordFact
	}{
		{name: "absent", intent: runFactIntent(t, dispatch.PhaseClaimDurable), want: dispatch.RunRecordAbsent},
		{name: "base", intent: runFactIntent(t, dispatch.PhaseClaimDurable), record: &base, want: dispatch.RunRecordBase},
		{name: "located", intent: runFactIntent(t, dispatch.PhaseRunDurable), record: &located, want: dispatch.RunRecordLocated},
		{name: "session before intent phase", intent: runFactIntent(t, dispatch.PhaseRunDurable), record: &session, want: dispatch.RunRecordSession},
		{name: "bound session", intent: runFactIntent(t, dispatch.PhaseHandoffDurable), record: &session, want: dispatch.RunRecordSession},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyDispatchRecord(tc.intent, tc.record); got != tc.want {
				t.Fatalf("fact = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestClassifyDispatchRecordRejectsEveryBindingMismatch(t *testing.T) {
	intent := runFactIntent(t, dispatch.PhaseClaimDurable)
	mutations := []func(*DispatchRecord){
		func(record *DispatchRecord) {
			record.RunID = core.RunID(uuid.MustParse("0197d200-0000-7000-8000-000000000004"))
		},
		func(record *DispatchRecord) { record.BeadID = "hk-other" },
		func(record *DispatchRecord) { record.QueueName = "other" },
		func(record *DispatchRecord) { record.QueueID = "0197d200-0000-7000-8000-000000000004" },
		func(record *DispatchRecord) { record.GroupIndex++ },
		func(record *DispatchRecord) { record.ItemIndex++ },
		func(record *DispatchRecord) {
			record.ClaimTransitionID = core.TransitionID(uuid.MustParse("0197d200-0000-7000-8000-000000000004"))
		},
	}
	for index, mutate := range mutations {
		record := testDispatchRecord()
		mutate(&record)
		if got := ClassifyDispatchRecord(intent, &record); got != dispatch.RunRecordConflict {
			t.Fatalf("mismatch %d = %q, want conflict", index, got)
		}
	}
}

func TestClassifyDispatchRecordRejectsInvalidAndMismatchedSession(t *testing.T) {
	intent := runFactIntent(t, dispatch.PhaseHandoffDurable)
	record := testDispatchRecord()
	record.Location = &ExecutionLocation{Kind: ExecutionLocalIndependent}
	record.SessionName = "other-session"
	if got := ClassifyDispatchRecord(intent, &record); got != dispatch.RunRecordConflict {
		t.Fatalf("mismatched session = %q", got)
	}
	record = testDispatchRecord()
	record.SchemaVersion = 1
	if got := ClassifyDispatchRecord(runFactIntent(t, dispatch.PhaseClaimDurable), &record); got != dispatch.RunRecordConflict {
		t.Fatalf("invalid record = %q", got)
	}
}

func runFactIntent(t *testing.T, phase dispatch.Phase) dispatch.Intent {
	t.Helper()
	base := testDispatchRecord()
	intent, err := dispatch.NewPrepared(dispatch.Binding{
		QueueID: base.QueueID, QueueName: base.QueueName, GroupIndex: base.GroupIndex, ItemIndex: base.ItemIndex,
		BeadID: base.BeadID, RunID: base.RunID, ClaimTransitionID: base.ClaimTransitionID,
	})
	if err != nil {
		t.Fatal(err)
	}
	switch phase {
	case dispatch.PhasePrepared:
		return intent
	case dispatch.PhaseClaimRefused:
		t.Fatalf("run fact fixture does not support phase %q", phase)
	case dispatch.PhaseClaimDurable:
		intent, err = intent.WithClaimDurable()
	case dispatch.PhaseRunDurable:
		intent, err = intent.WithClaimDurable()
		if err == nil {
			intent, err = intent.WithRunDurable()
		}
	case dispatch.PhaseHandoffDurable:
		intent, err = intent.WithClaimDurable()
		if err == nil {
			intent, err = intent.WithRunDurable()
		}
		if err == nil {
			intent, err = intent.WithHandoffDurable("harmonik-run-test")
		}
	}
	if err != nil {
		t.Fatal(err)
	}
	return intent
}

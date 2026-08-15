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
	location := testLocalLocation()
	located.Location = &location
	session := located
	session.SessionName = "harmonik-run-test"
	session.WindowName = "run-test"
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
		func(record *DispatchRecord) { record.ParentCommit = "abcdef0123456789abcdef0123456789abcdef01" },
		func(record *DispatchRecord) { record.RepositoryPath = "/srv/harmonik/other" },
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
	location := testLocalLocation()
	record.Location = &location
	record.SessionName = "other-session"
	record.WindowName = "run-test"
	if got := ClassifyDispatchRecord(intent, &record); got != dispatch.RunRecordConflict {
		t.Fatalf("mismatched session = %q", got)
	}
	record.SessionName = "harmonik-run-test"
	record.WindowName = "other-window"
	if got := ClassifyDispatchRecord(intent, &record); got != dispatch.RunRecordConflict {
		t.Fatalf("mismatched window = %q", got)
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
		ParentCommit: base.ParentCommit, RepositoryPath: base.RepositoryPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	switch phase {
	case dispatch.PhasePrepared:
		return intent
	case dispatch.PhaseClaimRefused:
		intent, err = intent.WithClaimRefused(dispatch.ClaimRefusalDependency)
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
			intent, err = intent.WithHandoffDurable("harmonik-run-test", "run-test")
		}
	}
	if err != nil {
		t.Fatal(err)
	}
	return intent
}

func TestClassifySessionStartReceipt(t *testing.T) {
	intent := runFactIntent(t, dispatch.PhaseHandoffDurable)
	record := testDispatchRecord()
	location := testLocalLocation()
	record.Location = &location
	record.SessionName = intent.Handoff.SessionName
	record.WindowName = intent.Handoff.WindowName
	receipt, err := dispatch.NewSessionStartReceipt(intent)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		receipt *dispatch.SessionStartReceipt
		want    dispatch.SessionReceiptFact
	}{
		{name: "absent", want: dispatch.SessionReceiptAbsent},
		{name: "exact", receipt: &receipt, want: dispatch.SessionReceiptExact},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifySessionStartReceipt(intent, &record, tc.receipt); got != tc.want {
				t.Fatalf("fact = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestClassifySessionStartReceiptRejectsEveryAuthorityMismatch(t *testing.T) {
	intent := runFactIntent(t, dispatch.PhaseHandoffDurable)
	record := testDispatchRecord()
	location := testLocalLocation()
	record.Location = &location
	record.SessionName = intent.Handoff.SessionName
	record.WindowName = intent.Handoff.WindowName
	receipt, err := dispatch.NewSessionStartReceipt(intent)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		mutate func(*dispatch.Intent, *DispatchRecord, *dispatch.SessionStartReceipt)
	}{
		{name: "invalid intent", mutate: func(i *dispatch.Intent, _ *DispatchRecord, _ *dispatch.SessionStartReceipt) { i.Binding.BeadID = "" }},
		{name: "missing record", mutate: func(_ *dispatch.Intent, r *DispatchRecord, _ *dispatch.SessionStartReceipt) { *r = DispatchRecord{} }},
		{name: "record binding", mutate: func(_ *dispatch.Intent, r *DispatchRecord, _ *dispatch.SessionStartReceipt) { r.BeadID = "hk-other" }},
		{name: "record session", mutate: func(_ *dispatch.Intent, r *DispatchRecord, _ *dispatch.SessionStartReceipt) { r.SessionName = "other" }},
		{name: "record window", mutate: func(_ *dispatch.Intent, r *DispatchRecord, _ *dispatch.SessionStartReceipt) { r.WindowName = "other" }},
		{name: "receipt binding", mutate: func(_ *dispatch.Intent, _ *DispatchRecord, r *dispatch.SessionStartReceipt) {
			r.Binding.BeadID = "hk-other"
		}},
		{name: "receipt session", mutate: func(_ *dispatch.Intent, _ *DispatchRecord, r *dispatch.SessionStartReceipt) { r.SessionName = "other" }},
		{name: "receipt window", mutate: func(_ *dispatch.Intent, _ *DispatchRecord, r *dispatch.SessionStartReceipt) { r.WindowName = "other" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			candidateIntent, candidateRecord, candidateReceipt := intent, record, receipt
			tc.mutate(&candidateIntent, &candidateRecord, &candidateReceipt)
			if got := ClassifySessionStartReceipt(candidateIntent, &candidateRecord, &candidateReceipt); got != dispatch.SessionReceiptConflict {
				t.Fatalf("fact = %q, want conflict", got)
			}
		})
	}
}

func TestClassifySessionStartReceiptRejectsReceiptBeforeHandoff(t *testing.T) {
	handoff := runFactIntent(t, dispatch.PhaseHandoffDurable)
	receipt, err := dispatch.NewSessionStartReceipt(handoff)
	if err != nil {
		t.Fatal(err)
	}
	for _, phase := range []dispatch.Phase{
		dispatch.PhasePrepared,
		dispatch.PhaseClaimDurable,
		dispatch.PhaseClaimRefused,
		dispatch.PhaseRunDurable,
	} {
		t.Run(string(phase), func(t *testing.T) {
			intent := runFactIntent(t, phase)
			if got := ClassifySessionStartReceipt(intent, nil, &receipt); got != dispatch.SessionReceiptConflict {
				t.Fatalf("present fact = %q, want conflict", got)
			}
		})
	}
}

func TestClassifySessionStartReceiptAbsenceDoesNotRequireRunRecord(t *testing.T) {
	for _, phase := range []dispatch.Phase{
		dispatch.PhasePrepared,
		dispatch.PhaseClaimDurable,
		dispatch.PhaseClaimRefused,
		dispatch.PhaseRunDurable,
		dispatch.PhaseHandoffDurable,
	} {
		t.Run(string(phase), func(t *testing.T) {
			intent := runFactIntent(t, phase)
			if got := ClassifySessionStartReceipt(intent, nil, nil); got != dispatch.SessionReceiptAbsent {
				t.Fatalf("absent fact = %q, want absent", got)
			}
		})
	}
}

func TestClassifySessionStartReceiptPresentHandoffRequiresRunRecord(t *testing.T) {
	intent := runFactIntent(t, dispatch.PhaseHandoffDurable)
	receipt, err := dispatch.NewSessionStartReceipt(intent)
	if err != nil {
		t.Fatal(err)
	}
	if got := ClassifySessionStartReceipt(intent, nil, &receipt); got != dispatch.SessionReceiptConflict {
		t.Fatalf("fact = %q, want conflict", got)
	}
}

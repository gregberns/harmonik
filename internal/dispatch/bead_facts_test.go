package dispatch

import (
	"testing"

	"github.com/gregberns/harmonik/internal/core"
)

func TestClassifyBeadRecordStatuses(t *testing.T) {
	for _, tc := range []struct {
		status core.CoarseStatus
		want   BeadFact
	}{
		{status: core.CoarseStatusOpen, want: BeadOpen},
		{status: core.CoarseStatusInProgress, want: BeadInProgress},
		{status: core.CoarseStatusClosed, want: BeadClosed},
		{status: core.CoarseStatusTombstone, want: BeadClosed},
		{status: core.CoarseStatusBlocked, want: BeadOther},
		{status: core.CoarseStatusDeferred, want: BeadOther},
		{status: core.CoarseStatusDraft, want: BeadOther},
		{status: core.CoarseStatusPinned, want: BeadOther},
	} {
		record := beadFactRecord(tc.status)
		if got := ClassifyBeadRecord(queueFactIntent(PhasePrepared, ""), &record); got != tc.want {
			t.Fatalf("status %q = %q, want %q", tc.status, got, tc.want)
		}
	}
}

func TestClassifyBeadRecordConflicts(t *testing.T) {
	intent := queueFactIntent(PhasePrepared, "")
	valid := beadFactRecord(core.CoarseStatusOpen)
	for _, record := range []*core.BeadRecord{
		nil,
		{},
		func() *core.BeadRecord { changed := valid; changed.BeadID = "hk-other"; return &changed }(),
		func() *core.BeadRecord { changed := valid; changed.Status = "future"; return &changed }(),
	} {
		if got := ClassifyBeadRecord(intent, record); got != BeadConflict {
			t.Fatalf("record %+v = %q, want conflict", record, got)
		}
	}
	intent.Binding.ItemIndex = -1
	if got := ClassifyBeadRecord(intent, &valid); got != BeadConflict {
		t.Fatalf("invalid intent = %q, want conflict", got)
	}
}

func beadFactRecord(status core.CoarseStatus) core.BeadRecord {
	return core.BeadRecord{
		BeadID: "hk-dispatch", Title: "dispatch", BeadType: "task", Status: status, AuditTrailRef: "audit",
	}
}

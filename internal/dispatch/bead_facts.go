package dispatch

import "github.com/gregberns/harmonik/internal/core"

// ClassifyBeadRecord reduces one exact ledger record to the replay vocabulary.
func ClassifyBeadRecord(intent Intent, record *core.BeadRecord) BeadFact {
	if intent.Validate() != nil || record == nil || !record.Valid() || record.BeadID != intent.Binding.BeadID {
		return BeadConflict
	}
	switch record.Status {
	case core.CoarseStatusOpen:
		return BeadOpen
	case core.CoarseStatusInProgress:
		return BeadInProgress
	case core.CoarseStatusClosed, core.CoarseStatusTombstone:
		return BeadClosed
	case core.CoarseStatusBlocked, core.CoarseStatusDeferred, core.CoarseStatusDraft, core.CoarseStatusPinned:
		return BeadOther
	default:
		return BeadConflict
	}
}

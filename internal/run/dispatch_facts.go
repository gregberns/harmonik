package run

import "github.com/gregberns/harmonik/internal/dispatch"

// ClassifyDispatchRecord validates one universal record against its intent.
func ClassifyDispatchRecord(intent dispatch.Intent, record *DispatchRecord) dispatch.RunRecordFact {
	if intent.Validate() != nil {
		return dispatch.RunRecordConflict
	}
	if record == nil {
		return dispatch.RunRecordAbsent
	}
	if record.Validate() != nil || !dispatchRecordMatchesIntent(*record, intent) {
		return dispatch.RunRecordConflict
	}
	if record.SessionName != "" {
		if intent.Phase == dispatch.PhaseHandoffDurable &&
			(record.SessionName != intent.Handoff.SessionName || record.WindowName != intent.Handoff.WindowName) {
			return dispatch.RunRecordConflict
		}
		return dispatch.RunRecordSession
	}
	if record.Location != nil {
		return dispatch.RunRecordLocated
	}
	return dispatch.RunRecordBase
}

func dispatchRecordMatchesIntent(record DispatchRecord, intent dispatch.Intent) bool {
	binding := intent.Binding
	return record.RunID == binding.RunID && record.BeadID == binding.BeadID &&
		record.QueueName == binding.QueueName && record.QueueID == binding.QueueID &&
		record.GroupIndex == binding.GroupIndex && record.ItemIndex == binding.ItemIndex &&
		record.ClaimTransitionID == binding.ClaimTransitionID
}

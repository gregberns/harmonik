package core

import "github.com/google/uuid"

// BudgetExhaustedPayload is the payload of the reconciliation_budget_exhausted
// event (reconciliation/schemas.md §6.1 RECORD BudgetExhaustedPayload, RC-018).
//
// The event is emitted when the reconciliation workflow's wall-clock budget is
// exhausted before the investigator produces a verdict. The daemon terminates
// the workflow and emits this payload to give operators and tooling the
// budgeting context needed to tune the budget ceiling.
//
// # Structural invariants (enforced by Valid)
//
//   - RunID is not the zero RunID (the reconciliation workflow's run_id).
//   - WorkflowID is not the zero WorkflowID (the workflow definition id).
//   - BudgetSeconds is >= 0 (declared wall-clock budget ceiling in seconds).
//   - ElapsedSeconds is >= 0 (actual elapsed when terminated, in seconds).
type BudgetExhaustedPayload struct {
	// RunID is the run_id of the reconciliation workflow whose budget was
	// exhausted. Must not be the zero RunID.
	RunID RunID

	// WorkflowID is the workflow definition id. Must not be the zero WorkflowID.
	WorkflowID WorkflowID

	// BudgetSeconds is the declared wall-clock budget ceiling in seconds
	// (reconciliation/schemas.md §6.1 budget_seconds). Must be >= 0.
	BudgetSeconds int64

	// ElapsedSeconds is the actual elapsed wall-clock seconds at the point
	// the workflow was terminated (reconciliation/schemas.md §6.1
	// elapsed_seconds). Must be >= 0.
	ElapsedSeconds int64
}

// Valid reports whether all structural invariants of the BudgetExhaustedPayload
// are satisfied.
//
// Rules per reconciliation/schemas.md §6.1:
//   - RunID is not the zero RunID.
//   - WorkflowID is not the zero WorkflowID.
//   - BudgetSeconds is >= 0.
//   - ElapsedSeconds is >= 0.
func (b BudgetExhaustedPayload) Valid() bool {
	if b.RunID == RunID(uuid.Nil) {
		return false
	}
	if b.WorkflowID == WorkflowID(uuid.Nil) {
		return false
	}
	if b.BudgetSeconds < 0 {
		return false
	}
	if b.ElapsedSeconds < 0 {
		return false
	}
	return true
}

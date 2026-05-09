package core

// WorkflowClass is the typed enum for the optional workflow_class tag on a
// Workflow record (reconciliation/schemas.md §6.5, execution-model.md §6.1).
//
// Absence of the field (nil *WorkflowClass on the Workflow record) means an
// ordinary, unclassed workflow. A non-nil value MUST be one of the declared
// constants; Valid() reports false for any other value.
//
// # MVH constraint
//
// At MVH exactly one value is accepted: WorkflowClassReconciliation.
// Additional classes (improvement-loop, operator-cli-handler) are reserved for
// future use per §6.5 "Future enum growth"; they are NOT declared here.
type WorkflowClass string

// WorkflowClassReconciliation is the only accepted WorkflowClass value at MVH.
// It flags the workflow as an RC-category investigator or auto-resolver dispatch
// (reconciliation/schemas.md §6.5, execution-model.md §4.5.EM-026):
//   - Subject to RC-002: exactly one checkpoint commit per reconciliation run.
//   - Subject to RC-002a: at most one per target_run_id.
//   - Subject to RC-INV-001: uniqueness audit sensor.
const WorkflowClassReconciliation WorkflowClass = "reconciliation"

// Valid reports whether wc is a declared WorkflowClass constant.
//
// At MVH the only accepted value is WorkflowClassReconciliation. Any other
// value — including the empty string — returns false per §4.9.EM-038.
func (wc WorkflowClass) Valid() bool {
	return wc == WorkflowClassReconciliation
}

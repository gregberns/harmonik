package core

// Evidence is the typed wrapper for the evidence map carried on a Transition
// record (execution-model.md §6.1, line 704: evidence : Map<String, Any>).
//
// The map is open: any string key is permitted. Callers MUST use the exported
// EvidenceKey* constants for the reserved keys to avoid typo collisions.
//
// # Reserved keys
//
// Two keys are reserved by the spec:
//   - EvidenceKeySubWorkflowPin (§4.8.EM-034c): sub-workflow expansion pin.
//   - EvidenceKeySynthesizedOutcome (§4.5.EM-023a): set to true for
//     daemon/reconciliation synthesized outcomes.
//
// # EM-021 externalization
//
// Large payloads MUST be externalized as sibling files under the canonical
// evidence directory:
//
//	.harmonik/transitions/<run_id>/<transition_id>/evidence/*
//
// Use EvidenceExternalDir to construct this path. Externalized files are part
// of the commit's tree and inherit the atomicity boundary of §4.4.EM-016.
// Writing them outside the tree is non-conforming. The primary
// <transition_id>.json SHOULD remain single-digit KB; externalized files are
// referenced from this map by relative path (execution-model.md §4.4.EM-021).
type Evidence map[string]any

const (
	// EvidenceKeySubWorkflowPin is the reserved evidence key for the
	// sub-workflow expansion pin per execution-model.md §4.8.EM-034c.
	EvidenceKeySubWorkflowPin = "sub_workflow_pin"

	// EvidenceKeySynthesizedOutcome is the reserved evidence key set to true
	// for daemon/reconciliation synthesized outcomes per
	// execution-model.md §4.5.EM-023a.
	EvidenceKeySynthesizedOutcome = "synthesized_outcome"
)

// Valid reports whether the Evidence map is structurally valid.
//
// The spec places no constraint on which keys are present or how many entries
// the map contains; arbitrary keys are permitted. A nil map is accepted (the
// spec does not require non-nil). Valid returns true in all cases.
func (e Evidence) Valid() bool {
	return true
}

package core

// Evidence is the typed wrapper for the evidence map carried on a Transition
// record (execution-model.md §6.1, line 704: evidence : Map<String, Any>).
//
// The map is open: any string key is permitted. Callers MUST use the exported
// EvidenceKey* constants for the reserved keys to avoid typo collisions.
//
// # Reserved keys
//
// Three keys are reserved by the spec:
//   - EvidenceKeySubWorkflowPin (§4.8.EM-034c): sub-workflow expansion pin.
//   - EvidenceKeySynthesizedOutcome (§4.5.EM-023a): set to true for
//     daemon/reconciliation synthesized outcomes.
//   - EvidenceKeyPartialSuccess (§4.5.EM-023a): set to true when outcome
//     status is PARTIAL_SUCCESS, so downstream consumers distinguish partial
//     from full success.
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
	//
	// Value shape: SubWorkflowExpansionPin (a struct with sub_workflow_ref,
	// sub_workflow_version, resolved_workflow_id).
	//
	// # Durability protocol (EM-034c)
	//
	// This key MUST appear in the Evidence map of the Transition record written
	// on the sub-workflow entry checkpoint commit — the checkpoint whose state
	// transitions the run from the sub-workflow node to its expanded
	// start_node_id. The entry checkpoint is a sub_workflow_entered transition
	// record. The pin inherits the atomicity boundary of §4.4.EM-016: the
	// Transition record file and the checkpoint commit are written as a single
	// git write-tree / commit-tree / update-ref unit.
	//
	// On restart, the daemon MUST reconstruct the pinned expansion by reading
	// this key from the most recent sub_workflow_entered transition record on
	// the run's task branch. The daemon MUST NOT re-consult the sub-workflow
	// registry. Registry updates between crash and restart cannot alter the
	// run's expansion.
	//
	// For nested expansions, each sub-workflow entry checkpoint carries its own
	// sub_workflow_pin; the outer expansion is reconstructed by walking the
	// checkpoint trail in commit order.
	EvidenceKeySubWorkflowPin = "sub_workflow_pin"

	// EvidenceKeySynthesizedOutcome is the reserved evidence key set to true
	// for daemon/reconciliation synthesized outcomes per
	// execution-model.md §4.5.EM-023a.
	EvidenceKeySynthesizedOutcome = "synthesized_outcome"

	// EvidenceKeyPartialSuccess is the reserved evidence key set to true on
	// Transition records whose associated outcome has status PARTIAL_SUCCESS.
	// Required by execution-model.md §4.5.EM-023a: "the Transition record MUST
	// carry a partial_success=true evidence flag so downstream consumers can
	// distinguish partial from full success."
	EvidenceKeyPartialSuccess = "partial_success"
)

// Valid reports whether the Evidence map is structurally valid.
//
// The spec places no constraint on which keys are present or how many entries
// the map contains; arbitrary keys are permitted. A nil map is accepted (the
// spec does not require non-nil). Valid returns true in all cases.
func (e Evidence) Valid() bool {
	return true
}

// SetGateVerdict inserts verdict into the Evidence map keyed by
// verdict.GateName, satisfying specs/control-points.md §4.8.CP-040.
//
// The Transition record's Evidence MUST carry the GateVerdictRecord under
// the gate_name key BEFORE the transition advances. Callers MUST call this
// method and write the resulting Evidence into the Transition record before
// issuing the checkpoint commit.
//
// If e is nil, a new map is allocated. The updated map is returned; the
// caller MUST use the return value (maps are reference types but nil
// initialisation requires allocation).
func (e Evidence) SetGateVerdict(verdict GateVerdictRecord) Evidence {
	if e == nil {
		e = make(Evidence)
	}
	e[verdict.GateName] = verdict
	return e
}

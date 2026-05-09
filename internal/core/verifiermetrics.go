package core

// VerifierMetrics is the typed wrapper for the verifier_metrics map carried on
// a Transition record (execution-model.md §6.1, line 705:
// verifier_metrics : Map<String, Any> -- structured).
//
// The map is open: any string key is permitted. No reserved keys are cited by
// the spec at this version.
//
// # EM-021 externalization
//
// Like Evidence, large verifier_metrics payloads MUST be externalized as
// sibling files under the canonical evidence directory:
//
//	.harmonik/transitions/<run_id>/<transition_id>/evidence/*
//
// Use EvidenceExternalDir to construct this path. Externalized files are part
// of the commit's tree and inherit the atomicity boundary of §4.4.EM-016.
// Writing them outside the tree is non-conforming. The primary
// <transition_id>.json SHOULD remain single-digit KB; externalized files are
// referenced from this map by relative path (execution-model.md §4.4.EM-021).
type VerifierMetrics map[string]any

// Valid reports whether the VerifierMetrics map is structurally valid.
//
// The spec places no constraint on which keys are present or how many entries
// the map contains; arbitrary keys are permitted. A nil map is accepted (the
// spec does not require non-nil). Valid returns true in all cases.
func (v VerifierMetrics) Valid() bool {
	return true
}

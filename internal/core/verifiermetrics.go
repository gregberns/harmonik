package core

// VerifierMetrics is the typed wrapper for the verifier_metrics map carried on
// a Transition record (execution-model.md §6.1, line 705:
// verifier_metrics : Map<String, Any> -- structured).
//
// The map is open: any string key is permitted. No reserved keys are cited by
// the spec at this version.
type VerifierMetrics map[string]any

// Valid reports whether the VerifierMetrics map is structurally valid.
//
// The spec places no constraint on which keys are present or how many entries
// the map contains; arbitrary keys are permitted. A nil map is accepted (the
// spec does not require non-nil). Valid returns true in all cases.
func (v VerifierMetrics) Valid() bool {
	return true
}

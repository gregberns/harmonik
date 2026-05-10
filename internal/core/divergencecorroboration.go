package core

import "fmt"

// DivergenceCorroboration is the typed enum for the corroboration field on a
// store_divergence_detected event payload per [event-model.md §8.6.8] and
// EV-023a [event-model.md §4.5].
//
// Detectors MUST classify every candidate divergence observation into one of
// the two corroborated values. A single-source observation whose corroboration
// cannot be established MUST NOT produce a store_divergence_detected event;
// instead it MUST emit divergence_inconclusive (event-model.md §8.6.10) with
// reason=no_authority_reference or reason=authority_unavailable per EV-023a.
//
// Cat 6b emissions (mechanically unrecoverable integrity violations) are exempt
// from this constraint via the dedicated escalation path of
// [reconciliation/spec.md §8.11a].
//
// Spec ref: event-model.md §4.5 EV-023a; event-model.md §8.6.8
// store_divergence_detected payload; reconciliation/spec.md §4.3 RC-019a.
//
// Tags: mechanism
type DivergenceCorroboration string

// DivergenceCorroboration values per event-model.md §8.6.8 and EV-023a.
const (
	// DivergenceCorroborationGitCorroborated indicates the divergence evidence
	// carries a commit hash or git reference that the detector tested against
	// the git DAG. Both git and at least one other store confirm the divergence.
	//
	// Spec ref: event-model.md §4.5 EV-023a — "An event is git-corroborated
	// when its payload carries a commit_hash (or other git reference) that the
	// detector can test against the git DAG."
	DivergenceCorroborationGitCorroborated DivergenceCorroboration = "git-corroborated"

	// DivergenceCorroborationBeadsCorroborated indicates the divergence evidence
	// carries a bead_id that the detector queried against Beads to confirm the
	// divergence.
	//
	// Spec ref: event-model.md §4.5 EV-023a — "it is beads-corroborated when
	// its payload carries a bead_id the detector can query against Beads."
	DivergenceCorroborationBeadsCorroborated DivergenceCorroboration = "beads-corroborated"
)

// Valid reports whether c is one of the two declared DivergenceCorroboration
// constants. Inconclusive observations MUST emit divergence_inconclusive per
// EV-023a and MUST NOT carry a corroboration field; only corroborated
// observations (git or Beads) are valid values for store_divergence_detected.
func (c DivergenceCorroboration) Valid() bool {
	switch c {
	case DivergenceCorroborationGitCorroborated,
		DivergenceCorroborationBeadsCorroborated:
		return true
	default:
		return false
	}
}

// MarshalText implements encoding.TextMarshaler so DivergenceCorroboration
// serialises correctly in JSON and YAML.
// It rejects any value that is not one of the two declared constants.
func (c DivergenceCorroboration) MarshalText() ([]byte, error) {
	if !c.Valid() {
		return nil, fmt.Errorf("divergencecorroboration: unknown value %q", string(c))
	}
	return []byte(c), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
// It rejects any value that is not one of the two declared constants.
// Per EV-023a, inconclusive observations MUST be routed to divergence_inconclusive,
// NOT stored as a DivergenceCorroboration value.
func (c *DivergenceCorroboration) UnmarshalText(text []byte) error {
	v := DivergenceCorroboration(text)
	if !v.Valid() {
		return fmt.Errorf(
			"divergencecorroboration: unknown value %q;"+
				" must be one of git-corroborated, beads-corroborated",
			string(text),
		)
	}
	*c = v
	return nil
}

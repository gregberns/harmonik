package core

import "fmt"

// OutcomeStatus is the result status of a workflow node outcome (execution-model.md §6.1).
// One of: SUCCESS, FAIL, RETRY, PARTIAL_SUCCESS.
// Drives the durability decision of §4.5.EM-023a.
type OutcomeStatus string

// OutcomeStatus values per execution-model.md §6.1 ENUM declaration.
// Values are UPPERCASE with underscores per the spec's convention for this type.
const (
	OutcomeStatusSuccess        OutcomeStatus = "SUCCESS"
	OutcomeStatusFail           OutcomeStatus = "FAIL"
	OutcomeStatusRetry          OutcomeStatus = "RETRY"
	OutcomeStatusPartialSuccess OutcomeStatus = "PARTIAL_SUCCESS"
)

// Valid reports whether s is one of the four declared OutcomeStatus constants.
// This is the predicate hook for EM-023a: the durability decision procedure
// keys on outcome_status and rejects any value not in the declared set.
func (s OutcomeStatus) Valid() bool {
	switch s {
	case OutcomeStatusSuccess, OutcomeStatusFail, OutcomeStatusRetry, OutcomeStatusPartialSuccess:
		return true
	default:
		return false
	}
}

// MarshalText implements encoding.TextMarshaler so OutcomeStatus serialises
// correctly in JSON and YAML workflow definitions.
func (s OutcomeStatus) MarshalText() ([]byte, error) {
	if !s.Valid() {
		return nil, fmt.Errorf("outcomestatus: unknown value %q", string(s))
	}
	return []byte(s), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
// It rejects any value that is not one of the four declared constants,
// satisfying the EM-023a requirement that unknown statuses are rejected.
func (s *OutcomeStatus) UnmarshalText(text []byte) error {
	v := OutcomeStatus(text)
	if !v.Valid() {
		return fmt.Errorf("outcomestatus: unknown value %q; must be one of SUCCESS, FAIL, RETRY, PARTIAL_SUCCESS", string(text))
	}
	*s = v
	return nil
}

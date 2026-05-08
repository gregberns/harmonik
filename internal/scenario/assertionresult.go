package scenario

import "fmt"

// AssertionResultKind is the kind discriminator for AssertionResult.
//
// Note: this enum is closely related to but NOT identical to
// EventExpectationKind / WorkspacePredicateKind: AssertionResult.AssertionKind
// represents the COURSE of evaluation (one row per declared assertion across
// the union), so its enum unifies event-direction (event_present|event_absent)
// with workspace_state and exit_code. Per spec.
type AssertionResultKind string

// Declared AssertionResultKind constants per specs/scenario-harness.md §6.1.
const (
	AssertionResultKindEventPresent   AssertionResultKind = "event_present"
	AssertionResultKindEventAbsent    AssertionResultKind = "event_absent"
	AssertionResultKindWorkspaceState AssertionResultKind = "workspace_state"
	AssertionResultKindExitCode       AssertionResultKind = "exit_code"
)

// Valid reports whether k is one of the four declared AssertionResultKind constants.
func (k AssertionResultKind) Valid() bool {
	switch k {
	case AssertionResultKindEventPresent,
		AssertionResultKindEventAbsent,
		AssertionResultKindWorkspaceState,
		AssertionResultKindExitCode:
		return true
	default:
		return false
	}
}

// MarshalText implements encoding.TextMarshaler so AssertionResultKind
// serialises correctly in JSON and YAML.
func (k AssertionResultKind) MarshalText() ([]byte, error) {
	if !k.Valid() {
		return nil, fmt.Errorf("assertionresultkind: unknown value %q", string(k))
	}
	return []byte(k), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
// It rejects any value not in the declared set.
func (k *AssertionResultKind) UnmarshalText(text []byte) error {
	v := AssertionResultKind(text)
	if !v.Valid() {
		return fmt.Errorf("assertionresultkind: unknown value %q; must be one of event_present, event_absent, workspace_state, exit_code", string(text))
	}
	*k = v
	return nil
}

// AssertionResult is the observed outcome of evaluating a single declared
// assertion against the captured scenario observables. One AssertionResult is
// produced per declared assertion (no short-circuit per SH-023).
//
// ActualValue and ExpectedValue are the Go realization of JSONValue: any value
// produced by encoding/json decode (nil, bool, float64, string, []any,
// map[string]any). Both fields accept nil (JSON null) as a valid value; no
// further constraint is imposed.
//
// Spec ref: specs/scenario-harness.md §6.1.
type AssertionResult struct {
	AssertionKind AssertionResultKind `json:"assertion_kind" yaml:"assertion_kind"`
	Description   string              `json:"description" yaml:"description"`
	ActualValue   any                 `json:"actual_value" yaml:"actual_value"`
	ExpectedValue any                 `json:"expected_value" yaml:"expected_value"`
	Passed        bool                `json:"passed" yaml:"passed"`
}

// Valid reports whether the AssertionResult is structurally well-formed:
//   - AssertionKind is one of the four declared constants.
//   - Description is non-empty.
//   - ActualValue and ExpectedValue may be any JSON-representable value
//     (including nil for JSON null); no further constraint.
func (a AssertionResult) Valid() bool {
	return a.AssertionKind.Valid() && a.Description != ""
}

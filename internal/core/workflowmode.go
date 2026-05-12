package core

import "fmt"

// WorkflowMode selects the dispatch shape for a Run
// (execution-model.md §6.1 ENUM WorkflowMode; §4.3.EM-012).
//
// The value is resolved once at claim time per §4.3.EM-012a and sealed into
// the Run record; it is immutable for the lifetime of the run.
type WorkflowMode string

// Declared WorkflowMode constants per execution-model.md §6.1.
const (
	// WorkflowModeSingle is the one-handler-per-node default (Core MVH).
	// Applies to ordinary workflow graphs.
	WorkflowModeSingle WorkflowMode = "single"

	// WorkflowModeReviewLoop is the hardcoded two-node implementer→reviewer
	// cycle per §4.3.EM-015d; iteration cap of 3 per §4.3.EM-015e.
	WorkflowModeReviewLoop WorkflowMode = "review-loop"

	// WorkflowModeDot is the general workflow-graph walker; reserved for
	// post-MVH. Out of scope for Core MVH conformance.
	WorkflowModeDot WorkflowMode = "dot"
)

// ErrUnknownWorkflowMode is returned by WorkflowMode validation and
// unmarshal when an unknown mode string is encountered.
type ErrUnknownWorkflowMode struct {
	Value string
}

func (e ErrUnknownWorkflowMode) Error() string {
	return fmt.Sprintf(
		"workflowmode: unknown value %q; must be one of single, review-loop, dot",
		e.Value,
	)
}

// Valid reports whether m is one of the declared WorkflowMode constants.
// An empty string or any unrecognised value returns false.
func (m WorkflowMode) Valid() bool {
	switch m {
	case WorkflowModeSingle, WorkflowModeReviewLoop, WorkflowModeDot:
		return true
	default:
		return false
	}
}

// MarshalText implements encoding.TextMarshaler so WorkflowMode serialises
// correctly in JSON and YAML.
// It rejects any value that is not one of the declared constants.
func (m WorkflowMode) MarshalText() ([]byte, error) {
	if !m.Valid() {
		return nil, ErrUnknownWorkflowMode{Value: string(m)}
	}
	return []byte(m), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
// It rejects any value that is not one of the declared constants.
// Per execution-model.md §4.3.EM-012a, unknown-mode labels treat tier 1 as
// absent and emit bead_label_conflict; callers MUST NOT silently degrade.
func (m *WorkflowMode) UnmarshalText(text []byte) error {
	v := WorkflowMode(text)
	if !v.Valid() {
		return ErrUnknownWorkflowMode{Value: string(text)}
	}
	*m = v
	return nil
}

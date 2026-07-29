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
	// WorkflowModeSingle is the one-handler-per-node default (Core).
	// Applies to ordinary workflow graphs.
	WorkflowModeSingle WorkflowMode = "single"

	// WorkflowModeDot is the general workflow-graph walker and the default
	// (§4.3.EM-012a tier 4, resolving the embedded standard-bead.dot).
	WorkflowModeDot WorkflowMode = "dot"
)

// WorkflowModeRetiredReviewLoop is the retired "review-loop" mode string
// (execution-model.md §4.3.EM-015d, RETIRED v0.10.0). It is NOT a WorkflowMode
// constant and MUST NOT be accepted by Valid: the review-loop driver was a
// hand-written particular of the graph the dot walker already executes, and it
// is gone.
//
// The literal survives only so the surfaces that must recognise a stale value
// can name it instead of hard-coding a string. Per BI-009a a bead still
// carrying `workflow:review-loop` is treated as an unknown mode — tier 1
// absent, bead_label_conflict emitted, resolution continues to dot — while per
// PL-004a a project config naming it MUST fail at load with an actionable
// message. The asymmetry is deliberate: config is operator-authored and read
// once at boot, whereas a bead label is queue data that must not wedge.
const WorkflowModeRetiredReviewLoop = "review-loop"

// ErrUnknownWorkflowMode is returned by WorkflowMode validation and
// unmarshal when an unknown mode string is encountered.
type ErrUnknownWorkflowMode struct {
	Value string
}

func (e ErrUnknownWorkflowMode) Error() string {
	if e.Value == WorkflowModeRetiredReviewLoop {
		return "workflowmode: \"review-loop\" was retired (execution-model.md §4.3.EM-015d); " +
			"use \"dot\", the general workflow-graph walker it was a hand-written special case of"
	}
	return fmt.Sprintf(
		"workflowmode: unknown value %q; must be one of single, dot",
		e.Value,
	)
}

// Valid reports whether m is one of the declared WorkflowMode constants.
// An empty string or any unrecognised value returns false.
func (m WorkflowMode) Valid() bool {
	switch m {
	case WorkflowModeSingle, WorkflowModeDot:
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
// It rejects any value that is not one of the declared constants, with ONE
// deliberate exception: the retired "review-loop" value decodes successfully.
//
// The split is intentional. Decoding is how we READ history — event logs on
// disk, replayed JSONL corpora, Run records written before the retirement — and
// those bytes are facts about what already happened. A forensic reader that
// refuses to parse them does not prevent anything; it just goes blind, silently,
// on exactly the runs an operator is most likely to be investigating.
//
// Selection stays strict, and does not rely on this method: Valid() still
// returns false for "review-loop", so a decoded value cannot pass Run.Valid()
// or reach a dispatch path; and the three surfaces that must fail hard on a
// retired value — the project config (PL-004a), the daemon bootconfig, and the
// `harmonik run --workflow-mode` flag — each compare the RAW STRING against
// WorkflowModeRetiredReviewLoop before any of this runs, so they reject it with
// an actionable message rather than depending on a decode error.
//
// Per execution-model.md §4.3.EM-012a, an unknown-mode bead label treats tier 1
// as absent and emits bead_label_conflict; callers MUST NOT silently degrade.
func (m *WorkflowMode) UnmarshalText(text []byte) error {
	v := WorkflowMode(text)
	if !v.Valid() && string(text) != WorkflowModeRetiredReviewLoop {
		return ErrUnknownWorkflowMode{Value: string(text)}
	}
	*m = v
	return nil
}

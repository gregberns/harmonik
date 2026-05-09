package scenario

import "github.com/gregberns/harmonik/internal/core"

// OutcomeExpectation is a declared expectation about the run-level terminal
// outcome status emitted by the orchestrator at the end of a scenario run.
//
// The outcome_status field matches the `outcome_status` field of the
// run_completed / run_failed event payload per [event-model.md §8.1.8] (EV-8.1.8).
// Its semantic value corresponds to [execution-model.md §4.1 EM-005]
// Outcome.status carried through to the terminal emission.
//
// Spec ref: specs/scenario-harness.md §6.1 — RECORD OutcomeExpectation.
type OutcomeExpectation struct {
	// OutcomeStatus is the expected run-level outcome of the scenario.
	// Must be one of the four declared core.OutcomeStatus constants:
	// SUCCESS, FAIL, RETRY, PARTIAL_SUCCESS.
	// Matches the run_completed / run_failed payload's outcome_status field
	// per event-model.md §8.1.8 (EV-8.1.8) and execution-model.md §4.1 EM-005.
	OutcomeStatus core.OutcomeStatus `json:"outcome_status" yaml:"outcome_status"`

	// Description is an operator-facing label for the assertion.
	// Required (non-empty).
	Description string `json:"description" yaml:"description"`
}

// Valid reports whether the OutcomeExpectation is structurally well-formed:
//   - OutcomeStatus is one of the four declared core.OutcomeStatus constants
//     (SUCCESS, FAIL, RETRY, PARTIAL_SUCCESS).
//   - Description is non-empty.
func (o OutcomeExpectation) Valid() bool {
	return o.OutcomeStatus.Valid() && o.Description != ""
}

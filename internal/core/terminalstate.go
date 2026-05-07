// Package core holds shared types that cross subsystem boundaries.
// internal/core imports nothing from internal/* subsystems.
package core

// TerminalStateKind classifies the terminal state of a run per EM-015c
// (execution-model.md §4.3.EM-015c).
//
// EM-015c: a run reaches terminal state when (a) its current node_id is in
// the workflow's terminal_node_ids list AND its last outcome.status ∈
// {SUCCESS, PARTIAL_SUCCESS} — terminating as completed; OR (b) the classifier
// (§8) produces a terminal failure verdict — terminating as failed; OR (c) an
// operator stop --immediate signal is observed per operator-nfr.md §4.3 —
// terminating as canceled.
type TerminalStateKind int

// TerminalStateKind values per EM-015c (a), (b), (c).
const (
	TerminalStateNonTerminal TerminalStateKind = iota // run is still in flight
	TerminalStateCompleted                            // EM-015c (a): terminal node + success/partial-success
	TerminalStateFailed                               // EM-015c (b): classifier terminal verdict
	TerminalStateCanceled                             // EM-015c (c): operator stop --immediate
)

// String returns a stable lowercase name for diagnostics.
func (k TerminalStateKind) String() string {
	switch k {
	case TerminalStateNonTerminal:
		return "non-terminal"
	case TerminalStateCompleted:
		return "completed"
	case TerminalStateFailed:
		return "failed"
	case TerminalStateCanceled:
		return "canceled"
	default:
		return "unknown"
	}
}

// LastOutcome enumerates the outcome of the most recent state advance.
type LastOutcome int

// LastOutcome values enumerate the possible outcomes of the most recent state advance.
const (
	LastOutcomeNone           LastOutcome = iota // no outcome yet (run hasn't advanced)
	LastOutcomeSuccess                           // outcome.status = SUCCESS
	LastOutcomePartialSuccess                    // outcome.status = PARTIAL_SUCCESS
	LastOutcomeFailure                           // any non-success — feeds the classifier (b) path
)

// TerminalStateInput bundles the classifier inputs for ClassifyTerminalState.
// All fields are value types; the function is pure — no I/O, no clock, no state.
type TerminalStateInput struct {
	CurrentNodeID             NodeID
	TerminalNodeIDs           []NodeID // declared by the workflow (execution-model.md §6.1)
	LastOutcome               LastOutcome
	ClassifierVerdictTerminal bool // true iff failure-classifier (§8) returned a terminal verdict
	OperatorStopImmediate     bool // true iff operator stop --immediate observed (operator-nfr.md §4.3)
}

// ClassifyTerminalState returns the terminal-state classification per EM-015c
// (execution-model.md §4.3.EM-015c).
//
// Precedence (per EM-015c (a) ∨ (b) ∨ (c)):
//  1. OperatorStopImmediate         → TerminalStateCanceled
//  2. ClassifierVerdictTerminal     → TerminalStateFailed
//  3. CurrentNodeID ∈ TerminalNodeIDs AND LastOutcome ∈ {Success, PartialSuccess} → TerminalStateCompleted
//  4. otherwise                     → TerminalStateNonTerminal
//
// Note on precedence ordering: the spec uses ∨ (OR) without an ordering, but
// the implementer chose canceled > failed > completed because operator stop
// MUST take effect even if the run also happened to reach a terminal node;
// classifier failure MUST take effect even if the workflow happened to be at
// a terminal node. This precedence is documented and tested.
//
// This function is pure: it performs no I/O, does not read the clock, and
// carries no internal state. It is safe to call from multiple goroutines.
func ClassifyTerminalState(in TerminalStateInput) TerminalStateKind {
	// Condition (c): operator stop --immediate wins over all other conditions.
	if in.OperatorStopImmediate {
		return TerminalStateCanceled
	}

	// Condition (b): classifier terminal verdict wins over condition (a).
	if in.ClassifierVerdictTerminal {
		return TerminalStateFailed
	}

	// Condition (a): terminal node + success/partial-success outcome.
	if (in.LastOutcome == LastOutcomeSuccess || in.LastOutcome == LastOutcomePartialSuccess) &&
		containsNodeID(in.TerminalNodeIDs, in.CurrentNodeID) {
		return TerminalStateCompleted
	}

	return TerminalStateNonTerminal
}

// containsNodeID reports whether id is in the ids slice.
func containsNodeID(ids []NodeID, id NodeID) bool {
	for _, t := range ids {
		if t == id {
			return true
		}
	}
	return false
}

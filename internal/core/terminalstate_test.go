package core

import "testing"

// TestClassifyTerminalState_EM015c_OperatorStopImmediateWins verifies EM-015c (c):
// operator stop --immediate produces TerminalStateCanceled even when the run is
// simultaneously at a terminal node with a success outcome and a classifier verdict.
// EM-015c: "operator stop --immediate signal is observed — terminating as canceled."
func TestClassifyTerminalState_EM015c_OperatorStopImmediateWins(t *testing.T) {
	t.Parallel()

	in := TerminalStateInput{
		CurrentNodeID:             "node-end",
		TerminalNodeIDs:           []NodeID{"node-end"},
		LastOutcome:               LastOutcomeSuccess,
		ClassifierVerdictTerminal: true,
		OperatorStopImmediate:     true,
	}
	got := ClassifyTerminalState(in)
	if got != TerminalStateCanceled {
		t.Errorf("EM-015c: expected TerminalStateCanceled, got %s", got)
	}
}

// TestClassifyTerminalState_EM015c_ClassifierFailedWins verifies EM-015c (b):
// a classifier terminal verdict produces TerminalStateFailed even when the run
// is simultaneously at a terminal node with a success outcome.
// EM-015c: "classifier (§8) produces a terminal failure verdict — terminating as failed."
func TestClassifyTerminalState_EM015c_ClassifierFailedWins(t *testing.T) {
	t.Parallel()

	in := TerminalStateInput{
		CurrentNodeID:             "node-end",
		TerminalNodeIDs:           []NodeID{"node-end"},
		LastOutcome:               LastOutcomeSuccess,
		ClassifierVerdictTerminal: true,
		OperatorStopImmediate:     false,
	}
	got := ClassifyTerminalState(in)
	if got != TerminalStateFailed {
		t.Errorf("EM-015c: expected TerminalStateFailed, got %s", got)
	}
}

// TestClassifyTerminalState_EM015c_TerminalNodeWithSuccessIsCompleted verifies
// EM-015c (a): when the current node is in terminal_node_ids and the last outcome
// is SUCCESS, the run terminates as completed.
// EM-015c: "current node_id ∈ terminal_node_ids AND outcome.status ∈ {SUCCESS, PARTIAL_SUCCESS} — terminating as completed."
func TestClassifyTerminalState_EM015c_TerminalNodeWithSuccessIsCompleted(t *testing.T) {
	t.Parallel()

	in := TerminalStateInput{
		CurrentNodeID:             "node-end",
		TerminalNodeIDs:           []NodeID{"node-end"},
		LastOutcome:               LastOutcomeSuccess,
		ClassifierVerdictTerminal: false,
		OperatorStopImmediate:     false,
	}
	got := ClassifyTerminalState(in)
	if got != TerminalStateCompleted {
		t.Errorf("EM-015c: expected TerminalStateCompleted, got %s", got)
	}
}

// TestClassifyTerminalState_EM015c_TerminalNodeWithPartialSuccessIsCompleted verifies
// EM-015c (a): when the current node is in terminal_node_ids and the last outcome
// is PARTIAL_SUCCESS, the run terminates as completed.
// EM-015c: "outcome.status ∈ {SUCCESS, PARTIAL_SUCCESS} — terminating as completed."
func TestClassifyTerminalState_EM015c_TerminalNodeWithPartialSuccessIsCompleted(t *testing.T) {
	t.Parallel()

	in := TerminalStateInput{
		CurrentNodeID:             "node-end",
		TerminalNodeIDs:           []NodeID{"node-end"},
		LastOutcome:               LastOutcomePartialSuccess,
		ClassifierVerdictTerminal: false,
		OperatorStopImmediate:     false,
	}
	got := ClassifyTerminalState(in)
	if got != TerminalStateCompleted {
		t.Errorf("EM-015c: expected TerminalStateCompleted, got %s", got)
	}
}

// TestClassifyTerminalState_EM015c_TerminalNodeWithFailureIsNotCompleted verifies
// that EM-015c (a) does NOT fire when the current node is in terminal_node_ids
// but the last outcome is Failure. The (a) condition requires outcome ∈
// {SUCCESS, PARTIAL_SUCCESS}; a failure outcome leaves the run non-terminal
// pending classifier (b) invocation.
// EM-015c: "outcome.status ∈ {SUCCESS, PARTIAL_SUCCESS}" is a required conjunction.
func TestClassifyTerminalState_EM015c_TerminalNodeWithFailureIsNotCompleted(t *testing.T) {
	t.Parallel()

	in := TerminalStateInput{
		CurrentNodeID:             "node-end",
		TerminalNodeIDs:           []NodeID{"node-end"},
		LastOutcome:               LastOutcomeFailure,
		ClassifierVerdictTerminal: false,
		OperatorStopImmediate:     false,
	}
	got := ClassifyTerminalState(in)
	if got != TerminalStateNonTerminal {
		t.Errorf("EM-015c: expected TerminalStateNonTerminal, got %s", got)
	}
}

// TestClassifyTerminalState_EM015c_NonTerminalNodeIsNotCompleted verifies that
// EM-015c (a) does NOT fire when the current node is not in terminal_node_ids,
// even with a success outcome and no classifier/operator signals.
// EM-015c: "current node_id ∈ terminal_node_ids" is a required conjunction.
func TestClassifyTerminalState_EM015c_NonTerminalNodeIsNotCompleted(t *testing.T) {
	t.Parallel()

	in := TerminalStateInput{
		CurrentNodeID:             "node-middle",
		TerminalNodeIDs:           []NodeID{"node-end"},
		LastOutcome:               LastOutcomeSuccess,
		ClassifierVerdictTerminal: false,
		OperatorStopImmediate:     false,
	}
	got := ClassifyTerminalState(in)
	if got != TerminalStateNonTerminal {
		t.Errorf("EM-015c: expected TerminalStateNonTerminal, got %s", got)
	}
}

// TestClassifyTerminalState_EM015c_EmptyTerminalSetIsNotCompleted verifies that
// EM-015c (a) does NOT fire when terminal_node_ids is empty, even with a success
// outcome and no classifier/operator signals. An empty terminal set means no
// node can satisfy condition (a).
// EM-015c: "current node_id ∈ terminal_node_ids" — empty set has no members.
func TestClassifyTerminalState_EM015c_EmptyTerminalSetIsNotCompleted(t *testing.T) {
	t.Parallel()

	in := TerminalStateInput{
		CurrentNodeID:             "node-end",
		TerminalNodeIDs:           []NodeID{},
		LastOutcome:               LastOutcomeSuccess,
		ClassifierVerdictTerminal: false,
		OperatorStopImmediate:     false,
	}
	got := ClassifyTerminalState(in)
	if got != TerminalStateNonTerminal {
		t.Errorf("EM-015c: expected TerminalStateNonTerminal, got %s", got)
	}
}

// TestTerminalStateKindString_EM015c verifies that String() returns stable
// lowercase names for all TerminalStateKind values, including an unknown value.
// EM-015c: TerminalStateKind is the canonical type for the three terminal states.
func TestTerminalStateKindString_EM015c(t *testing.T) {
	t.Parallel()

	cases := []struct {
		kind TerminalStateKind
		want string
	}{
		{TerminalStateNonTerminal, "non-terminal"},
		{TerminalStateCompleted, "completed"},
		{TerminalStateFailed, "failed"},
		{TerminalStateCanceled, "canceled"},
		{TerminalStateKind(99), "unknown"},
	}
	for _, tc := range cases {
		got := tc.kind.String()
		if got != tc.want {
			t.Errorf("EM-015c: TerminalStateKind(%d).String() = %q, want %q", int(tc.kind), got, tc.want)
		}
	}
}

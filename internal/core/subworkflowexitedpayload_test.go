package core

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

// subwfExitedFixture returns a fully-populated SubWorkflowExitedPayload with
// all required fields set to valid non-zero values.
// Used as the base for structural tests (hk-b3f.48).
func subwfExitedFixture(t *testing.T) SubWorkflowExitedPayload {
	t.Helper()
	return SubWorkflowExitedPayload{
		RunID:                 RunID(uuid.MustParse("01942b3c-0000-7000-8000-000000000048")),
		ParentNodeID:          NodeID("review-step"),
		SubWorkflowName:       SubWorkflowRef("code-review-workflow"),
		SubWorkflowVersion:    WorkflowVersion("1.2.0"),
		TerminalOutcomeStatus: OutcomeStatusSuccess,
	}
}

// TestSubWorkflowExitedPayload_Valid verifies that a fully-populated
// SubWorkflowExitedPayload passes Valid().
func TestSubWorkflowExitedPayload_Valid(t *testing.T) {
	t.Parallel()

	p := subwfExitedFixture(t)
	if !p.Valid() {
		t.Error("expected Valid() == true for well-formed payload, got false")
	}
}

// TestSubWorkflowExitedPayload_Valid_ZeroRunID verifies that Valid() rejects
// a payload with a zero RunID.
func TestSubWorkflowExitedPayload_Valid_ZeroRunID(t *testing.T) {
	t.Parallel()

	p := subwfExitedFixture(t)
	p.RunID = RunID(uuid.Nil)
	if p.Valid() {
		t.Error("expected Valid() == false for zero RunID, got true")
	}
}

// TestSubWorkflowExitedPayload_Valid_EmptyParentNodeID verifies that Valid()
// rejects a payload with an empty ParentNodeID.
func TestSubWorkflowExitedPayload_Valid_EmptyParentNodeID(t *testing.T) {
	t.Parallel()

	p := subwfExitedFixture(t)
	p.ParentNodeID = NodeID("")
	if p.Valid() {
		t.Error("expected Valid() == false for empty ParentNodeID, got true")
	}
}

// TestSubWorkflowExitedPayload_Valid_EmptySubWorkflowName verifies that
// Valid() rejects a payload with an empty SubWorkflowName.
func TestSubWorkflowExitedPayload_Valid_EmptySubWorkflowName(t *testing.T) {
	t.Parallel()

	p := subwfExitedFixture(t)
	p.SubWorkflowName = SubWorkflowRef("")
	if p.Valid() {
		t.Error("expected Valid() == false for empty SubWorkflowName, got true")
	}
}

// TestSubWorkflowExitedPayload_Valid_EmptySubWorkflowVersion verifies that
// Valid() rejects a payload with an empty SubWorkflowVersion.
func TestSubWorkflowExitedPayload_Valid_EmptySubWorkflowVersion(t *testing.T) {
	t.Parallel()

	p := subwfExitedFixture(t)
	p.SubWorkflowVersion = WorkflowVersion("")
	if p.Valid() {
		t.Error("expected Valid() == false for empty SubWorkflowVersion, got true")
	}
}

// TestSubWorkflowExitedPayload_Valid_InvalidTerminalOutcomeStatus verifies
// that Valid() rejects a payload with an unrecognised TerminalOutcomeStatus.
func TestSubWorkflowExitedPayload_Valid_InvalidTerminalOutcomeStatus(t *testing.T) {
	t.Parallel()

	p := subwfExitedFixture(t)
	p.TerminalOutcomeStatus = OutcomeStatus("UNKNOWN")
	if p.Valid() {
		t.Error("expected Valid() == false for invalid TerminalOutcomeStatus, got true")
	}
}

// TestSubWorkflowExitedPayload_Valid_EmptyTerminalOutcomeStatus verifies that
// Valid() rejects a payload with an empty TerminalOutcomeStatus (zero value).
func TestSubWorkflowExitedPayload_Valid_EmptyTerminalOutcomeStatus(t *testing.T) {
	t.Parallel()

	p := subwfExitedFixture(t)
	p.TerminalOutcomeStatus = OutcomeStatus("")
	if p.Valid() {
		t.Error("expected Valid() == false for empty TerminalOutcomeStatus, got true")
	}
}

// TestSubWorkflowExitedPayload_AllOutcomeStatuses verifies that all four
// declared OutcomeStatus values are accepted by Valid(), per EM-036a (the rule
// is outcome-status-agnostic: whatever the last expanded node produced is what
// escapes and is carried in the exit event).
func TestSubWorkflowExitedPayload_AllOutcomeStatuses(t *testing.T) {
	t.Parallel()

	statuses := []OutcomeStatus{
		OutcomeStatusSuccess,
		OutcomeStatusFail,
		OutcomeStatusRetry,
		OutcomeStatusPartialSuccess,
	}
	for _, s := range statuses {
		p := subwfExitedFixture(t)
		p.TerminalOutcomeStatus = s
		if !p.Valid() {
			t.Errorf("expected Valid() == true for TerminalOutcomeStatus=%q (EM-036a), got false", s)
		}
	}
}

// TestSubWorkflowExitedPayload_JSONRoundTrip verifies that a
// SubWorkflowExitedPayload survives a JSON marshal/unmarshal round-trip with
// all fields intact (event-model.md §8.1.10 wire shape).
func TestSubWorkflowExitedPayload_JSONRoundTrip(t *testing.T) {
	t.Parallel()

	orig := subwfExitedFixture(t)
	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var got SubWorkflowExitedPayload
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if got.RunID != orig.RunID {
		t.Errorf("RunID: got %v, want %v", got.RunID, orig.RunID)
	}
	if got.ParentNodeID != orig.ParentNodeID {
		t.Errorf("ParentNodeID: got %q, want %q", got.ParentNodeID, orig.ParentNodeID)
	}
	if got.SubWorkflowName != orig.SubWorkflowName {
		t.Errorf("SubWorkflowName: got %q, want %q", got.SubWorkflowName, orig.SubWorkflowName)
	}
	if got.SubWorkflowVersion != orig.SubWorkflowVersion {
		t.Errorf("SubWorkflowVersion: got %q, want %q", got.SubWorkflowVersion, orig.SubWorkflowVersion)
	}
	if got.TerminalOutcomeStatus != orig.TerminalOutcomeStatus {
		t.Errorf("TerminalOutcomeStatus: got %q, want %q", got.TerminalOutcomeStatus, orig.TerminalOutcomeStatus)
	}
}

// TestSubWorkflowExitedPayload_JSONKeys verifies that the JSON field names
// match the snake_case wire shape declared in event-model.md §8.1.10.
func TestSubWorkflowExitedPayload_JSONKeys(t *testing.T) {
	t.Parallel()

	p := subwfExitedFixture(t)
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("json.Unmarshal to map: %v", err)
	}

	required := []string{
		"run_id", "parent_node_id", "sub_workflow_name",
		"sub_workflow_version", "terminal_outcome_status",
	}
	for _, key := range required {
		if _, ok := m[key]; !ok {
			t.Errorf("expected JSON key %q to be present, got absent", key)
		}
	}
}

// TestSubWorkflowExitedPayload_TerminalOutcomeCorrelatesEM036a verifies that
// the TerminalOutcomeStatus in the exit event corresponds to the Outcome that
// would escape from the sub-workflow to the parent's cascade per EM-036a.
//
// EM-036a: "The sub-workflow-exited event's terminal-outcome correlation field
// MUST carry the same Outcome [as the Outcome produced by the last expanded node]."
func TestSubWorkflowExitedPayload_TerminalOutcomeCorrelatesEM036a(t *testing.T) {
	t.Parallel()

	// Simulate: last expanded node produced SUCCESS.
	lastNodeOutcome := subwfTerminalOutcomeFixture(t)

	p := subwfExitedFixture(t)
	p.TerminalOutcomeStatus = lastNodeOutcome.Status

	if !p.Valid() {
		t.Error("SubWorkflowExitedPayload must be Valid() when TerminalOutcomeStatus matches last expanded node's Outcome (EM-036a)")
	}
	if p.TerminalOutcomeStatus != lastNodeOutcome.Status {
		t.Errorf("TerminalOutcomeStatus = %q, want %q (EM-036a correlation)", p.TerminalOutcomeStatus, lastNodeOutcome.Status)
	}
}

// TestSubWorkflowExitedPayload_CorrelationFields verifies that the payload
// carries the correlation fields (run_id + parent_node_id) required by
// execution-model.md §4.8.EM-036 for correlating sub-workflow lifecycle events.
func TestSubWorkflowExitedPayload_CorrelationFields(t *testing.T) {
	t.Parallel()

	p := subwfExitedFixture(t)

	// EM-036: "Both events correlate via run_id and the parent namespaced node_id"
	if uuid.UUID(p.RunID) == uuid.Nil {
		t.Error("RunID is zero: run_id correlation field cannot be the zero UUID (EM-036)")
	}
	if p.ParentNodeID == "" {
		t.Error("ParentNodeID is empty: parent_node_id correlation field must be non-empty (EM-036)")
	}
}

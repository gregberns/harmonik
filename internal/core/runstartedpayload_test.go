package core

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

// runstartedFixture returns a fully-populated RunStartedPayload for a
// bead-tied run, with all fields set to valid non-zero values.
// Used as the base for structural tests (hk-b3f.16).
func runstartedFixture(t *testing.T) RunStartedPayload {
	t.Helper()
	beadID := BeadID("bead-abc-001")
	return RunStartedPayload{
		RunID:           RunID(uuid.MustParse("01942b3c-0000-7000-8000-000000000010")),
		WorkflowID:      WorkflowID(uuid.MustParse("01942b3c-0000-7000-8000-000000000020")),
		WorkflowVersion: WorkflowVersion("1.0.0"),
		BeadID:          &beadID,
		WorkspacePath:   "/tmp/harmonik/workspaces/abc-001",
		InputRef:        "ws-abc-001",
	}
}

// runstartedFixtureStandalone returns a RunStartedPayload for a
// standalone-input run (BeadID == nil).
func runstartedFixtureStandalone(t *testing.T) RunStartedPayload {
	t.Helper()
	return RunStartedPayload{
		RunID:           RunID(uuid.MustParse("01942b3c-0000-7000-8000-000000000011")),
		WorkflowID:      WorkflowID(uuid.MustParse("01942b3c-0000-7000-8000-000000000021")),
		WorkflowVersion: WorkflowVersion("1.0.0"),
		BeadID:          nil,
		WorkspacePath:   "/tmp/harmonik/workspaces/standalone-001",
		InputRef:        "ws-standalone-001",
	}
}

// TestRunStartedPayload_Valid_BeadTied verifies that a fully-populated
// bead-tied RunStartedPayload passes Valid().
func TestRunStartedPayload_Valid_BeadTied(t *testing.T) {
	t.Parallel()

	p := runstartedFixture(t)
	if !p.Valid() {
		t.Error("expected Valid() == true for bead-tied payload, got false")
	}
}

// TestRunStartedPayload_Valid_Standalone verifies that a standalone-input
// RunStartedPayload with nil BeadID passes Valid().
func TestRunStartedPayload_Valid_Standalone(t *testing.T) {
	t.Parallel()

	p := runstartedFixtureStandalone(t)
	if !p.Valid() {
		t.Error("expected Valid() == true for standalone payload (nil BeadID), got false")
	}
}

// TestRunStartedPayload_Valid_ZeroRunID verifies that Valid() rejects a
// payload with a zero RunID.
func TestRunStartedPayload_Valid_ZeroRunID(t *testing.T) {
	t.Parallel()

	p := runstartedFixture(t)
	p.RunID = RunID(uuid.Nil)
	if p.Valid() {
		t.Error("expected Valid() == false for zero RunID, got true")
	}
}

// TestRunStartedPayload_Valid_ZeroWorkflowID verifies that Valid() rejects a
// payload with a zero WorkflowID.
func TestRunStartedPayload_Valid_ZeroWorkflowID(t *testing.T) {
	t.Parallel()

	p := runstartedFixture(t)
	p.WorkflowID = WorkflowID(uuid.Nil)
	if p.Valid() {
		t.Error("expected Valid() == false for zero WorkflowID, got true")
	}
}

// TestRunStartedPayload_Valid_EmptyWorkflowVersion verifies that Valid()
// rejects a payload with an empty WorkflowVersion.
func TestRunStartedPayload_Valid_EmptyWorkflowVersion(t *testing.T) {
	t.Parallel()

	p := runstartedFixture(t)
	p.WorkflowVersion = ""
	if p.Valid() {
		t.Error("expected Valid() == false for empty WorkflowVersion, got true")
	}
}

// TestRunStartedPayload_Valid_EmptyBeadID verifies that Valid() rejects a
// bead-tied payload where BeadID is set but empty (set-but-empty invariant).
func TestRunStartedPayload_Valid_EmptyBeadID(t *testing.T) {
	t.Parallel()

	p := runstartedFixture(t)
	empty := BeadID("")
	p.BeadID = &empty
	if p.Valid() {
		t.Error("expected Valid() == false for set-but-empty BeadID, got true")
	}
}

// TestRunStartedPayload_Valid_EmptyWorkspacePath verifies that Valid() rejects
// a payload with an empty WorkspacePath.
func TestRunStartedPayload_Valid_EmptyWorkspacePath(t *testing.T) {
	t.Parallel()

	p := runstartedFixture(t)
	p.WorkspacePath = ""
	if p.Valid() {
		t.Error("expected Valid() == false for empty WorkspacePath, got true")
	}
}

// TestRunStartedPayload_Valid_EmptyInputRef verifies that Valid() rejects a
// payload with an empty InputRef.
func TestRunStartedPayload_Valid_EmptyInputRef(t *testing.T) {
	t.Parallel()

	p := runstartedFixture(t)
	p.InputRef = ""
	if p.Valid() {
		t.Error("expected Valid() == false for empty InputRef, got true")
	}
}

// TestRunStartedPayload_JSONRoundTrip verifies that a fully-populated
// RunStartedPayload survives a JSON marshal/unmarshal round-trip with all
// fields intact (event-model.md §8.1.1 wire shape).
func TestRunStartedPayload_JSONRoundTrip(t *testing.T) {
	t.Parallel()

	orig := runstartedFixture(t)
	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var got RunStartedPayload
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if got.RunID != orig.RunID {
		t.Errorf("RunID: got %v, want %v", got.RunID, orig.RunID)
	}
	if got.WorkflowID != orig.WorkflowID {
		t.Errorf("WorkflowID: got %v, want %v", got.WorkflowID, orig.WorkflowID)
	}
	if got.WorkflowVersion != orig.WorkflowVersion {
		t.Errorf("WorkflowVersion: got %q, want %q", got.WorkflowVersion, orig.WorkflowVersion)
	}
	if got.BeadID == nil || *got.BeadID != *orig.BeadID {
		t.Errorf("BeadID: got %v, want %v", got.BeadID, orig.BeadID)
	}
	if got.WorkspacePath != orig.WorkspacePath {
		t.Errorf("WorkspacePath: got %q, want %q", got.WorkspacePath, orig.WorkspacePath)
	}
	if got.InputRef != orig.InputRef {
		t.Errorf("InputRef: got %q, want %q", got.InputRef, orig.InputRef)
	}
}

// TestRunStartedPayload_JSONOmitsBeadIDWhenNil verifies that when BeadID is
// nil the JSON output omits the bead_id key (omitempty).
// Standalone-input runs must not carry bead_id in the event payload.
func TestRunStartedPayload_JSONOmitsBeadIDWhenNil(t *testing.T) {
	t.Parallel()

	p := runstartedFixtureStandalone(t)
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("json.Unmarshal to map: %v", err)
	}
	if _, ok := m["bead_id"]; ok {
		t.Error("bead_id key present in JSON for standalone run (nil BeadID), want omitted")
	}
}

// TestRunStartedPayload_JSONKeys verifies that the JSON field names match the
// snake_case wire shape declared in event-model.md §8.1.1 and §6.3.
func TestRunStartedPayload_JSONKeys(t *testing.T) {
	t.Parallel()

	p := runstartedFixture(t)
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("json.Unmarshal to map: %v", err)
	}

	required := []string{"run_id", "workflow_id", "workflow_version", "bead_id", "workspace_path", "input_ref"}
	for _, key := range required {
		if _, ok := m[key]; !ok {
			t.Errorf("expected JSON key %q to be present, got absent", key)
		}
	}
}

// TestRunStartedPayload_EmissionOrderSensor documents the EM-015a ordering
// invariant at the type level: run_started MUST be emitted AFTER create_run
// allocates run_id AND AFTER the Beads atomic-claim persists AND BEFORE any
// node is dispatched (execution-model.md §4.3.EM-015a).
//
// This sensor test asserts that RunStartedPayload carries all three
// identifiers that prove post-allocation ordering:
//   - RunID: non-zero ↔ create_run completed (run_id allocated)
//   - WorkflowID / WorkflowVersion: non-zero ↔ dispatch context resolved
//   - BeadID (when non-nil): non-empty ↔ Beads atomic-claim persisted (BI-009)
//
// The test constructs a payload from a valid post-claim state and verifies
// Valid() passes — a payload that fails Valid() MUST NOT be emitted.
func TestRunStartedPayload_EmissionOrderSensor(t *testing.T) {
	t.Parallel()

	// Simulate post-create_run, post-atomic-claim state (bead-tied run).
	// RunID assigned by create_run; BeadID claimed atomically per BI-009.
	postClaim := runstartedFixture(t)

	// Valid() gates emission: a daemon MUST NOT emit run_started with an invalid
	// payload. All three ordering-proof identifiers must be present.
	if !postClaim.Valid() {
		t.Fatal("post-claim RunStartedPayload failed Valid(): cannot represent emission precondition")
	}
	if uuid.UUID(postClaim.RunID) == uuid.Nil {
		t.Error("RunID is zero: create_run allocation not represented — ordering invariant broken")
	}
	if postClaim.BeadID == nil || *postClaim.BeadID == "" {
		t.Error("BeadID is absent: Beads atomic-claim not represented — ordering invariant broken")
	}

	// Simulate standalone run (no bead): Beads atomic-claim is skipped, so
	// BeadID is nil. Valid() must still pass — ordering is still satisfied.
	standalone := runstartedFixtureStandalone(t)
	if !standalone.Valid() {
		t.Fatal("standalone RunStartedPayload failed Valid(): cannot represent emission precondition")
	}
	if standalone.BeadID != nil {
		t.Error("standalone payload should have nil BeadID (no Beads claim for non-bead-tied run)")
	}
}

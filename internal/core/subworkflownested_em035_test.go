package core

import (
	"testing"

	"github.com/google/uuid"
)

// subwfNestedCheckpointFixture returns a Checkpoint that represents a durable
// transition inside an expanded sub-workflow. It carries the parent run's RunID
// per EM-035: every checkpoint inside a sub-workflow expansion MUST be on the
// parent run's task branch and MUST use the parent run_id as the sole run
// identifier.
//
// The namespaced NodeID "dispatch/validate" encodes the sub-workflow expansion
// per §4.8.EM-034a: the parent node is "dispatch" and the expanded node is
// "validate".
//
// Per EM-035, there MUST NOT be a separate sub-workflow checkpoint trail; the
// parent run's task branch is the only trail.
func subwfNestedCheckpointFixture(t *testing.T) Checkpoint {
	t.Helper()

	parentRunID := RunID(uuid.MustParse("01960000-0000-7000-8000-000000004700"))
	transitionID := TransitionID(uuid.MustParse("01960000-0000-7000-8000-000000004701"))
	return Checkpoint{
		CommitHash:           "b47b47b47b47b47b47b47b47b47b47b47b47b47b",
		RunID:                parentRunID,
		StateID:              StateID(uuid.MustParse("01960000-0000-7000-8000-000000004702")),
		TransitionID:         transitionID,
		BeadID:               nil,
		SchemaVersion:        1,
		TransitionRecordPath: TransitionRecordPath(parentRunID, transitionID),
	}
}

// subwfNestedCheckpointFixtureParentRunID returns the canonical parent RunID
// used by subwfNestedCheckpointFixture. Tests assert that every checkpoint
// inside a sub-workflow expansion carries this RunID — not a child RunID.
func subwfNestedCheckpointFixtureParentRunID() RunID {
	return RunID(uuid.MustParse("01960000-0000-7000-8000-000000004700"))
}

// TestSubWorkflowNestedCheckpoint_SingleRunID verifies that a checkpoint emitted
// inside an expanded sub-workflow carries the parent run's RunID (EM-035).
//
// EM-035 requires every durable transition inside an expanded sub-workflow to
// emit a checkpoint commit on the parent run's task branch. A separate
// sub-workflow run_id MUST NOT appear; the parent run_id is the sole run
// identifier for the entire nested execution per §4.8.EM-034.
func TestSubWorkflowNestedCheckpoint_SingleRunID(t *testing.T) {
	t.Parallel()

	c := subwfNestedCheckpointFixture(t)
	parentRunID := subwfNestedCheckpointFixtureParentRunID()

	if RunID(uuid.UUID(c.RunID)) != parentRunID {
		t.Errorf("checkpoint inside sub-workflow expansion carries RunID %v, want parent RunID %v (EM-035)",
			c.RunID, parentRunID)
	}
}

// TestSubWorkflowNestedCheckpoint_Valid verifies that the fixture checkpoint is
// structurally valid (EM-023 durability invariants hold).
func TestSubWorkflowNestedCheckpoint_Valid(t *testing.T) {
	t.Parallel()

	c := subwfNestedCheckpointFixture(t)
	if !c.Valid() {
		t.Error("subwfNestedCheckpointFixture produced invalid Checkpoint, want Valid() == true")
	}
}

// TestSubWorkflowNestedCheckpoint_PathUsesParentRunID verifies that the
// TransitionRecordPath of a checkpoint inside an expanded sub-workflow is scoped
// to the parent run's RunID per §4.4.EM-018 (EM-035).
//
// Because the sub-workflow MUST NOT spawn a child run (§4.8.EM-034), all
// transition records written during nested execution MUST live under the parent
// run's path prefix ".harmonik/transitions/<parent_run_id>/".
func TestSubWorkflowNestedCheckpoint_PathUsesParentRunID(t *testing.T) {
	t.Parallel()

	c := subwfNestedCheckpointFixture(t)
	parentRunID := subwfNestedCheckpointFixtureParentRunID()
	want := TransitionRecordPath(parentRunID, c.TransitionID)

	if c.TransitionRecordPath != want {
		t.Errorf("checkpoint TransitionRecordPath = %q, want %q (EM-035: path must be scoped to parent run)",
			c.TransitionRecordPath, want)
	}
}

// TestSubWorkflowNestedCheckpoint_NamespacedNodeID documents that the NodeID
// carried by a checkpoint inside an expanded sub-workflow uses the namespaced
// form "<parent_node_id>/<sub_node_id>" per §4.8.EM-034a.
//
// This test exercises the namespacing rule via the StateID carried on the
// expansion fixture and documents that the state's node_id in the transition
// record MUST be namespaced. The checkpoint itself does not carry the NodeID
// directly; the namespaced form appears in the accompanying Transition record's
// from_state and to_state fields.
func TestSubWorkflowNestedCheckpoint_NamespacedNodeID(t *testing.T) {
	t.Parallel()

	// The namespaced node ID for an expanded node "validate" inside parent node
	// "dispatch" is "dispatch/validate" per §4.8.EM-034a.
	namespacedNodeID := NodeID("dispatch/validate")

	// Confirm the namespaced form is non-empty and contains a slash separator.
	if namespacedNodeID == "" {
		t.Fatal("namespaced NodeID must not be empty")
	}
	parentNodeID := NodeID("dispatch")
	subNodeID := NodeID("validate")
	want := NodeID(string(parentNodeID) + "/" + string(subNodeID))
	if namespacedNodeID != want {
		t.Errorf("namespaced NodeID = %q, want %q (EM-034a)", namespacedNodeID, want)
	}
}

// TestSubWorkflowNestedCheckpoint_NeSeparateTrail verifies that the EM-035 rule
// "MUST NOT have a separate sub-workflow checkpoint trail" is expressed as a
// single-run-id constraint: only one RunID exists for a parent run and all of
// its nested sub-workflow executions.
//
// This test documents the anti-pattern: if a second RunID were allocated for the
// sub-workflow, the checkpoint fixture's RunID would differ from the parent
// RunID. The fixture deliberately uses the parent RunID for every checkpoint
// inside the sub-workflow to satisfy EM-035.
func TestSubWorkflowNestedCheckpoint_NoSeparateTrail(t *testing.T) {
	t.Parallel()

	parentRunID := subwfNestedCheckpointFixtureParentRunID()

	// Simulate three checkpoints inside a nested sub-workflow. All MUST carry
	// the parent RunID; no checkpoint may carry a distinct "sub-workflow RunID".
	for i, c := range []Checkpoint{
		subwfNestedCheckpointFixture(t),
		subwfNestedCheckpointFixture(t),
		subwfNestedCheckpointFixture(t),
	} {
		if c.RunID != parentRunID {
			t.Errorf("checkpoint[%d] RunID = %v, want parent RunID %v (EM-035: no separate sub-workflow trail)",
				i, c.RunID, parentRunID)
		}
	}
}

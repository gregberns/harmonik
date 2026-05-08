package core

import (
	"testing"

	"github.com/google/uuid"
)

// checkpointFixtureValid returns a fully-populated Checkpoint with all required
// fields non-zero, BeadID set to a non-nil non-empty value, and
// TransitionRecordPath set to the canonical EM-018 path matching RunID and
// TransitionID (hk-b3f.19-impl).
func checkpointFixtureValid(t *testing.T) Checkpoint {
	t.Helper()

	beadID := BeadID("bead-abc-123")
	runID := RunID(uuid.Must(uuid.NewV7()))
	transitionID := TransitionID(uuid.Must(uuid.NewV7()))
	return Checkpoint{
		CommitHash:           "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2",
		RunID:                runID,
		StateID:              StateID(uuid.Must(uuid.NewV7())),
		TransitionID:         transitionID,
		BeadID:               &beadID,
		SchemaVersion:        1,
		TransitionRecordPath: TransitionRecordPath(runID, transitionID),
	}
}

func TestCheckpointValid_AllFieldsSet(t *testing.T) {
	t.Parallel()

	c := checkpointFixtureValid(t)
	if !c.Valid() {
		t.Error("Valid() = false for fully-populated Checkpoint, want true")
	}
}

func TestCheckpointValid_NilBeadIDIsValid(t *testing.T) {
	t.Parallel()

	c := checkpointFixtureValid(t)
	c.BeadID = nil
	if !c.Valid() {
		t.Error("Valid() = false with nil BeadID, want true (BeadID is optional)")
	}
}

func TestCheckpointValid_EmptyCommitHash(t *testing.T) {
	t.Parallel()

	c := checkpointFixtureValid(t)
	c.CommitHash = ""
	if c.Valid() {
		t.Error("Valid() = true with empty CommitHash, want false")
	}
}

func TestCheckpointValid_ZeroRunID(t *testing.T) {
	t.Parallel()

	c := checkpointFixtureValid(t)
	c.RunID = RunID(uuid.Nil)
	if c.Valid() {
		t.Error("Valid() = true with zero RunID, want false")
	}
}

func TestCheckpointValid_ZeroStateID(t *testing.T) {
	t.Parallel()

	c := checkpointFixtureValid(t)
	c.StateID = StateID(uuid.Nil)
	if c.Valid() {
		t.Error("Valid() = true with zero StateID, want false")
	}
}

func TestCheckpointValid_ZeroTransitionID(t *testing.T) {
	t.Parallel()

	c := checkpointFixtureValid(t)
	c.TransitionID = TransitionID(uuid.Nil)
	if c.Valid() {
		t.Error("Valid() = true with zero TransitionID, want false")
	}
}

func TestCheckpointValid_NonNilEmptyBeadIDIsInvalid(t *testing.T) {
	t.Parallel()

	c := checkpointFixtureValid(t)
	empty := BeadID("")
	c.BeadID = &empty
	if c.Valid() {
		t.Error("Valid() = true with non-nil but empty BeadID, want false")
	}
}

func TestCheckpointValid_ZeroSchemaVersion(t *testing.T) {
	t.Parallel()

	c := checkpointFixtureValid(t)
	c.SchemaVersion = 0
	if c.Valid() {
		t.Error("Valid() = true with zero SchemaVersion, want false")
	}
}

// TestCheckpointValid_PathMismatchRunID verifies that Valid() rejects a
// Checkpoint whose TransitionRecordPath run_id component does not match RunID
// (execution-model.md §4.4.EM-018 path-coherence invariant; hk-b3f.19).
func TestCheckpointValid_PathMismatchRunID(t *testing.T) {
	t.Parallel()

	c := checkpointFixtureValid(t)
	// Replace run_id in the path with a different UUID while keeping transition_id correct.
	otherRunID := RunID(uuid.Must(uuid.NewV7()))
	c.TransitionRecordPath = TransitionRecordPath(otherRunID, c.TransitionID)
	if c.Valid() {
		t.Error("Valid() = true when path run_id does not match RunID, want false (EM-018 path coherence)")
	}
}

// TestCheckpointValid_PathMismatchTransitionID verifies that Valid() rejects a
// Checkpoint whose TransitionRecordPath transition_id component does not match
// TransitionID (execution-model.md §4.4.EM-018 path-coherence invariant; hk-b3f.19).
func TestCheckpointValid_PathMismatchTransitionID(t *testing.T) {
	t.Parallel()

	c := checkpointFixtureValid(t)
	// Replace transition_id in the path with a different UUID while keeping run_id correct.
	otherTransitionID := TransitionID(uuid.Must(uuid.NewV7()))
	c.TransitionRecordPath = TransitionRecordPath(c.RunID, otherTransitionID)
	if c.Valid() {
		t.Error("Valid() = true when path transition_id does not match TransitionID, want false (EM-018 path coherence)")
	}
}

// TestCheckpointValid_EmptyTransitionRecordPath verifies that Valid() rejects a
// Checkpoint with an empty TransitionRecordPath (trivial path-coherence failure).
func TestCheckpointValid_EmptyTransitionRecordPath(t *testing.T) {
	t.Parallel()

	c := checkpointFixtureValid(t)
	c.TransitionRecordPath = ""
	if c.Valid() {
		t.Error("Valid() = true with empty TransitionRecordPath, want false")
	}
}

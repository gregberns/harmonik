package core

import (
	"testing"

	"github.com/google/uuid"
)

// validCheckpoint returns a fully-populated Checkpoint with all required fields non-zero
// and BeadID set to a non-nil, non-empty value.
func validCheckpoint(t *testing.T) Checkpoint {
	t.Helper()

	beadID := BeadID("bead-abc-123")
	return Checkpoint{
		CommitHash:           "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2",
		RunID:                RunID(uuid.Must(uuid.NewV7())),
		StateID:              StateID(uuid.Must(uuid.NewV7())),
		TransitionID:         TransitionID(uuid.Must(uuid.NewV7())),
		BeadID:               &beadID,
		SchemaVersion:        1,
		TransitionRecordPath: ".harmonik/transitions/run-id/transition-id.json",
	}
}

func TestCheckpointValid_AllFieldsSet(t *testing.T) {
	t.Parallel()

	c := validCheckpoint(t)
	if !c.Valid() {
		t.Error("Valid() = false for fully-populated Checkpoint, want true")
	}
}

func TestCheckpointValid_NilBeadIDIsValid(t *testing.T) {
	t.Parallel()

	c := validCheckpoint(t)
	c.BeadID = nil
	if !c.Valid() {
		t.Error("Valid() = false with nil BeadID, want true (BeadID is optional)")
	}
}

func TestCheckpointValid_EmptyCommitHash(t *testing.T) {
	t.Parallel()

	c := validCheckpoint(t)
	c.CommitHash = ""
	if c.Valid() {
		t.Error("Valid() = true with empty CommitHash, want false")
	}
}

func TestCheckpointValid_ZeroRunID(t *testing.T) {
	t.Parallel()

	c := validCheckpoint(t)
	c.RunID = RunID(uuid.Nil)
	if c.Valid() {
		t.Error("Valid() = true with zero RunID, want false")
	}
}

func TestCheckpointValid_ZeroStateID(t *testing.T) {
	t.Parallel()

	c := validCheckpoint(t)
	c.StateID = StateID(uuid.Nil)
	if c.Valid() {
		t.Error("Valid() = true with zero StateID, want false")
	}
}

func TestCheckpointValid_ZeroTransitionID(t *testing.T) {
	t.Parallel()

	c := validCheckpoint(t)
	c.TransitionID = TransitionID(uuid.Nil)
	if c.Valid() {
		t.Error("Valid() = true with zero TransitionID, want false")
	}
}

func TestCheckpointValid_NonNilEmptyBeadIDIsInvalid(t *testing.T) {
	t.Parallel()

	c := validCheckpoint(t)
	empty := BeadID("")
	c.BeadID = &empty
	if c.Valid() {
		t.Error("Valid() = true with non-nil but empty BeadID, want false")
	}
}

func TestCheckpointValid_ZeroSchemaVersion(t *testing.T) {
	t.Parallel()

	c := validCheckpoint(t)
	c.SchemaVersion = 0
	if c.Valid() {
		t.Error("Valid() = true with zero SchemaVersion, want false")
	}
}

func TestCheckpointValid_EmptyTransitionRecordPath(t *testing.T) {
	t.Parallel()

	c := validCheckpoint(t)
	c.TransitionRecordPath = ""
	if c.Valid() {
		t.Error("Valid() = true with empty TransitionRecordPath, want false")
	}
}

package core

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// validRun returns a fully-populated Run with all required fields non-zero,
// Context initialised as a non-nil empty map, and BeadID set to a non-nil
// non-empty value.
func validRun(t *testing.T) Run {
	t.Helper()

	beadID := BeadID("bead-run-001")
	now := time.Now()
	end := now.Add(time.Minute)
	return Run{
		RunID:           RunID(uuid.Must(uuid.NewV7())),
		WorkflowID:      WorkflowID(uuid.Must(uuid.NewV7())),
		WorkflowVersion: WorkflowVersion("1.0.0"),
		Input:           WorkspaceRef("workspace://project/input"),
		BeadID:          &beadID,
		State:           StateID(uuid.Must(uuid.NewV7())),
		Context:         map[string]any{},
		StartTime:       now,
		EndTime:         &end,
	}
}

func TestRunValid_AllFieldsSet(t *testing.T) {
	t.Parallel()

	r := validRun(t)
	if !r.Valid() {
		t.Error("Valid() = false for fully-populated Run, want true")
	}
}

func TestRunValid_NilBeadIDIsValid(t *testing.T) {
	t.Parallel()

	r := validRun(t)
	r.BeadID = nil
	if !r.Valid() {
		t.Error("Valid() = false with nil BeadID, want true (BeadID is optional)")
	}
}

func TestRunValid_NilEndTimeIsValid(t *testing.T) {
	t.Parallel()

	r := validRun(t)
	r.EndTime = nil
	if !r.Valid() {
		t.Error("Valid() = false with nil EndTime, want true (EndTime is optional)")
	}
}

func TestRunValid_EmptyContextIsValid(t *testing.T) {
	t.Parallel()

	r := validRun(t)
	r.Context = map[string]any{}
	if !r.Valid() {
		t.Error("Valid() = false with empty (non-nil) Context, want true")
	}
}

func TestRunValid_PopulatedContextIsValid(t *testing.T) {
	t.Parallel()

	r := validRun(t)
	r.Context = map[string]any{"key": "value", "count": 42}
	if !r.Valid() {
		t.Error("Valid() = false with populated Context, want true")
	}
}

func TestRunValid_ZeroRunID(t *testing.T) {
	t.Parallel()

	r := validRun(t)
	r.RunID = RunID(uuid.Nil)
	if r.Valid() {
		t.Error("Valid() = true with zero RunID, want false")
	}
}

func TestRunValid_ZeroWorkflowID(t *testing.T) {
	t.Parallel()

	r := validRun(t)
	r.WorkflowID = WorkflowID(uuid.Nil)
	if r.Valid() {
		t.Error("Valid() = true with zero WorkflowID, want false")
	}
}

func TestRunValid_EmptyWorkflowVersion(t *testing.T) {
	t.Parallel()

	r := validRun(t)
	r.WorkflowVersion = WorkflowVersion("")
	if r.Valid() {
		t.Error("Valid() = true with empty WorkflowVersion, want false")
	}
}

func TestRunValid_EmptyInput(t *testing.T) {
	t.Parallel()

	r := validRun(t)
	r.Input = WorkspaceRef("")
	if r.Valid() {
		t.Error("Valid() = true with empty Input, want false")
	}
}

func TestRunValid_ZeroState(t *testing.T) {
	t.Parallel()

	r := validRun(t)
	r.State = StateID(uuid.Nil)
	if r.Valid() {
		t.Error("Valid() = true with zero State, want false")
	}
}

func TestRunValid_NonNilEmptyBeadIDIsInvalid(t *testing.T) {
	t.Parallel()

	r := validRun(t)
	empty := BeadID("")
	r.BeadID = &empty
	if r.Valid() {
		t.Error("Valid() = true with non-nil but empty BeadID, want false")
	}
}

func TestRunValid_NilContextIsInvalid(t *testing.T) {
	t.Parallel()

	r := validRun(t)
	r.Context = nil
	if r.Valid() {
		t.Error("Valid() = true with nil Context, want false")
	}
}

func TestRunValid_ZeroStartTime(t *testing.T) {
	t.Parallel()

	r := validRun(t)
	r.StartTime = time.Time{}
	if r.Valid() {
		t.Error("Valid() = true with zero StartTime, want false")
	}
}

func TestRunValid_NonNilZeroEndTimeIsInvalid(t *testing.T) {
	t.Parallel()

	r := validRun(t)
	zero := time.Time{}
	r.EndTime = &zero
	if r.Valid() {
		t.Error("Valid() = true with non-nil but zero EndTime, want false")
	}
}

// TestRunBeadID_BI017Sensor is the requirement-traceable sensor for BI-017.
// Per beads-integration.md §4 BI-017: Run.bead_id is recorded for bead-bound
// runs and unset (nil) for non-bead-bound runs.
func TestRunBeadID_BI017Sensor(t *testing.T) {
	t.Parallel()

	t.Run("nil_BeadID_valid_non_bead_bound", func(t *testing.T) {
		t.Parallel()
		r := validRun(t)
		r.BeadID = nil
		if !r.Valid() {
			t.Error("Valid() = false with nil BeadID, want true (non-bead-bound run; BI-017)")
		}
	})

	t.Run("non_nil_non_empty_BeadID_valid_bead_bound", func(t *testing.T) {
		t.Parallel()
		r := validRun(t)
		id := BeadID("bead-017-sensor")
		r.BeadID = &id
		if !r.Valid() {
			t.Error("Valid() = false with non-nil non-empty BeadID, want true (bead-bound run; BI-017)")
		}
	})

	t.Run("non_nil_empty_BeadID_invalid_set_but_empty", func(t *testing.T) {
		t.Parallel()
		r := validRun(t)
		empty := BeadID("")
		r.BeadID = &empty
		if r.Valid() {
			t.Error("Valid() = true with set-but-empty BeadID, want false (BI-017: never set to empty string)")
		}
	})
}

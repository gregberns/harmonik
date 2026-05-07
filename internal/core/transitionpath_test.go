// Package core — named requirement-traceable sensors for the transition-record path invariant.
//
// This file provides tests that verify the canonical relative path shape for transition
// records as required by execution-model.md §4.4 EM-019: given a run_id and a
// transition_id, the path MUST be
//
//	.harmonik/transitions/<run_id>/<transition_id>.json
//
// No cross-commit index may be required to resolve this path.
package core

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

// TestTransitionRecordPath_EM019Shape verifies that TransitionRecordPath produces
// the canonical path shape required by execution-model.md §4.4 EM-019.
func TestTransitionRecordPath_EM019Shape(t *testing.T) {
	runID := RunID(uuid.Must(uuid.NewV7()))
	transitionID := TransitionID(uuid.Must(uuid.NewV7()))

	got := TransitionRecordPath(runID, transitionID)

	runStr := runID.String()
	transStr := transitionID.String()

	if !strings.HasPrefix(got, ".harmonik/transitions/") {
		t.Errorf("TransitionRecordPath result %q does not start with .harmonik/transitions/", got)
	}

	if !strings.HasSuffix(got, ".json") {
		t.Errorf("TransitionRecordPath result %q does not end with .json", got)
	}

	if !strings.Contains(got, runStr) {
		t.Errorf("TransitionRecordPath result %q does not contain run_id %q", got, runStr)
	}

	if !strings.Contains(got, transStr) {
		t.Errorf("TransitionRecordPath result %q does not contain transition_id %q", got, transStr)
	}

	want := ".harmonik/transitions/" + runStr + "/" + transStr + ".json"
	if got != want {
		t.Errorf("TransitionRecordPath = %q, want %q", got, want)
	}
}

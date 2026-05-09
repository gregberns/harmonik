// Package core — named requirement-traceable sensors for the transition-record path invariant.
//
// This file provides tests that verify the canonical relative path shapes for transition
// records and externalized evidence per execution-model.md §4.4:
//
// EM-019: given a run_id and a transition_id, the transition-record path MUST be
//
//	.harmonik/transitions/<run_id>/<transition_id>.json
//
// No cross-commit index may be required to resolve this path.
//
// EM-021: externalized evidence/verifier_metrics files reside under
//
//	.harmonik/transitions/<run_id>/<transition_id>/evidence/*
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

// TestEvidenceExternalDir_EM021Shape verifies that EvidenceExternalDir produces
// the canonical evidence directory path required by execution-model.md §4.4 EM-021:
//
//	.harmonik/transitions/<run_id>/<transition_id>/evidence
func TestEvidenceExternalDir_EM021Shape(t *testing.T) {
	runID := RunID(uuid.Must(uuid.NewV7()))
	transitionID := TransitionID(uuid.Must(uuid.NewV7()))

	got := EvidenceExternalDir(runID, transitionID)

	runStr := runID.String()
	transStr := transitionID.String()

	if !strings.HasPrefix(got, ".harmonik/transitions/") {
		t.Errorf("EvidenceExternalDir result %q does not start with .harmonik/transitions/", got)
	}

	if !strings.HasSuffix(got, "/evidence") {
		t.Errorf("EvidenceExternalDir result %q does not end with /evidence", got)
	}

	if !strings.Contains(got, runStr) {
		t.Errorf("EvidenceExternalDir result %q does not contain run_id %q", got, runStr)
	}

	if !strings.Contains(got, transStr) {
		t.Errorf("EvidenceExternalDir result %q does not contain transition_id %q", got, transStr)
	}

	want := ".harmonik/transitions/" + runStr + "/" + transStr + "/evidence"
	if got != want {
		t.Errorf("EvidenceExternalDir = %q, want %q", got, want)
	}
}

// TestEvidenceExternalDir_EM021SiblingRelationship verifies that the evidence
// external directory is nested under the transition record's sibling directory,
// not a flat sibling of <transition_id>.json. The two paths must share the same
// run_id/transition_id prefix.
func TestEvidenceExternalDir_EM021SiblingRelationship(t *testing.T) {
	runID := RunID(uuid.Must(uuid.NewV7()))
	transitionID := TransitionID(uuid.Must(uuid.NewV7()))

	recordPath := TransitionRecordPath(runID, transitionID)
	evidenceDir := EvidenceExternalDir(runID, transitionID)

	// Strip the .json suffix from the record path; the evidence dir MUST be
	// a sub-path of the same <transition_id>/ directory.
	base := strings.TrimSuffix(recordPath, ".json")
	wantPrefix := base + "/evidence"
	if evidenceDir != wantPrefix {
		t.Errorf("EvidenceExternalDir = %q, want %q (must nest under transition record base dir)", evidenceDir, wantPrefix)
	}
}

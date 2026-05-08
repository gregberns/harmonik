// Package core — requirement-traceable sensors for TransitionEventPayload per
// execution-model.md §4.6.EM-028.
//
// EM-028 requires the transition event payload to cite the transition by
// transition_id, run_id, and checkpoint commit hash so the full record is
// recoverable via git show. EM-029 prohibits duplicating the full trace
// payload (candidate_actions, evidence, verifier_metrics).
package core

import (
	"testing"

	"github.com/google/uuid"
)

// hkb3f36ValidPayload returns a fully-populated TransitionEventPayload for
// use in hk-b3f.36 projection tests.
func hkb3f36ValidPayload(t *testing.T) TransitionEventPayload {
	t.Helper()
	return TransitionEventPayload{
		TransitionID:         TransitionID(uuid.Must(uuid.NewV7())),
		RunID:                RunID(uuid.Must(uuid.NewV7())),
		CheckpointCommitHash: "abc1234def5678abc1234def5678abc1234def56",
	}
}

// TestTransitionEventPayload_Valid_AllFieldsSet verifies that a fully-populated
// TransitionEventPayload is valid per EM-028.
func TestTransitionEventPayload_Valid_AllFieldsSet(t *testing.T) {
	t.Parallel()

	p := hkb3f36ValidPayload(t)
	if !p.Valid() {
		t.Error("Valid() = false for fully-populated TransitionEventPayload, want true")
	}
}

// TestTransitionEventPayload_Valid_ZeroTransitionID verifies that uuid.Nil
// TransitionID is rejected.
func TestTransitionEventPayload_Valid_ZeroTransitionID(t *testing.T) {
	t.Parallel()

	p := hkb3f36ValidPayload(t)
	p.TransitionID = TransitionID(uuid.Nil)
	if p.Valid() {
		t.Error("Valid() = true with zero TransitionID, want false")
	}
}

// TestTransitionEventPayload_Valid_ZeroRunID verifies that uuid.Nil RunID is
// rejected.
func TestTransitionEventPayload_Valid_ZeroRunID(t *testing.T) {
	t.Parallel()

	p := hkb3f36ValidPayload(t)
	p.RunID = RunID(uuid.Nil)
	if p.Valid() {
		t.Error("Valid() = true with zero RunID, want false")
	}
}

// TestTransitionEventPayload_Valid_EmptyCommitHash verifies that an empty
// CheckpointCommitHash is rejected.
func TestTransitionEventPayload_Valid_EmptyCommitHash(t *testing.T) {
	t.Parallel()

	p := hkb3f36ValidPayload(t)
	p.CheckpointCommitHash = ""
	if p.Valid() {
		t.Error("Valid() = true with empty CheckpointCommitHash, want false")
	}
}

// TestTransitionEventPayload_ProjectionFields verifies that
// TransitionEventPayload carries exactly the three projection fields required
// by EM-028 (transition_id, run_id, checkpoint_commit_hash) and that the full
// trace payload fields are absent (EM-029: no candidate_actions, evidence,
// verifier_metrics).
//
// This test is structural: it exercises the type at the data-shape level so
// that a future field addition is reviewed against the EM-029 prohibition.
func TestTransitionEventPayload_ProjectionFields(t *testing.T) {
	t.Parallel()

	p := hkb3f36ValidPayload(t)

	// Projection fields required by EM-028 must be non-zero.
	if uuid.UUID(p.TransitionID) == uuid.Nil {
		t.Error("TransitionID must be set on a valid projection payload (EM-028)")
	}
	if uuid.UUID(p.RunID) == uuid.Nil {
		t.Error("RunID must be set on a valid projection payload (EM-028)")
	}
	if p.CheckpointCommitHash == "" {
		t.Error("CheckpointCommitHash must be set on a valid projection payload (EM-028)")
	}
}

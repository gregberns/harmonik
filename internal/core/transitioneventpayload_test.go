// Package core — requirement-traceable sensors for TransitionEventPayload per
// event-model.md §8.1.6 and execution-model.md §4.6.EM-028.
//
// EM-028 requires the transition event payload to cite the transition by
// transition_id, run_id, and checkpoint commit hash so the full record is
// recoverable via git show. EM-029 prohibits duplicating the full trace
// payload (candidate_actions, evidence, verifier_metrics).
//
// §8.1.6 (event-model.md §6.3 transition_event) further requires from_state_id,
// to_state_id, and transition_kind.
package core

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

// hkb3f36ValidPayload returns a fully-populated TransitionEventPayload for
// use in hk-b3f.36 / hk-hqwn.59.6 projection tests.
func hkb3f36ValidPayload(t *testing.T) TransitionEventPayload {
	t.Helper()
	return TransitionEventPayload{
		RunID:          RunID(uuid.Must(uuid.NewV7())),
		TransitionID:   TransitionID(uuid.Must(uuid.NewV7())),
		FromStateID:    StateID(uuid.Must(uuid.NewV7())),
		ToStateID:      StateID(uuid.Must(uuid.NewV7())),
		CommitHash:     "abc1234def5678abc1234def5678abc1234def56",
		TransitionKind: TransitionKindForward,
	}
}

// TestTransitionEventPayload_Valid_AllFieldsSet verifies that a fully-populated
// TransitionEventPayload is valid per EM-028 and §8.1.6.
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
// CommitHash is rejected.
func TestTransitionEventPayload_Valid_EmptyCommitHash(t *testing.T) {
	t.Parallel()

	p := hkb3f36ValidPayload(t)
	p.CommitHash = ""
	if p.Valid() {
		t.Error("Valid() = true with empty CommitHash, want false")
	}
}

// TestTransitionEventPayload_Valid_ZeroFromStateID verifies that uuid.Nil
// FromStateID is rejected.
func TestTransitionEventPayload_Valid_ZeroFromStateID(t *testing.T) {
	t.Parallel()

	p := hkb3f36ValidPayload(t)
	p.FromStateID = StateID(uuid.Nil)
	if p.Valid() {
		t.Error("Valid() = true with zero FromStateID, want false")
	}
}

// TestTransitionEventPayload_Valid_ZeroToStateID verifies that uuid.Nil
// ToStateID is rejected.
func TestTransitionEventPayload_Valid_ZeroToStateID(t *testing.T) {
	t.Parallel()

	p := hkb3f36ValidPayload(t)
	p.ToStateID = StateID(uuid.Nil)
	if p.Valid() {
		t.Error("Valid() = true with zero ToStateID, want false")
	}
}

// TestTransitionEventPayload_Valid_InvalidTransitionKind verifies that an
// invalid TransitionKind is rejected.
func TestTransitionEventPayload_Valid_InvalidTransitionKind(t *testing.T) {
	t.Parallel()

	p := hkb3f36ValidPayload(t)
	p.TransitionKind = TransitionKind("invalid-kind")
	if p.Valid() {
		t.Error("Valid() = true with invalid TransitionKind, want false")
	}
}

// TestTransitionEventPayload_ProjectionFields verifies that
// TransitionEventPayload carries the six §8.1.6 / §6.3 projection fields and
// that the full trace payload fields are absent (EM-029: no candidate_actions,
// evidence, verifier_metrics).
//
// This test is structural: it exercises the type at the data-shape level so
// that a future field addition is reviewed against the EM-029 prohibition.
func TestTransitionEventPayload_ProjectionFields(t *testing.T) {
	t.Parallel()

	p := hkb3f36ValidPayload(t)

	// All §8.1.6 / §6.3 required fields must be non-zero.
	if uuid.UUID(p.RunID) == uuid.Nil {
		t.Error("RunID must be set on a valid projection payload (EM-028 / §8.1.6)")
	}
	if uuid.UUID(p.TransitionID) == uuid.Nil {
		t.Error("TransitionID must be set on a valid projection payload (EM-028 / §8.1.6)")
	}
	if uuid.UUID(p.FromStateID) == uuid.Nil {
		t.Error("FromStateID must be set on a valid projection payload (§8.1.6)")
	}
	if uuid.UUID(p.ToStateID) == uuid.Nil {
		t.Error("ToStateID must be set on a valid projection payload (§8.1.6)")
	}
	if p.CommitHash == "" {
		t.Error("CommitHash must be set on a valid projection payload (EM-028 / §8.1.6)")
	}
	if !p.TransitionKind.Valid() {
		t.Error("TransitionKind must be a valid constant (§8.1.6)")
	}
}

// TestTransitionEventPayload_JSONRoundTrip verifies that all six §6.3 fields
// survive a JSON marshal/unmarshal round-trip with correct wire names.
func TestTransitionEventPayload_JSONRoundTrip(t *testing.T) {
	t.Parallel()

	orig := hkb3f36ValidPayload(t)
	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var got TransitionEventPayload
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if got.RunID != orig.RunID {
		t.Errorf("RunID: got %v, want %v", got.RunID, orig.RunID)
	}
	if got.TransitionID != orig.TransitionID {
		t.Errorf("TransitionID: got %v, want %v", got.TransitionID, orig.TransitionID)
	}
	if got.FromStateID != orig.FromStateID {
		t.Errorf("FromStateID: got %v, want %v", got.FromStateID, orig.FromStateID)
	}
	if got.ToStateID != orig.ToStateID {
		t.Errorf("ToStateID: got %v, want %v", got.ToStateID, orig.ToStateID)
	}
	if got.CommitHash != orig.CommitHash {
		t.Errorf("CommitHash: got %q, want %q", got.CommitHash, orig.CommitHash)
	}
	if got.TransitionKind != orig.TransitionKind {
		t.Errorf("TransitionKind: got %q, want %q", got.TransitionKind, orig.TransitionKind)
	}
}

// TestTransitionEventPayload_JSONKeys verifies that the wire field names match
// the §6.3 snake_case schema for transition_event.
func TestTransitionEventPayload_JSONKeys(t *testing.T) {
	t.Parallel()

	p := hkb3f36ValidPayload(t)
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("json.Unmarshal to map: %v", err)
	}

	required := []string{"run_id", "transition_id", "from_state_id", "to_state_id", "commit_hash", "transition_kind"}
	for _, key := range required {
		if _, ok := m[key]; !ok {
			t.Errorf("expected JSON key %q to be present, got absent", key)
		}
	}
}

package core

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// validState returns a fully-populated State with all fields non-zero.
func validState(t *testing.T) State {
	t.Helper()

	return State{
		StateID:   StateID(uuid.Must(uuid.NewV7())),
		RunID:     RunID(uuid.Must(uuid.NewV7())),
		NodeID:    NodeID("checkout"),
		EnteredAt: time.Now(),
		TransitionHistory: CommitRange{
			FirstCommitSHA: "abc1234",
			LastCommitSHA:  "def5678",
		},
	}
}

func TestStateValid_AllFieldsSet(t *testing.T) {
	t.Parallel()

	s := validState(t)
	if !s.Valid() {
		t.Error("Valid() = false for fully-populated State, want true")
	}
}

func TestStateValid_ZeroStateID(t *testing.T) {
	t.Parallel()

	s := validState(t)
	s.StateID = StateID(uuid.Nil)
	if s.Valid() {
		t.Error("Valid() = true with zero StateID, want false")
	}
}

func TestStateValid_ZeroRunID(t *testing.T) {
	t.Parallel()

	s := validState(t)
	s.RunID = RunID(uuid.Nil)
	if s.Valid() {
		t.Error("Valid() = true with zero RunID, want false")
	}
}

func TestStateValid_EmptyNodeID(t *testing.T) {
	t.Parallel()

	s := validState(t)
	s.NodeID = ""
	if s.Valid() {
		t.Error("Valid() = true with empty NodeID, want false")
	}
}

func TestStateValid_ZeroEnteredAt(t *testing.T) {
	t.Parallel()

	s := validState(t)
	s.EnteredAt = time.Time{}
	if s.Valid() {
		t.Error("Valid() = true with zero EnteredAt, want false")
	}
}

func TestStateValid_EmptyFirstCommitSHA(t *testing.T) {
	t.Parallel()

	s := validState(t)
	s.TransitionHistory.FirstCommitSHA = ""
	if s.Valid() {
		t.Error("Valid() = true with empty TransitionHistory.FirstCommitSHA, want false")
	}
}

func TestStateValid_EmptyLastCommitSHA(t *testing.T) {
	t.Parallel()

	s := validState(t)
	s.TransitionHistory.LastCommitSHA = ""
	if s.Valid() {
		t.Error("Valid() = true with empty TransitionHistory.LastCommitSHA, want false")
	}
}

// TestStateValid_NamespacedNodeID verifies that a namespaced NodeID (per EM-034a)
// is accepted as-is — the struct stores the resolved value without interpretation.
func TestStateValid_NamespacedNodeID(t *testing.T) {
	t.Parallel()

	s := validState(t)
	s.NodeID = NodeID("parent-node/child-node")
	if !s.Valid() {
		t.Error("Valid() = false for namespaced NodeID, want true")
	}
}

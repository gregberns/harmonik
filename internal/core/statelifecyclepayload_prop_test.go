package core

// Property tests for the Valid() methods in statelifecyclepayload.go.
//
// Naming: TestProp_* per testing.md §Decisions #10.
// File:   *_prop_test.go per testing.md §Property layer.
//
// Bead ref: hk-z02yj (part of hk-j3hrn core coverage uplift).

import (
	"testing"

	"github.com/google/uuid"
	"pgregory.net/rapid"
)

// ---------------------------------------------------------------------------
// StateEnteredPayload
// ---------------------------------------------------------------------------

func TestProp_StateEnteredPayload_Valid_AcceptsFullPayload(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := StateEnteredPayload{
			RunID:     RunID(drawNonNilUUID(rt, "run_id")),
			StateID:   StateID(drawNonNilUUID(rt, "state_id")),
			NodeID:    NodeID(rapid.StringN(1, 64, -1).Draw(rt, "node_id")),
			EnteredAt: rapid.StringN(1, 64, -1).Draw(rt, "entered_at"),
		}
		if !p.Valid() {
			rt.Errorf("Valid() = false for fully-populated StateEnteredPayload, want true")
		}
	})
}

func TestProp_StateEnteredPayload_Valid_RejectsNilRunID(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := StateEnteredPayload{
			RunID:     RunID(uuid.Nil),
			StateID:   StateID(drawNonNilUUID(rt, "state_id")),
			NodeID:    NodeID(rapid.StringN(1, 64, -1).Draw(rt, "node_id")),
			EnteredAt: rapid.StringN(1, 64, -1).Draw(rt, "entered_at"),
		}
		if p.Valid() {
			rt.Errorf("Valid() = true with nil RunID, want false")
		}
	})
}

func TestProp_StateEnteredPayload_Valid_RejectsNilStateID(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := StateEnteredPayload{
			RunID:     RunID(drawNonNilUUID(rt, "run_id")),
			StateID:   StateID(uuid.Nil),
			NodeID:    NodeID(rapid.StringN(1, 64, -1).Draw(rt, "node_id")),
			EnteredAt: rapid.StringN(1, 64, -1).Draw(rt, "entered_at"),
		}
		if p.Valid() {
			rt.Errorf("Valid() = true with nil StateID, want false")
		}
	})
}

func TestProp_StateEnteredPayload_Valid_RejectsEmptyNodeID(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := StateEnteredPayload{
			RunID:     RunID(drawNonNilUUID(rt, "run_id")),
			StateID:   StateID(drawNonNilUUID(rt, "state_id")),
			NodeID:    "",
			EnteredAt: rapid.StringN(1, 64, -1).Draw(rt, "entered_at"),
		}
		if p.Valid() {
			rt.Errorf("Valid() = true with empty NodeID, want false")
		}
	})
}

func TestProp_StateEnteredPayload_Valid_RejectsEmptyEnteredAt(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := StateEnteredPayload{
			RunID:     RunID(drawNonNilUUID(rt, "run_id")),
			StateID:   StateID(drawNonNilUUID(rt, "state_id")),
			NodeID:    NodeID(rapid.StringN(1, 64, -1).Draw(rt, "node_id")),
			EnteredAt: "",
		}
		if p.Valid() {
			rt.Errorf("Valid() = true with empty EnteredAt, want false")
		}
	})
}

// ---------------------------------------------------------------------------
// StateExitedPayload
// ---------------------------------------------------------------------------

func TestProp_StateExitedPayload_Valid_AcceptsFullPayloadNoTransitionID(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := StateExitedPayload{
			RunID:        RunID(drawNonNilUUID(rt, "run_id")),
			StateID:      StateID(drawNonNilUUID(rt, "state_id")),
			NodeID:       NodeID(rapid.StringN(1, 64, -1).Draw(rt, "node_id")),
			ExitedAt:     rapid.StringN(1, 64, -1).Draw(rt, "exited_at"),
			TransitionID: nil,
		}
		if !p.Valid() {
			rt.Errorf("Valid() = false for StateExitedPayload without TransitionID, want true")
		}
	})
}

func TestProp_StateExitedPayload_Valid_AcceptsNonNilTransitionID(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		tid := TransitionID(drawNonNilUUID(rt, "transition_id"))
		p := StateExitedPayload{
			RunID:        RunID(drawNonNilUUID(rt, "run_id")),
			StateID:      StateID(drawNonNilUUID(rt, "state_id")),
			NodeID:       NodeID(rapid.StringN(1, 64, -1).Draw(rt, "node_id")),
			ExitedAt:     rapid.StringN(1, 64, -1).Draw(rt, "exited_at"),
			TransitionID: &tid,
		}
		if !p.Valid() {
			rt.Errorf("Valid() = false for StateExitedPayload with valid TransitionID, want true")
		}
	})
}

func TestProp_StateExitedPayload_Valid_RejectsNilRunID(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := StateExitedPayload{
			RunID:    RunID(uuid.Nil),
			StateID:  StateID(drawNonNilUUID(rt, "state_id")),
			NodeID:   NodeID(rapid.StringN(1, 64, -1).Draw(rt, "node_id")),
			ExitedAt: rapid.StringN(1, 64, -1).Draw(rt, "exited_at"),
		}
		if p.Valid() {
			rt.Errorf("Valid() = true with nil RunID, want false")
		}
	})
}

func TestProp_StateExitedPayload_Valid_RejectsNilStateID(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := StateExitedPayload{
			RunID:    RunID(drawNonNilUUID(rt, "run_id")),
			StateID:  StateID(uuid.Nil),
			NodeID:   NodeID(rapid.StringN(1, 64, -1).Draw(rt, "node_id")),
			ExitedAt: rapid.StringN(1, 64, -1).Draw(rt, "exited_at"),
		}
		if p.Valid() {
			rt.Errorf("Valid() = true with nil StateID, want false")
		}
	})
}

func TestProp_StateExitedPayload_Valid_RejectsEmptyNodeID(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := StateExitedPayload{
			RunID:    RunID(drawNonNilUUID(rt, "run_id")),
			StateID:  StateID(drawNonNilUUID(rt, "state_id")),
			NodeID:   "",
			ExitedAt: rapid.StringN(1, 64, -1).Draw(rt, "exited_at"),
		}
		if p.Valid() {
			rt.Errorf("Valid() = true with empty NodeID, want false")
		}
	})
}

func TestProp_StateExitedPayload_Valid_RejectsEmptyExitedAt(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := StateExitedPayload{
			RunID:    RunID(drawNonNilUUID(rt, "run_id")),
			StateID:  StateID(drawNonNilUUID(rt, "state_id")),
			NodeID:   NodeID(rapid.StringN(1, 64, -1).Draw(rt, "node_id")),
			ExitedAt: "",
		}
		if p.Valid() {
			rt.Errorf("Valid() = true with empty ExitedAt, want false")
		}
	})
}

func TestProp_StateExitedPayload_Valid_RejectsNilTransitionIDValue(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		nilTID := TransitionID(uuid.Nil)
		p := StateExitedPayload{
			RunID:        RunID(drawNonNilUUID(rt, "run_id")),
			StateID:      StateID(drawNonNilUUID(rt, "state_id")),
			NodeID:       NodeID(rapid.StringN(1, 64, -1).Draw(rt, "node_id")),
			ExitedAt:     rapid.StringN(1, 64, -1).Draw(rt, "exited_at"),
			TransitionID: &nilTID,
		}
		if p.Valid() {
			rt.Errorf("Valid() = true with TransitionID pointing to uuid.Nil, want false")
		}
	})
}

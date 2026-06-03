package core

// Property tests for Valid() methods in nodedispatchpayload.go and
// nodedispatchdecidedpayload.go.
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
// NodeDispatchOrigin
// ---------------------------------------------------------------------------

func TestProp_NodeDispatchOrigin_Valid_AcceptsDeclaredConstants(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		o := rapid.SampledFrom([]NodeDispatchOrigin{
			NodeDispatchOriginWorkflow,
			NodeDispatchOriginReconciliation,
			NodeDispatchOriginOperator,
		}).Draw(rt, "origin")
		if !o.Valid() {
			rt.Errorf("Valid() = false for declared NodeDispatchOrigin constant %q, want true", o)
		}
	})
}

func TestProp_NodeDispatchOrigin_Valid_RejectsArbitraryStrings(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		raw := rapid.StringN(1, 64, -1).Draw(rt, "raw")
		o := NodeDispatchOrigin(raw)
		if o == NodeDispatchOriginWorkflow || o == NodeDispatchOriginReconciliation || o == NodeDispatchOriginOperator {
			return
		}
		if o.Valid() {
			rt.Errorf("Valid() = true for undeclared NodeDispatchOrigin %q, want false", o)
		}
	})
}

// ---------------------------------------------------------------------------
// NodeDispatchRequestedPayload
// ---------------------------------------------------------------------------

func TestProp_NodeDispatchRequestedPayload_Valid_AcceptsFullPayload(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := NodeDispatchRequestedPayload{
			RunID:       RunID(drawNonNilUUID(rt, "run_id")),
			NodeID:      NodeID(rapid.StringN(1, 64, -1).Draw(rt, "node_id")),
			RequestedAt: rapid.StringN(1, 64, -1).Draw(rt, "requested_at"),
			Origin: rapid.SampledFrom([]NodeDispatchOrigin{
				NodeDispatchOriginWorkflow,
				NodeDispatchOriginReconciliation,
				NodeDispatchOriginOperator,
			}).Draw(rt, "origin"),
		}
		if !p.Valid() {
			rt.Errorf("Valid() = false for fully-populated NodeDispatchRequestedPayload, want true")
		}
	})
}

func TestProp_NodeDispatchRequestedPayload_Valid_RejectsNilRunID(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := NodeDispatchRequestedPayload{
			RunID:       RunID(uuid.Nil),
			NodeID:      NodeID(rapid.StringN(1, 64, -1).Draw(rt, "node_id")),
			RequestedAt: rapid.StringN(1, 64, -1).Draw(rt, "requested_at"),
			Origin:      NodeDispatchOriginWorkflow,
		}
		if p.Valid() {
			rt.Errorf("Valid() = true with nil RunID, want false")
		}
	})
}

func TestProp_NodeDispatchRequestedPayload_Valid_RejectsEmptyNodeID(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := NodeDispatchRequestedPayload{
			RunID:       RunID(drawNonNilUUID(rt, "run_id")),
			NodeID:      "",
			RequestedAt: rapid.StringN(1, 64, -1).Draw(rt, "requested_at"),
			Origin:      NodeDispatchOriginWorkflow,
		}
		if p.Valid() {
			rt.Errorf("Valid() = true with empty NodeID, want false")
		}
	})
}

func TestProp_NodeDispatchRequestedPayload_Valid_RejectsEmptyRequestedAt(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := NodeDispatchRequestedPayload{
			RunID:       RunID(drawNonNilUUID(rt, "run_id")),
			NodeID:      NodeID(rapid.StringN(1, 64, -1).Draw(rt, "node_id")),
			RequestedAt: "",
			Origin:      NodeDispatchOriginWorkflow,
		}
		if p.Valid() {
			rt.Errorf("Valid() = true with empty RequestedAt, want false")
		}
	})
}

func TestProp_NodeDispatchRequestedPayload_Valid_RejectsInvalidOrigin(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := NodeDispatchRequestedPayload{
			RunID:       RunID(drawNonNilUUID(rt, "run_id")),
			NodeID:      NodeID(rapid.StringN(1, 64, -1).Draw(rt, "node_id")),
			RequestedAt: rapid.StringN(1, 64, -1).Draw(rt, "requested_at"),
			Origin:      NodeDispatchOrigin(""),
		}
		if p.Valid() {
			rt.Errorf("Valid() = true with empty Origin, want false")
		}
	})
}

// ---------------------------------------------------------------------------
// NodeDispatchDecidedPayload
// ---------------------------------------------------------------------------

func TestProp_NodeDispatchDecidedPayload_Valid_AcceptsAdvanceOutcome(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := NodeDispatchDecidedPayload{
			RunID:      RunID(drawNonNilUUID(rt, "run_id")),
			FromNodeID: rapid.StringN(1, 64, -1).Draw(rt, "from_node"),
			NextNodeID: rapid.StringN(1, 64, -1).Draw(rt, "next_node"),
			IsTerminal: false,
			Failed:     false,
		}
		if !p.Valid() {
			rt.Errorf("Valid() = false for advance outcome NodeDispatchDecidedPayload, want true")
		}
	})
}

func TestProp_NodeDispatchDecidedPayload_Valid_AcceptsTerminalOutcome(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := NodeDispatchDecidedPayload{
			RunID:      RunID(drawNonNilUUID(rt, "run_id")),
			FromNodeID: rapid.StringN(1, 64, -1).Draw(rt, "from_node"),
			IsTerminal: true,
			Failed:     false,
		}
		if !p.Valid() {
			rt.Errorf("Valid() = false for terminal outcome NodeDispatchDecidedPayload, want true")
		}
	})
}

func TestProp_NodeDispatchDecidedPayload_Valid_AcceptsFailedOutcome(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := NodeDispatchDecidedPayload{
			RunID:      RunID(drawNonNilUUID(rt, "run_id")),
			FromNodeID: rapid.StringN(1, 64, -1).Draw(rt, "from_node"),
			IsTerminal: false,
			Failed:     true,
		}
		if !p.Valid() {
			rt.Errorf("Valid() = false for failed outcome NodeDispatchDecidedPayload, want true")
		}
	})
}

func TestProp_NodeDispatchDecidedPayload_Valid_RejectsNilRunID(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := NodeDispatchDecidedPayload{
			RunID:      RunID(uuid.Nil),
			FromNodeID: rapid.StringN(1, 64, -1).Draw(rt, "from_node"),
			IsTerminal: true,
		}
		if p.Valid() {
			rt.Errorf("Valid() = true with nil RunID, want false")
		}
	})
}

func TestProp_NodeDispatchDecidedPayload_Valid_RejectsEmptyFromNodeID(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := NodeDispatchDecidedPayload{
			RunID:      RunID(drawNonNilUUID(rt, "run_id")),
			FromNodeID: "",
			IsTerminal: true,
		}
		if p.Valid() {
			rt.Errorf("Valid() = true with empty FromNodeID, want false")
		}
	})
}

func TestProp_NodeDispatchDecidedPayload_Valid_RejectsNoOutcome(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// NextNodeID empty, IsTerminal false, Failed false → no outcome set.
		p := NodeDispatchDecidedPayload{
			RunID:      RunID(drawNonNilUUID(rt, "run_id")),
			FromNodeID: rapid.StringN(1, 64, -1).Draw(rt, "from_node"),
			IsTerminal: false,
			Failed:     false,
		}
		if p.Valid() {
			rt.Errorf("Valid() = true with no outcome set, want false")
		}
	})
}

func TestProp_NodeDispatchDecidedPayload_Valid_RejectsMultipleOutcomes(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// NextNodeID non-empty AND IsTerminal → two outcomes set simultaneously.
		p := NodeDispatchDecidedPayload{
			RunID:      RunID(drawNonNilUUID(rt, "run_id")),
			FromNodeID: rapid.StringN(1, 64, -1).Draw(rt, "from_node"),
			NextNodeID: rapid.StringN(1, 64, -1).Draw(rt, "next_node"),
			IsTerminal: true,
			Failed:     false,
		}
		if p.Valid() {
			rt.Errorf("Valid() = true with NextNodeID+IsTerminal both set, want false")
		}
	})
}

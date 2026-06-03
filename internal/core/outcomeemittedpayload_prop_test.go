package core

// Property tests for OutcomeEmittedPayload.Valid() in outcomeemittedpayload.go.
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

// validOutcomeStatuses holds the declared OutcomeStatus constants for rapid.SampledFrom.
var validOutcomeStatuses = []OutcomeStatus{
	OutcomeStatusSuccess,
	OutcomeStatusFail,
	OutcomeStatusRetry,
	OutcomeStatusPartialSuccess,
}

func TestProp_OutcomeEmittedPayload_Valid_AcceptsFullPayloadNoPreferredLabel(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := OutcomeEmittedPayload{
			RunID:         RunID(drawNonNilUUID(rt, "run_id")),
			SessionID:     SessionID(rapid.StringN(1, 64, -1).Draw(rt, "session_id")),
			NodeID:        NodeID(rapid.StringN(1, 64, -1).Draw(rt, "node_id")),
			OutcomeStatus: rapid.SampledFrom(validOutcomeStatuses).Draw(rt, "outcome_status"),
		}
		if !p.Valid() {
			rt.Errorf("Valid() = false for OutcomeEmittedPayload without PreferredLabel, want true")
		}
	})
}

func TestProp_OutcomeEmittedPayload_Valid_AcceptsNonEmptyPreferredLabel(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		label := rapid.StringN(1, 64, -1).Draw(rt, "label")
		p := OutcomeEmittedPayload{
			RunID:          RunID(drawNonNilUUID(rt, "run_id")),
			SessionID:      SessionID(rapid.StringN(1, 64, -1).Draw(rt, "session_id")),
			NodeID:         NodeID(rapid.StringN(1, 64, -1).Draw(rt, "node_id")),
			OutcomeStatus:  rapid.SampledFrom(validOutcomeStatuses).Draw(rt, "outcome_status"),
			PreferredLabel: &label,
		}
		if !p.Valid() {
			rt.Errorf("Valid() = false with non-empty PreferredLabel, want true")
		}
	})
}

func TestProp_OutcomeEmittedPayload_Valid_RejectsNilRunID(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := OutcomeEmittedPayload{
			RunID:         RunID(uuid.Nil),
			SessionID:     SessionID(rapid.StringN(1, 64, -1).Draw(rt, "session_id")),
			NodeID:        NodeID(rapid.StringN(1, 64, -1).Draw(rt, "node_id")),
			OutcomeStatus: OutcomeStatusSuccess,
		}
		if p.Valid() {
			rt.Errorf("Valid() = true with nil RunID, want false")
		}
	})
}

func TestProp_OutcomeEmittedPayload_Valid_RejectsEmptySessionID(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := OutcomeEmittedPayload{
			RunID:         RunID(drawNonNilUUID(rt, "run_id")),
			SessionID:     "",
			NodeID:        NodeID(rapid.StringN(1, 64, -1).Draw(rt, "node_id")),
			OutcomeStatus: OutcomeStatusSuccess,
		}
		if p.Valid() {
			rt.Errorf("Valid() = true with empty SessionID, want false")
		}
	})
}

func TestProp_OutcomeEmittedPayload_Valid_RejectsEmptyNodeID(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := OutcomeEmittedPayload{
			RunID:         RunID(drawNonNilUUID(rt, "run_id")),
			SessionID:     SessionID(rapid.StringN(1, 64, -1).Draw(rt, "session_id")),
			NodeID:        "",
			OutcomeStatus: OutcomeStatusSuccess,
		}
		if p.Valid() {
			rt.Errorf("Valid() = true with empty NodeID, want false")
		}
	})
}

func TestProp_OutcomeEmittedPayload_Valid_RejectsInvalidOutcomeStatus(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := OutcomeEmittedPayload{
			RunID:         RunID(drawNonNilUUID(rt, "run_id")),
			SessionID:     SessionID(rapid.StringN(1, 64, -1).Draw(rt, "session_id")),
			NodeID:        NodeID(rapid.StringN(1, 64, -1).Draw(rt, "node_id")),
			OutcomeStatus: OutcomeStatus(""),
		}
		if p.Valid() {
			rt.Errorf("Valid() = true with empty OutcomeStatus, want false")
		}
	})
}

func TestProp_OutcomeEmittedPayload_Valid_RejectsEmptyPreferredLabel(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		empty := ""
		p := OutcomeEmittedPayload{
			RunID:          RunID(drawNonNilUUID(rt, "run_id")),
			SessionID:      SessionID(rapid.StringN(1, 64, -1).Draw(rt, "session_id")),
			NodeID:         NodeID(rapid.StringN(1, 64, -1).Draw(rt, "node_id")),
			OutcomeStatus:  OutcomeStatusSuccess,
			PreferredLabel: &empty,
		}
		if p.Valid() {
			rt.Errorf("Valid() = true with PreferredLabel pointing to empty string, want false")
		}
	})
}

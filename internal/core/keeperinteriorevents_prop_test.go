package core

// keeperinteriorevents_prop_test.go — property tests for the §8.20
// session-keeper interior payload Valid() methods, plus a DecodePayloadStrict
// coverage test (codename:session-restart-substrate).
//
// Naming: TestProp_* per testing.md §Decisions #10. Approach mirrors
// reconciliationevents_hqwn59_prop_test.go: build a valid payload, flip exactly
// one required field to its zero/invalid value, assert Valid()==false;
// all-valid -> true.

import (
	"bytes"
	"encoding/json"
	"testing"

	"pgregory.net/rapid" //nolint:depguard // pgregory.net/rapid is the established property-test lib for in-package core tests (26 precedents); see COORD c065
)

var keeperModelDoneSources = []string{"idle_marker", "transcript_turn", "timeout"}

// ============================================================
// SessionKeeperHandoffWrittenPayload
// ============================================================

func TestProp_SessionKeeperHandoffWrittenPayload_AllValidAccepted(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := SessionKeeperHandoffWrittenPayload{
			AgentName: drawNonEmptyString(rt, "agent_name"),
			CycleID:   drawNonEmptyString(rt, "cycle_id"),
		}
		if !p.Valid() {
			rt.Error("Valid() == false for well-formed SessionKeeperHandoffWrittenPayload")
		}
	})
}

func TestProp_SessionKeeperHandoffWrittenPayload_EmptyAgentNameRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := SessionKeeperHandoffWrittenPayload{
			AgentName: "",
			CycleID:   drawNonEmptyString(rt, "cycle_id"),
		}
		if p.Valid() {
			rt.Error("Valid() should be false with empty AgentName")
		}
	})
}

func TestProp_SessionKeeperHandoffWrittenPayload_EmptyCycleIDRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := SessionKeeperHandoffWrittenPayload{
			AgentName: drawNonEmptyString(rt, "agent_name"),
			CycleID:   "",
		}
		if p.Valid() {
			rt.Error("Valid() should be false with empty CycleID")
		}
	})
}

// ============================================================
// SessionKeeperModelDonePayload
// ============================================================

func TestProp_SessionKeeperModelDonePayload_AllValidAccepted(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := SessionKeeperModelDonePayload{
			AgentName: drawNonEmptyString(rt, "agent_name"),
			CycleID:   drawNonEmptyString(rt, "cycle_id"),
			Source:    rapid.SampledFrom(keeperModelDoneSources).Draw(rt, "source"),
		}
		if !p.Valid() {
			rt.Error("Valid() == false for well-formed SessionKeeperModelDonePayload")
		}
	})
}

func TestProp_SessionKeeperModelDonePayload_EmptyAgentNameRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := SessionKeeperModelDonePayload{
			AgentName: "",
			CycleID:   drawNonEmptyString(rt, "cycle_id"),
			Source:    rapid.SampledFrom(keeperModelDoneSources).Draw(rt, "source"),
		}
		if p.Valid() {
			rt.Error("Valid() should be false with empty AgentName")
		}
	})
}

func TestProp_SessionKeeperModelDonePayload_EmptyCycleIDRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := SessionKeeperModelDonePayload{
			AgentName: drawNonEmptyString(rt, "agent_name"),
			CycleID:   "",
			Source:    rapid.SampledFrom(keeperModelDoneSources).Draw(rt, "source"),
		}
		if p.Valid() {
			rt.Error("Valid() should be false with empty CycleID")
		}
	})
}

// ============================================================
// SessionKeeperClearSentPayload
// ============================================================

func TestProp_SessionKeeperClearSentPayload_AllValidAccepted(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := SessionKeeperClearSentPayload{
			AgentName: drawNonEmptyString(rt, "agent_name"),
			CycleID:   drawNonEmptyString(rt, "cycle_id"),
			Attempt:   rapid.IntRange(1, 100).Draw(rt, "attempt"),
		}
		if !p.Valid() {
			rt.Error("Valid() == false for well-formed SessionKeeperClearSentPayload")
		}
	})
}

func TestProp_SessionKeeperClearSentPayload_EmptyCycleIDRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := SessionKeeperClearSentPayload{
			AgentName: drawNonEmptyString(rt, "agent_name"),
			CycleID:   "",
			Attempt:   rapid.IntRange(1, 100).Draw(rt, "attempt"),
		}
		if p.Valid() {
			rt.Error("Valid() should be false with empty CycleID")
		}
	})
}

func TestProp_SessionKeeperClearSentPayload_ZeroAttemptRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := SessionKeeperClearSentPayload{
			AgentName: drawNonEmptyString(rt, "agent_name"),
			CycleID:   drawNonEmptyString(rt, "cycle_id"),
			Attempt:   rapid.IntRange(-100, 0).Draw(rt, "attempt"),
		}
		if p.Valid() {
			rt.Errorf("Valid() should be false for Attempt=%d (must be >= 1)", p.Attempt)
		}
	})
}

// ============================================================
// SessionKeeperNewSessionUpPayload
// ============================================================

func TestProp_SessionKeeperNewSessionUpPayload_AllValidAccepted(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		prev := drawNonEmptyString(rt, "prev_session_id")
		p := SessionKeeperNewSessionUpPayload{
			AgentName:     drawNonEmptyString(rt, "agent_name"),
			CycleID:       drawNonEmptyString(rt, "cycle_id"),
			PrevSessionID: prev,
			NewSessionID:  prev + "-new", // guaranteed non-empty and != prev
		}
		if !p.Valid() {
			rt.Error("Valid() == false for well-formed SessionKeeperNewSessionUpPayload")
		}
	})
}

func TestProp_SessionKeeperNewSessionUpPayload_EmptyCycleIDRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		prev := drawNonEmptyString(rt, "prev_session_id")
		p := SessionKeeperNewSessionUpPayload{
			AgentName:     drawNonEmptyString(rt, "agent_name"),
			CycleID:       "",
			PrevSessionID: prev,
			NewSessionID:  prev + "-new",
		}
		if p.Valid() {
			rt.Error("Valid() should be false with empty CycleID")
		}
	})
}

func TestProp_SessionKeeperNewSessionUpPayload_EmptyNewSessionIDRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := SessionKeeperNewSessionUpPayload{
			AgentName:     drawNonEmptyString(rt, "agent_name"),
			CycleID:       drawNonEmptyString(rt, "cycle_id"),
			PrevSessionID: drawNonEmptyString(rt, "prev_session_id"),
			NewSessionID:  "",
		}
		if p.Valid() {
			rt.Error("Valid() should be false with empty NewSessionID")
		}
	})
}

func TestProp_SessionKeeperNewSessionUpPayload_UnchangedSessionIDRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		same := drawNonEmptyString(rt, "session_id")
		p := SessionKeeperNewSessionUpPayload{
			AgentName:     drawNonEmptyString(rt, "agent_name"),
			CycleID:       drawNonEmptyString(rt, "cycle_id"),
			PrevSessionID: same,
			NewSessionID:  same,
		}
		if p.Valid() {
			rt.Error("Valid() should be false when NewSessionID == PrevSessionID")
		}
	})
}

// ============================================================
// DecodePayloadStrict — additive-drift detection (EV-049)
// ============================================================

// TestDecodePayloadStrict_UnknownFieldRejected asserts that a payload carrying an
// extra (unmodeled) field decodes cleanly via DecodePayload but is rejected by
// DecodePayloadStrict, which is the mechanism that surfaces additive writer drift.
func TestDecodePayloadStrict_UnknownFieldRejected(t *testing.T) {
	// A well-formed §8.20 payload plus an unmodeled "surprise_field".
	raw := []byte(`{"agent_name":"a","cycle_id":"cyc-1","attempt":1,"surprise_field":"drift"}`)
	ev := minimalEvent(t, "session_keeper_clear_sent", raw)

	// Tolerant path: DecodePayload silently ignores the unknown field.
	got, err := ev.DecodePayload()
	if err != nil {
		t.Fatalf("DecodePayload: unexpected error on extra field: %v", err)
	}
	want := &SessionKeeperClearSentPayload{AgentName: "a", CycleID: "cyc-1", Attempt: 1}
	if !jsonEqual(t, got, want) {
		t.Errorf("DecodePayload dropped/altered modeled fields: got %+v, want %+v", got, want)
	}

	// Strict path: DecodePayloadStrict rejects the unknown field.
	if _, err := ev.DecodePayloadStrict(); err == nil {
		t.Error("DecodePayloadStrict: expected error on unknown field, got nil")
	}
}

// TestDecodePayloadStrict_WellFormedAccepted asserts DecodePayloadStrict decodes a
// clean payload without error (no false positives on well-formed input).
func TestDecodePayloadStrict_WellFormedAccepted(t *testing.T) {
	raw := []byte(`{"agent_name":"a","cycle_id":"cyc-1","attempt":2}`)
	ev := minimalEvent(t, "session_keeper_clear_sent", raw)
	got, err := ev.DecodePayloadStrict()
	if err != nil {
		t.Fatalf("DecodePayloadStrict: unexpected error on clean payload: %v", err)
	}
	want := &SessionKeeperClearSentPayload{AgentName: "a", CycleID: "cyc-1", Attempt: 2}
	if !jsonEqual(t, got, want) {
		t.Errorf("DecodePayloadStrict value mismatch: got %+v, want %+v", got, want)
	}
}

// jsonEqual reports whether a and b marshal to identical JSON.
func jsonEqual(t *testing.T, a, b any) bool {
	t.Helper()
	ab, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("json.Marshal(a): %v", err)
	}
	bb, err := json.Marshal(b)
	if err != nil {
		t.Fatalf("json.Marshal(b): %v", err)
	}
	return bytes.Equal(ab, bb)
}

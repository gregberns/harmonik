package core

// Property tests for the Valid() methods in handlerpauseevents_ifqnj.go.
//
// Naming: TestProp_* per testing.md §Decisions #10.
// File:   *_prop_test.go per testing.md §Property layer.
//
// Bead ref: hk-z02yj (part of hk-j3hrn core coverage uplift).

import (
	"testing"

	"pgregory.net/rapid"
)

// ---------------------------------------------------------------------------
// HandlerPauseCause
// ---------------------------------------------------------------------------

func TestProp_HandlerPauseCause_Valid_AcceptsFullCause(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		c := drawValidHandlerPauseCause(rt, "cause")
		if !c.Valid() {
			rt.Errorf("Valid() = false for fully-populated HandlerPauseCause, want true")
		}
	})
}

func TestProp_HandlerPauseCause_Valid_RejectsInvalidFailureClass(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		c := HandlerPauseCause{
			FailureClass: FailureClass(""),
			SubReason:    rapid.StringN(1, 64, -1).Draw(rt, "sub_reason"),
			SourceRunID:  rapid.StringN(1, 64, -1).Draw(rt, "src_run"),
			SourceBeadID: rapid.StringN(1, 64, -1).Draw(rt, "src_bead"),
			TrippedAt:    rapid.StringN(1, 64, -1).Draw(rt, "tripped_at"),
		}
		if c.Valid() {
			rt.Errorf("Valid() = true with empty FailureClass, want false")
		}
	})
}

func TestProp_HandlerPauseCause_Valid_RejectsEmptySubReason(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		c := HandlerPauseCause{
			FailureClass: drawValidFailureClass(rt, "fc"),
			SubReason:    "",
			SourceRunID:  rapid.StringN(1, 64, -1).Draw(rt, "src_run"),
			SourceBeadID: rapid.StringN(1, 64, -1).Draw(rt, "src_bead"),
			TrippedAt:    rapid.StringN(1, 64, -1).Draw(rt, "tripped_at"),
		}
		if c.Valid() {
			rt.Errorf("Valid() = true with empty SubReason, want false")
		}
	})
}

func TestProp_HandlerPauseCause_Valid_RejectsEmptySourceRunID(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		c := HandlerPauseCause{
			FailureClass: drawValidFailureClass(rt, "fc"),
			SubReason:    rapid.StringN(1, 64, -1).Draw(rt, "sub_reason"),
			SourceRunID:  "",
			SourceBeadID: rapid.StringN(1, 64, -1).Draw(rt, "src_bead"),
			TrippedAt:    rapid.StringN(1, 64, -1).Draw(rt, "tripped_at"),
		}
		if c.Valid() {
			rt.Errorf("Valid() = true with empty SourceRunID, want false")
		}
	})
}

func TestProp_HandlerPauseCause_Valid_RejectsEmptySourceBeadID(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		c := HandlerPauseCause{
			FailureClass: drawValidFailureClass(rt, "fc"),
			SubReason:    rapid.StringN(1, 64, -1).Draw(rt, "sub_reason"),
			SourceRunID:  rapid.StringN(1, 64, -1).Draw(rt, "src_run"),
			SourceBeadID: "",
			TrippedAt:    rapid.StringN(1, 64, -1).Draw(rt, "tripped_at"),
		}
		if c.Valid() {
			rt.Errorf("Valid() = true with empty SourceBeadID, want false")
		}
	})
}

func TestProp_HandlerPauseCause_Valid_RejectsEmptyTrippedAt(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		c := HandlerPauseCause{
			FailureClass: drawValidFailureClass(rt, "fc"),
			SubReason:    rapid.StringN(1, 64, -1).Draw(rt, "sub_reason"),
			SourceRunID:  rapid.StringN(1, 64, -1).Draw(rt, "src_run"),
			SourceBeadID: rapid.StringN(1, 64, -1).Draw(rt, "src_bead"),
			TrippedAt:    "",
		}
		if c.Valid() {
			rt.Errorf("Valid() = true with empty TrippedAt, want false")
		}
	})
}

// ---------------------------------------------------------------------------
// HandlerResumedBy
// ---------------------------------------------------------------------------

func TestProp_HandlerResumedBy_Valid_AcceptsDeclaredConstants(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		b := rapid.SampledFrom([]HandlerResumedBy{
			HandlerResumedByOperator,
			HandlerResumedByAutoBackoff,
			HandlerResumedBySignal,
		}).Draw(rt, "by")
		if !b.Valid() {
			rt.Errorf("Valid() = false for declared HandlerResumedBy constant %q, want true", b)
		}
	})
}

func TestProp_HandlerResumedBy_Valid_RejectsArbitraryStrings(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		raw := rapid.StringN(1, 64, -1).Draw(rt, "raw")
		b := HandlerResumedBy(raw)
		// Only reject if not one of the declared constants.
		if b == HandlerResumedByOperator || b == HandlerResumedByAutoBackoff || b == HandlerResumedBySignal {
			return
		}
		if b.Valid() {
			rt.Errorf("Valid() = true for undeclared HandlerResumedBy %q, want false", b)
		}
	})
}

// ---------------------------------------------------------------------------
// HandlerPausedPayload
// ---------------------------------------------------------------------------

func TestProp_HandlerPausedPayload_Valid_AcceptsFullPayload(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := HandlerPausedPayload{
			AgentType:     drawValidAgentType(rt, "agent_type"),
			Cause:         drawValidHandlerPauseCause(rt, "cause"),
			InFlightCount: rapid.IntRange(0, 100).Draw(rt, "in_flight"),
			PausedEpoch:   rapid.IntRange(1, 1000).Draw(rt, "epoch"),
		}
		if !p.Valid() {
			rt.Errorf("Valid() = false for fully-populated HandlerPausedPayload, want true")
		}
	})
}

func TestProp_HandlerPausedPayload_Valid_RejectsInvalidAgentType(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := HandlerPausedPayload{
			AgentType:     AgentType(""),
			Cause:         drawValidHandlerPauseCause(rt, "cause"),
			InFlightCount: rapid.IntRange(0, 100).Draw(rt, "in_flight"),
			PausedEpoch:   rapid.IntRange(1, 1000).Draw(rt, "epoch"),
		}
		if p.Valid() {
			rt.Errorf("Valid() = true with empty AgentType, want false")
		}
	})
}

func TestProp_HandlerPausedPayload_Valid_RejectsNegativeInFlightCount(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := HandlerPausedPayload{
			AgentType:     drawValidAgentType(rt, "agent_type"),
			Cause:         drawValidHandlerPauseCause(rt, "cause"),
			InFlightCount: rapid.IntRange(-100, -1).Draw(rt, "in_flight"),
			PausedEpoch:   rapid.IntRange(1, 1000).Draw(rt, "epoch"),
		}
		if p.Valid() {
			rt.Errorf("Valid() = true with negative InFlightCount, want false")
		}
	})
}

func TestProp_HandlerPausedPayload_Valid_RejectsZeroPausedEpoch(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := HandlerPausedPayload{
			AgentType:     drawValidAgentType(rt, "agent_type"),
			Cause:         drawValidHandlerPauseCause(rt, "cause"),
			InFlightCount: rapid.IntRange(0, 100).Draw(rt, "in_flight"),
			PausedEpoch:   rapid.IntRange(-100, 0).Draw(rt, "epoch"),
		}
		if p.Valid() {
			rt.Errorf("Valid() = true with PausedEpoch <= 0, want false")
		}
	})
}

// ---------------------------------------------------------------------------
// HandlerResumedPayload
// ---------------------------------------------------------------------------

func TestProp_HandlerResumedPayload_Valid_AcceptsFullPayload(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := HandlerResumedPayload{
			AgentType:   drawValidAgentType(rt, "agent_type"),
			By:          rapid.SampledFrom([]HandlerResumedBy{HandlerResumedByOperator, HandlerResumedByAutoBackoff, HandlerResumedBySignal}).Draw(rt, "by"),
			PriorCause:  drawValidHandlerPauseCause(rt, "prior_cause"),
			PausedEpoch: rapid.IntRange(1, 1000).Draw(rt, "epoch"),
		}
		if !p.Valid() {
			rt.Errorf("Valid() = false for fully-populated HandlerResumedPayload, want true")
		}
	})
}

func TestProp_HandlerResumedPayload_Valid_RejectsInvalidBy(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := HandlerResumedPayload{
			AgentType:   drawValidAgentType(rt, "agent_type"),
			By:          HandlerResumedBy(""),
			PriorCause:  drawValidHandlerPauseCause(rt, "prior_cause"),
			PausedEpoch: rapid.IntRange(1, 1000).Draw(rt, "epoch"),
		}
		if p.Valid() {
			rt.Errorf("Valid() = true with empty By, want false")
		}
	})
}

func TestProp_HandlerResumedPayload_Valid_RejectsZeroPausedEpoch(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := HandlerResumedPayload{
			AgentType:   drawValidAgentType(rt, "agent_type"),
			By:          HandlerResumedByOperator,
			PriorCause:  drawValidHandlerPauseCause(rt, "prior_cause"),
			PausedEpoch: rapid.IntRange(-100, 0).Draw(rt, "epoch"),
		}
		if p.Valid() {
			rt.Errorf("Valid() = true with PausedEpoch <= 0, want false")
		}
	})
}

// ---------------------------------------------------------------------------
// QueueItemHeldForHandlerPausePayload
// ---------------------------------------------------------------------------

func TestProp_QueueItemHeldForHandlerPausePayload_Valid_AcceptsFullPayload(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := QueueItemHeldForHandlerPausePayload{
			BeadID:      rapid.StringN(1, 64, -1).Draw(rt, "bead_id"),
			AgentType:   drawValidAgentType(rt, "agent_type"),
			PausedEpoch: rapid.IntRange(1, 1000).Draw(rt, "epoch"),
		}
		if !p.Valid() {
			rt.Errorf("Valid() = false for fully-populated QueueItemHeldForHandlerPausePayload, want true")
		}
	})
}

func TestProp_QueueItemHeldForHandlerPausePayload_Valid_RejectsEmptyBeadID(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := QueueItemHeldForHandlerPausePayload{
			BeadID:      "",
			AgentType:   drawValidAgentType(rt, "agent_type"),
			PausedEpoch: rapid.IntRange(1, 1000).Draw(rt, "epoch"),
		}
		if p.Valid() {
			rt.Errorf("Valid() = true with empty BeadID, want false")
		}
	})
}

func TestProp_QueueItemHeldForHandlerPausePayload_Valid_RejectsInvalidAgentType(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := QueueItemHeldForHandlerPausePayload{
			BeadID:      rapid.StringN(1, 64, -1).Draw(rt, "bead_id"),
			AgentType:   AgentType(""),
			PausedEpoch: rapid.IntRange(1, 1000).Draw(rt, "epoch"),
		}
		if p.Valid() {
			rt.Errorf("Valid() = true with empty AgentType, want false")
		}
	})
}

func TestProp_QueueItemHeldForHandlerPausePayload_Valid_RejectsZeroPausedEpoch(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := QueueItemHeldForHandlerPausePayload{
			BeadID:      rapid.StringN(1, 64, -1).Draw(rt, "bead_id"),
			AgentType:   drawValidAgentType(rt, "agent_type"),
			PausedEpoch: rapid.IntRange(-100, 0).Draw(rt, "epoch"),
		}
		if p.Valid() {
			rt.Errorf("Valid() = true with PausedEpoch <= 0, want false")
		}
	})
}

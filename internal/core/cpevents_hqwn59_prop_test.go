package core

// cpevents_hqwn59_prop_test.go — property tests for the Valid() methods
// declared in cpevents_hqwn59.go.
//
// Naming: TestProp_* per testing.md §Decisions #10.
// Approach: rapid generator builds a valid payload, flips exactly one required
// field to its zero/invalid value, asserts Valid()==false; all-valid -> true.
//
// Refs: hk-qgzso (property-test coverage uplift for hk-j3hrn core uplift).

import (
	"testing"

	"github.com/google/uuid"
	"pgregory.net/rapid"
)

// ============================================================
// ControlPointsRegisteredPayload
// ============================================================

func TestProp_ControlPointsRegisteredPayload_AllValidAccepted(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := ControlPointsRegisteredPayload{
			Count:     rapid.IntRange(0, 1000).Draw(rt, "count"),
			StartedAt: drawNonEmptyString(rt, "started_at"),
		}
		if !p.Valid() {
			rt.Error("Valid() == false for well-formed ControlPointsRegisteredPayload")
		}
	})
}

func TestProp_ControlPointsRegisteredPayload_NegativeCountRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := ControlPointsRegisteredPayload{
			Count:     rapid.IntRange(-1000, -1).Draw(rt, "count"),
			StartedAt: drawNonEmptyString(rt, "started_at"),
		}
		if p.Valid() {
			rt.Errorf("Valid() should be false for Count=%d (must be >= 0)", p.Count)
		}
	})
}

func TestProp_ControlPointsRegisteredPayload_EmptyStartedAtRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := ControlPointsRegisteredPayload{
			Count:     rapid.IntRange(0, 1000).Draw(rt, "count"),
			StartedAt: "",
		}
		if p.Valid() {
			rt.Error("Valid() should be false with empty StartedAt")
		}
	})
}

// ============================================================
// ControlPointsRegistrationStartedPayload
// ============================================================

func TestProp_ControlPointsRegistrationStartedPayload_AllValidAccepted(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := ControlPointsRegistrationStartedPayload{
			BatchID:   drawNonEmptyString(rt, "batch_id"),
			StartedAt: drawNonEmptyString(rt, "started_at"),
		}
		if !p.Valid() {
			rt.Error("Valid() == false for well-formed ControlPointsRegistrationStartedPayload")
		}
	})
}

func TestProp_ControlPointsRegistrationStartedPayload_EmptyBatchIDRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := ControlPointsRegistrationStartedPayload{
			BatchID:   "",
			StartedAt: drawNonEmptyString(rt, "started_at"),
		}
		if p.Valid() {
			rt.Error("Valid() should be false with empty BatchID")
		}
	})
}

func TestProp_ControlPointsRegistrationStartedPayload_EmptyStartedAtRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := ControlPointsRegistrationStartedPayload{
			BatchID:   drawNonEmptyString(rt, "batch_id"),
			StartedAt: "",
		}
		if p.Valid() {
			rt.Error("Valid() should be false with empty StartedAt")
		}
	})
}

// ============================================================
// VerdictEnvelopeMismatchPayload
// ============================================================

func TestProp_VerdictEnvelopeMismatchPayload_AllValidAccepted(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := VerdictEnvelopeMismatchPayload{
			RunID:               RunID(drawNonNilUUID(rt, "run_id")),
			ControlPointName:    drawNonEmptyString(rt, "cp_name"),
			StoredEnvelopeHash:  drawNonEmptyString(rt, "stored_hash"),
			CurrentEnvelopeHash: drawNonEmptyString(rt, "current_hash"),
			DetectedAt:          drawNonEmptyString(rt, "detected_at"),
		}
		if !p.Valid() {
			rt.Error("Valid() == false for well-formed VerdictEnvelopeMismatchPayload")
		}
	})
}

func TestProp_VerdictEnvelopeMismatchPayload_NilRunIDRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := VerdictEnvelopeMismatchPayload{
			RunID:               RunID(uuid.Nil),
			ControlPointName:    drawNonEmptyString(rt, "cp_name"),
			StoredEnvelopeHash:  drawNonEmptyString(rt, "stored_hash"),
			CurrentEnvelopeHash: drawNonEmptyString(rt, "current_hash"),
			DetectedAt:          drawNonEmptyString(rt, "detected_at"),
		}
		if p.Valid() {
			rt.Error("Valid() should be false with nil RunID")
		}
	})
}

func TestProp_VerdictEnvelopeMismatchPayload_EmptyControlPointNameRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := VerdictEnvelopeMismatchPayload{
			RunID:               RunID(drawNonNilUUID(rt, "run_id")),
			ControlPointName:    "",
			StoredEnvelopeHash:  drawNonEmptyString(rt, "stored_hash"),
			CurrentEnvelopeHash: drawNonEmptyString(rt, "current_hash"),
			DetectedAt:          drawNonEmptyString(rt, "detected_at"),
		}
		if p.Valid() {
			rt.Error("Valid() should be false with empty ControlPointName")
		}
	})
}

func TestProp_VerdictEnvelopeMismatchPayload_NilTransitionIDRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		nilTID := TransitionID(uuid.Nil)
		p := VerdictEnvelopeMismatchPayload{
			RunID:               RunID(drawNonNilUUID(rt, "run_id")),
			ControlPointName:    drawNonEmptyString(rt, "cp_name"),
			TransitionID:        &nilTID,
			StoredEnvelopeHash:  drawNonEmptyString(rt, "stored_hash"),
			CurrentEnvelopeHash: drawNonEmptyString(rt, "current_hash"),
			DetectedAt:          drawNonEmptyString(rt, "detected_at"),
		}
		if p.Valid() {
			rt.Error("Valid() should be false when TransitionID points to uuid.Nil")
		}
	})
}

func TestProp_VerdictEnvelopeMismatchPayload_NilEventIDRefRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		nilEID := EventID(uuid.Nil)
		p := VerdictEnvelopeMismatchPayload{
			RunID:               RunID(drawNonNilUUID(rt, "run_id")),
			ControlPointName:    drawNonEmptyString(rt, "cp_name"),
			EventIDRef:          &nilEID,
			StoredEnvelopeHash:  drawNonEmptyString(rt, "stored_hash"),
			CurrentEnvelopeHash: drawNonEmptyString(rt, "current_hash"),
			DetectedAt:          drawNonEmptyString(rt, "detected_at"),
		}
		if p.Valid() {
			rt.Error("Valid() should be false when EventIDRef points to uuid.Nil")
		}
	})
}

func TestProp_VerdictEnvelopeMismatchPayload_EmptyStoredEnvelopeHashRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := VerdictEnvelopeMismatchPayload{
			RunID:               RunID(drawNonNilUUID(rt, "run_id")),
			ControlPointName:    drawNonEmptyString(rt, "cp_name"),
			StoredEnvelopeHash:  "",
			CurrentEnvelopeHash: drawNonEmptyString(rt, "current_hash"),
			DetectedAt:          drawNonEmptyString(rt, "detected_at"),
		}
		if p.Valid() {
			rt.Error("Valid() should be false with empty StoredEnvelopeHash")
		}
	})
}

func TestProp_VerdictEnvelopeMismatchPayload_EmptyCurrentEnvelopeHashRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := VerdictEnvelopeMismatchPayload{
			RunID:               RunID(drawNonNilUUID(rt, "run_id")),
			ControlPointName:    drawNonEmptyString(rt, "cp_name"),
			StoredEnvelopeHash:  drawNonEmptyString(rt, "stored_hash"),
			CurrentEnvelopeHash: "",
			DetectedAt:          drawNonEmptyString(rt, "detected_at"),
		}
		if p.Valid() {
			rt.Error("Valid() should be false with empty CurrentEnvelopeHash")
		}
	})
}

func TestProp_VerdictEnvelopeMismatchPayload_EmptyDetectedAtRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := VerdictEnvelopeMismatchPayload{
			RunID:               RunID(drawNonNilUUID(rt, "run_id")),
			ControlPointName:    drawNonEmptyString(rt, "cp_name"),
			StoredEnvelopeHash:  drawNonEmptyString(rt, "stored_hash"),
			CurrentEnvelopeHash: drawNonEmptyString(rt, "current_hash"),
			DetectedAt:          "",
		}
		if p.Valid() {
			rt.Error("Valid() should be false with empty DetectedAt")
		}
	})
}

// ============================================================
// PolicyCostBound
// ============================================================

func TestProp_PolicyCostBound_UnknownValueRejected(t *testing.T) {
	known := make(map[string]bool)
	for _, v := range allPolicyCostBounds {
		known[string(v)] = true
	}
	rapid.Check(t, func(rt *rapid.T) {
		s := rapid.StringN(1, 64, -1).Draw(rt, "s")
		if known[s] {
			rt.Skip("known constant")
		}
		if PolicyCostBound(s).Valid() {
			rt.Errorf("Valid() == true for unknown PolicyCostBound %q", s)
		}
	})
}

func TestProp_PolicyCostBound_KnownConstantsAccepted(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		v := rapid.SampledFrom(allPolicyCostBounds).Draw(rt, "bound")
		if !v.Valid() {
			rt.Errorf("Valid() == false for declared PolicyCostBound %q", v)
		}
	})
}

// ============================================================
// PolicyEvalIODeterminism
// ============================================================

func TestProp_PolicyEvalIODeterminism_UnknownValueRejected(t *testing.T) {
	known := make(map[string]bool)
	for _, v := range allPolicyEvalIODeterminisms {
		known[string(v)] = true
	}
	rapid.Check(t, func(rt *rapid.T) {
		s := rapid.StringN(1, 64, -1).Draw(rt, "s")
		if known[s] {
			rt.Skip("known constant")
		}
		if PolicyEvalIODeterminism(s).Valid() {
			rt.Errorf("Valid() == true for unknown PolicyEvalIODeterminism %q", s)
		}
	})
}

func TestProp_PolicyEvalIODeterminism_KnownConstantsAccepted(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		v := rapid.SampledFrom(allPolicyEvalIODeterminisms).Draw(rt, "det")
		if !v.Valid() {
			rt.Errorf("Valid() == false for declared PolicyEvalIODeterminism %q", v)
		}
	})
}

// ============================================================
// PolicyExpressionExceededCostPayload
// ============================================================

func TestProp_PolicyExpressionExceededCostPayload_AllValidAccepted(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// ast_steps requires deterministic; wall_clock requires best-effort.
		bound := rapid.SampledFrom(allPolicyCostBounds).Draw(rt, "bound")
		var det PolicyEvalIODeterminism
		switch bound {
		case PolicyCostBoundASTSteps:
			det = PolicyEvalIODeterminismDeterministic
		case PolicyCostBoundWallClock:
			det = PolicyEvalIODeterminismBestEffort
		}
		p := PolicyExpressionExceededCostPayload{
			ControlPointName: drawNonEmptyString(rt, "cp_name"),
			BoundFired:       bound,
			IODeterminism:    det,
			AbortedAt:        drawNonEmptyString(rt, "aborted_at"),
		}
		if !p.Valid() {
			rt.Errorf("Valid() == false for well-formed PolicyExpressionExceededCostPayload (bound=%q)", bound)
		}
	})
}

func TestProp_PolicyExpressionExceededCostPayload_NilRunIDRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		nilID := RunID(uuid.Nil)
		p := PolicyExpressionExceededCostPayload{
			RunID:            &nilID,
			ControlPointName: drawNonEmptyString(rt, "cp_name"),
			BoundFired:       PolicyCostBoundASTSteps,
			IODeterminism:    PolicyEvalIODeterminismDeterministic,
			AbortedAt:        drawNonEmptyString(rt, "aborted_at"),
		}
		if p.Valid() {
			rt.Error("Valid() should be false when RunID points to uuid.Nil")
		}
	})
}

func TestProp_PolicyExpressionExceededCostPayload_EmptyControlPointNameRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := PolicyExpressionExceededCostPayload{
			ControlPointName: "",
			BoundFired:       PolicyCostBoundASTSteps,
			IODeterminism:    PolicyEvalIODeterminismDeterministic,
			AbortedAt:        drawNonEmptyString(rt, "aborted_at"),
		}
		if p.Valid() {
			rt.Error("Valid() should be false with empty ControlPointName")
		}
	})
}

func TestProp_PolicyExpressionExceededCostPayload_InvalidBoundFiredRejected(t *testing.T) {
	known := make(map[string]bool)
	for _, v := range allPolicyCostBounds {
		known[string(v)] = true
	}
	rapid.Check(t, func(rt *rapid.T) {
		s := rapid.StringN(1, 32, -1).Draw(rt, "bound_str")
		if known[s] {
			rt.Skip("known constant")
		}
		p := PolicyExpressionExceededCostPayload{
			ControlPointName: drawNonEmptyString(rt, "cp_name"),
			BoundFired:       PolicyCostBound(s),
			IODeterminism:    PolicyEvalIODeterminismDeterministic,
			AbortedAt:        drawNonEmptyString(rt, "aborted_at"),
		}
		if p.Valid() {
			rt.Errorf("Valid() should be false for unknown bound_fired %q", s)
		}
	})
}

func TestProp_PolicyExpressionExceededCostPayload_InconsistentBoundIODeterminismRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Flip the pairing: ast_steps+best-effort and wall_clock+deterministic are both invalid.
		type pair struct {
			b PolicyCostBound
			d PolicyEvalIODeterminism
		}
		bad := rapid.SampledFrom([]pair{
			{PolicyCostBoundASTSteps, PolicyEvalIODeterminismBestEffort},
			{PolicyCostBoundWallClock, PolicyEvalIODeterminismDeterministic},
		}).Draw(rt, "bad_pair")
		p := PolicyExpressionExceededCostPayload{
			ControlPointName: drawNonEmptyString(rt, "cp_name"),
			BoundFired:       bad.b,
			IODeterminism:    bad.d,
			AbortedAt:        drawNonEmptyString(rt, "aborted_at"),
		}
		if p.Valid() {
			rt.Errorf("Valid() should be false for inconsistent bound/io_determinism (%q/%q)", bad.b, bad.d)
		}
	})
}

func TestProp_PolicyExpressionExceededCostPayload_EmptyAbortedAtRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := PolicyExpressionExceededCostPayload{
			ControlPointName: drawNonEmptyString(rt, "cp_name"),
			BoundFired:       PolicyCostBoundASTSteps,
			IODeterminism:    PolicyEvalIODeterminismDeterministic,
			AbortedAt:        "",
		}
		if p.Valid() {
			rt.Error("Valid() should be false with empty AbortedAt")
		}
	})
}

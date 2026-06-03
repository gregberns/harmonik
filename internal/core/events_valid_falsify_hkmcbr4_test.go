package core

// events_valid_falsify_hkmcbr4_test.go — per-field falsification for Valid()
// methods in budget/guard/gate/cp/hookevents_hqwn59.go.
//
// Happy paths are covered by cpinv002_events_only_hka8bg55_test.go.
// This file adds one table row per missing `return false` branch.
// Rapid property tests cover numeric-range fields.
//
// Bead: hk-mcbr4 (partial Valid() coverage uplift; part of hk-j3hrn)
// Refs: hk-j3hrn

import (
	"testing"

	"github.com/google/uuid"
	"pgregory.net/rapid"
)

// ─── helpers ─────────────────────────────────────────────────────────────────

var (
	validRunID        = RunID(uuid.MustParse("0196a1b2-c3d4-7001-8abc-000000000001"))
	nilRunID          = RunID(uuid.Nil)
	validTriggerEvID  = EventID(uuid.MustParse("0196a1b2-c3d4-7001-8abc-000000000002"))
	nilEventID        = EventID(uuid.Nil)
	validTransitionID = TransitionID(uuid.MustParse("0196a1b2-c3d4-7001-8abc-000000000003"))
	nilTransitionID   = TransitionID(uuid.Nil)
	validTs           = "2026-06-01T00:00:00Z"
	validSideEffect   = SideEffect{
		Kind:             SideEffectKindEmitEvent,
		Target:           "some_event",
		IdempotencyClass: IdempotencyClassIdempotent,
	}
)

// ─── BudgetWarningPayload ─────────────────────────────────────────────────────

func TestBudgetWarningPayload_Valid(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		p     BudgetWarningPayload
		valid bool
	}{
		{"valid", BudgetWarningPayload{RunID: validRunID, BudgetRef: "b1", ThresholdFraction: 0.8, Remaining: 10}, true},
		{"nil-run-id", BudgetWarningPayload{RunID: nilRunID, BudgetRef: "b1", ThresholdFraction: 0.8, Remaining: 10}, false},
		{"empty-budget-ref", BudgetWarningPayload{RunID: validRunID, BudgetRef: "", ThresholdFraction: 0.8, Remaining: 10}, false},
		{"threshold-zero", BudgetWarningPayload{RunID: validRunID, BudgetRef: "b1", ThresholdFraction: 0, Remaining: 10}, false},
		{"threshold-negative", BudgetWarningPayload{RunID: validRunID, BudgetRef: "b1", ThresholdFraction: -0.1, Remaining: 10}, false},
		{"threshold-above-one", BudgetWarningPayload{RunID: validRunID, BudgetRef: "b1", ThresholdFraction: 1.01, Remaining: 10}, false},
		{"remaining-negative", BudgetWarningPayload{RunID: validRunID, BudgetRef: "b1", ThresholdFraction: 0.8, Remaining: -1}, false},
		// threshold exactly 1 is valid (boundary)
		{"threshold-one", BudgetWarningPayload{RunID: validRunID, BudgetRef: "b1", ThresholdFraction: 1.0, Remaining: 0}, true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.p.Valid(); got != tc.valid {
				t.Errorf("BudgetWarningPayload.Valid() = %v, want %v (case %q)", got, tc.valid, tc.name)
			}
		})
	}
}

// TestProp_BudgetWarningPayload_ThresholdRange checks that any threshold outside
// (0, 1] produces Valid() = false.
func TestProp_BudgetWarningPayload_ThresholdRange(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Draw a threshold that is strictly <= 0.
		frac := rapid.Float64Range(-1e9, 0).Draw(rt, "threshold_le_zero")
		p := BudgetWarningPayload{
			RunID:             validRunID,
			BudgetRef:         "prop-budget",
			ThresholdFraction: frac,
			Remaining:         0,
		}
		if p.Valid() {
			rt.Errorf("BudgetWarningPayload.Valid() = true for threshold %v, want false (threshold must be > 0)", frac)
		}
	})
}

// ─── BudgetAccrualPayload ─────────────────────────────────────────────────────

func TestBudgetAccrualPayload_Valid(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		p     BudgetAccrualPayload
		valid bool
	}{
		{"valid", BudgetAccrualPayload{RunID: validRunID, SessionID: "s1", CostUnits: 1.0, CostBasis: CostBasisOutputBytes}, true},
		{"nil-run-id", BudgetAccrualPayload{RunID: nilRunID, SessionID: "s1", CostUnits: 1.0, CostBasis: CostBasisOutputBytes}, false},
		{"empty-session-id", BudgetAccrualPayload{RunID: validRunID, SessionID: "", CostUnits: 1.0, CostBasis: CostBasisOutputBytes}, false},
		{"negative-cost-units", BudgetAccrualPayload{RunID: validRunID, SessionID: "s1", CostUnits: -0.01, CostBasis: CostBasisOutputBytes}, false},
		{"empty-cost-basis", BudgetAccrualPayload{RunID: validRunID, SessionID: "s1", CostUnits: 0, CostBasis: ""}, false},
		// zero cost units is valid (a zero-cost chunk)
		{"zero-cost-units", BudgetAccrualPayload{RunID: validRunID, SessionID: "s1", CostUnits: 0, CostBasis: CostBasisOutputBytes}, true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.p.Valid(); got != tc.valid {
				t.Errorf("BudgetAccrualPayload.Valid() = %v, want %v (case %q)", got, tc.valid, tc.name)
			}
		})
	}
}

// TestProp_BudgetAccrualPayload_NegativeCostUnits checks that any negative
// cost_units value produces Valid() = false.
func TestProp_BudgetAccrualPayload_NegativeCostUnits(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		cost := rapid.Float64Range(-1e9, -1e-12).Draw(rt, "negative_cost")
		p := BudgetAccrualPayload{
			RunID:     validRunID,
			SessionID: "prop-session",
			CostUnits: cost,
			CostBasis: CostBasisOutputBytes,
		}
		if p.Valid() {
			rt.Errorf("BudgetAccrualPayload.Valid() = true for cost_units %v, want false", cost)
		}
	})
}

// ─── BudgetExhaustedEventPayload ──────────────────────────────────────────────

func TestBudgetExhaustedEventPayload_Valid(t *testing.T) {
	t.Parallel()

	negFloat := -0.01
	zeroPosFloat := 0.0
	invalidScope := BudgetScope("unknown-scope")
	validScope := BudgetScopeHandlerAccount

	cases := []struct {
		name  string
		p     BudgetExhaustedEventPayload
		valid bool
	}{
		{"valid-minimal", BudgetExhaustedEventPayload{BudgetRef: "b1"}, true},
		{"valid-with-scope", BudgetExhaustedEventPayload{BudgetRef: "b1", BudgetScope: &validScope}, true},
		{"valid-with-spent-cap", BudgetExhaustedEventPayload{BudgetRef: "b1", SpentUSD: &zeroPosFloat, CapUSD: &zeroPosFloat}, true},
		{"empty-budget-ref", BudgetExhaustedEventPayload{BudgetRef: ""}, false},
		{"negative-attempted-dispatch-cost", BudgetExhaustedEventPayload{BudgetRef: "b1", AttemptedDispatchCost: -1}, false},
		{"invalid-budget-scope", BudgetExhaustedEventPayload{BudgetRef: "b1", BudgetScope: &invalidScope}, false},
		{"negative-spent-usd", BudgetExhaustedEventPayload{BudgetRef: "b1", SpentUSD: &negFloat}, false},
		{"negative-cap-usd", BudgetExhaustedEventPayload{BudgetRef: "b1", CapUSD: &negFloat}, false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.p.Valid(); got != tc.valid {
				t.Errorf("BudgetExhaustedEventPayload.Valid() = %v, want %v (case %q)", got, tc.valid, tc.name)
			}
		})
	}
}

// ─── GuardReorderedPayload ────────────────────────────────────────────────────

func TestGuardReorderedPayload_Valid(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		p     GuardReorderedPayload
		valid bool
	}{
		{"valid", GuardReorderedPayload{RunID: validRunID, GuardName: "g1", EdgeSetBefore: []string{"a"}, EdgeSetAfter: []string{"a"}}, true},
		{"valid-empty-sets", GuardReorderedPayload{RunID: validRunID, GuardName: "g1", EdgeSetBefore: []string{}, EdgeSetAfter: []string{}}, true},
		{"nil-run-id", GuardReorderedPayload{RunID: nilRunID, GuardName: "g1", EdgeSetBefore: []string{}, EdgeSetAfter: []string{}}, false},
		{"empty-guard-name", GuardReorderedPayload{RunID: validRunID, GuardName: "", EdgeSetBefore: []string{}, EdgeSetAfter: []string{}}, false},
		{"nil-edge-set-before", GuardReorderedPayload{RunID: validRunID, GuardName: "g1", EdgeSetBefore: nil, EdgeSetAfter: []string{}}, false},
		{"nil-edge-set-after", GuardReorderedPayload{RunID: validRunID, GuardName: "g1", EdgeSetBefore: []string{}, EdgeSetAfter: nil}, false},
		{"mismatched-lengths", GuardReorderedPayload{RunID: validRunID, GuardName: "g1", EdgeSetBefore: []string{"a", "b"}, EdgeSetAfter: []string{"a"}}, false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.p.Valid(); got != tc.valid {
				t.Errorf("GuardReorderedPayload.Valid() = %v, want %v (case %q)", got, tc.valid, tc.name)
			}
		})
	}
}

// ─── GuardFailedPayload ───────────────────────────────────────────────────────

func TestGuardFailedPayload_Valid(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		p     GuardFailedPayload
		valid bool
	}{
		{"valid", GuardFailedPayload{RunID: validRunID, GuardName: "g1", ErrorCategory: ErrorCategoryStructural, Reason: "oops"}, true},
		{"nil-run-id", GuardFailedPayload{RunID: nilRunID, GuardName: "g1", ErrorCategory: ErrorCategoryStructural, Reason: "oops"}, false},
		{"empty-guard-name", GuardFailedPayload{RunID: validRunID, GuardName: "", ErrorCategory: ErrorCategoryStructural, Reason: "oops"}, false},
		{"invalid-error-category", GuardFailedPayload{RunID: validRunID, GuardName: "g1", ErrorCategory: "bad-cat", Reason: "oops"}, false},
		{"empty-reason", GuardFailedPayload{RunID: validRunID, GuardName: "g1", ErrorCategory: ErrorCategoryStructural, Reason: ""}, false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.p.Valid(); got != tc.valid {
				t.Errorf("GuardFailedPayload.Valid() = %v, want %v (case %q)", got, tc.valid, tc.name)
			}
		})
	}
}

// ─── GateAllowedPayload ───────────────────────────────────────────────────────

func TestGateAllowedPayload_Valid(t *testing.T) {
	t.Parallel()

	emptyStr := ""
	nonEmptyStr := "allowed because X"

	cases := []struct {
		name  string
		p     GateAllowedPayload
		valid bool
	}{
		{"valid-no-reason", GateAllowedPayload{RunID: validRunID, GateName: "my-gate"}, true},
		{"valid-with-reason", GateAllowedPayload{RunID: validRunID, GateName: "my-gate", Reason: &nonEmptyStr}, true},
		{"nil-run-id", GateAllowedPayload{RunID: nilRunID, GateName: "my-gate"}, false},
		{"empty-gate-name", GateAllowedPayload{RunID: validRunID, GateName: ""}, false},
		{"empty-reason-ptr", GateAllowedPayload{RunID: validRunID, GateName: "my-gate", Reason: &emptyStr}, false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.p.Valid(); got != tc.valid {
				t.Errorf("GateAllowedPayload.Valid() = %v, want %v (case %q)", got, tc.valid, tc.name)
			}
		})
	}
}

// ─── GateDeniedPayload ────────────────────────────────────────────────────────

func TestGateDeniedPayload_Valid(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		p     GateDeniedPayload
		valid bool
	}{
		{"valid", GateDeniedPayload{RunID: validRunID, GateName: "my-gate", Reason: "denied"}, true},
		{"nil-run-id", GateDeniedPayload{RunID: nilRunID, GateName: "my-gate", Reason: "denied"}, false},
		{"empty-gate-name", GateDeniedPayload{RunID: validRunID, GateName: "", Reason: "denied"}, false},
		{"empty-reason", GateDeniedPayload{RunID: validRunID, GateName: "my-gate", Reason: ""}, false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.p.Valid(); got != tc.valid {
				t.Errorf("GateDeniedPayload.Valid() = %v, want %v (case %q)", got, tc.valid, tc.name)
			}
		})
	}
}

// ─── GateEscalatedPayload ─────────────────────────────────────────────────────

func TestGateEscalatedPayload_Valid(t *testing.T) {
	t.Parallel()

	emptyStr := ""
	nonEmptyStr := "needs human review"

	cases := []struct {
		name  string
		p     GateEscalatedPayload
		valid bool
	}{
		{"valid-no-reason", GateEscalatedPayload{RunID: validRunID, GateName: "my-gate"}, true},
		{"valid-with-reason", GateEscalatedPayload{RunID: validRunID, GateName: "my-gate", Reason: &nonEmptyStr}, true},
		{"nil-run-id", GateEscalatedPayload{RunID: nilRunID, GateName: "my-gate"}, false},
		{"empty-gate-name", GateEscalatedPayload{RunID: validRunID, GateName: ""}, false},
		{"empty-reason-ptr", GateEscalatedPayload{RunID: validRunID, GateName: "my-gate", Reason: &emptyStr}, false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.p.Valid(); got != tc.valid {
				t.Errorf("GateEscalatedPayload.Valid() = %v, want %v (case %q)", got, tc.valid, tc.name)
			}
		})
	}
}

// ─── ControlPointsRegisteredPayload ──────────────────────────────────────────

func TestControlPointsRegisteredPayload_Valid(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		p     ControlPointsRegisteredPayload
		valid bool
	}{
		{"valid", ControlPointsRegisteredPayload{Count: 3, StartedAt: validTs}, true},
		{"valid-zero-count", ControlPointsRegisteredPayload{Count: 0, StartedAt: validTs}, true},
		{"negative-count", ControlPointsRegisteredPayload{Count: -1, StartedAt: validTs}, false},
		{"empty-started-at", ControlPointsRegisteredPayload{Count: 1, StartedAt: ""}, false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.p.Valid(); got != tc.valid {
				t.Errorf("ControlPointsRegisteredPayload.Valid() = %v, want %v (case %q)", got, tc.valid, tc.name)
			}
		})
	}
}

// ─── ControlPointsRegistrationStartedPayload ─────────────────────────────────

func TestControlPointsRegistrationStartedPayload_Valid(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		p     ControlPointsRegistrationStartedPayload
		valid bool
	}{
		{"valid", ControlPointsRegistrationStartedPayload{BatchID: "batch-1", StartedAt: validTs}, true},
		{"empty-batch-id", ControlPointsRegistrationStartedPayload{BatchID: "", StartedAt: validTs}, false},
		{"empty-started-at", ControlPointsRegistrationStartedPayload{BatchID: "batch-1", StartedAt: ""}, false},
		{"both-empty", ControlPointsRegistrationStartedPayload{}, false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.p.Valid(); got != tc.valid {
				t.Errorf("ControlPointsRegistrationStartedPayload.Valid() = %v, want %v (case %q)", got, tc.valid, tc.name)
			}
		})
	}
}

// ─── VerdictEnvelopeMismatchPayload ───────────────────────────────────────────

func TestVerdictEnvelopeMismatchPayload_Valid(t *testing.T) {
	t.Parallel()

	validEventIDRef := EventID(uuid.MustParse("0196a1b2-c3d4-7001-8abc-000000000009"))

	cases := []struct {
		name  string
		p     VerdictEnvelopeMismatchPayload
		valid bool
	}{
		{
			"valid-minimal",
			VerdictEnvelopeMismatchPayload{
				RunID: validRunID, ControlPointName: "cp1",
				StoredEnvelopeHash: "aaa", CurrentEnvelopeHash: "bbb", DetectedAt: validTs,
			},
			true,
		},
		{
			"valid-with-optionals",
			VerdictEnvelopeMismatchPayload{
				RunID: validRunID, ControlPointName: "cp1",
				TransitionID: &validTransitionID, EventIDRef: &validEventIDRef,
				StoredEnvelopeHash: "aaa", CurrentEnvelopeHash: "bbb", DetectedAt: validTs,
			},
			true,
		},
		{
			"nil-run-id",
			VerdictEnvelopeMismatchPayload{
				RunID: nilRunID, ControlPointName: "cp1",
				StoredEnvelopeHash: "aaa", CurrentEnvelopeHash: "bbb", DetectedAt: validTs,
			},
			false,
		},
		{
			"empty-cp-name",
			VerdictEnvelopeMismatchPayload{
				RunID: validRunID, ControlPointName: "",
				StoredEnvelopeHash: "aaa", CurrentEnvelopeHash: "bbb", DetectedAt: validTs,
			},
			false,
		},
		{
			"nil-transition-id-value",
			VerdictEnvelopeMismatchPayload{
				RunID: validRunID, ControlPointName: "cp1",
				TransitionID:       &nilTransitionID,
				StoredEnvelopeHash: "aaa", CurrentEnvelopeHash: "bbb", DetectedAt: validTs,
			},
			false,
		},
		{
			"nil-event-id-ref-value",
			VerdictEnvelopeMismatchPayload{
				RunID: validRunID, ControlPointName: "cp1",
				EventIDRef:         &nilEventID,
				StoredEnvelopeHash: "aaa", CurrentEnvelopeHash: "bbb", DetectedAt: validTs,
			},
			false,
		},
		{
			"empty-stored-hash",
			VerdictEnvelopeMismatchPayload{
				RunID: validRunID, ControlPointName: "cp1",
				StoredEnvelopeHash: "", CurrentEnvelopeHash: "bbb", DetectedAt: validTs,
			},
			false,
		},
		{
			"empty-current-hash",
			VerdictEnvelopeMismatchPayload{
				RunID: validRunID, ControlPointName: "cp1",
				StoredEnvelopeHash: "aaa", CurrentEnvelopeHash: "", DetectedAt: validTs,
			},
			false,
		},
		{
			"empty-detected-at",
			VerdictEnvelopeMismatchPayload{
				RunID: validRunID, ControlPointName: "cp1",
				StoredEnvelopeHash: "aaa", CurrentEnvelopeHash: "bbb", DetectedAt: "",
			},
			false,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.p.Valid(); got != tc.valid {
				t.Errorf("VerdictEnvelopeMismatchPayload.Valid() = %v, want %v (case %q)", got, tc.valid, tc.name)
			}
		})
	}
}

// ─── PolicyExpressionExceededCostPayload ──────────────────────────────────────

func TestPolicyExpressionExceededCostPayload_Valid(t *testing.T) {
	t.Parallel()

	nilRunIDPtr := RunID(uuid.Nil)
	validRunIDPtr := validRunID

	cases := []struct {
		name  string
		p     PolicyExpressionExceededCostPayload
		valid bool
	}{
		{
			"valid-ast-steps",
			PolicyExpressionExceededCostPayload{
				ControlPointName: "cp1",
				BoundFired:       PolicyCostBoundASTSteps,
				IODeterminism:    PolicyEvalIODeterminismDeterministic,
				AbortedAt:        validTs,
			},
			true,
		},
		{
			"valid-wall-clock",
			PolicyExpressionExceededCostPayload{
				ControlPointName: "cp1",
				BoundFired:       PolicyCostBoundWallClock,
				IODeterminism:    PolicyEvalIODeterminismBestEffort,
				AbortedAt:        validTs,
			},
			true,
		},
		{
			"valid-with-run-id",
			PolicyExpressionExceededCostPayload{
				RunID:            &validRunIDPtr,
				ControlPointName: "cp1",
				BoundFired:       PolicyCostBoundASTSteps,
				IODeterminism:    PolicyEvalIODeterminismDeterministic,
				AbortedAt:        validTs,
			},
			true,
		},
		{
			"nil-run-id-value",
			PolicyExpressionExceededCostPayload{
				RunID:            &nilRunIDPtr,
				ControlPointName: "cp1",
				BoundFired:       PolicyCostBoundASTSteps,
				IODeterminism:    PolicyEvalIODeterminismDeterministic,
				AbortedAt:        validTs,
			},
			false,
		},
		{
			"empty-cp-name",
			PolicyExpressionExceededCostPayload{
				ControlPointName: "",
				BoundFired:       PolicyCostBoundASTSteps,
				IODeterminism:    PolicyEvalIODeterminismDeterministic,
				AbortedAt:        validTs,
			},
			false,
		},
		{
			"invalid-bound-fired",
			PolicyExpressionExceededCostPayload{
				ControlPointName: "cp1",
				BoundFired:       "bad-bound",
				IODeterminism:    PolicyEvalIODeterminismDeterministic,
				AbortedAt:        validTs,
			},
			false,
		},
		{
			"invalid-io-determinism",
			PolicyExpressionExceededCostPayload{
				ControlPointName: "cp1",
				BoundFired:       PolicyCostBoundASTSteps,
				IODeterminism:    "bad-det",
				AbortedAt:        validTs,
			},
			false,
		},
		{
			"ast-steps-with-best-effort",
			PolicyExpressionExceededCostPayload{
				ControlPointName: "cp1",
				BoundFired:       PolicyCostBoundASTSteps,
				IODeterminism:    PolicyEvalIODeterminismBestEffort,
				AbortedAt:        validTs,
			},
			false,
		},
		{
			"wall-clock-with-deterministic",
			PolicyExpressionExceededCostPayload{
				ControlPointName: "cp1",
				BoundFired:       PolicyCostBoundWallClock,
				IODeterminism:    PolicyEvalIODeterminismDeterministic,
				AbortedAt:        validTs,
			},
			false,
		},
		{
			"empty-aborted-at",
			PolicyExpressionExceededCostPayload{
				ControlPointName: "cp1",
				BoundFired:       PolicyCostBoundASTSteps,
				IODeterminism:    PolicyEvalIODeterminismDeterministic,
				AbortedAt:        "",
			},
			false,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.p.Valid(); got != tc.valid {
				t.Errorf("PolicyExpressionExceededCostPayload.Valid() = %v, want %v (case %q)", got, tc.valid, tc.name)
			}
		})
	}
}

// ─── HookFiredPayload ─────────────────────────────────────────────────────────

func TestHookFiredPayload_Valid(t *testing.T) {
	t.Parallel()

	nilRunIDPtr := RunID(uuid.Nil)
	validRunIDPtr := validRunID

	invalidSideEffect := SideEffect{Kind: "bad-kind", Target: "x", IdempotencyClass: IdempotencyClassIdempotent}

	cases := []struct {
		name  string
		p     HookFiredPayload
		valid bool
	}{
		{
			"valid-no-run-id",
			HookFiredPayload{HookName: "h1", TriggeringEventID: validTriggerEvID, SideEffectDescriptor: validSideEffect},
			true,
		},
		{
			"valid-with-run-id",
			HookFiredPayload{RunID: &validRunIDPtr, HookName: "h1", TriggeringEventID: validTriggerEvID, SideEffectDescriptor: validSideEffect},
			true,
		},
		{
			"nil-run-id-value",
			HookFiredPayload{RunID: &nilRunIDPtr, HookName: "h1", TriggeringEventID: validTriggerEvID, SideEffectDescriptor: validSideEffect},
			false,
		},
		{
			"empty-hook-name",
			HookFiredPayload{HookName: "", TriggeringEventID: validTriggerEvID, SideEffectDescriptor: validSideEffect},
			false,
		},
		{
			"nil-triggering-event-id",
			HookFiredPayload{HookName: "h1", TriggeringEventID: nilEventID, SideEffectDescriptor: validSideEffect},
			false,
		},
		{
			"invalid-side-effect",
			HookFiredPayload{HookName: "h1", TriggeringEventID: validTriggerEvID, SideEffectDescriptor: invalidSideEffect},
			false,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.p.Valid(); got != tc.valid {
				t.Errorf("HookFiredPayload.Valid() = %v, want %v (case %q)", got, tc.valid, tc.name)
			}
		})
	}
}

// ─── HookFailedPayload ────────────────────────────────────────────────────────

func TestHookFailedPayload_Valid(t *testing.T) {
	t.Parallel()

	nilRunIDPtr := RunID(uuid.Nil)
	validRunIDPtr := validRunID

	cases := []struct {
		name  string
		p     HookFailedPayload
		valid bool
	}{
		{
			"valid-no-run-id",
			HookFailedPayload{HookName: "h1", TriggeringEventID: validTriggerEvID, ErrorCategory: ErrorCategoryTransient, Reason: "timeout"},
			true,
		},
		{
			"valid-with-run-id",
			HookFailedPayload{RunID: &validRunIDPtr, HookName: "h1", TriggeringEventID: validTriggerEvID, ErrorCategory: ErrorCategoryTransient, Reason: "timeout"},
			true,
		},
		{
			"nil-run-id-value",
			HookFailedPayload{RunID: &nilRunIDPtr, HookName: "h1", TriggeringEventID: validTriggerEvID, ErrorCategory: ErrorCategoryTransient, Reason: "timeout"},
			false,
		},
		{
			"empty-hook-name",
			HookFailedPayload{HookName: "", TriggeringEventID: validTriggerEvID, ErrorCategory: ErrorCategoryTransient, Reason: "timeout"},
			false,
		},
		{
			"nil-triggering-event-id",
			HookFailedPayload{HookName: "h1", TriggeringEventID: nilEventID, ErrorCategory: ErrorCategoryTransient, Reason: "timeout"},
			false,
		},
		{
			"invalid-error-category",
			HookFailedPayload{HookName: "h1", TriggeringEventID: validTriggerEvID, ErrorCategory: "bad-cat", Reason: "timeout"},
			false,
		},
		{
			"empty-reason",
			HookFailedPayload{HookName: "h1", TriggeringEventID: validTriggerEvID, ErrorCategory: ErrorCategoryTransient, Reason: ""},
			false,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.p.Valid(); got != tc.valid {
				t.Errorf("HookFailedPayload.Valid() = %v, want %v (case %q)", got, tc.valid, tc.name)
			}
		})
	}
}

// ─── HookVerdictPersistedPayload ──────────────────────────────────────────────

func TestHookVerdictPersistedPayload_Valid(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		p     HookVerdictPersistedPayload
		valid bool
	}{
		{
			"valid",
			HookVerdictPersistedPayload{
				RunID:            validRunID,
				HookInvocationID: "inv-1",
				HookName:         "h1",
				VerdictPath:      "runs/abc/hook_verdict.json",
				CommitHash:       "deadbeef",
			},
			true,
		},
		{
			"nil-run-id",
			HookVerdictPersistedPayload{RunID: nilRunID, HookInvocationID: "inv-1", HookName: "h1", VerdictPath: "p", CommitHash: "c"},
			false,
		},
		{
			"empty-hook-invocation-id",
			HookVerdictPersistedPayload{RunID: validRunID, HookInvocationID: "", HookName: "h1", VerdictPath: "p", CommitHash: "c"},
			false,
		},
		{
			"empty-hook-name",
			HookVerdictPersistedPayload{RunID: validRunID, HookInvocationID: "inv-1", HookName: "", VerdictPath: "p", CommitHash: "c"},
			false,
		},
		{
			"empty-verdict-path",
			HookVerdictPersistedPayload{RunID: validRunID, HookInvocationID: "inv-1", HookName: "h1", VerdictPath: "", CommitHash: "c"},
			false,
		},
		{
			"empty-commit-hash",
			HookVerdictPersistedPayload{RunID: validRunID, HookInvocationID: "inv-1", HookName: "h1", VerdictPath: "p", CommitHash: ""},
			false,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.p.Valid(); got != tc.valid {
				t.Errorf("HookVerdictPersistedPayload.Valid() = %v, want %v (case %q)", got, tc.valid, tc.name)
			}
		})
	}
}

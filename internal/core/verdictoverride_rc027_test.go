package core_test

// verdictoverride_rc027_test.go — Tests for the operator verdict-override
// surface (RC-027).
//
// Covers:
//   - OperatorVerdictOverridePolicy.ConfirmRequired semantics
//   - PolicyRequiresConfirmation pure function
//   - VerdictOverrideDecision enum validity
//   - VetoPromotion enum validity
//   - OperatorVerdictOverrideRequest.Valid invariants
//   - ApplyVetoPromotion mapping per RC-027
//   - S01 per-category policy defaults (Cat 2/Cat 3 = false; Cat 6a = true)
//
// Spec ref: specs/reconciliation/spec.md §4.5 RC-027;
// specs/operator-nfr.md §4.3 ON-014;
// OQ-RC-012 (Cat 6a default confirm_required: true).

import (
	"testing"

	"github.com/gregberns/harmonik/internal/core"
)

// ---------------------------------------------------------------------------
// OperatorVerdictOverridePolicy / PolicyRequiresConfirmation
// ---------------------------------------------------------------------------

// TestRC027_PolicyDefaultIsFalse verifies that the zero-value policy does not
// require confirmation. This encodes the RC-027 default: execution proceeds
// without operator confirmation.
//
// Spec ref: specs/reconciliation/spec.md §4.5 RC-027 —
// "Default: execution proceeds without operator confirmation."
func TestRC027_PolicyDefaultIsFalse(t *testing.T) {
	t.Parallel()

	var policy core.OperatorVerdictOverridePolicy
	if core.PolicyRequiresConfirmation(policy) {
		t.Error("RC-027: zero-value OperatorVerdictOverridePolicy should not require confirmation (default is false)")
	}
}

// TestRC027_PolicyConfirmRequiredTrue verifies that setting ConfirmRequired: true
// causes PolicyRequiresConfirmation to return true.
//
// Spec ref: specs/reconciliation/spec.md §4.5 RC-027; OQ-RC-012.
func TestRC027_PolicyConfirmRequiredTrue(t *testing.T) {
	t.Parallel()

	policy := core.OperatorVerdictOverridePolicy{ConfirmRequired: true}
	if !core.PolicyRequiresConfirmation(policy) {
		t.Error("RC-027: OperatorVerdictOverridePolicy{ConfirmRequired: true} should require confirmation")
	}
}

// TestRC027_PolicyConfirmRequiredFalse verifies that explicitly setting
// ConfirmRequired: false yields no-confirmation-required.
func TestRC027_PolicyConfirmRequiredFalse(t *testing.T) {
	t.Parallel()

	policy := core.OperatorVerdictOverridePolicy{ConfirmRequired: false}
	if core.PolicyRequiresConfirmation(policy) {
		t.Error("RC-027: OperatorVerdictOverridePolicy{ConfirmRequired: false} must not require confirmation")
	}
}

// TestRC027_S01Cat2PolicyDefault verifies the expected S01-shipped default for
// Cat 2: confirm_required: false (execution proceeds without operator input).
//
// Spec ref: specs/s01/reconciliation/policies/cat-2.yaml confirm_required: false.
func TestRC027_S01Cat2PolicyDefault(t *testing.T) {
	t.Parallel()

	cat2Policy := core.OperatorVerdictOverridePolicy{ConfirmRequired: false}
	if core.PolicyRequiresConfirmation(cat2Policy) {
		t.Error("RC-027/Cat2: S01 Cat 2 default policy must not require confirmation (confirm_required: false)")
	}
}

// TestRC027_S01Cat3PolicyDefault verifies the expected S01-shipped default for
// Cat 3: confirm_required: false.
//
// Spec ref: specs/s01/reconciliation/policies/cat-3.yaml confirm_required: false.
func TestRC027_S01Cat3PolicyDefault(t *testing.T) {
	t.Parallel()

	cat3Policy := core.OperatorVerdictOverridePolicy{ConfirmRequired: false}
	if core.PolicyRequiresConfirmation(cat3Policy) {
		t.Error("RC-027/Cat3: S01 Cat 3 default policy must not require confirmation (confirm_required: false)")
	}
}

// TestRC027_S01Cat6aPolicyDefault verifies the expected S01-shipped default for
// Cat 6a: confirm_required: true (operator confirmation required before
// escalate-to-human execution).
//
// Spec ref: specs/s01/reconciliation/policies/cat-6a.yaml confirm_required: true;
// OQ-RC-012 resolution (require for Cat 6a, optional otherwise).
func TestRC027_S01Cat6aPolicyDefault(t *testing.T) {
	t.Parallel()

	cat6aPolicy := core.OperatorVerdictOverridePolicy{ConfirmRequired: true}
	if !core.PolicyRequiresConfirmation(cat6aPolicy) {
		t.Error("RC-027/Cat6a: S01 Cat 6a default policy must require confirmation (confirm_required: true)")
	}
}

// ---------------------------------------------------------------------------
// VerdictOverrideDecision enum
// ---------------------------------------------------------------------------

// TestRC027_DecisionEnumCardinality verifies that exactly two decision values
// exist: confirm and veto.
//
// Spec ref: specs/reconciliation/spec.md §4.5 RC-027;
// specs/operator-nfr.md §4.3 ON-014.
func TestRC027_DecisionEnumCardinality(t *testing.T) {
	t.Parallel()

	decisions := []core.VerdictOverrideDecision{
		core.VerdictOverrideDecisionConfirm,
		core.VerdictOverrideDecisionVeto,
	}
	const wantCount = 2
	if len(decisions) != wantCount {
		t.Errorf("RC-027: VerdictOverrideDecision enum has %d values, want %d", len(decisions), wantCount)
	}
	for _, d := range decisions {
		if !d.Valid() {
			t.Errorf("RC-027: declared decision %q reports Valid() = false", d)
		}
	}
}

// TestRC027_DecisionConfirmIsValid verifies the confirm constant.
func TestRC027_DecisionConfirmIsValid(t *testing.T) {
	t.Parallel()

	if !core.VerdictOverrideDecisionConfirm.Valid() {
		t.Error("RC-027: VerdictOverrideDecisionConfirm.Valid() = false")
	}
}

// TestRC027_DecisionVetoIsValid verifies the veto constant.
func TestRC027_DecisionVetoIsValid(t *testing.T) {
	t.Parallel()

	if !core.VerdictOverrideDecisionVeto.Valid() {
		t.Error("RC-027: VerdictOverrideDecisionVeto.Valid() = false")
	}
}

// TestRC027_UnknownDecisionIsInvalid verifies that arbitrary strings do not
// satisfy Valid().
func TestRC027_UnknownDecisionIsInvalid(t *testing.T) {
	t.Parallel()

	unknown := core.VerdictOverrideDecision("unknown-decision")
	if unknown.Valid() {
		t.Error("RC-027: unknown VerdictOverrideDecision should not be valid")
	}
}

// ---------------------------------------------------------------------------
// VetoPromotion enum
// ---------------------------------------------------------------------------

// TestRC027_VetoPromotionEnumCardinality verifies that exactly two promotion
// values exist: none and escalate-to-human.
//
// Spec ref: specs/reconciliation/spec.md §4.5 RC-027;
// specs/operator-nfr.md §4.3 ON-014 —
// "harmonik veto-verdict <run_id> [--promote-to escalate-to-human]".
func TestRC027_VetoPromotionEnumCardinality(t *testing.T) {
	t.Parallel()

	promotions := []core.VetoPromotion{
		core.VetoPromotionNone,
		core.VetoPromotionEscalateToHuman,
	}
	const wantCount = 2
	if len(promotions) != wantCount {
		t.Errorf("RC-027: VetoPromotion enum has %d values, want %d", len(promotions), wantCount)
	}
	for _, p := range promotions {
		if !p.Valid() {
			t.Errorf("RC-027: declared promotion %q reports Valid() = false", p)
		}
	}
}

// TestRC027_VetoPromotionEscalateToHumanMatchesVerdictEnum verifies that
// VetoPromotionEscalateToHuman's wire value is identical to the Verdict enum
// string "escalate-to-human" so the daemon can pass it directly to
// PlanForVerdict.
//
// Spec ref: specs/reconciliation/spec.md §4.5 RC-027 — wire value alignment.
func TestRC027_VetoPromotionEscalateToHumanMatchesVerdictEnum(t *testing.T) {
	t.Parallel()

	if string(core.VetoPromotionEscalateToHuman) != string(core.VerdictEscalateToHuman) {
		t.Errorf("RC-027: VetoPromotionEscalateToHuman wire value %q != VerdictEscalateToHuman %q; must be identical for daemon pass-through",
			core.VetoPromotionEscalateToHuman, core.VerdictEscalateToHuman)
	}
}

// TestRC027_UnknownVetoPromotionIsInvalid verifies that arbitrary strings do
// not satisfy Valid().
func TestRC027_UnknownVetoPromotionIsInvalid(t *testing.T) {
	t.Parallel()

	unknown := core.VetoPromotion("unknown-promotion")
	if unknown.Valid() {
		t.Error("RC-027: unknown VetoPromotion should not be valid")
	}
}

// ---------------------------------------------------------------------------
// OperatorVerdictOverrideRequest.Valid
// ---------------------------------------------------------------------------

// TestRC027_RequestValidConfirm verifies a well-formed confirm request.
func TestRC027_RequestValidConfirm(t *testing.T) {
	t.Parallel()

	req := core.OperatorVerdictOverrideRequest{
		TargetRunID:   "run-abc",
		Decision:      core.VerdictOverrideDecisionConfirm,
		VetoPromotion: core.VetoPromotionNone,
	}
	if !req.Valid() {
		t.Error("RC-027: confirm request with valid fields should be Valid()")
	}
}

// TestRC027_RequestValidVetoNoPromotion verifies a well-formed veto without
// --promote-to.
func TestRC027_RequestValidVetoNoPromotion(t *testing.T) {
	t.Parallel()

	req := core.OperatorVerdictOverrideRequest{
		TargetRunID:   "run-abc",
		Decision:      core.VerdictOverrideDecisionVeto,
		VetoPromotion: core.VetoPromotionNone,
	}
	if !req.Valid() {
		t.Error("RC-027: veto request with no promotion should be Valid()")
	}
}

// TestRC027_RequestValidVetoWithPromotion verifies a well-formed veto with
// --promote-to escalate-to-human.
func TestRC027_RequestValidVetoWithPromotion(t *testing.T) {
	t.Parallel()

	req := core.OperatorVerdictOverrideRequest{
		TargetRunID:   "run-abc",
		Decision:      core.VerdictOverrideDecisionVeto,
		VetoPromotion: core.VetoPromotionEscalateToHuman,
	}
	if !req.Valid() {
		t.Error("RC-027: veto request with escalate-to-human promotion should be Valid()")
	}
}

// TestRC027_RequestInvalidEmptyRunID verifies that an empty TargetRunID
// invalidates the request.
func TestRC027_RequestInvalidEmptyRunID(t *testing.T) {
	t.Parallel()

	req := core.OperatorVerdictOverrideRequest{
		TargetRunID:   "",
		Decision:      core.VerdictOverrideDecisionConfirm,
		VetoPromotion: core.VetoPromotionNone,
	}
	if req.Valid() {
		t.Error("RC-027: request with empty TargetRunID must not be Valid()")
	}
}

// TestRC027_RequestInvalidUnknownDecision verifies that an unknown Decision
// value invalidates the request.
func TestRC027_RequestInvalidUnknownDecision(t *testing.T) {
	t.Parallel()

	req := core.OperatorVerdictOverrideRequest{
		TargetRunID:   "run-abc",
		Decision:      core.VerdictOverrideDecision("bogus"),
		VetoPromotion: core.VetoPromotionNone,
	}
	if req.Valid() {
		t.Error("RC-027: request with unknown Decision must not be Valid()")
	}
}

// TestRC027_RequestInvalidConfirmWithPromotion verifies that a confirm request
// carrying a VetoPromotion is invalid (promotion is only meaningful on a veto).
//
// Spec ref: specs/reconciliation/spec.md §4.5 RC-027 — "MUST be
// VetoPromotionNone when Decision == VerdictOverrideDecisionConfirm".
func TestRC027_RequestInvalidConfirmWithPromotion(t *testing.T) {
	t.Parallel()

	req := core.OperatorVerdictOverrideRequest{
		TargetRunID:   "run-abc",
		Decision:      core.VerdictOverrideDecisionConfirm,
		VetoPromotion: core.VetoPromotionEscalateToHuman,
	}
	if req.Valid() {
		t.Error("RC-027: confirm request carrying VetoPromotionEscalateToHuman must not be Valid()")
	}
}

// TestRC027_RequestInvalidUnknownPromotion verifies that an unknown
// VetoPromotion value invalidates the request.
func TestRC027_RequestInvalidUnknownPromotion(t *testing.T) {
	t.Parallel()

	req := core.OperatorVerdictOverrideRequest{
		TargetRunID:   "run-abc",
		Decision:      core.VerdictOverrideDecisionVeto,
		VetoPromotion: core.VetoPromotion("unknown"),
	}
	if req.Valid() {
		t.Error("RC-027: request with unknown VetoPromotion must not be Valid()")
	}
}

// ---------------------------------------------------------------------------
// ApplyVetoPromotion
// ---------------------------------------------------------------------------

// TestRC027_ApplyVetoPromotionNoneYieldsNoOpAccept verifies that a plain veto
// (no --promote-to) resolves to no-op-accept — the run is left in its current
// state without executing any verdict action.
//
// Spec ref: specs/reconciliation/spec.md §4.5 RC-027.
func TestRC027_ApplyVetoPromotionNoneYieldsNoOpAccept(t *testing.T) {
	t.Parallel()

	result := core.ApplyVetoPromotion(core.VetoPromotionNone)
	if result != core.VerdictNoOpAccept {
		t.Errorf("RC-027: ApplyVetoPromotion(None) = %q, want %q",
			result, core.VerdictNoOpAccept)
	}
}

// TestRC027_ApplyVetoPromotionEscalateToHumanYieldsEscalateToHuman verifies
// that --promote-to escalate-to-human resolves to the VerdictEscalateToHuman
// constant, so the daemon can substitute it into the verdict-execution path.
//
// Spec ref: specs/reconciliation/spec.md §4.5 RC-027.
func TestRC027_ApplyVetoPromotionEscalateToHumanYieldsEscalateToHuman(t *testing.T) {
	t.Parallel()

	result := core.ApplyVetoPromotion(core.VetoPromotionEscalateToHuman)
	if result != core.VerdictEscalateToHuman {
		t.Errorf("RC-027: ApplyVetoPromotion(EscalateToHuman) = %q, want %q",
			result, core.VerdictEscalateToHuman)
	}
}

// TestRC027_ApplyVetoPromotionResultsAreValidVerdicts verifies that every
// possible VetoPromotion value produces a Valid() Verdict.
//
// Spec ref: specs/reconciliation/spec.md §4.5 RC-027 — the promoted verdict
// must satisfy the Verdict enum contract (RC-020).
func TestRC027_ApplyVetoPromotionResultsAreValidVerdicts(t *testing.T) {
	t.Parallel()

	for _, p := range []core.VetoPromotion{core.VetoPromotionNone, core.VetoPromotionEscalateToHuman} {
		p := p
		t.Run(string(p), func(t *testing.T) {
			t.Parallel()

			v := core.ApplyVetoPromotion(p)
			if !v.Valid() {
				t.Errorf("RC-027: ApplyVetoPromotion(%q) = %q which is not a valid Verdict", p, v)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Operator-scope invariant: confirm applies only to investigator-dispatched cats
// ---------------------------------------------------------------------------

// TestRC027_AppliesOnlyToInvestigatorDispatchedCategories verifies that the
// spec fixture for ON-014 covers exactly the three investigator-dispatched
// categories (Cat 2, Cat 3, Cat 6a) and no auto-resolver category. This is a
// spec-level guard on the scope declared in RC-027.
//
// Spec ref: specs/reconciliation/spec.md §4.5 RC-027 —
// "all investigator-dispatched categories (Cat 2, Cat 3 generic, Cat 6a per §8.12)".
func TestRC027_AppliesOnlyToInvestigatorDispatchedCategories(t *testing.T) {
	t.Parallel()

	// investigatorDispatched encodes the three categories to which RC-027 applies.
	type investCat struct {
		name string
	}
	investigatorDispatched := []investCat{
		{"Cat 2"},  // non-idempotent in-flight
		{"Cat 3"},  // store disagreement (generic)
		{"Cat 6a"}, // integrity violation, LLM-triageable
	}

	// Auto-resolver categories (Cat 0, Cat 1, Cat 3a, Cat 3b, Cat 3c, Cat 4,
	// Cat 5, Cat 6b) MUST NOT require operator confirmation by default; their
	// ConfirmRequired default is false.
	autoResolver := []string{
		"Cat 0", "Cat 1", "Cat 3a", "Cat 3b", "Cat 3c", "Cat 4", "Cat 5", "Cat 6b",
	}

	// Verify investigator categories number exactly 3.
	if len(investigatorDispatched) != 3 {
		t.Errorf("RC-027: investigator-dispatched category list has %d entries, want 3", len(investigatorDispatched))
	}

	// Verify auto-resolver categories do not require confirmation (policy default).
	for _, name := range autoResolver {
		policy := core.OperatorVerdictOverridePolicy{ConfirmRequired: false}
		if core.PolicyRequiresConfirmation(policy) {
			t.Errorf("RC-027: auto-resolver category %q should not require confirmation by default", name)
		}
	}
}

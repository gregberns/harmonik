package core

// cp004_cp005_conformance_test.go — Conformance tests for CP-004 + CP-005
//
// specs/control-points.md §4.1.CP-004 + CP-005:
//
//	The four Kinds' triggers, evaluator inputs, evaluator return types,
//	outcome-action enums, and boundary-classification rules MUST match the
//	§4.1 table. No ControlPoint may deviate from its Kind's row.
//
// §4.1.CP-005 table:
//
//	| Kind   | Trigger               | Evaluator returns              | OutcomeActions                    | Boundary rule            |
//	|--------|-----------------------|--------------------------------|-----------------------------------|--------------------------|
//	| Gate   | Transition attempt    | {allow, deny} + optional reason| {allow, deny, escalate-to-human}  | Mechanism OR cognition   |
//	| Hook   | Event match           | Side-effect descriptor         | {fire-side-effect, no-op}         | Mechanism OR cognition   |
//	| Guard  | Edge evaluation       | Reordered edge list            | {reorder-edges}                   | Mechanism only           |
//	| Budget | Dispatch attempt /    | {admit, warn, deny}            | {admit, warn, deny}               | Mechanism only           |
//	|        | per-chunk accrual     |                                |                                   |                          |
//
// These tests cover:
//  1. Kind.AllowsCognition() encoding the boundary-rule column.
//  2. Cognition-tagged Guard rejected at MapRegistry.Register (CP-020).
//  3. Cognition-tagged Budget rejected at MapRegistry.Register (CP-005 boundary rule).
//  4. Gate and Hook accept both mechanism and cognition evaluators.
//  5. OutcomeAction per-Kind vocabulary (using OutcomeAction.ValidForKind).

import (
	"errors"
	"testing"
)

// ---------------------------------------------------------------------------
// §4.1.CP-005 boundary rule: Kind.AllowsCognition
// ---------------------------------------------------------------------------

// TestCP005_BoundaryRule_AllowsCognition verifies that Kind.AllowsCognition()
// encodes the boundary-classification column of the §4.1 table correctly:
//
//   - Gate → true (mechanism OR cognition)
//   - Hook → true (mechanism OR cognition)
//   - Guard → false (mechanism only; CP-020)
//   - Budget → false (mechanism only; CP-005)
func TestCP005_BoundaryRule_AllowsCognition(t *testing.T) {
	t.Parallel()

	cases := []struct {
		kind Kind
		want bool
	}{
		{KindGate, true},
		{KindHook, true},
		{KindGuard, false},
		{KindBudget, false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(string(tc.kind), func(t *testing.T) {
			t.Parallel()

			got := tc.kind.AllowsCognition()
			if got != tc.want {
				t.Errorf("Kind(%q).AllowsCognition() = %v, want %v", tc.kind, got, tc.want)
			}
		})
	}
}

// TestCP005_BoundaryRule_UnknownKind verifies that AllowsCognition() returns
// false for an unknown Kind value — matching the sentinel "false / invalid"
// convention for unrecognised values.
func TestCP005_BoundaryRule_UnknownKind(t *testing.T) {
	t.Parallel()

	unknown := Kind("UnknownKind")
	if unknown.AllowsCognition() {
		t.Errorf("Kind(%q).AllowsCognition() = true, want false for unknown Kind", unknown)
	}
}

// ---------------------------------------------------------------------------
// §4.1.CP-005 boundary rule: registry rejection of cognition-only Kinds
// ---------------------------------------------------------------------------

// cp005FixtureCognitionBudget returns a cognition-tagged Budget ControlPoint —
// forbidden per the §4.1 boundary-rule table.
func cp005FixtureCognitionBudget(t *testing.T, name string) ControlPoint {
	t.Helper()
	dp := &DelegationPath{
		Role:              "reviewer",
		ModelClass:        "reviewer-tier-1",
		InputSchemaRef:    "budget-input-v1",
		ResponseSchemaRef: "budget-response-v1",
		PromptTemplateRef: "budget-prompt-v1",
	}
	return ControlPoint{
		Name:    name,
		Kind:    KindBudget,
		Trigger: Trigger{Name: "dispatch"},
		Evaluator: Evaluator{
			Mode:           ModeTagCognition,
			DelegationPath: dp,
		},
		OutcomeAction: OutcomeActionAdmit,
		Payload: KindPayload{Budget: &BudgetPayload{
			Resource:         BudgetResourceTokens,
			Scope:            BudgetScopePerRun,
			Limit:            10000,
			WarningThreshold: 0.8,
			ScopeTarget:      ScopeTargetWildcard(),
		}},
		Axes:          BaselineAxisTags,
		ModeTag:       ModeTagCognition,
		SchemaVersion: 1,
	}
}

// TestCP005_CognitionBudgetRejectedAtRegistration verifies that a Budget
// ControlPoint with a cognition-tagged evaluator is rejected with
// ErrCognitionBudget at MapRegistry.Register.
//
// specs/control-points.md §4.1.CP-005 boundary-classification table:
// Budget MUST be mechanism-tagged (mechanism only).
func TestCP005_CognitionBudgetRejectedAtRegistration(t *testing.T) {
	t.Parallel()

	reg := NewMapRegistry()
	bad := cp005FixtureCognitionBudget(t, "bad-budget")

	err := reg.Register(bad)
	if err == nil {
		t.Fatal("Register cognition-tagged Budget: expected error, got nil")
	}
	if !errors.Is(err, ErrCognitionBudget) {
		t.Errorf("Register cognition-tagged Budget: got %v, want ErrCognitionBudget", err)
	}

	// Registry must remain empty; the bad registration must not persist.
	all := reg.All()
	if len(all) != 0 {
		t.Errorf("registry has %d entries after cognition-Budget rejection, want 0", len(all))
	}
}

// TestCP005_CognitionGuardRejectedAtRegistration verifies the adjacent
// cognition-Guard path (CP-020) using the MapRegistry, so both mechanism-only
// Kinds are exercised in the same test file.
func TestCP005_CognitionGuardRejectedAtRegistration(t *testing.T) {
	t.Parallel()

	reg := NewMapRegistry()
	bad := cp002FixtureCognitionGuard(t, "bad-guard")

	err := reg.Register(bad)
	if err == nil {
		t.Fatal("Register cognition-tagged Guard: expected error, got nil")
	}
	if !errors.Is(err, ErrCognitionGuard) {
		t.Errorf("Register cognition-tagged Guard: got %v, want ErrCognitionGuard", err)
	}

	if len(reg.All()) != 0 {
		t.Errorf("registry has %d entries after cognition-Guard rejection, want 0", len(reg.All()))
	}
}

// TestCP005_MechanismBudgetAccepted verifies the valid adjacent path: a
// mechanism-tagged Budget registers without error.
func TestCP005_MechanismBudgetAccepted(t *testing.T) {
	t.Parallel()

	reg := NewMapRegistry()
	good := registryFixtureControlPoint(t, KindBudget)

	if err := reg.Register(good); err != nil {
		t.Errorf("Register mechanism-tagged Budget: unexpected error: %v", err)
	}
}

// TestCP005_CognitionGateAndHookAccepted verifies that Gate and Hook accept
// cognition-tagged evaluators (mechanism OR cognition per the boundary table).
func TestCP005_CognitionGateAndHookAccepted(t *testing.T) {
	t.Parallel()

	cases := []struct {
		kind Kind
		name string
	}{
		{KindGate, "cognition-gate"},
		{KindHook, "cognition-hook"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(string(tc.kind), func(t *testing.T) {
			t.Parallel()

			reg := NewMapRegistry()

			// Build a cognition-tagged ControlPoint of the given Kind.
			cp := registryFixtureControlPoint(t, tc.kind)
			cp.Name = tc.name
			dp := &DelegationPath{
				Role:              "reviewer",
				ModelClass:        "reviewer-tier-1",
				InputSchemaRef:    string(tc.kind) + "-input-v1",
				ResponseSchemaRef: string(tc.kind) + "-response-v1",
				PromptTemplateRef: string(tc.kind) + "-prompt-v1",
			}
			cp.Evaluator = Evaluator{Mode: ModeTagCognition, DelegationPath: dp}
			cp.ModeTag = ModeTagCognition

			if err := reg.Register(cp); err != nil {
				t.Errorf("Register cognition-tagged %s: unexpected error: %v", tc.kind, err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// §4.1.CP-005: outcome-action enum per Kind
// ---------------------------------------------------------------------------

// TestCP005_OutcomeActionVocabularyPerKind verifies that each Kind's valid
// outcome-action set matches the §4.1 table exactly, using
// OutcomeAction.ValidForKind as the declarative encoding.
//
// All seven declared OutcomeAction values are tested against all four Kinds
// to ensure the disjoint vocabulary is complete and correct.
func TestCP005_OutcomeActionVocabularyPerKind(t *testing.T) {
	t.Parallel()

	type row struct {
		kind   Kind
		action OutcomeAction
		valid  bool
	}

	table := []row{
		// Gate: {allow, deny, escalate-to-human}
		{KindGate, OutcomeActionAllow, true},
		{KindGate, OutcomeActionDeny, true},
		{KindGate, OutcomeActionEscalateToHuman, true},
		{KindGate, OutcomeActionSideEffect, false},
		{KindGate, OutcomeActionReorder, false},
		{KindGate, OutcomeActionAdmit, false},
		{KindGate, OutcomeActionWarn, false},

		// Hook: {side-effect} (a Hook never halts; no-op is represented by no side-effect)
		{KindHook, OutcomeActionSideEffect, true},
		{KindHook, OutcomeActionAllow, false},
		{KindHook, OutcomeActionDeny, false},
		{KindHook, OutcomeActionEscalateToHuman, false},
		{KindHook, OutcomeActionReorder, false},
		{KindHook, OutcomeActionAdmit, false},
		{KindHook, OutcomeActionWarn, false},

		// Guard: {reorder}
		{KindGuard, OutcomeActionReorder, true},
		{KindGuard, OutcomeActionAllow, false},
		{KindGuard, OutcomeActionDeny, false},
		{KindGuard, OutcomeActionEscalateToHuman, false},
		{KindGuard, OutcomeActionSideEffect, false},
		{KindGuard, OutcomeActionAdmit, false},
		{KindGuard, OutcomeActionWarn, false},

		// Budget: {admit, warn, deny}
		{KindBudget, OutcomeActionAdmit, true},
		{KindBudget, OutcomeActionWarn, true},
		{KindBudget, OutcomeActionDeny, true},
		{KindBudget, OutcomeActionAllow, false},
		{KindBudget, OutcomeActionEscalateToHuman, false},
		{KindBudget, OutcomeActionSideEffect, false},
		{KindBudget, OutcomeActionReorder, false},
	}

	for _, r := range table {
		r := r
		t.Run(string(r.kind)+"/"+string(r.action), func(t *testing.T) {
			t.Parallel()

			got := r.action.ValidForKind(r.kind)
			if got != r.valid {
				t.Errorf("OutcomeAction(%q).ValidForKind(%q) = %v, want %v",
					r.action, r.kind, got, r.valid)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// §4.1.CP-004: no ControlPoint may deviate from its Kind's row
// ---------------------------------------------------------------------------

// TestCP004_WrongOutcomeActionForKindRejectedByValid verifies that
// ControlPoint.Valid() rejects a ControlPoint whose OutcomeAction does not
// match its Kind's row in the §4.1 table. "No ControlPoint may deviate from
// its Kind's row" (CP-004).
func TestCP004_WrongOutcomeActionForKindRejectedByValid(t *testing.T) {
	t.Parallel()

	cases := []struct {
		kind   Kind
		action OutcomeAction
	}{
		// Gate must not use Hook/Guard/Budget actions.
		{KindGate, OutcomeActionSideEffect},
		{KindGate, OutcomeActionReorder},
		{KindGate, OutcomeActionAdmit},
		// Hook must not use Gate/Guard/Budget actions.
		{KindHook, OutcomeActionAllow},
		{KindHook, OutcomeActionDeny},
		{KindHook, OutcomeActionReorder},
		// Guard must not use Gate/Hook/Budget actions.
		{KindGuard, OutcomeActionAllow},
		{KindGuard, OutcomeActionDeny},
		{KindGuard, OutcomeActionSideEffect},
		// Budget must not use Gate/Hook/Guard actions.
		{KindBudget, OutcomeActionAllow},
		{KindBudget, OutcomeActionEscalateToHuman},
		{KindBudget, OutcomeActionReorder},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(string(tc.kind)+"/"+string(tc.action), func(t *testing.T) {
			t.Parallel()

			cp := registryFixtureControlPoint(t, tc.kind)
			cp.OutcomeAction = tc.action
			if cp.Valid() {
				t.Errorf("ControlPoint{Kind=%q, OutcomeAction=%q}.Valid() = true, want false (CP-004)",
					tc.kind, tc.action)
			}
		})
	}
}

// TestCP004_CorrectOutcomeActionPerKindIsValid verifies the positive path:
// a ControlPoint with the canonical OutcomeAction for its Kind is valid per
// the §4.1 table. This confirms the table rows are correctly encoded.
func TestCP004_CorrectOutcomeActionPerKindIsValid(t *testing.T) {
	t.Parallel()

	cases := []struct {
		kind   Kind
		action OutcomeAction
	}{
		{KindGate, OutcomeActionAllow},
		{KindGate, OutcomeActionDeny},
		{KindGate, OutcomeActionEscalateToHuman},
		{KindHook, OutcomeActionSideEffect},
		{KindGuard, OutcomeActionReorder},
		{KindBudget, OutcomeActionAdmit},
		{KindBudget, OutcomeActionWarn},
		{KindBudget, OutcomeActionDeny},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(string(tc.kind)+"/"+string(tc.action), func(t *testing.T) {
			t.Parallel()

			cp := registryFixtureControlPoint(t, tc.kind)
			cp.OutcomeAction = tc.action
			if !cp.Valid() {
				t.Errorf("ControlPoint{Kind=%q, OutcomeAction=%q}.Valid() = false, want true (CP-004)",
					tc.kind, tc.action)
			}
		})
	}
}

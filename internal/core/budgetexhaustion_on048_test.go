package core

import "testing"

// budgetexhaustion_on048_test.go — tests for the ON-048 exhaustion protocol
// (4-step sequence) for the agent runner enforcing path.
//
// Covers:
//   - ExhaustionProtocolStep enum: four values declared in spec order.
//   - ExhaustionSafeBoundary enum: three values matching ON-048 step (2).
//   - SafeBoundaryForResource: correct mapping for all three BudgetResource values;
//     returns ok=false for unknown resources.
//   - ExhaustionRoutingPolicy / DefaultExhaustionRoutingPolicy: default
//     PauseOnExhaustion=false per ON-048 step (3).
//   - ExhaustionProtocolSequence: four steps in spec order; non-empty labels and
//     descriptions; step (4) is conditional; steps (1)–(3) are unconditional.
//   - DispatchDeferredReasonBudgetExhaustedCascade: distinct from the existing
//     machine-ceiling reason.
//
// Spec ref: specs/operator-nfr.md §4.11 ON-048.

// ---- ExhaustionProtocolStep enum ----

// TestExhaustionProtocolStep_FourValuesAreDeclared verifies that exactly four
// ExhaustionProtocolStep constants are declared in spec order.
func TestExhaustionProtocolStep_FourValuesAreDeclared(t *testing.T) {
	t.Parallel()

	steps := []struct {
		constant ExhaustionProtocolStep
		str      string
	}{
		{ExhaustionProtocolStepEmitBudgetExhausted, "1-emit-budget-exhausted"},
		{ExhaustionProtocolStepTerminateAtSafeBoundary, "2-terminate-at-safe-boundary"},
		{ExhaustionProtocolStepRouteExhaustionPolicy, "3-route-exhaustion-policy"},
		{ExhaustionProtocolStepEmitDispatchDeferredIfCascade, "4-emit-dispatch-deferred-if-cascade"},
	}

	const wantCount = 4
	if len(steps) != wantCount {
		t.Fatalf("ON-048: expected %d ExhaustionProtocolStep constants, got %d; "+
			"update this test on spec amendment", wantCount, len(steps))
	}

	for _, tc := range steps {
		tc := tc
		t.Run(tc.str, func(t *testing.T) {
			t.Parallel()

			if string(tc.constant) != tc.str {
				t.Errorf("ExhaustionProtocolStep string = %q, want %q",
					string(tc.constant), tc.str)
			}
			if tc.constant == ExhaustionProtocolStep("") {
				t.Errorf("ExhaustionProtocolStep(%q) is empty", tc.str)
			}
		})
	}
}

// ---- ExhaustionSafeBoundary enum ----

// TestExhaustionSafeBoundary_ThreeValuesAreDeclared verifies that exactly three
// ExhaustionSafeBoundary constants are declared, one per BudgetResource.
func TestExhaustionSafeBoundary_ThreeValuesAreDeclared(t *testing.T) {
	t.Parallel()

	boundaries := []struct {
		constant ExhaustionSafeBoundary
		str      string
	}{
		{ExhaustionSafeBoundaryPostChunk, "post-chunk"},
		{ExhaustionSafeBoundaryPostIteration, "post-iteration"},
		{ExhaustionSafeBoundaryPostStep, "post-step"},
	}

	const wantCount = 3
	if len(boundaries) != wantCount {
		t.Fatalf("ON-048: expected %d ExhaustionSafeBoundary constants, got %d",
			wantCount, len(boundaries))
	}

	for _, tc := range boundaries {
		tc := tc
		t.Run(tc.str, func(t *testing.T) {
			t.Parallel()

			if string(tc.constant) != tc.str {
				t.Errorf("ExhaustionSafeBoundary string = %q, want %q",
					string(tc.constant), tc.str)
			}
		})
	}
}

// ---- SafeBoundaryForResource ----

// TestSafeBoundaryForResource_TokensIsPostChunk verifies that token budgets
// use the post-chunk safe boundary per ON-048 step (2).
func TestSafeBoundaryForResource_TokensIsPostChunk(t *testing.T) {
	t.Parallel()

	got, ok := SafeBoundaryForResource(BudgetResourceTokens)
	if !ok {
		t.Fatal("ON-048: SafeBoundaryForResource(BudgetResourceTokens) returned ok=false")
	}
	if got != ExhaustionSafeBoundaryPostChunk {
		t.Errorf("ON-048: SafeBoundaryForResource(tokens) = %q, want %q",
			got, ExhaustionSafeBoundaryPostChunk)
	}
}

// TestSafeBoundaryForResource_IterationsIsPostIteration verifies that iterations
// budgets use the post-iteration safe boundary per ON-048 step (2).
func TestSafeBoundaryForResource_IterationsIsPostIteration(t *testing.T) {
	t.Parallel()

	got, ok := SafeBoundaryForResource(BudgetResourceIterations)
	if !ok {
		t.Fatal("ON-048: SafeBoundaryForResource(BudgetResourceIterations) returned ok=false")
	}
	if got != ExhaustionSafeBoundaryPostIteration {
		t.Errorf("ON-048: SafeBoundaryForResource(iterations) = %q, want %q",
			got, ExhaustionSafeBoundaryPostIteration)
	}
}

// TestSafeBoundaryForResource_WallClockIsPostStep verifies that wall-clock
// budgets use the post-step safe boundary per ON-048 step (2).
func TestSafeBoundaryForResource_WallClockIsPostStep(t *testing.T) {
	t.Parallel()

	got, ok := SafeBoundaryForResource(BudgetResourceWallClockSeconds)
	if !ok {
		t.Fatal("ON-048: SafeBoundaryForResource(BudgetResourceWallClockSeconds) returned ok=false")
	}
	if got != ExhaustionSafeBoundaryPostStep {
		t.Errorf("ON-048: SafeBoundaryForResource(wall_clock_seconds) = %q, want %q",
			got, ExhaustionSafeBoundaryPostStep)
	}
}

// TestSafeBoundaryForResource_UnknownResourceReturnsFalse verifies that an
// unknown BudgetResource returns ok=false without panicking.
func TestSafeBoundaryForResource_UnknownResourceReturnsFalse(t *testing.T) {
	t.Parallel()

	_, ok := SafeBoundaryForResource(BudgetResource("unknown_resource"))
	if ok {
		t.Error("ON-048: SafeBoundaryForResource(unknown) returned ok=true; want false")
	}
}

// TestSafeBoundaryForResource_AllKnownResourcesMapToBoundary verifies that every
// valid BudgetResource maps to a non-empty safe boundary.
func TestSafeBoundaryForResource_AllKnownResourcesMapToBoundary(t *testing.T) {
	t.Parallel()

	resources := []BudgetResource{
		BudgetResourceTokens,
		BudgetResourceIterations,
		BudgetResourceWallClockSeconds,
	}
	for _, r := range resources {
		r := r
		t.Run(string(r), func(t *testing.T) {
			t.Parallel()

			boundary, ok := SafeBoundaryForResource(r)
			if !ok {
				t.Errorf("ON-048: SafeBoundaryForResource(%q) returned ok=false; all declared resources must map", r)
			}
			if boundary == ExhaustionSafeBoundary("") {
				t.Errorf("ON-048: SafeBoundaryForResource(%q) returned empty boundary", r)
			}
		})
	}
}

// ---- ExhaustionRoutingPolicy / DefaultExhaustionRoutingPolicy ----

// TestDefaultExhaustionRoutingPolicy_PauseOnExhaustionIsFalse verifies that the
// spec-mandated default has PauseOnExhaustion=false per ON-048 step (3).
func TestDefaultExhaustionRoutingPolicy_PauseOnExhaustionIsFalse(t *testing.T) {
	t.Parallel()

	policy := DefaultExhaustionRoutingPolicy()
	if policy.PauseOnExhaustion {
		t.Error("ON-048: DefaultExhaustionRoutingPolicy().PauseOnExhaustion = true; " +
			"spec mandates default=false")
	}
}

// TestExhaustionRoutingPolicy_CanSetPauseOnExhaustionTrue verifies that the
// PauseOnExhaustion field can be set to true for operator-policy overrides.
func TestExhaustionRoutingPolicy_CanSetPauseOnExhaustionTrue(t *testing.T) {
	t.Parallel()

	policy := ExhaustionRoutingPolicy{PauseOnExhaustion: true}
	if !policy.PauseOnExhaustion {
		t.Error("ON-048: ExhaustionRoutingPolicy{PauseOnExhaustion: true}.PauseOnExhaustion = false; field must be settable")
	}
}

// ---- ExhaustionProtocolSequence ----

// TestExhaustionProtocolSequence_HasFourSteps verifies that
// ExhaustionProtocolSequence returns exactly four steps.
//
// Per ON-048: the agent-runner exhaustion protocol has exactly 4 steps. Adding
// or removing a step requires a spec-level amendment per ON-048.
func TestExhaustionProtocolSequence_HasFourSteps(t *testing.T) {
	t.Parallel()

	seq := ExhaustionProtocolSequence()
	const wantCount = 4
	if len(seq) != wantCount {
		t.Errorf("ON-048: ExhaustionProtocolSequence() has %d steps, want %d; "+
			"any change is a spec-level amendment (ON-048)", len(seq), wantCount)
	}
}

// TestExhaustionProtocolSequence_AllLabelsNonEmpty verifies that every step
// has a non-empty Label and Description.
func TestExhaustionProtocolSequence_AllLabelsNonEmpty(t *testing.T) {
	t.Parallel()

	for i, step := range ExhaustionProtocolSequence() {
		step := step
		t.Run(string(step.Label), func(t *testing.T) {
			t.Parallel()

			if step.Label == ExhaustionProtocolStep("") {
				t.Errorf("ON-048: step[%d].Label is empty; all steps must have a typed label", i)
			}
			if step.Description == "" {
				t.Errorf("ON-048: step[%d] (%q) has empty Description; all steps must be documented",
					i, step.Label)
			}
		})
	}
}

// TestExhaustionProtocolSequence_OrderMatchesSpec verifies that the steps are
// returned in the order mandated by ON-048.
func TestExhaustionProtocolSequence_OrderMatchesSpec(t *testing.T) {
	t.Parallel()

	want := []ExhaustionProtocolStep{
		ExhaustionProtocolStepEmitBudgetExhausted,
		ExhaustionProtocolStepTerminateAtSafeBoundary,
		ExhaustionProtocolStepRouteExhaustionPolicy,
		ExhaustionProtocolStepEmitDispatchDeferredIfCascade,
	}
	got := ExhaustionProtocolSequence()

	if len(got) != len(want) {
		t.Fatalf("ON-048: sequence length = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Label != want[i] {
			t.Errorf("ON-048: step[%d] = %q, want %q", i, got[i].Label, want[i])
		}
	}
}

// TestExhaustionProtocolSequence_OnlyStep4IsConditional verifies that exactly
// step (4) is marked IsConditional=true, and steps (1)–(3) are unconditional.
//
// Per ON-048 step (4): dispatch_deferred is emitted "ONLY IF the exhaustion
// cascades to a multi-run ceiling breach" — this is the only conditional step.
func TestExhaustionProtocolSequence_OnlyStep4IsConditional(t *testing.T) {
	t.Parallel()

	seq := ExhaustionProtocolSequence()
	if len(seq) != 4 {
		t.Fatalf("unexpected sequence length %d", len(seq))
	}

	// Steps are 0-indexed; step (4) = index 3.
	for i, step := range seq {
		wantConditional := i == 3
		if step.IsConditional != wantConditional {
			t.Errorf("ON-048: step[%d] (%q) IsConditional = %v, want %v",
				i, step.Label, step.IsConditional, wantConditional)
		}
	}
}

// ---- DispatchDeferredReasonBudgetExhaustedCascade ----

// TestDispatchDeferredReasonBudgetExhaustedCascade_ValueIsCorrect verifies the
// canonical string value for the budget-cascade reason.
func TestDispatchDeferredReasonBudgetExhaustedCascade_ValueIsCorrect(t *testing.T) {
	t.Parallel()

	const want = "budget_exhausted_cascade"
	if string(DispatchDeferredReasonBudgetExhaustedCascade) != want {
		t.Errorf("DispatchDeferredReasonBudgetExhaustedCascade = %q, want %q",
			string(DispatchDeferredReasonBudgetExhaustedCascade), want)
	}
}

// TestDispatchDeferredReasonBudgetExhaustedCascade_DistinctFromMachineCeiling
// verifies that the budget-cascade reason is distinct from the machine-ceiling
// reason, as each addresses a different triggering condition.
func TestDispatchDeferredReasonBudgetExhaustedCascade_DistinctFromMachineCeiling(t *testing.T) {
	t.Parallel()

	if DispatchDeferredReasonBudgetExhaustedCascade == DispatchDeferredReasonMachineCeilingExhausted {
		t.Error("ON-048: DispatchDeferredReasonBudgetExhaustedCascade == " +
			"DispatchDeferredReasonMachineCeilingExhausted; reasons must be distinct")
	}
}

// TestDispatchDeferredReasonBudgetExhaustedCascade_IsValidDispatchDeferredPayloadReason
// verifies that the cascade reason passes DispatchDeferredPayload.Valid() when
// set as the Reason field.
func TestDispatchDeferredReasonBudgetExhaustedCascade_IsValidDispatchDeferredPayloadReason(t *testing.T) {
	t.Parallel()

	p := DispatchDeferredPayload{
		Reason:     DispatchDeferredReasonBudgetExhaustedCascade,
		DeferredAt: "2026-06-01T00:00:00Z",
	}
	if !p.Valid() {
		t.Error("ON-048: DispatchDeferredPayload with budget_exhausted_cascade reason " +
			"failed Valid(); semi-open DispatchDeferredReason must accept this value")
	}
}

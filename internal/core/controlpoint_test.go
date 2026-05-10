package core

import (
	"testing"
)

// registryFixtureControlPoint returns a fully-populated, valid ControlPoint of
// the given Kind for use in Registry and ControlPoint tests (hk-a8bg.77).
//
// Helper prefix: registryFixture.
func registryFixtureControlPoint(t *testing.T, kind Kind) ControlPoint {
	t.Helper()

	expr := PolicyExpression(`run.context["phase"] == "review"`)
	var payload KindPayload
	var trigger Trigger
	var outcomeAction OutcomeAction

	switch kind {
	case KindGate:
		approver := "ops-lead"
		payload = KindPayload{Gate: &GatePayload{
			Subtype:       GateSubtypeApproval,
			AttachPoint:   AttachPointNodePreEntry,
			NamedApprover: &approver,
		}}
		trigger = Trigger{Name: "node-pre-entry"}
		outcomeAction = OutcomeActionAllow

	case KindHook:
		payload = KindPayload{Hook: &HookPayload{
			TriggerEvent:   "on_agent_started",
			SideEffectKind: SideEffectKindEmitEvent,
			HaltOnFailure:  false,
		}}
		trigger = Trigger{Name: "on_agent_started"}
		outcomeAction = OutcomeActionSideEffect

	case KindGuard:
		nodeID := NodeID("node-review")
		payload = KindPayload{Guard: &GuardPayload{
			AppliesToNode: &nodeID,
		}}
		trigger = Trigger{Name: ""}
		outcomeAction = OutcomeActionReorder

	case KindBudget:
		payload = KindPayload{Budget: &BudgetPayload{
			Resource:         BudgetResourceTokens,
			Scope:            BudgetScopePerRun,
			Limit:            10000,
			WarningThreshold: 0.8,
			ScopeTarget:      ScopeTargetWildcard(),
		}}
		trigger = Trigger{Name: "dispatch"}
		outcomeAction = OutcomeActionAdmit

	default:
		t.Fatalf("registryFixtureControlPoint: unsupported kind %q", kind)
	}

	return ControlPoint{
		Name:          string(kind) + "-test-cp",
		Kind:          kind,
		Trigger:       trigger,
		Evaluator:     Evaluator{Mode: ModeTagMechanism, Expression: &expr},
		OutcomeAction: outcomeAction,
		Payload:       payload,
		Axes:          BaselineAxisTags,
		ModeTag:       ModeTagMechanism,
		SchemaVersion: 1,
	}
}

// TestControlPointValid_AllKinds verifies that a well-formed ControlPoint for
// each Kind passes Valid() (specs/control-points.md §6.1, CP-001).
func TestControlPointValid_AllKinds(t *testing.T) {
	t.Parallel()

	kinds := []Kind{KindGate, KindHook, KindGuard, KindBudget}
	for _, k := range kinds {
		k := k
		t.Run(string(k), func(t *testing.T) {
			t.Parallel()

			cp := registryFixtureControlPoint(t, k)
			if !cp.Valid() {
				t.Errorf("ControlPoint{Kind=%q}.Valid() = false, want true", k)
			}
		})
	}
}

// TestControlPointValid_EmptyName verifies that a ControlPoint with an empty
// Name is invalid (specs/control-points.md §6.1: name unique within daemon registry).
func TestControlPointValid_EmptyName(t *testing.T) {
	t.Parallel()

	cp := registryFixtureControlPoint(t, KindGate)
	cp.Name = ""
	if cp.Valid() {
		t.Error("ControlPoint{Name=\"\"}.Valid() = true, want false")
	}
}

// TestControlPointValid_InvalidKind verifies that an unknown Kind causes
// Valid() to return false (specs/control-points.md §4.9: unknown Kind rejected).
func TestControlPointValid_InvalidKind(t *testing.T) {
	t.Parallel()

	cp := registryFixtureControlPoint(t, KindGate)
	cp.Kind = Kind("BadKind")
	if cp.Valid() {
		t.Error("ControlPoint{Kind=\"BadKind\"}.Valid() = true, want false")
	}
}

// TestControlPointValid_ModeMismatch verifies that a ControlPoint whose ModeTag
// does not match Evaluator.Mode is invalid (specs/control-points.md §6.1).
func TestControlPointValid_ModeMismatch(t *testing.T) {
	t.Parallel()

	cp := registryFixtureControlPoint(t, KindGate)
	cp.ModeTag = ModeTagCognition // Evaluator.Mode remains ModeTagMechanism
	if cp.Valid() {
		t.Error("ControlPoint with ModeTag/Evaluator.Mode mismatch.Valid() = true, want false")
	}
}

// TestControlPointValid_WrongOutcomeActionForKind verifies that Valid() returns
// false when OutcomeAction does not match the ControlPoint's Kind
// (specs/control-points.md §4.1.CP-005 per-Kind table).
func TestControlPointValid_WrongOutcomeActionForKind(t *testing.T) {
	t.Parallel()

	cases := []struct {
		kind   Kind
		action OutcomeAction
	}{
		{KindGate, OutcomeActionSideEffect},
		{KindGate, OutcomeActionReorder},
		{KindHook, OutcomeActionAllow},
		{KindGuard, OutcomeActionDeny},
		{KindBudget, OutcomeActionReorder},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(string(tc.kind)+"/"+string(tc.action), func(t *testing.T) {
			t.Parallel()

			cp := registryFixtureControlPoint(t, tc.kind)
			cp.OutcomeAction = tc.action
			if cp.Valid() {
				t.Errorf("ControlPoint{Kind=%q, OutcomeAction=%q}.Valid() = true, want false", tc.kind, tc.action)
			}
		})
	}
}

// TestControlPointValid_WrongPayloadForKind verifies that Valid() returns false
// when the Payload does not match the ControlPoint's Kind
// (specs/control-points.md §6.1 KindPayload discriminated union).
func TestControlPointValid_WrongPayloadForKind(t *testing.T) {
	t.Parallel()

	// Build a Gate-valid payload then assign it to a Hook ControlPoint.
	cp := registryFixtureControlPoint(t, KindHook)
	approver := "ops-lead"
	cp.Payload = KindPayload{Gate: &GatePayload{
		Subtype:       GateSubtypeApproval,
		AttachPoint:   AttachPointNodePreEntry,
		NamedApprover: &approver,
	}}
	if cp.Valid() {
		t.Error("Hook ControlPoint with Gate payload.Valid() = true, want false")
	}
}

// TestControlPointValid_InvalidSchemaVersion verifies that a zero or negative
// SchemaVersion is invalid (specs/control-points.md §4.7.CP-038).
func TestControlPointValid_InvalidSchemaVersion(t *testing.T) {
	t.Parallel()

	cases := []struct {
		label string
		v     int
	}{
		{"zero", 0},
		{"negative_one", -1},
		{"negative_large", -100},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.label, func(t *testing.T) {
			t.Parallel()

			cp := registryFixtureControlPoint(t, KindGate)
			cp.SchemaVersion = tc.v
			if cp.Valid() {
				t.Errorf("ControlPoint{SchemaVersion=%d}.Valid() = true, want false", tc.v)
			}
		})
	}
}

// TestOutcomeActionValidForKind verifies the per-Kind vocabulary declared in
// specs/control-points.md §4.1.CP-005.
func TestOutcomeActionValidForKind(t *testing.T) {
	t.Parallel()

	cases := []struct {
		action OutcomeAction
		kind   Kind
		want   bool
	}{
		// Gate actions
		{OutcomeActionAllow, KindGate, true},
		{OutcomeActionDeny, KindGate, true},
		{OutcomeActionEscalateToHuman, KindGate, true},
		{OutcomeActionSideEffect, KindGate, false},
		{OutcomeActionReorder, KindGate, false},
		{OutcomeActionAdmit, KindGate, false},
		// Hook actions
		{OutcomeActionSideEffect, KindHook, true},
		{OutcomeActionAllow, KindHook, false},
		{OutcomeActionDeny, KindHook, false},
		// Guard actions
		{OutcomeActionReorder, KindGuard, true},
		{OutcomeActionAllow, KindGuard, false},
		{OutcomeActionDeny, KindGuard, false},
		// Budget actions
		{OutcomeActionAdmit, KindBudget, true},
		{OutcomeActionWarn, KindBudget, true},
		{OutcomeActionDeny, KindBudget, true},
		{OutcomeActionAllow, KindBudget, false},
		{OutcomeActionReorder, KindBudget, false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(string(tc.kind)+"/"+string(tc.action), func(t *testing.T) {
			t.Parallel()

			got := tc.action.ValidForKind(tc.kind)
			if got != tc.want {
				t.Errorf("OutcomeAction(%q).ValidForKind(%q) = %v, want %v",
					tc.action, tc.kind, got, tc.want)
			}
		})
	}
}

// TestKindPayloadValidForKind verifies that KindPayload.ValidForKind enforces
// exactly-one-non-nil invariant per Kind (specs/control-points.md §6.1).
func TestKindPayloadValidForKind(t *testing.T) {
	t.Parallel()

	// Empty (all nil) is invalid for all Kinds.
	var empty KindPayload
	for _, k := range []Kind{KindGate, KindHook, KindGuard, KindBudget} {
		k := k
		t.Run("empty/"+string(k), func(t *testing.T) {
			t.Parallel()
			if empty.ValidForKind(k) {
				t.Errorf("empty KindPayload.ValidForKind(%q) = true, want false", k)
			}
		})
	}

	// Two-non-nil is invalid.
	approver := "ops-lead"
	multi := KindPayload{
		Gate: &GatePayload{
			Subtype:       GateSubtypeApproval,
			AttachPoint:   AttachPointNodePreEntry,
			NamedApprover: &approver,
		},
		Guard: &GuardPayload{},
	}
	if multi.ValidForKind(KindGate) {
		t.Error("KindPayload with two non-nil fields.ValidForKind(KindGate) = true, want false")
	}
}

// TestEvaluatorValid_Mechanism verifies that a mechanism-tagged Evaluator with
// a non-empty Expression and no DelegationPath is valid
// (specs/control-points.md §6.1 RECORD Evaluator).
func TestEvaluatorValid_Mechanism(t *testing.T) {
	t.Parallel()

	expr := PolicyExpression(`run.state.id == "start"`)
	e := Evaluator{Mode: ModeTagMechanism, Expression: &expr}
	if !e.Valid() {
		t.Error("mechanism Evaluator.Valid() = false, want true")
	}
}

// TestEvaluatorValid_Cognition verifies that a cognition-tagged Evaluator with
// a valid DelegationPath and no Expression is valid
// (specs/control-points.md §6.1 RECORD Evaluator, §6.1.5).
func TestEvaluatorValid_Cognition(t *testing.T) {
	t.Parallel()

	e := Evaluator{
		Mode: ModeTagCognition,
		DelegationPath: &DelegationPath{
			Role:              "reviewer",
			ModelClass:        "reviewer-tier-1",
			InputSchemaRef:    "gate-input-v1",
			ResponseSchemaRef: "gate-response-v1",
			PromptTemplateRef: "gate-prompt-v1",
		},
	}
	if !e.Valid() {
		t.Error("cognition Evaluator.Valid() = false, want true")
	}
}

// TestEvaluatorValid_MechanismMissingExpression verifies that a mechanism
// Evaluator without Expression is invalid.
func TestEvaluatorValid_MechanismMissingExpression(t *testing.T) {
	t.Parallel()

	e := Evaluator{Mode: ModeTagMechanism}
	if e.Valid() {
		t.Error("mechanism Evaluator without Expression.Valid() = true, want false")
	}
}

// TestEvaluatorValid_CognitionMissingPath verifies that a cognition Evaluator
// without DelegationPath is invalid.
func TestEvaluatorValid_CognitionMissingPath(t *testing.T) {
	t.Parallel()

	e := Evaluator{Mode: ModeTagCognition}
	if e.Valid() {
		t.Error("cognition Evaluator without DelegationPath.Valid() = true, want false")
	}
}

// TestEvaluatorValid_MechanismWithPath verifies that a mechanism Evaluator that
// also carries a DelegationPath is invalid (mutually exclusive).
func TestEvaluatorValid_MechanismWithPath(t *testing.T) {
	t.Parallel()

	expr := PolicyExpression("true")
	e := Evaluator{
		Mode:       ModeTagMechanism,
		Expression: &expr,
		DelegationPath: &DelegationPath{
			Role:              "reviewer",
			ModelClass:        "reviewer-tier-1",
			InputSchemaRef:    "gate-input-v1",
			ResponseSchemaRef: "gate-response-v1",
			PromptTemplateRef: "gate-prompt-v1",
		},
	}
	if e.Valid() {
		t.Error("mechanism Evaluator with DelegationPath.Valid() = true, want false")
	}
}

// TestDelegationPathValid verifies the five-field non-empty constraint on
// DelegationPath (specs/control-points.md §6.1.5).
func TestDelegationPathValid(t *testing.T) {
	t.Parallel()

	full := DelegationPath{
		Role:              "reviewer",
		ModelClass:        "reviewer-tier-1",
		InputSchemaRef:    "gate-input-v1",
		ResponseSchemaRef: "gate-response-v1",
		PromptTemplateRef: "gate-prompt-v1",
	}
	if !full.Valid() {
		t.Error("fully-populated DelegationPath.Valid() = false, want true")
	}

	// Each missing field makes it invalid.
	cases := []struct {
		name string
		dp   DelegationPath
	}{
		{"missing_role", DelegationPath{ModelClass: full.ModelClass, InputSchemaRef: full.InputSchemaRef, ResponseSchemaRef: full.ResponseSchemaRef, PromptTemplateRef: full.PromptTemplateRef}},
		{"missing_model_class", DelegationPath{Role: full.Role, InputSchemaRef: full.InputSchemaRef, ResponseSchemaRef: full.ResponseSchemaRef, PromptTemplateRef: full.PromptTemplateRef}},
		{"missing_input_schema_ref", DelegationPath{Role: full.Role, ModelClass: full.ModelClass, ResponseSchemaRef: full.ResponseSchemaRef, PromptTemplateRef: full.PromptTemplateRef}},
		{"missing_response_schema_ref", DelegationPath{Role: full.Role, ModelClass: full.ModelClass, InputSchemaRef: full.InputSchemaRef, PromptTemplateRef: full.PromptTemplateRef}},
		{"missing_prompt_template_ref", DelegationPath{Role: full.Role, ModelClass: full.ModelClass, InputSchemaRef: full.InputSchemaRef, ResponseSchemaRef: full.ResponseSchemaRef}},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if tc.dp.Valid() {
				t.Errorf("DelegationPath(%v).Valid() = true, want false", tc.name)
			}
		})
	}
}

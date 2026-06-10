package core

// s02registrar_hka8bg45_test.go — S02Registrar tests
//
// Covers specs/control-points.md §4.9.CP-043:
//
//   - S02 owns the ControlPoint registry.
//   - S02 constructs ControlPoint instances by reading policy YAML per §4.7.
//   - Construction is pure (no side effects).
//   - RegisterFromDocument populates the registry from a PolicyDocument.
//   - Re-registration with identical body succeeds silently (CP-044).
//   - Divergent-body re-registration fails (CP-044, via MapRegistry).
//   - Construction errors surface for malformed YAML entries.
//   - Registry() returns the Registry interface that consumers (S01, S05) use.
//
// Refs: hk-a8bg.45

import (
	"errors"
	"testing"
)

// ---------------------------------------------------------------------------
// Fixture helpers
// ---------------------------------------------------------------------------

// s02MarkAllSectionsPresent marks all seven CP-035 section presence flags on
// a PolicyDocument that was constructed directly (not via ParsePolicyDocument).
//
// Production code always parses from YAML, so sectionPresence is populated by
// ParsePolicyDocument. Tests that build PolicyDocument via struct literals must
// call this helper to satisfy the CP-035 check in RegisterFromDocument.
func s02MarkAllSectionsPresent(doc PolicyDocument) PolicyDocument {
	doc.sectionPresence = policyDocumentSections{
		metadata:        true,
		roles:           true,
		freedomProfiles: true,
		gates:           true,
		hooks:           true,
		guards:          true,
		budgets:         true,
	}
	return doc
}

// s02FixtureDocument returns a minimal, fully-valid PolicyDocument containing
// one Gate, one Hook, one Guard, and one Budget, with all CP-035 section
// presence flags set.
func s02FixtureDocument() PolicyDocument {
	return s02MarkAllSectionsPresent(PolicyDocument{
		Metadata: PolicyDocumentMeta{
			Name:          "test-policy",
			Version:       "1.0.0",
			Author:        "test-author",
			SchemaVersion: 1,
		},
		Gates: []PolicyGate{
			{
				Name:        "deploy-gate",
				Subtype:     "goal-gate",
				AttachPoint: "node-pre-entry",
				Evaluator: PolicyEvaluatorBlock{
					Mode:       "mechanism",
					Expression: `run.context["phase"] == "deploy"`,
				},
			},
		},
		Hooks: []PolicyHook{
			{
				Name:           "post-merge-hook",
				TriggerEvent:   "on_agent_started",
				SideEffectKind: "emit-event",
				HaltOnFailure:  false,
				Evaluator: PolicyEvaluatorBlock{
					Mode:       "mechanism",
					Expression: `true`,
				},
			},
		},
		Guards: []PolicyGuard{
			{
				Name: "edge-priority-guard",
				Evaluator: PolicyEvaluatorBlock{
					Mode:       "mechanism",
					Expression: `true`,
				},
			},
		},
		Budgets: []PolicyBudget{
			{
				Name:             "token-budget",
				Resource:         "tokens",
				Scope:            "per_run",
				Limit:            10000,
				WarningThreshold: 0.8,
				ScopeTarget:      "*",
			},
		},
	})
}

// s02FixtureApprovalGate returns a PolicyGate with subtype "approval-gate"
// carrying a named_approver.
func s02FixtureApprovalGate() PolicyGate {
	return PolicyGate{
		Name:          "approval-gate",
		Subtype:       "approval-gate",
		AttachPoint:   "node-post-exit",
		NamedApprover: "ops-lead",
		Evaluator: PolicyEvaluatorBlock{
			Mode:       "mechanism",
			Expression: `run.context["needs_approval"] == true`,
		},
	}
}

// s02FixtureQualityGate returns a PolicyGate with subtype "quality-gate"
// carrying a verification_ref.
func s02FixtureQualityGate() PolicyGate {
	return PolicyGate{
		Name:            "quality-gate",
		Subtype:         "quality-gate",
		AttachPoint:     "edge-before-selection",
		VerificationRef: "verify-step-1",
		Evaluator: PolicyEvaluatorBlock{
			Mode:       "mechanism",
			Expression: `outcome.score >= 0.9`,
		},
	}
}

// s02FixtureCognitionGate returns a PolicyGate with a cognition evaluator.
func s02FixtureCognitionGate() PolicyGate {
	return PolicyGate{
		Name:        "cognition-gate",
		Subtype:     "goal-gate",
		AttachPoint: "node-pre-entry",
		Evaluator: PolicyEvaluatorBlock{
			Mode: "cognition",
			DelegationPath: &PolicyDelegationPathBlock{
				Role:              "reviewer",
				ModelClass:        "reviewer-tier-1",
				InputSchemaRef:    "gate-input-v1",
				ResponseSchemaRef: "gate-response-v1",
				PromptTemplateRef: "gate-prompt-v1",
			},
		},
	}
}

// ---------------------------------------------------------------------------
// NewS02Registrar
// ---------------------------------------------------------------------------

// TestS02Registrar_NewReturnsEmptyRegistry verifies that NewS02Registrar
// returns a registrar whose registry is empty.
func TestS02Registrar_NewReturnsEmptyRegistry(t *testing.T) {
	t.Parallel()

	s := NewS02Registrar()
	reg := s.Registry()

	all := reg.All()
	if len(all) != 0 {
		t.Errorf("NewS02Registrar().Registry().All() len = %d, want 0", len(all))
	}
}

// TestS02Registrar_RegistryImplementsInterface verifies that Registry()
// returns a value satisfying the Registry interface.
func TestS02Registrar_RegistryImplementsInterface(t *testing.T) {
	t.Parallel()

	s := NewS02Registrar()
	var _ Registry = s.Registry() // compile-time interface check
}

// ---------------------------------------------------------------------------
// RegisterFromDocument — positive paths
// ---------------------------------------------------------------------------

// TestS02Registrar_RegisterFromDocument_AllKinds verifies that
// RegisterFromDocument populates the registry with one ControlPoint per kind
// when given a valid PolicyDocument containing each kind.
func TestS02Registrar_RegisterFromDocument_AllKinds(t *testing.T) {
	t.Parallel()

	s := NewS02Registrar()
	doc := s02FixtureDocument()

	if err := s.RegisterFromDocument(doc); err != nil {
		t.Fatalf("RegisterFromDocument: unexpected error: %v", err)
	}

	reg := s.Registry()
	all := reg.All()
	if len(all) != 4 {
		t.Errorf("All() len = %d, want 4 (gate + hook + guard + budget)", len(all))
	}

	// Verify each expected name is present.
	for _, name := range []string{"deploy-gate", "post-merge-hook", "edge-priority-guard", "token-budget"} {
		if _, ok := reg.LookupByName(name); !ok {
			t.Errorf("LookupByName(%q) not found after RegisterFromDocument", name)
		}
	}
}

// TestS02Registrar_RegisterFromDocument_GateKindAndFields verifies that
// a Gate ControlPoint constructed from a PolicyGate carries the correct
// Kind, Trigger, OutcomeAction, and Payload fields.
func TestS02Registrar_RegisterFromDocument_GateKindAndFields(t *testing.T) {
	t.Parallel()

	s := NewS02Registrar()
	doc := s02FixtureDocument()

	if err := s.RegisterFromDocument(doc); err != nil {
		t.Fatalf("RegisterFromDocument: %v", err)
	}

	cp, ok := s.Registry().LookupByName("deploy-gate")
	if !ok {
		t.Fatal("deploy-gate not found")
	}

	if cp.Kind != KindGate {
		t.Errorf("Kind = %q, want KindGate", cp.Kind)
	}
	if cp.Trigger.Name != "node-pre-entry" {
		t.Errorf("Trigger.Name = %q, want %q", cp.Trigger.Name, "node-pre-entry")
	}
	if cp.OutcomeAction != OutcomeActionAllow {
		t.Errorf("OutcomeAction = %q, want OutcomeActionAllow", cp.OutcomeAction)
	}
	if cp.Payload.Gate == nil {
		t.Fatal("Payload.Gate is nil")
	}
	if cp.Payload.Gate.Subtype != GateSubtypeGoal {
		t.Errorf("Payload.Gate.Subtype = %q, want GateSubtypeGoal", cp.Payload.Gate.Subtype)
	}
	if cp.Payload.Gate.AttachPoint != AttachPointNodePreEntry {
		t.Errorf("Payload.Gate.AttachPoint = %q, want AttachPointNodePreEntry", cp.Payload.Gate.AttachPoint)
	}
	if cp.SchemaVersion != 1 {
		t.Errorf("SchemaVersion = %d, want 1", cp.SchemaVersion)
	}
	if cp.ModeTag != ModeTagMechanism {
		t.Errorf("ModeTag = %q, want ModeTagMechanism", cp.ModeTag)
	}
}

// TestS02Registrar_RegisterFromDocument_HookKindAndFields verifies Hook
// ControlPoint construction.
func TestS02Registrar_RegisterFromDocument_HookKindAndFields(t *testing.T) {
	t.Parallel()

	s := NewS02Registrar()
	doc := s02FixtureDocument()

	if err := s.RegisterFromDocument(doc); err != nil {
		t.Fatalf("RegisterFromDocument: %v", err)
	}

	cp, ok := s.Registry().LookupByName("post-merge-hook")
	if !ok {
		t.Fatal("post-merge-hook not found")
	}

	if cp.Kind != KindHook {
		t.Errorf("Kind = %q, want KindHook", cp.Kind)
	}
	if cp.Trigger.Name != "on_agent_started" {
		t.Errorf("Trigger.Name = %q, want %q", cp.Trigger.Name, "on_agent_started")
	}
	if cp.OutcomeAction != OutcomeActionSideEffect {
		t.Errorf("OutcomeAction = %q, want OutcomeActionSideEffect", cp.OutcomeAction)
	}
	if cp.Payload.Hook == nil {
		t.Fatal("Payload.Hook is nil")
	}
	if cp.Payload.Hook.TriggerEvent != "on_agent_started" {
		t.Errorf("Payload.Hook.TriggerEvent = %q, want %q", cp.Payload.Hook.TriggerEvent, "on_agent_started")
	}
	if cp.Payload.Hook.SideEffectKind != SideEffectKindEmitEvent {
		t.Errorf("Payload.Hook.SideEffectKind = %q, want SideEffectKindEmitEvent", cp.Payload.Hook.SideEffectKind)
	}
}

// TestS02Registrar_RegisterFromDocument_GuardKindAndFields verifies Guard
// ControlPoint construction.
func TestS02Registrar_RegisterFromDocument_GuardKindAndFields(t *testing.T) {
	t.Parallel()

	s := NewS02Registrar()
	doc := s02FixtureDocument()

	if err := s.RegisterFromDocument(doc); err != nil {
		t.Fatalf("RegisterFromDocument: %v", err)
	}

	cp, ok := s.Registry().LookupByName("edge-priority-guard")
	if !ok {
		t.Fatal("edge-priority-guard not found")
	}

	if cp.Kind != KindGuard {
		t.Errorf("Kind = %q, want KindGuard", cp.Kind)
	}
	if cp.Trigger.Name != "" {
		t.Errorf("Trigger.Name = %q, want empty (Guard has implicit trigger)", cp.Trigger.Name)
	}
	if cp.OutcomeAction != OutcomeActionReorder {
		t.Errorf("OutcomeAction = %q, want OutcomeActionReorder", cp.OutcomeAction)
	}
	if cp.Payload.Guard == nil {
		t.Fatal("Payload.Guard is nil")
	}
}

// TestS02Registrar_RegisterFromDocument_BudgetKindAndFields verifies Budget
// ControlPoint construction.
func TestS02Registrar_RegisterFromDocument_BudgetKindAndFields(t *testing.T) {
	t.Parallel()

	s := NewS02Registrar()
	doc := s02FixtureDocument()

	if err := s.RegisterFromDocument(doc); err != nil {
		t.Fatalf("RegisterFromDocument: %v", err)
	}

	cp, ok := s.Registry().LookupByName("token-budget")
	if !ok {
		t.Fatal("token-budget not found")
	}

	if cp.Kind != KindBudget {
		t.Errorf("Kind = %q, want KindBudget", cp.Kind)
	}
	if cp.Trigger.Name != "dispatch" {
		t.Errorf("Trigger.Name = %q, want \"dispatch\"", cp.Trigger.Name)
	}
	if cp.OutcomeAction != OutcomeActionAdmit {
		t.Errorf("OutcomeAction = %q, want OutcomeActionAdmit", cp.OutcomeAction)
	}
	if cp.Payload.Budget == nil {
		t.Fatal("Payload.Budget is nil")
	}
	if cp.Payload.Budget.Resource != BudgetResourceTokens {
		t.Errorf("Payload.Budget.Resource = %q, want tokens", cp.Payload.Budget.Resource)
	}
	if cp.Payload.Budget.Scope != BudgetScopePerRun {
		t.Errorf("Payload.Budget.Scope = %q, want per_run", cp.Payload.Budget.Scope)
	}
	if cp.Payload.Budget.Limit != 10000 {
		t.Errorf("Payload.Budget.Limit = %d, want 10000", cp.Payload.Budget.Limit)
	}
	if cp.ModeTag != ModeTagMechanism {
		t.Errorf("ModeTag = %q, want ModeTagMechanism (Budgets are always mechanism-tagged)", cp.ModeTag)
	}
}

// TestS02Registrar_RegisterFromDocument_SchemaVersionPropagated verifies that
// the document's SchemaVersion is propagated to all constructed ControlPoints.
func TestS02Registrar_RegisterFromDocument_SchemaVersionPropagated(t *testing.T) {
	t.Parallel()

	s := NewS02Registrar()
	doc := s02FixtureDocument()
	doc.Metadata.SchemaVersion = 3

	if err := s.RegisterFromDocument(doc); err != nil {
		t.Fatalf("RegisterFromDocument: %v", err)
	}

	for _, name := range []string{"deploy-gate", "post-merge-hook", "edge-priority-guard", "token-budget"} {
		cp, ok := s.Registry().LookupByName(name)
		if !ok {
			t.Errorf("LookupByName(%q) not found", name)
			continue
		}
		if cp.SchemaVersion != 3 {
			t.Errorf("%q SchemaVersion = %d, want 3", name, cp.SchemaVersion)
		}
	}
}

// TestS02Registrar_RegisterFromDocument_ApprovalGate verifies that an
// approval-gate entry (with named_approver) is constructed correctly.
func TestS02Registrar_RegisterFromDocument_ApprovalGate(t *testing.T) {
	t.Parallel()

	s := NewS02Registrar()
	doc := s02MarkAllSectionsPresent(PolicyDocument{
		Metadata: PolicyDocumentMeta{Name: "p", Version: "1", Author: "a", SchemaVersion: 1},
		Gates:    []PolicyGate{s02FixtureApprovalGate()},
	})

	if err := s.RegisterFromDocument(doc); err != nil {
		t.Fatalf("RegisterFromDocument: %v", err)
	}

	cp, ok := s.Registry().LookupByName("approval-gate")
	if !ok {
		t.Fatal("approval-gate not found")
	}
	if cp.Payload.Gate == nil {
		t.Fatal("Payload.Gate is nil")
	}
	if cp.Payload.Gate.Subtype != GateSubtypeApproval {
		t.Errorf("Subtype = %q, want GateSubtypeApproval", cp.Payload.Gate.Subtype)
	}
	if cp.Payload.Gate.NamedApprover == nil {
		t.Fatal("NamedApprover is nil, want non-nil")
	}
	if *cp.Payload.Gate.NamedApprover != "ops-lead" {
		t.Errorf("NamedApprover = %q, want %q", *cp.Payload.Gate.NamedApprover, "ops-lead")
	}
}

// TestS02Registrar_RegisterFromDocument_QualityGate verifies that a
// quality-gate entry (with verification_ref) is constructed correctly.
func TestS02Registrar_RegisterFromDocument_QualityGate(t *testing.T) {
	t.Parallel()

	s := NewS02Registrar()
	doc := s02MarkAllSectionsPresent(PolicyDocument{
		Metadata: PolicyDocumentMeta{Name: "p", Version: "1", Author: "a", SchemaVersion: 1},
		Gates:    []PolicyGate{s02FixtureQualityGate()},
	})

	if err := s.RegisterFromDocument(doc); err != nil {
		t.Fatalf("RegisterFromDocument: %v", err)
	}

	cp, ok := s.Registry().LookupByName("quality-gate")
	if !ok {
		t.Fatal("quality-gate not found")
	}
	if cp.Payload.Gate == nil {
		t.Fatal("Payload.Gate is nil")
	}
	if cp.Payload.Gate.Subtype != GateSubtypeQuality {
		t.Errorf("Subtype = %q, want GateSubtypeQuality", cp.Payload.Gate.Subtype)
	}
	if cp.Payload.Gate.VerificationRef == nil {
		t.Fatal("VerificationRef is nil, want non-nil")
	}
	if *cp.Payload.Gate.VerificationRef != "verify-step-1" {
		t.Errorf("VerificationRef = %q, want %q", *cp.Payload.Gate.VerificationRef, "verify-step-1")
	}
}

// TestS02Registrar_RegisterFromDocument_CognitionGate verifies that a
// cognition-tagged Gate is constructed with Mode=cognition and a DelegationPath.
func TestS02Registrar_RegisterFromDocument_CognitionGate(t *testing.T) {
	t.Parallel()

	s := NewS02Registrar()
	doc := s02MarkAllSectionsPresent(PolicyDocument{
		Metadata: PolicyDocumentMeta{Name: "p", Version: "1", Author: "a", SchemaVersion: 1},
		Gates:    []PolicyGate{s02FixtureCognitionGate()},
	})

	if err := s.RegisterFromDocument(doc); err != nil {
		t.Fatalf("RegisterFromDocument: %v", err)
	}

	cp, ok := s.Registry().LookupByName("cognition-gate")
	if !ok {
		t.Fatal("cognition-gate not found")
	}
	if cp.ModeTag != ModeTagCognition {
		t.Errorf("ModeTag = %q, want ModeTagCognition", cp.ModeTag)
	}
	if cp.Evaluator.Mode != ModeTagCognition {
		t.Errorf("Evaluator.Mode = %q, want ModeTagCognition", cp.Evaluator.Mode)
	}
	if cp.Evaluator.DelegationPath == nil {
		t.Fatal("Evaluator.DelegationPath is nil, want non-nil")
	}
	if cp.Evaluator.DelegationPath.Role != "reviewer" {
		t.Errorf("DelegationPath.Role = %q, want %q", cp.Evaluator.DelegationPath.Role, "reviewer")
	}
}

// TestS02Registrar_RegisterFromDocument_HookWithSubscriptionFilter verifies
// that a Hook with a non-empty SubscriptionFilter is constructed correctly.
func TestS02Registrar_RegisterFromDocument_HookWithSubscriptionFilter(t *testing.T) {
	t.Parallel()

	hook := PolicyHook{
		Name:               "filtered-hook",
		TriggerEvent:       "on_agent_started",
		SideEffectKind:     "emit-event",
		SubscriptionFilter: `event.payload["role"] == "builder"`,
		Evaluator: PolicyEvaluatorBlock{
			Mode:       "mechanism",
			Expression: `true`,
		},
	}

	s := NewS02Registrar()
	doc := s02MarkAllSectionsPresent(PolicyDocument{
		Metadata: PolicyDocumentMeta{Name: "p", Version: "1", Author: "a", SchemaVersion: 1},
		Hooks:    []PolicyHook{hook},
	})

	if err := s.RegisterFromDocument(doc); err != nil {
		t.Fatalf("RegisterFromDocument: %v", err)
	}

	cp, ok := s.Registry().LookupByName("filtered-hook")
	if !ok {
		t.Fatal("filtered-hook not found")
	}
	if cp.Payload.Hook == nil {
		t.Fatal("Payload.Hook is nil")
	}
	if cp.Payload.Hook.SubscriptionFilter == nil {
		t.Fatal("SubscriptionFilter is nil, want non-nil")
	}
	wantSF := PolicyExpression(`event.payload["role"] == "builder"`)
	if *cp.Payload.Hook.SubscriptionFilter != wantSF {
		t.Errorf("SubscriptionFilter = %q, want %q", *cp.Payload.Hook.SubscriptionFilter, wantSF)
	}
}

// TestS02Registrar_RegisterFromDocument_GuardWithAppliesToNode verifies that
// a Guard with AppliesToNode is constructed with a non-nil AppliesToNode.
func TestS02Registrar_RegisterFromDocument_GuardWithAppliesToNode(t *testing.T) {
	t.Parallel()

	guard := PolicyGuard{
		Name:          "node-scoped-guard",
		AppliesToNode: "deploy-node",
		Evaluator: PolicyEvaluatorBlock{
			Mode:       "mechanism",
			Expression: `true`,
		},
	}

	s := NewS02Registrar()
	doc := s02MarkAllSectionsPresent(PolicyDocument{
		Metadata: PolicyDocumentMeta{Name: "p", Version: "1", Author: "a", SchemaVersion: 1},
		Guards:   []PolicyGuard{guard},
	})

	if err := s.RegisterFromDocument(doc); err != nil {
		t.Fatalf("RegisterFromDocument: %v", err)
	}

	cp, ok := s.Registry().LookupByName("node-scoped-guard")
	if !ok {
		t.Fatal("node-scoped-guard not found")
	}
	if cp.Payload.Guard == nil {
		t.Fatal("Payload.Guard is nil")
	}
	if cp.Payload.Guard.AppliesToNode == nil {
		t.Fatal("AppliesToNode is nil, want non-nil")
	}
	if *cp.Payload.Guard.AppliesToNode != NodeID("deploy-node") {
		t.Errorf("AppliesToNode = %q, want %q", *cp.Payload.Guard.AppliesToNode, "deploy-node")
	}
}

// ---------------------------------------------------------------------------
// CP-043: registry is the single source of truth (S02 ownership)
// ---------------------------------------------------------------------------

// TestS02Registrar_RegistryIsSingleSourceOfTruth verifies that S02Registrar
// is the single source of truth: all ControlPoints observable by consumers
// (S01, S05) are accessible via Registry() and only via Registry(), per CP-043.
func TestS02Registrar_RegistryIsSingleSourceOfTruth(t *testing.T) {
	t.Parallel()

	s := NewS02Registrar()
	doc := s02FixtureDocument()

	if err := s.RegisterFromDocument(doc); err != nil {
		t.Fatalf("RegisterFromDocument: %v", err)
	}

	// Consumers (S01 and S05) obtain the Registry via Registry().
	s01Registry := s.Registry()
	s05Registry := s.Registry()

	// Both consumers see the same ControlPoints — no private stores.
	for _, name := range []string{"deploy-gate", "post-merge-hook", "edge-priority-guard", "token-budget"} {
		_, ok1 := s01Registry.LookupByName(name)
		_, ok2 := s05Registry.LookupByName(name)
		if !ok1 || !ok2 {
			t.Errorf("CP %q: s01 found=%v, s05 found=%v (must both be true)", name, ok1, ok2)
		}
	}

	// The All() output is identical across consumers.
	all01 := s01Registry.All()
	all05 := s05Registry.All()
	if len(all01) != len(all05) {
		t.Fatalf("s01 All() len = %d, s05 All() len = %d (must be equal)", len(all01), len(all05))
	}
	for i := range all01 {
		if all01[i].Name != all05[i].Name {
			t.Errorf("all01[%d].Name = %q, all05[%d].Name = %q (non-deterministic)", i, all01[i].Name, i, all05[i].Name)
		}
	}
}

// ---------------------------------------------------------------------------
// CP-044: re-registration-safe (identical body = silent success; divergent = error)
// ---------------------------------------------------------------------------

// TestS02Registrar_RegisterFromDocument_IdempotentByName verifies that
// registering the same PolicyDocument twice succeeds silently (CP-044 via
// MapRegistry: identical body = idempotent).
func TestS02Registrar_RegisterFromDocument_IdempotentByName(t *testing.T) {
	t.Parallel()

	s := NewS02Registrar()
	doc := s02FixtureDocument()

	// First registration.
	if err := s.RegisterFromDocument(doc); err != nil {
		t.Fatalf("first RegisterFromDocument: %v", err)
	}

	// Second registration with identical document — must succeed silently.
	if err := s.RegisterFromDocument(doc); err != nil {
		t.Errorf("second RegisterFromDocument (identical doc): got error %v, want nil", err)
	}

	// Registry still has exactly 4 entries.
	all := s.Registry().All()
	if len(all) != 4 {
		t.Errorf("All() len = %d after idempotent re-registration, want 4", len(all))
	}
}

// TestS02Registrar_RegisterFromDocument_DivergentBodyFails verifies that
// registering a different body under an existing name returns ErrDivergentBody.
func TestS02Registrar_RegisterFromDocument_DivergentBodyFails(t *testing.T) {
	t.Parallel()

	s := NewS02Registrar()
	doc := s02FixtureDocument()

	// First registration.
	if err := s.RegisterFromDocument(doc); err != nil {
		t.Fatalf("first RegisterFromDocument: %v", err)
	}

	// Second document: same name "deploy-gate" but different attach_point
	// → divergent body per CP-044.
	divergentDoc := s02MarkAllSectionsPresent(PolicyDocument{
		Metadata: doc.Metadata,
		Gates: []PolicyGate{
			{
				Name:        "deploy-gate", // same name
				Subtype:     "goal-gate",
				AttachPoint: "node-post-exit", // different attach_point → different body
				Evaluator: PolicyEvaluatorBlock{
					Mode:       "mechanism",
					Expression: `run.context["phase"] == "deploy"`,
				},
			},
		},
	})

	err := s.RegisterFromDocument(divergentDoc)
	if err == nil {
		t.Fatal("RegisterFromDocument with divergent body: expected error, got nil")
	}
	if !errors.Is(err, ErrDivergentBody) {
		t.Errorf("RegisterFromDocument with divergent body: got %v, want error wrapping ErrDivergentBody", err)
	}
}

// ---------------------------------------------------------------------------
// Construction error paths
// ---------------------------------------------------------------------------

// TestS02Registrar_RegisterFromDocument_UnknownEvaluatorModeGate verifies
// that a Gate with an unknown evaluator mode returns ErrConstructControlPoint.
func TestS02Registrar_RegisterFromDocument_UnknownEvaluatorModeGate(t *testing.T) {
	t.Parallel()

	s := NewS02Registrar()
	doc := s02MarkAllSectionsPresent(PolicyDocument{
		Metadata: PolicyDocumentMeta{Name: "p", Version: "1", Author: "a", SchemaVersion: 1},
		Gates: []PolicyGate{
			{
				Name:        "bad-gate",
				Subtype:     "goal-gate",
				AttachPoint: "node-pre-entry",
				Evaluator:   PolicyEvaluatorBlock{Mode: "invalid-mode"},
			},
		},
	})

	err := s.RegisterFromDocument(doc)
	if err == nil {
		t.Fatal("expected error for unknown evaluator mode, got nil")
	}
	if !errors.Is(err, ErrConstructControlPoint) {
		t.Errorf("got %v, want error wrapping ErrConstructControlPoint", err)
	}
}

// TestS02Registrar_RegisterFromDocument_InvalidAttachPoint verifies that a
// Gate with an unknown attach_point returns ErrConstructControlPoint.
func TestS02Registrar_RegisterFromDocument_InvalidAttachPoint(t *testing.T) {
	t.Parallel()

	s := NewS02Registrar()
	doc := s02MarkAllSectionsPresent(PolicyDocument{
		Metadata: PolicyDocumentMeta{Name: "p", Version: "1", Author: "a", SchemaVersion: 1},
		Gates: []PolicyGate{
			{
				Name:        "bad-gate",
				Subtype:     "goal-gate",
				AttachPoint: "invalid-attach-point",
				Evaluator: PolicyEvaluatorBlock{
					Mode:       "mechanism",
					Expression: `true`,
				},
			},
		},
	})

	err := s.RegisterFromDocument(doc)
	if err == nil {
		t.Fatal("expected error for invalid attach_point, got nil")
	}
	if !errors.Is(err, ErrConstructControlPoint) {
		t.Errorf("got %v, want error wrapping ErrConstructControlPoint", err)
	}
}

// TestS02Registrar_RegisterFromDocument_InvalidBudgetResource verifies that a
// Budget with an unknown resource returns ErrConstructControlPoint.
func TestS02Registrar_RegisterFromDocument_InvalidBudgetResource(t *testing.T) {
	t.Parallel()

	s := NewS02Registrar()
	doc := s02MarkAllSectionsPresent(PolicyDocument{
		Metadata: PolicyDocumentMeta{Name: "p", Version: "1", Author: "a", SchemaVersion: 1},
		Budgets: []PolicyBudget{
			{
				Name:     "bad-budget",
				Resource: "unknown-resource",
				Scope:    "per_run",
				Limit:    1000,
			},
		},
	})

	err := s.RegisterFromDocument(doc)
	if err == nil {
		t.Fatal("expected error for invalid budget resource, got nil")
	}
	if !errors.Is(err, ErrConstructControlPoint) {
		t.Errorf("got %v, want error wrapping ErrConstructControlPoint", err)
	}
}

// TestS02Registrar_RegisterFromDocument_BudgetWarningThresholdDefault verifies
// that a Budget with WarningThreshold = 0 gets the CP-022 default (0.8).
func TestS02Registrar_RegisterFromDocument_BudgetWarningThresholdDefault(t *testing.T) {
	t.Parallel()

	s := NewS02Registrar()
	doc := s02MarkAllSectionsPresent(PolicyDocument{
		Metadata: PolicyDocumentMeta{Name: "p", Version: "1", Author: "a", SchemaVersion: 1},
		Budgets: []PolicyBudget{
			{
				Name:             "default-threshold-budget",
				Resource:         "tokens",
				Scope:            "per_run",
				Limit:            5000,
				WarningThreshold: 0, // zero → apply CP-022 default 0.8
				ScopeTarget:      "*",
			},
		},
	})

	if err := s.RegisterFromDocument(doc); err != nil {
		t.Fatalf("RegisterFromDocument: %v", err)
	}

	cp, ok := s.Registry().LookupByName("default-threshold-budget")
	if !ok {
		t.Fatal("default-threshold-budget not found")
	}
	if cp.Payload.Budget == nil {
		t.Fatal("Payload.Budget is nil")
	}
	if cp.Payload.Budget.WarningThreshold != 0.8 {
		t.Errorf("WarningThreshold = %v, want 0.8 (CP-022 default)", cp.Payload.Budget.WarningThreshold)
	}
}

// ---------------------------------------------------------------------------
// parseScopeTarget
// ---------------------------------------------------------------------------

// TestParseScopeTarget_Wildcard verifies that "*" and "" both parse as
// ScopeTargetKindWildcard.
func TestParseScopeTarget_Wildcard(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"*", ""} {
		raw := raw
		t.Run(raw, func(t *testing.T) {
			t.Parallel()
			st, err := parseScopeTarget(raw)
			if err != nil {
				t.Fatalf("parseScopeTarget(%q): %v", raw, err)
			}
			if st.Kind != ScopeTargetKindWildcard {
				t.Errorf("Kind = %q, want ScopeTargetKindWildcard", st.Kind)
			}
		})
	}
}

// TestParseScopeTarget_Predicate verifies that "node_type:<x>" parses as
// ScopeTargetKindPredicate with the correct PredicateType.
func TestParseScopeTarget_Predicate(t *testing.T) {
	t.Parallel()

	st, err := parseScopeTarget("node_type:gate")
	if err != nil {
		t.Fatalf("parseScopeTarget: %v", err)
	}
	if st.Kind != ScopeTargetKindPredicate {
		t.Errorf("Kind = %q, want ScopeTargetKindPredicate", st.Kind)
	}
	if st.PredicateType != "gate" {
		t.Errorf("PredicateType = %q, want %q", st.PredicateType, "gate")
	}
}

// TestParseScopeTarget_Singleton verifies that a bare non-special string
// parses as ScopeTargetKindSingleton.
func TestParseScopeTarget_Singleton(t *testing.T) {
	t.Parallel()

	st, err := parseScopeTarget("builder-role")
	if err != nil {
		t.Fatalf("parseScopeTarget: %v", err)
	}
	if st.Kind != ScopeTargetKindSingleton {
		t.Errorf("Kind = %q, want ScopeTargetKindSingleton", st.Kind)
	}
	if len(st.IDs) != 1 || st.IDs[0] != "builder-role" {
		t.Errorf("IDs = %v, want [\"builder-role\"]", st.IDs)
	}
}

// TestS02Registrar_ConstructedControlPointsAreValid verifies that every
// ControlPoint constructed by RegisterFromDocument passes cp.Valid().
func TestS02Registrar_ConstructedControlPointsAreValid(t *testing.T) {
	t.Parallel()

	s := NewS02Registrar()
	doc := s02FixtureDocument()

	if err := s.RegisterFromDocument(doc); err != nil {
		t.Fatalf("RegisterFromDocument: %v", err)
	}

	for _, cp := range s.Registry().All() {
		if !cp.Valid() {
			t.Errorf("ControlPoint{Name=%q, Kind=%q}.Valid() = false (CP-001)", cp.Name, cp.Kind)
		}
	}
}

// ---------------------------------------------------------------------------
// CP-035: Required-section validation enforced at registration time
// ---------------------------------------------------------------------------

// TestS02Registrar_RegisterFromDocument_CP035_MissingSection verifies that
// RegisterFromDocument rejects a PolicyDocument whose YAML source is missing
// any one of the seven required top-level sections (CP-035). The error must
// wrap ErrMissingPolicySection and the registry must remain empty.
func TestS02Registrar_RegisterFromDocument_CP035_MissingSection(t *testing.T) {
	t.Parallel()

	for _, section := range requiredSections {
		section := section
		t.Run("missing_"+section, func(t *testing.T) {
			t.Parallel()

			// Parse a document with the target section absent (sectionPresence not set).
			data := policyDocFixtureMissingSectionYAML(t, section)
			doc, err := ParsePolicyDocument(data)
			if err != nil {
				t.Fatalf("ParsePolicyDocument: %v", err)
			}

			s := NewS02Registrar()
			err = s.RegisterFromDocument(doc)
			if err == nil {
				t.Errorf("RegisterFromDocument: expected ErrMissingPolicySection for missing %q section, got nil", section)
				return
			}
			if !errors.Is(err, ErrMissingPolicySection) {
				t.Errorf("RegisterFromDocument: got %v, want error wrapping ErrMissingPolicySection", err)
			}

			// Registry must remain empty — rejected document must not partially register.
			if all := s.Registry().All(); len(all) != 0 {
				t.Errorf("registry has %d entries after CP-035 rejection, want 0", len(all))
			}
		})
	}
}

// TestS02Registrar_RegisterFromDocument_CP035_AllSectionsPresent verifies
// that a document parsed from YAML with all seven required sections present
// passes RegisterFromDocument without error (CP-035 positive path).
func TestS02Registrar_RegisterFromDocument_CP035_AllSectionsPresent(t *testing.T) {
	t.Parallel()

	data := policyDocFixtureValidYAML(t)
	doc, err := ParsePolicyDocument(data)
	if err != nil {
		t.Fatalf("ParsePolicyDocument: %v", err)
	}

	s := NewS02Registrar()
	if err := s.RegisterFromDocument(doc); err != nil {
		t.Errorf("RegisterFromDocument: unexpected error for fully-valid document: %v", err)
	}
}

package core

import (
	"errors"
	"testing"
)

var cp039CognitionAxes = AxisTags{
	LLMFreedom:    LLMFreedomBounded,
	IODeterminism: IODeterminismBestEffort,
	ReplaySafety:  ReplaySafetySafe,
	Idempotency:   AxisIdempotencyIdempotent,
}

func cp039FullDelegationPath() DelegationPath {
	return DelegationPath{
		Role:              "reviewer",
		ModelClass:        "reviewer-tier-1",
		InputSchemaRef:    "gate-input-v1",
		ResponseSchemaRef: "gate-response-v1",
		PromptTemplateRef: "gate-prompt-v1",
	}
}

func cp039CognitionGate(t *testing.T, name string, dp *DelegationPath) ControlPoint {
	t.Helper()
	approver := "ops-lead"
	return ControlPoint{
		Name:    name,
		Kind:    KindGate,
		Trigger: Trigger{Name: "node-pre-entry"},
		Evaluator: Evaluator{
			Mode:           ModeTagCognition,
			DelegationPath: dp,
		},
		OutcomeAction: OutcomeActionAllow,
		Payload: KindPayload{Gate: &GatePayload{
			Subtype:       GateSubtypeApproval,
			AttachPoint:   AttachPointNodePreEntry,
			NamedApprover: &approver,
		}},
		Axes:          cp039CognitionAxes,
		ModeTag:       ModeTagCognition,
		SchemaVersion: 1,
	}
}

func cp039CognitionHook(t *testing.T, name string, dp *DelegationPath) ControlPoint {
	t.Helper()
	return ControlPoint{
		Name:    name,
		Kind:    KindHook,
		Trigger: Trigger{Name: "on_review_required"},
		Evaluator: Evaluator{
			Mode:           ModeTagCognition,
			DelegationPath: dp,
		},
		OutcomeAction: OutcomeActionSideEffect,
		Payload: KindPayload{Hook: &HookPayload{
			TriggerEvent:      "on_review_required",
			SideEffectKind:    SideEffectKindEmitEvent,
			HaltOnFailure:     false,
			SubsystemPriority: 10,
		}},
		Axes:          cp039CognitionAxes,
		ModeTag:       ModeTagCognition,
		SchemaVersion: 1,
	}
}

// TestCP039_CognitionGate_FullPath_Registers verifies that a cognition-tagged
// Gate with a fully-populated DelegationPath registers without error.
//
// specs/control-points.md §4.8.CP-039 positive path: the delegation path is
// named; registration succeeds; the path is subsequently readable from the
// registry for reviewer verification.
func TestCP039_CognitionGate_FullPath_Registers(t *testing.T) {
	t.Parallel()

	dp := cp039FullDelegationPath()
	cp := cp039CognitionGate(t, "review-quality-gate", &dp)

	if !cp.Valid() {
		t.Fatal("fixture cognition Gate with full DelegationPath.Valid() = false, want true")
	}

	reg := NewMapRegistry()
	if err := reg.Register(cp); err != nil {
		t.Fatalf("Register cognition Gate with full DelegationPath: unexpected error: %v", err)
	}

	got, ok := reg.LookupByName("review-quality-gate")
	if !ok {
		t.Fatal("LookupByName after registration: not found")
	}
	if got.Kind != KindGate {
		t.Errorf("Kind = %q, want Gate", got.Kind)
	}
	if got.Evaluator.Mode != ModeTagCognition {
		t.Errorf("Evaluator.Mode = %q, want cognition", got.Evaluator.Mode)
	}
}

// TestCP039_CognitionHook_FullPath_Registers verifies that a cognition-tagged
// Hook with a fully-populated DelegationPath registers without error.
//
// CP-017 (cognition-tagged Hook evaluator) is the Hook-side analogue of CP-011;
// CP-039 applies equally to both.
func TestCP039_CognitionHook_FullPath_Registers(t *testing.T) {
	t.Parallel()

	dp := cp039FullDelegationPath()
	cp := cp039CognitionHook(t, "review-hook", &dp)

	if !cp.Valid() {
		t.Fatal("fixture cognition Hook with full DelegationPath.Valid() = false, want true")
	}

	reg := NewMapRegistry()
	if err := reg.Register(cp); err != nil {
		t.Fatalf("Register cognition Hook with full DelegationPath: unexpected error: %v", err)
	}

	got, ok := reg.LookupByName("review-hook")
	if !ok {
		t.Fatal("LookupByName after registration: not found")
	}
	if got.Kind != KindHook {
		t.Errorf("Kind = %q, want Hook", got.Kind)
	}
	if got.Evaluator.Mode != ModeTagCognition {
		t.Errorf("Evaluator.Mode = %q, want cognition", got.Evaluator.Mode)
	}
}

// TestCP039_CognitionGate_NilPath_FailsRegistration verifies that a
// cognition-tagged Gate with a nil DelegationPath fails registration with
// ErrInvalidControlPoint.
//
// specs/control-points.md §4.8.CP-039: "unnamed paths fail registration."
func TestCP039_CognitionGate_NilPath_FailsRegistration(t *testing.T) {
	t.Parallel()

	cp := cp039CognitionGate(t, "unnamed-cognition-gate", nil)

	reg := NewMapRegistry()
	err := reg.Register(cp)
	if err == nil {
		t.Fatal("Register cognition Gate with nil DelegationPath: expected error, got nil")
	}
	if !errors.Is(err, ErrInvalidControlPoint) {
		t.Errorf("Register cognition Gate with nil DelegationPath: got %v, want ErrInvalidControlPoint", err)
	}

	if len(reg.All()) != 0 {
		t.Errorf("registry has %d entries after rejection, want 0", len(reg.All()))
	}
}

// TestCP039_CognitionHook_NilPath_FailsRegistration verifies that a
// cognition-tagged Hook with a nil DelegationPath fails registration.
//
// specs/control-points.md §4.8.CP-039 applies equally to Hook cognition
// evaluators (see also §4.3.CP-017).
func TestCP039_CognitionHook_NilPath_FailsRegistration(t *testing.T) {
	t.Parallel()

	cp := cp039CognitionHook(t, "unnamed-cognition-hook", nil)

	reg := NewMapRegistry()
	err := reg.Register(cp)
	if err == nil {
		t.Fatal("Register cognition Hook with nil DelegationPath: expected error, got nil")
	}
	if !errors.Is(err, ErrInvalidControlPoint) {
		t.Errorf("Register cognition Hook with nil DelegationPath: got %v, want ErrInvalidControlPoint", err)
	}

	if len(reg.All()) != 0 {
		t.Errorf("registry has %d entries after rejection, want 0", len(reg.All()))
	}
}

// TestCP039_CognitionGate_PartialPath_FailsRegistration verifies that a
// cognition-tagged Gate where any single DelegationPath field is empty fails
// registration.
//
// §6.1.5 declares all five fields required; §4.8.CP-039 enforces this at
// registration time. Each sub-test removes exactly one field from an otherwise
// complete DelegationPath.
func TestCP039_CognitionGate_PartialPath_FailsRegistration(t *testing.T) {
	t.Parallel()

	full := cp039FullDelegationPath()

	cases := []struct {
		name string
		dp   DelegationPath
	}{
		{
			name: "missing_role",
			dp: DelegationPath{
				ModelClass:        full.ModelClass,
				InputSchemaRef:    full.InputSchemaRef,
				ResponseSchemaRef: full.ResponseSchemaRef,
				PromptTemplateRef: full.PromptTemplateRef,
			},
		},
		{
			name: "missing_model_class",
			dp: DelegationPath{
				Role:              full.Role,
				InputSchemaRef:    full.InputSchemaRef,
				ResponseSchemaRef: full.ResponseSchemaRef,
				PromptTemplateRef: full.PromptTemplateRef,
			},
		},
		{
			name: "missing_input_schema_ref",
			dp: DelegationPath{
				Role:              full.Role,
				ModelClass:        full.ModelClass,
				ResponseSchemaRef: full.ResponseSchemaRef,
				PromptTemplateRef: full.PromptTemplateRef,
			},
		},
		{
			name: "missing_response_schema_ref",
			dp: DelegationPath{
				Role:              full.Role,
				ModelClass:        full.ModelClass,
				InputSchemaRef:    full.InputSchemaRef,
				PromptTemplateRef: full.PromptTemplateRef,
			},
		},
		{
			name: "missing_prompt_template_ref",
			dp: DelegationPath{
				Role:              full.Role,
				ModelClass:        full.ModelClass,
				InputSchemaRef:    full.InputSchemaRef,
				ResponseSchemaRef: full.ResponseSchemaRef,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dp := tc.dp // copy to avoid sharing
			cp := cp039CognitionGate(t, "partial-gate-"+tc.name, &dp)

			reg := NewMapRegistry()
			err := reg.Register(cp)
			if err == nil {
				t.Fatalf("Register cognition Gate with %s: expected error, got nil", tc.name)
			}
			if !errors.Is(err, ErrInvalidControlPoint) {
				t.Errorf("Register cognition Gate with %s: got %v, want ErrInvalidControlPoint",
					tc.name, err)
			}
		})
	}
}

// TestCP039_CognitionHook_PartialPath_FailsRegistration verifies that a
// cognition-tagged Hook where any single DelegationPath field is empty fails
// registration.
//
// Mirrors TestCP039_CognitionGate_PartialPath_FailsRegistration for Hooks per
// §4.3.CP-017 + §4.8.CP-039.
func TestCP039_CognitionHook_PartialPath_FailsRegistration(t *testing.T) {
	t.Parallel()

	full := cp039FullDelegationPath()

	cases := []struct {
		name string
		dp   DelegationPath
	}{
		{
			name: "missing_role",
			dp: DelegationPath{
				ModelClass:        full.ModelClass,
				InputSchemaRef:    full.InputSchemaRef,
				ResponseSchemaRef: full.ResponseSchemaRef,
				PromptTemplateRef: full.PromptTemplateRef,
			},
		},
		{
			name: "missing_model_class",
			dp: DelegationPath{
				Role:              full.Role,
				InputSchemaRef:    full.InputSchemaRef,
				ResponseSchemaRef: full.ResponseSchemaRef,
				PromptTemplateRef: full.PromptTemplateRef,
			},
		},
		{
			name: "missing_input_schema_ref",
			dp: DelegationPath{
				Role:              full.Role,
				ModelClass:        full.ModelClass,
				ResponseSchemaRef: full.ResponseSchemaRef,
				PromptTemplateRef: full.PromptTemplateRef,
			},
		},
		{
			name: "missing_response_schema_ref",
			dp: DelegationPath{
				Role:              full.Role,
				ModelClass:        full.ModelClass,
				InputSchemaRef:    full.InputSchemaRef,
				PromptTemplateRef: full.PromptTemplateRef,
			},
		},
		{
			name: "missing_prompt_template_ref",
			dp: DelegationPath{
				Role:              full.Role,
				ModelClass:        full.ModelClass,
				InputSchemaRef:    full.InputSchemaRef,
				ResponseSchemaRef: full.ResponseSchemaRef,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dp := tc.dp
			cp := cp039CognitionHook(t, "partial-hook-"+tc.name, &dp)

			reg := NewMapRegistry()
			err := reg.Register(cp)
			if err == nil {
				t.Fatalf("Register cognition Hook with %s: expected error, got nil", tc.name)
			}
			if !errors.Is(err, ErrInvalidControlPoint) {
				t.Errorf("Register cognition Hook with %s: got %v, want ErrInvalidControlPoint",
					tc.name, err)
			}
		})
	}
}

// TestCP039_DelegationPath_FieldsReadable_AfterRegistration verifies that
// after a cognition-tagged ControlPoint is registered, all five DelegationPath
// fields are readable from the registry entry.
//
// specs/control-points.md §4.8.CP-039: "A reviewer verifies the path at
// registration." The registry preserves the full DelegationPath so that a
// post-registration audit (§7.1) can inspect every field: role, model_class,
// input_schema_ref, response_schema_ref, and prompt_template_ref.
func TestCP039_DelegationPath_FieldsReadable_AfterRegistration(t *testing.T) {
	t.Parallel()

	dp := cp039FullDelegationPath()

	tests := []struct {
		name string
		cp   ControlPoint
	}{
		{
			name: "Gate",
			cp:   cp039CognitionGate(t, "readable-path-gate", &dp),
		},
		{
			name: "Hook",
			cp:   cp039CognitionHook(t, "readable-path-hook", &dp),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			reg := NewMapRegistry()
			if err := reg.Register(tc.cp); err != nil {
				t.Fatalf("Register: %v", err)
			}

			got, ok := reg.LookupByName(tc.cp.Name)
			if !ok {
				t.Fatal("LookupByName after registration: not found")
			}

			if got.Evaluator.DelegationPath == nil {
				t.Fatal("retrieved Evaluator.DelegationPath is nil, want non-nil")
			}

			gotDP := *got.Evaluator.DelegationPath

			if gotDP.Role != dp.Role {
				t.Errorf("DelegationPath.Role = %q, want %q", gotDP.Role, dp.Role)
			}
			if gotDP.ModelClass != dp.ModelClass {
				t.Errorf("DelegationPath.ModelClass = %q, want %q", gotDP.ModelClass, dp.ModelClass)
			}
			if gotDP.InputSchemaRef != dp.InputSchemaRef {
				t.Errorf("DelegationPath.InputSchemaRef = %q, want %q", gotDP.InputSchemaRef, dp.InputSchemaRef)
			}
			if gotDP.ResponseSchemaRef != dp.ResponseSchemaRef {
				t.Errorf("DelegationPath.ResponseSchemaRef = %q, want %q", gotDP.ResponseSchemaRef, dp.ResponseSchemaRef)
			}
			if gotDP.PromptTemplateRef != dp.PromptTemplateRef {
				t.Errorf("DelegationPath.PromptTemplateRef = %q, want %q", gotDP.PromptTemplateRef, dp.PromptTemplateRef)
			}
		})
	}
}

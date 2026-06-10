package core

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// Fixtures — hk-a8bg.34 helper prefix: exprEnvFixture
// ---------------------------------------------------------------------------

// exprEnvFixtureRun returns a valid Run for PolicyExprEnv tests.
func exprEnvFixtureRun(t *testing.T) Run {
	t.Helper()
	return Run{
		RunID:           RunID(uuid.New()),
		WorkflowID:      WorkflowID(uuid.New()),
		WorkflowVersion: WorkflowVersion("v1.0.0"),
		Input:           WorkspaceRef("workspace/test-input"),
		WorkflowMode:    WorkflowModeSingle,
		State:           StateID(uuid.New()),
		Context: map[string]any{
			"phase":    "review",
			"attempts": 2,
		},
		StartTime: time.Now(),
	}
}

// exprEnvFixtureOutcome returns a valid SUCCESS Outcome for PolicyExprEnv tests.
func exprEnvFixtureOutcome(t *testing.T) Outcome {
	t.Helper()
	return Outcome{
		Status: OutcomeStatusSuccess,
		Kind:   OutcomeKindDefault,
	}
}

// exprEnvFixtureEvent returns a valid Event for PolicyExprEnv tests.
func exprEnvFixtureEvent(t *testing.T) Event {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{
		"run_id":    "some-run-id",
		"bead_id":   "hk-test",
		"exit_code": 0,
	})
	return Event{
		EventID:         EventID(uuid.New()),
		SchemaVersion:   1,
		Type:            "agent_completed",
		TimestampWall:   time.Now(),
		SourceSubsystem: "test",
		Payload:         json.RawMessage(payload),
	}
}

// exprEnvFixtureEdges returns a slice of valid Edges for PolicyExprEnv tests.
func exprEnvFixtureEdges(t *testing.T) []Edge {
	t.Helper()
	label := "success"
	cap := 3
	return []Edge{
		{
			FromNode:       NodeID("node-a"),
			ToNode:         NodeID("node-b"),
			Weight:         10,
			OrderingKey:    "a",
			PreferredLabel: &label,
			TraversalCap:   &cap,
		},
		{
			FromNode:    NodeID("node-a"),
			ToNode:      NodeID("node-c"),
			Weight:      5,
			OrderingKey: "b",
		},
	}
}

// exprEnvFixturePolicyMeta returns a sample policy metadata map.
func exprEnvFixturePolicyMeta(t *testing.T) map[string]string {
	t.Helper()
	return map[string]string{
		"required_version": "v1.0.0",
		"owner":            "platform-team",
	}
}

// ---------------------------------------------------------------------------
// CP-034: PolicyExprEnv struct shape — top-level binding names
// ---------------------------------------------------------------------------

// TestPolicyExprEnv_GateEnv_BindingNames verifies that NewPolicyExprGateEnv
// produces an environment where expr.Compile recognises the §6.4 bindings
// (run, outcome, context, policy_meta) at type-check time (CP-034).
// event and edges are absent (Gate context); expressions using them should
// fail type-checking.
func TestPolicyExprEnv_GateEnv_BindingNames(t *testing.T) {
	t.Parallel()

	run := exprEnvFixtureRun(t)
	outcome := exprEnvFixtureOutcome(t)
	policyMeta := exprEnvFixturePolicyMeta(t)

	env := NewPolicyExprGateEnv(run, outcome, false, policyMeta, nil)

	cases := []struct {
		name    string
		expr    string
		wantErr bool
	}{
		// run binding exists and key fields are accessible
		{
			name: "run_binding_id",
			expr: `run.id != ""`,
		},
		{
			name: "run_binding_workflow_version",
			expr: `run.workflow_version == "v1.0.0"`,
		},
		{
			name: "run_binding_paused",
			expr: `run.paused == false`,
		},
		{
			name: "run_context_alias",
			expr: `run.context["phase"] == "review"`,
		},
		// outcome binding exists and key fields are accessible
		{
			name: "outcome_status",
			expr: `outcome.status == "SUCCESS"`,
		},
		// context binding exists (top-level alias)
		{
			name: "context_top_level",
			expr: `context["phase"] == "review"`,
		},
		// policy_meta binding exists
		{
			name: "policy_meta_field",
			expr: `policy_meta["required_version"] == "v1.0.0"`,
		},
	}

	evaluator := NewPolicyExprEvaluator(DefaultPolicyExprEvaluatorConfig())

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			prog, _, err := evaluator.Compile(tc.expr, env)
			if tc.wantErr {
				if err == nil {
					t.Errorf("Compile(%q) = nil error, want type-check error", tc.expr)
				}
				return
			}
			if err != nil {
				t.Errorf("Compile(%q) = %v, want nil (type-check should pass)", tc.expr, err)
				return
			}
			// Evaluate and ensure it doesn't panic.
			if _, evalErr := evaluator.Evaluate(context.Background(), prog, env); evalErr != nil {
				t.Errorf("Evaluate(%q) = %v, want nil", tc.expr, evalErr)
			}
		})
	}
}

// TestPolicyExprEnv_HookEnv_EventBinding verifies that NewPolicyExprHookEnv
// produces an environment where the event binding is non-nil and its key
// fields (event.type, event.payload) are accessible per §6.4.1.
func TestPolicyExprEnv_HookEnv_EventBinding(t *testing.T) {
	t.Parallel()

	run := exprEnvFixtureRun(t)
	outcome := exprEnvFixtureOutcome(t)
	event := exprEnvFixtureEvent(t)
	policyMeta := exprEnvFixturePolicyMeta(t)

	env := NewPolicyExprHookEnv(run, outcome, event, false, policyMeta)

	if env.Event == nil {
		t.Fatal("NewPolicyExprHookEnv: Event is nil, want non-nil (Hook context requires event binding)")
	}
	if env.Edges != nil {
		t.Errorf("NewPolicyExprHookEnv: Edges = %v, want nil (edges not available in Hook context)", env.Edges)
	}

	evaluator := NewPolicyExprEvaluator(DefaultPolicyExprEvaluatorConfig())

	cases := []struct {
		name string
		expr string
	}{
		{
			name: "event_type",
			expr: `event.type == "agent_completed"`,
		},
		{
			name: "event_payload_run_id",
			expr: `event.payload["run_id"] == "some-run-id"`,
		},
		{
			name: "event_payload_exit_code",
			expr: `event.payload["exit_code"] == 0`,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			prog, _, err := evaluator.Compile(tc.expr, env)
			if err != nil {
				t.Errorf("Compile(%q) = %v, want nil", tc.expr, err)
				return
			}
			if _, evalErr := evaluator.Evaluate(context.Background(), prog, env); evalErr != nil {
				t.Errorf("Evaluate(%q) = %v, want nil", tc.expr, evalErr)
			}
		})
	}
}

// TestPolicyExprEnv_GuardEnv_EdgesBinding verifies that NewPolicyExprGuardEnv
// produces an environment where the edges binding is non-nil and edge fields
// (edge.weight, edge.preferred_label, edge.target.id) are accessible per §6.4.1.
func TestPolicyExprEnv_GuardEnv_EdgesBinding(t *testing.T) {
	t.Parallel()

	run := exprEnvFixtureRun(t)
	outcome := exprEnvFixtureOutcome(t)
	edges := exprEnvFixtureEdges(t)
	policyMeta := exprEnvFixturePolicyMeta(t)

	env := NewPolicyExprGuardEnv(run, outcome, edges, false, policyMeta)

	if env.Event != nil {
		t.Errorf("NewPolicyExprGuardEnv: Event = %v, want nil (event not available in Guard context)", env.Event)
	}
	if env.Edges == nil {
		t.Fatal("NewPolicyExprGuardEnv: Edges is nil, want non-nil (Guard context requires edges binding)")
	}
	if len(env.Edges) != len(edges) {
		t.Errorf("NewPolicyExprGuardEnv: len(Edges) = %d, want %d", len(env.Edges), len(edges))
	}

	evaluator := NewPolicyExprEvaluator(DefaultPolicyExprEvaluatorConfig())

	cases := []struct {
		name string
		expr string
	}{
		{
			name: "edge_count",
			expr: `len(edges) == 2`,
		},
		{
			name: "first_edge_weight",
			expr: `edges[0].weight != nil`,
		},
		{
			name: "first_edge_preferred_label",
			expr: `edges[0].preferred_label != nil`,
		},
		{
			name: "first_edge_target_id",
			expr: `edges[0].target.id == "node-b"`,
		},
		{
			name: "second_edge_target_id",
			expr: `edges[1].target.id == "node-c"`,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			prog, _, err := evaluator.Compile(tc.expr, env)
			if err != nil {
				t.Errorf("Compile(%q) = %v, want nil", tc.expr, err)
				return
			}
			if _, evalErr := evaluator.Evaluate(context.Background(), prog, env); evalErr != nil {
				t.Errorf("Evaluate(%q) = %v, want nil", tc.expr, evalErr)
			}
		})
	}
}

// TestPolicyExprEnv_ContextAlias verifies that the top-level context binding
// is the same map as run.context (alias per §6.4).
func TestPolicyExprEnv_ContextAlias(t *testing.T) {
	t.Parallel()

	run := exprEnvFixtureRun(t)
	outcome := exprEnvFixtureOutcome(t)
	env := NewPolicyExprGateEnv(run, outcome, false, nil, nil)

	// Both run.context and the top-level context binding should reference the
	// same underlying map.
	if len(env.Context) != len(env.Run.Context) {
		t.Errorf("context len = %d, run.context len = %d; must be the same map",
			len(env.Context), len(env.Run.Context))
	}
	for k, v := range env.Run.Context {
		if cv, ok := env.Context[k]; !ok || cv != v {
			t.Errorf("context[%q] = %v, run.context[%q] = %v; must be the same map", k, cv, k, v)
		}
	}
}

// TestPolicyExprEnv_OutcomeFailureClass_NilOnSuccess verifies that
// outcome.failure_class is nil in the view when outcome.status is SUCCESS
// (FailureClass is only present on FAIL per HC-058).
func TestPolicyExprEnv_OutcomeFailureClass_NilOnSuccess(t *testing.T) {
	t.Parallel()

	run := exprEnvFixtureRun(t)
	outcome := Outcome{Status: OutcomeStatusSuccess, Kind: OutcomeKindDefault}
	env := NewPolicyExprGateEnv(run, outcome, false, nil, nil)

	if env.Outcome.FailureClass != nil {
		t.Errorf("Outcome.FailureClass = %v, want nil for SUCCESS status", env.Outcome.FailureClass)
	}
	if env.Outcome.Status != "SUCCESS" {
		t.Errorf("Outcome.Status = %q, want \"SUCCESS\"", env.Outcome.Status)
	}
}

// TestPolicyExprEnv_OutcomeFailureClass_PresentOnFail verifies that
// outcome.failure_class is populated in the view when outcome.status is FAIL.
func TestPolicyExprEnv_OutcomeFailureClass_PresentOnFail(t *testing.T) {
	t.Parallel()

	run := exprEnvFixtureRun(t)
	fc := FailureClassTransient
	outcome := Outcome{
		Status:       OutcomeStatusFail,
		Kind:         OutcomeKindDefault,
		FailureClass: &fc,
	}
	env := NewPolicyExprGateEnv(run, outcome, false, nil, nil)

	if env.Outcome.FailureClass == nil {
		t.Fatal("Outcome.FailureClass = nil, want non-nil for FAIL outcome with FailureClass set")
	}
	if *env.Outcome.FailureClass != string(fc) {
		t.Errorf("Outcome.FailureClass = %q, want %q", *env.Outcome.FailureClass, fc)
	}
}

// TestPolicyExprEnv_RunView_BeadID verifies that run.bead_id is set correctly:
// - nil when the Run has no bead
// - non-nil string when BeadID is present
func TestPolicyExprEnv_RunView_BeadID(t *testing.T) {
	t.Parallel()

	run := exprEnvFixtureRun(t)
	outcome := exprEnvFixtureOutcome(t)

	// No bead: BeadID nil.
	envNoBead := NewPolicyExprGateEnv(run, outcome, false, nil, nil)
	if envNoBead.Run.BeadID != nil {
		t.Errorf("Run.BeadID = %v, want nil for run without bead", envNoBead.Run.BeadID)
	}

	// With bead: BeadID non-nil.
	bid := BeadID("hk-a8bg.34")
	run.BeadID = &bid
	envWithBead := NewPolicyExprGateEnv(run, outcome, false, nil, nil)
	if envWithBead.Run.BeadID == nil {
		t.Fatal("Run.BeadID = nil, want non-nil for run with bead")
	}
	if *envWithBead.Run.BeadID != string(bid) {
		t.Errorf("Run.BeadID = %q, want %q", *envWithBead.Run.BeadID, bid)
	}
}

// TestPolicyExprEnv_EventPayload_DecodeError verifies that an Event with
// non-JSON payload produces an empty map in the view (not a fatal error);
// expressions see no payload fields but evaluation is not blocked.
func TestPolicyExprEnv_EventPayload_DecodeError(t *testing.T) {
	t.Parallel()

	event := exprEnvFixtureEvent(t)
	event.Payload = json.RawMessage("not-valid-json{{{")

	run := exprEnvFixtureRun(t)
	outcome := exprEnvFixtureOutcome(t)
	env := NewPolicyExprHookEnv(run, outcome, event, false, nil)

	if env.Event == nil {
		t.Fatal("Event is nil, want non-nil even for bad payload")
	}
	// Payload decode error → empty map, not nil.
	if env.Event.Payload == nil {
		t.Error("Event.Payload = nil, want empty map on JSON decode error")
	}
	if len(env.Event.Payload) != 0 {
		t.Errorf("Event.Payload len = %d, want 0 on JSON decode error", len(env.Event.Payload))
	}
}

// TestPolicyExprEnv_GateExpr_E2E verifies the full compile + evaluate pipeline
// for a representative Gate expression (returns Bool) against a real
// PolicyExprEnv per §6.4.2.
//
// Gate expression: outcome.status == "SUCCESS" && run.workflow_version == policy_meta["required_version"]
func TestPolicyExprEnv_GateExpr_E2E(t *testing.T) {
	t.Parallel()

	run := exprEnvFixtureRun(t)
	outcome := exprEnvFixtureOutcome(t) // status = SUCCESS
	policyMeta := map[string]string{"required_version": string(run.WorkflowVersion)}

	env := NewPolicyExprGateEnv(run, outcome, false, policyMeta, nil)

	expression := `outcome.status == "SUCCESS" && run.workflow_version == policy_meta["required_version"]`

	evaluator := NewPolicyExprEvaluator(DefaultPolicyExprEvaluatorConfig())
	prog, _, err := evaluator.Compile(expression, env)
	if err != nil {
		t.Fatalf("Compile(%q) = %v, want nil", expression, err)
	}

	result, evalErr := evaluator.Evaluate(context.Background(), prog, env)
	if evalErr != nil {
		t.Fatalf("Evaluate(%q) = %v, want nil", expression, evalErr)
	}
	got, ok := result.Value.(bool)
	if !ok {
		t.Fatalf("Evaluate result type = %T, want bool", result.Value)
	}
	if !got {
		t.Errorf("Gate expression = false, want true for SUCCESS outcome with matching version")
	}
}

// TestPolicyExprEnv_HookSubscriptionFilter_E2E verifies the full pipeline for
// a Hook subscription_filter expression (returns Bool) per §6.4.2.
//
// Filter: event.type == "agent_completed" && event.payload["exit_code"] != 0
func TestPolicyExprEnv_HookSubscriptionFilter_E2E(t *testing.T) {
	t.Parallel()

	run := exprEnvFixtureRun(t)
	outcome := exprEnvFixtureOutcome(t)
	event := exprEnvFixtureEvent(t) // exit_code = 0

	env := NewPolicyExprHookEnv(run, outcome, event, false, nil)

	expression := `event.type == "agent_completed" && event.payload["exit_code"] != 0`

	evaluator := NewPolicyExprEvaluator(DefaultPolicyExprEvaluatorConfig())
	prog, _, err := evaluator.Compile(expression, env)
	if err != nil {
		t.Fatalf("Compile(%q) = %v, want nil", expression, err)
	}

	result, evalErr := evaluator.Evaluate(context.Background(), prog, env)
	if evalErr != nil {
		t.Fatalf("Evaluate(%q) = %v, want nil", expression, evalErr)
	}
	got, ok := result.Value.(bool)
	if !ok {
		t.Fatalf("Evaluate result type = %T, want bool", result.Value)
	}
	// exit_code == 0 in fixture, so filter should return false (no match).
	if got {
		t.Errorf("Hook filter = true for exit_code=0, want false (filter matches exit_code != 0)")
	}
}

// TestPolicyExprEnv_SideEffectFree verifies that the PolicyExprEnv is used with
// the expr-lang/expr library in a way that enforces the side-effect-free
// requirement of CP-034: expressions MUST NOT mutate the environment or perform
// I/O. The expr-lang/expr grammar is side-effect-free by construction (no
// assignment operator, no I/O primitives); this test verifies that evaluating
// an expression against the env does not modify the run's Context map.
func TestPolicyExprEnv_SideEffectFree(t *testing.T) {
	t.Parallel()

	run := exprEnvFixtureRun(t)
	outcome := exprEnvFixtureOutcome(t)
	env := NewPolicyExprGateEnv(run, outcome, false, nil, nil)

	// Capture initial context state.
	initialLen := len(env.Context)
	initialPhase, _ := env.Context["phase"].(string)

	evaluator := NewPolicyExprEvaluator(DefaultPolicyExprEvaluatorConfig())

	expression := `context["phase"] == "review"`
	prog, _, err := evaluator.Compile(expression, env)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if _, evalErr := evaluator.Evaluate(context.Background(), prog, env); evalErr != nil {
		t.Fatalf("Evaluate: %v", evalErr)
	}

	// Context must not have been mutated by expression evaluation.
	if len(env.Context) != initialLen {
		t.Errorf("context len changed after evaluation: before=%d, after=%d", initialLen, len(env.Context))
	}
	if got, _ := env.Context["phase"].(string); got != initialPhase {
		t.Errorf("context[\"phase\"] changed after evaluation: before=%q, after=%q", initialPhase, got)
	}
}

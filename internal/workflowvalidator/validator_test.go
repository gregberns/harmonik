package workflowvalidator

// validator_test.go — unit tests for PreRunValidator (EM-038).
//
// Tests use the preRunValidatorFixture prefix for package-level helpers per
// implementer-protocol.md helper-prefix discipline.
//
// Spec ref: specs/execution-model.md §10.2 EM-038 test obligation.
// Test obligation cite: §4.9.EM-038, §4.8.EM-034b, §4.10.EM-043.

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
)

// --- Fixture helpers (prefix: preRunValidatorFixture) ---

// preRunValidatorFixtureNew returns a PreRunValidator with a nil resolver and
// nil registry (suitable for structural-only tests where reference resolution
// is not needed).
func preRunValidatorFixtureNew(t *testing.T) *PreRunValidator {
	t.Helper()
	return New(nil, nil)
}

// preRunValidatorFixtureWithResolver returns a PreRunValidator that uses the
// given map (ref → dotSrc) as its WorkflowResolver.
func preRunValidatorFixtureWithResolver(t *testing.T, workflows map[string]string) *PreRunValidator {
	t.Helper()
	return New(&mapResolver{m: workflows}, nil)
}

// preRunValidatorFixtureWithRegistry returns a PreRunValidator that uses the
// given registry (see mapRegistry below).
func preRunValidatorFixtureWithRegistry(t *testing.T, reg *mapRegistry) *PreRunValidator {
	t.Helper()
	return New(nil, reg)
}

// preRunValidatorFixtureValidDOT returns a minimal valid workflow DOT string.
// The workflow has a start node and a terminal node connected by an edge.
func preRunValidatorFixtureValidDOT() string {
	return `digraph workflow {
    graph [
        workflow_id       = "018f1e2a-0000-7000-8000-000000000099"
        name              = "fixture-valid"
        version           = "0.1.0"
        start_node_id     = "node_a"
        terminal_node_ids = "node_b"
    ]

    node_a [
        type               = "non-agentic"
        idempotency_class  = "idempotent"
        "llm-freedom"      = "none"
        "io-determinism"   = "deterministic"
        "replay-safety"    = "safe"
        idempotency        = "idempotent"
        mode               = "mechanism"
    ]

    node_b [
        type               = "non-agentic"
        idempotency_class  = "idempotent"
        "llm-freedom"      = "none"
        "io-determinism"   = "deterministic"
        "replay-safety"    = "safe"
        idempotency        = "idempotent"
        mode               = "mechanism"
    ]

    node_a -> node_b [ordering_key = "a"]
}`
}

// preRunValidatorFixtureAgenticDOT returns a valid agentic-node workflow DOT string.
func preRunValidatorFixtureAgenticDOT() string {
	return `digraph workflow {
    graph [
        workflow_id       = "018f1e2a-0000-7000-8000-000000000098"
        name              = "fixture-agentic"
        version           = "0.1.0"
        start_node_id     = "agent_node"
        terminal_node_ids = "done_node"
    ]

    agent_node [
        type               = "agentic"
        handler_ref        = "handlers/my-handler"
        idempotency_class  = "non-idempotent"
        "llm-freedom"      = "unbounded"
        "io-determinism"   = "nondeterministic"
        "replay-safety"    = "unsafe"
        idempotency        = "non-idempotent"
        mode               = "cognition"
    ]

    done_node [
        type               = "non-agentic"
        idempotency_class  = "idempotent"
        "llm-freedom"      = "none"
        "io-determinism"   = "deterministic"
        "replay-safety"    = "safe"
        idempotency        = "idempotent"
        mode               = "mechanism"
    ]

    agent_node -> done_node [ordering_key = "a"]
}`
}

// preRunValidatorFixtureCycleWithCap returns a valid cyclic workflow DOT string
// where the cycle has an edge with a traversal_cap.
func preRunValidatorFixtureCycleWithCap() string {
	return `digraph workflow {
    graph [
        workflow_id       = "018f1e2a-0000-7000-8000-000000000097"
        name              = "fixture-cycle-capped"
        version           = "0.1.0"
        start_node_id     = "node_a"
        terminal_node_ids = "node_c"
    ]

    node_a [
        type               = "non-agentic"
        idempotency_class  = "idempotent"
        "llm-freedom"      = "none"
        "io-determinism"   = "deterministic"
        "replay-safety"    = "safe"
        idempotency        = "idempotent"
        mode               = "mechanism"
    ]

    node_b [
        type               = "non-agentic"
        idempotency_class  = "idempotent"
        "llm-freedom"      = "none"
        "io-determinism"   = "deterministic"
        "replay-safety"    = "safe"
        idempotency        = "idempotent"
        mode               = "mechanism"
    ]

    node_c [
        type               = "non-agentic"
        idempotency_class  = "idempotent"
        "llm-freedom"      = "none"
        "io-determinism"   = "deterministic"
        "replay-safety"    = "safe"
        idempotency        = "idempotent"
        mode               = "mechanism"
    ]

    node_a -> node_b [ordering_key = "a" traversal_cap = "5"]
    node_b -> node_a [ordering_key = "b"]
    node_b -> node_c [ordering_key = "c" condition = "done"]
}`
}

// --- Stub implementations for WorkflowResolver and ReferenceRegistry ---

// mapResolver is a WorkflowResolver backed by a string map.
type mapResolver struct {
	m map[string]string
}

func (r *mapResolver) Resolve(ref core.SubWorkflowRef) (string, error) {
	src, ok := r.m[string(ref)]
	if !ok {
		return "", errorf("mapResolver: no workflow registered for ref %q", ref)
	}
	return src, nil
}

// mapRegistry is a ReferenceRegistry backed by pre-populated string sets.
type mapRegistry struct {
	handlers     map[string]bool
	policies     map[string]bool
	gates        map[string]bool
	freedomProfs map[string]bool
	budgets      map[string]bool
	skills       map[string]bool
}

func newMapRegistry() *mapRegistry {
	return &mapRegistry{
		handlers:     make(map[string]bool),
		policies:     make(map[string]bool),
		gates:        make(map[string]bool),
		freedomProfs: make(map[string]bool),
		budgets:      make(map[string]bool),
		skills:       make(map[string]bool),
	}
}

func (r *mapRegistry) HasHandler(ref string) bool        { return r.handlers[ref] }
func (r *mapRegistry) HasPolicy(ref core.PolicyRef) bool { return r.policies[string(ref)] }
func (r *mapRegistry) HasGate(ref core.GateRef) bool     { return r.gates[string(ref)] }
func (r *mapRegistry) HasFreedomProfile(ref core.FreedomProfileRef) bool {
	return r.freedomProfs[string(ref)]
}
func (r *mapRegistry) HasBudget(ref core.BudgetRef) bool { return r.budgets[string(ref)] }
func (r *mapRegistry) HasSkill(name string) bool         { return r.skills[name] }

// errorf constructs a plain error (not a ValidationError) for resolver/registry failures.
func errorf(format string, args ...any) error {
	return fmt.Errorf(format, args...)
}

// --- Tests: valid workflow passes ---

func TestPreRunValidator_ValidWorkflow(t *testing.T) {
	t.Parallel()
	v := preRunValidatorFixtureNew(t)
	if err := v.Validate(preRunValidatorFixtureValidDOT()); err != nil {
		t.Errorf("Validate(valid DOT) = %v, want nil", err)
	}
}

func TestPreRunValidator_ValidAgenticWorkflow(t *testing.T) {
	t.Parallel()
	// Agentic workflow with handler_ref present — should pass structural validation.
	v := preRunValidatorFixtureNew(t)
	if err := v.Validate(preRunValidatorFixtureAgenticDOT()); err != nil {
		t.Errorf("Validate(valid agentic DOT) = %v, want nil", err)
	}
}

func TestPreRunValidator_ValidCycleWithCap(t *testing.T) {
	t.Parallel()
	v := preRunValidatorFixtureNew(t)
	if err := v.Validate(preRunValidatorFixtureCycleWithCap()); err != nil {
		t.Errorf("Validate(cycle with traversal_cap) = %v, want nil", err)
	}
}

// --- Tests: DOT parseability ---

func TestPreRunValidator_MalformedDOT_NotParseable(t *testing.T) {
	t.Parallel()
	v := preRunValidatorFixtureNew(t)
	err := v.Validate("this is not valid DOT at all {{{")
	if err == nil {
		t.Fatal("Validate(malformed DOT) = nil, want error")
	}
	assertErrorCode(t, err, codeNotParseable)
}

// --- Tests using malformed-DOT corpus ---

// TestPreRunValidator_CorpusBadEnum tests the malformedDotFixtureBadEnum corpus constant.
// The fixture uses type="human" which is not one of the five declared NodeType values.
func TestPreRunValidator_CorpusBadEnum(t *testing.T) {
	t.Parallel()
	v := preRunValidatorFixtureNew(t)
	err := v.Validate(malformedDotFixtureBadEnum)
	if err == nil {
		t.Fatal("Validate(malformedDotFixtureBadEnum) = nil, want validation error (EM-006: unknown node type)")
	}
	assertErrorCode(t, err, codeBadNodeType)
}

// TestPreRunValidator_CorpusMissingHandlerRef tests the malformedDotFixtureMissingHandlerRef corpus constant.
// An agentic node without handler_ref must be rejected per EM-007.
func TestPreRunValidator_CorpusMissingHandlerRef(t *testing.T) {
	t.Parallel()
	v := preRunValidatorFixtureNew(t)
	err := v.Validate(malformedDotFixtureMissingHandlerRef)
	if err == nil {
		t.Fatal("Validate(malformedDotFixtureMissingHandlerRef) = nil, want validation error (EM-007: agentic without handler_ref)")
	}
	assertErrorCode(t, err, codeMissingHandlerRef)
}

// TestPreRunValidator_CorpusMissingIdempotencyClass tests the malformedDotFixtureMissingIdempotencyClass corpus constant.
// A node without idempotency_class must be rejected per EM-009.
func TestPreRunValidator_CorpusMissingIdempotencyClass(t *testing.T) {
	t.Parallel()
	v := preRunValidatorFixtureNew(t)
	err := v.Validate(malformedDotFixtureMissingIdempotencyClass)
	if err == nil {
		t.Fatal("Validate(malformedDotFixtureMissingIdempotencyClass) = nil, want validation error (EM-009: missing idempotency_class)")
	}
	assertErrorCode(t, err, codeBadIdempotencyClass)
}

// TestPreRunValidator_CorpusUnreachableNode tests the malformedDotFixtureUnreachableNode corpus constant.
// A node not reachable from start_node_id must be rejected per EM-038 reachability.
func TestPreRunValidator_CorpusUnreachableNode(t *testing.T) {
	t.Parallel()
	v := preRunValidatorFixtureNew(t)
	err := v.Validate(malformedDotFixtureUnreachableNode)
	if err == nil {
		t.Fatal("Validate(malformedDotFixtureUnreachableNode) = nil, want validation error (EM-038 reachability)")
	}
	assertErrorCode(t, err, codeNodeNotReachable)
}

// TestPreRunValidator_CorpusMissingTerminalNodeIDs tests the malformedDotFixtureMissingTerminalNodeIDs corpus constant.
// An empty terminal_node_ids must be rejected per EM-038 attribute check.
func TestPreRunValidator_CorpusMissingTerminalNodeIDs(t *testing.T) {
	t.Parallel()
	v := preRunValidatorFixtureNew(t)
	err := v.Validate(malformedDotFixtureMissingTerminalNodeIDs)
	if err == nil {
		t.Fatal("Validate(malformedDotFixtureMissingTerminalNodeIDs) = nil, want validation error (EM-038: empty terminal_node_ids)")
	}
	assertErrorCode(t, err, codeMissingTerminalNodeIDs)
}

// TestPreRunValidator_CorpusMissingStartNodeID tests the malformedDotFixtureMissingStartNodeID corpus constant.
// A workflow without start_node_id must be rejected per EM-038 attribute check.
func TestPreRunValidator_CorpusMissingStartNodeID(t *testing.T) {
	t.Parallel()
	v := preRunValidatorFixtureNew(t)
	err := v.Validate(malformedDotFixtureMissingStartNodeID)
	if err == nil {
		t.Fatal("Validate(malformedDotFixtureMissingStartNodeID) = nil, want validation error (EM-038: missing start_node_id)")
	}
	assertErrorCode(t, err, codeMissingStartNodeID)
}

// TestPreRunValidator_CorpusSubWorkflowRefCycle tests the malformedDotFixtureSubWorkflowRefCycle corpus constant.
// A mutual sub-workflow reference cycle must be rejected per EM-034b.
func TestPreRunValidator_CorpusSubWorkflowRefCycle(t *testing.T) {
	t.Parallel()
	// The child workflow that references the parent, forming a cycle.
	childDOT := `digraph workflow {
    graph [
        workflow_id       = "wf-cycle-child-001"
        name              = "cycle-child"
        version           = "0.1.0"
        start_node_id     = "back_node"
        terminal_node_ids = "back_node"
    ]

    back_node [
        type                = "sub-workflow"
        sub_workflow_ref    = "wf-cycle-parent-001"
        idempotency_class   = "idempotent"
        "llm-freedom"       = "none"
        "io-determinism"    = "deterministic"
        "replay-safety"     = "safe"
        idempotency         = "idempotent"
        mode                = "mechanism"
    ]
}`
	workflows := map[string]string{
		"wf-cycle-child-001":  childDOT,
		"wf-cycle-parent-001": malformedDotFixtureSubWorkflowRefCycle,
	}
	v := preRunValidatorFixtureWithResolver(t, workflows)
	err := v.Validate(malformedDotFixtureSubWorkflowRefCycle)
	if err == nil {
		t.Fatal("Validate(malformedDotFixtureSubWorkflowRefCycle) = nil, want validation error (EM-034b: sub-workflow reference cycle)")
	}
	assertErrorCode(t, err, codeSubWorkflowCycle)
}

// TestPreRunValidator_CorpusMissingCapCycle tests the malformedDotFixtureMissingCapCycle corpus constant.
// A cycle without a traversal_cap on any edge must be rejected per EM-043.
func TestPreRunValidator_CorpusMissingCapCycle(t *testing.T) {
	t.Parallel()
	v := preRunValidatorFixtureNew(t)
	err := v.Validate(malformedDotFixtureMissingCapCycle)
	if err == nil {
		t.Fatal("Validate(malformedDotFixtureMissingCapCycle) = nil, want validation error (EM-043: cycle without traversal_cap)")
	}
	assertErrorCode(t, err, codeCycleNoCap)
}

// --- Additional targeted tests ---

// TestPreRunValidator_StartNodeNotDeclared checks that start_node_id pointing at
// a non-existent node is rejected.
func TestPreRunValidator_StartNodeNotDeclared(t *testing.T) {
	t.Parallel()
	dot := `digraph workflow {
    graph [
        workflow_id       = "wf-start-not-declared-001"
        name              = "start-not-declared"
        version           = "0.1.0"
        start_node_id     = "ghost"
        terminal_node_ids = "node_a"
    ]

    node_a [
        type               = "non-agentic"
        idempotency_class  = "idempotent"
        "llm-freedom"      = "none"
        "io-determinism"   = "deterministic"
        "replay-safety"    = "safe"
        idempotency        = "idempotent"
        mode               = "mechanism"
    ]
}`
	v := preRunValidatorFixtureNew(t)
	err := v.Validate(dot)
	if err == nil {
		t.Fatal("Validate() = nil, want error for undeclared start_node_id")
	}
	assertErrorCode(t, err, codeStartNodeNotDeclared)
}

// TestPreRunValidator_TerminalNodeNotDeclared checks that a terminal_node_ids
// entry pointing at a non-existent node is rejected.
func TestPreRunValidator_TerminalNodeNotDeclared(t *testing.T) {
	t.Parallel()
	dot := `digraph workflow {
    graph [
        workflow_id       = "wf-terminal-not-declared-001"
        name              = "terminal-not-declared"
        version           = "0.1.0"
        start_node_id     = "node_a"
        terminal_node_ids = "ghost"
    ]

    node_a [
        type               = "non-agentic"
        idempotency_class  = "idempotent"
        "llm-freedom"      = "none"
        "io-determinism"   = "deterministic"
        "replay-safety"    = "safe"
        idempotency        = "idempotent"
        mode               = "mechanism"
    ]
}`
	v := preRunValidatorFixtureNew(t)
	err := v.Validate(dot)
	if err == nil {
		t.Fatal("Validate() = nil, want error for undeclared terminal_node_id")
	}
	assertErrorCode(t, err, codeTerminalNodeNotDeclared)
}

// TestPreRunValidator_BadWorkflowClass verifies that a non-nil workflow_class
// other than "reconciliation" is rejected per EM-038.
func TestPreRunValidator_BadWorkflowClass(t *testing.T) {
	t.Parallel()
	dot := `digraph workflow {
    graph [
        workflow_id       = "wf-bad-class-001"
        name              = "bad-class"
        version           = "0.1.0"
        start_node_id     = "node_a"
        terminal_node_ids = "node_a"
        workflow_class    = "improvement-loop"
    ]

    node_a [
        type               = "non-agentic"
        idempotency_class  = "idempotent"
        "llm-freedom"      = "none"
        "io-determinism"   = "deterministic"
        "replay-safety"    = "safe"
        idempotency        = "idempotent"
        mode               = "mechanism"
    ]
}`
	v := preRunValidatorFixtureNew(t)
	err := v.Validate(dot)
	if err == nil {
		t.Fatal("Validate() = nil, want error for unsupported workflow_class")
	}
	assertErrorCode(t, err, codeBadWorkflowClass)
}

// TestPreRunValidator_ReconciliationWorkflowClass verifies that workflow_class
// = "reconciliation" is accepted.
func TestPreRunValidator_ReconciliationWorkflowClass(t *testing.T) {
	t.Parallel()
	dot := `digraph workflow {
    graph [
        workflow_id       = "wf-reconciliation-class-001"
        name              = "reconciliation-wf"
        version           = "0.1.0"
        start_node_id     = "node_a"
        terminal_node_ids = "node_a"
        workflow_class    = "reconciliation"
    ]

    node_a [
        type               = "non-agentic"
        idempotency_class  = "idempotent"
        "llm-freedom"      = "none"
        "io-determinism"   = "deterministic"
        "replay-safety"    = "safe"
        idempotency        = "idempotent"
        mode               = "mechanism"
    ]
}`
	v := preRunValidatorFixtureNew(t)
	if err := v.Validate(dot); err != nil {
		t.Errorf("Validate(workflow_class=reconciliation) = %v, want nil", err)
	}
}

// TestPreRunValidator_SubWorkflowResolved verifies that a valid sub-workflow
// that resolves correctly passes validation.
func TestPreRunValidator_SubWorkflowResolved(t *testing.T) {
	t.Parallel()
	childDOT := `digraph workflow {
    graph [
        workflow_id       = "wf-child-001"
        name              = "child-workflow"
        version           = "0.1.0"
        start_node_id     = "child_a"
        terminal_node_ids = "child_a"
    ]

    child_a [
        type               = "non-agentic"
        idempotency_class  = "idempotent"
        "llm-freedom"      = "none"
        "io-determinism"   = "deterministic"
        "replay-safety"    = "safe"
        idempotency        = "idempotent"
        mode               = "mechanism"
    ]
}`
	parentDOT := `digraph workflow {
    graph [
        workflow_id       = "wf-parent-001"
        name              = "parent-workflow"
        version           = "0.1.0"
        start_node_id     = "sub_node"
        terminal_node_ids = "sub_node"
    ]

    sub_node [
        type               = "sub-workflow"
        sub_workflow_ref   = "wf-child-001"
        idempotency_class  = "idempotent"
        "llm-freedom"      = "none"
        "io-determinism"   = "deterministic"
        "replay-safety"    = "safe"
        idempotency        = "idempotent"
        mode               = "mechanism"
    ]
}`
	v := preRunValidatorFixtureWithResolver(t, map[string]string{
		"wf-child-001": childDOT,
	})
	if err := v.Validate(parentDOT); err != nil {
		t.Errorf("Validate(sub-workflow resolves correctly) = %v, want nil", err)
	}
}

// TestPreRunValidator_SubWorkflowUnresolved verifies that a sub-workflow ref
// that has no registration fails with the correct error code.
func TestPreRunValidator_SubWorkflowUnresolved(t *testing.T) {
	t.Parallel()
	dot := `digraph workflow {
    graph [
        workflow_id       = "wf-unresolved-sub-001"
        name              = "unresolved-sub"
        version           = "0.1.0"
        start_node_id     = "sub_node"
        terminal_node_ids = "sub_node"
    ]

    sub_node [
        type               = "sub-workflow"
        sub_workflow_ref   = "wf-does-not-exist"
        idempotency_class  = "idempotent"
        "llm-freedom"      = "none"
        "io-determinism"   = "deterministic"
        "replay-safety"    = "safe"
        idempotency        = "idempotent"
        mode               = "mechanism"
    ]
}`
	v := preRunValidatorFixtureWithResolver(t, map[string]string{})
	err := v.Validate(dot)
	if err == nil {
		t.Fatal("Validate(unresolved sub_workflow_ref) = nil, want error")
	}
	assertErrorCode(t, err, codeSubWorkflowUnresolved)
}

// TestPreRunValidator_ReferenceResolution_HandlerRefUnresolved verifies that an
// agentic node whose handler_ref is not in the registry is rejected.
func TestPreRunValidator_ReferenceResolution_HandlerRefUnresolved(t *testing.T) {
	t.Parallel()
	reg := newMapRegistry()
	// Note: registry has no handler named "handlers/my-handler".
	v := preRunValidatorFixtureWithRegistry(t, reg)
	err := v.Validate(preRunValidatorFixtureAgenticDOT())
	if err == nil {
		t.Fatal("Validate(agentic with unregistered handler_ref) = nil, want error")
	}
	assertErrorCode(t, err, codeHandlerRefUnresolved)
}

// TestPreRunValidator_ReferenceResolution_HandlerRefRegistered verifies that an
// agentic node whose handler_ref resolves passes registry validation.
func TestPreRunValidator_ReferenceResolution_HandlerRefRegistered(t *testing.T) {
	t.Parallel()
	reg := newMapRegistry()
	reg.handlers["handlers/my-handler"] = true
	v := preRunValidatorFixtureWithRegistry(t, reg)
	if err := v.Validate(preRunValidatorFixtureAgenticDOT()); err != nil {
		t.Errorf("Validate(agentic with registered handler_ref) = %v, want nil", err)
	}
}

// TestPreRunValidator_NodeCannotReachTerminal verifies that a node with no path
// to any terminal is flagged.
func TestPreRunValidator_NodeCannotReachTerminal(t *testing.T) {
	t.Parallel()
	// node_b exists, is reachable from start, but has no outgoing edge to terminal.
	dot := `digraph workflow {
    graph [
        workflow_id       = "wf-dead-end-001"
        name              = "dead-end"
        version           = "0.1.0"
        start_node_id     = "node_a"
        terminal_node_ids = "node_c"
    ]

    node_a [
        type               = "non-agentic"
        idempotency_class  = "idempotent"
        "llm-freedom"      = "none"
        "io-determinism"   = "deterministic"
        "replay-safety"    = "safe"
        idempotency        = "idempotent"
        mode               = "mechanism"
    ]

    node_b [
        type               = "non-agentic"
        idempotency_class  = "idempotent"
        "llm-freedom"      = "none"
        "io-determinism"   = "deterministic"
        "replay-safety"    = "safe"
        idempotency        = "idempotent"
        mode               = "mechanism"
    ]

    node_c [
        type               = "non-agentic"
        idempotency_class  = "idempotent"
        "llm-freedom"      = "none"
        "io-determinism"   = "deterministic"
        "replay-safety"    = "safe"
        idempotency        = "idempotent"
        mode               = "mechanism"
    ]

    node_a -> node_b [ordering_key = "a"]
    node_a -> node_c [ordering_key = "b"]
}`
	// node_b is reachable but cannot reach node_c.
	v := preRunValidatorFixtureNew(t)
	err := v.Validate(dot)
	if err == nil {
		t.Fatal("Validate(node with no path to terminal) = nil, want error")
	}
	assertErrorCode(t, err, codeNodeCannotReachTerminal)
}

// TestPreRunValidator_TimeoutNotPositive verifies that a non-positive timeout is rejected.
func TestPreRunValidator_TimeoutNotPositive(t *testing.T) {
	t.Parallel()
	dot := `digraph workflow {
    graph [
        workflow_id       = "wf-timeout-zero-001"
        name              = "timeout-zero"
        version           = "0.1.0"
        start_node_id     = "node_a"
        terminal_node_ids = "node_a"
    ]

    node_a [
        type               = "non-agentic"
        idempotency_class  = "idempotent"
        "llm-freedom"      = "none"
        "io-determinism"   = "deterministic"
        "replay-safety"    = "safe"
        idempotency        = "idempotent"
        mode               = "mechanism"
        timeout            = "0"
    ]
}`
	v := preRunValidatorFixtureNew(t)
	err := v.Validate(dot)
	if err == nil {
		t.Fatal("Validate(timeout=0) = nil, want error (timeout must be positive)")
	}
	assertErrorCode(t, err, codeTimeoutNotPositive)
}

// TestPreRunValidator_PositiveTimeout verifies that a positive timeout is accepted.
func TestPreRunValidator_PositiveTimeout(t *testing.T) {
	t.Parallel()
	dot := `digraph workflow {
    graph [
        workflow_id       = "wf-timeout-pos-001"
        name              = "timeout-positive"
        version           = "0.1.0"
        start_node_id     = "node_a"
        terminal_node_ids = "node_a"
    ]

    node_a [
        type               = "non-agentic"
        idempotency_class  = "idempotent"
        "llm-freedom"      = "none"
        "io-determinism"   = "deterministic"
        "replay-safety"    = "safe"
        idempotency        = "idempotent"
        mode               = "mechanism"
        timeout            = "30"
    ]
}`
	v := preRunValidatorFixtureNew(t)
	if err := v.Validate(dot); err != nil {
		t.Errorf("Validate(timeout=30) = %v, want nil", err)
	}
}

// --- Tests: CP-036 / CP-056 — DOT attributes reference policy YAML by name ---

// preRunValidatorFixtureGateDOT returns a minimal valid gate-node workflow DOT
// string for use in gate_ref reference-resolution tests.
func preRunValidatorFixtureGateDOT(gateRef, handlerRef string) string {
	return `digraph workflow {
    graph [
        workflow_id       = "018f1e2a-0000-7000-8000-000000000097"
        name              = "fixture-gate"
        version           = "0.1.0"
        start_node_id     = "gate_node"
        terminal_node_ids = "done_node"
    ]

    gate_node [
        type               = "gate"
        gate_ref           = "` + gateRef + `"
        handler_ref        = "` + handlerRef + `"
        idempotency_class  = "idempotent"
        "llm-freedom"      = "none"
        "io-determinism"   = "deterministic"
        "replay-safety"    = "safe"
        idempotency        = "idempotent"
        mode               = "mechanism"
    ]

    done_node [
        type               = "non-agentic"
        idempotency_class  = "idempotent"
        "llm-freedom"      = "none"
        "io-determinism"   = "deterministic"
        "replay-safety"    = "safe"
        idempotency        = "idempotent"
        mode               = "mechanism"
    ]

    gate_node -> done_node
}`
}

// preRunValidatorFixtureWithRefDOT returns a minimal non-agentic workflow DOT
// string that carries an optional typed ref attribute (e.g., freedom_profile_ref
// or budget_ref) on its start node.
func preRunValidatorFixtureWithRefDOT(attrKey, attrValue string) string {
	return `digraph workflow {
    graph [
        workflow_id       = "018f1e2a-0000-7000-8000-000000000096"
        name              = "fixture-with-ref"
        version           = "0.1.0"
        start_node_id     = "node_a"
        terminal_node_ids = "node_b"
    ]

    node_a [
        type               = "non-agentic"
        idempotency_class  = "idempotent"
        "llm-freedom"      = "none"
        "io-determinism"   = "deterministic"
        "replay-safety"    = "safe"
        idempotency        = "idempotent"
        mode               = "mechanism"
        ` + attrKey + `    = "` + attrValue + `"
    ]

    node_b [
        type               = "non-agentic"
        idempotency_class  = "idempotent"
        "llm-freedom"      = "none"
        "io-determinism"   = "deterministic"
        "replay-safety"    = "safe"
        idempotency        = "idempotent"
        mode               = "mechanism"
    ]

    node_a -> node_b
}`
}

// TestPreRunValidator_CP056_PolicyRefRejected verifies that a node carrying the
// deprecated policy_ref attribute fails with codePolicyRefDeprecated (CP-056).
// policy_ref must be rejected regardless of whether a registry is present.
func TestPreRunValidator_CP056_PolicyRefRejected(t *testing.T) {
	t.Parallel()
	v := preRunValidatorFixtureNew(t) // no registry
	err := v.Validate(malformedDotFixturePolicyRefDeprecated)
	if err == nil {
		t.Fatal("Validate(policy_ref present) = nil, want codePolicyRefDeprecated error")
	}
	assertErrorCode(t, err, codePolicyRefDeprecated)
}

// TestPreRunValidator_CP056_PolicyRefRejectedWithRegistry verifies that the
// policy_ref rejection fires even when a registry is provided (defense-in-depth).
func TestPreRunValidator_CP056_PolicyRefRejectedWithRegistry(t *testing.T) {
	t.Parallel()
	reg := newMapRegistry()
	// Even if "some-deprecated-policy" were registered, policy_ref must be rejected.
	v := preRunValidatorFixtureWithRegistry(t, reg)
	err := v.Validate(malformedDotFixturePolicyRefDeprecated)
	if err == nil {
		t.Fatal("Validate(policy_ref with registry) = nil, want codePolicyRefDeprecated error")
	}
	assertErrorCode(t, err, codePolicyRefDeprecated)
}

// TestPreRunValidator_CP056_PolicyRefMessageNamesReplacements verifies that the
// CP-056 rejection message names the typed replacement attributes per the spec.
func TestPreRunValidator_CP056_PolicyRefMessageNamesReplacements(t *testing.T) {
	t.Parallel()
	v := preRunValidatorFixtureNew(t)
	err := v.Validate(malformedDotFixturePolicyRefDeprecated)
	if err == nil {
		t.Fatal("Validate(policy_ref present) = nil, want error")
	}
	for _, want := range []string{"gate_ref", "skills_ref", "freedom_profile_ref"} {
		if !hasDetailSubstring(err, want) {
			t.Errorf("CP-056 rejection message does not mention replacement attribute %q; got: %v", want, err)
		}
	}
}

// TestPreRunValidator_GateNodeMissingGateRef verifies that a gate node without
// gate_ref fails with codeMissingGateRef (CP-036 / CP-054).
func TestPreRunValidator_GateNodeMissingGateRef(t *testing.T) {
	t.Parallel()
	v := preRunValidatorFixtureNew(t)
	err := v.Validate(malformedDotFixtureMissingGateRef)
	if err == nil {
		t.Fatal("Validate(gate node without gate_ref) = nil, want codeMissingGateRef error")
	}
	assertErrorCode(t, err, codeMissingGateRef)
}

// TestPreRunValidator_GateNodeValidWithGateRef verifies that a gate node with
// both gate_ref and handler_ref passes structural validation.
func TestPreRunValidator_GateNodeValidWithGateRef(t *testing.T) {
	t.Parallel()
	v := preRunValidatorFixtureNew(t) // no registry: structural pass only
	dot := preRunValidatorFixtureGateDOT("my-gate", "handlers/gate-eval")
	if err := v.Validate(dot); err != nil {
		t.Errorf("Validate(gate node with gate_ref+handler_ref) = %v, want nil", err)
	}
}

// TestPreRunValidator_ReferenceResolution_GateRefUnresolved verifies that a gate
// node whose gate_ref is not in the registry is rejected (CP-036).
func TestPreRunValidator_ReferenceResolution_GateRefUnresolved(t *testing.T) {
	t.Parallel()
	reg := newMapRegistry()
	// Registry has no gate named "my-gate".
	v := preRunValidatorFixtureWithRegistry(t, reg)
	dot := preRunValidatorFixtureGateDOT("my-gate", "handlers/gate-eval")
	reg.handlers["handlers/gate-eval"] = true
	err := v.Validate(dot)
	if err == nil {
		t.Fatal("Validate(gate_ref not registered) = nil, want codeGateRefUnresolved error")
	}
	assertErrorCode(t, err, codeGateRefUnresolved)
}

// TestPreRunValidator_ReferenceResolution_GateRefRegistered verifies that a gate
// node whose gate_ref resolves in the registry passes validation (CP-036).
func TestPreRunValidator_ReferenceResolution_GateRefRegistered(t *testing.T) {
	t.Parallel()
	reg := newMapRegistry()
	reg.gates["my-gate"] = true
	reg.handlers["handlers/gate-eval"] = true
	v := preRunValidatorFixtureWithRegistry(t, reg)
	dot := preRunValidatorFixtureGateDOT("my-gate", "handlers/gate-eval")
	if err := v.Validate(dot); err != nil {
		t.Errorf("Validate(gate_ref registered) = %v, want nil", err)
	}
}

// TestPreRunValidator_ReferenceResolution_FreedomProfileRefUnresolved verifies
// that a node whose freedom_profile_ref is not registered is rejected (CP-036).
func TestPreRunValidator_ReferenceResolution_FreedomProfileRefUnresolved(t *testing.T) {
	t.Parallel()
	reg := newMapRegistry()
	v := preRunValidatorFixtureWithRegistry(t, reg)
	dot := preRunValidatorFixtureWithRefDOT("freedom_profile_ref", "strict-profile")
	err := v.Validate(dot)
	if err == nil {
		t.Fatal("Validate(freedom_profile_ref not registered) = nil, want codeFreedomProfileUnresolved error")
	}
	assertErrorCode(t, err, codeFreedomProfileUnresolved)
}

// TestPreRunValidator_ReferenceResolution_FreedomProfileRefRegistered verifies
// that a node whose freedom_profile_ref resolves in the registry passes (CP-036).
func TestPreRunValidator_ReferenceResolution_FreedomProfileRefRegistered(t *testing.T) {
	t.Parallel()
	reg := newMapRegistry()
	reg.freedomProfs["strict-profile"] = true
	v := preRunValidatorFixtureWithRegistry(t, reg)
	dot := preRunValidatorFixtureWithRefDOT("freedom_profile_ref", "strict-profile")
	if err := v.Validate(dot); err != nil {
		t.Errorf("Validate(freedom_profile_ref registered) = %v, want nil", err)
	}
}

// TestPreRunValidator_ReferenceResolution_BudgetRefUnresolved verifies that a
// node whose budget_ref is not registered is rejected (CP-036).
func TestPreRunValidator_ReferenceResolution_BudgetRefUnresolved(t *testing.T) {
	t.Parallel()
	reg := newMapRegistry()
	v := preRunValidatorFixtureWithRegistry(t, reg)
	dot := preRunValidatorFixtureWithRefDOT("budget_ref", "token-budget-1k")
	err := v.Validate(dot)
	if err == nil {
		t.Fatal("Validate(budget_ref not registered) = nil, want codeBudgetRefUnresolved error")
	}
	assertErrorCode(t, err, codeBudgetRefUnresolved)
}

// TestPreRunValidator_ReferenceResolution_BudgetRefRegistered verifies that a
// node whose budget_ref resolves in the registry passes validation (CP-036).
func TestPreRunValidator_ReferenceResolution_BudgetRefRegistered(t *testing.T) {
	t.Parallel()
	reg := newMapRegistry()
	reg.budgets["token-budget-1k"] = true
	v := preRunValidatorFixtureWithRegistry(t, reg)
	dot := preRunValidatorFixtureWithRefDOT("budget_ref", "token-budget-1k")
	if err := v.Validate(dot); err != nil {
		t.Errorf("Validate(budget_ref registered) = %v, want nil", err)
	}
}

// --- Helper: assertErrorCode ---

// assertErrorCode unwraps err (which may be a joined multi-error) and asserts
// that at least one ValidationError with the given code is present.
func assertErrorCode(t *testing.T, err error, code string) {
	t.Helper()
	if hasCode(err, code) {
		return
	}
	t.Errorf("expected validation error with code %q, got: %v", code, err)
}

// hasDetailSubstring recursively unwraps a (possibly joined) error and reports
// whether any ValidationError's Detail field contains the given substring.
func hasDetailSubstring(err error, sub string) bool {
	if err == nil {
		return false
	}
	var ve *ValidationError
	if errors.As(err, &ve) && strings.Contains(ve.Detail, sub) {
		return true
	}
	type multiUnwrap interface {
		Unwrap() []error
	}
	if mu, ok := err.(multiUnwrap); ok {
		for _, inner := range mu.Unwrap() {
			if hasDetailSubstring(inner, sub) {
				return true
			}
		}
	}
	return false
}

// hasCode recursively unwraps a (possibly joined) error and reports whether
// any ValidationError with the given code is present.
func hasCode(err error, code string) bool {
	if err == nil {
		return false
	}
	var ve *ValidationError
	if errors.As(err, &ve) && ve.Code == code {
		return true
	}
	// Unwrap joined errors (errors.Join returns an interface with Unwrap() []error).
	type multiUnwrap interface {
		Unwrap() []error
	}
	if mu, ok := err.(multiUnwrap); ok {
		for _, inner := range mu.Unwrap() {
			if hasCode(inner, code) {
				return true
			}
		}
	}
	return false
}

// ─────────────────────────────────────────────────────────────────────────────
// Corpus tests for new malformed_dot_corpus_test.go entries (hk-420yr.5)
// ─────────────────────────────────────────────────────────────────────────────

// TestPreRunValidator_CorpusForbiddenHandlerRef tests the
// malformedDotFixtureForbiddenHandlerRef corpus constant. A non-agentic node
// with handler_ref must be rejected per EM-038.
func TestPreRunValidator_CorpusForbiddenHandlerRef(t *testing.T) {
	t.Parallel()
	v := preRunValidatorFixtureNew(t)
	err := v.Validate(malformedDotFixtureForbiddenHandlerRef)
	if err == nil {
		t.Fatal("Validate(malformedDotFixtureForbiddenHandlerRef) = nil, want validation error (EM-038: forbidden handler_ref on non-agentic)")
	}
	assertErrorCode(t, err, codeForbiddenHandlerRef)
}

// TestPreRunValidator_CorpusMissingSubWorkflowRef tests the
// malformedDotFixtureMissingSubWorkflowRef corpus constant. A sub-workflow
// node without sub_workflow_ref must be rejected per EM-038.
func TestPreRunValidator_CorpusMissingSubWorkflowRef(t *testing.T) {
	t.Parallel()
	v := preRunValidatorFixtureNew(t)
	err := v.Validate(malformedDotFixtureMissingSubWorkflowRef)
	if err == nil {
		t.Fatal("Validate(malformedDotFixtureMissingSubWorkflowRef) = nil, want validation error (EM-038: sub-workflow node missing sub_workflow_ref)")
	}
	assertErrorCode(t, err, codeMissingSubWorkflowRef)
}

// TestPreRunValidator_CorpusBadLLMFreedom tests the malformedDotFixtureBadLLMFreedom
// corpus constant. A node with an unrecognised llm-freedom value must be rejected
// per EM-038.
func TestPreRunValidator_CorpusBadLLMFreedom(t *testing.T) {
	t.Parallel()
	v := preRunValidatorFixtureNew(t)
	err := v.Validate(malformedDotFixtureBadLLMFreedom)
	if err == nil {
		t.Fatal("Validate(malformedDotFixtureBadLLMFreedom) = nil, want validation error (EM-038: bad llm-freedom value)")
	}
	assertErrorCode(t, err, codeBadLLMFreedom)
}

// TestPreRunValidator_CorpusBadModeTag tests the malformedDotFixtureBadModeTag
// corpus constant. A node with an unrecognised mode value must be rejected per
// EM-038 / AR-005.
func TestPreRunValidator_CorpusBadModeTag(t *testing.T) {
	t.Parallel()
	v := preRunValidatorFixtureNew(t)
	err := v.Validate(malformedDotFixtureBadModeTag)
	if err == nil {
		t.Fatal("Validate(malformedDotFixtureBadModeTag) = nil, want validation error (EM-038/AR-005: bad mode tag)")
	}
	assertErrorCode(t, err, codeBadModeTag)
}

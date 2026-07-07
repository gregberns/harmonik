package workflowvalidator

// dot_happypath_hk420yr4_test.go — B1a subsystem-proof:
// PreRunValidator.Validate happy-path acceptance.
//
// All tests call Validate with inline DOT string literals — no files, no daemon.
// Covers: valid node-attrs for every node type, valid refs (fully-registered
// registry), and acyclic DAG graphs. Every test must produce nil error.
//
// Bead: hk-420yr.10 (subsystem-proofs B1a redo, re-land after false-close).
// Spec ref: specs/execution-model.md §4.9.EM-038.

import (
	"testing"
)

// ─────────────────────────────────────────────────────────────────────────────
// Happy-path: valid node-attrs for each node type
// ─────────────────────────────────────────────────────────────────────────────

// TestDOTHappyPath_B1a_NonAgenticNodeAllAttrs proves that a non-agentic node
// with every optional axis attribute set to valid values passes validation.
func TestDOTHappyPath_B1a_NonAgenticNodeAllAttrs(t *testing.T) {
	t.Parallel()
	dot := `digraph workflow {
    graph [
        workflow_id       = "b1a-non-agentic-all-attrs-001"
        name              = "b1a-non-agentic-all-attrs"
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
        timeout            = "120"
    ]

    node_b [
        type               = "non-agentic"
        idempotency_class  = "recoverable-non-idempotent"
        "llm-freedom"      = "none"
        "io-determinism"   = "best-effort"
        "replay-safety"    = "n/a"
        idempotency        = "n/a"
        mode               = "mechanism"
    ]

    node_a -> node_b [ordering_key = "a"]
}`
	v := preRunValidatorFixtureNew(t)
	if err := v.Validate(dot); err != nil {
		t.Errorf("Validate(non-agentic all attrs) = %v, want nil", err)
	}
}

// TestDOTHappyPath_B1a_AgenticNodeAllAttrs proves that an agentic node with
// all required and optional attributes set to valid values passes validation.
func TestDOTHappyPath_B1a_AgenticNodeAllAttrs(t *testing.T) {
	t.Parallel()
	dot := `digraph workflow {
    graph [
        workflow_id       = "b1a-agentic-all-attrs-001"
        name              = "b1a-agentic-all-attrs"
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
        timeout            = "1800"
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
	v := preRunValidatorFixtureNew(t)
	if err := v.Validate(dot); err != nil {
		t.Errorf("Validate(agentic all attrs) = %v, want nil", err)
	}
}

// TestDOTHappyPath_B1a_GateNodeAllAttrs proves that a gate node with
// both handler_ref and gate_ref passes structural validation.
func TestDOTHappyPath_B1a_GateNodeAllAttrs(t *testing.T) {
	t.Parallel()
	dot := `digraph workflow {
    graph [
        workflow_id       = "b1a-gate-all-attrs-001"
        name              = "b1a-gate-all-attrs"
        version           = "0.1.0"
        start_node_id     = "gate_node"
        terminal_node_ids = "done_node"
    ]

    gate_node [
        type               = "gate"
        handler_ref        = "handlers/gate-eval"
        gate_ref           = "gates/quality-gate"
        idempotency_class  = "idempotent"
        "llm-freedom"      = "none"
        "io-determinism"   = "deterministic"
        "replay-safety"    = "safe"
        idempotency        = "idempotent"
        mode               = "mechanism"
        timeout            = "60"
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

    gate_node -> done_node [ordering_key = "a"]
}`
	v := preRunValidatorFixtureNew(t)
	if err := v.Validate(dot); err != nil {
		t.Errorf("Validate(gate all attrs) = %v, want nil", err)
	}
}

// TestDOTHappyPath_B1a_AllNodeTypes proves all four node types can appear in
// the same workflow (start=non-agentic, agentic, gate, terminal=non-agentic).
func TestDOTHappyPath_B1a_AllNodeTypes(t *testing.T) {
	t.Parallel()
	dot := `digraph workflow {
    graph [
        workflow_id       = "b1a-all-node-types-001"
        name              = "b1a-all-node-types"
        version           = "0.1.0"
        start_node_id     = "start_node"
        terminal_node_ids = "done_node"
    ]

    start_node [
        type               = "non-agentic"
        idempotency_class  = "idempotent"
        "llm-freedom"      = "none"
        "io-determinism"   = "deterministic"
        "replay-safety"    = "safe"
        idempotency        = "idempotent"
        mode               = "mechanism"
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

    gate_node [
        type               = "gate"
        handler_ref        = "handlers/gate-eval"
        gate_ref           = "gates/quality-gate"
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

    start_node -> agent_node [ordering_key = "a"]
    agent_node -> gate_node  [ordering_key = "b"]
    gate_node  -> done_node  [ordering_key = "c"]
}`
	v := preRunValidatorFixtureNew(t)
	if err := v.Validate(dot); err != nil {
		t.Errorf("Validate(all node types) = %v, want nil", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Happy-path: valid refs (fully-registered registry)
// ─────────────────────────────────────────────────────────────────────────────

// TestDOTHappyPath_B1a_AllRefsRegistered proves that a workflow with every
// optional ref type (handler, gate, freedom_profile, budget, required_skills)
// passes validation when all refs are registered.
func TestDOTHappyPath_B1a_AllRefsRegistered(t *testing.T) {
	t.Parallel()
	dot := `digraph workflow {
    graph [
        workflow_id       = "b1a-all-refs-registered-001"
        name              = "b1a-all-refs-registered"
        version           = "0.1.0"
        start_node_id     = "agent_node"
        terminal_node_ids = "done_node"
    ]

    agent_node [
        type                 = "agentic"
        handler_ref          = "handlers/my-handler"
        freedom_profile_ref  = "profiles/strict"
        budget_ref           = "budgets/standard"
        required_skills      = "beads-cli harmonik-dispatch"
        idempotency_class    = "non-idempotent"
        "llm-freedom"        = "unbounded"
        "io-determinism"     = "nondeterministic"
        "replay-safety"      = "unsafe"
        idempotency          = "non-idempotent"
        mode                 = "cognition"
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
	reg := newMapRegistry()
	reg.handlers["handlers/my-handler"] = true
	reg.freedomProfs["profiles/strict"] = true
	reg.budgets["budgets/standard"] = true
	reg.skills["beads-cli"] = true
	reg.skills["harmonik-dispatch"] = true

	v := New(nil, reg)
	if err := v.Validate(dot); err != nil {
		t.Errorf("Validate(all refs registered) = %v, want nil", err)
	}
}

// TestDOTHappyPath_B1a_GateWithRegisteredRefs proves that a gate node with
// both gate_ref and handler_ref registered passes registry validation.
func TestDOTHappyPath_B1a_GateWithRegisteredRefs(t *testing.T) {
	t.Parallel()
	dot := preRunValidatorFixtureGateDOT("gates/quality-gate", "handlers/gate-eval")
	reg := newMapRegistry()
	reg.handlers["handlers/gate-eval"] = true
	reg.gates["gates/quality-gate"] = true

	v := New(nil, reg)
	if err := v.Validate(dot); err != nil {
		t.Errorf("Validate(gate with registered refs) = %v, want nil", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Happy-path: acyclic graphs (no cycles)
// ─────────────────────────────────────────────────────────────────────────────

// TestDOTHappyPath_B1a_LinearDAG proves a simple linear DAG (A → B → C)
// passes all reachability and cycle checks.
func TestDOTHappyPath_B1a_LinearDAG(t *testing.T) {
	t.Parallel()
	dot := `digraph workflow {
    graph [
        workflow_id       = "b1a-linear-dag-001"
        name              = "b1a-linear-dag"
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
    node_b -> node_c [ordering_key = "b"]
}`
	v := preRunValidatorFixtureNew(t)
	if err := v.Validate(dot); err != nil {
		t.Errorf("Validate(linear DAG A→B→C) = %v, want nil", err)
	}
}

// TestDOTHappyPath_B1a_DiamondDAG proves a diamond-shaped DAG
// (A → B, A → C, B → D, C → D) passes all checks with no cycle errors.
func TestDOTHappyPath_B1a_DiamondDAG(t *testing.T) {
	t.Parallel()
	dot := `digraph workflow {
    graph [
        workflow_id       = "b1a-diamond-dag-001"
        name              = "b1a-diamond-dag"
        version           = "0.1.0"
        start_node_id     = "node_a"
        terminal_node_ids = "node_d"
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

    node_d [
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
    node_b -> node_d [ordering_key = "c"]
    node_c -> node_d [ordering_key = "d"]
}`
	v := preRunValidatorFixtureNew(t)
	if err := v.Validate(dot); err != nil {
		t.Errorf("Validate(diamond DAG) = %v, want nil", err)
	}
}

// TestDOTHappyPath_B1a_CycleWithCapPasses proves that a workflow with a cycle
// is accepted when at least one edge in the cycle carries a positive traversal_cap.
func TestDOTHappyPath_B1a_CycleWithCapPasses(t *testing.T) {
	t.Parallel()
	// Re-exercises the capped-cycle fixture to confirm the happy-path acceptance.
	v := preRunValidatorFixtureNew(t)
	if err := v.Validate(preRunValidatorFixtureCycleWithCap()); err != nil {
		t.Errorf("Validate(cycle with traversal_cap) = %v, want nil", err)
	}
}

// TestDOTHappyPath_B1a_ReconciliationClass proves that workflow_class="reconciliation"
// is accepted by the validator.
func TestDOTHappyPath_B1a_ReconciliationClass(t *testing.T) {
	t.Parallel()
	dot := `digraph workflow {
    graph [
        workflow_id       = "b1a-reconciliation-class-001"
        name              = "b1a-reconciliation"
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

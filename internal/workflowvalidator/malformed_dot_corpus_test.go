package workflowvalidator

// Canonical malformed-DOT corpus for workflow-validator unit tests.
//
// Each constant is a self-contained DOT document that triggers exactly one
// EM-038 failure mode. Tests MUST pass the constant to the validator and assert
// that validation fails with the expected failure category. All constants in
// this file are named with the malformedDotFixture prefix per hk-b3f.88.
//
// Spec ref: specs/execution-model.md §10.2 EM-038 test obligation.
//
// Coverage map:
//   malformedDotFixtureBadEnum           — EM-006: unknown node type
//   malformedDotFixtureMissingHandlerRef — EM-007: agentic node without handler_ref
//   malformedDotFixtureMissingIdempotencyClass — EM-009: node without idempotency_class
//   malformedDotFixtureUnreachableNode   — EM-038 reachability: node not reachable from start_node_id
//   malformedDotFixtureMissingTerminalNodeIDs  — EM-038 attribute: terminal_node_ids is empty
//   malformedDotFixtureMissingStartNodeID      — EM-038 attribute: start_node_id absent
//   malformedDotFixtureSubWorkflowRefCycle     — EM-034b: mutual sub-workflow reference cycle
//   malformedDotFixtureMissingCapCycle         — EM-043: cycle without a per-edge traversal cap

// malformedDotFixtureBadEnum is a workflow whose node declares an unknown
// type value ("human"). The validator MUST reject this per EM-006: the only
// accepted types are agentic, non-agentic, gate, control-point, sub-workflow.
const malformedDotFixtureBadEnum = `digraph workflow {
    graph [
        workflow_id    = "wf-bad-enum-001"
        name           = "bad-enum-fixture"
        version        = "0.1.0"
        start_node_id  = "node_a"
        terminal_node_ids = "node_b"
    ]

    node_a [
        type               = "human"
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

    node_a -> node_b
}`

// malformedDotFixtureMissingHandlerRef is a workflow whose agentic node omits
// the required handler_ref attribute. The validator MUST reject this per EM-007:
// every agentic node MUST declare a handler_ref.
const malformedDotFixtureMissingHandlerRef = `digraph workflow {
    graph [
        workflow_id    = "wf-missing-handler-ref-001"
        name           = "missing-handler-ref-fixture"
        version        = "0.1.0"
        start_node_id  = "node_a"
        terminal_node_ids = "node_b"
    ]

    node_a [
        type               = "agentic"
        idempotency_class  = "non-idempotent"
        "llm-freedom"      = "full"
        "io-determinism"   = "non-deterministic"
        "replay-safety"    = "unsafe"
        idempotency        = "non-idempotent"
        mode               = "cognition"
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

// malformedDotFixtureMissingIdempotencyClass is a workflow whose node omits
// the idempotency_class attribute. The validator MUST reject this per EM-009:
// every node MUST carry an idempotency_class (explicit or policy-inherited;
// absence without a covering policy is an authoring error).
const malformedDotFixtureMissingIdempotencyClass = `digraph workflow {
    graph [
        workflow_id    = "wf-missing-idempotency-class-001"
        name           = "missing-idempotency-class-fixture"
        version        = "0.1.0"
        start_node_id  = "node_a"
        terminal_node_ids = "node_b"
    ]

    node_a [
        type               = "non-agentic"
        "llm-freedom"      = "none"
        "io-determinism"   = "deterministic"
        "replay-safety"    = "safe"
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

    node_a -> node_b
}`

// malformedDotFixtureUnreachableNode is a workflow that declares a node
// (orphan_node) with no incoming edge from start_node_id. The validator MUST
// reject this per EM-038 reachability: every node MUST be reachable from
// start_node_id.
const malformedDotFixtureUnreachableNode = `digraph workflow {
    graph [
        workflow_id    = "wf-unreachable-node-001"
        name           = "unreachable-node-fixture"
        version        = "0.1.0"
        start_node_id  = "node_a"
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

    orphan_node [
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

// malformedDotFixtureMissingTerminalNodeIDs is a workflow that declares an
// empty terminal_node_ids list. The validator MUST reject this per EM-038
// attribute check: the terminal_node_ids list MUST be non-empty (§6.1 EM-001).
const malformedDotFixtureMissingTerminalNodeIDs = `digraph workflow {
    graph [
        workflow_id    = "wf-missing-terminal-node-ids-001"
        name           = "missing-terminal-node-ids-fixture"
        version        = "0.1.0"
        start_node_id  = "node_a"
        terminal_node_ids = ""
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

// malformedDotFixtureMissingStartNodeID is a workflow that omits
// the start_node_id graph attribute. The validator MUST reject this per
// EM-038 attribute check: a workflow MUST declare start_node_id.
const malformedDotFixtureMissingStartNodeID = `digraph workflow {
    graph [
        workflow_id    = "wf-missing-start-node-id-001"
        name           = "missing-start-node-id-fixture"
        version        = "0.1.0"
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

// malformedDotFixtureSubWorkflowRefCycle is a workflow that contains a
// sub-workflow node referencing a workflow that in turn references this
// workflow (mutual reference). The validator MUST reject this per EM-034b:
// the sub-workflow reference graph MUST be acyclic. This fixture represents
// the local half of the cycle; the validator detects the cycle during
// transitive resolution.
//
// Cycle: wf-cycle-parent-001 → wf-cycle-child-001 → wf-cycle-parent-001
const malformedDotFixtureSubWorkflowRefCycle = `digraph workflow {
    graph [
        workflow_id    = "wf-cycle-parent-001"
        name           = "subworkflow-ref-cycle-fixture"
        version        = "0.1.0"
        start_node_id  = "expand_node"
        terminal_node_ids = "expand_node"
    ]

    expand_node [
        type                = "sub-workflow"
        sub_workflow_ref    = "wf-cycle-child-001"
        idempotency_class   = "idempotent"
        "llm-freedom"       = "none"
        "io-determinism"    = "deterministic"
        "replay-safety"     = "safe"
        idempotency         = "idempotent"
        mode                = "mechanism"
    ]
}`

// malformedDotFixtureMissingCapCycle is a workflow that contains a cycle
// (node_a → node_b → node_a) where no edge in the cycle carries a
// traversal_cap attribute. The validator MUST reject this per EM-043: every
// cycle MUST have at least one edge with a declared per-edge traversal cap.
const malformedDotFixtureMissingCapCycle = `digraph workflow {
    graph [
        workflow_id    = "wf-missing-cap-cycle-001"
        name           = "missing-cap-cycle-fixture"
        version        = "0.1.0"
        start_node_id  = "node_a"
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

    node_a -> node_b
    node_b -> node_a
    node_b -> node_c [condition = "done"]
}`

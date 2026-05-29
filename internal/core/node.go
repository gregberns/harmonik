package core

// Node is the 12-field graph vertex record for a workflow node
// (execution-model.md §6.1 RECORD Node).
//
// A Node is one of five declared types (NodeType). The conditional fields
// HandlerRef and SubWorkflowRef are governed by invariants enforced by Valid().
type Node struct {
	// NodeID is the workflow-unique identifier for this node.
	// Namespaced per §4.8.EM-034a on sub-workflow expansion (required; non-empty).
	NodeID NodeID

	// Type is the declared kind of this node.
	// One of: agentic, non-agentic, gate, control-point, sub-workflow.
	// Required; must satisfy NodeType.Valid().
	Type NodeType

	// HandlerRef is the handler reference for agentic nodes.
	// Required when Type == NodeTypeAgentic; forbidden otherwise.
	// See [handler-contract.md §4.1] and the HandlerRef type declaration
	// in this package (core/handlerref.go).
	//
	// A nil pointer represents the absent case (node is not agentic).
	// Non-agentic, gate, and control-point nodes MUST carry nil; agentic
	// nodes MUST carry a non-nil, non-empty HandlerRef per Valid().
	HandlerRef *HandlerRef

	// Timeout is the optional execution deadline for this node in integer seconds.
	// When set, must be positive (> 0). The wire shape is Integer | None per
	// execution-model.md §6.1 Node.timeout ("positive seconds").
	Timeout *int

	// ToolCommand is the shell command to execute for non-agentic shell nodes
	// (WG-039 / HC-063). When non-nil and HandlerRef points to "shell", the
	// built-in shell handler executes /bin/sh -c <ToolCommand> using Timeout
	// (default 300s). Forbidden on agentic, gate, and sub-workflow nodes.
	ToolCommand *string

	// RequiredSkills is the list of skill names this node requires, resolved
	// per [control-points.md §4.11] and [handler-contract.md §4.11].
	// An empty slice is valid (no skill requirements).
	RequiredSkills []string

	// PolicyRef is an optional reference to the policy governing this node.
	// See [control-points.md §6.4] (authoritative); also cited at §6.3 in some
	// prior contexts — §6.4 is the correct anchor per execution-model.md §6.1.
	PolicyRef *PolicyRef

	// GateRef is an optional reference to the gate governing this node.
	// See [control-points.md §6.2].
	GateRef *GateRef

	// FreedomProfileRef is an optional reference to the freedom profile for this node.
	// See [control-points.md §4.6].
	FreedomProfileRef *FreedomProfileRef

	// BudgetRef is an optional reference to the budget configuration for this node.
	// See [control-points.md §4.5].
	BudgetRef *BudgetRef

	// IdempotencyClass is the per-node tag driving reconciliation behavior.
	// One of: idempotent, non-idempotent, recoverable-non-idempotent.
	// Required; must satisfy IdempotencyClass.Valid().
	// May be inherited from a per-node-type default declared in a YAML policy
	// per [control-points.md §6.3] (EM-010); attribute absence is an authoring
	// error detected by the workflow validator (§4.9.EM-038).
	IdempotencyClass IdempotencyClass

	// Axes carries the four-axis classification tuple for this node
	// (llm-freedom, io-determinism, replay-safety, idempotency) per
	// [architecture.md §4.1 AR-001].
	Axes AxisTags

	// ModeTag is the mechanism/cognition classification for this node
	// per [architecture.md §4.2 AR-005]. One of: "mechanism", "cognition".
	ModeTag ModeTag

	// Prompt is the optional inline LLM prompt for this node (WG-040 §I.3,
	// HC-006a §III.3). When non-empty and Type == NodeTypeAgentic with an
	// implementer phase, it overrides the bead body as the agent's task
	// description (CHB-028 Body channel). Inert on reviewer nodes;
	// retained-but-ignored on non-agentic/gate nodes. Empty when absent.
	Prompt string

	// Model is the optional per-node model override (WG-042 §I.5, EM-012b-NODE).
	// When non-empty, overrides the run-level resolvedModel at dispatch time
	// for this node only. Shape: ^[A-Za-z0-9._:/-]+$, <=128 chars.
	// Empty when absent (node inherits run-level model).
	Model string

	// Effort is the optional per-node effort level (WG-042 §I.5, EM-012b-NODE).
	// When non-empty, must be one of {low,medium,high,xhigh,max} and overrides
	// the run-level resolvedEffort at dispatch time for this node only.
	// Empty when absent (node inherits run-level effort).
	Effort string

	// NonCommitting is the optional non_committing boolean field (WG-041 §I.4,
	// EM-015d carve-out). When true on an implementer-class agentic node, a
	// clean agent exit yields Outcome{SUCCESS} without requiring HEAD advance.
	// False by default. Meaningful only for dot-mode agentic implementer nodes.
	NonCommitting bool

	// SubWorkflowRef is the reference to the sub-workflow definition for
	// sub-workflow nodes. Required when Type == NodeTypeSubWorkflow; forbidden
	// otherwise.
	SubWorkflowRef *SubWorkflowRef
}

// Valid reports whether n satisfies all structural invariants declared in
// execution-model.md §6.1:
//
//   - NodeID is non-empty
//   - Type is one of the five declared NodeType values
//   - HandlerRef is non-nil iff Type == NodeTypeAgentic
//   - Timeout, when non-nil, is positive (> 0)
//   - IdempotencyClass is one of the three declared values
//   - Axes is a valid AxisTags tuple
//   - Axes.Idempotency matches IdempotencyClass (EM-011 cross-field constraint)
//   - ModeTag is one of the two declared ModeTag values
//   - SubWorkflowRef is non-nil iff Type == NodeTypeSubWorkflow
func (n Node) Valid() bool {
	if n.NodeID == "" {
		return false
	}
	if !n.Type.Valid() {
		return false
	}
	// HandlerRef: required iff agentic; forbidden otherwise.
	if n.Type == NodeTypeAgentic && n.HandlerRef == nil {
		return false
	}
	if n.Type != NodeTypeAgentic && n.HandlerRef != nil {
		return false
	}
	// Timeout: when set must be positive.
	if n.Timeout != nil && *n.Timeout <= 0 {
		return false
	}
	if !n.IdempotencyClass.Valid() {
		return false
	}
	if !n.Axes.Valid() {
		return false
	}
	// EM-011: Axes.Idempotency MUST match IdempotencyClass.
	if !idempotencyAxisMatchesClass(n.Axes.Idempotency, n.IdempotencyClass) {
		return false
	}
	if !n.ModeTag.Valid() {
		return false
	}
	// SubWorkflowRef: required iff sub-workflow; forbidden otherwise.
	if n.Type == NodeTypeSubWorkflow && n.SubWorkflowRef == nil {
		return false
	}
	if n.Type != NodeTypeSubWorkflow && n.SubWorkflowRef != nil {
		return false
	}
	return true
}

// idempotencyAxisMatchesClass reports whether the AxisIdempotency value from
// Axes.Idempotency is consistent with the node's IdempotencyClass per
// execution-model.md §4.2.EM-011.
//
// The mapping is one-to-one for the three shared values; AxisIdempotencyNA has
// no corresponding IdempotencyClass and always returns false.
func idempotencyAxisMatchesClass(axis AxisIdempotency, class IdempotencyClass) bool {
	switch class {
	case IdempotencyClassIdempotent:
		return axis == AxisIdempotencyIdempotent
	case IdempotencyClassNonIdempotent:
		return axis == AxisIdempotencyNonIdempotent
	case IdempotencyClassRecoverableNonIdempotent:
		return axis == AxisIdempotencyRecoverableNonIdempotent
	default:
		return false
	}
}

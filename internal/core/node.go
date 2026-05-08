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
	// See [handler-contract.md §4.1]. Bead hk-8mwo.72 tracks the typed-alias
	// upgrade from *string once HandlerRef is defined in core.
	//
	// TODO(hk-8mwo.72): replace *string with HandlerRef typed alias.
	HandlerRef *string

	// Timeout is the optional execution deadline for this node in integer seconds.
	// When set, must be positive (> 0). The wire shape is Integer | None per
	// execution-model.md §6.1 Node.timeout ("positive seconds").
	Timeout *int

	// RequiredSkills is the list of skill names this node requires, resolved
	// per [control-points.md §4.11] and [handler-contract.md §4.11].
	// An empty slice is valid (no skill requirements).
	RequiredSkills []string

	// PolicyRef is an optional reference to the policy governing this node.
	// See [control-points.md §6.3]. Bead hk-b3f.99 tracks the typed-alias
	// upgrade from *string once PolicyRef is defined in core.
	//
	// TODO(hk-b3f.99): replace *string with PolicyRef typed alias.
	PolicyRef *string

	// GateRef is an optional reference to the gate governing this node.
	// See [control-points.md §6.2]. Bead hk-b3f.100 tracks the typed-alias
	// upgrade from *string once GateRef is defined in core.
	//
	// TODO(hk-b3f.100): replace *string with GateRef typed alias.
	GateRef *string

	// FreedomProfileRef is an optional reference to the freedom profile for this node.
	// See [control-points.md §4.6]. Bead hk-b3f.101 tracks the typed-alias
	// upgrade from *string once FreedomProfileRef is defined in core.
	//
	// TODO(hk-b3f.101): replace *string with FreedomProfileRef typed alias.
	FreedomProfileRef *string

	// BudgetRef is an optional reference to the budget configuration for this node.
	// See [control-points.md §4.5]. Bead hk-b3f.102 tracks the typed-alias
	// upgrade from *string once BudgetRef is defined in core.
	//
	// TODO(hk-b3f.102): replace *string with BudgetRef typed alias.
	BudgetRef *string

	// IdempotencyClass is the per-node tag driving reconciliation behavior.
	// One of: idempotent, non-idempotent, recoverable-non-idempotent.
	// Required; must satisfy IdempotencyClass.Valid().
	// May be inherited from a per-node-type default declared in a YAML policy
	// per [control-points.md §6.3] (EM-010); attribute absence is an authoring
	// error detected by the workflow validator (§4.9.EM-038).
	IdempotencyClass IdempotencyClass

	// Axes carries the four-axis classification tuple for this node
	// (llm-freedom, io-determinism, replay-safety, idempotency) per
	// [architecture.md §4.1 AR-001]. Bead hk-b3f.97 tracks the typed-alias
	// upgrade from string once AxisTags is defined in core.
	//
	// TODO(hk-b3f.97): replace string with AxisTags typed alias.
	Axes string

	// ModeTag is the mechanism/cognition classification for this node
	// per [architecture.md §4.2 AR-005]. One of: "mechanism", "cognition".
	// Bead hk-b3f.98 tracks the typed-alias upgrade from string once ModeTag
	// is defined in core.
	//
	// TODO(hk-b3f.98): replace string with ModeTag typed alias.
	ModeTag string

	// SubWorkflowRef is the reference to the sub-workflow definition for
	// sub-workflow nodes. Required when Type == NodeTypeSubWorkflow; forbidden
	// otherwise. Bead hk-b3f.103 tracks the typed-alias upgrade from *string
	// once SubWorkflowRef is defined in core.
	//
	// TODO(hk-b3f.103): replace *string with SubWorkflowRef typed alias.
	SubWorkflowRef *string
}

// Valid reports whether n satisfies all structural invariants declared in
// execution-model.md §6.1:
//
//   - NodeID is non-empty
//   - Type is one of the five declared NodeType values
//   - HandlerRef is non-nil iff Type == NodeTypeAgentic
//   - Timeout, when non-nil, is positive (> 0)
//   - IdempotencyClass is one of the three declared values
//   - Axes is non-empty
//   - ModeTag is non-empty
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
	if n.Axes == "" {
		return false
	}
	if n.ModeTag == "" {
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

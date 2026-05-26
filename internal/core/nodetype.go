// Package core holds shared types that cross subsystem boundaries.
// It imports nothing from internal/* subsystems — only stdlib and a narrow allowlist.
// See docs/foundation/project-level/subsystem-organization.md §Shared types.
package core

import "fmt"

// NodeType is the kind of a workflow node (execution-model.md §6.1).
// One of: agentic, non-agentic, gate, sub-workflow.
// Validators (EM-038) reject any other value. The former "control-point" value
// was removed per WG-001 (workflow-graph.md §3, §16.2): control points bind to
// node attributes (gate_ref, hook_ref, guard_ref, budget_ref), not to a node
// type of their own.
type NodeType string

// NodeType values per execution-model.md §6.1 ENUM declaration (WG-001: four members).
const (
	NodeTypeAgentic     NodeType = "agentic"
	NodeTypeNonAgentic  NodeType = "non-agentic"
	NodeTypeGate        NodeType = "gate"
	NodeTypeSubWorkflow NodeType = "sub-workflow"
)

// Valid reports whether n is one of the four declared NodeType constants.
// This is the predicate hook for EM-038: the pre-run validator calls Valid
// on every node's type field and rejects workflows that contain unknown values.
func (n NodeType) Valid() bool {
	switch n {
	case NodeTypeAgentic, NodeTypeNonAgentic, NodeTypeGate, NodeTypeSubWorkflow:
		return true
	default:
		return false
	}
}

// MarshalText implements encoding.TextMarshaler so NodeType serialises
// correctly in JSON and YAML workflow definitions.
func (n NodeType) MarshalText() ([]byte, error) {
	if !n.Valid() {
		return nil, fmt.Errorf("nodetype: unknown value %q", string(n))
	}
	return []byte(n), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
// It rejects any value that is not one of the four declared constants,
// satisfying the EM-038 requirement that unknown types are rejected.
func (n *NodeType) UnmarshalText(text []byte) error {
	v := NodeType(text)
	if !v.Valid() {
		return fmt.Errorf("nodetype: unknown value %q; must be one of agentic, non-agentic, gate, sub-workflow", string(text))
	}
	*n = v
	return nil
}

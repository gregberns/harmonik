package dot

// ast.go — typed AST for workflow_mode=dot graphs.
//
// The AST produced by Parse is the canonical in-memory representation of a
// .dot workflow artifact.  It encodes the closed-set node types, the edge
// field set locked by EM-002, and the mixed strict/permissive unknown-attribute
// policy of WG-031/WG-032.
//
// Spec refs:
//   - specs/workflow-graph.md §4 (WG-001, WG-002) — node types and attributes.
//   - specs/workflow-graph.md §5 (WG-009) — edge field set.
//   - specs/workflow-graph.md §6 (WG-013, WG-014, WG-015) — edge-condition dialect.
//   - specs/workflow-graph.md §10 (WG-031, WG-032) — unknown-attribute policy.
//   - specs/workflow-graph.md §11 (WG-033, WG-035) — schema_version / version.
//
// Tags: mechanism, normative

import (
	"fmt"
	"strings"

	"github.com/gregberns/harmonik/internal/core"
)

// Ensure core is used (NodeType is referenced on Node.Type).
var _ core.NodeType

// Graph is the top-level parsed representation of a .dot workflow artifact.
// It corresponds to a single `digraph` block.
//
// Spec: WG-033 (schema_version graph-level), WG-035 (version distinct from schema_version),
// WG-027 (start_node / terminal_node_ids well-formedness).
type Graph struct {
	// Name is the DOT digraph name (may be empty if the source omits it).
	Name string

	// SchemaVersion is the value of the graph-level schema_version attribute
	// per WG-033.  Parser does NOT interpret the version; the validator layer
	// enforces the N-1 contract per WG-034.
	SchemaVersion string

	// Version is the workflow author's own version field per WG-035
	// (distinct from SchemaVersion).
	Version string

	// StartNodeID is the parsed value of the start_node graph-level DOT
	// attribute per WG-027.  The corresponding DOT attribute name is
	// "start_node"; the parsed Go field is StartNodeID.
	StartNodeID string

	// TerminalNodeIDs is the parsed value of the terminal_node_ids
	// graph-level DOT attribute per WG-027.  Comma or space separated in
	// the source; stored as a slice here.
	TerminalNodeIDs []string

	// ContextKeys is the parsed value of the context_keys graph-level DOT
	// attribute per WG-031a.  Comma or space separated in the source;
	// stored as a slice here.  At v1.0 the loader retains but does NOT
	// validate individual context.<key> LHS references against this list.
	ContextKeys []string

	// WorkflowClass is the optional workflow_class graph-level attribute.
	WorkflowClass string

	// Nodes is the ordered list of nodes in declaration order.
	Nodes []*Node

	// Edges is the ordered list of edges in declaration order.
	Edges []*Edge

	// Warnings is the list of parse-time permissive warnings (WG-031).
	// Non-nil when any unknown permissive attribute was encountered.
	Warnings []ParseWarning

	// UnknownAttrs retains any non-reserved graph-level attributes per
	// WG-032 (round-trip retention of unknown permissive attributes).
	UnknownAttrs map[string]string
}

// Node is one graph vertex.  Its Type field drives all other attribute
// interpretation.
//
// Spec: WG-001 (closed enum), WG-002 (per-type attribute catalog),
// WG-031 (reserved-name strict errors), WG-032 (unknown permissive retention).
type Node struct {
	// ID is the DOT node identifier.
	ID string

	// Type is the node type; one of the four members of the closed enum
	// {agentic, non-agentic, gate, sub-workflow} per WG-001.
	// A node with an unknown type is a strict ingest error per WG-024;
	// the parser still populates RawType so callers can produce precise errors.
	Type core.NodeType

	// RawType is the raw string from the DOT source before NodeType
	// coercion.  Useful for error messages when Type == "".
	RawType string

	// --- Required / optional per-type attributes (WG-002) ---

	// AgentType is the agent_type attribute on agentic nodes (open set per WG-003).
	AgentType string

	// HandlerRef is the handler_ref attribute.  Required on agentic, gate, and
	// non-agentic nodes per WG-005 (EM-007 amendment).
	HandlerRef string

	// GateRef is the gate_ref attribute.  Required on gate nodes per WG-005.
	GateRef string

	// SubWorkflowRef is the sub_workflow_ref attribute.  Required on
	// sub-workflow nodes per WG-006.
	SubWorkflowRef string

	// WorkflowVersion is the workflow_version attribute on sub-workflow nodes
	// per WG-006.
	WorkflowVersion string

	// InputMapping is the optional input_mapping attribute on sub-workflow
	// nodes per WG-006.
	InputMapping string

	// IdempotencyClass is the idempotency_class attribute per WG-008.
	// Required on agentic and non-agentic nodes; forbidden on gate and
	// sub-workflow nodes per WG-008.
	IdempotencyClass string

	// AxisTags is the optional axis_tags attribute (open set per WG-030).
	AxisTags string

	// HookRef is the optional hook_ref attribute.
	HookRef string

	// GuardRef is the optional guard_ref attribute.
	GuardRef string

	// BudgetRef is the optional budget_ref attribute.
	BudgetRef string

	// SkillsRef is the optional skills_ref attribute (typed *_ref per CP-055).
	SkillsRef string

	// FreedomProfileRef is the optional freedom_profile_ref attribute (typed *_ref per CP-055).
	FreedomProfileRef string

	// UnknownAttrs retains non-reserved node-level attributes per WG-032.
	// Keys and values are retained verbatim for debugging tools / replay tooling.
	// The dispatcher MUST NOT read from this map; it is for informational use only.
	UnknownAttrs map[string]string
}

// Edge is a directed transition between two nodes.
// The field set is locked by EM-002 and cited in WG-009.
type Edge struct {
	// FromNodeID is the source node ID.
	FromNodeID string

	// ToNodeID is the destination node ID.
	ToNodeID string

	// Condition is the parsed edge condition per the restricted dialect of §6
	// WG-013.  Nil means the edge is unconditional.
	Condition *Condition

	// ConditionRaw is the original raw condition string from the DOT source.
	// Retained for round-trip and error reporting even when Condition is non-nil.
	ConditionRaw string

	// PreferredLabel is the optional preferred_label attribute per WG-009 /
	// EM-002.
	PreferredLabel string

	// Weight is the optional weight attribute per WG-009 / EM-002.
	// Stored as a string; callers convert to int as needed.
	Weight string

	// OrderingKey is the optional ordering_key attribute per WG-009 / EM-002.
	OrderingKey string

	// UnknownAttrs retains non-reserved edge-level attributes per WG-032.
	UnknownAttrs map[string]string
}

// Condition is a parsed edge condition in the restricted equality dialect of
// §6 WG-013.  The top-level conjunction is a slice of one or more Equality
// expressions joined by &&.
type Condition struct {
	// Clauses contains the equality expressions.  At least one element;
	// multiple elements represent a conjunction (&&).
	Clauses []Equality
}

// Equality is a single lhs op literal comparison per WG-013.
type Equality struct {
	// LHS is the left-hand side; must be one of the WG-014 whitelist members.
	LHS string

	// Op is "==" or "!=".
	Op string

	// RHS is the right-hand operand literal (string, integer, or enum member).
	RHS string
}

// ParseWarning records a non-fatal permissive-position warning from Parse per
// WG-031.  The run MAY start; the warning is for tooling / replay callers.
type ParseWarning struct {
	// Line is the 1-based source line at which the unknown attribute was
	// encountered.
	Line int

	// Message describes the unknown attribute (e.g. "node \"start\": unknown
	// permissive attribute \"priority\"").
	Message string
}

// ParseError is a fatal strict-position error from Parse per WG-031.
// A graph with any ParseError MUST NOT start.
type ParseError struct {
	// Line is the 1-based source line at which the error was detected.
	Line int

	// Message describes the violation.
	Message string
}

func (e *ParseError) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("dot:%d: %s", e.Line, e.Message)
	}
	return "dot: " + e.Message
}

// ParseErrors is a multi-error value when multiple strict errors are collected
// during a single Parse call.
type ParseErrors []*ParseError

func (pe ParseErrors) Error() string {
	if len(pe) == 0 {
		return "dot: no errors"
	}
	msgs := make([]string, len(pe))
	for i, e := range pe {
		msgs[i] = e.Error()
	}
	return strings.Join(msgs, "; ")
}

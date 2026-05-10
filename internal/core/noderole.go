package core

import "fmt"

// NodeRole is the semantic role of a workflow node that drives per-type
// idempotency-class defaults (execution-model.md §4.2.EM-010).
//
// NodeRole is distinct from [NodeType]: NodeType describes a node's structural
// kind (agentic, non-agentic, gate, …); NodeRole describes its operational
// purpose (reviewer, builder, …). A NodeRole tag enables the runtime to apply
// the correct idempotency-class default absent a YAML policy override.
//
// The enum is closed at MVH for the eight declared roles. Post-MVH node types
// MAY register additional NodeRole values with a declared resume protocol per
// EM-010; extension requires the amendment protocol per [architecture.md §4.6].
//
// A reader observing an unknown NodeRole MUST NOT silently default to a class;
// callers MUST call [DefaultIdempotencyClassForNodeRole] and check the ok
// return before using the result.
type NodeRole string

// NodeRole values per execution-model.md §4.2.EM-010.
const (
	// NodeRoleReviewer is a node that performs review or quality-assessment
	// cognition (agentic, cognition-tagged). Defaults to idempotent per EM-010.
	NodeRoleReviewer NodeRole = "reviewer"

	// NodeRoleResearcher is a node that performs open-ended research cognition
	// (agentic, cognition-tagged). Defaults to idempotent per EM-010.
	NodeRoleResearcher NodeRole = "researcher"

	// NodeRoleLint is a node that runs a linting tool (non-agentic, mechanism-tagged).
	// Defaults to idempotent per EM-010.
	NodeRoleLint NodeRole = "lint"

	// NodeRoleTest is a node that runs a test suite (non-agentic, mechanism-tagged).
	// Defaults to idempotent per EM-010.
	NodeRoleTest NodeRole = "test"

	// NodeRoleTypecheck is a node that runs a type-checker (non-agentic, mechanism-tagged).
	// Defaults to idempotent per EM-010.
	NodeRoleTypecheck NodeRole = "typecheck"

	// NodeRoleAnalysis is a node that runs a static-analysis tool
	// (non-agentic, mechanism-tagged). Defaults to idempotent per EM-010.
	NodeRoleAnalysis NodeRole = "analysis"

	// NodeRoleBuilder is a node that performs build or compilation work
	// (non-agentic or agentic; produces build artifacts). Defaults to
	// non-idempotent per EM-010.
	NodeRoleBuilder NodeRole = "builder"

	// NodeRoleMerge is a node that performs a merge or integration operation
	// (non-agentic; mutates shared state). Defaults to non-idempotent per EM-010.
	NodeRoleMerge NodeRole = "merge"
)

// Valid reports whether r is one of the eight declared NodeRole constants at MVH.
// Unknown values return false; callers MUST NOT silently degrade to a default
// idempotency class — check the ok return of [DefaultIdempotencyClassForNodeRole]
// instead.
func (r NodeRole) Valid() bool {
	switch r {
	case NodeRoleReviewer,
		NodeRoleResearcher,
		NodeRoleLint,
		NodeRoleTest,
		NodeRoleTypecheck,
		NodeRoleAnalysis,
		NodeRoleBuilder,
		NodeRoleMerge:
		return true
	default:
		return false
	}
}

// MarshalText implements encoding.TextMarshaler so NodeRole serialises
// correctly in JSON and YAML workflow definitions.
// It rejects any value that is not one of the eight declared constants at MVH.
func (r NodeRole) MarshalText() ([]byte, error) {
	if !r.Valid() {
		return nil, fmt.Errorf("noderole: unknown value %q", string(r))
	}
	return []byte(r), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
// It rejects any value that is not one of the eight declared constants at MVH.
// Callers MUST NOT silently degrade to a default role on error.
func (r *NodeRole) UnmarshalText(text []byte) error {
	v := NodeRole(text)
	if !v.Valid() {
		return fmt.Errorf(
			"noderole: unknown value %q; must be one of reviewer, researcher, lint, test, typecheck, analysis, builder, merge",
			string(text),
		)
	}
	*r = v
	return nil
}

// Package workflowvalidator implements the pre-run workflow validator required
// by specs/execution-model.md §4.9 EM-038. The validator verifies every
// structural and attribute constraint that MUST hold before any node in a
// workflow executes; any failure prevents the workflow from starting.
//
// Scope (per EM-038):
//   - DOT parseability.
//   - Sub-workflow resolution (transitive) and acyclicity (EM-034b).
//   - Reference resolution: handler_ref, gate_ref, freedom_profile_ref,
//     budget_ref, and required_skills entries resolve to registered targets.
//   - CP-056 rejection: policy_ref is deprecated; any occurrence is rejected.
//   - Attribute type checks: enum values, required attributes, positive integers.
//   - Reachability: every node reachable from start_node_id; every node can
//     reach at least one terminal_node_ids entry.
//   - Cycle-bound check: every cycle carries a per-edge traversal cap (EM-043).
//
// Every check in this package is mechanism-tagged (EM-039); no check delegates
// to cognition.
package workflowvalidator

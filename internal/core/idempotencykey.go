// Package core holds shared types that cross subsystem boundaries.
// No imports from any internal subsystem are permitted (see internal/core depguard rule).
package core

// IdempotencyKey derives the deterministic idempotency key for a terminal-transition
// write to the Beads CLI adapter.
//
// The key has the form:
//
//	<run_id>:<transition_id>:<op>
//
// This satisfies beads-integration.md §4.10 BI-029: the key MUST be deterministic —
// identical (runID, transitionID, op) inputs MUST produce identical keys across
// invocations. run_id and transition_id are rendered as canonical hyphenated UUID
// strings (lowercase); op is one of "claim", "close", "reopen".
//
// The caller is responsible for ensuring op is valid before calling IdempotencyKey.
// No validation is performed here; the function is a pure key-derivation primitive.
func IdempotencyKey(runID RunID, transitionID TransitionID, op TerminalOp) string {
	return runID.String() + ":" + transitionID.String() + ":" + string(op)
}

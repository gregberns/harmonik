// Package core holds shared types that cross subsystem boundaries.
// No imports from any internal subsystem are permitted (see internal/core depguard rule).
package core

import "fmt"

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
//
// NOTE: the reset op (BI-010d) uses a different key formula — see ResetBeadIdempotencyKey.
func IdempotencyKey(runID RunID, transitionID TransitionID, op TerminalOp) string {
	return runID.String() + ":" + transitionID.String() + ":" + string(op)
}

// ResetBeadIdempotencyKey derives the deterministic idempotency key for a
// BI-010d orphan-sweep reset write (in_progress → open).
//
// The key has the form:
//
//	<project_hash>:<bead_id>:reset:<daemon_start_ns>
//
// This is the formula mandated by beads-integration.md §4.4 BI-010d (NOTE after
// the BI-010a table):
//
//	"the idempotency-key formula is `<project_hash>:<bead_id>:reset:<daemon_start_ns>`"
//
// daemonStartNS is the daemon startup time expressed as nanoseconds since the
// Unix epoch. It scopes the reset key to a single daemon lifetime so that a
// subsequent daemon startup on the same project produces a distinct key.
//
// The caller is responsible for ensuring projectHash and beadID are non-empty.
// No validation is performed here; the function is a pure key-derivation primitive.
//
// Spec ref: specs/beads-integration.md §4.4 BI-010d; §4.10 BI-029.
func ResetBeadIdempotencyKey(projectHash ProjectHash, beadID BeadID, daemonStartNS int64) string {
	return fmt.Sprintf("%s:%s:reset:%d", string(projectHash), string(beadID), daemonStartNS)
}

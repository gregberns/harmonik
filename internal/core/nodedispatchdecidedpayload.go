package core

import "github.com/google/uuid"

// NodeDispatchDecidedPayload is the typed event payload for the
// node_dispatch_decided event, emitted by the DOT-mode cascade engine after
// the EM-041 edge-selection cascade (WG-010) has resolved the next node, or
// determined that the current node is terminal, or produced a cascade failure
// (no matching edge per WG-012, or traversal cap hit per EM-043).
//
// Durability class: O (ordinary — observability stream; the durable transition
// record is the checkpoint written by the daemon, not this event).
//
// Spec refs:
//   - specs/workflow-graph.md §5 WG-010 — five-step cascade.
//   - specs/workflow-graph.md §5 WG-011 — unconditional-edge fallback invariant.
//   - specs/workflow-graph.md §5 WG-012 — no-match-set fallback (failure).
//   - specs/execution-model.md §4.10 EM-041 — deterministic cascade.
//   - specs/execution-model.md §4.10 EM-043 — traversal cap / compilation_loop.
//   - specs/execution-model.md §4.3 EM-015e — cap_hit completion_reason vocabulary.
//   - specs/execution-model.md §7.5.2 EM-056 — DOT dispatch equivalence.
//
// Tags: mechanism
// Bead ref: hk-bf85t (T-IMPL-008).
type NodeDispatchDecidedPayload struct {
	// RunID identifies the run for which the dispatch decision was made.
	// Required (must not be uuid.Nil).
	RunID RunID `json:"run_id"`

	// FromNodeID is the node from which the cascade ran. Required (non-empty).
	FromNodeID string `json:"from_node_id"`

	// NextNodeID is the selected next node. Populated when Advance is true;
	// empty when IsTerminal or Failed is true.
	NextNodeID string `json:"next_node_id,omitempty"`

	// IsTerminal is true when FromNodeID is in the workflow's terminal_node_ids
	// and the run has reached its terminal state.
	IsTerminal bool `json:"is_terminal,omitempty"`

	// Failed is true when the cascade produced no satisfiable match (WG-012 /
	// EM-046a) or the traversal cap was reached (EM-043).
	Failed bool `json:"failed,omitempty"`

	// FailureClass carries the cascade failure class when Failed is true.
	// "structural" for no matching edge (EM-046a); "compilation_loop" for cap hit (EM-043).
	FailureClass string `json:"failure_class,omitempty"`

	// FailureReason is the human-readable failure reason when Failed is true.
	FailureReason string `json:"failure_reason,omitempty"`

	// CompletionReason is "cap_hit" when the traversal cap was reached per the
	// EM-015d-RFD cap-hit vocabulary (execution-model.md §4.3.EM-015e).
	CompletionReason string `json:"completion_reason,omitempty"`
}

// Valid reports whether p is a well-formed NodeDispatchDecidedPayload.
//
// Rules:
//   - RunID must not be uuid.Nil.
//   - FromNodeID must be non-empty.
//   - Exactly one of NextNodeID (non-empty), IsTerminal, or Failed must be true.
func (p NodeDispatchDecidedPayload) Valid() bool {
	if uuid.UUID(p.RunID) == uuid.Nil {
		return false
	}
	if p.FromNodeID == "" {
		return false
	}
	// Exactly one outcome must hold.
	outcomes := 0
	if p.NextNodeID != "" {
		outcomes++
	}
	if p.IsTerminal {
		outcomes++
	}
	if p.Failed {
		outcomes++
	}
	return outcomes == 1
}

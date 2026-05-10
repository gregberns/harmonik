package core

import "github.com/google/uuid"

// NodeDispatchRequestedPayload is the typed event payload for the
// node_dispatch_requested event (event-model.md §8.1.11).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent
// Durability class: O (ordinary — observability and audit; the authoritative
// dispatch record lives in the run's state machine, not in this event).
//
// # Spec source (event-model.md §8.1.11)
//
// Emitted by daemon-core when a node dispatch is requested, immediately before
// the handler subprocess is spawned. The origin field discriminates whether the
// dispatch originated from normal workflow progression, a reconciliation action,
// or an explicit operator command.
//
// # Payload fields (event-model.md §8.1.11)
//
//   - run_id        — the run for which dispatch is requested (required)
//   - node_id       — the workflow node being dispatched (required)
//   - requested_at  — RFC 3339 wall-clock timestamp at dispatch request (required)
//   - origin        — dispatch origin discriminator: "workflow" | "reconciliation" | "operator"
type NodeDispatchRequestedPayload struct {
	// RunID is the run for which dispatch is requested. Required (must not be uuid.Nil).
	RunID RunID `json:"run_id"`

	// NodeID is the workflow node being dispatched. Required (non-empty).
	NodeID NodeID `json:"node_id"`

	// RequestedAt is the RFC 3339 wall-clock timestamp at the moment the dispatch
	// was requested. Required (non-empty). Kept as string; mirrors EndedAt
	// rationale in RunCompletedPayload.
	RequestedAt string `json:"requested_at"`

	// Origin is the dispatch origin discriminator per event-model.md §8.1.11.
	// One of: "workflow", "reconciliation", "operator". Required (non-empty).
	Origin NodeDispatchOrigin `json:"origin"`
}

// NodeDispatchOrigin is the typed discriminator for the origin field of a
// node_dispatch_requested event (event-model.md §8.1.11).
//
// The three values are:
//   - NodeDispatchOriginWorkflow:       normal workflow progression dispatch
//   - NodeDispatchOriginReconciliation: dispatch triggered by a reconciliation action
//   - NodeDispatchOriginOperator:       dispatch triggered by an explicit operator command
type NodeDispatchOrigin string

const (
	// NodeDispatchOriginWorkflow is emitted when the dispatch follows from the
	// workflow state machine's normal edge-selection and transition rules.
	NodeDispatchOriginWorkflow NodeDispatchOrigin = "workflow"

	// NodeDispatchOriginReconciliation is emitted when the dispatch is triggered
	// by a reconciliation action (e.g., re-dispatch after detecting a missed
	// checkpoint per reconciliation/spec.md §4.3).
	NodeDispatchOriginReconciliation NodeDispatchOrigin = "reconciliation"

	// NodeDispatchOriginOperator is emitted when the dispatch is triggered by an
	// explicit operator command (e.g., manual re-trigger via operator-nfr.md §4.3).
	NodeDispatchOriginOperator NodeDispatchOrigin = "operator"
)

// Valid reports whether o is one of the three declared NodeDispatchOrigin constants.
func (o NodeDispatchOrigin) Valid() bool {
	switch o {
	case NodeDispatchOriginWorkflow, NodeDispatchOriginReconciliation, NodeDispatchOriginOperator:
		return true
	default:
		return false
	}
}

// Valid reports whether p is a well-formed NodeDispatchRequestedPayload.
//
// Rules per event-model.md §8.1.11:
//   - RunID must not be uuid.Nil.
//   - NodeID must be non-empty.
//   - RequestedAt must be non-empty.
//   - Origin must be a valid NodeDispatchOrigin constant.
func (p NodeDispatchRequestedPayload) Valid() bool {
	if uuid.UUID(p.RunID) == uuid.Nil {
		return false
	}
	if p.NodeID == "" {
		return false
	}
	if p.RequestedAt == "" {
		return false
	}
	if !p.Origin.Valid() {
		return false
	}
	return true
}

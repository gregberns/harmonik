package core

import "github.com/google/uuid"

// GatePendingRecord is the in-memory record that the daemon maintains while a
// run is in the gate-pending sub-state of `running`
// (execution-model.md §4.10.EM-042a).
//
// When [DispatchEdge] returns [DispatchOutcome.Stay] == true (gate denial),
// the daemon MUST construct a GatePendingRecord and park the run. While parked:
//
//   - The daemon MUST NOT re-dispatch the source node.
//   - The daemon MUST NOT re-run the cascade against the same context and
//     outcome (the gate is a deterministic function; repeating would loop).
//   - The daemon MUST wait for a [GateResolutionSignal] before re-evaluating.
//
// On receipt of a gate-resolution signal (context-change, operator-override,
// or timeout), the daemon re-evaluates the cascade. If the gate now permits,
// the run advances normally. If the gate still denies and the signal was
// [GateResolutionSignalTimeout], the run fails with failure class `structural`
// per execution-model.md §8.2.
//
// # ContextHash and OutcomeHash
//
// ContextHash and OutcomeHash record the SHA-256 hex digests of the run context
// and outcome at the moment of gate denial. The daemon uses these digests to
// verify that a context-change signal has actually modified the context before
// re-evaluating, and to detect stale context-change signals (a signal carrying
// an identical context hash MUST NOT trigger re-evaluation — the gate would
// still deny).
//
// Both hashes are plain strings; callers MUST format them as 64-character
// lowercase hex strings per the SHA-256 digest encoding convention used
// throughout this package (InputEnvelopeHash in GateVerdictRecord,
// model_response_digest in CognitionMeta).
//
// # EnteredAt
//
// EnteredAt is the RFC 3339 timestamp at which the run entered gate-pending.
// It is used by the daemon to compute elapsed time against the gate's
// per-policy timeout. Kept as string to mirror the ProducedAt rationale in
// GateVerdictRecord and HookVerdictRecord (no silent timezone normalization
// on round-trip).
//
// # GatePendingRecord is not persisted to git
//
// GatePendingRecord is a daemon-memory artifact, not a durable git artifact.
// It is reconstructed on restart from the run's last durable checkpoint
// (which records that the run is still in the source state) combined with the
// emitted gate_denied event payload per event-model.md §8.2. There is no
// separate ".harmonik/gate-pending/<run_id>.json" file at MVH.
type GatePendingRecord struct {
	// RunID identifies the parked run.
	// Required (must not be zero UUID).
	RunID RunID `json:"run_id"`

	// GateName is the ControlPoint name of the gate that denied the transition.
	// Required (non-empty). Matches the registered ControlPoint.Name field.
	GateName string `json:"gate_name"`

	// SourceNode is the NodeID of the node the run is parked on.
	// The daemon MUST NOT re-dispatch this node until the gate resolves.
	// Required (non-empty).
	SourceNode NodeID `json:"source_node"`

	// DeniedEdge is the edge the gate denied. The daemon uses this to
	// re-present the same candidate to the cascade after gate resolution,
	// rather than re-running the full cascade from scratch.
	// Required; must satisfy Edge.Valid().
	DeniedEdge Edge `json:"denied_edge"`

	// ContextHash is the SHA-256 hex digest of the run context at the moment
	// of gate denial. Required (64-character lowercase hex string, non-empty).
	// The daemon uses this to detect whether a context-change signal has
	// actually changed the context before re-evaluating.
	ContextHash string `json:"context_hash"`

	// OutcomeHash is the SHA-256 hex digest of the outcome at the moment of
	// gate denial. Required (64-character lowercase hex string, non-empty).
	// The daemon uses this to prevent re-evaluation against an unchanged outcome.
	OutcomeHash string `json:"outcome_hash"`

	// EnteredAt is the RFC 3339 timestamp at which the run entered gate-pending.
	// Required (non-empty). Kept as string; see type-decision note above.
	EnteredAt string `json:"entered_at"`
}

// Valid reports whether r is a well-formed GatePendingRecord.
//
// Rules per execution-model.md §4.10.EM-042a:
//   - RunID must not be the zero UUID.
//   - GateName must be non-empty.
//   - SourceNode must be non-empty.
//   - DeniedEdge must satisfy Edge.Valid().
//   - ContextHash must be non-empty.
//   - OutcomeHash must be non-empty.
//   - EnteredAt must be non-empty.
func (r GatePendingRecord) Valid() bool {
	if uuid.UUID(r.RunID) == uuid.Nil {
		return false
	}
	if r.GateName == "" {
		return false
	}
	if r.SourceNode == "" {
		return false
	}
	if !r.DeniedEdge.Valid() {
		return false
	}
	if r.ContextHash == "" {
		return false
	}
	if r.OutcomeHash == "" {
		return false
	}
	if r.EnteredAt == "" {
		return false
	}
	return true
}

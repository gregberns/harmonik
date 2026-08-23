package core

// AgentInputAckedPayload is the typed event payload for agent_input_acked
// (event-model.md §8.21.1).
//
// Emitted by the input driver at the input-acceptance boundary of a submission.
// The event's existence IS the positive ack (COORD c019 — no acceptance
// "class"). A protocol-level refusal is the synchronous Ack{Rejected} return of
// SubmitInput (structured driver only), NOT an agent_input_acked variant; the
// never-confirmed case is the distinct agent_input_stale timeout terminal.
//
// Idempotent on (RunID, InputSeq): a re-emitted ack for the same submission is a
// reactor no-op. InputSeq is the monotonic submission sequence and the
// idempotency key.
//
// Durability class: O (observational).
type AgentInputAckedPayload struct {
	// RunID is the run whose input submission was accepted. REQUIRED; Valid()
	// rejects empty.
	RunID string `json:"run_id"`

	// InputSeq is the monotonic submission sequence — the idempotency key.
	// REQUIRED; Valid() rejects negative.
	InputSeq int64 `json:"input_seq"`

	// AcceptanceRef is the protocol turn id the driver acknowledged (the wire
	// form of Ack.Token; serialized as acceptance_token). The Go field is NOT
	// named "*Token" to satisfy the EV-036 secret-prefix rule, following the
	// AckRef precedent in eventpayloads_u3q6o.go. Optional.
	AcceptanceRef string `json:"acceptance_token,omitempty"`

	// SessionID is the session identity at the acceptance boundary. Optional.
	SessionID string `json:"session_id,omitempty"`

	// AckedAt is the RFC3339 wall-clock at the acceptance boundary.
	AckedAt string `json:"acked_at"`
}

// AgentInputStalePayload is the typed event payload for agent_input_stale
// (event-model.md §8.21.2).
//
// Emitted by the input driver when the InputAckTimeout window elapses with no
// agent_input_acked for the same (RunID, InputSeq) — the never-confirmed wedge /
// resume-hang fix (agent-input.md §5 AIS-INV-001). Non-idempotent: a distinct
// timeout boundary, not keyed for dedupe.
//
// Durability class: O (observational).
type AgentInputStalePayload struct {
	// RunID is the run whose input submission timed out. REQUIRED; Valid()
	// rejects empty.
	RunID string `json:"run_id"`

	// InputSeq is the submission whose acceptance window elapsed. REQUIRED;
	// Valid() rejects negative.
	InputSeq int64 `json:"input_seq"`

	// SessionID is the session identity at window expiry. Optional.
	SessionID string `json:"session_id,omitempty"`

	// TimedOutAt is the RFC3339 wall-clock at window expiry. REQUIRED; Valid()
	// rejects empty.
	TimedOutAt string `json:"timed_out_at"`

	// Window is the InputAckTimeout + overhead bound that elapsed (e.g. "30s").
	Window string `json:"window,omitempty"`
}

// Valid reports whether p is a well-formed AgentInputAckedPayload: non-empty
// run_id and input_seq >= 0 (event-model.md §8.21.1).
//
// Valid() is NOT wired into DecodePayload (EventPayload is an empty marker
// interface); it is exercised by the roundtrip tests and the replay harness
// explicitly, following the §8.20 keeper-interior precedent.
func (p AgentInputAckedPayload) Valid() bool {
	return p.RunID != "" && p.InputSeq >= 0
}

// Valid reports whether p is a well-formed AgentInputStalePayload: non-empty
// run_id, input_seq >= 0, and non-empty timed_out_at (event-model.md §8.21.2).
func (p AgentInputStalePayload) Valid() bool {
	return p.RunID != "" && p.InputSeq >= 0 && p.TimedOutAt != ""
}

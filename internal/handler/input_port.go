// Package handler — the consumer-owned InputPort seam (AIS-001 / HC-069).
//
// InputPort is the typed input verb the process-spawn seam exposes so the daemon
// can deliver input to a spawned agent and receive a first-class acceptance
// signal (the Ack), replacing the six type-asserted paste-inject side-interfaces
// and the no-op SubstrateSession stdin stubs.
//
// The port is DECLARED handler-side and SUPPLIED daemon-side (the depguard
// inversion of HC-071 / AIS-002): the concrete input driver — the interim
// tmux/paste implementation today, the structured Codex app-server driver later —
// lives outside internal/handler so that internal/handler never imports
// internal/lifecycle/tmux. A SubstrateSession MAY satisfy InputPort; the daemon
// obtains it by a structural type assertion at the process-spawn seam
// (see AsInputPort).
//
// Spec refs: specs/agent-input.md §4.1 AIS-001, §4.2 AIS-003/004, §6.1, §6.2;
// specs/handler-contract.md §4.1a HC-069/HC-070/HC-071, §5 HC-INV-008.
package handler

import (
	"context"
	"fmt"
)

// InputPort is the consumer-owned narrow input port (the RS-004 port idiom:
// two methods, no Result/Option/Either container in its surface). A
// SubstrateSession MAY satisfy it structurally; the daemon obtains it by a
// structural assertion at the process-spawn seam (AIS-001, HC-069).
type InputPort interface {
	// SubmitInput delivers ONE input submission to the running agent and MUST
	// block until the input reaches exactly one terminal — a returned Ack
	// (Delivered or Rejected) or an emitted agent_input_stale-class event —
	// within the bounded window of HC-INV-008 / AIS-INV-001. It MUST NOT return
	// silently in flight.
	//
	// The interim tmux/paste implementation returns Ack{Delivered} for a
	// successful write; its positive acceptance is confirmed ASYNCHRONOUSLY by
	// the Claude-hook-bridge signal (outcome_emitted / agent_ready), never by a
	// capture-pane scrape. It MUST NOT synthesize a positive acceptance it did
	// not observe. Rejected is returned only on a protocol-level refusal, which
	// the tmux/paste path cannot produce (AIS-003, HC-070).
	SubmitInput(ctx context.Context, req InputRequest) (Ack, error)

	// CloseInput signals end-of-input. It replaces and retires the no-op
	// substrateSessionAdapter.CloseStdin (AIS-001, HC-069).
	CloseInput(ctx context.Context) error
}

// InputRequest carries one input submission for InputPort.SubmitInput. The full
// field schema is owned by specs/agent-input.md §6.2.
type InputRequest struct {
	// Payload is the input bytes to deliver to the agent.
	Payload []byte
	// TurnIntent distinguishes a new-turn submission from a steer/interrupt
	// intent (e.g. "new-turn", "steer"); empty for the default new-turn case.
	TurnIntent string `json:"turn_intent,omitempty"`
}

// DeliveryOutcome is the binary delivery outcome an Ack carries (AIS-003a,
// HC-070). It is NOT an acceptance "class" or capability tier — the two input
// methods (tmux paste-driven for Claude; the structured Codex app-server driver)
// are PEERS. Positive acceptance is NOT a DeliveryOutcome value; it is the async
// agent_input_acked event (its existence IS the ack).
type DeliveryOutcome int

const (
	// Delivered — the input was handed to the driver; the acceptance verdict
	// arrives asynchronously as the agent_input_acked event.
	Delivered DeliveryOutcome = iota
	// Rejected — a protocol-level refusal (structured drivers only; the tmux
	// paste path cannot produce one).
	Rejected
)

// String renders a DeliveryOutcome for logs and events.
func (o DeliveryOutcome) String() string {
	switch o {
	case Delivered:
		return "delivered"
	case Rejected:
		return "rejected"
	default:
		return fmt.Sprintf("DeliveryOutcome(%d)", int(o))
	}
}

// Ack is the SYNCHRONOUS delivery-handoff result of SubmitInput. It carries
// exactly three fields (AIS-003, HC-070, specs/agent-input.md §6.2). Positive
// acceptance is decoupled from Ack and travels as the async agent_input_acked
// event; the never-confirmed case reaches the distinct agent_input_stale
// terminal (HC-INV-008 / AIS-INV-001).
type Ack struct {
	// Outcome is the delivery outcome (Delivered | Rejected). No acceptance
	// "class", no capability hierarchy (AIS-003a).
	Outcome DeliveryOutcome
	// Seq is the driver-internal monotonic input sequence id, codec-owned per
	// RS-008 (AIS-003b). Its serialized wire form is input_seq (event-model §6.3).
	Seq uint64 `json:"input_seq,omitempty"`
	// Token is the OPTIONAL protocol-level acceptance token — the turn id the
	// input opened, when the wire protocol supplies one; empty otherwise
	// (AIS-003c). Its serialized wire form is acceptance_token.
	Token string `json:"token,omitempty"`
}

// ErrInputUnsupported is returned at the seam when a session does not satisfy
// InputPort (HC-069: "ErrDeterministic(\"input unsupported\") at the seam when
// the session does not satisfy InputPort"). It wraps ErrDeterministic — retrying
// the same plan against a non-input-capable session is futile.
var ErrInputUnsupported = fmt.Errorf("handler: input unsupported: %w", ErrDeterministic)

// AsInputPort is the structural-assertion seam by which the daemon obtains the
// InputPort from a SubstrateSession without internal/handler knowing the
// concrete driver type (the HC-071 / AIS-002 depguard inversion: handler
// DECLARES the port, the daemon SUPPLIES the driver). It reports (nil, false)
// when the session does not satisfy InputPort.
func AsInputPort(sess SubstrateSession) (InputPort, bool) {
	ip, ok := sess.(InputPort)
	return ip, ok
}

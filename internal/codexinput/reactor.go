// Package codexinput implements the INPUT-direction reactor for the structured
// Codex app-server driver (agent-input-substrate M2, T5+T4). It is the pure
// state machine that carries the input-submission lifecycle and the
// bounded-liveness (output-or-stale) guarantee of AIS-INV-001 / HC-INV-008.
//
// # Relationship to codexreactor
//
// The sibling internal/codexreactor is the OUTPUT-direction reactor: it maps an
// already-running codex session's server notifications (turn/started, deltas,
// turn/completed, token usage) into harmonik-side output actions. codexinput is
// its INPUT-direction peer: it models the FULL driver lifecycle a submission
// travels through — Spawning → Handshaking → Ready → AwaitingAck → InTurn →
// {Draining|Exited} — and owns the ack-anchor and stale-timeout terminals.
// The two are kept as separate reactors because their invariants differ (the
// output reactor's dedup-by-seq / one-turn-in-flight vs the input reactor's
// bounded-liveness front-stop), so neither muddies the other.
//
// # The codec
//
// The codex wire codec (internal/codexwire) already models every frame this
// driver reads and writes — the input framing (initialize handshake, turn/start,
// turn/started, */delta, turn/completed) parses to known methods with Extra
// passthrough and zero FrameKindRaw on the modeled set, proven by
// codexwire's TestCorpusRoundTrip. codexinput therefore adds no codec entries;
// it consumes already-decoded, codec-independent Events so the pure Step never
// touches JSON or IO. The concrete driver (T6) bridges codexwire frames and the
// codec's ErrorEvent/DisconnectEvent terminals (RS-009) into these Events.
//
// # Bounded liveness (AIS-INV-001 / HC-INV-008, T4)
//
// Every input submission reaches EXACTLY one terminal — an Ack (Delivered via
// the async agent_input_acked, or Rejected) OR an emitted agent_input_stale —
// within InputAckTimeout + injection overhead; silence is FORBIDDEN.
// Structurally: every TimerFired edge in Step lands in a state with an outgoing
// action, so the machine cannot wedge silently. The window is measured via the
// substrate ClockPort (never wall-clock): the Step emits ArmTimer/CancelTimer
// Actions carrying durations, and the driver translates them into ClockPort
// sleeps that feed TimerFired Events back in — verifiable under FakeClock
// (reactor_test.go). The handshake edge is bounded the same way (no handshake
// within the bound → agent_launch_failure, AIS-017 fast-fail; never a silent
// exit-0).
//
// # Wire
//
// EventSource → Event channel → Reactor.Step → []Action → Effector.Execute
//
// Spec refs: specs/agent-input.md §6.2, §6.3, §7.1, §7.2, §8, AIS-006,
// AIS-INV-001; specs/handler-contract.md HC-INV-008; specs/replay-substrate.md
// §4.6 RS-015 (ClockPort), §4.9 RS-021 (type-alias re-instantiation).
package codexinput

import (
	"context"
	"time"

	"github.com/gregberns/harmonik/internal/substrate"
)

// ─── Emitted event names (durable bus events; owned by event-model §8.21) ─────

// EmitType is the cross-bus event name an ActionTypeEmit forwards. The payload
// STRUCTURES are owned by event-model §8.21 / §6.3; this reactor owns the WHEN.
type EmitType string

const (
	// EmitInputSubmitted marks a submission handed to the driver (front-stop
	// entry; AIS-INV-001 timer armed alongside it).
	EmitInputSubmitted EmitType = "agent_input_submitted"
	// EmitInputAcked is the async POSITIVE acceptance — its existence IS the
	// ack; carries input_seq and acceptance_token (the turn id). No class.
	EmitInputAcked EmitType = "agent_input_acked"
	// EmitInputStale is the timeout / transport-terminal terminal — the
	// resume-hang fix (AIS-INV-001). Carries input_seq and an optional reason.
	EmitInputStale EmitType = "agent_input_stale"
	// EmitLaunchFailure is the handshake fast-fail terminal (AIS-017): no
	// handshake within the bound, or a transport terminal before Ready.
	EmitLaunchFailure EmitType = "agent_launch_failure"
)

// ─── Timer kinds ──────────────────────────────────────────────────────────────

// TimerKind names a bounded-liveness timer the Step arms. Every kind has a
// TimerFired edge that lands in a state with an outgoing action (AIS-INV-001).
type TimerKind string

const (
	// TimerHandshake bounds the initialize handshake (AIS-017 fast-fail).
	TimerHandshake TimerKind = "handshake_timeout"
	// TimerInputAck bounds one input submission's acceptance (AIS-INV-001).
	TimerInputAck TimerKind = "input_ack_timeout"
)

// ─── Driver states ────────────────────────────────────────────────────────────

// DriverState is the input reactor's lifecycle state (spec §7.2, extended with
// the spawn/handshake/drain lifecycle the driver needs).
type DriverState int

const (
	// Spawning — process not yet observed up; awaiting Spawned.
	Spawning DriverState = iota
	// Handshaking — initialize sent, awaiting HandshakeOK (TimerHandshake armed).
	Handshaking
	// Ready — handshake complete, idle, no submission in flight.
	Ready
	// AwaitingAck — a submission is in flight (TimerInputAck armed); the
	// bounded-liveness-bearing state (spec §7.2).
	AwaitingAck
	// InTurn — the submission was acked and its turn is open (deltas stream).
	InTurn
	// Draining — CloseInput requested; awaiting turn/process wind-down.
	Draining
	// Exited — terminal; further events are dropped.
	Exited
)

// String renders a DriverState for logs and test failures.
func (s DriverState) String() string {
	switch s {
	case Spawning:
		return "spawning"
	case Handshaking:
		return "handshaking"
	case Ready:
		return "ready"
	case AwaitingAck:
		return "awaiting_ack"
	case InTurn:
		return "in_turn"
	case Draining:
		return "draining"
	case Exited:
		return "exited"
	default:
		return "DriverState(?)"
	}
}

// ─── Event ────────────────────────────────────────────────────────────────────

// EventType classifies a typed input-direction event fed to the reactor.
type EventType string

const (
	// EventTypeSpawned — the child process is up; drives the handshake.
	EventTypeSpawned EventType = "spawned"
	// EventTypeHandshakeOK — the initialize handshake completed (input support
	// confirmed).
	EventTypeHandshakeOK EventType = "handshake_ok"

	// EventTypeInputSubmitted — the consumer submitted one input (InputSeq).
	EventTypeInputSubmitted EventType = "input_submitted"
	// EventTypeInputAcked — the wire observed a positive ack for InputSeq; TurnID
	// is the acceptance token the turn opened.
	EventTypeInputAcked EventType = "input_acked"
	// EventTypeInputRejected — a protocol-level refusal of InputSeq (structured
	// driver only; resolves a synchronous Ack{Rejected}, no positive event).
	EventTypeInputRejected EventType = "input_rejected"

	// EventTypeTurnCompleted — the open turn reached its terminal.
	EventTypeTurnCompleted EventType = "turn_completed"
	// EventTypeDelta — an assistant output delta for the open turn (routed to the
	// capture tee; no lifecycle transition).
	EventTypeDelta EventType = "delta"

	// EventTypeCloseRequested — the consumer signalled end-of-input (CloseInput).
	EventTypeCloseRequested EventType = "close_requested"

	// EventTypeTimerFired — a previously-armed timer elapsed (Kind).
	EventTypeTimerFired EventType = "timer_fired"

	// EventTypeError — the codec's ErrorEvent (fatal decode/transport). Not a new
	// substrate fault mode (RS-009).
	EventTypeError EventType = "error"
	// EventTypeDisconnected — the codec's DisconnectEvent (child stdout closed).
	EventTypeDisconnected EventType = "disconnected"
)

// Event is a flat, omitempty, JSON-round-trippable typed event. Which fields are
// populated depends on Type; the flat shape lets scenario files round-trip
// without type-switching (mirrors codexreactor.Event).
type Event struct {
	Type EventType `json:"type"`

	// InputSeq is the driver-internal monotonic input sequence id (RS-008,
	// AIS-003b) an InputSubmitted/InputAcked/InputRejected carries.
	InputSeq uint64 `json:"input_seq,omitempty"`
	// TurnID is the acceptance token an InputAcked carries (AIS-003c), and the
	// open turn a TurnCompleted/Delta refers to.
	TurnID string `json:"turn_id,omitempty"`
	// Kind is the fired timer's kind on a TimerFired event.
	Kind TimerKind `json:"kind,omitempty"`
	// Reason carries a rejection reason or a transport-error message.
	Reason string `json:"reason,omitempty"`
}

// ─── Action ───────────────────────────────────────────────────────────────────

// ActionType classifies a side-effect request produced by the reactor.
type ActionType string

const (
	// ActionTypeSendHandshake — write the initialize handshake to the child.
	ActionTypeSendHandshake ActionType = "send_handshake"
	// ActionTypeWriteInput — write one submission's payload to the child stdin
	// (the turn/start input frame). Carries InputSeq.
	ActionTypeWriteInput ActionType = "write_input"
	// ActionTypeCloseInput — close the child's stdin (end-of-input).
	ActionTypeCloseInput ActionType = "close_input"
	// ActionTypeInterrupt — gracefully interrupt the open turn (turn/interrupt),
	// used on close mid-turn (AIS-017 graceful shutdown). Carries TurnID.
	ActionTypeInterrupt ActionType = "interrupt"

	// ActionTypeArmTimer — arm a bounded-liveness timer measured via ClockPort
	// (RS-015). Carries Kind + Duration.
	ActionTypeArmTimer ActionType = "arm_timer"
	// ActionTypeCancelTimer — cancel a previously-armed timer. Carries Kind.
	ActionTypeCancelTimer ActionType = "cancel_timer"

	// ActionTypeEmit — emit a durable cross-bus event (Emit + fields). The
	// dual-delivery obligation of AIS-004: the sync Ack travels via the driver's
	// return; this is the async event peer.
	ActionTypeEmit ActionType = "emit"
)

// Action is a flat, omitempty side-effect request (mirrors codexreactor.Action).
type Action struct {
	Type ActionType `json:"type"`

	// InputSeq — the submission a WriteInput / Emit refers to.
	InputSeq uint64 `json:"input_seq,omitempty"`
	// Payload — the input bytes a WriteInput delivers.
	Payload []byte `json:"payload,omitempty"`
	// TurnID — the turn an Interrupt targets, and the acceptance token an
	// Emit(agent_input_acked) forwards.
	TurnID string `json:"turn_id,omitempty"`
	// Kind — the timer an ArmTimer/CancelTimer names.
	Kind TimerKind `json:"kind,omitempty"`
	// Duration — the window an ArmTimer measures (ClockPort virtual time).
	Duration time.Duration `json:"duration,omitempty"`
	// Emit — the cross-bus event name an ActionTypeEmit forwards.
	Emit EmitType `json:"emit,omitempty"`
	// Reason — a stale/reject reason carried on an Emit.
	Reason string `json:"reason,omitempty"`
}

// ─── Seam aliases (RS-021 type-alias re-instantiation) ────────────────────────

// Effector is the codex-input instantiation of the generic substrate effector
// (substrate.Effector[Action]). It is a type ALIAS (=), not a defined type, so
// the generic substrate doubles satisfy it unchanged and existing/parallel call
// sites keep compiling (RS-021; substrate-design §2.1).
type Effector = substrate.Effector[Action]

// EventSource is the codex-input instantiation of the generic substrate event
// source (substrate.EventSource[Event]). It is a type ALIAS (=) for the same
// RS-021 reason as Effector above.
type EventSource = substrate.EventSource[Event]

// ─── Config ───────────────────────────────────────────────────────────────────

// Config holds the bounded-liveness windows the Step arms (measured via
// ClockPort by the driver; never wall-clock). Zero values fall back to the
// defaults so a zero-value Reactor is usable.
type Config struct {
	// HandshakeTimeout bounds the initialize handshake (AIS-017 fast-fail).
	HandshakeTimeout time.Duration
	// InputAckTimeout bounds one submission's acceptance (AIS-INV-001).
	InputAckTimeout time.Duration
}

const (
	defaultHandshakeTimeout = 30 * time.Second
	defaultInputAckTimeout  = 60 * time.Second
)

func (c Config) handshakeTimeout() time.Duration {
	if c.HandshakeTimeout > 0 {
		return c.HandshakeTimeout
	}
	return defaultHandshakeTimeout
}

func (c Config) inputAckTimeout() time.Duration {
	if c.InputAckTimeout > 0 {
		return c.InputAckTimeout
	}
	return defaultInputAckTimeout
}

// ─── State ────────────────────────────────────────────────────────────────────

// State is the reactor's mutable state, inspectable between Steps in tests.
type State struct {
	// Phase is the current lifecycle state.
	Phase DriverState
	// PendingSeq is the input seq currently AwaitingAck (0 when none). It anchors
	// the ack/stale terminal to the right submission (RS-008).
	PendingSeq uint64
	// TurnID is the acceptance token of the open turn (set on InputAcked, cleared
	// on TurnCompleted).
	TurnID string
}

// ─── Reactor ──────────────────────────────────────────────────────────────────

// Reactor is the pure input-direction state machine. Step is deterministic given
// (state, event) with no IO, no goroutines, no clock reads. Construct with New.
type Reactor struct {
	cfg   Config
	state State
}

// New constructs a Reactor in the Spawning state with cfg's windows.
func New(cfg Config) *Reactor { return &Reactor{cfg: cfg} }

// State returns the current reactor state. Safe to call between Steps.
func (r *Reactor) State() State { return r.state }

// Step processes one event, updates state, and returns the actions to execute.
//
// Purity: no IO, no goroutines, no clock reads, no allocations beyond the
// returned slice. Returns nil (not an empty slice) when there is no action.
//
// Bounded liveness (AIS-INV-001 / HC-INV-008): every TimerFired edge below
// lands in a state with an outgoing action, and every path out of AwaitingAck
// resolves the submission to exactly one terminal (acked, rejected, or stale) —
// no path leaves a submission pending with no terminal.
func (r *Reactor) Step(ev Event) []Action {
	if r.state.Phase == Exited {
		return nil // terminal; drop.
	}

	switch ev.Type {
	case EventTypeSpawned:
		return r.stepSpawned()
	case EventTypeHandshakeOK:
		return r.stepHandshakeOK()
	case EventTypeInputSubmitted:
		return r.stepInputSubmitted(ev)
	case EventTypeInputAcked:
		return r.stepInputAcked(ev)
	case EventTypeInputRejected:
		return r.stepInputRejected(ev)
	case EventTypeTimerFired:
		return r.stepTimerFired(ev)
	case EventTypeTurnCompleted:
		return r.stepTurnCompleted()
	case EventTypeDelta:
		return nil // capture-tee routed; no lifecycle transition.
	case EventTypeCloseRequested:
		return r.stepClose()
	case EventTypeError:
		return r.stepTransportTerminal(ev.Reason)
	case EventTypeDisconnected:
		return r.stepTransportTerminal("disconnected")
	}
	return nil // unknown event type: drop (forward-compat).
}

func (r *Reactor) stepSpawned() []Action {
	if r.state.Phase != Spawning {
		return nil
	}
	r.state.Phase = Handshaking
	return []Action{
		{Type: ActionTypeSendHandshake},
		{Type: ActionTypeArmTimer, Kind: TimerHandshake, Duration: r.cfg.handshakeTimeout()},
	}
}

func (r *Reactor) stepHandshakeOK() []Action {
	if r.state.Phase != Handshaking {
		return nil
	}
	r.state.Phase = Ready
	return []Action{{Type: ActionTypeCancelTimer, Kind: TimerHandshake}}
}

func (r *Reactor) stepInputSubmitted(ev Event) []Action {
	// A submission is accepted only from Ready or InTurn (mid-turn steer).
	// From any other phase it cannot be delivered; drop (the driver's port
	// front-stop rejects before submitting in those phases).
	if r.state.Phase != Ready && r.state.Phase != InTurn {
		return nil
	}
	r.state.Phase = AwaitingAck
	r.state.PendingSeq = ev.InputSeq
	return []Action{
		{Type: ActionTypeWriteInput, InputSeq: ev.InputSeq, Payload: nil},
		{Type: ActionTypeEmit, Emit: EmitInputSubmitted, InputSeq: ev.InputSeq},
		{Type: ActionTypeArmTimer, Kind: TimerInputAck, Duration: r.cfg.inputAckTimeout()},
	}
}

func (r *Reactor) stepInputAcked(ev Event) []Action {
	if r.state.Phase != AwaitingAck || ev.InputSeq != r.state.PendingSeq {
		return nil // stale/mismatched ack; drop.
	}
	seq := r.state.PendingSeq
	r.state.PendingSeq = 0
	r.state.TurnID = ev.TurnID
	r.state.Phase = InTurn
	// The positive ack: its existence IS the acceptance (no class). Token is
	// the turn id the input opened (AIS-003c).
	return []Action{
		{Type: ActionTypeCancelTimer, Kind: TimerInputAck},
		{Type: ActionTypeEmit, Emit: EmitInputAcked, InputSeq: seq, TurnID: ev.TurnID},
	}
}

func (r *Reactor) stepInputRejected(ev Event) []Action {
	if r.state.Phase != AwaitingAck || ev.InputSeq != r.state.PendingSeq {
		return nil
	}
	// Resolves the synchronous Ack{Rejected}; no positive event (spec §7.2).
	r.state.PendingSeq = 0
	r.state.Phase = Ready
	return []Action{{Type: ActionTypeCancelTimer, Kind: TimerInputAck}}
}

func (r *Reactor) stepTurnCompleted() []Action {
	if r.state.Phase == InTurn {
		r.state.TurnID = ""
		r.state.Phase = Ready
	}
	return nil // output-side routing lives in codexreactor; no input action.
}

// stepTimerFired handles the bounded-liveness timer edges. EVERY branch lands in
// a state with an outgoing action — the structural AIS-INV-001 guarantee.
func (r *Reactor) stepTimerFired(ev Event) []Action {
	switch ev.Kind {
	case TimerHandshake:
		if r.state.Phase != Handshaking {
			return nil // handshake already resolved; the fire is stale.
		}
		// Fast-fail: no handshake within the bound (AIS-017). Terminal, not a
		// silent exit-0.
		r.state.Phase = Exited
		return []Action{{Type: ActionTypeEmit, Emit: EmitLaunchFailure, Reason: "handshake timeout"}}

	case TimerInputAck:
		if r.state.Phase != AwaitingAck {
			return nil // already acked/rejected; the fire is stale.
		}
		// The resume-hang fix: a missing ack in-bound becomes a recoverable
		// terminal (agent_input_stale), never silence (AIS-INV-001).
		seq := r.state.PendingSeq
		r.state.PendingSeq = 0
		r.state.Phase = Ready
		return []Action{{Type: ActionTypeEmit, Emit: EmitInputStale, InputSeq: seq, Reason: "input_ack_timeout"}}
	}
	return nil
}

// stepClose handles CloseInput: gracefully interrupt an open turn (AIS-017),
// then close stdin and drain.
func (r *Reactor) stepClose() []Action {
	switch r.state.Phase {
	case Exited, Draining:
		return nil
	case InTurn:
		turn := r.state.TurnID
		r.state.Phase = Draining
		return []Action{
			{Type: ActionTypeInterrupt, TurnID: turn},
			{Type: ActionTypeCloseInput},
		}
	case AwaitingAck:
		// A submission is still pending its terminal. Once we enter Draining the
		// resolving edges (TimerInputAck/InputAcked/InputRejected) are all guarded
		// on Phase==AwaitingAck and would drop silently, so resolve the pending
		// seq to agent_input_stale HERE — never leave a SubmitInput without a
		// terminal (AIS-INV-001; silence is forbidden).
		seq := r.state.PendingSeq
		r.state.PendingSeq = 0
		r.state.Phase = Draining
		return []Action{
			{Type: ActionTypeCancelTimer, Kind: TimerInputAck},
			{Type: ActionTypeEmit, Emit: EmitInputStale, InputSeq: seq, Reason: "close_while_awaiting_ack"},
			{Type: ActionTypeCloseInput},
		}
	default:
		r.state.Phase = Draining
		return []Action{{Type: ActionTypeCloseInput}}
	}
}

// stepTransportTerminal handles the codec's ErrorEvent/DisconnectEvent (RS-009).
// If a submission is pending, it MUST resolve to agent_input_stale (never
// silence, AIS-INV-001); before Ready it is a launch failure (AIS-017).
func (r *Reactor) stepTransportTerminal(reason string) []Action {
	switch r.state.Phase {
	case Spawning, Handshaking:
		r.state.Phase = Exited
		return []Action{{Type: ActionTypeEmit, Emit: EmitLaunchFailure, Reason: reason}}
	case AwaitingAck:
		seq := r.state.PendingSeq
		r.state.PendingSeq = 0
		r.state.Phase = Exited
		return []Action{
			{Type: ActionTypeCancelTimer, Kind: TimerInputAck},
			{Type: ActionTypeEmit, Emit: EmitInputStale, InputSeq: seq, Reason: reason},
		}
	default:
		// Ready / InTurn / Draining: no submission owes a terminal; wind down.
		r.state.Phase = Exited
		return nil
	}
}

// Run drives the reactor loop over the generic substrate.Run driver: reads
// Events from src, calls Step, executes the resulting Actions via eff. It is a
// one-line wrapper — Go forbids generic methods, so the canonical loop is the
// free function substrate.Run (RS-002/RS-021). r.Step is a bound method value of
// type func(Event) []Action, exactly substrate.Run's step parameter after the
// E=Event, A=Action instantiation, so no adapter is needed.
func (r *Reactor) Run(ctx context.Context, src EventSource, eff Effector) error {
	return substrate.Run(ctx, src, r.Step, eff)
}

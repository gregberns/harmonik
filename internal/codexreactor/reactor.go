// Package codexreactor implements the EventSource→Reactor→Effector seam for
// the codex app-server integration (codex-app-server T3, hk-5co9a).
//
// The reactor is the translation brain: it consumes typed Events fed by any
// EventSource (live/replay/synthetic) and produces Actions consumed by any
// Effector (real/fake). All side effects flow through the single Effector
// interface; the reactor itself is pure state.
//
// # Invariants
//
// I1 — one-turn-in-flight: the reactor tracks whether a codex turn is active
// (State.InFlight). It does NOT start a second turn while one is in-flight; a
// TurnStarted while InFlight=true supersedes the previous turn (rare; server
// guarantees ordering, but the state machine is defensive).
//
// I2 — dedup-by-seq: every Event carries a Seq counter. Events with Seq ≤
// State.LastSeq are silently dropped. Seq=0 bypasses dedup (used for
// connection-lifecycle events such as Connected and Disconnected).
//
// # Wire
//
// EventSource → Event channel → Reactor.Step → []Action → Effector.Execute
//
// Bead: hk-5co9a [codex-app-server T3]
package codexreactor

import "context"

// ─── Event types ─────────────────────────────────────────────────────────────

// EventType classifies a typed event fed to the reactor.
type EventType string

const (
	// Connection lifecycle. These events carry Seq=0 (bypass dedup).
	EventTypeConnected    EventType = "connected"
	EventTypeDisconnected EventType = "disconnected"

	// Turn lifecycle (server-side notifications from the codex app-server).
	EventTypeTurnStarted   EventType = "turn_started"
	EventTypeTurnCompleted EventType = "turn_completed"

	// Content streaming.
	EventTypeMessageDelta EventType = "message_delta"

	// Thread telemetry.
	EventTypeThreadStatus EventType = "thread_status"
	EventTypeTokenUsage   EventType = "token_usage"

	// Error reported by the server or transport layer.
	EventTypeError EventType = "error"
)

// Event is a typed event produced by an EventSource and consumed by the reactor.
//
// All fields are optional; which fields are populated depends on Type. The flat
// struct enables JSON round-trip for scenario files without type-switching.
//
// Seq is a monotonic sequence number scoped to one connection. Seq=0 disables
// dedup (used for connection lifecycle events). After a reconnect the source
// resets its counter; the reactor resets State.LastSeq on EventTypeConnected.
type Event struct {
	Seq           uint64    `json:"seq"`
	Type          EventType `json:"type"`
	ThreadID      string    `json:"thread_id,omitempty"`
	TurnID        string    `json:"turn_id,omitempty"`
	ItemID        string    `json:"item_id,omitempty"`
	Delta         string    `json:"delta,omitempty"`
	Status        string    `json:"status,omitempty"`
	TotalTokens   int64     `json:"total_tokens,omitempty"`
	ContextWindow int64     `json:"context_window,omitempty"`
	Message       string    `json:"message,omitempty"`
}

// ─── Action types ────────────────────────────────────────────────────────────

// ActionType classifies an action produced by the reactor for the effector.
type ActionType string

const (
	// ActionTypeCompleteTurn signals that the current codex turn has ended.
	// Effector should close out the turn on the harmonik side.
	ActionTypeCompleteTurn ActionType = "complete_turn"

	// ActionTypeEmitOutput forwards a streamed agent message delta.
	ActionTypeEmitOutput ActionType = "emit_output"

	// ActionTypeEmitError signals an error condition (server error or disconnect).
	ActionTypeEmitError ActionType = "emit_error"

	// ActionTypeNotifyStatus forwards a thread status change (active/idle).
	ActionTypeNotifyStatus ActionType = "notify_status"

	// ActionTypeNotifyTokenUsage forwards a token usage update.
	ActionTypeNotifyTokenUsage ActionType = "notify_token_usage"
)

// Action is a side-effect request produced by the reactor.
//
// Same flat design as Event: all fields optional, which fields are set depends
// on Type. Enables JSON comparison in scenario tests without type-switching.
type Action struct {
	Type          ActionType `json:"type"`
	ThreadID      string     `json:"thread_id,omitempty"`
	TurnID        string     `json:"turn_id,omitempty"`
	ItemID        string     `json:"item_id,omitempty"`
	Delta         string     `json:"delta,omitempty"`
	Status        string     `json:"status,omitempty"`
	TotalTokens   int64      `json:"total_tokens,omitempty"`
	ContextWindow int64      `json:"context_window,omitempty"`
	Message       string     `json:"message,omitempty"`
}

// ─── Effector ────────────────────────────────────────────────────────────────

// Effector executes Actions produced by the reactor.
//
// The real effector wires into the harmonik event bus and the codex stdio
// channel. FakeEffector (in fake.go) records actions for scenario assertions.
type Effector interface {
	Execute(ctx context.Context, a Action) error
}

// ─── EventSource ─────────────────────────────────────────────────────────────

// EventSource is the provider of typed Events.
//
// Implementations:
//   - Live source: wraps a codexwire stream from a running codex app-server.
//   - Replay source: reads from a saved JSONL file (e.g. the corpus).
//   - SyntheticSource (fake.go): delivers a pre-defined []Event slice; used in
//     unit and scenario tests.
type EventSource interface {
	// Events returns a channel delivering events until ctx is cancelled or the
	// source is exhausted. The channel is closed when delivery is complete.
	Events(ctx context.Context) <-chan Event
}

// ─── State ───────────────────────────────────────────────────────────────────

// State is the reactor's mutable state. It is updated by Step and can be
// inspected between steps in tests.
type State struct {
	// ThreadID is the currently active codex thread, set on TurnStarted.
	ThreadID string

	// TurnID is the currently active turn, set on TurnStarted and cleared on
	// TurnCompleted or error. Empty when no turn is in-flight.
	TurnID string

	// InFlight is true when a codex turn is active (invariant I1).
	InFlight bool

	// LastSeq is the highest Seq processed so far (invariant I2). Reset to 0
	// on EventTypeConnected (reconnect).
	LastSeq uint64
}

// ─── Reactor ─────────────────────────────────────────────────────────────────

// Reactor is the translation brain for the codex app-server integration.
//
// It is a pure state machine: Step is deterministic given (state, event) and
// produces (new state, actions) with no side effects. All side effects are
// executed by the Effector after Step returns.
//
// Construct with New. The zero value is not valid (use New).
type Reactor struct {
	state State
}

// New constructs a Reactor with empty state.
func New() *Reactor { return &Reactor{} }

// State returns the current reactor state. Safe to call between Steps.
func (r *Reactor) State() State { return r.state }

// Step processes one event, updates state, and returns the actions to execute.
//
// Step is the core of the state machine. It is pure: no goroutines, no I/O,
// no allocations beyond the returned slice. Callers must execute the returned
// actions via an Effector.
//
// Returns nil (not an empty slice) when no actions are produced.
func (r *Reactor) Step(ev Event) []Action {
	// I2 — dedup-by-seq. Seq=0 bypasses dedup (connection lifecycle events).
	if ev.Seq > 0 && ev.Seq <= r.state.LastSeq {
		return nil
	}
	if ev.Seq > 0 {
		r.state.LastSeq = ev.Seq
	}

	switch ev.Type {

	case EventTypeConnected:
		// Reconnect: reset dedup seq and clear in-flight state. Any turn that
		// was in-flight before disconnect was already terminated by the
		// Disconnected event.
		r.state.InFlight = false
		r.state.TurnID = ""
		r.state.LastSeq = 0
		return nil

	case EventTypeDisconnected:
		if r.state.InFlight {
			// I1: terminate the in-flight turn on disconnect.
			r.state.InFlight = false
			r.state.TurnID = ""
			return []Action{{
				Type:    ActionTypeEmitError,
				Message: "disconnected during turn",
			}}
		}
		return nil

	case EventTypeTurnStarted:
		// I1: record the new turn in-flight.
		// (If another turn was already in-flight this supersedes it; the server
		// guarantees ordering in normal operation.)
		r.state.ThreadID = ev.ThreadID
		r.state.TurnID = ev.TurnID
		r.state.InFlight = true
		return nil

	case EventTypeTurnCompleted:
		if !r.state.InFlight {
			// Stale completion with no turn in-flight; drop silently.
			return nil
		}
		r.state.InFlight = false
		r.state.TurnID = ""
		return []Action{{
			Type:     ActionTypeCompleteTurn,
			ThreadID: ev.ThreadID,
			TurnID:   ev.TurnID,
			Status:   ev.Status,
		}}

	case EventTypeMessageDelta:
		return []Action{{
			Type:     ActionTypeEmitOutput,
			ThreadID: ev.ThreadID,
			TurnID:   ev.TurnID,
			ItemID:   ev.ItemID,
			Delta:    ev.Delta,
		}}

	case EventTypeThreadStatus:
		return []Action{{
			Type:     ActionTypeNotifyStatus,
			ThreadID: ev.ThreadID,
			Status:   ev.Status,
		}}

	case EventTypeTokenUsage:
		return []Action{{
			Type:          ActionTypeNotifyTokenUsage,
			ThreadID:      ev.ThreadID,
			TurnID:        ev.TurnID,
			TotalTokens:   ev.TotalTokens,
			ContextWindow: ev.ContextWindow,
		}}

	case EventTypeError:
		// Server or transport error terminates any in-flight turn.
		r.state.InFlight = false
		r.state.TurnID = ""
		return []Action{{
			Type:    ActionTypeEmitError,
			Message: ev.Message,
		}}
	}

	// Unknown event type: drop silently (forward-compat).
	return nil
}

// Run drives the reactor loop: reads Events from src, calls Step, and executes
// the resulting Actions via eff. Returns when ctx is cancelled, src is
// exhausted (channel closed), or Execute returns an error.
func (r *Reactor) Run(ctx context.Context, src EventSource, eff Effector) error {
	for ev := range src.Events(ctx) {
		actions := r.Step(ev)
		for _, a := range actions {
			if err := eff.Execute(ctx, a); err != nil {
				return err
			}
		}
	}
	return nil
}

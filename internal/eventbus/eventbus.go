// Package eventbus declares the in-process pub/sub EventBus interface for harmonik.
//
// The EventBus is the primary mechanism through which subsystems emit typed events
// and consumers register to receive them. All consumers MUST register at daemon
// startup before [EventBus.Seal] is called; post-seal registration is forbidden
// per EV-009 (specs/event-model.md §4.2 EV-009).
//
// Spec ref: specs/event-model.md §6.1 INTERFACE EventBus.
package eventbus

import (
	"context"

	"github.com/gregberns/harmonik/internal/core"
)

// RunDrainer is an optional capability implemented by bus implementations that
// support per-run quiescence (hk-fx6zl).
//
// busImpl satisfies this interface via [busImpl.DrainRun]. Callers that need
// per-run drain (e.g. graceful shutdown of a single bead run without blocking
// other concurrent runs) should type-assert the EventBus value:
//
//	if rd, ok := bus.(eventbus.RunDrainer); ok {
//	    if err := rd.DrainRun(ctx, runID); err != nil { ... }
//	}
//
// The base [EventBus.Drain] method continues to wait for ALL in-flight
// goroutines across all runs and is unchanged by this interface.
//
// Bead: hk-fx6zl.
type RunDrainer interface {
	// DrainRun blocks until all in-flight asynchronous and observer dispatches
	// for runID complete, or ctx is cancelled.
	DrainRun(ctx context.Context, runID core.RunID) error
}

// CommsMessageEmitter is an optional capability implemented by bus implementations
// that support emitting agent_message events and returning the minted event_id.
//
// busImpl satisfies this interface via [busImpl.EmitAgentMessage]. Callers that
// need to emit an agent_message and receive the event_id (e.g. the comms-send
// socket op at agent-comms spec §2.1 C2) should type-assert the EventBus value:
//
//	if ce, ok := bus.(eventbus.CommsMessageEmitter); ok {
//	    eventID, err := ce.EmitAgentMessage(ctx, payload)
//	}
//
// This is a separate interface (not on [EventBus]) so the core EventBus contract
// is unchanged. The pattern follows [RunDrainer].
//
// Bead: hk-nbrmf (comms-send T4).
type CommsMessageEmitter interface {
	// EmitAgentMessage emits an agent_message event (F-class, fsync-boundary per
	// agent-comms spec §1.1) and returns the minted event_id so the caller can
	// relay it back to the CLI. The event_id is UUIDv7-ordered.
	EmitAgentMessage(ctx context.Context, payload core.AgentMessagePayload) (core.EventID, error)
}

// TailTruncationCallback is the optional consumer-supplied callback the bus
// invokes immediately after restart replay completes when the JSONL tail was
// truncated by the read-recovery rule (specs/event-model.md §6.2).
//
// lastDurableEventID is the EventID of the last successfully parsed and
// validated event in the JSONL file before the torn tail was discarded.
// Consumers that do not register a callback receive no truncation signal and
// operate under EV-INV-002's "tolerate loss" obligation alone.
type TailTruncationCallback func(ctx context.Context, lastDurableEventID core.EventID)

// EventBus is the in-process pub/sub mechanism that routes typed events to
// registered consumers (specs/event-model.md §6.1 INTERFACE EventBus).
//
// # Lifecycle
//
// All consumers MUST register via [EventBus.Subscribe] at daemon startup before
// [EventBus.Seal] is called (EV-009). Once [EventBus.Seal] returns, the bus
// enters live-delivery mode; any subsequent [EventBus.Subscribe] call MUST
// return a typed sealed-bus error.
//
// # Emission
//
// [EventBus.Emit] redacts secret-prefixed payload fields (EV-035), appends the
// event to the durable JSONL file with fsync for fsync-boundary–class events
// (EV-015, EV-016), then dispatches the event to all matching consumers:
// synchronous consumers block the caller; asynchronous and observer consumers
// are dispatched off the critical path via the bus worker pool (EV-014a).
//
// # Recovery
//
// On daemon startup, for every [core.Subscription] whose [core.Subscription.Since]
// field is non-nil, the bus performs a JSONL-tail replay to that consumer before
// live-stream delivery begins (EV-014d). If the JSONL tail was truncated the bus
// invokes the consumer's on_tail_truncation callback (when registered) immediately
// after replay completes (specs/event-model.md §6.1).
//
// # Consumer classes
//
// Three consumer classes govern failure-handling and replay behaviour:
// synchronous (EV-010), asynchronous (EV-011), and observer (EV-012).
// Synchronous consumers do NOT participate in replay — their critical-path
// contract ended when the producer returned from [EventBus.Emit].
//
// Spec ref: specs/event-model.md §6.1, §4.2 EV-009–EV-016, EV-035.
type EventBus interface {
	// Emit redacts secret-prefixed payload fields (EV-035), appends the event
	// envelope to the durable JSONL file (EV-015) with fsync for
	// fsync-boundary–class events (EV-016), and dispatches the event to all
	// matching registered consumers (EV-014a).
	//
	// Synchronous consumers are invoked on the caller's goroutine before Emit
	// returns. Asynchronous and observer dispatches occur off the critical path
	// and MUST NOT extend Emit latency (EV-014a).
	//
	// Returns a non-nil error if redaction, JSONL append, or synchronous
	// consumer dispatch fails.
	//
	// Spec ref: specs/event-model.md §6.1, §7.1.
	Emit(ctx context.Context, eventType core.EventType, payload []byte) error

	// EmitWithRunID is identical to Emit but stamps the run_id field on the
	// EV-001 envelope before JSONL append and consumer dispatch.
	//
	// Use EmitWithRunID for all run-scoped events (run_started, run_completed,
	// run_failed, etc.).  Plain Emit is reserved for daemon-level events where no
	// run is in flight (daemon_started, daemon_orphan_sweep_completed, etc.).
	//
	// Spec ref: specs/event-model.md §6.1 EV-001; specs/execution-model.md §4.3 EM-013.
	// Bead: hk-n9f51.
	EmitWithRunID(ctx context.Context, runID core.RunID, eventType core.EventType, payload []byte) error

	// Subscribe registers a consumer with the bus.
	//
	// Subscribe is startup-only (EV-009): it MUST be called before [Seal] and
	// MUST return a typed sealed-bus error if called after [Seal]. The returned
	// Subscription may carry bus-assigned fields (e.g., a normalised
	// ConsumerClass default) and is the canonical record for the registration.
	//
	// When sub.Since is non-nil, the bus schedules a JSONL-tail replay to this
	// consumer during the startup-replay phase (EV-014d); replay is serialised
	// per consumer and live events are buffered until replay reaches the current
	// JSONL tail.
	//
	// Spec ref: specs/event-model.md §6.1, §4.2 EV-009, EV-014d.
	Subscribe(sub core.Subscription) (core.Subscription, error)

	// Seal closes the subscription-registration window.
	//
	// Seal is called by daemon.Start once all consumers have registered. After
	// Seal returns the bus enters live-delivery mode; any further calls to
	// Subscribe MUST return a typed sealed-bus error. Seal initiates the
	// startup-replay phase for consumers whose Since or OffsetCheckpointEventID
	// is non-nil before live delivery begins (EV-014d).
	//
	// Spec ref: specs/event-model.md §6.1, §4.2 EV-009.
	Seal() error

	// ReplayFrom re-issues JSONL events whose event_id is strictly greater than
	// since to the named consumer.
	//
	// Consumers MUST be idempotent on replay delivery: the same event may be
	// delivered more than once (EV-014b). Replay is ordered by event_id
	// (UUIDv7 ascending). Dead-letter and spill files are NOT replayed by
	// ReplayFrom; use [DeadLetterReplay] for those (EV-011).
	//
	// Spec ref: specs/event-model.md §6.1, §4.2 EV-014b.
	ReplayFrom(consumerID string, since core.EventID) error

	// DeadLetterReplay replays events from the dead-letter log
	// (.harmonik/events/dead-letters.jsonl) to the named consumer.
	//
	// This is operator-initiated: the dead-letter log holds events whose
	// delivery to an asynchronous consumer failed after retry exhaustion
	// (EV-011). An optional filter may constrain which dead-letter entries
	// are replayed; nil means replay all entries for the named consumer.
	// Consumers MUST be idempotent on replay delivery (EV-014b).
	//
	// Spec ref: specs/event-model.md §6.1, §6.2, §4.2 EV-011, EV-014b.
	DeadLetterReplay(consumerName string, filter *core.EventPattern) error

	// Drain blocks until all in-flight dispatches for all consumers have
	// completed.
	//
	// Drain is the quiescence primitive used by graceful-shutdown sequences to
	// ensure no event is dropped mid-flight. Drain returns an error if ctx is
	// cancelled before quiescence is reached.
	//
	// Spec ref: specs/event-model.md §6.1.
	Drain(ctx context.Context) error
}

package eventbus

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// jsonlAppender is the write surface of [JSONLWriter] used by [busImpl].
//
// The interface allows [busImpl] to hold either a real [*JSONLWriter] or a
// [nullJSONLWriter] without nil-guarding every call site.  Constructors that
// receive a nil [*JSONLWriter] MUST substitute [nullJSONLWriter{}] so that
// Emit and EmitWithRunID can call Append unconditionally.
//
// Bead ref: hk-2m3bq.
type jsonlAppender interface {
	Append(line []byte, sync bool) error
}

// nullJSONLWriter is a jsonlAppender that silently discards all writes.
// Used as the required-argument default when no log path is configured.
//
// Bead ref: hk-2m3bq.
type nullJSONLWriter struct{}

func (nullJSONLWriter) Append(_ []byte, _ bool) error { return nil }

// busImpl is the concrete in-process implementation of [EventBus].
//
// Emit applies the full EV-035 redaction pipeline before JSONL append and
// consumer dispatch: HC-031 common-prefix field-name redaction PLUS HC-032
// per-handler value-pattern redaction via the [handlercontract.RedactionRegistry]
// supplied at construction.
//
// When constructed via [NewBusImpl] (no patterns), the registry applies HC-031
// only. When constructed via [NewBusImplWithRegistry], the caller's patterns
// are also applied.
//
// # JSONL wiring (hk-8mup.63)
//
// When constructed via [NewBusImplWithWriter], a [*JSONLWriter] is threaded
// through the bus and Emit appends each redacted event as a JSONL line.
// F-class (fsync-boundary) events call Append(line, sync=true); O-class and
// L-class events call Append(line, sync=false). The durability class is
// derived from [isFsyncBoundaryEvent] per the §8 taxonomy table.
//
// When no writer is provided, jsonlWriter holds a [nullJSONLWriter] and
// Append calls are unconditional no-ops (no nil-guard required).
//
// # Dead-letter sink (hk-xvpwb)
//
// When constructed via [NewBusImplWithSink], a [handlercontract.DeadLetterSink]
// is injected at construction time and receives undeliverable events:
//   - Observer/async consumer panics are recorded with reason "observer_panic".
//   - Async/observer consumer dispatch errors are recorded with reason "consumer_error".
//
// When no sink is provided, deadLetterSink holds a [handlercontract.NoopDeadLetterSink]
// and Record calls are unconditional no-ops (no nil-guard required).
//
// # Dispatch order (EV-014a)
//
// Emit returns after: (a) redaction (EV-035), (b) JSONL append + fsync for
// F-class events per EV-016 (hk-8mup.63), (c) synchronous consumer dispatch
// on the caller's goroutine. Asynchronous and observer consumers are dispatched
// off the critical path via per-handler goroutines (MVH) and MUST NOT extend
// Emit latency. A bounded worker pool (default 4 workers, operator-configurable)
// replaces the per-goroutine approach in the post-MVH worker-pool bead.
//
// # Per-run Drain coordination (hk-fx6zl)
//
// wg is the process-level WaitGroup: it tracks ALL in-flight async/observer
// goroutines and is used by the existing [EventBus.Drain] method to wait for
// global quiescence. runDrainersMu guards runDrainers, a per-run WaitGroup
// map keyed by run_id string. EmitWithRunID adds each dispatched goroutine to
// both wg (global) and the run-specific WaitGroup so that [busImpl.DrainRun]
// can wait for a single run's consumers without blocking on other runs.
// Plain Emit (no run_id) only touches wg; DrainRun does not wait for those.
//
// Spec ref: specs/event-model.md §6.1, §4.2 EV-014a, EV-016, EV-035.
// Bead refs: hk-8mup.62, hk-8i31.83, hk-hqwn.19, hk-8mup.63, hk-fx6zl, hk-xvpwb, hk-2m3bq.
type busImpl struct {
	registry       *handlercontract.RedactionRegistry
	jsonlWriter    jsonlAppender                  // never nil; nullJSONLWriter when no log path configured
	deadLetterSink handlercontract.DeadLetterSink // never nil; NoopDeadLetterSink when no sink configured
	idGen          *core.EventIDGenerator
	mu             sync.Mutex
	subscriptions  []core.Subscription
	sealed         bool
	// wg tracks ALL in-flight async/observer goroutines (global quiescence).
	// Drain(ctx) waits on wg. EmitWithRunID also increments the per-run entry
	// in runDrainers so DrainRun can wait for just one run (hk-fx6zl).
	wg sync.WaitGroup
	// runDrainersMu guards runDrainers.
	runDrainersMu sync.Mutex
	// runDrainers maps run_id → WaitGroup tracking only that run's in-flight
	// goroutines. Entries are created lazily on first EmitWithRunID for a run
	// and are never removed (they become no-ops once the run drains).
	runDrainers map[string]*sync.WaitGroup
}

// fsyncBoundaryEventTypes is the static set of F-class (fsync-boundary)
// event types derived from the §8 taxonomy table in specs/event-model.md.
// F-class events require Append(line, sync=true) per EV-016 / EV-016a.
//
// This set is exhaustive for the §8 rows marked "F" as of event-model.md
// v0.3.4. Additions to the §8 taxonomy MUST update this map.
//
// Spec ref: specs/event-model.md §4.4 EV-016, EV-016a; §8 taxonomy table.
// Bead ref: hk-8mup.63.
var fsyncBoundaryEventTypes = map[core.EventType]struct{}{
	// §8.1 Run lifecycle (F-class rows).
	core.EventType("run_started"):        {},
	core.EventType("run_completed"):      {},
	core.EventType("run_failed"):         {},
	core.EventType("transition_event"):   {},
	core.EventType("checkpoint_written"): {},
	// §8.7 Daemon lifecycle (F-class rows).
	core.EventType("daemon_started"):             {},
	core.EventType("daemon_ready"):               {},
	core.EventType("daemon_shutdown"):            {},
	core.EventType("daemon_startup_failed"):      {},
	core.EventType("operator_upgrade_completed"): {},
}

// isFsyncBoundaryEvent reports whether eventType is an F-class (fsync-boundary)
// event per the §8 taxonomy table. F-class events require an fsync after JSONL
// append (EV-016 / EV-016a). All other classes (O = ordinary, L = lossy-tail-ok)
// do not require an fsync.
//
// Spec ref: specs/event-model.md §4.4 EV-016, EV-016a; §8.
// Bead ref: hk-8mup.63.
func isFsyncBoundaryEvent(eventType core.EventType) bool {
	_, ok := fsyncBoundaryEventTypes[eventType]
	return ok
}

// NewBusImpl constructs a busImpl with a zero-pattern RedactionRegistry.
//
// This constructor provides backward compatibility for call sites that do not
// yet have a RedactionRegistry available. Redaction applies HC-031
// (common-prefix field names) only. Callers that need HC-032 per-handler
// value-pattern redaction MUST use [NewBusImplWithRegistry].
//
// The returned bus is unsealed; callers MUST call Subscribe for all consumers
// before calling Seal (EV-009). The returned value satisfies [EventBus].
//
// Spec ref: specs/event-model.md §6.1, §4.2 EV-035, PL-005 step 0.
func NewBusImpl() EventBus {
	return &busImpl{
		registry:       handlercontract.NewRedactionRegistry(),
		jsonlWriter:    nullJSONLWriter{},
		deadLetterSink: handlercontract.NoopDeadLetterSink{},
		idGen:          core.NewEventIDGenerator(),
		runDrainers:    make(map[string]*sync.WaitGroup),
	}
}

// NewBusImplWithRegistry constructs a busImpl that delegates all redaction to
// the supplied [handlercontract.RedactionRegistry].
//
// The registry MUST be fully populated (all RegisterPattern calls complete)
// before the bus is sealed per PL-005 step 0. Passing a nil registry is
// equivalent to calling [NewBusImpl].
//
// The returned bus is unsealed; callers MUST call Subscribe for all consumers
// before calling Seal (EV-009). The returned value satisfies [EventBus].
//
// Spec ref: specs/event-model.md §6.1, §4.2 EV-035; specs/handler-contract.md §4.7.HC-032.
func NewBusImplWithRegistry(registry *handlercontract.RedactionRegistry) EventBus {
	if registry == nil {
		return NewBusImpl()
	}
	return &busImpl{
		registry:       registry,
		jsonlWriter:    nullJSONLWriter{},
		deadLetterSink: handlercontract.NoopDeadLetterSink{},
		idGen:          core.NewEventIDGenerator(),
		runDrainers:    make(map[string]*sync.WaitGroup),
	}
}

// NewBusImplWithWriter constructs a busImpl with both a
// [handlercontract.RedactionRegistry] and a [*JSONLWriter] for durable event
// logging.
//
// Every Emit call will append the redacted event to the JSONL log via writer.
// F-class (fsync-boundary) event types are fsynced before Emit returns
// (EV-016 / EV-016a); O-class and L-class events are written without fsync.
//
// Passing a nil registry is equivalent to a zero-pattern registry (HC-031
// only). Passing a nil writer substitutes a [nullJSONLWriter] (Append is a
// no-op); this has the same observable behaviour as [NewBusImplWithRegistry]
// but keeps the busImpl invariant that jsonlWriter is never nil.
//
// The returned bus is unsealed; callers MUST call Subscribe for all consumers
// before calling Seal (EV-009). The returned value satisfies [EventBus].
//
// Spec ref: specs/event-model.md §6.1, §4.2 EV-016, EV-016a, EV-035;
// specs/handler-contract.md §4.7.HC-032.
// Bead ref: hk-8mup.63, hk-2m3bq.
func NewBusImplWithWriter(registry *handlercontract.RedactionRegistry, writer *JSONLWriter) EventBus {
	if registry == nil {
		registry = handlercontract.NewRedactionRegistry()
	}
	var w jsonlAppender = nullJSONLWriter{}
	if writer != nil {
		w = writer
	}
	return &busImpl{
		registry:       registry,
		jsonlWriter:    w,
		deadLetterSink: handlercontract.NoopDeadLetterSink{},
		idGen:          core.NewEventIDGenerator(),
		runDrainers:    make(map[string]*sync.WaitGroup),
	}
}

// NewBusImplWithSink constructs a busImpl with a [handlercontract.RedactionRegistry],
// a [*JSONLWriter], and a [handlercontract.DeadLetterSink] for undeliverable events.
//
// Async/observer consumer panics are recorded to sink with reason "observer_panic".
// Async/observer consumer dispatch errors are recorded to sink with reason "consumer_error".
//
// Passing nil for registry, writer, or sink is safe:
//   - nil registry falls back to HC-031-only redaction (same as [NewBusImpl]).
//   - nil writer substitutes a [nullJSONLWriter] (JSONL append is a no-op).
//   - nil sink substitutes a [handlercontract.NoopDeadLetterSink] (undeliverable
//     events are silently discarded). Record is called unconditionally — no nil-guard.
//
// This constructor is the preferred call site for daemon.Start when
// MVH_ROADMAP row #9 dead-letter wiring is active.
//
// The returned bus is unsealed; callers MUST call Subscribe for all consumers
// before calling Seal (EV-009). The returned value satisfies [EventBus].
//
// Spec ref: specs/event-model.md §6.1, §4.2 EV-016, EV-016a, EV-035;
// specs/handler-contract.md §4.7.HC-032.
// Bead ref: hk-xvpwb, hk-2m3bq.
func NewBusImplWithSink(registry *handlercontract.RedactionRegistry, writer *JSONLWriter, sink handlercontract.DeadLetterSink) EventBus {
	if registry == nil {
		registry = handlercontract.NewRedactionRegistry()
	}
	var w jsonlAppender = nullJSONLWriter{}
	if writer != nil {
		w = writer
	}
	if sink == nil {
		sink = handlercontract.NoopDeadLetterSink{}
	}
	return &busImpl{
		registry:       registry,
		jsonlWriter:    w,
		deadLetterSink: sink,
		idGen:          core.NewEventIDGenerator(),
		runDrainers:    make(map[string]*sync.WaitGroup),
	}
}

// Emit applies EV-035 redaction to payload via the registry's
// RedactionMiddleware, then appends the event to the JSONL log (stub at MVH),
// then dispatches to matching registered consumers per EV-014a.
//
// Dispatch order (EV-014a):
//
//  1. Redaction via HC-031 + HC-032 registry (EV-035).
//  2. JSONL append + fsync per durability class (EV-016). Deferred stub at MVH:
//     file path not yet threaded through daemon.Config; follow-up bead adds wiring.
//  3. Synchronous-consumer dispatch on the caller's goroutine — Emit blocks until
//     the at-most-one synchronous consumer returns or errors (EV-010).
//  4. Asynchronous and observer consumers are dispatched off the critical path
//     via per-handler goroutines (MVH; post-MVH: bounded worker pool) and MUST
//     NOT extend Emit latency (EV-014a).
//
// Spec ref: specs/event-model.md §6.1, §7.1, §4.2 EV-014a, §4.4 EV-035.
func (b *busImpl) Emit(ctx context.Context, eventType core.EventType, payload []byte) error {
	// Step 1: decode payload to map for redaction (EV-035).
	var rawPayload map[string]any
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &rawPayload); err != nil {
			return fmt.Errorf("eventbus.Emit: payload unmarshal for redaction: %w", err)
		}
	}

	// Step 2: apply HC-031 + HC-032 redaction pipeline BEFORE JSONL append
	// and consumer dispatch (EV-035).
	redacted := b.registry.RedactionMiddleware(rawPayload)

	// Step 3: re-encode redacted payload.
	redactedBytes, err := json.Marshal(redacted)
	if err != nil {
		return fmt.Errorf("eventbus.Emit: re-encoding redacted payload: %w", err)
	}

	// Step 4a: build the complete EV-001 envelope. event_id and timestamp_wall
	// are stamped here, inside the emitter, per EV-001. source_subsystem uses
	// the eventbus package identifier; callers that need a subsystem-specific
	// value should set it before dispatch (post-MVH daemon-watcher stamping per
	// EV-002b will own this). schema_version=1 is the current envelope version.
	eventID, idErr := b.idGen.Next()
	if idErr != nil {
		return fmt.Errorf("eventbus.Emit: generate event_id: %w", idErr)
	}
	now := time.Now()
	evt := core.Event{
		EventID:         eventID,
		SchemaVersion:   1,
		Type:            string(eventType),
		TimestampWall:   now,
		SourceSubsystem: "eventbus",
		Payload:         redactedBytes,
	}

	// Step 4b: JSONL append + fsync per EV-016 durability class (hk-8mup.63).
	// Marshal the COMPLETE envelope (all EV-001 fields + nested payload) to a
	// single JSON object. F-class (fsync-boundary) events are fsynced before
	// returning; O-class and L-class events are written without fsync.
	// jsonlWriter is never nil (nullJSONLWriter when no log path configured).
	envelopeBytes, marshalErr := json.Marshal(evt)
	if marshalErr != nil {
		return fmt.Errorf("eventbus.Emit: marshal envelope: %w", marshalErr)
	}
	needsSync := isFsyncBoundaryEvent(eventType)
	if appendErr := b.jsonlWriter.Append(envelopeBytes, needsSync); appendErr != nil {
		return fmt.Errorf("eventbus.Emit: JSONL append: %w", appendErr)
	}

	// Step 5: collect matching subscriptions once under lock so dispatch runs
	// without holding the mutex.
	b.mu.Lock()
	subs := make([]core.Subscription, len(b.subscriptions))
	copy(subs, b.subscriptions)
	b.mu.Unlock()

	// Step 6: dispatch per consumer class (EV-014a).
	for _, sub := range subs {
		if !sub.EventPattern.MatchesType(string(eventType)) {
			continue
		}
		if sub.Handler == nil {
			continue
		}

		switch sub.ConsumerClass {
		case core.ConsumerClassSynchronous:
			// Synchronous consumers run on the caller's goroutine and block
			// Emit until they return (EV-010). At most one per event type is
			// permitted; enforced at subscription time (hk-hqwn.49).
			if handlerErr := sub.Handler(ctx, evt); handlerErr != nil {
				return fmt.Errorf("eventbus.Emit: synchronous consumer %q: %w", sub.ConsumerID, handlerErr)
			}

		default:
			// Asynchronous and observer consumers run off the critical path
			// (EV-014a / EV-011 / EV-012). At MVH a dedicated goroutine is
			// launched per dispatch; post-MVH: replace with bounded worker pool
			// (default 4 workers, operator-configurable per EV-014a).
			sub := sub // capture loop variable
			b.wg.Add(1)
			go func() {
				defer b.wg.Done()
				// Panic recovery (hk-xvpwb): recover observer panics and record
				// them to the dead-letter sink with reason "observer_panic". If
				// the sink is nil, the panic is absorbed and logged nowhere
				// (post-MVH: add structured logger fallback).
				defer func() {
					if r := recover(); r != nil {
						// deadLetterSink is never nil (NoopDeadLetterSink when no sink configured).
						_ = b.deadLetterSink.Record(ctx, evt, "observer_panic")
					}
				}()
				// Context is passed through so callers can cancel in-flight
				// async/observer work during shutdown before Drain returns.
				// Consumer errors are recorded to the dead-letter sink with
				// reason "consumer_error" (hk-xvpwb).
				if handlerErr := sub.Handler(ctx, evt); handlerErr != nil {
					_ = b.deadLetterSink.Record(ctx, evt, "consumer_error")
				}
			}()
		}
	}
	return nil
}

// EmitWithRunID is identical to Emit but stamps the run_id envelope field to
// runID before JSONL append and consumer dispatch.
//
// Use EmitWithRunID for all run-scoped events (run_started, run_completed,
// run_failed, etc.).  Plain Emit is reserved for daemon-level events where no
// run is in flight (daemon_started, daemon_orphan_sweep_completed, etc.).
//
// Spec ref: specs/event-model.md §6.1 EV-001; specs/execution-model.md §4.3 EM-013.
// Bead: hk-n9f51.
func (b *busImpl) EmitWithRunID(ctx context.Context, runID core.RunID, eventType core.EventType, payload []byte) error {
	// Step 1: decode payload to map for redaction (EV-035).
	var rawPayload map[string]any
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &rawPayload); err != nil {
			return fmt.Errorf("eventbus.EmitWithRunID: payload unmarshal for redaction: %w", err)
		}
	}

	// Step 2: apply HC-031 + HC-032 redaction pipeline BEFORE JSONL append
	// and consumer dispatch (EV-035).
	redacted := b.registry.RedactionMiddleware(rawPayload)

	// Step 3: re-encode redacted payload.
	redactedBytes, err := json.Marshal(redacted)
	if err != nil {
		return fmt.Errorf("eventbus.EmitWithRunID: re-encoding redacted payload: %w", err)
	}

	// Step 4a: build the complete EV-001 envelope with run_id stamped.
	eventID, idErr := b.idGen.Next()
	if idErr != nil {
		return fmt.Errorf("eventbus.EmitWithRunID: generate event_id: %w", idErr)
	}
	now := time.Now()
	runIDVal := runID
	evt := core.Event{
		EventID:         eventID,
		SchemaVersion:   1,
		Type:            string(eventType),
		TimestampWall:   now,
		RunID:           &runIDVal,
		SourceSubsystem: "eventbus",
		Payload:         redactedBytes,
	}

	// Step 4b: JSONL append + fsync per EV-016 durability class (hk-8mup.63).
	// jsonlWriter is never nil (nullJSONLWriter when no log path configured).
	envelopeBytes, marshalErr := json.Marshal(evt)
	if marshalErr != nil {
		return fmt.Errorf("eventbus.EmitWithRunID: marshal envelope: %w", marshalErr)
	}
	needsSync := isFsyncBoundaryEvent(eventType)
	if appendErr := b.jsonlWriter.Append(envelopeBytes, needsSync); appendErr != nil {
		return fmt.Errorf("eventbus.EmitWithRunID: JSONL append: %w", appendErr)
	}

	// Step 5: collect matching subscriptions once under lock.
	b.mu.Lock()
	subs := make([]core.Subscription, len(b.subscriptions))
	copy(subs, b.subscriptions)
	b.mu.Unlock()

	// Step 6: dispatch per consumer class (EV-014a).
	for _, sub := range subs {
		if !sub.EventPattern.MatchesType(string(eventType)) {
			continue
		}
		if sub.Handler == nil {
			continue
		}

		switch sub.ConsumerClass {
		case core.ConsumerClassSynchronous:
			if handlerErr := sub.Handler(ctx, evt); handlerErr != nil {
				return fmt.Errorf("eventbus.EmitWithRunID: synchronous consumer %q: %w", sub.ConsumerID, handlerErr)
			}
		default:
			// Asynchronous and observer consumers for a run-scoped event are
			// tracked in BOTH the global wg (for Drain/global quiescence) and
			// the per-run WaitGroup (for DrainRun/per-run fair termination).
			// Bead: hk-fx6zl.
			sub := sub // capture loop variable
			runWG := b.runDrainer(evt.RunID.String())
			b.wg.Add(1)
			runWG.Add(1)
			go func() {
				defer b.wg.Done()
				defer runWG.Done()
				// Panic recovery (hk-xvpwb): recover observer panics and record
				// them to the dead-letter sink with reason "observer_panic".
				// deadLetterSink is never nil (NoopDeadLetterSink when no sink configured).
				defer func() {
					if r := recover(); r != nil {
						_ = b.deadLetterSink.Record(ctx, evt, "observer_panic")
					}
				}()
				// Consumer errors are recorded to the dead-letter sink with
				// reason "consumer_error" (hk-xvpwb).
				if handlerErr := sub.Handler(ctx, evt); handlerErr != nil {
					_ = b.deadLetterSink.Record(ctx, evt, "consumer_error")
				}
			}()
		}
	}
	return nil
}

// ErrDuplicateSynchronousConsumer is the typed configuration error returned
// by Subscribe when a second synchronous consumer registers for an event type
// that already has one. At most one synchronous consumer per event type is
// permitted per EV-014 / EV-INV-003.
//
// Spec ref: specs/event-model.md §4.2 EV-014; §5.3 EV-INV-003.
type ErrDuplicateSynchronousConsumer struct {
	// ConflictingConsumerID is the consumer ID that was already registered.
	ConflictingConsumerID string
	// IncomingConsumerID is the consumer ID that triggered the conflict.
	IncomingConsumerID string
	// EventType is a representative event type string where the conflict was
	// detected. For wildcard subscriptions the value is "*".
	EventType string
}

func (e *ErrDuplicateSynchronousConsumer) Error() string {
	return fmt.Sprintf(
		"eventbus: EV-014 / EV-INV-003: duplicate synchronous consumer for event type %q: "+
			"existing=%q incoming=%q; at most one synchronous consumer per event type is permitted",
		e.EventType, e.ConflictingConsumerID, e.IncomingConsumerID,
	)
}

// ErrSynchronousConsumerCycle is the typed configuration error returned by
// Subscribe when a synchronous consumer's DeclaredEmitTypes would introduce a
// re-dispatch cycle among synchronous consumers (EV-010 acyclicity clause).
//
// Spec ref: specs/event-model.md §4.2 EV-010; §5.3 EV-INV-003.
type ErrSynchronousConsumerCycle struct {
	// IncomingConsumerID is the consumer that triggered the cycle.
	IncomingConsumerID string
	// CyclePath is the sequence of consumer IDs that form the cycle,
	// starting and ending at IncomingConsumerID.
	CyclePath []string
}

func (e *ErrSynchronousConsumerCycle) Error() string {
	return fmt.Sprintf(
		"eventbus: EV-010 / EV-INV-003: synchronous consumer %q would introduce a "+
			"re-dispatch cycle; acyclicity check fail-closed; cycle path: %v",
		e.IncomingConsumerID, e.CyclePath,
	)
}

// Subscribe registers a consumer with the bus.
//
// For synchronous consumers, Subscribe enforces two registration-time invariants:
//
//  1. Cardinality ≤ 1 per event type (EV-014 / EV-INV-003): if an existing
//     synchronous consumer's EventPattern overlaps with sub.EventPattern, Subscribe
//     returns [*ErrDuplicateSynchronousConsumer].
//
//  2. Acyclicity of declared emission surfaces (EV-010 / EV-INV-003): if
//     sub.DeclaredEmitTypes would introduce a re-dispatch cycle among synchronous
//     consumers, Subscribe returns [*ErrSynchronousConsumerCycle].
//
// Returns a typed error if called after Seal (EV-009).
//
// Spec ref: specs/event-model.md §6.1, §4.2 EV-009, EV-010, EV-014; §5.3 EV-INV-003.
func (b *busImpl) Subscribe(sub core.Subscription) (core.Subscription, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.sealed {
		return core.Subscription{}, fmt.Errorf("eventbus: Subscribe called after Seal (EV-009): consumer %q", sub.ConsumerID)
	}

	// Registration-time invariant checks for synchronous consumers (EV-014, EV-010).
	if sub.ConsumerClass == core.ConsumerClassSynchronous {
		// Check 1 — EV-014 cardinality: at most one synchronous consumer per event type.
		if err := b.checkSyncCardinality(sub); err != nil {
			return core.Subscription{}, err
		}

		// Check 2 — EV-010 acyclicity: DeclaredEmitTypes MUST NOT form a
		// re-dispatch cycle among synchronous consumers.
		if err := b.checkSyncAcyclicity(sub); err != nil {
			return core.Subscription{}, err
		}
	}

	b.subscriptions = append(b.subscriptions, sub)
	return sub, nil
}

// SubscriptionCount returns the number of consumers registered with the bus.
//
// This is a test-and-diagnostics helper.  In production the count is only
// meaningful between the last Subscribe call and Seal(); after Seal the
// slice is immutable and the returned value reflects the final wired count.
//
// Bead ref: hk-37zy8 (used in production-composition subscription test).
func (b *busImpl) SubscriptionCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.subscriptions)
}

// BusSubscriptionCount returns the number of subscriptions registered on bus.
//
// It uses a type assertion against an unexported interface to avoid adding
// SubscriptionCount to the public EventBus interface.  Returns -1 when bus
// does not implement the counter (e.g. a mock in tests).
//
// Bead ref: hk-37zy8.
func BusSubscriptionCount(bus EventBus) int {
	type counter interface {
		SubscriptionCount() int
	}
	if c, ok := bus.(counter); ok {
		return c.SubscriptionCount()
	}
	return -1
}

// SubscribedConsumerIDs returns the ConsumerIDs of all subscriptions registered
// on the bus.
//
// Bead ref: hk-ndysh.
func (b *busImpl) SubscribedConsumerIDs() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	ids := make([]string, len(b.subscriptions))
	for i, s := range b.subscriptions {
		ids[i] = s.ConsumerID
	}
	return ids
}

// BusSubscribedConsumerIDs returns the ConsumerIDs of all subscriptions on bus.
//
// It uses a type assertion against an unexported interface to avoid adding
// SubscribedConsumerIDs to the public EventBus interface. Returns nil when bus
// does not implement the lister (e.g. a mock in tests).
//
// Bead ref: hk-ndysh.
func BusSubscribedConsumerIDs(bus EventBus) []string {
	type lister interface {
		SubscribedConsumerIDs() []string
	}
	if l, ok := bus.(lister); ok {
		return l.SubscribedConsumerIDs()
	}
	return nil
}

// checkSyncCardinality enforces EV-014 / EV-INV-003: at most one synchronous
// consumer per event type. Called under b.mu.
func (b *busImpl) checkSyncCardinality(incoming core.Subscription) error {
	for _, existing := range b.subscriptions {
		if existing.ConsumerClass != core.ConsumerClassSynchronous {
			continue
		}
		// Detect overlap between existing.EventPattern and incoming.EventPattern.
		// Overlap exists when both patterns can match the same event type:
		//   - wildcard ∩ anything → overlap
		//   - explicit ∩ explicit → check set intersection
		conflictType, overlaps := syncPatternOverlap(existing.EventPattern, incoming.EventPattern)
		if overlaps {
			return &ErrDuplicateSynchronousConsumer{
				ConflictingConsumerID: existing.ConsumerID,
				IncomingConsumerID:    incoming.ConsumerID,
				EventType:             conflictType,
			}
		}
	}
	return nil
}

// syncPatternOverlap reports whether two EventPatterns can match the same event
// type. Returns a representative conflicting type string and true when they overlap.
// For wildcard-vs-anything, returns "*". For explicit-vs-explicit, returns a
// member of the intersection set.
func syncPatternOverlap(a, b core.EventPattern) (string, bool) {
	if a.Wildcard || b.Wildcard {
		// Wildcard overlaps with everything.
		return "*", true
	}
	// Both explicit: check for intersection.
	for t := range a.Types {
		if _, ok := b.Types[t]; ok {
			return t, true
		}
	}
	return "", false
}

// checkSyncAcyclicity enforces EV-010 / EV-INV-003: synchronous consumers MUST
// NOT form a re-dispatch cycle via their DeclaredEmitTypes. Called under b.mu.
//
// A cycle exists when following the edges:
//
//	syncConsumer.EventPattern → syncConsumer.DeclaredEmitTypes → (next syncConsumer) → …
//
// produces a path that eventually re-reaches a consumer whose pattern matches
// one of the event types the incoming consumer subscribes to.
//
// The check is conservative: for wildcard patterns, all declared emit types are
// treated as potential subscribers to avoid false negatives.
func (b *busImpl) checkSyncAcyclicity(incoming core.Subscription) error {
	if len(incoming.DeclaredEmitTypes) == 0 {
		return nil // emits nothing → cannot form a cycle
	}

	// Build the set of all synchronous consumers (existing + incoming) for DFS.
	// incoming is included so the DFS can detect direct self-cycles.
	allSync := make([]core.Subscription, 0, len(b.subscriptions)+1)
	allSync = append(allSync, incoming)
	for _, s := range b.subscriptions {
		if s.ConsumerClass == core.ConsumerClassSynchronous {
			allSync = append(allSync, s)
		}
	}

	// DFS from each of incoming's DeclaredEmitTypes to check for a path back
	// to the incoming consumer itself.
	//
	// The cycle condition: incoming declares it emits type T → some existing sync
	// consumer A subscribes to T and declares it emits type U → … → eventually
	// some consumer emits a type that matches incoming's EventPattern. This means
	// an Emit on that final type would re-dispatch to incoming, completing the cycle.
	//
	// visited tracks consumer IDs already explored in the current DFS to avoid
	// infinite loops. path records the current DFS path for error reporting.
	var dfs func(emitType core.EventType, visited map[string]bool, path []string) []string
	dfs = func(emitType core.EventType, visited map[string]bool, path []string) []string {
		// First check: does emitType itself match the incoming consumer's subscription?
		// If yes, a direct re-dispatch cycle is detected.
		if incoming.EventPattern.MatchesType(string(emitType)) {
			return append(path, incoming.ConsumerID)
		}

		// Find existing synchronous consumers that subscribe to emitType and
		// follow their declared emissions.
		for _, s := range allSync {
			if s.ConsumerID == incoming.ConsumerID {
				continue
			}
			if !s.EventPattern.MatchesType(string(emitType)) {
				continue
			}
			if visited[s.ConsumerID] {
				continue
			}
			visited[s.ConsumerID] = true
			// Follow s's declared emissions transitively.
			for _, nextEmit := range s.DeclaredEmitTypes {
				if cyclePath := dfs(nextEmit, visited, append(path, s.ConsumerID)); cyclePath != nil {
					return cyclePath
				}
			}
		}
		return nil
	}

	visited := map[string]bool{incoming.ConsumerID: true}
	for _, emitType := range incoming.DeclaredEmitTypes {
		path := []string{incoming.ConsumerID}
		if cyclePath := dfs(emitType, visited, path); cyclePath != nil {
			return &ErrSynchronousConsumerCycle{
				IncomingConsumerID: incoming.ConsumerID,
				CyclePath:          cyclePath,
			}
		}
	}
	return nil
}

// Seal closes the subscription-registration window.
//
// Spec ref: specs/event-model.md §6.1, §4.2 EV-009.
func (b *busImpl) Seal() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sealed = true
	return nil
}

// ReplayFrom re-issues JSONL events to the named consumer (stub — JSONL path
// wiring is deferred to the JSONL bead).
//
// Spec ref: specs/event-model.md §6.1, §4.2 EV-014b.
func (b *busImpl) ReplayFrom(_ string, _ core.EventID) error {
	return nil
}

// DeadLetterReplay replays events from the dead-letter log (stub).
//
// Spec ref: specs/event-model.md §6.1, §6.2, §4.2 EV-011.
func (b *busImpl) DeadLetterReplay(_ string, _ *core.EventPattern) error {
	return nil
}

// runDrainer returns the WaitGroup for runID, creating it if it does not exist.
// The returned pointer is stable for the lifetime of busImpl; callers may call
// Add/Done/Wait without holding runDrainersMu.
func (b *busImpl) runDrainer(runID string) *sync.WaitGroup {
	b.runDrainersMu.Lock()
	defer b.runDrainersMu.Unlock()
	wg, ok := b.runDrainers[runID]
	if !ok {
		wg = &sync.WaitGroup{}
		b.runDrainers[runID] = wg
	}
	return wg
}

// DrainRun blocks until all in-flight asynchronous and observer dispatches
// for the given runID complete, or ctx is cancelled.
//
// DrainRun provides fair per-run quiescence: a slow consumer from run A does
// NOT delay shutdown of run B (hk-fx6zl). Plain [EventBus.Drain] waits for
// ALL in-flight goroutines across all runs and remains unchanged.
//
// DrainRun only tracks goroutines launched by [busImpl.EmitWithRunID] for the
// given runID. Goroutines launched by plain [busImpl.Emit] (no run_id) are not
// tracked per-run and will not be waited on.
//
// Bead: hk-fx6zl.
func (b *busImpl) DrainRun(ctx context.Context, runID core.RunID) error {
	wg := b.runDrainer(runID.String())
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("eventbus.DrainRun(%s): %w", runID, ctx.Err())
	}
}

// Drain blocks until all in-flight asynchronous and observer dispatches
// complete, or ctx is cancelled.
//
// Synchronous consumers run on the caller's goroutine and are always complete
// by the time Emit returns; Drain only waits for off-path (async / observer)
// goroutines (EV-014a).
//
// Spec ref: specs/event-model.md §6.1.
func (b *busImpl) Drain(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		b.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("eventbus.Drain: %w", ctx.Err())
	}
}

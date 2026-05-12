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
// # Dead-letter sink (hk-xvpwb)
//
// When constructed via [NewBusImplWithSink], a [handlercontract.DeadLetterSink]
// is injected at construction time and receives undeliverable events:
//   - Observer/async consumer panics are recorded with reason "observer_panic".
//   - Async/observer consumer dispatch errors are recorded with reason "consumer_error".
//
// The sink field is optional; a nil sink causes the bus to silently drop
// undeliverable events (logging is not yet wired — post-MVH).
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
// Bead refs: hk-8mup.62, hk-8i31.83, hk-hqwn.19, hk-8mup.63, hk-fx6zl, hk-xvpwb.
type busImpl struct {
	registry       *handlercontract.RedactionRegistry
	jsonlWriter    *JSONLWriter                   // nil when no log path is configured
	deadLetterSink handlercontract.DeadLetterSink // nil when no sink is configured
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
		registry:    handlercontract.NewRedactionRegistry(),
		idGen:       core.NewEventIDGenerator(),
		runDrainers: make(map[string]*sync.WaitGroup),
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
		registry:    registry,
		idGen:       core.NewEventIDGenerator(),
		runDrainers: make(map[string]*sync.WaitGroup),
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
// only). Passing a nil writer disables JSONL append (same behaviour as
// [NewBusImplWithRegistry]).
//
// The returned bus is unsealed; callers MUST call Subscribe for all consumers
// before calling Seal (EV-009). The returned value satisfies [EventBus].
//
// Spec ref: specs/event-model.md §6.1, §4.2 EV-016, EV-016a, EV-035;
// specs/handler-contract.md §4.7.HC-032.
// Bead ref: hk-8mup.63.
func NewBusImplWithWriter(registry *handlercontract.RedactionRegistry, writer *JSONLWriter) EventBus {
	if registry == nil {
		registry = handlercontract.NewRedactionRegistry()
	}
	return &busImpl{
		registry:    registry,
		jsonlWriter: writer,
		idGen:       core.NewEventIDGenerator(),
		runDrainers: make(map[string]*sync.WaitGroup),
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
//   - nil writer disables JSONL append.
//   - nil sink silently drops undeliverable events (bus MUST NOT panic on nil sink).
//
// This constructor is the preferred call site for daemon.Start when
// MVH_ROADMAP row #9 dead-letter wiring is active.
//
// The returned bus is unsealed; callers MUST call Subscribe for all consumers
// before calling Seal (EV-009). The returned value satisfies [EventBus].
//
// Spec ref: specs/event-model.md §6.1, §4.2 EV-016, EV-016a, EV-035;
// specs/handler-contract.md §4.7.HC-032.
// Bead ref: hk-xvpwb.
func NewBusImplWithSink(registry *handlercontract.RedactionRegistry, writer *JSONLWriter, sink handlercontract.DeadLetterSink) EventBus {
	if registry == nil {
		registry = handlercontract.NewRedactionRegistry()
	}
	return &busImpl{
		registry:       registry,
		jsonlWriter:    writer,
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
	// returning; O-class and L-class events are written without fsync. When no
	// writer is configured (nil), this step is a no-op (e.g., in-memory-only tests).
	if b.jsonlWriter != nil {
		envelopeBytes, marshalErr := json.Marshal(evt)
		if marshalErr != nil {
			return fmt.Errorf("eventbus.Emit: marshal envelope: %w", marshalErr)
		}
		needsSync := isFsyncBoundaryEvent(eventType)
		if appendErr := b.jsonlWriter.Append(envelopeBytes, needsSync); appendErr != nil {
			return fmt.Errorf("eventbus.Emit: JSONL append: %w", appendErr)
		}
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
						if b.deadLetterSink != nil {
							_ = b.deadLetterSink.Record(ctx, evt, "observer_panic")
						}
					}
				}()
				// Context is passed through so callers can cancel in-flight
				// async/observer work during shutdown before Drain returns.
				// Consumer errors are recorded to the dead-letter sink with
				// reason "consumer_error" (hk-xvpwb).
				if handlerErr := sub.Handler(ctx, evt); handlerErr != nil {
					if b.deadLetterSink != nil {
						_ = b.deadLetterSink.Record(ctx, evt, "consumer_error")
					}
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
	if b.jsonlWriter != nil {
		envelopeBytes, marshalErr := json.Marshal(evt)
		if marshalErr != nil {
			return fmt.Errorf("eventbus.EmitWithRunID: marshal envelope: %w", marshalErr)
		}
		needsSync := isFsyncBoundaryEvent(eventType)
		if appendErr := b.jsonlWriter.Append(envelopeBytes, needsSync); appendErr != nil {
			return fmt.Errorf("eventbus.EmitWithRunID: JSONL append: %w", appendErr)
		}
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
				// them to the dead-letter sink with reason "observer_panic". If
				// the sink is nil, the panic is absorbed and logged nowhere
				// (post-MVH: add structured logger fallback).
				defer func() {
					if r := recover(); r != nil {
						if b.deadLetterSink != nil {
							_ = b.deadLetterSink.Record(ctx, evt, "observer_panic")
						}
					}
				}()
				// Consumer errors are recorded to the dead-letter sink with
				// reason "consumer_error" (hk-xvpwb).
				if handlerErr := sub.Handler(ctx, evt); handlerErr != nil {
					if b.deadLetterSink != nil {
						_ = b.deadLetterSink.Record(ctx, evt, "consumer_error")
					}
				}
			}()
		}
	}
	return nil
}

// Subscribe registers a consumer with the bus.
//
// Returns a typed error if called after Seal (EV-009).
//
// Spec ref: specs/event-model.md §6.1, §4.2 EV-009.
func (b *busImpl) Subscribe(sub core.Subscription) (core.Subscription, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.sealed {
		return core.Subscription{}, fmt.Errorf("eventbus: Subscribe called after Seal (EV-009): consumer %q", sub.ConsumerID)
	}
	b.subscriptions = append(b.subscriptions, sub)
	return sub, nil
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

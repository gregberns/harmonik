package eventbus

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gregberns/harmonik/internal/core"
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
// per-handler value-pattern redaction via the [core.RedactionRegistry]
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
// When constructed via [NewBusImplWithSink], a [core.DeadLetterSink]
// is injected at construction time and receives undeliverable events:
//   - Observer/async consumer panics are recorded with reason "observer_panic".
//   - Async/observer consumer dispatch errors are recorded with reason "consumer_error".
//
// When no sink is provided, deadLetterSink holds a [core.NoopDeadLetterSink]
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
// inflight is the process-level in-flight counter: it tracks ALL in-flight
// async/observer goroutines and is used by the existing [EventBus.Drain]
// method to wait for global quiescence. runDrainersMu guards runDrainers, a
// per-run WaitGroup map keyed by run_id string. EmitWithRunID counts each
// dispatched goroutine in both inflight (global) and the run-specific
// WaitGroup so that [busImpl.DrainRun] can wait for a single run's consumers
// without blocking on other runs. Plain Emit (no run_id) only touches
// inflight; DrainRun does not wait for those.
//
// Spec ref: specs/event-model.md §6.1, §4.2 EV-014a, EV-016, EV-035.
// Bead refs: hk-8mup.62, hk-8i31.83, hk-hqwn.19, hk-8mup.63, hk-fx6zl, hk-xvpwb, hk-2m3bq.
type busImpl struct {
	registry       *core.RedactionRegistry
	jsonlWriter    jsonlAppender       // never nil; nullJSONLWriter when no log path configured
	deadLetterSink core.DeadLetterSink // never nil; NoopDeadLetterSink when no sink configured
	idGen          *core.EventIDGenerator
	mu             sync.Mutex
	subscriptions  []core.Subscription
	sealed         bool
	// inflight counts ALL in-flight async/observer goroutines (global
	// quiescence). Guarded by drainMu; Drain(ctx) waits for it to reach 0 via
	// drainCond. EmitWithRunID also increments the per-run entry in
	// runDrainers so DrainRun can wait for just one run (hk-fx6zl).
	inflight int
	// runDrainersMu guards runDrainers.
	runDrainersMu sync.Mutex
	// runDrainers maps run_id → WaitGroup tracking only that run's in-flight
	// goroutines. Entries are created lazily on first EmitWithRunID for a run
	// and are never removed (they become no-ops once the run drains).
	runDrainers map[string]*sync.WaitGroup
	// runSealed marks run_ids whose DrainRun has begun. Once sealed,
	// EmitWithRunID stops per-run WaitGroup tracking for that run so a late
	// emit from a lingering per-run goroutine cannot call runWG.Add
	// concurrently with DrainRun's runWG.Wait — the Go WaitGroup contract
	// violation that aborts the whole process ("fatal error: sync: WaitGroup
	// misuse: Add called concurrently with Wait"). Guarded by runDrainersMu;
	// lazily initialised so the five constructors need no change.
	runSealed map[string]bool

	// drainMu guards inflight and drainCond. Every Emit* dispatch increments
	// inflight under drainMu (addGlobalDrainer) and the goroutine decrements
	// it on exit (doneGlobalDrainer), broadcasting drainCond when the counter
	// reaches 0. Drain waits on drainCond in a `for inflight > 0` loop, so a
	// re-entrant Emit from a still-in-flight handler (a cascade, e.g. the hook
	// dispatcher emitting hook_fired while handling agent_started) bumps the
	// counter and Drain keeps waiting — cascades are fully flushed (hk-okzy1).
	// There is no WaitGroup here, so the H7 crash ("sync: WaitGroup misuse:
	// Add called concurrently with Wait") is structurally impossible, and the
	// bus stays usable after Drain returns (no permanent seal).
	//
	// drainCond is lazily initialised under drainMu (drainCondLocked) so the
	// five constructors need no change.
	drainMu   sync.Mutex
	drainCond *sync.Cond

	// HWM persistence fields (EV-002c). hwmPath is the absolute path of the
	// event_id_hwm file; empty string disables HWM writes (test mode / no
	// project dir). hwmMu serialises concurrent writes; hwmLast tracks the
	// highest value already written so redundant file I/O is skipped.
	hwmPath string
	hwmMu   sync.Mutex
	hwmLast [16]byte

	// jsonlPath is the absolute path of the primary JSONL event log used for
	// replay operations (EV-014d, EV-011). Empty string disables replay
	// (test mode / no project dir configured).
	jsonlPath string
}

// fsyncBoundaryEventTypes is the static set of F-class (fsync-boundary)
// event types derived from the §8 taxonomy table in specs/event-model.md.
// F-class events require Append(line, sync=true) per EV-016 / EV-016a.
//
// This set is exhaustive for the §8 rows marked "F" as of event-model.md
// v0.6.4. Additions to the §8 taxonomy MUST update this map.
//
// Spec ref: specs/event-model.md §4.4 EV-016, EV-016a; §8 taxonomy table.
// Bead ref: hk-8mup.63, hk-uunpf (G1 gap — 15 missing F-class entries added).
var fsyncBoundaryEventTypes = map[core.EventType]struct{}{
	// §8.1 Run lifecycle (F-class rows).
	core.EventType("run_started"):        {},
	core.EventType("run_completed"):      {},
	core.EventType("run_failed"):         {},
	core.EventType("transition_event"):   {},
	core.EventType("checkpoint_written"): {},
	// §8.1a Review-loop lifecycle (F-class rows; added v0.4.0, hk-uunpf G1).
	core.EventTypeReviewerVerdict:         {},
	core.EventTypeReviewLoopCycleComplete: {},
	// §8.2 Control-point lifecycle (F-class rows; added v0.3.4/v0.5.3, hk-uunpf G1).
	core.EventTypePolicyExpressionExceededCost: {},
	core.EventTypeGateDefinitionDrift:          {},
	core.EventTypeGateRedefinedUnderCat6:       {},
	// §8.5 Workspace lifecycle (F-class rows; added v0.5.0, hk-uunpf G1).
	core.EventTypeWorkspaceMergeStatus: {},
	// §8.7 Daemon lifecycle (F-class rows).
	core.EventType("daemon_started"):             {},
	core.EventType("daemon_ready"):               {},
	core.EventType("daemon_shutdown"):            {},
	core.EventType("daemon_startup_failed"):      {},
	core.EventType("operator_upgrade_completed"): {},
	// §8.10 Queue lifecycle (F-class rows; added v0.5.0–v0.5.1, hk-uunpf G1).
	core.EventTypeQueueSubmitted:      {},
	core.EventTypeQueueGroupCompleted: {},
	core.EventTypeQueuePaused:         {},
	core.EventTypeQueueItemReconciled: {},
	// §8.11 Handler-pause lifecycle (F-class rows; added v0.5.2, hk-uunpf G1).
	core.EventTypeHandlerPaused:  {},
	core.EventTypeHandlerResumed: {},
	// §8.12 Daemon escalation (F-class rows; added v0.6.0, hk-uunpf G1).
	core.EventTypeDecisionRequired:     {},
	core.EventTypeDecisionAcknowledged: {},
	// agent-comms §1.1 (hk-djqc9): agent_message is F-class so comms-send is
	// durable before returning OK ("no silent drops" goal G2).
	core.EventType("agent_message"): {},
	// hitl-decisions §1 (hk-33p, K1): the three decision_* events are F-class
	// (SPEC §6 N1, load-bearing) — a lost decision_resolved would leave the
	// blocked agent waiting forever (Risk R1). Distinct from the §8.12
	// decision_required/decision_acknowledged daemon-escalation family.
	core.EventType("decision_needed"):    {},
	core.EventType("decision_resolved"):  {},
	core.EventType("decision_withdrawn"): {},
	// §8.15 Beads adapter (F-class rows; added v0.6.4, hk-uunpf G1).
	core.EventTypeBeadSyncFailed: {},
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
		registry:       core.NewRedactionRegistry(),
		jsonlWriter:    nullJSONLWriter{},
		deadLetterSink: core.NoopDeadLetterSink{},
		idGen:          core.NewEventIDGenerator(),
		runDrainers:    make(map[string]*sync.WaitGroup),
	}
}

// NewBusImplWithRegistry constructs a busImpl that delegates all redaction to
// the supplied [core.RedactionRegistry].
//
// The registry MUST be fully populated (all RegisterPattern calls complete)
// before the bus is sealed per PL-005 step 0. Passing a nil registry is
// equivalent to calling [NewBusImpl].
//
// The returned bus is unsealed; callers MUST call Subscribe for all consumers
// before calling Seal (EV-009). The returned value satisfies [EventBus].
//
// Spec ref: specs/event-model.md §6.1, §4.2 EV-035; specs/handler-contract.md §4.7.HC-032.
func NewBusImplWithRegistry(registry *core.RedactionRegistry) EventBus {
	if registry == nil {
		return NewBusImpl()
	}
	return &busImpl{
		registry:       registry,
		jsonlWriter:    nullJSONLWriter{},
		deadLetterSink: core.NoopDeadLetterSink{},
		idGen:          core.NewEventIDGenerator(),
		runDrainers:    make(map[string]*sync.WaitGroup),
	}
}

// NewBusImplWithWriter constructs a busImpl with both a
// [core.RedactionRegistry] and a [*JSONLWriter] for durable event
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
func NewBusImplWithWriter(registry *core.RedactionRegistry, writer *JSONLWriter) EventBus {
	if registry == nil {
		registry = core.NewRedactionRegistry()
	}
	var w jsonlAppender = nullJSONLWriter{}
	if writer != nil {
		w = writer
	}
	return &busImpl{
		registry:       registry,
		jsonlWriter:    w,
		deadLetterSink: core.NoopDeadLetterSink{},
		idGen:          core.NewEventIDGenerator(),
		runDrainers:    make(map[string]*sync.WaitGroup),
	}
}

// NewBusImplWithSink constructs a busImpl with a [core.RedactionRegistry],
// a [*JSONLWriter], and a [core.DeadLetterSink] for undeliverable events.
//
// Async/observer consumer panics are recorded to sink with reason "observer_panic".
// Async/observer consumer dispatch errors are recorded to sink with reason "consumer_error".
//
// Passing nil for registry, writer, or sink is safe:
//   - nil registry falls back to HC-031-only redaction (same as [NewBusImpl]).
//   - nil writer substitutes a [nullJSONLWriter] (JSONL append is a no-op).
//   - nil sink substitutes a [core.NoopDeadLetterSink] (undeliverable
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
func NewBusImplWithSink(registry *core.RedactionRegistry, writer *JSONLWriter, sink core.DeadLetterSink) EventBus {
	if registry == nil {
		registry = core.NewRedactionRegistry()
	}
	var w jsonlAppender = nullJSONLWriter{}
	if writer != nil {
		w = writer
	}
	if sink == nil {
		sink = core.NoopDeadLetterSink{}
	}
	return &busImpl{
		registry:       registry,
		jsonlWriter:    w,
		deadLetterSink: sink,
		idGen:          core.NewEventIDGenerator(),
		runDrainers:    make(map[string]*sync.WaitGroup),
	}
}

// NewBusImplWithWriterAndHWM constructs a busImpl with a RedactionRegistry, a
// JSONLWriter, a pre-seeded EventIDGenerator, an HWM file path for EV-002c
// cross-restart monotonicity, and a JSONL path for EV-014d startup replay.
//
// gen is the EventIDGenerator to use; when nil, NewEventIDGenerator() is used.
// Callers SHOULD supply a generator seeded from the persisted HWM file via
// [core.NewEventIDGeneratorWithHWM] so that event_ids are strictly greater
// than any pre-restart ids. hwmPath is the absolute path of the
// event_id_hwm file (lifecycle.EventIDHWMPath); when empty, HWM writes are
// disabled (no-op, suitable for unit tests). jsonlPath is the absolute path
// of the primary JSONL event log; when empty, startup replay and ReplayFrom
// are disabled (no-op, suitable for unit tests).
//
// The returned bus is unsealed; callers MUST Subscribe all consumers before
// calling Seal (EV-009).
//
// Spec ref: event-model.md §4.1 EV-002c; §4.3 EV-014d.
func NewBusImplWithWriterAndHWM(
	registry *core.RedactionRegistry,
	writer *JSONLWriter,
	gen *core.EventIDGenerator,
	hwmPath string,
	jsonlPath string,
) EventBus {
	if registry == nil {
		registry = core.NewRedactionRegistry()
	}
	var w jsonlAppender = nullJSONLWriter{}
	if writer != nil {
		w = writer
	}
	if gen == nil {
		gen = core.NewEventIDGenerator()
	}
	return &busImpl{
		registry:       registry,
		jsonlWriter:    w,
		deadLetterSink: core.NoopDeadLetterSink{},
		idGen:          gen,
		hwmPath:        hwmPath,
		jsonlPath:      jsonlPath,
		runDrainers:    make(map[string]*sync.WaitGroup),
	}
}

// maybeUpdateHWM writes hwm to the HWM file when hwm is strictly greater than
// the last persisted value. Called after every F-class JSONL fsync to keep
// the HWM file current per EV-002c.
//
// The write is atomic (temp-file + rename) but not fsynced; HWM durability
// piggybacks on the JSONL fsync domain (EV-002c "no additional fsync cost").
// On crash between JSONL fsync and this write the HWM file may be stale;
// daemon startup handles that via the "seed from wall clock" fallback.
//
// Thread-safe: protected by hwmMu.
// Non-fatal: write errors are logged but do not fail the Emit call.
func (b *busImpl) maybeUpdateHWM(hwm core.EventID) {
	if b.hwmPath == "" {
		return
	}
	hwmBytes := [16]byte(hwm)
	b.hwmMu.Lock()
	defer b.hwmMu.Unlock()
	if bytes.Compare(hwmBytes[:], b.hwmLast[:]) <= 0 {
		return
	}
	if err := core.WriteEventIDHWMAtomicNoSync(b.hwmPath, hwm); err != nil {
		log.Printf("eventbus: HWM update to %s failed: %v", b.hwmPath, err)
		return
	}
	b.hwmLast = hwmBytes
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
	// EV-002b will own this). schema_version is taken from the per-type registry
	// per EV-028 so it matches the declared payload version for this event type.
	eventID, idErr := b.idGen.Next()
	if idErr != nil {
		return fmt.Errorf("eventbus.Emit: generate event_id: %w", idErr)
	}
	typeSchemaVersion, knownType := core.LookupTypeSchemaVersion(string(eventType))
	if !knownType {
		// Unknown type: fall back to version 1 rather than failing here; the
		// registry-coverage sensor (EV-034) catches unregistered types at startup.
		typeSchemaVersion = 1
	}
	now := time.Now().UTC()
	evt := core.Event{
		EventID:         eventID,
		SchemaVersion:   typeSchemaVersion,
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
	if needsSync {
		b.maybeUpdateHWM(eventID)
	}

	// Step 5: collect matching subscriptions once under lock so dispatch runs
	// without holding the mutex.
	b.mu.Lock()
	subs := make([]core.Subscription, len(b.subscriptions))
	copy(subs, b.subscriptions)
	b.mu.Unlock()

	// Step 6: dispatch per consumer class (EV-014a).
	for _, sub := range subs {
		if !sub.EventPattern.MatchesType(eventType) {
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
			b.addGlobalDrainer()
			go func() {
				defer b.doneGlobalDrainer()
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
	typeSchemaVersionWithRun, knownTypeWithRun := core.LookupTypeSchemaVersion(string(eventType))
	if !knownTypeWithRun {
		typeSchemaVersionWithRun = 1
	}
	now := time.Now().UTC()
	runIDVal := runID
	evt := core.Event{
		EventID:         eventID,
		SchemaVersion:   typeSchemaVersionWithRun,
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
	if needsSync {
		b.maybeUpdateHWM(eventID)
	}

	// Step 5: collect matching subscriptions once under lock.
	b.mu.Lock()
	subs := make([]core.Subscription, len(b.subscriptions))
	copy(subs, b.subscriptions)
	b.mu.Unlock()

	// Step 6: dispatch per consumer class (EV-014a).
	for _, sub := range subs {
		if !sub.EventPattern.MatchesType(eventType) {
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
			// tracked in BOTH the global inflight counter (for Drain/global
			// quiescence) and the per-run WaitGroup (for DrainRun/per-run fair
			// termination). Bead: hk-fx6zl.
			sub := sub // capture loop variable
			// addRunDrainer does runWG.Add(1) while holding runDrainersMu, so it
			// cannot race a DrainRun that has already sealed this run; it returns
			// nil when the run is sealed (teardown in progress), in which case we
			// skip per-run tracking (the global inflight counter still accounts
			// for the goroutine).
			runWG := b.addRunDrainer(evt.RunID.String())
			b.addGlobalDrainer()
			go func() {
				defer b.doneGlobalDrainer()
				if runWG != nil {
					defer runWG.Done()
				}
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

// EmitAgentMessage emits an agent_message event and returns the minted event_id.
//
// This satisfies [CommsMessageEmitter] for the comms-send socket op (agent-comms
// spec §2.1 C2, bead hk-nbrmf). It mirrors [Emit] but returns the event_id so
// the caller can relay it to the CLI — the [EventBus.Emit] signature does not
// return the ID, so this separate method is used instead of modifying the interface.
//
// agent_message is F-class (fsync-boundary per fsyncBoundaryEventTypes), so the
// JSONL append is fsynced before this method returns, satisfying the "no silent
// drops" guarantee (G2, agent-comms spec §1.1).
func (b *busImpl) EmitAgentMessage(ctx context.Context, payload core.AgentMessagePayload) (core.EventID, error) {
	payloadBytes, marshalErr := json.Marshal(payload)
	if marshalErr != nil {
		return core.EventID{}, fmt.Errorf("eventbus.EmitAgentMessage: marshal payload: %w", marshalErr)
	}

	// Steps 1–3: redaction pipeline (EV-035) — same as Emit.
	var rawPayload map[string]any
	if err := json.Unmarshal(payloadBytes, &rawPayload); err != nil {
		return core.EventID{}, fmt.Errorf("eventbus.EmitAgentMessage: payload unmarshal for redaction: %w", err)
	}
	redacted := b.registry.RedactionMiddleware(rawPayload)
	redactedBytes, err := json.Marshal(redacted)
	if err != nil {
		return core.EventID{}, fmt.Errorf("eventbus.EmitAgentMessage: re-encoding redacted payload: %w", err)
	}

	// Step 4a: generate event_id BEFORE building the envelope so we can return it.
	eventID, idErr := b.idGen.Next()
	if idErr != nil {
		return core.EventID{}, fmt.Errorf("eventbus.EmitAgentMessage: generate event_id: %w", idErr)
	}
	const agentMessageType = "agent_message"
	typeSchemaVersion, knownType := core.LookupTypeSchemaVersion(agentMessageType)
	if !knownType {
		typeSchemaVersion = 1
	}
	evt := core.Event{
		EventID:         eventID,
		SchemaVersion:   typeSchemaVersion,
		Type:            agentMessageType,
		TimestampWall:   time.Now().UTC(),
		SourceSubsystem: "eventbus",
		Payload:         redactedBytes,
	}

	// Step 4b: JSONL append with fsync (F-class per fsyncBoundaryEventTypes).
	envelopeBytes, marshalEnvErr := json.Marshal(evt)
	if marshalEnvErr != nil {
		return core.EventID{}, fmt.Errorf("eventbus.EmitAgentMessage: marshal envelope: %w", marshalEnvErr)
	}
	if appendErr := b.jsonlWriter.Append(envelopeBytes, true /* always fsync — F-class */); appendErr != nil {
		return core.EventID{}, fmt.Errorf("eventbus.EmitAgentMessage: JSONL append: %w", appendErr)
	}
	b.maybeUpdateHWM(eventID)

	// Steps 5–6: fan-out to subscribers — same pattern as Emit (no run_id, no runWG).
	b.mu.Lock()
	subs := make([]core.Subscription, len(b.subscriptions))
	copy(subs, b.subscriptions)
	b.mu.Unlock()

	for _, sub := range subs {
		if !sub.EventPattern.MatchesType(core.EventType(agentMessageType)) {
			continue
		}
		if sub.Handler == nil {
			continue
		}
		switch sub.ConsumerClass {
		case core.ConsumerClassSynchronous:
			if handlerErr := sub.Handler(ctx, evt); handlerErr != nil {
				return core.EventID{}, fmt.Errorf("eventbus.EmitAgentMessage: synchronous consumer %q: %w", sub.ConsumerID, handlerErr)
			}
		default:
			sub := sub // capture loop variable
			b.addGlobalDrainer()
			go func() {
				defer b.doneGlobalDrainer()
				defer func() {
					if r := recover(); r != nil {
						_ = b.deadLetterSink.Record(ctx, evt, "observer_panic")
					}
				}()
				if handlerErr := sub.Handler(ctx, evt); handlerErr != nil {
					_ = b.deadLetterSink.Record(ctx, evt, "consumer_error")
				}
			}()
		}
	}
	return eventID, nil
}

// EmitAgentPresence emits an agent_presence event and returns the minted event_id.
//
// This satisfies [CommsPresenceEmitter] for the comms-presence socket op (agent-comms
// spec §2.5 C6, bead hk-7t27s). It mirrors [EmitAgentMessage] but emits O-class
// (ordinary durability) — agent_presence is NOT in fsyncBoundaryEventTypes because
// losing a refresh beat on crash is harmless; the TTL projection reconciles it
// (spec §4 / Q2 / §1.2).
func (b *busImpl) EmitAgentPresence(ctx context.Context, payload core.AgentPresencePayload) (core.EventID, error) {
	payloadBytes, marshalErr := json.Marshal(payload)
	if marshalErr != nil {
		return core.EventID{}, fmt.Errorf("eventbus.EmitAgentPresence: marshal payload: %w", marshalErr)
	}

	// Redaction pipeline (EV-035) — same as EmitAgentMessage.
	var rawPayload map[string]any
	if err := json.Unmarshal(payloadBytes, &rawPayload); err != nil {
		return core.EventID{}, fmt.Errorf("eventbus.EmitAgentPresence: payload unmarshal for redaction: %w", err)
	}
	redacted := b.registry.RedactionMiddleware(rawPayload)
	redactedBytes, err := json.Marshal(redacted)
	if err != nil {
		return core.EventID{}, fmt.Errorf("eventbus.EmitAgentPresence: re-encoding redacted payload: %w", err)
	}

	// Generate event_id before building the envelope so we can return it.
	eventID, idErr := b.idGen.Next()
	if idErr != nil {
		return core.EventID{}, fmt.Errorf("eventbus.EmitAgentPresence: generate event_id: %w", idErr)
	}
	const agentPresenceType = "agent_presence"
	typeSchemaVersion, knownType := core.LookupTypeSchemaVersion(agentPresenceType)
	if !knownType {
		typeSchemaVersion = 1
	}
	evt := core.Event{
		EventID:         eventID,
		SchemaVersion:   typeSchemaVersion,
		Type:            agentPresenceType,
		TimestampWall:   time.Now().UTC(),
		SourceSubsystem: "eventbus",
		Payload:         redactedBytes,
	}

	// JSONL append — O-class: fsync=false (agent_presence is not fsync-boundary).
	// Refresh beats are not persisted: only join/leave edges carry signal worth
	// storing (logmine TA3 noise cut; in-memory fan-out below still fires for
	// "comms who" TTL projection). Refs: hk-ubp1.
	if payload.Reason != core.AgentPresenceReasonRefresh {
		envelopeBytes, marshalEnvErr := json.Marshal(evt)
		if marshalEnvErr != nil {
			return core.EventID{}, fmt.Errorf("eventbus.EmitAgentPresence: marshal envelope: %w", marshalEnvErr)
		}
		if appendErr := b.jsonlWriter.Append(envelopeBytes, false /* O-class: no fsync */); appendErr != nil {
			return core.EventID{}, fmt.Errorf("eventbus.EmitAgentPresence: JSONL append: %w", appendErr)
		}
	}

	// Fan-out to subscribers.
	b.mu.Lock()
	subs := make([]core.Subscription, len(b.subscriptions))
	copy(subs, b.subscriptions)
	b.mu.Unlock()

	for _, sub := range subs {
		if !sub.EventPattern.MatchesType(core.EventType(agentPresenceType)) {
			continue
		}
		if sub.Handler == nil {
			continue
		}
		switch sub.ConsumerClass {
		case core.ConsumerClassSynchronous:
			if handlerErr := sub.Handler(ctx, evt); handlerErr != nil {
				return core.EventID{}, fmt.Errorf("eventbus.EmitAgentPresence: synchronous consumer %q: %w", sub.ConsumerID, handlerErr)
			}
		default:
			sub := sub
			b.addGlobalDrainer()
			go func() {
				defer b.doneGlobalDrainer()
				defer func() {
					if r := recover(); r != nil {
						_ = b.deadLetterSink.Record(ctx, evt, "observer_panic")
					}
				}()
				if handlerErr := sub.Handler(ctx, evt); handlerErr != nil {
					_ = b.deadLetterSink.Record(ctx, evt, "consumer_error")
				}
			}()
		}
	}
	return eventID, nil
}

// EmitTyped emits an event of the given type carrying the JSON-marshalled
// payload and returns the minted event_id.
//
// This satisfies [TypedEmitter]. It generalises [EmitAgentMessage] /
// [EmitAgentPresence]: it generates the event_id BEFORE building the envelope
// (so the caller can relay it back to the CLI — the base [EventBus.Emit] returns
// only error), applies the EV-035 redaction pipeline, appends the envelope to
// the durable JSONL with fsync derived from the §8 taxonomy via
// [isFsyncBoundaryEvent] (F-class types are fsync'd before return), and fans out
// to subscribers identically to Emit.
//
// It is used by the hitl-decisions emit ops (decisions-raise →
// decision_needed, decisions-withdraw → decision_withdrawn, decisions-answer →
// decision_resolved): all three are F-class (busimpl.go:fsyncBoundaryEventTypes,
// hitl-decisions SPEC §6 N1), so the decision landmark is durable before the
// blocked agent can act on it (Risk R1).
//
// payload is the already-JSON-encoded event payload (e.g. the marshalled
// core.DecisionNeededPayload). The decision_id a caller returns to the agent is
// the minted event_id's canonical string form (hitl-decisions SPEC §1).
func (b *busImpl) EmitTyped(ctx context.Context, eventType core.EventType, payload []byte) (core.EventID, error) {
	typeName := string(eventType)

	// Step 1–3: redaction pipeline (EV-035) — same as EmitAgentMessage.
	var rawPayload map[string]any
	if err := json.Unmarshal(payload, &rawPayload); err != nil {
		return core.EventID{}, fmt.Errorf("eventbus.EmitTyped(%s): payload unmarshal for redaction: %w", typeName, err)
	}
	redacted := b.registry.RedactionMiddleware(rawPayload)
	redactedBytes, err := json.Marshal(redacted)
	if err != nil {
		return core.EventID{}, fmt.Errorf("eventbus.EmitTyped(%s): re-encoding redacted payload: %w", typeName, err)
	}

	// Step 4a: generate event_id BEFORE building the envelope so we can return it.
	eventID, idErr := b.idGen.Next()
	if idErr != nil {
		return core.EventID{}, fmt.Errorf("eventbus.EmitTyped(%s): generate event_id: %w", typeName, idErr)
	}
	typeSchemaVersion, knownType := core.LookupTypeSchemaVersion(typeName)
	if !knownType {
		typeSchemaVersion = 1
	}
	evt := core.Event{
		EventID:         eventID,
		SchemaVersion:   typeSchemaVersion,
		Type:            typeName,
		TimestampWall:   time.Now().UTC(),
		SourceSubsystem: "eventbus",
		Payload:         redactedBytes,
	}

	// Step 4b: JSONL append. Fsync iff the type is F-class per the §8 taxonomy.
	envelopeBytes, marshalEnvErr := json.Marshal(evt)
	if marshalEnvErr != nil {
		return core.EventID{}, fmt.Errorf("eventbus.EmitTyped(%s): marshal envelope: %w", typeName, marshalEnvErr)
	}
	fsync := isFsyncBoundaryEvent(eventType)
	if appendErr := b.jsonlWriter.Append(envelopeBytes, fsync); appendErr != nil {
		return core.EventID{}, fmt.Errorf("eventbus.EmitTyped(%s): JSONL append: %w", typeName, appendErr)
	}
	if fsync {
		b.maybeUpdateHWM(eventID)
	}

	// Step 5–6: fan-out to subscribers — same pattern as EmitAgentMessage.
	b.mu.Lock()
	subs := make([]core.Subscription, len(b.subscriptions))
	copy(subs, b.subscriptions)
	b.mu.Unlock()

	for _, sub := range subs {
		if !sub.EventPattern.MatchesType(eventType) {
			continue
		}
		if sub.Handler == nil {
			continue
		}
		switch sub.ConsumerClass {
		case core.ConsumerClassSynchronous:
			if handlerErr := sub.Handler(ctx, evt); handlerErr != nil {
				return core.EventID{}, fmt.Errorf("eventbus.EmitTyped(%s): synchronous consumer %q: %w", typeName, sub.ConsumerID, handlerErr)
			}
		default:
			sub := sub // capture loop variable
			b.addGlobalDrainer()
			go func() {
				defer b.doneGlobalDrainer()
				defer func() {
					if r := recover(); r != nil {
						_ = b.deadLetterSink.Record(ctx, evt, "observer_panic")
					}
				}()
				if handlerErr := sub.Handler(ctx, evt); handlerErr != nil {
					_ = b.deadLetterSink.Record(ctx, evt, "consumer_error")
				}
			}()
		}
	}
	return eventID, nil
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
			return string(t), true
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
		if incoming.EventPattern.MatchesType(emitType) {
			return append(path, incoming.ConsumerID)
		}

		// Find existing synchronous consumers that subscribe to emitType and
		// follow their declared emissions.
		for _, s := range allSync {
			if s.ConsumerID == incoming.ConsumerID {
				continue
			}
			if !s.EventPattern.MatchesType(emitType) {
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

// Seal closes the subscription-registration window and runs the EV-014d
// startup replay phase for every consumer whose Since or
// OffsetCheckpointEventID is non-nil.
//
// Replay is synchronous and completes before Seal returns, so live-stream
// delivery (the first Emit after Seal) never races with replay events.
// Synchronous consumers are skipped (EV-014d: their critical-path contract
// ended when the producer returned from Emit; re-invoking risks double
// side-effects).
//
// Spec ref: specs/event-model.md §6.1, §4.2 EV-009, EV-014d.
func (b *busImpl) Seal() error {
	b.mu.Lock()
	b.sealed = true
	// Copy subscriptions while holding the lock so replay sees a stable
	// snapshot without holding the mutex across potentially slow handler calls.
	subs := make([]core.Subscription, len(b.subscriptions))
	copy(subs, b.subscriptions)
	b.mu.Unlock()

	if b.jsonlPath == "" {
		return nil // no JSONL path; startup replay disabled (test / no project dir)
	}

	ctx := context.Background()
	for _, sub := range subs {
		var effectiveSince core.EventID
		switch {
		case sub.Since != nil:
			effectiveSince = *sub.Since
		case sub.OffsetCheckpointEventID != nil:
			effectiveSince = *sub.OffsetCheckpointEventID
		default:
			continue // no replay checkpoint; consumer starts from live stream
		}
		if sub.ConsumerClass == core.ConsumerClassSynchronous {
			continue // synchronous consumers do not participate in replay per EV-014d
		}
		lastDurable, truncated, err := replayAndDetectTrunc(ctx, b.jsonlPath, effectiveSince, func(ctx context.Context, ev core.Event) error {
			if !sub.EventPattern.MatchesType(core.EventType(ev.Type)) {
				return nil
			}
			return sub.Handler(ctx, ev)
		})
		if err != nil {
			log.Printf("eventbus: startup replay for consumer %q failed: %v", sub.ConsumerID, err)
		}
		if truncated && sub.OnTailTruncation != nil {
			sub.OnTailTruncation(ctx, lastDurable)
		}
	}
	return nil
}

// ReplayFrom re-issues JSONL events whose event_id is strictly greater than
// since to the named consumer's handler, filtered by the consumer's
// EventPattern. Synchronous consumers are skipped (EV-014d). A missing JSONL
// path is a no-op (test/no-project-dir mode).
//
// Spec ref: specs/event-model.md §6.1, §4.2 EV-014b.
func (b *busImpl) ReplayFrom(consumerID string, since core.EventID) error {
	if b.jsonlPath == "" {
		return nil
	}

	b.mu.Lock()
	var found *core.Subscription
	for i := range b.subscriptions {
		if b.subscriptions[i].ConsumerID == consumerID {
			s := b.subscriptions[i]
			found = &s
			break
		}
	}
	b.mu.Unlock()

	if found == nil {
		return fmt.Errorf("eventbus.ReplayFrom: consumer %q not registered", consumerID)
	}
	if found.ConsumerClass == core.ConsumerClassSynchronous {
		return nil
	}

	ctx := context.Background()
	_, _, err := replayAndDetectTrunc(ctx, b.jsonlPath, since, func(ctx context.Context, ev core.Event) error {
		if !found.EventPattern.MatchesType(core.EventType(ev.Type)) {
			return nil
		}
		return found.Handler(ctx, ev)
	})
	return err
}

// deadLetterEntry is the JSON shape of one entry in the dead-letter JSONL file.
// Mirrors the unexported deadLetterRecord written by core.jsonlDeadLetterSink.
type deadLetterEntry struct {
	Envelope core.Event `json:"envelope"`
}

// DeadLetterReplay replays events from the dead-letter log to the named
// consumer. filter, when non-nil, constrains which event types are replayed;
// nil replays all dead-letter entries that match the consumer's EventPattern.
// A missing dead-letter file or JSONL path is a no-op.
//
// Spec ref: specs/event-model.md §6.1, §6.2, §4.2 EV-011, EV-014b.
func (b *busImpl) DeadLetterReplay(consumerName string, filter *core.EventPattern) error {
	if b.jsonlPath == "" {
		return nil
	}

	b.mu.Lock()
	var found *core.Subscription
	for i := range b.subscriptions {
		if b.subscriptions[i].ConsumerID == consumerName {
			s := b.subscriptions[i]
			found = &s
			break
		}
	}
	b.mu.Unlock()

	if found == nil {
		return fmt.Errorf("eventbus.DeadLetterReplay: consumer %q not registered", consumerName)
	}
	if found.ConsumerClass == core.ConsumerClassSynchronous {
		return nil
	}

	dlPath := filepath.Join(filepath.Dir(b.jsonlPath), "dead-letters.jsonl")
	//nolint:gosec // G304: path is derived from daemon-startup-resolved jsonlPath; not user input.
	f, err := os.Open(dlPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("eventbus.DeadLetterReplay: open %s: %w", dlPath, err)
	}
	defer func() { _ = f.Close() }()

	ctx := context.Background()
	reader := bufio.NewReader(f)
	for {
		lineBytes, readErr := reader.ReadBytes('\n')
		if len(lineBytes) > 0 {
			var entry deadLetterEntry
			if decodeErr := json.Unmarshal(bytes.TrimRight(lineBytes, "\n"), &entry); decodeErr != nil {
				log.Printf("eventbus.DeadLetterReplay: malformed line (skipping): %v", decodeErr)
			} else {
				evType := core.EventType(entry.Envelope.Type)
				matches := found.EventPattern.MatchesType(evType)
				if matches && filter != nil {
					matches = filter.MatchesType(evType)
				}
				if matches {
					if handlerErr := found.Handler(ctx, entry.Envelope); handlerErr != nil {
						return fmt.Errorf("eventbus.DeadLetterReplay: consumer %q: %w", consumerName, handlerErr)
					}
				}
			}
		}
		if readErr == io.EOF {
			return nil
		}
		if readErr != nil {
			return fmt.Errorf("eventbus.DeadLetterReplay: read %s: %w", dlPath, readErr)
		}
	}
}

// replayAndDetectTrunc scans the JSONL file at path for events with event_id
// strictly greater than sinceID (UUIDv7 byte-lexicographic order per EV-002),
// dispatches them in file order to handler, and detects a torn tail.
//
// Returns:
//   - lastDurable: event_id of the last successfully parsed event (zero if none)
//   - tailTruncated: true when the file ends with a partial line without '\n'
//   - err: first handler error or scan error; a torn tail is not an error
//
// A missing file is treated as an empty log (no error, no events, no truncation).
//
//nolint:gosec // G304: path is daemon-startup-resolved; not user input.
func replayAndDetectTrunc(ctx context.Context, path string, sinceID core.EventID, handler func(context.Context, core.Event) error) (lastDurable core.EventID, tailTruncated bool, err error) {
	f, openErr := os.Open(path)
	if openErr != nil {
		if os.IsNotExist(openErr) {
			return core.EventID{}, false, nil
		}
		return core.EventID{}, false, fmt.Errorf("eventbus: open %s: %w", path, openErr)
	}
	defer func() { _ = f.Close() }()

	since := [16]byte(sinceID)
	reader := bufio.NewReader(f)
	for {
		lineBytes, readErr := reader.ReadBytes('\n')
		if len(lineBytes) > 0 {
			hasTerm := lineBytes[len(lineBytes)-1] == '\n'
			if !hasTerm && readErr == io.EOF {
				// Torn tail: non-empty partial line at EOF without newline terminator.
				return lastDurable, true, nil
			}
			trimmed := bytes.TrimRight(lineBytes, "\n")
			var ev core.Event
			if decodeErr := json.Unmarshal(trimmed, &ev); decodeErr != nil {
				log.Printf("eventbus: malformed line in %s (skipping): %v", path, decodeErr)
			} else {
				lastDurable = ev.EventID
				evUID := [16]byte(ev.EventID)
				if bytes.Compare(evUID[:], since[:]) > 0 {
					if handlerErr := handler(ctx, ev); handlerErr != nil {
						return lastDurable, false, handlerErr
					}
				}
			}
		}
		if readErr == io.EOF {
			return lastDurable, false, nil
		}
		if readErr != nil {
			return lastDurable, false, fmt.Errorf("eventbus: read %s: %w", path, readErr)
		}
	}
}

// addGlobalDrainer registers one in-flight async/observer dispatch goroutine
// by incrementing b.inflight under drainMu. Every registration is tracked —
// including re-entrant emits from handlers running during a Drain — so Drain
// waits for cascades (hk-okzy1). The caller must arrange a matching
// doneGlobalDrainer (typically `defer b.doneGlobalDrainer()` inside the
// dispatch goroutine). Because Drain waits on a condition variable rather
// than a WaitGroup, incrementing during a Drain is safe (no H7-style
// "Add called concurrently with Wait" hazard).
func (b *busImpl) addGlobalDrainer() {
	b.drainMu.Lock()
	b.inflight++
	b.drainMu.Unlock()
}

// doneGlobalDrainer is the matching decrement for addGlobalDrainer. When the
// counter reaches 0 it broadcasts drainCond so any Drain waiter re-checks
// quiescence.
func (b *busImpl) doneGlobalDrainer() {
	b.drainMu.Lock()
	b.inflight--
	if b.inflight == 0 && b.drainCond != nil {
		b.drainCond.Broadcast()
	}
	b.drainMu.Unlock()
}

// drainCondLocked returns b.drainCond, lazily initialising it. The caller
// MUST hold drainMu.
func (b *busImpl) drainCondLocked() *sync.Cond {
	if b.drainCond == nil {
		b.drainCond = sync.NewCond(&b.drainMu)
	}
	return b.drainCond
}

// addRunDrainer registers one in-flight per-run goroutine: it returns the
// run's WaitGroup after calling Add(1) on it WHILE HOLDING runDrainersMu, so
// the Add is serialised against DrainRun's seal-then-Wait. It returns nil when
// the run is already sealed (DrainRun has begun) — the caller then skips
// per-run Done, relying on the global inflight counter. Doing Add under the lock is what
// prevents "Add called concurrently with Wait".
func (b *busImpl) addRunDrainer(runID string) *sync.WaitGroup {
	b.runDrainersMu.Lock()
	defer b.runDrainersMu.Unlock()
	if b.runSealed[runID] {
		return nil
	}
	wg, ok := b.runDrainers[runID]
	if !ok {
		wg = &sync.WaitGroup{}
		b.runDrainers[runID] = wg
	}
	wg.Add(1)
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
	// Seal the run under runDrainersMu, then read its WaitGroup. After the seal
	// is set, addRunDrainer returns nil for this run (no further Add); any Add
	// already performed happened under the same lock before this point, so it
	// happens-before the Wait below. This closes the Add-concurrent-with-Wait
	// race that otherwise aborts the process under concurrent dispatch.
	b.runDrainersMu.Lock()
	if b.runSealed == nil {
		b.runSealed = make(map[string]bool)
	}
	b.runSealed[runID.String()] = true
	wg := b.runDrainers[runID.String()]
	b.runDrainersMu.Unlock()
	if wg == nil {
		// No per-run goroutine was ever tracked for this run — nothing to wait on.
		return nil
	}
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
	// Wait for the global in-flight counter to reach 0 via drainCond. Because
	// cond.Wait releases drainMu while parked, a re-entrant Emit from a
	// still-in-flight handler (a cascade) can increment the counter during the
	// wait; the loop re-checks after every broadcast, so Drain returns only at
	// true quiescence — cascade descendants included (hk-okzy1). No WaitGroup
	// is involved, so the H7 "Add called concurrently with Wait" crash cannot
	// occur, and the bus remains fully usable after Drain returns.
	//
	// ctx cancellation: sync.Cond has no context-aware wait, so the wait loop
	// runs in a goroutine that closes done at quiescence. On ctx cancellation
	// we close stop and broadcast so the waiter wakes, observes stop, and
	// exits — it cannot block forever after cancellation.
	done := make(chan struct{})
	stop := make(chan struct{})
	go func() {
		b.drainMu.Lock()
		cond := b.drainCondLocked()
		for b.inflight > 0 {
			select {
			case <-stop:
				b.drainMu.Unlock()
				return
			default:
			}
			cond.Wait()
		}
		b.drainMu.Unlock()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		close(stop)
		b.drainMu.Lock()
		b.drainCondLocked().Broadcast()
		b.drainMu.Unlock()
		return fmt.Errorf("eventbus.Drain: %w", ctx.Err())
	}
}

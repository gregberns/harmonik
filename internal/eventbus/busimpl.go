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

type jsonlAppender interface {
	Append(line []byte, sync bool) error
}

type nullJSONLWriter struct{}

func (nullJSONLWriter) Append(_ []byte, _ bool) error { return nil }

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
	// runInflight so DrainRun can wait for just one run (hk-fx6zl).
	inflight int
	// runDrainersMu guards runInflight and runDrainCond.
	runDrainersMu sync.Mutex
	// runInflight maps run_id → count of that run's in-flight async/observer
	// goroutines. addRunDrainer increments the entry under runDrainersMu and
	// the dispatch goroutine decrements it via doneRunDrainer on exit,
	// broadcasting runDrainCond and deleting the entry when the count reaches
	// 0. Because DrainRun waits on runDrainCond in a `for runInflight[id] > 0`
	// loop rather than on a per-run WaitGroup, a re-entrant EmitWithRunID from
	// a still-in-flight handler during that run's DrainRun (a run-scoped
	// cascade) bumps the counter and DrainRun keeps waiting — the run's
	// cascade tail is fully delivered (hk-4hctu). There is no per-run
	// WaitGroup, so the H7 crash ("sync: WaitGroup misuse: Add called
	// concurrently with Wait") is structurally impossible; no seal is needed.
	runInflight map[string]int
	// runDrainCond is the shared condition variable DrainRun waits on; a
	// broadcast wakes every DrainRun waiter, each of which re-checks its own
	// run's counter. Lazily initialised under runDrainersMu (runDrainCondLocked).
	runDrainCond *sync.Cond

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

func (b *busImpl) recordDeadLetter(ctx context.Context, evt core.Event, reason string) {
	if err := b.deadLetterSink.Record(ctx, evt, reason); err != nil {
		log.Printf("eventbus: record dead letter for event %s (%s): %v", evt.EventID, reason, err)
	}
}

var fsyncBoundaryEventTypes = map[core.EventType]struct{}{
	core.EventTypeRunStarted:        {},
	core.EventTypeRunCompleted:      {},
	core.EventTypeRunFailed:         {},
	core.EventTypeTransitionEvent:   {},
	core.EventTypeCheckpointWritten: {},
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
	core.EventTypeDaemonStarted:            {},
	core.EventTypeDaemonReady:              {},
	core.EventTypeDaemonShutdown:           {},
	core.EventTypeDaemonStartupFailed:      {},
	core.EventTypeOperatorUpgradeCompleted: {},
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
	core.EventTypeAgentMessage: {},
	// hitl-decisions §1 (hk-33p, K1): the three decision_* events are F-class
	// (SPEC §6 N1, load-bearing) — a lost decision_resolved would leave the
	// blocked agent waiting forever (Risk R1). Distinct from the §8.12
	// decision_required/decision_acknowledged daemon-escalation family.
	core.EventTypeDecisionNeeded:    {},
	core.EventTypeDecisionResolved:  {},
	core.EventTypeDecisionWithdrawn: {},
	// §8.15 Beads adapter (F-class rows; added v0.6.4, hk-uunpf G1).
	core.EventTypeBeadSyncFailed: {},
}

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
		runInflight:    make(map[string]int),
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
		runInflight:    make(map[string]int),
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
		runInflight:    make(map[string]int),
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
// EARLY_ROADMAP row #9 dead-letter wiring is active.
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
		runInflight:    make(map[string]int),
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
		runInflight:    make(map[string]int),
	}
}

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
// RedactionMiddleware, then appends the event to the JSONL log (currently a stub),
// then dispatches to matching registered consumers per EV-014a.
//
// Dispatch order (EV-014a):
//
//  1. Redaction via HC-031 + HC-032 registry (EV-035).
//  2. JSONL append + fsync per durability class (EV-016). Deferred stub:
//     file path not yet threaded through daemon.Config; follow-up bead adds wiring.
//  3. Synchronous-consumer dispatch on the caller's goroutine — Emit blocks until
//     the at-most-one synchronous consumer returns or errors (EV-010).
//  4. Asynchronous and observer consumers are dispatched off the critical path
//     via per-handler goroutines (later: bounded worker pool) and MUST
//     NOT extend Emit latency (EV-014a).
//
// Spec ref: specs/event-model.md §6.1, §7.1, §4.2 EV-014a, §4.4 EV-035.
func (b *busImpl) Emit(ctx context.Context, eventType core.EventType, payload []byte) error {
	var rawPayload map[string]any
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &rawPayload); err != nil {
			return fmt.Errorf("eventbus.Emit: payload unmarshal for redaction: %w", err)
		}
	}

	redacted := b.registry.RedactionMiddleware(rawPayload)

	redactedBytes, err := json.Marshal(redacted)
	if err != nil {
		return fmt.Errorf("eventbus.Emit: re-encoding redacted payload: %w", err)
	}

	eventID, idErr := b.idGen.Next()
	if idErr != nil {
		return fmt.Errorf("eventbus.Emit: generate event_id: %w", idErr)
	}
	typeSchemaVersion, knownType := core.LookupTypeSchemaVersion(eventType)
	if !knownType {
		return fmt.Errorf("eventbus.Emit: %w: %q", core.ErrUnknownEventType, eventType)
	}
	now := time.Now().UTC()
	evt := core.Event{
		EventID:         eventID,
		SchemaVersion:   typeSchemaVersion,
		Type:            eventType,
		TimestampWall:   now,
		SourceSubsystem: "eventbus",
		Payload:         redactedBytes,
	}

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
				return fmt.Errorf("eventbus.Emit: synchronous consumer %q: %w", sub.ConsumerID, handlerErr)
			}

		default:
			sub := sub // capture loop variable
			b.addGlobalDrainer()
			go func() {
				defer b.doneGlobalDrainer()
				defer func() {
					if r := recover(); r != nil {
						b.recordDeadLetter(ctx, evt, "observer_panic")
					}
				}()
				if handlerErr := sub.Handler(ctx, evt); handlerErr != nil {
					b.recordDeadLetter(ctx, evt, "consumer_error")
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
	var rawPayload map[string]any
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &rawPayload); err != nil {
			return fmt.Errorf("eventbus.EmitWithRunID: payload unmarshal for redaction: %w", err)
		}
	}

	redacted := b.registry.RedactionMiddleware(rawPayload)

	redactedBytes, err := json.Marshal(redacted)
	if err != nil {
		return fmt.Errorf("eventbus.EmitWithRunID: re-encoding redacted payload: %w", err)
	}

	eventID, idErr := b.idGen.Next()
	if idErr != nil {
		return fmt.Errorf("eventbus.EmitWithRunID: generate event_id: %w", idErr)
	}
	typeSchemaVersionWithRun, knownTypeWithRun := core.LookupTypeSchemaVersion(eventType)
	if !knownTypeWithRun {
		return fmt.Errorf("eventbus.EmitWithRunID: %w: %q", core.ErrUnknownEventType, eventType)
	}
	now := time.Now().UTC()
	runIDVal := runID
	evt := core.Event{
		EventID:         eventID,
		SchemaVersion:   typeSchemaVersionWithRun,
		Type:            eventType,
		TimestampWall:   now,
		RunID:           &runIDVal,
		SourceSubsystem: "eventbus",
		Payload:         redactedBytes,
	}

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
				return fmt.Errorf("eventbus.EmitWithRunID: synchronous consumer %q: %w", sub.ConsumerID, handlerErr)
			}
		default:
			sub := sub // capture loop variable
			runKey := evt.RunID.String()
			b.addRunDrainer(runKey)
			b.addGlobalDrainer()
			go func() {
				defer b.doneGlobalDrainer()
				defer b.doneRunDrainer(runKey)
				defer func() {
					if r := recover(); r != nil {
						b.recordDeadLetter(ctx, evt, "observer_panic")
					}
				}()
				if handlerErr := sub.Handler(ctx, evt); handlerErr != nil {
					b.recordDeadLetter(ctx, evt, "consumer_error")
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

	var rawPayload map[string]any
	if err := json.Unmarshal(payloadBytes, &rawPayload); err != nil {
		return core.EventID{}, fmt.Errorf("eventbus.EmitAgentMessage: payload unmarshal for redaction: %w", err)
	}
	redacted := b.registry.RedactionMiddleware(rawPayload)
	redactedBytes, err := json.Marshal(redacted)
	if err != nil {
		return core.EventID{}, fmt.Errorf("eventbus.EmitAgentMessage: re-encoding redacted payload: %w", err)
	}

	eventID, idErr := b.idGen.Next()
	if idErr != nil {
		return core.EventID{}, fmt.Errorf("eventbus.EmitAgentMessage: generate event_id: %w", idErr)
	}
	const agentMessageType = core.EventTypeAgentMessage
	typeSchemaVersion, knownType := core.LookupTypeSchemaVersion(agentMessageType)
	if !knownType {
		return core.EventID{}, fmt.Errorf("eventbus.EmitAgentMessage: %w: %q", core.ErrUnknownEventType, agentMessageType)
	}
	evt := core.Event{
		EventID:         eventID,
		SchemaVersion:   typeSchemaVersion,
		Type:            agentMessageType,
		TimestampWall:   time.Now().UTC(),
		SourceSubsystem: "eventbus",
		Payload:         redactedBytes,
	}

	envelopeBytes, marshalEnvErr := json.Marshal(evt)
	if marshalEnvErr != nil {
		return core.EventID{}, fmt.Errorf("eventbus.EmitAgentMessage: marshal envelope: %w", marshalEnvErr)
	}
	if appendErr := b.jsonlWriter.Append(envelopeBytes, true /* always fsync — F-class */); appendErr != nil {
		return core.EventID{}, fmt.Errorf("eventbus.EmitAgentMessage: JSONL append: %w", appendErr)
	}
	b.maybeUpdateHWM(eventID)

	b.mu.Lock()
	subs := make([]core.Subscription, len(b.subscriptions))
	copy(subs, b.subscriptions)
	b.mu.Unlock()

	for _, sub := range subs {
		if !sub.EventPattern.MatchesType(agentMessageType) {
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
						b.recordDeadLetter(ctx, evt, "observer_panic")
					}
				}()
				if handlerErr := sub.Handler(ctx, evt); handlerErr != nil {
					b.recordDeadLetter(ctx, evt, "consumer_error")
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
// Refresh beats are ordinary (non-fsync) events, but they are persisted because
// the daemon-free `comms who` projection derives liveness from events.jsonl.
func (b *busImpl) EmitAgentPresence(ctx context.Context, payload core.AgentPresencePayload) (core.EventID, error) {
	payloadBytes, marshalErr := json.Marshal(payload)
	if marshalErr != nil {
		return core.EventID{}, fmt.Errorf("eventbus.EmitAgentPresence: marshal payload: %w", marshalErr)
	}

	var rawPayload map[string]any
	if err := json.Unmarshal(payloadBytes, &rawPayload); err != nil {
		return core.EventID{}, fmt.Errorf("eventbus.EmitAgentPresence: payload unmarshal for redaction: %w", err)
	}
	redacted := b.registry.RedactionMiddleware(rawPayload)
	redactedBytes, err := json.Marshal(redacted)
	if err != nil {
		return core.EventID{}, fmt.Errorf("eventbus.EmitAgentPresence: re-encoding redacted payload: %w", err)
	}

	eventID, idErr := b.idGen.Next()
	if idErr != nil {
		return core.EventID{}, fmt.Errorf("eventbus.EmitAgentPresence: generate event_id: %w", idErr)
	}
	const agentPresenceType = core.EventTypeAgentPresence
	typeSchemaVersion, knownType := core.LookupTypeSchemaVersion(agentPresenceType)
	if !knownType {
		return core.EventID{}, fmt.Errorf("eventbus.EmitAgentPresence: %w: %q", core.ErrUnknownEventType, agentPresenceType)
	}
	evt := core.Event{
		EventID:         eventID,
		SchemaVersion:   typeSchemaVersion,
		Type:            agentPresenceType,
		TimestampWall:   time.Now().UTC(),
		SourceSubsystem: "eventbus",
		Payload:         redactedBytes,
	}

	envelopeBytes, marshalEnvErr := json.Marshal(evt)
	if marshalEnvErr != nil {
		return core.EventID{}, fmt.Errorf("eventbus.EmitAgentPresence: marshal envelope: %w", marshalEnvErr)
	}
	if appendErr := b.jsonlWriter.Append(envelopeBytes, false /* O-class: no fsync */); appendErr != nil {
		return core.EventID{}, fmt.Errorf("eventbus.EmitAgentPresence: JSONL append: %w", appendErr)
	}

	b.mu.Lock()
	subs := make([]core.Subscription, len(b.subscriptions))
	copy(subs, b.subscriptions)
	b.mu.Unlock()

	for _, sub := range subs {
		if !sub.EventPattern.MatchesType(agentPresenceType) {
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
						b.recordDeadLetter(ctx, evt, "observer_panic")
					}
				}()
				if handlerErr := sub.Handler(ctx, evt); handlerErr != nil {
					b.recordDeadLetter(ctx, evt, "consumer_error")
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

	var rawPayload map[string]any
	if err := json.Unmarshal(payload, &rawPayload); err != nil {
		return core.EventID{}, fmt.Errorf("eventbus.EmitTyped(%s): payload unmarshal for redaction: %w", typeName, err)
	}
	redacted := b.registry.RedactionMiddleware(rawPayload)
	redactedBytes, err := json.Marshal(redacted)
	if err != nil {
		return core.EventID{}, fmt.Errorf("eventbus.EmitTyped(%s): re-encoding redacted payload: %w", typeName, err)
	}

	eventID, idErr := b.idGen.Next()
	if idErr != nil {
		return core.EventID{}, fmt.Errorf("eventbus.EmitTyped(%s): generate event_id: %w", typeName, idErr)
	}
	typeSchemaVersion, knownType := core.LookupTypeSchemaVersion(eventType)
	if !knownType {
		return core.EventID{}, fmt.Errorf("eventbus.EmitTyped(%s): %w", typeName, core.ErrUnknownEventType)
	}
	evt := core.Event{
		EventID:         eventID,
		SchemaVersion:   typeSchemaVersion,
		Type:            eventType,
		TimestampWall:   time.Now().UTC(),
		SourceSubsystem: "eventbus",
		Payload:         redactedBytes,
	}

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
						b.recordDeadLetter(ctx, evt, "observer_panic")
					}
				}()
				if handlerErr := sub.Handler(ctx, evt); handlerErr != nil {
					b.recordDeadLetter(ctx, evt, "consumer_error")
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

	if sub.ConsumerClass == core.ConsumerClassSynchronous {
		if err := b.checkSyncCardinality(sub); err != nil {
			return core.Subscription{}, err
		}

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

func (b *busImpl) checkSyncCardinality(incoming core.Subscription) error {
	for _, existing := range b.subscriptions {
		if existing.ConsumerClass != core.ConsumerClassSynchronous {
			continue
		}
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

func syncPatternOverlap(a, b core.EventPattern) (string, bool) {
	if a.Wildcard || b.Wildcard {
		return "*", true
	}
	for t := range a.Types {
		if _, ok := b.Types[t]; ok {
			return string(t), true
		}
	}
	return "", false
}

func (b *busImpl) checkSyncAcyclicity(incoming core.Subscription) error {
	if len(incoming.DeclaredEmitTypes) == 0 {
		return nil // emits nothing → cannot form a cycle
	}

	allSync := make([]core.Subscription, 0, len(b.subscriptions)+1)
	allSync = append(allSync, incoming)
	for _, s := range b.subscriptions {
		if s.ConsumerClass == core.ConsumerClassSynchronous {
			allSync = append(allSync, s)
		}
	}

	var dfs func(emitType core.EventType, visited map[string]bool, path []string) []string
	dfs = func(emitType core.EventType, visited map[string]bool, path []string) []string {
		if incoming.EventPattern.MatchesType(emitType) {
			return append(path, incoming.ConsumerID)
		}

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
			if !sub.EventPattern.MatchesType(ev.Type) {
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
		if !found.EventPattern.MatchesType(ev.Type) {
			return nil
		}
		return found.Handler(ctx, ev)
	})
	return err
}

type deadLetterEntry struct {
	Envelope core.Event `json:"envelope"`
}

// DeadLetterReplay replays events from the dead-letter log to the named
// consumer. filter, when non-nil, constrains which event types are replayed;
// nil replays all dead-letter entries that match the consumer's EventPattern.
// A missing dead-letter file or JSONL path is a no-op.
//
// Spec ref: specs/event-model.md §6.1, §6.2, §4.2 EV-011, EV-014b.
func (b *busImpl) DeadLetterReplay(consumerName string, filter *core.EventPattern) (err error) {
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
	defer func() {
		if closeErr := f.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("eventbus.DeadLetterReplay: close %s: %w", dlPath, closeErr)
		}
	}()

	ctx := context.Background()
	reader := bufio.NewReader(f)
	for {
		lineBytes, readErr := reader.ReadBytes('\n')
		if len(lineBytes) > 0 {
			var entry deadLetterEntry
			if decodeErr := json.Unmarshal(bytes.TrimRight(lineBytes, "\n"), &entry); decodeErr != nil {
				log.Printf("eventbus.DeadLetterReplay: malformed line (skipping): %v", decodeErr)
			} else {
				evType := entry.Envelope.Type
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
	defer func() {
		if closeErr := f.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("eventbus: close %s: %w", path, closeErr)
		}
	}()

	since := [16]byte(sinceID)
	reader := bufio.NewReader(f)
	for {
		lineBytes, readErr := reader.ReadBytes('\n')
		if len(lineBytes) > 0 {
			hasTerm := lineBytes[len(lineBytes)-1] == '\n'
			if !hasTerm && readErr == io.EOF {
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

func (b *busImpl) addGlobalDrainer() {
	b.drainMu.Lock()
	b.inflight++
	b.drainMu.Unlock()
}

func (b *busImpl) doneGlobalDrainer() {
	b.drainMu.Lock()
	b.inflight--
	if b.inflight == 0 && b.drainCond != nil {
		b.drainCond.Broadcast()
	}
	b.drainMu.Unlock()
}

func (b *busImpl) drainCondLocked() *sync.Cond {
	if b.drainCond == nil {
		b.drainCond = sync.NewCond(&b.drainMu)
	}
	return b.drainCond
}

func (b *busImpl) addRunDrainer(runID string) {
	b.runDrainersMu.Lock()
	b.runInflight[runID]++
	b.runDrainersMu.Unlock()
}

func (b *busImpl) doneRunDrainer(runID string) {
	b.runDrainersMu.Lock()
	b.runInflight[runID]--
	if b.runInflight[runID] <= 0 {
		delete(b.runInflight, runID)
		if b.runDrainCond != nil {
			b.runDrainCond.Broadcast()
		}
	}
	b.runDrainersMu.Unlock()
}

func (b *busImpl) runDrainCondLocked() *sync.Cond {
	if b.runDrainCond == nil {
		b.runDrainCond = sync.NewCond(&b.runDrainersMu)
	}
	return b.runDrainCond
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
	key := runID.String()
	done := make(chan struct{})
	stop := make(chan struct{})
	go func() {
		b.runDrainersMu.Lock()
		cond := b.runDrainCondLocked()
		for b.runInflight[key] > 0 {
			select {
			case <-stop:
				b.runDrainersMu.Unlock()
				return
			default:
			}
			cond.Wait()
		}
		b.runDrainersMu.Unlock()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		close(stop)
		b.runDrainersMu.Lock()
		b.runDrainCondLocked().Broadcast()
		b.runDrainersMu.Unlock()
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

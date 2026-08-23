package handlercontract

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	hclifecycle "github.com/gregberns/harmonik/internal/handlercontract/lifecycle"
)

// EventEmitter is the single-method interface the watcher requires from the
// in-process event bus.  It matches the Emit method of eventbus.EventBus
// (specs/event-model.md §6.1 INTERFACE EventBus) exactly, so any
// eventbus.EventBus implementation satisfies EventEmitter without an adapter.
//
// A narrow interface is used rather than eventbus.EventBus directly for two
// reasons:
//   - The watcher needs only Emit; requiring 6 methods couples it to the full
//     lifecycle surface (Seal, Drain, etc.) that is irrelevant here.
//   - Keeping the handlercontract package free of an import on internal/eventbus
//     avoids a circular-dependency risk as both packages grow.
//
// The EventBus stamps event_id, source_subsystem, and envelope timestamps at
// enqueue time per EV-002b; the watcher supplies only eventType and payload.
//
// Spec: specs/handler-contract.md §4.3.HC-011, specs/event-model.md §6.1.
type EventEmitter interface {
	// Emit redacts secret-prefixed payload fields, appends the event to the
	// durable JSONL file, and dispatches to all matching consumers.
	//
	// The call MUST NOT block asynchronous/observer consumer delivery on the
	// caller's goroutine; those dispatches are off-critical-path per EV-014a.
	// Returns a non-nil error only on hard failures (redaction fault, JSONL
	// append fault, or synchronous-consumer fault).
	//
	// Spec: specs/event-model.md §6.1, §7.1.
	Emit(ctx context.Context, eventType core.EventType, payload []byte) error

	// EmitWithRunID is identical to Emit but sets the run_id envelope field to
	// runID before JSONL append and consumer dispatch (EV-001; EM-013).
	//
	// Use EmitWithRunID for all run-scoped events (run_started, run_completed,
	// run_failed, etc.) so that the JSONL envelope carries the join key across
	// git, Beads, and JSONL per EM-013 / POST_OPERATIONAL_PARALLELISM_ROADMAP row #1.
	// Plain Emit is reserved for daemon-level events where no run is in flight
	// (daemon_started, daemon_orphan_sweep_completed, etc.).
	//
	// Spec: specs/event-model.md §6.1 EV-001; specs/execution-model.md §4.3 EM-013.
	// Bead: hk-n9f51.
	EmitWithRunID(ctx context.Context, runID core.RunID, eventType core.EventType, payload []byte) error
}

// WatcherDeadLetterSink is the interface the watcher uses to route pre-envelope
// events that cannot be delivered to the in-process bus (bus full, subscriber
// panic) per HC-027.
//
// The watcher MUST NOT drop events silently; per HC-027 they MUST reach the
// dead-letter destination declared by [event-model.md §4.3].
//
// eventType and payload mirror the arguments the watcher would have passed to
// EventEmitter.Emit; since the bus has not yet stamped the envelope (emission
// failed), the dead-letter sink receives the pre-envelope form.
//
// For the post-envelope dead-letter sink (bus consumer errors) see [DeadLetterSink].
//
// Spec: specs/handler-contract.md §4.6.HC-027.
type WatcherDeadLetterSink interface {
	// Append records the (eventType, payload) pair in the dead-letter store.
	//
	// Implementations MUST be non-blocking or use a bounded-retry policy.
	// A nil return indicates durable receipt; a non-nil error means the event
	// was not durably stored.
	//
	// The watcher cannot recover from a non-nil return — the event is lost —
	// but it does NOT drop the failure silently: every failed Append is counted
	// on the Watcher handle ([Watcher.DeadLetterFailures],
	// [Watcher.LastDeadLetterFailure]) and, when the spawning caller supplied
	// [SpawnWatcherConfig.OnDeadLetterFailure], reported to that hook. A sink
	// that is failing is therefore distinguishable from one that is working.
	Append(eventType core.EventType, payload []byte, reason string) error
}

// WatcherPublishBufSize is the default capacity of the watcher-to-event-bus
// publish channel per specs/handler-contract.md §4.3.HC-011a.
//
// The spec mandates "a small bounded buffer (implementation SHOULD default to
// 8 events)".  On buffer-full the watcher MUST route to the dead-letter per
// HC-027 rather than block indefinitely.
const WatcherPublishBufSize = 8

// WatcherPanicSubReason is the sub_reason value the watcher MUST use when a
// panic inside the watcher goroutine is converted to an agent_failed event per
// HC-011a.
//
// Error class: ErrStructural.
// Spec: specs/handler-contract.md §4.3.HC-011a.
const WatcherPanicSubReason = "watcher_panic"

// WatcherWedgedSubReason is the sub_reason value a daemon-level supervisor MUST
// use when a watcher goroutine has not advanced its LastReadEventAt timestamp
// within T/2 despite the subprocess being required to heartbeat at ≤ T/2 per
// HC-026a.
//
// Error class: ErrStructural.
// Spec: specs/handler-contract.md §4.3.HC-011a.
const WatcherWedgedSubReason = "watcher_wedged"

// SpawnWatcherConfig is the per-session configuration supplied to SpawnWatcher.
//
// All fields are required unless noted optional. The zero value is not usable.
//
// Spec: specs/handler-contract.md §4.3.HC-011.
type SpawnWatcherConfig struct {
	// SessionID is the stable daemon-assigned identifier for this session.
	// Carried on every handler-lifecycle event in the session's lifetime per §6.1.
	// Required (non-empty).
	SessionID core.SessionID

	// ProgressStream is the io.Reader over the NDJSON-framed progress stream
	// produced by the handler subprocess.  The watcher owns the read-loop; callers
	// MUST NOT read from ProgressStream after SpawnWatcher returns.
	// Required (non-nil).
	ProgressStream io.Reader

	// Publisher is the in-process event bus to which the watcher emits
	// translated handler-lifecycle events.  Must be non-blocking per HC-011.
	// Required (non-nil).
	//
	// Any eventbus.EventBus implementation satisfies EventEmitter; no adapter
	// is needed (hk-8i31.82 substitution from EventPublisher placeholder).
	Publisher EventEmitter

	// DeadLetter is the sink for events that could not be delivered to Publisher
	// (buffer-full, subscriber panic).  The watcher MUST NOT silently drop
	// undeliverable events per HC-027.
	// Required (non-nil).
	DeadLetter WatcherDeadLetterSink

	// OnDeadLetterFailure is invoked when DeadLetter.Append itself returns an
	// error — i.e. an event that already failed to reach the bus also failed to
	// reach the dead-letter store and is now lost. eventType and reason are the
	// arguments that were passed to Append; err is Append's return.
	//
	// This is the push half of the dead-letter failure signal; the pull half
	// ([Watcher.DeadLetterFailures] / [Watcher.LastDeadLetterFailure]) is always
	// on and needs no configuration. The hook exists so the daemon-side caller
	// can log the failure without handlercontract — a contract package —
	// acquiring a logger dependency.
	//
	// The hook runs on the watcher goroutine, on the read-loop's critical path:
	// implementations MUST be non-blocking and MUST NOT panic.
	//
	// The failure this hook reports is NOT rare by nature. When the bus is down
	// every progress line spills to the dead-letter sink, so a sink that is also
	// down invokes this hook once per line read. An implementation that writes
	// one unbuffered line per call therefore produces unbounded output on the
	// read loop, and every such write parks the goroutine that advances
	// [Watcher.LastReadEventAt] — the timestamp HC-011a wedge detection reads.
	// Implementations that log MUST rate-limit, sample, or aggregate. Sampling
	// costs no information: the exact count and the most recent error stay
	// available from [Watcher.DeadLetterFailures] /
	// [Watcher.LastDeadLetterFailure], which this hook does not gate.
	//
	// Optional: when nil, failures are still counted on the Watcher handle.
	//
	// Bead ref: hk-0eqik.
	OnDeadLetterFailure func(eventType core.EventType, reason string, err error)

	// PublishBufSize is the capacity of the internal publish channel.
	// When zero, WatcherPublishBufSize (8) is used per HC-011a.
	// Optional (≥ 0).
	PublishBufSize int

	// Machine is the per-session lifecycle FSM (handler-contract.md §4.13
	// HC-064..HC-067). When non-nil, the watcher calls Machine.Transition on
	// agent lifecycle events (agent_ready→Ready, agent_started→Executing,
	// agent_completed→Ready, agent_failed→Failed, agent_warning_silent_hang→Failed)
	// and Machine.RecordActivity on heartbeat events.
	//
	// Optional: when nil the watcher emits events to the bus but does NOT drive
	// FSM transitions (backward-compatible with callers that predate HC-064).
	Machine *hclifecycle.Machine

	// RunID is the durable run identity for progress messages whose wire shape
	// does not carry it. Handler callers source it from LaunchSpec.RunID.
	//
	// Optional for legacy watchers that do not receive handler_capabilities.
	// A handler_capabilities message without a valid RunID is sent to the
	// dead-letter sink. The watcher never publishes its raw wire payload.
	RunID core.RunID

	// NodeType is the workflow-graph node type for the node this session is
	// executing, per specs/handler-contract.md §4.2a HC-058 / HC-061.
	//
	// When set to core.NodeTypeSubWorkflow the watcher MUST reject any
	// outcome_emitted message from the handler subprocess as a structural error
	// (ErrStructural, sub-reason SubworkflowBoundaryEmitSubReason) because
	// sub-workflow nodes are graph-level expansion constructs that MUST NOT
	// dispatch handler subprocesses and MUST NOT emit an Outcome at the
	// boundary.
	//
	// Optional: the zero value (empty string) disables the HC-061 guard and
	// is the default for sessions whose node type is not a sub-workflow.
	NodeType core.NodeType

	// WireTap is an optional lossless raw sink for the NDJSON progress stream.
	// When non-nil, the read-loop reads through io.TeeReader(cfg.ProgressStream,
	// cfg.WireTap), so every byte the watcher consumes is copied verbatim to the
	// tap BEFORE decode — a byte-exact capture of the wire. The twin-parity
	// WS3-Claude-A reference-capture harness feeds this tap's output to
	// internal/twinparity.AssertStreamEquivalent.
	//
	// Optional: when nil the read-loop consumes cfg.ProgressStream directly and
	// behavior is byte-identical to the pre-WireTap watcher (production untouched).
	// This is an explicit config seam — NOT an env var — so it is testable with
	// no global state, and it never alters read-loop timing when unset.
	WireTap io.Writer
}

// Watcher is the daemon-side goroutine that owns (a) the NDJSON read-loop on
// the handler's progress stream, (b) publication of handler-emitted events to
// the in-process event bus, and (c) cleanup at session end.
//
// One Watcher is spawned per active handler session; N active sessions produce
// N live Watcher goroutines. Watchers MUST NOT share state across sessions.
//
// # Lifecycle
//
// SpawnWatcher creates and starts the watcher goroutine; it returns immediately
// after the goroutine is launched. The goroutine runs until:
//
//   - the progress stream reaches EOF (clean handler exit), or
//   - the enclosing context is cancelled (operator stop / policy cancellation), or
//   - the watcher detects a framing violation and emits agent_failed.
//
// The goroutine records the terminal condition in the Watcher value; callers may
// observe it via Done (a channel that is closed when the goroutine exits) and
// Err (the terminal error, if any).
//
// # Goroutine ownership
//
// The watcher goroutine is owned by S01 (Orchestrator Core / daemon). S04 (Agent
// Runner) MUST NOT spawn per-session goroutines; per-session state lives entirely
// in this watcher's stack or closure per HC-012.
//
// # Liveness
//
// LastReadEventAt is updated on every successful io.Reader.Read return (not on
// message decode — per HC-011a, the two timestamps are distinct). A daemon-level
// supervisor MUST poll LastReadEventAt at cadence ≤ T/4 and classify the watcher
// as wedged (sub-reason WatcherWedgedSubReason) if it has not advanced within
// T/2 while the subprocess is alive.
//
// # Panic recovery
//
// The watcher goroutine body installs a recover() barrier per HC-011a. A panic
// inside the watcher is converted to an agent_failed event with class
// ErrStructural and sub-reason WatcherPanicSubReason and does NOT bring down
// the daemon.
//
// Spec: specs/handler-contract.md §4.3.HC-011, §4.3.HC-011a.
type Watcher struct {
	sessionID core.SessionID

	// runID is the run identifier carried on watcher-synthesized agent_failed
	// events so they are attributable by the reconciler. Sourced from
	// cfg.Machine.RunID() at spawn time; empty when no Machine was supplied.
	runID string

	// capabilitiesRunID is the valid durable run ID for the typed
	// handler_capabilities event. It is distinct from runID because the
	// lifecycle machine is optional and can use the legacy "unknown" value.
	capabilitiesRunID core.RunID

	// done is closed when the goroutine exits (success or failure).
	done chan struct{}

	// termErr holds the terminal error from the watcher goroutine.
	// Set exactly once before done is closed; safe to read after done is closed.
	termErr atomic.Pointer[error]

	// lastReadEventAt is the Unix nanoseconds of the last successful Read call
	// on the progress stream.  Atomically updated by the watcher goroutine;
	// atomically read by the supervisor for wedge detection per HC-011a.
	// Zero until the goroutine performs its first successful Read.
	lastReadEventAt atomic.Int64

	// deadLetterFailures counts WatcherDeadLetterSink.Append calls that returned
	// a non-nil error — events lost because both the bus and the dead-letter
	// store rejected them. Written by the watcher goroutine, read by anyone.
	deadLetterFailures atomic.Uint64

	// lastDeadLetterErr holds the most recent non-nil Append error, or nil if
	// the sink has never failed.
	lastDeadLetterErr atomic.Pointer[error]

	// onDeadLetterFailure mirrors SpawnWatcherConfig.OnDeadLetterFailure; nil
	// when the caller supplied no hook. Read-only after SpawnWatcher returns.
	onDeadLetterFailure func(eventType core.EventType, reason string, err error)
}

// SessionID returns the stable identifier for the session this watcher serves.
//
// Safe to call from any goroutine.
func (w *Watcher) SessionID() core.SessionID {
	return w.sessionID
}

// Done returns a channel that is closed when the watcher goroutine exits.
//
// The channel is never re-opened. Safe to call from any goroutine.
// Callers can use <-w.Done() to block until the watcher has finished all
// cleanup, including session-end publication.
func (w *Watcher) Done() <-chan struct{} {
	return w.done
}

// Err returns the terminal error from the watcher goroutine.
//
// Returns nil when the watcher completed cleanly (progress stream reached EOF
// after a successful outcome_emitted publication) or when the goroutine has not
// yet finished (Done is not yet closed).
// Safe to call from any goroutine; callers SHOULD wait for Done before reading.
func (w *Watcher) Err() error {
	p := w.termErr.Load()
	if p == nil {
		return nil
	}
	return *p
}

// LastReadEventAt returns the wall-clock time of the last successful io.Reader.Read
// return in the watcher's read-loop, or the zero time.Time if no read has
// occurred yet.
//
// This is the per-watcher liveness timestamp required by HC-011a for
// watcher-wedge detection. It is distinct from the per-session
// last_progress_event_at of §7.1 (which updates on successful message decode).
//
// Safe to call from any goroutine.
func (w *Watcher) LastReadEventAt() time.Time {
	ns := w.lastReadEventAt.Load()
	if ns == 0 {
		return time.Time{}
	}
	return time.Unix(0, ns)
}

// DeadLetterFailures returns the number of times this watcher's
// WatcherDeadLetterSink.Append returned a non-nil error — that is, the number
// of events lost because neither the bus nor the dead-letter store accepted
// them.
//
// A non-zero count means the dead-letter sink is not working. It is the
// always-on half of the failure signal: unlike
// [SpawnWatcherConfig.OnDeadLetterFailure] it requires no configuration, so a
// failing sink is never indistinguishable from a healthy one.
//
// This serves HC-027 ("the watcher MUST NOT drop events silently") for the
// dead-letter path itself, but it is not a quotation of it: §4.6 HC-027 of
// specs/handler-contract.md specifies the routing, not a counter and not a
// hook. The spec amendment for this shape is tracked separately (hk-nkrb9).
//
// Safe to call from any goroutine.
//
// Bead ref: hk-0eqik.
func (w *Watcher) DeadLetterFailures() uint64 {
	return w.deadLetterFailures.Load()
}

// LastDeadLetterFailure returns the most recent error returned by this
// watcher's WatcherDeadLetterSink.Append, or nil if the sink has never failed.
//
// Safe to call from any goroutine.
//
// Bead ref: hk-0eqik.
func (w *Watcher) LastDeadLetterFailure() error {
	p := w.lastDeadLetterErr.Load()
	if p == nil {
		return nil
	}
	return *p
}

// SpawnWatcher creates and starts the per-session watcher goroutine described
// by HC-011.  It returns the Watcher handle immediately after the goroutine is
// launched; callers MUST NOT read from cfg.ProgressStream after this call.
//
// SpawnWatcher panics if any required config field is nil/zero — that is a
// daemon-defect: the daemon assembled a malformed configuration.
//
// Spec: specs/handler-contract.md §4.3.HC-011.
func SpawnWatcher(ctx context.Context, cfg SpawnWatcherConfig) *Watcher {
	if cfg.ProgressStream == nil {
		panic("handlercontract: SpawnWatcher: cfg.ProgressStream is nil — daemon defect")
	}
	if cfg.Publisher == nil {
		panic("handlercontract: SpawnWatcher: cfg.Publisher is nil — daemon defect")
	}
	if cfg.DeadLetter == nil {
		panic("handlercontract: SpawnWatcher: cfg.DeadLetter is nil — daemon defect")
	}
	if cfg.SessionID == "" {
		panic("handlercontract: SpawnWatcher: cfg.SessionID is empty — daemon defect")
	}

	bufSize := cfg.PublishBufSize
	if bufSize <= 0 {
		bufSize = WatcherPublishBufSize
	}

	w := &Watcher{
		sessionID:           cfg.SessionID,
		capabilitiesRunID:   cfg.RunID,
		done:                make(chan struct{}),
		onDeadLetterFailure: cfg.OnDeadLetterFailure,
	}
	if cfg.Machine != nil {
		w.runID = cfg.Machine.RunID()
		if w.capabilitiesRunID == (core.RunID{}) {
			runID, err := uuid.Parse(w.runID)
			if err == nil && runID != uuid.Nil {
				w.capabilitiesRunID = core.RunID(runID)
			}
		}
	}

	go w.runLoop(ctx, cfg, bufSize)
	return w
}

func (w *Watcher) runLoop(ctx context.Context, cfg SpawnWatcherConfig, bufSize int) {
	defer close(w.done)

	defer func() {
		r := recover()
		if r == nil {
			return
		}
		panicErr := fmt.Errorf("handlercontract: watcher panic: %v: %w", r, ErrStructural)
		w.setTermErr(panicErr)

		eventType, payload := buildWatcherFailedPayload(w.sessionID, w.runID, WatcherPanicSubReason, panicErr)
		w.publishOrDeadLetter(ctx, eventType, payload, cfg.Publisher, cfg.DeadLetter)
	}()

	w.readLoop(ctx, cfg, bufSize)
}

func (w *Watcher) readLoop(ctx context.Context, cfg SpawnWatcherConfig, _ int) {
	src := cfg.ProgressStream
	if cfg.WireTap != nil {
		src = io.TeeReader(cfg.ProgressStream, cfg.WireTap)
	}
	scanner := bufio.NewScanner(&readStampReader{inner: src, w: w})
	scanner.Buffer(make([]byte, NDJSONMaxLineLenBytes+1), NDJSONMaxLineLenBytes+1)

	for {
		select {
		case <-ctx.Done():
			cancelErr := fmt.Errorf("handlercontract: watcher context cancelled: %w", ErrCanceled)
			w.setTermErr(cancelErr)
			return
		default:
		}

		gotLine := scanner.Scan()

		if !gotLine {
			scanErr := scanner.Err()
			if scanErr != nil {
				if isLineTooLong(scanErr) {
					termErr := fmt.Errorf("handlercontract: ndjson line too long: %w", ErrProtocolMismatch)
					w.setTermErr(termErr)
					et, pl := buildWatcherFailedPayload(w.sessionID, w.runID, NDJSONLineTooLongSubReason, termErr)
					w.publishOrDeadLetter(ctx, et, pl, cfg.Publisher, cfg.DeadLetter)
					return
				}
				if errors.Is(scanErr, ErrProtocolMismatch) {
					termErr := fmt.Errorf("handlercontract: progress stream protocol mismatch: %w", scanErr)
					w.setTermErr(termErr)
					et, pl := buildWatcherFailedPayload(w.sessionID, w.runID, ProtocolMismatchSubReason, termErr)
					w.publishOrDeadLetter(ctx, et, pl, cfg.Publisher, cfg.DeadLetter)
					return
				}
				termErr := fmt.Errorf("handlercontract: progress stream read error: %w: %w", scanErr, ErrStructural)
				w.setTermErr(termErr)
				et, pl := buildWatcherFailedPayload(w.sessionID, w.runID, PartialMessageSubReason, termErr)
				w.publishOrDeadLetter(ctx, et, pl, cfg.Publisher, cfg.DeadLetter)
				return
			}
			w.setTermErr(nil)
			return
		}

		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var typeOnly struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(line, &typeOnly); err != nil {
			termErr := fmt.Errorf("handlercontract: malformed NDJSON line: %w: %w", err, ErrStructural)
			w.setTermErr(termErr)
			et, pl := buildWatcherFailedPayload(w.sessionID, w.runID, MalformedProgressMessageSubReason, termErr)
			w.publishOrDeadLetter(ctx, et, pl, cfg.Publisher, cfg.DeadLetter)
			return
		}

		if typeOnly.Type == "" || !isKnownProgressMsgType(typeOnly.Type) {
			continue
		}

		if typeOnly.Type == ProgressMsgTypeOutcomeEmitted && cfg.NodeType == core.NodeTypeSubWorkflow {
			termErr := fmt.Errorf("handlercontract: handler emitted Outcome on sub-workflow boundary node (HC-061 violation): %w", ErrStructural)
			w.setTermErr(termErr)
			et, pl := buildWatcherFailedPayload(w.sessionID, w.runID, SubworkflowBoundaryEmitSubReason, termErr)
			w.publishOrDeadLetter(ctx, et, pl, cfg.Publisher, cfg.DeadLetter)
			return
		}

		if typeOnly.Type == ProgressMsgTypeHandlerCapabilities {
			var wire HandlerCapabilitiesMsg
			if err := json.Unmarshal(line, &wire); err != nil {
				continue
			}
			if w.capabilitiesRunID == (core.RunID{}) {
				w.appendDeadLetter(cfg.DeadLetter, core.EventTypeHandlerCapabilities, line, "handler_capabilities has no valid run ID")
				continue
			}
			versions := make([]string, len(wire.SupportedVersions))
			for i, version := range wire.SupportedVersions {
				versions[i] = strconv.Itoa(version)
			}
			payload := core.HandlerCapabilitiesPayload{RunID: w.capabilitiesRunID, SessionID: w.sessionID, ProtocolVersionsSupported: versions}
			if wire.ClaudeSessionID != "" {
				payload.ClaudeSessionID = &wire.ClaudeSessionID
			}
			encoded, err := json.Marshal(payload)
			if err != nil {
				w.appendDeadLetter(cfg.DeadLetter, core.EventTypeHandlerCapabilities, line, fmt.Sprintf("handler_capabilities encode: %v", err))
				continue
			}
			line = encoded
		}
		w.publishOrDeadLetter(ctx, core.EventType(typeOnly.Type), line, cfg.Publisher, cfg.DeadLetter)

		if typeOnly.Type == ProgressMsgTypeAgentOutputChunk {
			w.emitBudgetAccrualForChunk(ctx, line, cfg.Publisher, cfg.DeadLetter)
		}

		if cfg.Machine != nil {
			w.driveLifecycleFSM(ctx, cfg.Machine, typeOnly.Type, cfg.Publisher, cfg.DeadLetter)
		}
	}
}

func (w *Watcher) publishOrDeadLetter(
	ctx context.Context,
	eventType core.EventType,
	payload []byte,
	pub EventEmitter,
	dl WatcherDeadLetterSink,
) {
	if err := pub.Emit(ctx, eventType, payload); err != nil {
		w.appendDeadLetter(dl, eventType, payload, fmt.Sprintf("emit failed: %v", err))
	}
}

func (w *Watcher) appendDeadLetter(dl WatcherDeadLetterSink, eventType core.EventType, payload []byte, reason string) {
	err := dl.Append(eventType, payload, reason)
	if err == nil {
		return
	}
	w.deadLetterFailures.Add(1)
	w.lastDeadLetterErr.Store(&err)
	if w.onDeadLetterFailure != nil {
		w.onDeadLetterFailure(eventType, reason, err)
	}
}

func (w *Watcher) setTermErr(err error) {
	w.termErr.Store(&err)
}

func (w *Watcher) emitBudgetAccrualForChunk(ctx context.Context, chunkLine []byte, pub EventEmitter, dl WatcherDeadLetterSink) {
	var msg struct {
		RunID        core.RunID     `json:"run_id"`
		SessionID    core.SessionID `json:"session_id"`
		ChunkIndex   int            `json:"chunk_index"`
		BytesEmitted int            `json:"bytes_emitted"`
	}
	if err := json.Unmarshal(chunkLine, &msg); err != nil {
		w.appendDeadLetter(dl, core.EventTypeBudgetAccrual, chunkLine, fmt.Sprintf("budget_accrual decode: %v", err))
		return
	}

	chunkIdx := msg.ChunkIndex
	p := core.BudgetAccrualPayload{
		RunID:      msg.RunID,
		SessionID:  msg.SessionID,
		ChunkIndex: &chunkIdx,
		CostUnits:  float64(msg.BytesEmitted),
		CostBasis:  core.CostBasisOutputBytes,
	}

	payload, err := json.Marshal(p)
	if err != nil {
		w.appendDeadLetter(dl, core.EventTypeBudgetAccrual, nil, fmt.Sprintf("budget_accrual marshal: %v", err))
		return
	}

	w.publishOrDeadLetter(ctx, core.EventTypeBudgetAccrual, payload, pub, dl)
}

func (w *Watcher) driveLifecycleFSM(
	ctx context.Context,
	m *hclifecycle.Machine,
	msgType ProgressMsgType,
	pub EventEmitter,
	dl WatcherDeadLetterSink,
) {
	switch msgType {
	case ProgressMsgTypeAgentHeartbeat:
		m.RecordActivity()
	case ProgressMsgTypeAgentReady:
		w.emitMachineTransition(ctx, m, hclifecycle.StateReady, hclifecycle.ReasonInitComplete, "", "", pub, dl)
	case ProgressMsgTypeAgentStarted:
		w.emitMachineTransition(ctx, m, hclifecycle.StateExecuting, hclifecycle.ReasonCommandStarted, "", "", pub, dl)
	case ProgressMsgTypeAgentCompleted:
		w.emitMachineTransition(ctx, m, hclifecycle.StateReady, hclifecycle.ReasonCommandComplete, "", "", pub, dl)
	case ProgressMsgTypeAgentFailed:
		w.emitMachineTransition(ctx, m, hclifecycle.StateFailed, hclifecycle.ReasonError, "agent_failed", "agent process failed", pub, dl)
	default:
	}
}

func (w *Watcher) emitMachineTransition(
	ctx context.Context,
	m *hclifecycle.Machine,
	to hclifecycle.LifecycleState,
	reason hclifecycle.TransitionReason,
	errCode, errMsg string,
	pub EventEmitter,
	dl WatcherDeadLetterSink,
) {
	from := m.Current()
	if err := m.Transition(to, reason, errCode, errMsg); err != nil {
		return
	}
	p := core.LifecycleTransitionPayload{
		SessionID:      core.SessionID(m.SessionID()),
		FromState:      from.String(),
		ToState:        to.String(),
		Reason:         string(reason),
		TransitionedAt: time.Now().Format(time.RFC3339Nano),
		ErrCode:        errCode,
		ErrMsg:         errMsg,
	}
	payload, err := json.Marshal(p)
	if err != nil {
		w.appendDeadLetter(dl, core.EventTypeLifecycleTransition, nil, fmt.Sprintf("lifecycle_transition marshal: %v", err))
		return
	}
	if parsedUUID, parseErr := uuid.Parse(m.RunID()); parseErr == nil {
		if emitErr := pub.EmitWithRunID(ctx, core.RunID(parsedUUID), core.EventTypeLifecycleTransition, payload); emitErr != nil {
			w.appendDeadLetter(dl, core.EventTypeLifecycleTransition, payload, fmt.Sprintf("lifecycle_transition emit: %v", emitErr))
		}
	} else {
		if emitErr := pub.Emit(ctx, core.EventTypeLifecycleTransition, payload); emitErr != nil {
			w.appendDeadLetter(dl, core.EventTypeLifecycleTransition, payload, fmt.Sprintf("lifecycle_transition emit: %v", emitErr))
		}
	}
}

func buildWatcherFailedPayload(sessionID core.SessionID, runID, sub string, cause error) (eventType core.EventType, encoded []byte) {
	payload, _ := json.Marshal(map[string]string{ //nolint:errcheck,errchkjson // map[string]string marshal cannot fail; see comment above
		"type":           ProgressMsgTypeAgentFailed,
		"session_id":     string(sessionID),
		"run_id":         runID,
		"error_category": Class(cause),
		"sub_reason":     sub,
	})
	return core.EventType(ProgressMsgTypeAgentFailed), payload
}

type readStampReader struct {
	inner io.Reader
	w     *Watcher
}

func (r *readStampReader) Read(p []byte) (int, error) {
	n, err := r.inner.Read(p)
	if n > 0 || err == nil {
		r.w.lastReadEventAt.Store(time.Now().UnixNano())
	}
	return n, err
}

func isLineTooLong(err error) bool {
	return errors.Is(err, bufio.ErrTooLong)
}

var knownProgressMsgTypes = map[ProgressMsgType]struct{}{
	ProgressMsgTypeHandlerCapabilities:   {},
	ProgressMsgTypeAgentReady:            {},
	ProgressMsgTypeAgentStarted:          {},
	ProgressMsgTypeAgentOutputChunk:      {},
	ProgressMsgTypeAgentCompleted:        {},
	ProgressMsgTypeAgentFailed:           {},
	ProgressMsgTypeAgentRateLimited:      {},
	ProgressMsgTypeAgentRateLimitCleared: {},
	ProgressMsgTypeAgentHeartbeat:        {},
	ProgressMsgTypeSessionLogLocation:    {},
	ProgressMsgTypeSkillsProvisioned:     {},
	ProgressMsgTypeOutcomeEmitted:        {},
	ProgressMsgTypeLaunchInitiated:       {},
}

// KnownProgressMsgTypes returns a copy of the complete set of required
// progress-stream message type strings per HC-007. Exposed for consumers that
// need to assemble a legal-kind vocabulary from the progress-stream layer
// (e.g. internal/twinparity) without re-listing the types by hand. Additive
// accessor only — it does not participate in the watcher publish path.
func KnownProgressMsgTypes() []string {
	out := make([]string, 0, len(knownProgressMsgTypes))
	for t := range knownProgressMsgTypes {
		out = append(out, t)
	}
	return out
}

func isKnownProgressMsgType(msgType ProgressMsgType) bool {
	_, ok := knownProgressMsgTypes[msgType]
	return ok
}

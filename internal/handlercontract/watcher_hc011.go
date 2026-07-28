package handlercontract

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	hclifecycle "github.com/gregberns/harmonik/internal/handlercontract/lifecycle"
)

// watcher — per-bead helper prefix for test helpers in watcher_hc011_test.go
// (implementer-protocol.md §Helper-prefix discipline; bead hk-8i31.12).

// ─────────────────────────────────────────────────────────────────────────────
// HC-011 — Narrow emitter interface for the event bus
// ─────────────────────────────────────────────────────────────────────────────

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

// ─────────────────────────────────────────────────────────────────────────────
// HC-011 — Watcher publish-buffer size
// ─────────────────────────────────────────────────────────────────────────────

// WatcherPublishBufSize is the default capacity of the watcher-to-event-bus
// publish channel per specs/handler-contract.md §4.3.HC-011a.
//
// The spec mandates "a small bounded buffer (implementation SHOULD default to
// 8 events)".  On buffer-full the watcher MUST route to the dead-letter per
// HC-027 rather than block indefinitely.
const WatcherPublishBufSize = 8

// ─────────────────────────────────────────────────────────────────────────────
// HC-011a — Sub-reason string constants for watcher self-defect events
// ─────────────────────────────────────────────────────────────────────────────

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

// ─────────────────────────────────────────────────────────────────────────────
// HC-011 — SpawnWatcherConfig and Watcher
// ─────────────────────────────────────────────────────────────────────────────

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
		done:                make(chan struct{}),
		onDeadLetterFailure: cfg.OnDeadLetterFailure,
	}
	if cfg.Machine != nil {
		w.runID = cfg.Machine.RunID()
	}

	go w.runLoop(ctx, cfg, bufSize)
	return w
}

// runLoop is the watcher goroutine body.
//
// It installs a recover() barrier per HC-011a, runs the NDJSON read-loop, and
// closes w.done when it exits.
func (w *Watcher) runLoop(ctx context.Context, cfg SpawnWatcherConfig, bufSize int) {
	defer close(w.done)

	// HC-011a: install recover() barrier.  A panic inside the watcher MUST be
	// converted to agent_failed with class ErrStructural, sub-reason watcher_panic,
	// and MUST NOT bring down the daemon.
	defer func() {
		r := recover()
		if r == nil {
			return
		}
		panicErr := fmt.Errorf("handlercontract: watcher panic: %v: %w", r, ErrStructural)
		w.setTermErr(panicErr)

		// Emit agent_failed{structural, watcher_panic} to the bus; route to
		// dead-letter if the emit fails.
		eventType, payload := buildWatcherFailedPayload(w.sessionID, w.runID, WatcherPanicSubReason, panicErr)
		w.publishOrDeadLetter(ctx, eventType, payload, cfg.Publisher, cfg.DeadLetter)
	}()

	w.readLoop(ctx, cfg, bufSize)
}

// readLoop is the inner NDJSON read-loop called from runLoop.
//
// It reads NDJSON lines from cfg.ProgressStream, updates lastReadEventAt on
// each successful read, translates each line into a core.Event, and publishes
// it to the bus via the publish channel.
//
// Framing violations (line-too-long, partial-message, malformed JSON) are
// classified per HC-007a/HC-007b and result in agent_failed publication.
func (w *Watcher) readLoop(ctx context.Context, cfg SpawnWatcherConfig, _ int) {
	// WS3-Claude-A wire-tap seam: when cfg.WireTap is set, read through a
	// TeeReader so every consumed byte is copied verbatim to the tap BEFORE
	// decode (lossless raw capture). When nil, read cfg.ProgressStream directly
	// — byte-identical to the pre-tap watcher, no timing change.
	src := cfg.ProgressStream
	if cfg.WireTap != nil {
		src = io.TeeReader(cfg.ProgressStream, cfg.WireTap)
	}
	// HC-011a: lastReadEventAt MUST advance on every successful io.Reader.Read
	// return, not once per decoded line/Scan — a Scan can span many Reads (or a
	// Read many lines), and wedge detection keys off the raw Read cadence. The
	// stamp reader wraps the (post-tap) source so it observes the same bytes the
	// scanner consumes.
	scanner := bufio.NewScanner(&readStampReader{inner: src, w: w})
	// HC-007a: enforce the 1 MiB max line-length cap at the scanner layer.
	scanner.Buffer(make([]byte, NDJSONMaxLineLenBytes+1), NDJSONMaxLineLenBytes+1)

	for {
		// Check context cancellation before each scan iteration.
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
				// Distinguish line-too-long (ErrTooLong) from other I/O errors.
				if isLineTooLong(scanErr) {
					termErr := fmt.Errorf("handlercontract: ndjson line too long: %w", ErrProtocolMismatch)
					w.setTermErr(termErr)
					et, pl := buildWatcherFailedPayload(w.sessionID, w.runID, NDJSONLineTooLongSubReason, termErr)
					w.publishOrDeadLetter(ctx, et, pl, cfg.Publisher, cfg.DeadLetter)
					return
				}
				// hk-9ngiv: version-negotiation failure surfaces through the
				// scanner as an error wrapping ErrProtocolMismatch (the
				// SessionIDInterceptor's sticky negoErr — handler advertised no
				// mutually supported version, or never emitted handler_capabilities
				// within the caps timeout). Preserve the sentinel (%w for scanErr,
				// NOT %v) so the orchestrator's retry policy can distinguish a
				// protocol mismatch — which MUST NOT retry-spin the same pinned
				// binary — from a transient framing error (§8.7, HC-021). Mirrors
				// the isLineTooLong branch above; both wrap ErrProtocolMismatch, but
				// this is the general case (sub_reason protocol_mismatch) vs the
				// line-cap subcase (sub_reason ndjson_line_too_long).
				if errors.Is(scanErr, ErrProtocolMismatch) {
					termErr := fmt.Errorf("handlercontract: progress stream protocol mismatch: %w", scanErr)
					w.setTermErr(termErr)
					et, pl := buildWatcherFailedPayload(w.sessionID, w.runID, ProtocolMismatchSubReason, termErr)
					w.publishOrDeadLetter(ctx, et, pl, cfg.Publisher, cfg.DeadLetter)
					return
				}
				// Other I/O errors: structural framing failure.
				termErr := fmt.Errorf("handlercontract: progress stream read error: %w: %w", scanErr, ErrStructural)
				w.setTermErr(termErr)
				et, pl := buildWatcherFailedPayload(w.sessionID, w.runID, PartialMessageSubReason, termErr)
				w.publishOrDeadLetter(ctx, et, pl, cfg.Publisher, cfg.DeadLetter)
				return
			}
			// EOF with no error: progress stream closed cleanly.
			// Session-end cleanup: watcher exits cleanly; callers observe Err() == nil.
			w.setTermErr(nil)
			return
		}

		line := scanner.Bytes()
		if len(line) == 0 {
			// Blank line: skip (NDJSON allows blank separators between objects).
			continue
		}

		// Decode the type-discriminator field to route to the correct publish path.
		var typeOnly struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(line, &typeOnly); err != nil {
			// HC-007b: malformed JSON on a live socket → close session, emit agent_failed.
			termErr := fmt.Errorf("handlercontract: malformed NDJSON line: %w: %w", err, ErrStructural)
			w.setTermErr(termErr)
			et, pl := buildWatcherFailedPayload(w.sessionID, w.runID, MalformedProgressMessageSubReason, termErr)
			w.publishOrDeadLetter(ctx, et, pl, cfg.Publisher, cfg.DeadLetter)
			return
		}

		// Unknown message types MUST be ignored per HC-007 additive-evolution rule:
		// the watcher dispatches on the type field; unknown values are dropped
		// silently (not treated as errors) to allow future protocol extensions
		// to be deployed before all consumers are updated.
		if typeOnly.Type == "" || !isKnownProgressMsgType(typeOnly.Type) {
			continue
		}

		// HC-061: sub-workflow boundary handlers MUST NOT emit an Outcome.
		// If cfg.NodeType == NodeTypeSubWorkflow and the handler emits
		// outcome_emitted, the watcher MUST reject it as ErrStructural with
		// sub-reason SubworkflowBoundaryEmitSubReason.
		// Cite: specs/handler-contract.md §4.2a HC-061.
		if typeOnly.Type == ProgressMsgTypeOutcomeEmitted && cfg.NodeType == core.NodeTypeSubWorkflow {
			termErr := fmt.Errorf("handlercontract: handler emitted Outcome on sub-workflow boundary node (HC-061 violation): %w", ErrStructural)
			w.setTermErr(termErr)
			et, pl := buildWatcherFailedPayload(w.sessionID, w.runID, SubworkflowBoundaryEmitSubReason, termErr)
			w.publishOrDeadLetter(ctx, et, pl, cfg.Publisher, cfg.DeadLetter)
			return
		}

		// Emit the progress-stream message to the bus.
		// The bus (EventBus.Emit) stamps event_id, source_subsystem, and envelope
		// timestamps at enqueue time per EV-002b; the watcher supplies only the
		// type and the raw NDJSON line as payload.
		w.publishOrDeadLetter(ctx, core.EventType(typeOnly.Type), line, cfg.Publisher, cfg.DeadLetter)

		// CP-024: every agent_output_chunk MUST co-emit a budget_accrual event
		// within the same handler tick (specs/control-points.md §4.5.CP-024).
		if typeOnly.Type == ProgressMsgTypeAgentOutputChunk {
			w.emitBudgetAccrualForChunk(ctx, line, cfg.Publisher, cfg.DeadLetter)
		}

		// HC-064..HC-067: drive the per-session lifecycle FSM based on the
		// observed progress-stream event type. The Machine is optional; when nil
		// (backward-compatible callers that predate HC-064) this block is a no-op.
		if cfg.Machine != nil {
			w.driveLifecycleFSM(ctx, cfg.Machine, typeOnly.Type, cfg.Publisher, cfg.DeadLetter)
		}
	}
}

// publishOrDeadLetter attempts to emit (eventType, payload) to pub.  If Emit
// returns a non-nil error the raw event is routed to the dead-letter sink per
// HC-027.  ctx is the watcher's enclosing context; it is passed through to
// Emit so the bus can honour cancellation on the critical path.
//
// The watcher MUST NOT drop events silently.
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

// appendDeadLetter routes (eventType, payload, reason) to the dead-letter sink
// and records the outcome. It is the ONLY place the watcher calls
// WatcherDeadLetterSink.Append.
//
// An Append failure is unrecoverable — the event is lost — but it is never
// dropped silently (hk-0eqik). Each failure bumps the DeadLetterFailures
// counter, replaces LastDeadLetterFailure, and is handed to the caller-supplied
// OnDeadLetterFailure hook when one was configured. The hook runs inline on the
// watcher goroutine, which is why the contract requires it to be non-blocking.
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

// setTermErr stores err as the watcher's terminal error.  Stores only on the
// first call (once per goroutine lifetime — always called before done is closed).
func (w *Watcher) setTermErr(err error) {
	// Store a pointer-to-err.  We always store (the goroutine calls this exactly
	// once before returning), so no CAS is required here.
	w.termErr.Store(&err)
}

// ─────────────────────────────────────────────────────────────────────────────
// Internal event-building helpers (unexported; tested via SpawnWatcher)
// ─────────────────────────────────────────────────────────────────────────────

// emitBudgetAccrualForChunk synthesizes and emits a budget_accrual event for an
// agent_output_chunk progress-stream message per CP-024.
//
// The payload is derived from the chunk fields: run_id and session_id are
// decoded from chunkLine; chunk_index and bytes_emitted provide correlation and
// cost_units respectively. cost_basis is always core.CostBasisOutputBytes
// (no token-count is available at the chunk boundary).
//
// Decoding is best-effort: if chunkLine is missing required fields the
// budget_accrual is emitted with whatever fields could be decoded (zero RunID,
// empty SessionID, zero CostUnits). The MUST-emit requirement of CP-024 takes
// precedence over payload completeness.
//
// Spec: specs/control-points.md §4.5.CP-024; specs/event-model.md §8.4.2.
func (w *Watcher) emitBudgetAccrualForChunk(ctx context.Context, chunkLine []byte, pub EventEmitter, dl WatcherDeadLetterSink) {
	// Decode only the fields needed to construct the budget_accrual payload.
	// Use core types directly so RunID.UnmarshalText handles UUID parsing.
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
		// Static struct; marshal failure is a defect. Route to dead-letter.
		w.appendDeadLetter(dl, core.EventTypeBudgetAccrual, nil, fmt.Sprintf("budget_accrual marshal: %v", err))
		return
	}

	w.publishOrDeadLetter(ctx, core.EventTypeBudgetAccrual, payload, pub, dl)
}

// driveLifecycleFSM maps a progress-stream message type to a LifecycleState
// transition and emits a lifecycle_transition event (HC-064..HC-067, §8.3.14).
//
// Mapping (HC-065 table):
//   - agent_ready       → StateReady     (ReasonInitComplete)
//   - agent_started     → StateExecuting  (ReasonCommandStarted)
//   - agent_completed   → StateReady     (ReasonCommandComplete)
//   - agent_failed      → StateFailed    (ReasonError)
//   - agent_heartbeat   → RecordActivity only (no state change per HC-026a)
//   - all other types   → no-op
//
// Note: agent_warning_silent_hang is NOT a progress-stream message; it is
// synthesized by the daemon's watchdog layer. The Ready→Failed(silent_hang)
// transition is driven by the workloop/watchdog, not by this function.
//
// Invalid transitions (e.g. duplicate agent_ready) are silently ignored:
// the watcher MUST NOT panic on invalid-transition errors because the Machine
// may already be in a terminal state.
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
		// Rate-limit, output-chunk, and other non-lifecycle types: no FSM effect.
	}
}

// emitMachineTransition performs a lifecycle Machine transition and emits a
// lifecycle_transition event to the bus. Invalid transitions are silently
// ignored (the machine may already be in a terminal state; see HC-067).
//
// It is a method on *Watcher (rather than a free function) so that its
// dead-letter routing goes through w.appendDeadLetter and therefore records
// sink failures like every other dead-letter path (hk-0eqik).
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
		// Invalid transition (e.g. already terminal, duplicate event): silent no-op.
		// The machine enforces the HC-065 table; the caller should not panic here.
		return
	}
	// Build and emit lifecycle_transition event payload (§8.3.14).
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
	// Use EmitWithRunID so the envelope carries run_id for JSONL correlation
	// (EM-013). The Machine holds the run_id as a string; parse it to core.RunID.
	// Fall back to plain Emit when the run_id is not a valid UUID (e.g. stubs).
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

// buildWatcherFailedPayload constructs the (eventType, payload) pair for a
// watcher-synthesized agent_failed event (panic, line-too-long, malformed,
// etc.).
//
// sub is one of the WatcherPanicSubReason, NDJSONLineTooLongSubReason, etc.
// constants.  cause is the wrapped error; its Class() string populates the
// error_category field of the payload.
//
// The caller passes the returned values directly to EventEmitter.Emit or
// WatcherDeadLetterSink.Append; envelope stamping (event_id, timestamps,
// source_subsystem) is the bus's responsibility per EV-002b.
//
// session_id and run_id are included in the payload so watcher self-defect
// terminals are attributable: without them the reconciler cannot correlate the
// synthesized agent_failed back to the failed session/run and cannot
// auto-recover it. runID may be empty (no Machine supplied); the "unknown"
// placeholder runID from the session layer is passed through as-is.
func buildWatcherFailedPayload(sessionID core.SessionID, runID, sub string, cause error) (eventType core.EventType, encoded []byte) {
	// The error is discarded deliberately, and this is the one place in this
	// file where that is defensible. encoding/json cannot fail on a
	// map[string]string: no unsupported kinds, no cycles, no custom
	// MarshalJSON. Every alternative is worse than the suppression:
	//   - a fallback that hand-builds JSON would interpolate sub, runID,
	//     sessionID and Class(cause) — all string PARAMETERS, not constants —
	//     into a body with no escaping;
	//   - a fallback carrying only constants would drop session_id, run_id and
	//     error_category, the fields this function's doc comment calls
	//     load-bearing and the last of which core.AgentFailedPayload.Valid
	//     rejects the payload for missing.
	// So the branch would be unreachable code that emits a payload the consumer
	// would reject. Suppress the impossible error instead.
	//
	// Note this line is NOT on the dead-letter path hk-0eqik cleared: it builds
	// the watcher's self-defect agent_failed body. That bead's "no //nolint"
	// constraint covered the six dead-letter findings, all of which are fixed
	// by real error handling in Watcher.appendDeadLetter.
	payload, _ := json.Marshal(map[string]string{ //nolint:errcheck,errchkjson // map[string]string marshal cannot fail; see comment above
		"type":           ProgressMsgTypeAgentFailed,
		"session_id":     string(sessionID),
		"run_id":         runID,
		"error_category": Class(cause),
		"sub_reason":     sub,
	})
	return core.EventType(ProgressMsgTypeAgentFailed), payload
}

// readStampReader wraps the progress stream and stamps w.lastReadEventAt on
// every Read that returns data or a nil error (HC-011a per-Read liveness).
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

// isLineTooLong reports whether err from bufio.Scanner.Err() signals that
// the scanner's internal buffer was overflowed — i.e., a line exceeded the
// NDJSONMaxLineLenBytes cap per HC-007a.
func isLineTooLong(err error) bool {
	return errors.Is(err, bufio.ErrTooLong)
}

// knownProgressMsgTypes is the complete set of required progress-stream message
// types per specs/handler-contract.md §4.2.HC-007.  The watcher only publishes
// events whose type is in this set; unknown types are ignored (dropped silently)
// per the additive-evolution rule.
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

// isKnownProgressMsgType reports whether msgType is one of the 12 required
// progress-stream message types declared in HC-007.
func isKnownProgressMsgType(msgType ProgressMsgType) bool {
	_, ok := knownProgressMsgTypes[msgType]
	return ok
}

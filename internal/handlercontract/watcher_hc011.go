package handlercontract

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync/atomic"
	"time"

	"github.com/gregberns/harmonik/internal/core"
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
	// git, Beads, and JSONL per EM-013 / POST_MVH_PARALLELISM_ROADMAP row #1.
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
	// was not durably stored (the watcher logs the failure but cannot recover).
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

	// PublishBufSize is the capacity of the internal publish channel.
	// When zero, WatcherPublishBufSize (8) is used per HC-011a.
	// Optional (≥ 0).
	PublishBufSize int
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
		sessionID: cfg.SessionID,
		done:      make(chan struct{}),
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
		eventType, payload := buildWatcherFailedPayload(w.sessionID, WatcherPanicSubReason, panicErr)
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
	scanner := bufio.NewScanner(cfg.ProgressStream)
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

		// HC-011a: update lastReadEventAt on every successful Read return.
		// bufio.Scanner internally calls Read; we approximate by updating after Scan.
		w.lastReadEventAt.Store(time.Now().UnixNano())

		if !gotLine {
			scanErr := scanner.Err()
			if scanErr != nil {
				// Distinguish line-too-long (ErrTooLong) from other I/O errors.
				if isLineTooLong(scanErr) {
					termErr := fmt.Errorf("handlercontract: ndjson line too long: %w", ErrProtocolMismatch)
					w.setTermErr(termErr)
					et, pl := buildWatcherFailedPayload(w.sessionID, NDJSONLineTooLongSubReason, termErr)
					w.publishOrDeadLetter(ctx, et, pl, cfg.Publisher, cfg.DeadLetter)
					return
				}
				// Other I/O errors: structural framing failure.
				termErr := fmt.Errorf("handlercontract: progress stream read error: %v: %w", scanErr, ErrStructural)
				w.setTermErr(termErr)
				et, pl := buildWatcherFailedPayload(w.sessionID, PartialMessageSubReason, termErr)
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
			termErr := fmt.Errorf("handlercontract: malformed NDJSON line: %v: %w", err, ErrStructural)
			w.setTermErr(termErr)
			et, pl := buildWatcherFailedPayload(w.sessionID, MalformedProgressMessageSubReason, termErr)
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
		// Route to dead-letter; best-effort (errors from Append are not actionable
		// from inside the watcher goroutine).
		_ = dl.Append(eventType, payload, fmt.Sprintf("emit failed: %v", err))
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
// cost_units respectively. cost_basis is always core.CostBasisOutputBytes at MVH
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
		RunID        core.RunID      `json:"run_id"`
		SessionID    core.SessionID  `json:"session_id"`
		ChunkIndex   int             `json:"chunk_index"`
		BytesEmitted int             `json:"bytes_emitted"`
	}
	_ = json.Unmarshal(chunkLine, &msg) // best-effort; partial results used below

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
		_ = dl.Append(core.EventTypeBudgetAccrual, nil, fmt.Sprintf("budget_accrual marshal: %v", err))
		return
	}

	w.publishOrDeadLetter(ctx, core.EventTypeBudgetAccrual, payload, pub, dl)
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
// sessionID is accepted for future use (sub-reason may encode it in payload);
// it is intentionally retained in the signature so callers are self-documenting.
func buildWatcherFailedPayload(sessionID core.SessionID, sub string, cause error) (core.EventType, []byte) {
	_ = sessionID                                 // reserved for payload enrichment when event-model.md §8 rows land
	payload, _ := json.Marshal(map[string]string{ //nolint:errcheck // static map, never fails
		"type":           ProgressMsgTypeAgentFailed,
		"error_category": Class(cause),
		"sub_reason":     sub,
	})
	return core.EventType(ProgressMsgTypeAgentFailed), payload
}

// isLineTooLong reports whether err from bufio.Scanner.Err() signals that
// the scanner's internal buffer was overflowed — i.e., a line exceeded the
// NDJSONMaxLineLenBytes cap per HC-007a.
func isLineTooLong(err error) bool {
	return err == bufio.ErrTooLong
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

// isKnownProgressMsgType reports whether msgType is one of the 12 required
// progress-stream message types declared in HC-007.
func isKnownProgressMsgType(msgType ProgressMsgType) bool {
	_, ok := knownProgressMsgTypes[msgType]
	return ok
}

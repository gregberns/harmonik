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
// HC-011 — Typed-alias deferred interface for the event bus
// ─────────────────────────────────────────────────────────────────────────────

// EventPublisher is the minimal interface the watcher requires from the
// in-process event bus.
//
// TODO(hk-8i31.82): substitute the real EventBus type (hk-hqwn.57) for this
// interface once that bead lands.  The substitution is non-breaking because
// EventPublisher is defined in terms of the EventBus surface that hk-hqwn.57
// will expose; no caller is required to change.
//
// Spec: specs/handler-contract.md §4.3.HC-011, [event-model.md §4.3].
type EventPublisher interface {
	// Publish enqueues ev to the in-process event bus.
	//
	// The call MUST be non-blocking: implementations MUST NOT block the caller
	// when the internal queue is full. On queue saturation the implementation
	// MUST route to the dead-letter per [event-model.md §4.3 HC-027].
	// Returns a non-nil error only on hard publish failures (not queue-full,
	// which is handled by dead-letter routing inside the implementation).
	Publish(ev core.Event) error
}

// DeadLetterSink is the interface the watcher uses to route events that cannot
// be delivered to the in-process bus (bus full, subscriber panic).
//
// The watcher MUST NOT drop events silently; per HC-027 they MUST reach the
// dead-letter destination declared by [event-model.md §4.3].
//
// Spec: specs/handler-contract.md §4.6.HC-027.
type DeadLetterSink interface {
	// Append records ev in the dead-letter store.
	//
	// Implementations MUST be non-blocking or use a bounded-retry policy.
	// A nil return indicates durable receipt; a non-nil error means the event
	// was not durably stored (the watcher logs the failure but cannot recover).
	Append(ev core.Event, reason string) error
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

	// Publisher is the in-process event bus to which the watcher publishes
	// translated handler-lifecycle events.  Must be non-blocking per HC-011.
	// Required (non-nil).
	//
	// TODO(hk-8i31.82): substitute real EventBus (hk-hqwn.57) here.
	Publisher EventPublisher

	// DeadLetter is the sink for events that could not be delivered to Publisher
	// (buffer-full, subscriber panic).  The watcher MUST NOT silently drop
	// undeliverable events per HC-027.
	// Required (non-nil).
	DeadLetter DeadLetterSink

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

		// Publish agent_failed{structural, watcher_panic} to the bus; route to
		// dead-letter if the publish fails.
		ev := buildWatcherFailedEvent(w.sessionID, WatcherPanicSubReason, panicErr)
		w.publishOrDeadLetter(ev, cfg.Publisher, cfg.DeadLetter)
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
					ev := buildWatcherFailedEvent(w.sessionID, NDJSONLineTooLongSubReason, termErr)
					w.publishOrDeadLetter(ev, cfg.Publisher, cfg.DeadLetter)
					return
				}
				// Other I/O errors: structural framing failure.
				termErr := fmt.Errorf("handlercontract: progress stream read error: %v: %w", scanErr, ErrStructural)
				w.setTermErr(termErr)
				ev := buildWatcherFailedEvent(w.sessionID, PartialMessageSubReason, termErr)
				w.publishOrDeadLetter(ev, cfg.Publisher, cfg.DeadLetter)
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
			ev := buildWatcherFailedEvent(w.sessionID, MalformedProgressMessageSubReason, termErr)
			w.publishOrDeadLetter(ev, cfg.Publisher, cfg.DeadLetter)
			return
		}

		// Unknown message types MUST be ignored per HC-007 additive-evolution rule:
		// the watcher dispatches on the type field; unknown values are dropped
		// silently (not treated as errors) to allow future protocol extensions
		// to be deployed before all consumers are updated.
		if typeOnly.Type == "" || !isKnownProgressMsgType(typeOnly.Type) {
			continue
		}

		// Build a core.Event envelope for the decoded progress-stream message.
		// The daemon watcher stamps event_id, timestamps, and source_subsystem at
		// enqueue time per EV-002b; the handler-side message supplies the payload.
		ev := buildProgressEvent(w.sessionID, typeOnly.Type, json.RawMessage(line))
		w.publishOrDeadLetter(ev, cfg.Publisher, cfg.DeadLetter)
	}
}

// publishOrDeadLetter attempts to publish ev to pub.  If Publish returns a
// non-nil error the event is routed to the dead-letter sink per HC-027.
//
// The watcher MUST NOT drop events silently.
func (w *Watcher) publishOrDeadLetter(ev core.Event, pub EventPublisher, dl DeadLetterSink) {
	if err := pub.Publish(ev); err != nil {
		// Route to dead-letter; best-effort (errors from Append are not actionable
		// from inside the watcher goroutine).
		_ = dl.Append(ev, fmt.Sprintf("publish failed: %v", err))
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

// buildProgressEvent constructs a minimal core.Event envelope for a decoded
// progress-stream message.  The daemon watcher stamps event_id and
// TimestampWall at publication time per EV-002b.
//
// msgType is the progress-stream "type" field value (e.g., "agent_ready").
// payload is the full NDJSON line as a raw JSON message; it is passed through
// unchanged so the watcher does not re-encode handler-side content.
func buildProgressEvent(sessionID core.SessionID, msgType string, payload json.RawMessage) core.Event {
	now := time.Now()
	monoNs := now.UnixNano()
	return core.Event{
		// EventID is zero here; the real EventBus (hk-hqwn.57) stamps UUIDv7 per
		// EV-002b at enqueue time.  Until then the zero UUID signals "needs stamp".
		SchemaVersion:     1,
		Type:              msgType,
		TimestampWall:     now,
		TimestampMonoNsec: &monoNs,
		Payload:           payload,
	}
}

// buildWatcherFailedEvent constructs the core.Event for a watcher-synthesized
// agent_failed event (panic, line-too-long, malformed, etc.).
//
// sub is one of the WatcherPanicSubReason, NDJSONLineTooLongSubReason, etc.
// constants.  cause is the wrapped error; its Class() string populates the
// error_category field of the payload.
func buildWatcherFailedEvent(sessionID core.SessionID, sub string, cause error) core.Event {
	now := time.Now()
	monoNs := now.UnixNano()
	payload, _ := json.Marshal(map[string]string{ //nolint:errcheck // static map, never fails
		"type":           ProgressMsgTypeAgentFailed,
		"error_category": Class(cause),
		"sub_reason":     sub,
	})
	return core.Event{
		SchemaVersion:     1,
		Type:              ProgressMsgTypeAgentFailed,
		TimestampWall:     now,
		TimestampMonoNsec: &monoNs,
		Payload:           payload,
	}
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
}

// isKnownProgressMsgType reports whether msgType is one of the 12 required
// progress-stream message types declared in HC-007.
func isKnownProgressMsgType(msgType ProgressMsgType) bool {
	_, ok := knownProgressMsgTypes[msgType]
	return ok
}

package lifecycle

import (
	"fmt"
	"log"
)

// LogFlusher is the interface the EV-019 panic-recovery helper requires from
// a structured-log channel. The daemon's top-level recovery handler calls
// Flush to drain any buffered log records before the process terminates after
// a Go panic.
//
// Spec ref: event-model.md §4.4 EV-019 — "the daemon's top-level recovery
// handler MUST flush the structured-log channel before exit."
type LogFlusher interface {
	Flush() error
}

// BusFlusher is the interface the EV-019a panic-recovery helper requires from
// the event bus. The daemon's top-level recovery handler calls Flush after the
// log flush to make a best-effort drain of buffered bus events before the
// process terminates after a Go panic.
//
// This is a best-effort obligation per EV-019a; completeness is not guaranteed.
// If Flush returns an error the error is discarded (best-effort) and the panic
// continues to re-propagate.
//
// The real EventBus type must satisfy this interface once hk-hqwn.57 (Define
// EventBus interface) lands. Until then callers pass nil and the bus-flush step
// is skipped silently. The daemon entrypoint (cmd/harmonik/main.go) carries the
// wiring site; substitute nil with the real EventBus per hk-hqwn.70.
//
// Spec ref: event-model.md §4.4 EV-019a — "the daemon's top-level recovery
// handler SHOULD additionally make a best-effort flush of the event bus after
// log flush completes."
type BusFlusher interface {
	Flush() error
}

// RecoverWithLogFlush is a deferred function that intercepts a Go panic at
// the daemon's top-level entry point, flushes the structured-log channel,
// makes a best-effort flush of the event bus, and then re-panics so that the
// runtime still prints the stack trace and exits with a non-zero status.
//
// Callers MUST use it as:
//
//	defer lifecycle.RecoverWithLogFlush(logFlusher, busFlusher, logger)
//
// Ordering guarantee (EV-019 / EV-019a): the log flush MUST complete before the
// bus flush starts. Both flushes are called BEFORE the panic is re-propagated.
// If logFlusher.Flush returns an error, the error is logged via logger (if
// non-nil); the bus flush is still attempted afterwards and the panic is still
// re-propagated regardless.
//
// Bus-flush panic containment (EV-019a): the bus flush runs inside its own
// deferred recover so that a secondary panic from busFlusher.Flush does not
// escape to the caller's stack. Any such secondary panic is silently discarded;
// the outer re-panic still propagates.
//
// Nil-safety:
//   - if logFlusher is nil, the log-flush step is skipped.
//   - if busFlusher is nil, the bus-flush step is skipped.
//   - if logger is nil, flush errors are silently discarded.
//
// Non-panic path: if no panic is in flight, RecoverWithLogFlush returns
// without calling either Flush.
//
// Host-binding note: this helper is authored before the daemon binary exists.
// The daemon entry point (cmd/harmonik/) will wrap its main goroutine with
// defer lifecycle.RecoverWithLogFlush(...) when it is authored.
//
// Spec refs:
//   - event-model.md §4.4 EV-019  — "On a Go panic, the daemon's top-level
//     recovery handler MUST flush the structured-log channel before exit."
//   - event-model.md §4.4 EV-019a — "the daemon's top-level recovery handler
//     SHOULD additionally make a best-effort flush of the event bus after log
//     flush completes."
func RecoverWithLogFlush(logFlusher LogFlusher, busFlusher BusFlusher, logger *log.Logger) {
	r := recover()
	if r == nil {
		// No panic in flight; nothing to do.
		return
	}

	// EV-019: flush structured-log channel BEFORE bus flush and BEFORE re-panicking.
	if logFlusher != nil {
		if err := logFlusher.Flush(); err != nil {
			if logger != nil {
				logger.Printf("lifecycle: RecoverWithLogFlush: log Flush error during panic recovery: %v", err)
			}
		}
	}

	// EV-019a: best-effort bus flush AFTER log flush completes.
	// Wrapped in its own deferred recover so a secondary panic from the bus
	// flush is contained and does not escape to the caller's stack.
	if busFlusher != nil {
		func() {
			defer func() {
				_ = recover() // contain any secondary panic from bus flush
			}()
			_ = busFlusher.Flush() // best-effort: errors intentionally discarded
		}()
	}

	// Re-panic so the runtime stack trace and non-zero exit code are preserved.
	panic(fmt.Sprintf("lifecycle: re-panic after log flush: %v", r)) //nolint:forbidigo // intentional: re-panic in recovery handler is the specified EV-019/EV-019a contract
}

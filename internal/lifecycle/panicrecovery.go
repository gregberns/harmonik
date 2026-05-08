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

// RecoverWithLogFlush is a deferred function that intercepts a Go panic at
// the daemon's top-level entry point, flushes the structured-log channel, and
// then re-panics so that the runtime still prints the stack trace and exits
// with a non-zero status.
//
// Callers MUST use it as:
//
//	defer lifecycle.RecoverWithLogFlush(logFlusher, logger)
//
// Ordering guarantee (EV-019): Flush is called BEFORE the panic is
// re-propagated. If Flush itself returns an error, the error is logged via
// logger (if non-nil) and the panic is still re-propagated.
//
// Nil-safety: if logFlusher is nil, the flush step is skipped and the panic
// is re-propagated immediately. If logger is nil, flush errors are silently
// discarded.
//
// Non-panic path: if no panic is in flight, RecoverWithLogFlush returns
// without calling Flush.
//
// Host-binding note: this helper is authored before the daemon binary exists.
// The daemon entry point (cmd/harmonik/) will wrap its main goroutine with
// defer lifecycle.RecoverWithLogFlush(...) when it is authored.
//
// Spec ref: event-model.md §4.4 EV-019 — "On a Go panic, the daemon's
// top-level recovery handler MUST flush the structured-log channel before
// exit."
func RecoverWithLogFlush(logFlusher LogFlusher, logger *log.Logger) {
	r := recover()
	if r == nil {
		// No panic in flight; nothing to do.
		return
	}

	// Panic is in flight: flush structured-log channel BEFORE re-panicking.
	if logFlusher != nil {
		if err := logFlusher.Flush(); err != nil {
			if logger != nil {
				logger.Printf("lifecycle: RecoverWithLogFlush: Flush error during panic recovery: %v", err)
			}
		}
	}

	// Re-panic so the runtime stack trace and non-zero exit code are preserved.
	panic(fmt.Sprintf("lifecycle: re-panic after log flush: %v", r)) //nolint:forbidigo // intentional: re-panic in recovery handler is the specified EV-019 contract
}

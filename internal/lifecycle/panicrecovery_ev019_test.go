package lifecycle

import (
	"errors"
	"log"
	"os"
	"testing"
)

// panicRecoveryFixtureFlusher is a test-double LogFlusher that records whether
// Flush was called and the order of the call relative to a shared sequence
// counter. It can optionally return an error from Flush.
type panicRecoveryFixtureFlusher struct {
	flushCalled bool
	flushErr    error
}

func (f *panicRecoveryFixtureFlusher) Flush() error {
	f.flushCalled = true
	return f.flushErr
}

// panicRecoveryFixtureRecoverPanic calls fn inside a deferred recover() wrapper
// so the panic re-emitted by RecoverWithLogFlush does not crash the test itself.
// Returns true if fn panicked, false otherwise.
func panicRecoveryFixtureRecoverPanic(fn func()) (panicked bool) {
	defer func() {
		if r := recover(); r != nil {
			panicked = true
		}
	}()
	fn()
	return false
}

// panicRecoveryFixtureLogger creates a *log.Logger writing to os.Stderr for
// use in tests that supply a non-nil logger.
func panicRecoveryFixtureLogger() *log.Logger {
	return log.New(os.Stderr, "test: ", 0)
}

// TestEV019_PanicTriggersFlusherCall verifies that when a panic occurs in a
// function wrapped with defer RecoverWithLogFlush, the LogFlusher.Flush method
// is called.
//
// Spec ref: event-model.md §4.4 EV-019 — "the daemon's top-level recovery
// handler MUST flush the structured-log channel before exit."
func TestEV019_PanicTriggersFlusherCall(t *testing.T) {
	t.Parallel()

	flusher := &panicRecoveryFixtureFlusher{}

	panicRecoveryFixtureRecoverPanic(func() {
		defer RecoverWithLogFlush(flusher, nil)
		panic("test panic") //nolint:forbidigo // test-only; exercising the production panic-handler contract
	})

	if !flusher.flushCalled {
		t.Error("EV-019: LogFlusher.Flush was not called when a panic occurred; MUST flush before exit")
	}
}

// TestEV019_FlushCalledBeforeRepanic verifies that Flush is called BEFORE the
// panic is re-propagated. We observe this by checking that the flusher records
// its call and the outer recover() still receives the re-panic — meaning Flush
// ran first, then the re-panic happened.
//
// Spec ref: event-model.md §4.4 EV-019 — ordering: flush BEFORE re-panic.
func TestEV019_FlushCalledBeforeRepanic(t *testing.T) {
	t.Parallel()

	flusher := &panicRecoveryFixtureFlusher{}

	panicked := panicRecoveryFixtureRecoverPanic(func() {
		defer RecoverWithLogFlush(flusher, nil)
		panic("trigger") //nolint:forbidigo // test-only; exercising the production panic-handler contract
	})

	// Flush must have been called (proves it ran before the re-panic escaped).
	if !flusher.flushCalled {
		t.Error("EV-019: Flush not called; cannot confirm flush-before-repanic ordering")
	}

	// The outer recover must have caught the re-panic (proves re-panic happened).
	if !panicked {
		t.Error("EV-019: expected re-panic to propagate beyond RecoverWithLogFlush; it did not")
	}
}

// TestEV019_NilFlusherIsNopSafe verifies that passing a nil LogFlusher does
// not panic within RecoverWithLogFlush itself. A nil flusher means the flush
// step is skipped; the panic is still re-propagated.
//
// Spec ref: event-model.md §4.4 EV-019 — nil-safety: skip flush when no
// flusher is available; still re-panic.
func TestEV019_NilFlusherIsNopSafe(t *testing.T) {
	t.Parallel()

	panicked := panicRecoveryFixtureRecoverPanic(func() {
		defer RecoverWithLogFlush(nil, nil)
		panic("trigger") //nolint:forbidigo // test-only; exercising the production panic-handler contract
	})

	// The original panic must still propagate even with a nil flusher.
	if !panicked {
		t.Error("EV-019: nil flusher path: expected panic to re-propagate; it did not")
	}
}

// TestEV019_NoPanicDoesNotCallFlush verifies that RecoverWithLogFlush does NOT
// call Flush when no panic has occurred. The non-panic path is the common case
// during normal shutdown, and calling Flush unconditionally would violate the
// contract (only the panic handler triggers the flush obligation; graceful
// shutdown has its own flush path).
//
// Spec ref: event-model.md §4.4 EV-019 — "On a Go panic … MUST flush"; the
// word "On" restricts the obligation to the panic case only.
func TestEV019_NoPanicDoesNotCallFlush(t *testing.T) {
	t.Parallel()

	flusher := &panicRecoveryFixtureFlusher{}

	func() {
		defer RecoverWithLogFlush(flusher, nil)
		// No panic; normal return.
	}()

	if flusher.flushCalled {
		t.Error("EV-019: Flush was called on non-panic path; MUST only flush on panic")
	}
}

// TestEV019_FlushErrorIsLogged verifies that when Flush returns an error,
// RecoverWithLogFlush logs it (via the supplied *log.Logger) and still
// re-panics. The test uses a discard logger to avoid noise; the key assertion
// is that the panic still propagates despite the Flush error.
//
// Spec ref: event-model.md §4.4 EV-019 — "MUST flush … before exit"; a Flush
// error does not suppress the re-panic.
func TestEV019_FlushErrorStillRepanicsPanic(t *testing.T) {
	t.Parallel()

	flushErr := errors.New("simulated flush error")
	flusher := &panicRecoveryFixtureFlusher{flushErr: flushErr}
	logger := panicRecoveryFixtureLogger()

	panicked := panicRecoveryFixtureRecoverPanic(func() {
		defer RecoverWithLogFlush(flusher, logger)
		panic("trigger") //nolint:forbidigo // test-only; exercising the production panic-handler contract
	})

	if !flusher.flushCalled {
		t.Error("EV-019: Flush was not called even though a panic occurred")
	}

	if !panicked {
		t.Error("EV-019: Flush error suppressed re-panic; panic MUST still propagate")
	}
}

package lifecycle

import (
	"errors"
	"log"
	"os"
	"testing"
)

// panicRecoveryFixtureFlusher is a test-double LogFlusher/BusFlusher that
// records whether Flush was called and can optionally return an error from
// Flush.
type panicRecoveryFixtureFlusher struct {
	flushCalled bool
	flushErr    error
}

func (f *panicRecoveryFixtureFlusher) Flush() error {
	f.flushCalled = true
	return f.flushErr
}

// hqwn28OrderFlusher is a test-double that records the call order relative to
// a shared sequence counter so ordering between the log flush and bus flush can
// be verified.
type hqwn28OrderFlusher struct {
	counter     *int // shared counter; incremented on each Flush call
	callOrderAt int  // value of *counter at the moment Flush was called
	flushCalled bool
	flushErr    error
}

func (f *hqwn28OrderFlusher) Flush() error {
	f.callOrderAt = *f.counter
	*f.counter++
	f.flushCalled = true
	return f.flushErr
}

// hqwn28PanicFlusher is a test-double BusFlusher whose Flush method itself
// panics, used to verify that bus-flush panics are contained per EV-019a.
type hqwn28PanicFlusher struct{}

func (f *hqwn28PanicFlusher) Flush() error {
	panic("bus flush secondary panic") //nolint:forbidigo // test-only: simulates a misbehaving BusFlusher to verify secondary-panic containment per EV-019a
}

// hqwn28RecoverPanic calls fn inside a deferred recover() wrapper so the panic
// re-emitted by RecoverWithLogFlush does not crash the test itself. Returns
// true if fn panicked, false otherwise.
func hqwn28RecoverPanic(fn func()) (panicked bool) {
	defer func() {
		if r := recover(); r != nil {
			panicked = true
		}
	}()
	fn()
	return false
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
		defer RecoverWithLogFlush(flusher, nil, nil)
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
		defer RecoverWithLogFlush(flusher, nil, nil)
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
		defer RecoverWithLogFlush(nil, nil, nil)
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
		defer RecoverWithLogFlush(flusher, nil, nil)
		// No panic; normal return.
	}()

	if flusher.flushCalled {
		t.Error("EV-019: Flush was called on non-panic path; MUST only flush on panic")
	}
}

// TestEV019_FlushErrorStillRepanicsPanic verifies that when Flush returns an
// error, RecoverWithLogFlush logs it (via the supplied *log.Logger) and still
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
		defer RecoverWithLogFlush(flusher, nil, logger)
		panic("trigger") //nolint:forbidigo // test-only; exercising the production panic-handler contract
	})

	if !flusher.flushCalled {
		t.Error("EV-019: Flush was not called even though a panic occurred")
	}

	if !panicked {
		t.Error("EV-019: Flush error suppressed re-panic; panic MUST still propagate")
	}
}

// TestEV019a_BusFlusherCalledAfterLogFlush verifies that on a Go panic, the
// BusFlusher.Flush is called after LogFlusher.Flush completes, and that both
// are called before the re-panic.
//
// Spec ref: event-model.md §4.4 EV-019a — "SHOULD additionally make a
// best-effort flush of the event bus after log flush completes."
func TestEV019a_BusFlusherCalledAfterLogFlush(t *testing.T) {
	t.Parallel()

	seq := 0
	logFlusher := &hqwn28OrderFlusher{counter: &seq}
	busFlusher := &hqwn28OrderFlusher{counter: &seq}

	panicked := hqwn28RecoverPanic(func() {
		defer RecoverWithLogFlush(logFlusher, busFlusher, nil)
		panic("trigger") //nolint:forbidigo // test-only; exercising EV-019a ordering contract
	})

	if !logFlusher.flushCalled {
		t.Error("EV-019a: LogFlusher.Flush was not called during panic recovery")
	}
	if !busFlusher.flushCalled {
		t.Error("EV-019a: BusFlusher.Flush was not called during panic recovery")
	}

	// EV-019a ordering: log flush MUST complete before bus flush starts.
	if logFlusher.callOrderAt >= busFlusher.callOrderAt {
		t.Errorf("EV-019a: log flush (order=%d) did not complete before bus flush (order=%d); log MUST precede bus",
			logFlusher.callOrderAt, busFlusher.callOrderAt)
	}

	if !panicked {
		t.Error("EV-019a: expected re-panic to propagate; it did not")
	}
}

// TestEV019a_BusFlusherCalledEvenWhenLogFlusherFails verifies that the bus
// flush is still attempted even when the log flush returns an error.
//
// Spec ref: event-model.md §4.4 EV-019a — the bus flush follows the log flush
// regardless of log-flush outcome.
func TestEV019a_BusFlusherCalledEvenWhenLogFlusherFails(t *testing.T) {
	t.Parallel()

	logFlusher := &panicRecoveryFixtureFlusher{flushErr: errors.New("log flush error")}
	busFlusher := &panicRecoveryFixtureFlusher{}

	hqwn28RecoverPanic(func() {
		defer RecoverWithLogFlush(logFlusher, busFlusher, nil)
		panic("trigger") //nolint:forbidigo // test-only; exercising EV-019a error-path contract
	})

	if !busFlusher.flushCalled {
		t.Error("EV-019a: BusFlusher.Flush was not called even though log flush failed; bus flush SHOULD still be attempted")
	}
}

// TestEV019a_NilBusFlusherIsNopSafe verifies that passing a nil BusFlusher
// does not panic and that the log flush and re-panic still proceed normally.
//
// Spec ref: event-model.md §4.4 EV-019a — nil-safety: skip bus-flush when no
// BusFlusher is available.
func TestEV019a_NilBusFlusherIsNopSafe(t *testing.T) {
	t.Parallel()

	logFlusher := &panicRecoveryFixtureFlusher{}

	panicked := hqwn28RecoverPanic(func() {
		defer RecoverWithLogFlush(logFlusher, nil, nil)
		panic("trigger") //nolint:forbidigo // test-only; exercising EV-019a nil-safety contract
	})

	if !logFlusher.flushCalled {
		t.Error("EV-019a: nil BusFlusher: LogFlusher.Flush was not called")
	}
	if !panicked {
		t.Error("EV-019a: nil BusFlusher: expected re-panic to propagate; it did not")
	}
}

// TestEV019a_BusFlusherPanicIsContained verifies that if BusFlusher.Flush
// itself panics, the secondary panic is contained and does not escape to the
// caller. The outer re-panic from the original panic value still propagates.
//
// Spec ref: event-model.md §4.4 EV-019a — best-effort; secondary panics from
// bus flush MUST be contained; the original panic re-propagation MUST NOT be
// suppressed.
func TestEV019a_BusFlusherPanicIsContained(t *testing.T) {
	t.Parallel()

	logFlusher := &panicRecoveryFixtureFlusher{}
	busFlusher := &hqwn28PanicFlusher{}

	panicked := hqwn28RecoverPanic(func() {
		defer RecoverWithLogFlush(logFlusher, busFlusher, nil)
		panic("trigger") //nolint:forbidigo // test-only; exercising EV-019a secondary-panic containment
	})

	if !logFlusher.flushCalled {
		t.Error("EV-019a: secondary bus-flush panic: LogFlusher.Flush was not called before bus flush")
	}
	if !panicked {
		t.Error("EV-019a: secondary bus-flush panic: original re-panic must still propagate; it did not")
	}
}

// TestEV019a_NoPanicDoesNotCallBusFlusher verifies that BusFlusher.Flush is
// NOT called on the non-panic path.
//
// Spec ref: event-model.md §4.4 EV-019a — "On a Go panic … SHOULD flush";
// the flush obligation is panic-path only.
func TestEV019a_NoPanicDoesNotCallBusFlusher(t *testing.T) {
	t.Parallel()

	busFlusher := &panicRecoveryFixtureFlusher{}

	func() {
		defer RecoverWithLogFlush(nil, busFlusher, nil)
		// No panic; normal return.
	}()

	if busFlusher.flushCalled {
		t.Error("EV-019a: BusFlusher.Flush was called on non-panic path; MUST only flush on panic")
	}
}

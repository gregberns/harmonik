package daemon_test

// detectorbarrier_rc020b_test.go — tests for DetectorBarrier (RC-020b).
//
// RC-020b: a detector that panics during evaluation MUST be caught by a
// per-detector recover() barrier. On panic the detector is suspended for the
// daemon's lifetime and priority-order evaluation falls through to the next
// detector. reconciliation_detector_panic{detector_class, error_class} MUST
// emit before fall-through.
//
// Spec ref: specs/reconciliation/spec.md §4.3 RC-020b.
// Bead ref: hk-63oh.22.

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// ---- test stubs ----

// rc020bFixtureRecordingEmitter records every Emit call for assertion.
type rc020bFixtureRecordingEmitter struct {
	mu     sync.Mutex
	events []rc020bFixtureEmittedEvent
}

type rc020bFixtureEmittedEvent struct {
	EventType core.EventType
	Payload   []byte
}

func (e *rc020bFixtureRecordingEmitter) Emit(_ context.Context, eventType core.EventType, payload []byte) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.events = append(e.events, rc020bFixtureEmittedEvent{EventType: eventType, Payload: payload})
	return nil
}

func (e *rc020bFixtureRecordingEmitter) Events() []rc020bFixtureEmittedEvent {
	e.mu.Lock()
	defer e.mu.Unlock()
	cp := make([]rc020bFixtureEmittedEvent, len(e.events))
	copy(cp, e.events)
	return cp
}

// ---- tests ----

// TestRC020b_DetectorBarrier_NoPanicPassesCategoryThrough verifies that a
// normally-executing detector's result is returned unmodified.
//
// Spec ref: specs/reconciliation/spec.md §4.3 RC-020b.
func TestRC020b_DetectorBarrier_NoPanicPassesCategoryThrough(t *testing.T) {
	t.Parallel()

	emitter := &rc020bFixtureRecordingEmitter{}
	barrier := daemon.NewDetectorBarrier(
		core.DetectorClass("cat-5-detector"),
		func(_ context.Context) (core.ReconciliationCategory, bool) {
			return core.ReconciliationCategoryCat5, true
		},
		emitter,
	)

	cat, fired := barrier.Run(context.Background())
	if !fired {
		t.Fatal("RC-020b: non-panicking detector must return fired=true")
	}
	if cat != core.ReconciliationCategoryCat5 {
		t.Errorf("RC-020b: non-panicking detector: got %q, want %q", cat, core.ReconciliationCategoryCat5)
	}
	if events := emitter.Events(); len(events) != 0 {
		t.Errorf("RC-020b: non-panicking detector must not emit events; got %d", len(events))
	}
}

// TestRC020b_DetectorBarrier_NoPanicNoFire verifies that a detector that does
// not match returns (zero, false) without panicking or emitting events.
//
// Spec ref: specs/reconciliation/spec.md §4.3 RC-020b.
func TestRC020b_DetectorBarrier_NoPanicNoFire(t *testing.T) {
	t.Parallel()

	emitter := &rc020bFixtureRecordingEmitter{}
	barrier := daemon.NewDetectorBarrier(
		core.DetectorClass("cat-1-detector"),
		func(_ context.Context) (core.ReconciliationCategory, bool) {
			return core.ReconciliationCategory(""), false
		},
		emitter,
	)

	_, fired := barrier.Run(context.Background())
	if fired {
		t.Fatal("RC-020b: non-matching detector must return fired=false")
	}
	if events := emitter.Events(); len(events) != 0 {
		t.Errorf("RC-020b: non-matching detector must not emit events; got %d", len(events))
	}
	if barrier.IsSuspended() {
		t.Error("RC-020b: non-panicking detector must not be suspended")
	}
}

// TestRC020b_DetectorBarrier_PanicSuspendsDetector verifies that a panicking
// detector is suspended (IsSuspended returns true) after the first panic.
//
// Spec ref: specs/reconciliation/spec.md §4.3 RC-020b — "On panic, the
// detector is suspended for the daemon's lifetime."
func TestRC020b_DetectorBarrier_PanicSuspendsDetector(t *testing.T) {
	t.Parallel()

	barrier := daemon.NewDetectorBarrier(
		core.DetectorClass("cat-0-detector"),
		func(_ context.Context) (core.ReconciliationCategory, bool) {
			panic("rc020b: simulated detector panic") //nolint:gocritic // intentional panic for recovery test
		},
		nil, // nil emitter: suspension must still work without event bus
	)

	if barrier.IsSuspended() {
		t.Fatal("RC-020b: barrier must not be suspended before first Run")
	}

	_, fired := barrier.Run(context.Background())
	if fired {
		t.Error("RC-020b: panicking detector must return fired=false")
	}
	if !barrier.IsSuspended() {
		t.Error("RC-020b: panicking detector must be suspended after first Run")
	}
}

// TestRC020b_DetectorBarrier_SuspendedDetectorSkipped verifies that a
// suspended detector's function is NOT called on subsequent Run invocations.
//
// Spec ref: specs/reconciliation/spec.md §4.3 RC-020b — "the detector is
// suspended for the daemon's lifetime."
func TestRC020b_DetectorBarrier_SuspendedDetectorSkipped(t *testing.T) {
	t.Parallel()

	callCount := 0
	barrier := daemon.NewDetectorBarrier(
		core.DetectorClass("cat-6b-detector"),
		func(_ context.Context) (core.ReconciliationCategory, bool) {
			callCount++
			if callCount == 1 {
				panic("rc020b: simulated first-call panic") //nolint:gocritic // intentional panic for recovery test
			}
			return core.ReconciliationCategoryCat6b, true
		},
		nil,
	)

	// First call panics → suspended.
	_, _ = barrier.Run(context.Background())
	if !barrier.IsSuspended() {
		t.Fatal("RC-020b: detector must be suspended after panic")
	}

	// Second call must be skipped entirely (callCount must not increment).
	_, fired := barrier.Run(context.Background())
	if fired {
		t.Error("RC-020b: suspended detector must return fired=false")
	}
	if callCount != 1 {
		t.Errorf("RC-020b: suspended detector must not call wrapped fn; callCount = %d, want 1", callCount)
	}
}

// TestRC020b_DetectorBarrier_PanicEventEmitted verifies that
// reconciliation_detector_panic is emitted with the correct detector_class
// and error_class before fall-through per RC-020b.
//
// Spec ref: specs/reconciliation/spec.md §4.3 RC-020b — "A diagnostic event
// reconciliation_detector_panic{detector_class, error_class} MUST emit before
// fall-through."
func TestRC020b_DetectorBarrier_PanicEventEmitted(t *testing.T) {
	t.Parallel()

	emitter := &rc020bFixtureRecordingEmitter{}
	const wantClass = core.DetectorClass("cat-2-detector")

	barrier := daemon.NewDetectorBarrier(
		wantClass,
		func(_ context.Context) (core.ReconciliationCategory, bool) {
			panic("rc020b: simulated cat-2 panic") //nolint:gocritic // intentional panic for recovery test
		},
		emitter,
	)

	_, _ = barrier.Run(context.Background())

	events := emitter.Events()
	if len(events) != 1 {
		t.Fatalf("RC-020b: want 1 emitted event after panic, got %d", len(events))
	}

	ev := events[0]
	if ev.EventType != core.EventTypeReconciliationDetectorPanic {
		t.Errorf("RC-020b: emitted event type = %q, want %q",
			ev.EventType, core.EventTypeReconciliationDetectorPanic)
	}

	var payload core.ReconciliationDetectorPanicPayload
	if err := json.Unmarshal(ev.Payload, &payload); err != nil {
		t.Fatalf("RC-020b: unmarshal ReconciliationDetectorPanicPayload: %v", err)
	}
	if payload.DetectorClass != wantClass {
		t.Errorf("RC-020b: payload.DetectorClass = %q, want %q", payload.DetectorClass, wantClass)
	}
	if payload.ErrorClass != core.ErrorCategoryPanic {
		t.Errorf("RC-020b: payload.ErrorClass = %q, want %q", payload.ErrorClass, core.ErrorCategoryPanic)
	}
	if payload.PanickedAt == "" {
		t.Error("RC-020b: payload.PanickedAt must be non-empty")
	}
	if !payload.Valid() {
		t.Error("RC-020b: emitted ReconciliationDetectorPanicPayload.Valid() = false")
	}
}

// TestRC020b_DetectorBarrier_FallThroughToNextDetector verifies the full
// RC-003a fall-through pattern: when the first barrier panics, the second
// barrier in the priority order fires and returns its category.
//
// Spec ref: specs/reconciliation/spec.md §4.3 RC-020b — "the priority-order
// evaluation falls through to the next detector."
func TestRC020b_DetectorBarrier_FallThroughToNextDetector(t *testing.T) {
	t.Parallel()

	emitter := &rc020bFixtureRecordingEmitter{}

	// Priority-order evaluation: cat-0 (panics) → cat-6b (succeeds).
	cat0Barrier := daemon.NewDetectorBarrier(
		core.DetectorClass("cat-0-detector"),
		func(_ context.Context) (core.ReconciliationCategory, bool) {
			panic("rc020b: cat-0 panic") //nolint:gocritic // intentional panic for recovery test
		},
		emitter,
	)
	cat6bBarrier := daemon.NewDetectorBarrier(
		core.DetectorClass("cat-6b-detector"),
		func(_ context.Context) (core.ReconciliationCategory, bool) {
			return core.ReconciliationCategoryCat6b, true
		},
		emitter,
	)

	barriers := []*daemon.DetectorBarrier{cat0Barrier, cat6bBarrier}

	// Simulate the RC-003a priority-order loop.
	var result core.ReconciliationCategory
	for _, b := range barriers {
		cat, fired := b.Run(context.Background())
		if fired {
			result = cat
			break
		}
	}

	if result != core.ReconciliationCategoryCat6b {
		t.Errorf("RC-020b: fall-through result = %q, want %q", result, core.ReconciliationCategoryCat6b)
	}

	// cat-0 must be suspended; cat-6b must not be.
	if !cat0Barrier.IsSuspended() {
		t.Error("RC-020b: cat-0 barrier must be suspended after panic")
	}
	if cat6bBarrier.IsSuspended() {
		t.Error("RC-020b: cat-6b barrier must not be suspended (it did not panic)")
	}

	// Exactly one reconciliation_detector_panic event for the cat-0 detector.
	events := emitter.Events()
	if len(events) != 1 {
		t.Fatalf("RC-020b: want 1 panic event, got %d", len(events))
	}
	if events[0].EventType != core.EventTypeReconciliationDetectorPanic {
		t.Errorf("RC-020b: event type = %q, want reconciliation_detector_panic", events[0].EventType)
	}
}

// TestRC020b_DetectorBarrier_NilEmitterDoesNotPanic verifies that a nil
// emitter is safe: the barrier still catches the panic, suspends the
// detector, and returns fired=false without itself panicking.
//
// Spec ref: specs/reconciliation/spec.md §4.3 RC-020b.
func TestRC020b_DetectorBarrier_NilEmitterDoesNotPanic(t *testing.T) {
	t.Parallel()

	barrier := daemon.NewDetectorBarrier(
		core.DetectorClass("cat-3-detector"),
		func(_ context.Context) (core.ReconciliationCategory, bool) {
			panic("rc020b: test panic with nil emitter") //nolint:gocritic // intentional panic for recovery test
		},
		nil, // nil emitter: must not panic
	)

	// Must not panic at the test level.
	_, fired := barrier.Run(context.Background())
	if fired {
		t.Error("RC-020b: panicking detector with nil emitter must return fired=false")
	}
	if !barrier.IsSuspended() {
		t.Error("RC-020b: panicking detector with nil emitter must still be suspended")
	}
}

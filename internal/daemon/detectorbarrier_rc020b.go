package daemon

// detectorbarrier_rc020b.go — per-detector recover() barrier for RC-020b.
//
// RC-020b: a detector that panics during evaluation per RC-003a's first-match
// priority order MUST be caught by a per-detector recover() barrier (per
// process-lifecycle.md §4.6 PL-018a's per-goroutine recover obligation
// extended to detector functions). On panic, the detector is suspended for
// the daemon's lifetime and priority-order evaluation falls through to the
// next detector. A diagnostic event reconciliation_detector_panic
// {detector_class, error_class} MUST emit before fall-through.
//
// This file provides:
//
//   - DetectorFunc: the function signature every reconciliation detector
//     implements.
//   - DetectorBarrier: wraps a DetectorFunc with a per-invocation
//     recover() barrier, tracks suspension state, and emits the
//     reconciliation_detector_panic event on panic.
//
// Design notes:
//   - Suspension is lifetime-scoped to the DetectorBarrier instance.
//     The daemon creates one DetectorBarrier per detector class at startup;
//     a panic sets suspended=true and the detector skips all subsequent
//     calls within that daemon run.
//   - The emitter is an optional narrow interface (detectorPanicEmitter).
//     If nil, the panic is still caught and the detector is suspended;
//     only the event emission step is skipped. This allows the barrier to
//     be used in tests without a real event bus.
//   - recover() is called inside a nested closure so that the panic
//     value is captured before any deferred cleanup runs on the outer
//     goroutine.
//
// Spec ref: specs/reconciliation/spec.md §4.3 RC-020b.
// Bead ref: hk-63oh.22.

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/gregberns/harmonik/internal/core"
)

// DetectorFunc is the signature for a reconciliation detector function.
//
// The function inspects git + Beads + (optionally) JSONL evidence for a
// single in-flight run and returns:
//
//   - (category, true)  — the detector's rule fired; category is the assigned
//     ReconciliationCategory per RC-003a priority order.
//   - (zero, false)     — the detector's rule did not fire; evaluation falls
//     through to the next detector in the priority order.
//
// Detectors MUST be deterministic: the same (target_run_id, snapshot) MUST
// always produce the same result per RC-020a idempotency contract.
//
// Spec ref: specs/reconciliation/spec.md §4.3 RC-020b.
type DetectorFunc func(ctx context.Context) (core.ReconciliationCategory, bool)

// detectorPanicEmitter is the narrow event-emission interface required by
// DetectorBarrier. Any value whose Emit method matches is accepted.
//
// The interface is unexported intentionally: callers supply the real event
// bus (which satisfies handlercontract.EventEmitter) or a test stub; the
// barrier itself has no dependency on the bus import.
type detectorPanicEmitter interface {
	Emit(ctx context.Context, eventType core.EventType, payload []byte) error
}

// DetectorBarrier wraps a DetectorFunc with a per-invocation recover()
// barrier, tracks suspension state for the daemon's lifetime, and emits
// reconciliation_detector_panic events on panic per RC-020b.
//
// Zero value is not usable; construct via NewDetectorBarrier.
type DetectorBarrier struct {
	class   core.DetectorClass
	fn      DetectorFunc
	emitter detectorPanicEmitter

	mu        sync.Mutex
	suspended bool
}

// NewDetectorBarrier constructs a DetectorBarrier for the given detector
// class and function.
//
//   - class must be non-empty (DetectorClass is a string-backed opaque label).
//   - fn must be non-nil.
//   - emitter may be nil; when nil the barrier still catches panics and
//     suspends the detector, but the reconciliation_detector_panic event is
//     not emitted (useful in tests without a real event bus).
func NewDetectorBarrier(class core.DetectorClass, fn DetectorFunc, emitter detectorPanicEmitter) *DetectorBarrier {
	if class == "" {
		panic("detectorbarrier: detector class must be non-empty")
	}
	if fn == nil {
		panic("detectorbarrier: detector function must be non-nil")
	}
	return &DetectorBarrier{
		class:   class,
		fn:      fn,
		emitter: emitter,
	}
}

// IsSuspended reports whether this detector has been suspended due to a
// prior panic. A suspended detector is skipped for the remainder of the
// daemon's lifetime per RC-020b.
func (b *DetectorBarrier) IsSuspended() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.suspended
}

// Run executes the wrapped detector function with a per-invocation recover()
// barrier per RC-020b.
//
// Return values mirror DetectorFunc:
//
//   - (category, true)  — detector fired; category is the assigned class.
//   - (zero, false)     — detector did not fire, is suspended, or panicked.
//
// On panic: the barrier sets the detector to suspended, emits
// reconciliation_detector_panic (if emitter is non-nil), and returns
// (zero, false) so the caller falls through to the next detector in the
// RC-003a priority order.
//
// On a subsequent call after suspension: returns (zero, false) immediately
// without calling the wrapped function.
func (b *DetectorBarrier) Run(ctx context.Context) (cat core.ReconciliationCategory, fired bool) {
	if b.IsSuspended() {
		return cat, false
	}

	var panicVal interface{}
	cat, fired = func() (retCat core.ReconciliationCategory, retFired bool) {
		defer func() {
			if r := recover(); r != nil {
				panicVal = r
			}
		}()
		return b.fn(ctx)
	}()

	if panicVal == nil {
		return cat, fired
	}

	// Panic was caught: suspend detector for daemon lifetime.
	b.mu.Lock()
	b.suspended = true
	b.mu.Unlock()

	// Emit reconciliation_detector_panic before fall-through (RC-020b).
	b.emitPanicEvent(ctx, panicVal)

	return core.ReconciliationCategory(""), false
}

// emitPanicEvent marshals and emits the reconciliation_detector_panic event.
// Errors during emission are silently discarded per the best-effort contract:
// suspending the detector is the safety-critical action; event emission is
// observability.
func (b *DetectorBarrier) emitPanicEvent(ctx context.Context, panicVal interface{}) {
	if b.emitter == nil {
		return
	}

	payload := core.ReconciliationDetectorPanicPayload{
		DetectorClass: b.class,
		ErrorClass:    core.ErrorCategoryPanic,
		PanickedAt:    time.Now().UTC().Format(time.RFC3339),
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		// Marshal failure: discard silently; suspension already took effect.
		return
	}

	_ = b.emitter.Emit(ctx, core.EventTypeReconciliationDetectorPanic, payloadBytes)
	// Emit errors intentionally discarded: suspension is the authoritative
	// action; event emission is best-effort observability per RC-020b.
	_ = panicVal // panicVal is caught in the closure; value is recorded via ErrorClass
}

package daemon

import (
	"context"
	"encoding/json"
	"log/slog"
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

	b.mu.Lock()
	b.suspended = true
	b.mu.Unlock()

	b.emitPanicEvent(ctx, panicVal)

	return core.ReconciliationCategory(""), false
}

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
		return
	}

	if emitErr := b.emitter.Emit(ctx, core.EventTypeReconciliationDetectorPanic, payloadBytes); emitErr != nil {
		slog.WarnContext(ctx, "daemon: emit reconciliation_detector_panic failed", "err", emitErr, "detector_class", b.class)
	}
	_ = panicVal // panicVal is caught in the closure; value is recorded via ErrorClass
}

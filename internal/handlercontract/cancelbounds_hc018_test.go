package handlercontract_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// cancelBoundFixture — per-bead helper prefix for test helpers in this file
// (implementer-protocol.md §Helper-prefix discipline; bead hk-8i31.22).

// cancelBoundFixtureBlockingAdapter is a minimal Adapter whose CleanExitSequence
// blocks until ctx is cancelled, then returns ErrCanceled wrapped per §4.5.
// Used by HC-018 sensor tests to confirm that a conforming adapter propagates
// cancellation within CancelGoSideBound.
type cancelBoundFixtureBlockingAdapter struct{}

func (cancelBoundFixtureBlockingAdapter) DetectReady(_ core.EventEnvelope) bool { return false }
func (cancelBoundFixtureBlockingAdapter) DetectRateLimit(_ core.EventEnvelope) (bool, time.Duration) {
	return false, 0
}
func (cancelBoundFixtureBlockingAdapter) CleanExitSequence(ctx context.Context, _ handlercontract.Session) error {
	<-ctx.Done()
	return cancelBoundFixtureWrapCanceled(ctx.Err())
}
func (cancelBoundFixtureBlockingAdapter) RotateAccount(_ context.Context) error { return nil }
func (cancelBoundFixtureBlockingAdapter) Diagnose(_ context.Context) (handlercontract.DiagnosticReport, error) {
	return handlercontract.DiagnosticReport{}, handlercontract.ErrDeterministic
}

// cancelBoundFixtureAssertImplements is a compile-time check that
// cancelBoundFixtureBlockingAdapter satisfies the Adapter interface.
var cancelBoundFixtureAssertImplements handlercontract.Adapter = cancelBoundFixtureBlockingAdapter{}

// cancelBoundFixtureWrapCanceled wraps cause as ErrCanceled per §4.5.
func cancelBoundFixtureWrapCanceled(cause error) error {
	return fmt.Errorf("%w: %w", handlercontract.ErrCanceled, cause)
}

// ─────────────────────────────────────────────────────────────────────────────
// HC-018 bound constants — normative value checks
// ─────────────────────────────────────────────────────────────────────────────

// TestCancelBound_GoSideBoundValue verifies that CancelGoSideBound equals 500ms
// as required by specs/handler-contract.md §4.4.HC-018.
func TestCancelBound_GoSideBoundValue(t *testing.T) {
	t.Parallel()
	const want = 500 * time.Millisecond
	if handlercontract.CancelGoSideBound != want {
		t.Errorf("CancelGoSideBound = %v, want %v", handlercontract.CancelGoSideBound, want)
	}
}

// TestCancelBound_SubprocessBoundValue verifies that CancelSubprocessBound equals
// 5s as required by specs/handler-contract.md §4.4.HC-018.
func TestCancelBound_SubprocessBoundValue(t *testing.T) {
	t.Parallel()
	const want = 5 * time.Second
	if handlercontract.CancelSubprocessBound != want {
		t.Errorf("CancelSubprocessBound = %v, want %v", handlercontract.CancelSubprocessBound, want)
	}
}

// TestCancelBound_GoSideLessThanSubprocess verifies the ordering constraint:
// the Go-side bound must be strictly less than the subprocess cleanup bound,
// reflecting HC-018's layered escalation model (Go-side return first, hard
// termination per §4.6 only if subprocess cleanup exceeds the larger bound).
func TestCancelBound_GoSideLessThanSubprocess(t *testing.T) {
	t.Parallel()
	if handlercontract.CancelGoSideBound >= handlercontract.CancelSubprocessBound {
		t.Errorf(
			"CancelGoSideBound (%v) must be < CancelSubprocessBound (%v) per HC-018 escalation ordering",
			handlercontract.CancelGoSideBound,
			handlercontract.CancelSubprocessBound,
		)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// HC-018 — ErrCanceled wrapping requirement (§4.4, §4.5, §8.4)
// ─────────────────────────────────────────────────────────────────────────────

// TestCancelBound_ErrCanceledWrapsContextCanceled verifies that an error produced
// by wrapping context.Canceled as ErrCanceled (the pattern conforming adapters
// MUST follow per §4.5 and §8.4) satisfies both errors.Is(err, ErrCanceled) and
// Class(err) == "canceled".
func TestCancelBound_ErrCanceledWrapsContextCanceled(t *testing.T) {
	t.Parallel()

	err := cancelBoundFixtureWrapCanceled(context.Canceled)
	if err == nil {
		t.Fatal("expected non-nil error")
	}

	if !errors.Is(err, handlercontract.ErrCanceled) {
		t.Errorf("errors.Is(err, ErrCanceled) = false; HC-018/§4.5 requires cancellation errors to wrap ErrCanceled")
	}

	if got := handlercontract.Class(err); got != "canceled" {
		t.Errorf("Class(err) = %q, want %q", got, "canceled")
	}
}

// TestCancelBound_CleanExitSequenceHonoursContext verifies that a conforming
// CleanExitSequence implementation propagates ctx cancellation and returns
// within CancelGoSideBound (specs/handler-contract.md §4.4.HC-018).
//
// The test uses cancelBoundFixtureBlockingAdapter, which blocks on <-ctx.Done()
// and then returns ErrCanceled. After cancelling the context the test asserts:
//   - CleanExitSequence returns within CancelGoSideBound
//   - The returned error satisfies errors.Is(err, ErrCanceled)
func TestCancelBound_CleanExitSequenceHonoursContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())

	var a handlercontract.Adapter = cancelBoundFixtureBlockingAdapter{}

	done := make(chan error, 1)
	go func() {
		done <- a.CleanExitSequence(ctx, sessionFixtureStub{})
	}()

	// Cancel after a brief delay to ensure the goroutine has entered its block.
	time.AfterFunc(10*time.Millisecond, cancel)

	start := time.Now()
	select {
	case err := <-done:
		elapsed := time.Since(start)
		if elapsed > handlercontract.CancelGoSideBound {
			t.Errorf("CleanExitSequence returned after %v, exceeding CancelGoSideBound (%v)",
				elapsed, handlercontract.CancelGoSideBound)
		}
		if !errors.Is(err, handlercontract.ErrCanceled) {
			t.Errorf("CleanExitSequence returned %v; errors.Is(err, ErrCanceled) = false, want true", err)
		}
	case <-time.After(handlercontract.CancelGoSideBound + 100*time.Millisecond):
		cancel() // avoid goroutine leak
		t.Errorf("CleanExitSequence did not return within CancelGoSideBound (%v) after context cancellation",
			handlercontract.CancelGoSideBound)
	}
}

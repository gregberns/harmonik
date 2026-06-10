package handlercontract_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// turnBoundary — per-bead helper prefix for test helpers in this file.
// (implementer-protocol.md §Helper-prefix discipline; bead hk-8i31.16)

// ─────────────────────────────────────────────────────────────────────────────
// Test fixtures
// ─────────────────────────────────────────────────────────────────────────────

// turnBoundaryFixtureAdapter is a minimal Adapter implementation for HC-013a
// tests.  RotateAccount records each call and returns the configured error.
type turnBoundaryFixtureAdapter struct {
	mu        sync.Mutex
	calls     int
	returnErr error
}

func (a *turnBoundaryFixtureAdapter) DetectReady(_ core.EventEnvelope) bool { return false }
func (a *turnBoundaryFixtureAdapter) DetectRateLimit(_ core.EventEnvelope) (bool, time.Duration) {
	return false, 0
}

func (a *turnBoundaryFixtureAdapter) CleanExitSequence(_ context.Context, _ handlercontract.Session) error {
	return nil
}

func (a *turnBoundaryFixtureAdapter) RotateAccount(_ context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls++
	return a.returnErr
}

func (a *turnBoundaryFixtureAdapter) Diagnose(_ context.Context) (handlercontract.DiagnosticReport, error) {
	return handlercontract.DiagnosticReport{}, handlercontract.ErrDeterministic
}

func (a *turnBoundaryFixtureAdapter) callCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.calls
}

// Compile-time assertion: turnBoundaryFixtureAdapter satisfies Adapter.
var _ handlercontract.Adapter = (*turnBoundaryFixtureAdapter)(nil)

// ─────────────────────────────────────────────────────────────────────────────
// Tests
// ─────────────────────────────────────────────────────────────────────────────

// TestHC013a_IsTurnInFlight_InitiallyFalse verifies that a freshly created
// TurnBoundaryGuard reports IsTurnInFlight() = false.
//
// Spec ref: handler-contract.md §4.3.HC-013a.
func TestHC013a_IsTurnInFlight_InitiallyFalse(t *testing.T) {
	t.Parallel()

	g := handlercontract.NewTurnBoundaryGuard()
	if g.IsTurnInFlight() {
		t.Error("HC-013a: NewTurnBoundaryGuard: IsTurnInFlight() = true, want false")
	}
}

// TestHC013a_MarkTurnStarted_SetsInFlight verifies that MarkTurnStarted sets
// the in-flight state to true.
//
// Spec ref: handler-contract.md §4.3.HC-013a.
func TestHC013a_MarkTurnStarted_SetsInFlight(t *testing.T) {
	t.Parallel()

	g := handlercontract.NewTurnBoundaryGuard()
	g.MarkTurnStarted()
	if !g.IsTurnInFlight() {
		t.Error("HC-013a: after MarkTurnStarted: IsTurnInFlight() = false, want true")
	}
}

// TestHC013a_MarkTurnCompleted_ClearsInFlight verifies that MarkTurnCompleted
// clears the in-flight state.
//
// Spec ref: handler-contract.md §4.3.HC-013a.
func TestHC013a_MarkTurnCompleted_ClearsInFlight(t *testing.T) {
	t.Parallel()

	g := handlercontract.NewTurnBoundaryGuard()
	g.MarkTurnStarted()
	g.MarkTurnCompleted()
	if g.IsTurnInFlight() {
		t.Error("HC-013a: after MarkTurnCompleted: IsTurnInFlight() = true, want false")
	}
}

// TestHC013a_ScheduleRotateAccount_NoTurnInFlight verifies that when no turn
// is in-flight, ScheduleRotateAccount calls RotateAccount immediately.
//
// Spec ref: handler-contract.md §4.3.HC-013a — "at the next clean turn
// boundary: after agent_completed of one turn and before the next dispatch."
func TestHC013a_ScheduleRotateAccount_NoTurnInFlight(t *testing.T) {
	t.Parallel()

	g := handlercontract.NewTurnBoundaryGuard()
	a := &turnBoundaryFixtureAdapter{returnErr: nil}

	err := g.ScheduleRotateAccount(context.Background(), a, 5*time.Second)
	if err != nil {
		t.Errorf("HC-013a: ScheduleRotateAccount with no turn in-flight: got err %v, want nil", err)
	}
	if a.callCount() != 1 {
		t.Errorf("HC-013a: ScheduleRotateAccount: RotateAccount call count = %d, want 1", a.callCount())
	}
}

// TestHC013a_ScheduleRotateAccount_WaitsForTurnCompletion verifies that when a
// turn is in-flight, ScheduleRotateAccount blocks until MarkTurnCompleted is
// called, then calls RotateAccount.
//
// Spec ref: handler-contract.md §4.3.HC-013a — "watcher MUST schedule a
// requested rotation at the next clean turn boundary."
func TestHC013a_ScheduleRotateAccount_WaitsForTurnCompletion(t *testing.T) {
	t.Parallel()

	g := handlercontract.NewTurnBoundaryGuard()
	a := &turnBoundaryFixtureAdapter{returnErr: nil}

	g.MarkTurnStarted()

	// Schedule rotation in a goroutine — it should block.
	errC := make(chan error, 1)
	go func() {
		errC <- g.ScheduleRotateAccount(context.Background(), a, 5*time.Second)
	}()

	// Give the goroutine time to start and confirm it hasn't called RotateAccount.
	time.Sleep(20 * time.Millisecond)
	if a.callCount() != 0 {
		t.Errorf("HC-013a: ScheduleRotateAccount must not call RotateAccount mid-turn; got %d calls", a.callCount())
	}

	// Complete the turn; the goroutine should unblock and call RotateAccount.
	g.MarkTurnCompleted()

	select {
	case err := <-errC:
		if err != nil {
			t.Errorf("HC-013a: ScheduleRotateAccount after turn completion: got err %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("HC-013a: ScheduleRotateAccount did not unblock after MarkTurnCompleted")
	}

	if a.callCount() != 1 {
		t.Errorf("HC-013a: RotateAccount call count = %d, want 1", a.callCount())
	}
}

// TestHC013a_ScheduleRotateAccount_TimeoutReturnsErrTransient verifies that
// when the timeout elapses before a clean boundary, ErrTransient is returned.
//
// Spec ref: handler-contract.md §4.3.HC-013a — "If no quiescent turn boundary
// is observed within a configurable window, RotateAccount MUST return
// ErrTransient."
func TestHC013a_ScheduleRotateAccount_TimeoutReturnsErrTransient(t *testing.T) {
	t.Parallel()

	g := handlercontract.NewTurnBoundaryGuard()
	a := &turnBoundaryFixtureAdapter{returnErr: nil}

	g.MarkTurnStarted()

	// Use a very short timeout to trigger the ErrTransient path quickly.
	err := g.ScheduleRotateAccount(context.Background(), a, 10*time.Millisecond)
	if err == nil {
		t.Fatal("HC-013a: ScheduleRotateAccount with timeout: expected non-nil error, got nil")
	}
	if !errors.Is(err, handlercontract.ErrTransient) {
		t.Errorf("HC-013a: timeout error class: got %v, want wrapping ErrTransient", err)
	}
	if a.callCount() != 0 {
		t.Errorf("HC-013a: RotateAccount must not be called on timeout; got %d calls", a.callCount())
	}
}

// TestHC013a_ScheduleRotateAccount_CtxCancelReturnsErrCanceled verifies that
// context cancellation returns ErrCanceled while waiting for a turn boundary.
//
// Spec ref: handler-contract.md §4.3.HC-013a.
func TestHC013a_ScheduleRotateAccount_CtxCancelReturnsErrCanceled(t *testing.T) {
	t.Parallel()

	g := handlercontract.NewTurnBoundaryGuard()
	a := &turnBoundaryFixtureAdapter{returnErr: nil}

	g.MarkTurnStarted()

	ctx, cancel := context.WithCancel(context.Background())
	errC := make(chan error, 1)
	go func() {
		errC <- g.ScheduleRotateAccount(ctx, a, 30*time.Second)
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-errC:
		if err == nil {
			t.Fatal("HC-013a: ctx cancel: expected non-nil error, got nil")
		}
		if !errors.Is(err, handlercontract.ErrCanceled) {
			t.Errorf("HC-013a: ctx cancel error class: got %v, want wrapping ErrCanceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("HC-013a: ScheduleRotateAccount did not unblock after ctx cancel")
	}
}

// TestHC013a_MarkTurnStarted_PanicsWhenInFlight verifies that MarkTurnStarted
// panics when a turn is already in-flight (daemon defect detection).
//
// Spec ref: handler-contract.md §4.3.HC-013a — only one turn at a time.
func TestHC013a_MarkTurnStarted_PanicsWhenInFlight(t *testing.T) {
	t.Parallel()

	g := handlercontract.NewTurnBoundaryGuard()
	g.MarkTurnStarted()

	defer func() {
		r := recover()
		if r == nil {
			t.Error("HC-013a: MarkTurnStarted while in-flight: expected panic, got none")
		}
	}()

	g.MarkTurnStarted() // must panic
}

// TestHC013a_MarkTurnCompleted_PanicsWhenNotInFlight verifies that
// MarkTurnCompleted panics when no turn is in-flight (daemon defect detection).
//
// Spec ref: handler-contract.md §4.3.HC-013a.
func TestHC013a_MarkTurnCompleted_PanicsWhenNotInFlight(t *testing.T) {
	t.Parallel()

	g := handlercontract.NewTurnBoundaryGuard()

	defer func() {
		r := recover()
		if r == nil {
			t.Error("HC-013a: MarkTurnCompleted with no turn in-flight: expected panic, got none")
		}
	}()

	g.MarkTurnCompleted() // must panic
}

// TestHC013a_RotateAccountError_Propagated verifies that an error returned by
// RotateAccount is propagated by ScheduleRotateAccount.
//
// Spec ref: handler-contract.md §4.3.HC-013a.
func TestHC013a_RotateAccountError_Propagated(t *testing.T) {
	t.Parallel()

	g := handlercontract.NewTurnBoundaryGuard()
	sentinel := handlercontract.ErrDeterministic
	a := &turnBoundaryFixtureAdapter{returnErr: sentinel}

	err := g.ScheduleRotateAccount(context.Background(), a, 5*time.Second)
	if !errors.Is(err, sentinel) {
		t.Errorf("HC-013a: RotateAccount error propagation: got %v, want %v", err, sentinel)
	}
}

// TestHC013a_MultipleWaiters_AllUnblocked verifies that multiple concurrent
// callers of ScheduleRotateAccount are all unblocked when MarkTurnCompleted is
// called.
//
// Spec ref: handler-contract.md §4.3.HC-013a.
func TestHC013a_MultipleWaiters_AllUnblocked(t *testing.T) {
	t.Parallel()

	const n = 5
	g := handlercontract.NewTurnBoundaryGuard()
	g.MarkTurnStarted()

	adapters := make([]*turnBoundaryFixtureAdapter, n)
	errCs := make([]chan error, n)
	for i := 0; i < n; i++ {
		adapters[i] = &turnBoundaryFixtureAdapter{returnErr: nil}
		errCs[i] = make(chan error, 1)
		a := adapters[i]
		c := errCs[i]
		go func() {
			c <- g.ScheduleRotateAccount(context.Background(), a, 5*time.Second)
		}()
	}

	time.Sleep(30 * time.Millisecond)
	g.MarkTurnCompleted()

	for i := 0; i < n; i++ {
		select {
		case err := <-errCs[i]:
			if err != nil {
				t.Errorf("HC-013a: waiter %d: got err %v, want nil", i, err)
			}
		case <-time.After(2 * time.Second):
			t.Errorf("HC-013a: waiter %d did not unblock", i)
		}
	}
}

package daemon_test

// handlerpause_autoresume_0otqs_test.go — unit tests for auto-resume on timed
// backoff (hk-0otqs).
//
// Acceptance criteria per bead:
//   - Controller exposes a Schedule(type, after) primitive.
//   - Auto-resume must consult Adapter.Diagnose first; skip resume when Healthy=false.
//   - Hysteresis on auto-resume → re-pause to avoid flapping (backoff doubles).
//   - Operator can disable auto-resume per handler-type via config.
//
// Tests drive Schedule directly via the exported test-seam wrappers and use
// very short timers to exercise the timer path without real delays.
//
// Bead ref: hk-0otqs.

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

// arNewController builds a HandlerPauseController backed by a sealed bus.
func arNewController(t *testing.T) *daemon.HandlerPauseController {
	t.Helper()
	bus := eventbus.NewBusImpl()
	if err := bus.Seal(); err != nil {
		t.Fatalf("arNewController: bus.Seal: %v", err)
	}
	return daemon.NewHandlerPauseController(bus, nil)
}

// arPauseHandler calls Pause on ctrl with a minimal valid cause.
func arPauseHandler(t *testing.T, ctrl *daemon.HandlerPauseController, agentType core.AgentType) {
	t.Helper()
	ctx := context.Background()
	cause := core.HandlerPauseCause{
		FailureClass: core.FailureClassTransient,
		SubReason:    "rate_limit",
		SourceRunID:  "run-ar-001",
		SourceBeadID: "hk-ar001",
		TrippedAt:    time.Now().UTC().Format(time.RFC3339Nano),
	}
	if err := ctrl.Pause(ctx, agentType, cause, nil); err != nil {
		t.Fatalf("arPauseHandler: Pause: %v", err)
	}
}

// arWaitFor waits until fn returns true or the deadline is reached.
func arWaitFor(t *testing.T, fn func() bool, timeout time.Duration, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("arWaitFor: %s (timed out after %s)", msg, timeout)
}

// healthyAdapter is a handlercontract.Adapter stub that always returns Healthy=true.
type healthyAdapter struct{ handlercontract.Adapter }

func (healthyAdapter) DetectReady(_ core.EventEnvelope) bool { return false }
func (healthyAdapter) DetectRateLimit(_ core.EventEnvelope) (bool, time.Duration) {
	return false, 0
}

func (healthyAdapter) CleanExitSequence(_ context.Context, _ handlercontract.Session) error {
	return nil
}
func (healthyAdapter) RotateAccount(_ context.Context) error { return nil }
func (healthyAdapter) Diagnose(_ context.Context) (handlercontract.DiagnosticReport, error) {
	return handlercontract.DiagnosticReport{Healthy: true, Message: "healthy"}, nil
}

// unhealthyAdapter is a handlercontract.Adapter stub that always returns Healthy=false.
type unhealthyAdapter struct{ handlercontract.Adapter }

func (unhealthyAdapter) DetectReady(_ core.EventEnvelope) bool { return false }
func (unhealthyAdapter) DetectRateLimit(_ core.EventEnvelope) (bool, time.Duration) {
	return false, 0
}

func (unhealthyAdapter) CleanExitSequence(_ context.Context, _ handlercontract.Session) error {
	return nil
}
func (unhealthyAdapter) RotateAccount(_ context.Context) error { return nil }
func (unhealthyAdapter) Diagnose(_ context.Context) (handlercontract.DiagnosticReport, error) {
	return handlercontract.DiagnosticReport{Healthy: false, Message: "still rate-limited"}, nil
}

// noopAdapter is a handlercontract.Adapter stub that returns ErrDeterministic from Diagnose.
type noopAdapter struct{ handlercontract.Adapter }

func (noopAdapter) DetectReady(_ core.EventEnvelope) bool { return false }
func (noopAdapter) DetectRateLimit(_ core.EventEnvelope) (bool, time.Duration) {
	return false, 0
}

func (noopAdapter) CleanExitSequence(_ context.Context, _ handlercontract.Session) error {
	return nil
}
func (noopAdapter) RotateAccount(_ context.Context) error { return nil }
func (noopAdapter) Diagnose(_ context.Context) (handlercontract.DiagnosticReport, error) {
	return handlercontract.DiagnosticReport{}, handlercontract.ErrDeterministic
}

// countingAdapter counts Diagnose calls and optionally toggles health.
type countingAdapter struct {
	mu        sync.Mutex
	callCount int
	healthy   bool
}

func (a *countingAdapter) DetectReady(_ core.EventEnvelope) bool { return false }
func (a *countingAdapter) DetectRateLimit(_ core.EventEnvelope) (bool, time.Duration) {
	return false, 0
}

func (a *countingAdapter) CleanExitSequence(_ context.Context, _ handlercontract.Session) error {
	return nil
}
func (a *countingAdapter) RotateAccount(_ context.Context) error { return nil }
func (a *countingAdapter) Diagnose(_ context.Context) (handlercontract.DiagnosticReport, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.callCount++
	return handlercontract.DiagnosticReport{Healthy: a.healthy}, nil
}

func (a *countingAdapter) calls() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.callCount
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests
// ─────────────────────────────────────────────────────────────────────────────

// TestSchedule_NoopWhenDisabled verifies that Schedule is a no-op when auto-resume
// is disabled for the handler type.
func TestSchedule_NoopWhenDisabled(t *testing.T) {
	t.Parallel()

	ctrl := arNewController(t)
	at := core.AgentTypeClaudeCode

	// Disable auto-resume for this handler type.
	daemon.ExportedHandlerPauseControllerSetAutoResumeCfg(ctrl, at, daemon.ExportedAutoResumeConfig{
		Disabled: true,
	})
	ctrl.SetAdapter(healthyAdapter{})

	arPauseHandler(t, ctrl, at)
	if !ctrl.IsPaused(at) {
		t.Fatal("expected handler to be paused before Schedule")
	}

	// Schedule with a very short duration; the timer should NOT fire a resume
	// because auto-resume is disabled.
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	daemon.ExportedHandlerPauseControllerSchedule(ctrl, ctx, at, 20*time.Millisecond)

	// Wait slightly longer than the timer; handler should still be paused.
	time.Sleep(80 * time.Millisecond)
	if !ctrl.IsPaused(at) {
		t.Fatal("expected handler to still be paused when auto-resume is disabled")
	}
}

// TestSchedule_AutoResumesAfterDelay verifies the golden path: Schedule fires
// after the given duration and calls Resume when Adapter.Diagnose returns Healthy=true.
func TestSchedule_AutoResumesAfterDelay(t *testing.T) {
	t.Parallel()

	ctrl := arNewController(t)
	at := core.AgentTypeClaudeCode
	ctrl.SetAdapter(healthyAdapter{})

	arPauseHandler(t, ctrl, at)
	if !ctrl.IsPaused(at) {
		t.Fatal("expected handler to be paused")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Schedule an auto-resume after a short delay.
	daemon.ExportedHandlerPauseControllerSchedule(ctrl, ctx, at, 30*time.Millisecond)

	// Wait for the handler to become live.
	arWaitFor(t, func() bool { return !ctrl.IsPaused(at) }, time.Second, "handler to auto-resume")

	if ctrl.IsPaused(at) {
		t.Fatal("expected handler to be live after auto-resume")
	}
}

// TestSchedule_SkipsResumeWhenDiagnoseUnhealthy verifies that Schedule does NOT
// call Resume when Adapter.Diagnose returns Healthy=false.
func TestSchedule_SkipsResumeWhenDiagnoseUnhealthy(t *testing.T) {
	t.Parallel()

	ctrl := arNewController(t)
	at := core.AgentTypeClaudeCode
	ctrl.SetAdapter(unhealthyAdapter{})

	arPauseHandler(t, ctrl, at)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	daemon.ExportedHandlerPauseControllerSchedule(ctrl, ctx, at, 20*time.Millisecond)

	// Wait longer than the timer; handler should remain paused.
	time.Sleep(100 * time.Millisecond)
	if !ctrl.IsPaused(at) {
		t.Fatal("expected handler to remain paused when Diagnose returns Healthy=false")
	}
}

// TestSchedule_ProceedsWhenAdapterAbsent verifies that Schedule proceeds with
// resume when no adapter is set (nil → Diagnose not supported).
func TestSchedule_ProceedsWhenAdapterAbsent(t *testing.T) {
	t.Parallel()

	ctrl := arNewController(t)
	// No SetAdapter call — adapter is nil.
	at := core.AgentTypeClaudeCode

	arPauseHandler(t, ctrl, at)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	daemon.ExportedHandlerPauseControllerSchedule(ctrl, ctx, at, 30*time.Millisecond)

	arWaitFor(t, func() bool { return !ctrl.IsPaused(at) }, time.Second, "handler to auto-resume without adapter")
}

// TestSchedule_ProceedsWhenDiagnoseNotSupported verifies that Schedule proceeds
// with resume when Diagnose returns ErrDeterministic.
func TestSchedule_ProceedsWhenDiagnoseNotSupported(t *testing.T) {
	t.Parallel()

	ctrl := arNewController(t)
	ctrl.SetAdapter(noopAdapter{})
	at := core.AgentTypeClaudeCode

	arPauseHandler(t, ctrl, at)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	daemon.ExportedHandlerPauseControllerSchedule(ctrl, ctx, at, 30*time.Millisecond)

	arWaitFor(t, func() bool { return !ctrl.IsPaused(at) }, time.Second, "handler to auto-resume when Diagnose not supported")
}

// TestSchedule_CancelledWhenOperatorResumes verifies that a pending auto-resume
// goroutine is cancelled when the operator calls Resume before the timer fires.
func TestSchedule_CancelledWhenOperatorResumes(t *testing.T) {
	t.Parallel()

	adp := &countingAdapter{healthy: true}

	ctrl := arNewController(t)
	ctrl.SetAdapter(adp)
	at := core.AgentTypeClaudeCode

	arPauseHandler(t, ctrl, at)
	// Pause calls runDiagnose once.
	callsAfterPause := adp.calls() // baseline = 1

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Schedule with a long delay (500ms).
	daemon.ExportedHandlerPauseControllerSchedule(ctrl, ctx, at, 500*time.Millisecond)

	// Operator resumes immediately before the timer fires.
	if err := ctrl.Resume(context.Background(), at, core.HandlerResumedByOperator); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	// Resume also calls runDiagnose once.
	callsAfterResume := adp.calls() // baseline = 2

	// Wait past the original 500ms delay to confirm the goroutine did NOT fire.
	time.Sleep(600 * time.Millisecond)

	// The auto-resume goroutine should have been cancelled before the timer fired.
	// No additional Diagnose calls beyond those from Pause+Resume should appear.
	got := adp.calls()
	if got != callsAfterResume {
		t.Errorf("expected %d Diagnose calls (from Pause+Resume only), got %d; "+
			"the auto-resume goroutine fired unexpectedly", callsAfterResume, got)
	}
	_ = callsAfterPause
}

// TestSchedule_FlappingHysteresis verifies that a handler that flaps (re-paused
// quickly after auto-resume) causes the next Schedule call to double the backoff.
//
// We cannot directly observe the doubled duration, so we verify the functional
// consequence: after a flap, the handler remains paused longer than a single
// timer period because the effective duration is greater.
func TestSchedule_FlappingHysteresis(t *testing.T) {
	t.Parallel()

	ctrl := arNewController(t)
	ctrl.SetAdapter(healthyAdapter{})
	at := core.AgentTypeClaudeCode

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// First pause + Schedule.
	arPauseHandler(t, ctrl, at)
	daemon.ExportedHandlerPauseControllerSchedule(ctrl, ctx, at, 20*time.Millisecond)

	// Wait for first auto-resume.
	arWaitFor(t, func() bool { return !ctrl.IsPaused(at) }, time.Second, "first auto-resume")

	// Re-pause immediately (within the flap window) — triggers flap detection.
	arPauseHandler(t, ctrl, at)

	// Measure when we schedule the second auto-resume.
	scheduledAt := time.Now()
	const baseDelay = 20 * time.Millisecond
	// After 1 flap, effective delay = 20ms * 2 = 40ms.
	const expectedMinDelay = 35 * time.Millisecond // allow 5ms slack below 40ms

	daemon.ExportedHandlerPauseControllerSchedule(ctrl, ctx, at, baseDelay)

	// Wait for second auto-resume.
	arWaitFor(t, func() bool { return !ctrl.IsPaused(at) }, time.Second, "second auto-resume after flap")

	elapsed := time.Since(scheduledAt)
	if elapsed < expectedMinDelay {
		t.Errorf("expected second auto-resume to take at least %s (doubled backoff), got %s", expectedMinDelay, elapsed)
	}
}

// TestSchedule_NoopWhenNotPaused verifies that Schedule is a no-op when the
// handler is already live (not paused).
func TestSchedule_NoopWhenNotPaused(t *testing.T) {
	t.Parallel()

	ctrl := arNewController(t)
	ctrl.SetAdapter(healthyAdapter{})
	at := core.AgentTypeClaudeCode
	// Handler is live; do not call Pause.

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	// Schedule on a live handler; should be a no-op.
	daemon.ExportedHandlerPauseControllerSchedule(ctrl, ctx, at, 20*time.Millisecond)

	time.Sleep(60 * time.Millisecond)
	// Handler was never paused; it must still be live with no state corruption.
	if ctrl.IsPaused(at) {
		t.Fatal("expected handler to remain live after Schedule on a live handler")
	}
}

// TestSchedule_SupersedingScheduleCancelsOld verifies that a second Schedule call
// cancels any pending goroutine from the first call.
func TestSchedule_SupersedingScheduleCancelsOld(t *testing.T) {
	t.Parallel()

	adp := &countingAdapter{healthy: true}
	ctrl := arNewController(t)
	ctrl.SetAdapter(adp)
	at := core.AgentTypeClaudeCode

	arPauseHandler(t, ctrl, at)
	// Pause itself calls runDiagnose once.
	callsAfterPause := adp.calls() // 1

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// First Schedule with a long delay.
	daemon.ExportedHandlerPauseControllerSchedule(ctrl, ctx, at, 500*time.Millisecond)
	// Second Schedule immediately supersedes the first with a shorter delay.
	daemon.ExportedHandlerPauseControllerSchedule(ctrl, ctx, at, 30*time.Millisecond)

	arWaitFor(t, func() bool { return !ctrl.IsPaused(at) }, time.Second, "auto-resume from second Schedule")

	// Wait a bit extra to let goroutine 1 (if not cancelled) fire.
	time.Sleep(600 * time.Millisecond)

	// Expected calls: 1 (Pause) + 1 (doAutoResume from goroutine 2) + 1 (Resume from goroutine 2) = 3.
	// The first goroutine should have been cancelled.
	// If goroutine 1 also fired, the count would be higher (5+).
	got := adp.calls()
	expected := callsAfterPause + 2 // doAutoResume (1) + Resume (1)
	if got != expected {
		t.Errorf("expected %d Diagnose calls (Pause+doAutoResume+Resume), got %d; "+
			"first goroutine may not have been cancelled", expected, got)
	}
}

// TestHandlerResumedByAutoBackoffValid verifies that HandlerResumedByAutoBackoff
// satisfies the HandlerResumedBy.Valid() contract.
func TestHandlerResumedByAutoBackoffValid(t *testing.T) {
	t.Parallel()

	if !core.HandlerResumedByAutoBackoff.Valid() {
		t.Fatal("HandlerResumedByAutoBackoff.Valid() returned false; expected true")
	}
}

// TestSchedule_BackoffCapAtMaxBackoff verifies that backoff is capped at MaxBackoff.
//
// This is a functional cap test: we set MaxBackoff very short, create multiple
// flaps, and verify the handler still auto-resumes within a reasonable wall-clock
// window (i.e., the cap prevents unbounded growth).
func TestSchedule_BackoffCapAtMaxBackoff(t *testing.T) {
	t.Parallel()

	ctrl := arNewController(t)
	ctrl.SetAdapter(healthyAdapter{})
	at := core.AgentTypeClaudeCode

	// Set a tight MaxBackoff so even with many flaps the resume fires quickly.
	daemon.ExportedHandlerPauseControllerSetAutoResumeCfg(ctrl, at, daemon.ExportedAutoResumeConfig{
		MaxBackoff: 50 * time.Millisecond,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Simulate multiple flaps to exhaust backoff headroom.
	for i := 0; i < 5; i++ {
		arPauseHandler(t, ctrl, at)
		daemon.ExportedHandlerPauseControllerSchedule(ctrl, ctx, at, 10*time.Millisecond)
		arWaitFor(t, func() bool { return !ctrl.IsPaused(at) }, 2*time.Second, "auto-resume iteration")
		// Immediately re-pause to simulate flap (within autoResumeFlapWindow).
	}

	// After all flaps, the auto-resume must still fire (capped at MaxBackoff=50ms).
	arPauseHandler(t, ctrl, at)
	schedStart := time.Now()
	daemon.ExportedHandlerPauseControllerSchedule(ctrl, ctx, at, 10*time.Millisecond)
	arWaitFor(t, func() bool { return !ctrl.IsPaused(at) }, 2*time.Second, "auto-resume after multiple flaps")
	elapsed := time.Since(schedStart)

	// Should have fired within MaxBackoff (50ms) + some execution slack (100ms).
	maxExpected := 150 * time.Millisecond
	if elapsed > maxExpected {
		t.Errorf("auto-resume took %s but max expected %s (cap not enforced)", elapsed, maxExpected)
	}
}

// Compile-time assertion: the test adapter stubs implement handlercontract.Adapter.
var (
	_ handlercontract.Adapter = healthyAdapter{}
	_ handlercontract.Adapter = unhealthyAdapter{}
	_ handlercontract.Adapter = noopAdapter{}
	_ handlercontract.Adapter = (*countingAdapter)(nil)
)

// Compile-time assertion: ErrDeterministic is accessible from the handlercontract package.
var _ = errors.Is(handlercontract.ErrDeterministic, handlercontract.ErrDeterministic)

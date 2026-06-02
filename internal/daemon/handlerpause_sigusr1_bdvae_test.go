//go:build !windows

package daemon_test

// handlerpause_sigusr1_bdvae_test.go — tests for SIGUSR1-based external-trigger
// resume (hk-bdvae).
//
// Acceptance criteria per bead:
//   - At least one external trigger path implemented (SIGUSR1).
//   - All paths funnel through HandlerPauseController.Resume() — single transition logic.
//   - Authentication / authorisation model documented (OS-enforced, same UID or root).
//   - Test: trigger fires Resume; daemon-state transitions; events emitted.
//
// Testing strategy:
//   - Real OS signals are process-global and interfere with other test goroutines.
//     ExportedSignalResumeWatcherHandle calls handleSignalResume directly for unit tests.
//   - Signal delivery via Run is tested with a real SIGUSR1 sent to os.Getpid()
//     in a non-parallel test to avoid interference.
//
// Bead ref: hk-bdvae.

import (
	"context"
	"encoding/json"
	"os"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/eventbus"
)

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

// srSetup creates a HandlerPauseController whose bus has a pre-Seal subscriber
// that captures handler_resumed events.
//
// Returns the controller, watcher, and a function that blocks until at least
// one handler_resumed event is delivered (or timeout expires).
func srSetup(t *testing.T) (ctrl *daemon.HandlerPauseController, watcher *daemon.SignalResumeWatcher, resumedEvents func() []core.HandlerResumedPayload) {
	t.Helper()

	var mu sync.Mutex
	var collected []core.HandlerResumedPayload

	bus := eventbus.NewBusImpl()

	sub := core.Subscription{
		ConsumerID:    "test-signal-resume-observer-bdvae",
		ConsumerClass: core.ConsumerClassSynchronous,
		EventPattern: core.EventPattern{
			Types: map[string]struct{}{
				string(core.EventTypeHandlerResumed): {},
			},
		},
		OnPanic: core.OnPanicRecoverAndLog,
		Handler: func(_ context.Context, evt core.Event) error {
			var p core.HandlerResumedPayload
			if err := json.Unmarshal(evt.Payload, &p); err != nil {
				return nil
			}
			mu.Lock()
			collected = append(collected, p)
			mu.Unlock()
			return nil
		},
	}
	if _, err := bus.Subscribe(sub); err != nil {
		t.Fatalf("srSetup: bus.Subscribe: %v", err)
	}
	if err := bus.Seal(); err != nil {
		t.Fatalf("srSetup: bus.Seal: %v", err)
	}

	ctrl = daemon.NewHandlerPauseController(bus, nil)
	watcher = daemon.NewSignalResumeWatcher(ctrl, nil)

	resumedEvents = func() []core.HandlerResumedPayload {
		mu.Lock()
		defer mu.Unlock()
		out := make([]core.HandlerResumedPayload, len(collected))
		copy(out, collected)
		return out
	}
	return ctrl, watcher, resumedEvents
}

// srPause pauses a handler with a minimal valid cause.
func srPause(t *testing.T, ctrl *daemon.HandlerPauseController, agentType core.AgentType) {
	t.Helper()
	cause := core.HandlerPauseCause{
		FailureClass: core.FailureClassTransient,
		SubReason:    "rate_limit",
		SourceRunID:  "run-sr-001",
		SourceBeadID: "hk-sr001",
		TrippedAt:    time.Now().UTC().Format(time.RFC3339Nano),
	}
	if err := ctrl.Pause(context.Background(), agentType, cause, nil); err != nil {
		t.Fatalf("srPause: %v", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests
// ─────────────────────────────────────────────────────────────────────────────

// TestSignalResumeWatcher_ResumesAllPaused verifies the golden path:
// handleSignalResume resumes every paused handler, transitions state to live,
// and emits handler_resumed with by=signal.
func TestSignalResumeWatcher_ResumesAllPaused(t *testing.T) {
	t.Parallel()

	ctrl, watcher, resumedEvents := srSetup(t)
	at := core.AgentTypeClaudeCode

	srPause(t, ctrl, at)
	if !ctrl.IsPaused(at) {
		t.Fatal("expected handler to be paused before signal resume")
	}

	daemon.ExportedSignalResumeWatcherHandle(watcher, context.Background())

	// State transition: handler must now be live.
	if ctrl.IsPaused(at) {
		t.Fatal("expected handler to be live after signal resume")
	}

	// Events: at least one handler_resumed with by=signal.
	payloads := resumedEvents()
	if len(payloads) == 0 {
		t.Fatal("expected at least one handler_resumed event")
	}
	last := payloads[len(payloads)-1]
	if last.By != core.HandlerResumedBySignal {
		t.Errorf("expected by=%q, got by=%q", core.HandlerResumedBySignal, last.By)
	}
	if last.AgentType != at {
		t.Errorf("expected agent_type=%q, got %q", at, last.AgentType)
	}
	if last.PausedEpoch < 1 {
		t.Errorf("expected paused_epoch >= 1, got %d", last.PausedEpoch)
	}
}

// TestSignalResumeWatcher_NoopWhenNotPaused verifies that handleSignalResume
// is a no-op when no handler is paused (no error, no spurious events).
func TestSignalResumeWatcher_NoopWhenNotPaused(t *testing.T) {
	t.Parallel()

	_, watcher, resumedEvents := srSetup(t)

	daemon.ExportedSignalResumeWatcherHandle(watcher, context.Background())

	if payloads := resumedEvents(); len(payloads) != 0 {
		t.Errorf("expected no handler_resumed events when nothing is paused, got %d", len(payloads))
	}
}

// TestSignalResumeWatcher_MultipleHandlers verifies that handleSignalResume
// resumes all paused handlers when more than one is paused.
func TestSignalResumeWatcher_MultipleHandlers(t *testing.T) {
	t.Parallel()

	ctrl, watcher, resumedEvents := srSetup(t)
	at1 := core.AgentTypeClaudeCode
	at2 := core.AgentType("codex")

	srPause(t, ctrl, at1)
	srPause(t, ctrl, at2)

	if !ctrl.IsPaused(at1) || !ctrl.IsPaused(at2) {
		t.Fatal("expected both handlers to be paused before signal resume")
	}

	daemon.ExportedSignalResumeWatcherHandle(watcher, context.Background())

	if ctrl.IsPaused(at1) {
		t.Errorf("expected %s to be live after signal resume", at1)
	}
	if ctrl.IsPaused(at2) {
		t.Errorf("expected %s to be live after signal resume", at2)
	}

	payloads := resumedEvents()
	if len(payloads) < 2 {
		t.Errorf("expected at least 2 handler_resumed events, got %d", len(payloads))
	}
	for _, p := range payloads {
		if p.By != core.HandlerResumedBySignal {
			t.Errorf("expected by=signal, got by=%q", p.By)
		}
	}
}

// TestSignalResumeWatcher_RunExitsOnContextCancel verifies that Run returns
// when the context is cancelled (daemon shutdown path).
func TestSignalResumeWatcher_RunExitsOnContextCancel(t *testing.T) {
	t.Parallel()

	ctrl, _, _ := srSetup(t)
	watcher := daemon.NewSignalResumeWatcher(ctrl, nil)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		defer close(done)
		watcher.Run(ctx)
	}()

	cancel()

	select {
	case <-done:
		// Run exited as expected.
	case <-time.After(time.Second):
		t.Fatal("Run did not exit after context cancellation")
	}
}

// TestSignalResumeWatcher_RunResumesOnSIGUSR1 tests the full end-to-end signal
// delivery path: Run registers the signal handler; SIGUSR1 to the current process
// causes all paused handlers to be resumed.
//
// Not run in parallel to avoid SIGUSR1 interference with other test goroutines.
func TestSignalResumeWatcher_RunResumesOnSIGUSR1(t *testing.T) {
	ctrl, watcher, _ := srSetup(t)
	at := core.AgentTypeClaudeCode

	srPause(t, ctrl, at)
	if !ctrl.IsPaused(at) {
		t.Fatal("expected handler to be paused")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	go watcher.Run(ctx)

	// Give Run a moment to register the signal channel before sending.
	time.Sleep(20 * time.Millisecond)

	if err := syscall.Kill(os.Getpid(), syscall.SIGUSR1); err != nil {
		t.Fatalf("failed to send SIGUSR1: %v", err)
	}

	// Wait for the handler to become live.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if !ctrl.IsPaused(at) {
			return // success
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("handler was not resumed after SIGUSR1")
}

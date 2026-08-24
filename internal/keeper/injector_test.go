package keeper

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/substrate"
)

//nolint:gosec,errcheck // Real tmux commands use test-owned names and best-effort cleanup.
func TestResetTmuxInput_RemovesQueuedClear(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not available")
	}
	session := fmt.Sprintf("keeper-reset-input-%d", time.Now().UnixNano())
	start := exec.CommandContext(t.Context(), "tmux", "new-session", "-d", "-s", session, "bash --noprofile --norc")
	if out, err := start.CombinedOutput(); err != nil {
		t.Fatalf("start tmux: %v: %s", err, out)
	}
	t.Cleanup(func() { _ = exec.CommandContext(context.Background(), "tmux", "kill-session", "-t", session).Run() })

	if out, err := exec.CommandContext(t.Context(), "tmux", "send-keys", "-t", session, "-l", "/clear").CombinedOutput(); err != nil {
		t.Fatalf("queue clear: %v: %s", err, out)
	}
	capture := func() string {
		t.Helper()
		out, err := exec.CommandContext(t.Context(), "tmux", "capture-pane", "-p", "-t", session).CombinedOutput()
		if err != nil {
			t.Fatalf("capture tmux: %v: %s", err, out)
		}
		return string(out)
	}
	if got := capture(); !strings.Contains(got, "/clear") {
		t.Fatalf("queued clear not visible before reset: %q", got)
	}
	if err := ResetTmuxInput(t.Context(), session); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.CommandContext(t.Context(), "tmux", "send-keys", "-t", session, "Enter").CombinedOutput(); err != nil {
		t.Fatalf("submit prompt after reset: %v: %s", err, out)
	}
	time.Sleep(50 * time.Millisecond)
	if got := capture(); strings.Contains(got, "bash: /clear") || strings.Contains(got, "/clear: command not found") {
		t.Fatalf("queued clear executed after reset: %q", got)
	}
}

// TestInjectorSleep_FullDurationElapsed verifies that the injector's settle
// sleep returns true when the full duration elapses without cancellation.
func TestInjectorSleep_FullDurationElapsed(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	got := substrate.SystemClock{}.Sleep(ctx, 5*time.Millisecond)
	if !got {
		t.Error("Sleep: want true (full duration elapsed), got false")
	}
}

// TestInjectorSleep_CancelledBefore verifies that the settle sleep returns
// false when the context is already cancelled before the call.
func TestInjectorSleep_CancelledBefore(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before calling

	got := substrate.SystemClock{}.Sleep(ctx, 10*time.Second) // would block for 10s if not properly handled
	if got {
		t.Error("Sleep: want false (context already cancelled), got true")
	}
}

// TestInjectorSleep_CancelledDuring verifies that the settle sleep returns
// false when the context is cancelled while the sleep is in progress.
func TestInjectorSleep_CancelledDuring(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		time.Sleep(5 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	got := substrate.SystemClock{}.Sleep(ctx, 10*time.Second) // would block for 10s without cancel
	elapsed := time.Since(start)

	if got {
		t.Error("Sleep: want false (context cancelled during wait), got true")
	}
	if elapsed >= 2*time.Second {
		t.Errorf("Sleep: did not respect context cancellation (elapsed %v; want <2s)", elapsed)
	}
}

// TestInjectText_EmptyTargetReturnsError verifies that InjectText returns a
// non-nil error immediately when tmuxTarget is empty, without spawning any tmux
// process. This is the guard at the top of InjectText before any exec calls.
func TestInjectText_EmptyTargetReturnsError(t *testing.T) {
	t.Parallel()

	err := InjectText(context.Background(), "", "some text")
	if err == nil {
		t.Error("InjectText(\"\", ...): want non-nil error for empty target, got nil")
	}
}

// TestInjectText_SettleConstants verifies that the timing constants introduced
// in 55753ac (hk-89g) retain their designed values. A regression here would
// reintroduce the submit-Enter race where injected commands sit unsubmitted.
func TestInjectText_SettleConstants(t *testing.T) {
	t.Parallel()

	if submitSettle != 750*time.Millisecond {
		t.Errorf("submitSettle = %v; want 750ms (hk-89g: race window)", submitSettle)
	}

	if submitRetries != 2 {
		t.Errorf("submitRetries = %d; want 2 (hk-89g: bounded retry count)", submitRetries)
	}

	if submitRetryDelay != 400*time.Millisecond {
		t.Errorf("submitRetryDelay = %v; want 400ms (hk-89g: retry inter-delay)", submitRetryDelay)
	}
}

// TestInjectText_SettleCanBeOverriddenInTests verifies that submitSettle is a
// var (not a const), meaning tests can zero it out to skip the settle wait when
// invoking InjectText with a real tmux target in integration tests.
//
// Deliberately NOT t.Parallel(): it mutates the package-level submitSettle,
// which TestInjectText_SettleConstants (which IS parallel) reads. Go runs
// sequential top-level tests to completion before resuming any paused parallel
// one, so staying sequential is what keeps the mutation from interleaving with
// that assertion.
func TestInjectText_SettleCanBeOverriddenInTests(t *testing.T) {
	original := submitSettle
	defer func() { submitSettle = original }()

	submitSettle = 0
	if submitSettle != 0 {
		t.Error("submitSettle is not assignable; it must be a var, not a const")
	}
}

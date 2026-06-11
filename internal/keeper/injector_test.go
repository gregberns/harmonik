package keeper

// injector_test.go — unit tests for InjectText settle/retry sequencing (hk-89g).
//
// Tests in package keeper (not package keeper_test) so they can reach the
// unexported sleepCtx function and the package-level submitSettle var /
// submitRetries + submitRetryDelay constants that implement the sequencing.

import (
	"context"
	"testing"
	"time"
)

// ── sleepCtx ──────────────────────────────────────────────────────────────────

// TestSleepCtx_FullDurationElapsed verifies that sleepCtx returns true when the
// full duration elapses without context cancellation.
func TestSleepCtx_FullDurationElapsed(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	got := sleepCtx(ctx, 5*time.Millisecond)
	if !got {
		t.Error("sleepCtx: want true (full duration elapsed), got false")
	}
}

// TestSleepCtx_CancelledBefore verifies that sleepCtx returns false when the
// context is already cancelled before the call.
func TestSleepCtx_CancelledBefore(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before calling

	got := sleepCtx(ctx, 10*time.Second) // would block for 10s if not properly handled
	if got {
		t.Error("sleepCtx: want false (context already cancelled), got true")
	}
}

// TestSleepCtx_CancelledDuring verifies that sleepCtx returns false when the
// context is cancelled while the sleep is in progress.
func TestSleepCtx_CancelledDuring(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel the context after a brief delay, while sleepCtx is blocking on a
	// longer duration. sleepCtx must return false (not block for the full duration).
	go func() {
		time.Sleep(5 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	got := sleepCtx(ctx, 10*time.Second) // would block for 10s without cancel
	elapsed := time.Since(start)

	if got {
		t.Error("sleepCtx: want false (context cancelled during wait), got true")
	}
	if elapsed >= 2*time.Second {
		t.Errorf("sleepCtx: did not respect context cancellation (elapsed %v; want <2s)", elapsed)
	}
}

// ── InjectText validation ──────────────────────────────────────────────────────

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

// ── settle / retry constant regression guards ─────────────────────────────────

// TestInjectText_SettleConstants verifies that the timing constants introduced
// in 55753ac (hk-89g) retain their designed values. A regression here would
// reintroduce the submit-Enter race where injected commands sit unsubmitted.
func TestInjectText_SettleConstants(t *testing.T) {
	t.Parallel()

	// submitSettle must be 750ms — mirrors the daemon's splash-settle and gives
	// the Claude Code REPL enough time to finish ingesting the pasted text.
	if submitSettle != 750*time.Millisecond {
		t.Errorf("submitSettle = %v; want 750ms (hk-89g: race window)", submitSettle)
	}

	// submitRetries must be 2 — two redundant Enters defend against a dropped
	// first keypress without creating more than a harmless empty line.
	if submitRetries != 2 {
		t.Errorf("submitRetries = %d; want 2 (hk-89g: bounded retry count)", submitRetries)
	}

	// submitRetryDelay must be 400ms — long enough for the REPL to process
	// the first Enter before re-sending.
	if submitRetryDelay != 400*time.Millisecond {
		t.Errorf("submitRetryDelay = %v; want 400ms (hk-89g: retry inter-delay)", submitRetryDelay)
	}
}

// TestInjectText_SettleCanBeOverriddenInTests verifies that submitSettle is a
// var (not a const), meaning tests can zero it out to skip the settle wait when
// invoking InjectText with a real tmux target in integration tests.
func TestInjectText_SettleCanBeOverriddenInTests(t *testing.T) {
	t.Parallel()

	original := submitSettle
	defer func() { submitSettle = original }()

	submitSettle = 0
	if submitSettle != 0 {
		t.Error("submitSettle is not assignable; it must be a var, not a const")
	}
}

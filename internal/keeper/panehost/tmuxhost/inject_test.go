package tmuxhost

import (
	"context"
	"testing"
	"time"
)

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

	if SubmitSettle != 750*time.Millisecond {
		t.Errorf("SubmitSettle = %v; want 750ms (hk-89g: race window)", SubmitSettle)
	}

	if SubmitRetries != 2 {
		t.Errorf("SubmitRetries = %d; want 2 (hk-89g: bounded retry count)", SubmitRetries)
	}

	if SubmitRetryDelay != 400*time.Millisecond {
		t.Errorf("SubmitRetryDelay = %v; want 400ms (hk-89g: retry inter-delay)", SubmitRetryDelay)
	}
}

// TestInjectText_SettleCanBeOverriddenInTests verifies that SubmitSettle is a
// var (not a const), meaning tests can zero it out to skip the settle wait when
// invoking InjectText with a real tmux target in integration tests.
//
// Deliberately NOT t.Parallel(): it mutates the package-level SubmitSettle,
// which TestInjectText_SettleConstants (which IS parallel) reads. Go runs
// sequential top-level tests to completion before resuming any paused parallel
// one, so staying sequential is what keeps the mutation from interleaving with
// that assertion.
func TestInjectText_SettleCanBeOverriddenInTests(t *testing.T) {
	original := SubmitSettle
	defer func() { SubmitSettle = original }()

	SubmitSettle = 0
	if SubmitSettle != 0 {
		t.Error("SubmitSettle is not assignable; it must be a var, not a const")
	}
}

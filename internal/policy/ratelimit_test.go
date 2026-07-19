package policy

// ratelimit_test.go — pure truth-table tests for the rate-limit hysteresis
// reducer (StepRateLimit) and the budget-exhausted predicate. These
// migrate the pure decision logic out of the daemon's handlerpause_policy_37zy8
// tests: no controller, no bus, no RunRegistry — value-in / value-out.
//
// The daemon-side effect coverage (Cause stamping, IsPaused, epoch idempotence,
// the RunRegistry freeze-list) stays in package daemon.
//
// Spec ref: specs/handler-pause.md §5.

import (
	"testing"
)

// TestStepRateLimit_NoTripOnSingleRateLimit: one active event does not trip
// (hysteresis threshold = 2), and the counter advances to 1.
func TestStepRateLimit_NoTripOnSingleRateLimit(t *testing.T) {
	t.Parallel()

	v := StepRateLimit(
		RateLimitState{Consecutive: 0},
		RateLimitEvent{Active: true},
		DefaultRateLimitThreshold,
	)
	if v.Trip {
		t.Error("single active event tripped; want no-trip (threshold=2)")
	}
	if v.NewState.Consecutive != 1 {
		t.Errorf("Consecutive=%d, want 1", v.NewState.Consecutive)
	}
}

// TestStepRateLimit_TripOnTwoConsecutiveRateLimits: two consecutive active
// events (no intervening cleared) trip on the second.
func TestStepRateLimit_TripOnTwoConsecutiveRateLimits(t *testing.T) {
	t.Parallel()

	first := StepRateLimit(
		RateLimitState{Consecutive: 0},
		RateLimitEvent{Active: true},
		DefaultRateLimitThreshold,
	)
	if first.Trip {
		t.Fatal("tripped on first active; want no-trip yet")
	}

	second := StepRateLimit(
		first.NewState,
		RateLimitEvent{Active: true},
		DefaultRateLimitThreshold,
	)
	if !second.Trip {
		t.Error("did not trip on second consecutive active; want trip")
	}
	if second.NewState.Consecutive != 2 {
		t.Errorf("Consecutive=%d, want 2", second.NewState.Consecutive)
	}
}

// TestStepRateLimit_NoTripAfterClearance: active + cleared + active resets the
// counter, so the trailing active lands at count=1 and does not trip.
func TestStepRateLimit_NoTripAfterClearance(t *testing.T) {
	t.Parallel()

	s := StepRateLimit(RateLimitState{}, RateLimitEvent{Active: true}, DefaultRateLimitThreshold).NewState
	if s.Consecutive != 1 {
		t.Fatalf("after first active Consecutive=%d, want 1", s.Consecutive)
	}

	cleared := StepRateLimit(s, RateLimitEvent{Cleared: true}, DefaultRateLimitThreshold)
	if cleared.Trip {
		t.Error("cleared event tripped; want no-trip")
	}
	if cleared.NewState.Consecutive != 0 {
		t.Errorf("after cleared Consecutive=%d, want 0 (reset)", cleared.NewState.Consecutive)
	}

	again := StepRateLimit(cleared.NewState, RateLimitEvent{Active: true}, DefaultRateLimitThreshold)
	if again.Trip {
		t.Error("active after clearance tripped; want no-trip (counter was reset)")
	}
	if again.NewState.Consecutive != 1 {
		t.Errorf("Consecutive=%d, want 1", again.NewState.Consecutive)
	}
}

// TestStepRateLimit_NeitherIsNoOp: an event that is neither active nor cleared
// leaves state unchanged and never trips (mirrors the daemon's status switch
// with only the two known cases).
func TestStepRateLimit_NeitherIsNoOp(t *testing.T) {
	t.Parallel()

	in := RateLimitState{Consecutive: 1}
	v := StepRateLimit(in, RateLimitEvent{}, DefaultRateLimitThreshold)
	if v.Trip {
		t.Error("unknown-status event tripped; want no-trip")
	}
	if v.NewState != in {
		t.Errorf("NewState=%+v, want unchanged %+v", v.NewState, in)
	}
}

// TestBudgetExhaustedTrips: any valid budget_exhausted event trips (single-hit,
// no hysteresis).
func TestBudgetExhaustedTrips(t *testing.T) {
	t.Parallel()

	if !BudgetExhaustedTrips() {
		t.Error("BudgetExhaustedTrips()=false, want true (single-hit trip)")
	}
}

package policy

// ratelimit.go — the pure rate-limit hysteresis reducer and the
// budget-exhausted always-trip predicate for handler-pause.
//
// Moved out of internal/daemon/handlerpause_policy_37zy8.go (hk-37zy8) without
// semantic change: the daemon shell still owns the mutex-guarded per-agent-type
// counter map, the clock-stamped HandlerPauseCause, the RunRegistry freeze-list,
// and the Controller.Pause call. Only the decision — "does this event trip a
// pause, and what is the new counter?" — lives here.
//
// Spec ref: specs/handler-pause.md §5 (trigger taxonomy).

// DefaultRateLimitThreshold is the number of consecutive rate-limit active
// events required to trip a pause. Two consecutive hits (without an intervening
// cleared event) trigger a pause — the minimum hysteresis per hk-37zy8.
const DefaultRateLimitThreshold = 2

// RateLimitState is the hysteresis state for a single agent type: the count of
// consecutive rate-limit active events observed without an intervening cleared.
type RateLimitState struct {
	// Consecutive is the current consecutive active-event count.
	Consecutive int
}

// RateLimitEvent is the daemon's projection of a core.AgentRateLimitStatusPayload
// into the two booleans the reducer actually reads. The daemon performs this
// projection at the call site (BEFORE calling StepRateLimit), keeping uuid and
// the payload type out of the pure package.
//
//   - Cleared is true for an AgentRateLimitStatusCleared event.
//   - Active is true for an AgentRateLimitStatusActive event.
//
// An event that is neither (e.g. an unrecognised status) leaves state unchanged
// and never trips, mirroring the daemon's original switch with only the cleared
// and active cases.
type RateLimitEvent struct {
	Cleared bool
	Active  bool
}

// PauseVerdict is the reducer's value-out result: whether to trip a pause and
// the state to write back.
type PauseVerdict struct {
	// Trip is true when this event met the trip condition.
	Trip bool
	// NewState is the state the caller must persist for the next event.
	NewState RateLimitState
}

// StepRateLimit is the pure rate-limit hysteresis reducer. Given the current
// state, a projected event, and the consecutive-count threshold, it returns
// whether to trip a pause and the next state.
//
// Semantics (preserved exactly from handlerpause_policy_37zy8.go
// handleRateLimitStatus):
//   - cleared: reset the consecutive counter to 0; never trips.
//   - active:  increment the consecutive counter; trip when it reaches the
//     threshold (count >= threshold).
//   - neither: no state change; never trips.
//
// The cleared case is checked first so a cleared+active-in-one-event projection
// (which the daemon never produces) would reset rather than count.
func StepRateLimit(s RateLimitState, ev RateLimitEvent, threshold int) PauseVerdict {
	switch {
	case ev.Cleared:
		return PauseVerdict{Trip: false, NewState: RateLimitState{Consecutive: 0}}
	case ev.Active:
		count := s.Consecutive + 1
		return PauseVerdict{
			Trip:     count >= threshold,
			NewState: RateLimitState{Consecutive: count},
		}
	default:
		return PauseVerdict{Trip: false, NewState: s}
	}
}

// BudgetExhaustedTrips reports whether a (valid) budget_exhausted event should
// trip a pause. Budget exhaustion is a single-hit trigger with no hysteresis:
// any valid budget_exhausted event trips immediately (specs/handler-pause.md §5).
// The daemon still gates on payload.Valid() before calling this.
func BudgetExhaustedTrips() bool {
	return true
}

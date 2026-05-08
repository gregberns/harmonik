package lifecycle

import (
	"testing"
)

// supervisionFixtureDispatchDeferredReason is the exact reason string emitted
// by the per-daemon ceiling-exhaustion path.
//
// Spec ref: process-lifecycle.md §4.5 PL-014a — "Exceeding the ceiling MUST
// emit dispatch_deferred{reason='per_daemon_ceiling_exhausted'} (NOT the
// cross-daemon machine_ceiling_exhausted reason of ON-041)."
const supervisionFixtureDispatchDeferredReason = "per_daemon_ceiling_exhausted"

// supervisionFixtureDispatchDeferred is the event payload shape for a deferred
// dispatch. The reason field discriminates per-daemon ceiling exhaustion from
// the cross-daemon machine_ceiling_exhausted reason owned by operator-nfr.
//
// Spec ref: process-lifecycle.md §4.5 PL-014a — dispatch_deferred payload;
// event-model.md §8.7.13 (schema anchor).
type supervisionFixtureDispatchDeferred struct {
	Reason string `json:"reason"`
}

// supervisionFixtureMakeDispatchDeferred constructs a dispatch_deferred event
// payload for the per-daemon ceiling-exhaustion case.
//
// Spec ref: process-lifecycle.md §4.5 PL-014a — "Exceeding the ceiling MUST
// emit dispatch_deferred{reason='per_daemon_ceiling_exhausted'}."
func supervisionFixtureMakeDispatchDeferred() supervisionFixtureDispatchDeferred {
	return supervisionFixtureDispatchDeferred{
		Reason: supervisionFixtureDispatchDeferredReason,
	}
}

// supervisionFixtureCeilingExhaustedDetector simulates the daemon-side ceiling
// check: returns a dispatch_deferred payload when activeCount >= ceiling.
//
// Spec ref: process-lifecycle.md §4.5 PL-014a — "Exceeding the ceiling MUST
// emit dispatch_deferred{reason='per_daemon_ceiling_exhausted'}."
func supervisionFixtureCeilingExhaustedDetector(activeCount, ceiling uint64) (deferred bool, evt supervisionFixtureDispatchDeferred) {
	if activeCount >= ceiling {
		return true, supervisionFixtureMakeDispatchDeferred()
	}
	return false, supervisionFixtureDispatchDeferred{}
}

// TestPL014a_DispatchDeferred verifies the dispatch_deferred event shape and
// emission logic when the per-daemon concurrency ceiling is exhausted.
//
// Spec ref: process-lifecycle.md §4.5 PL-014a — "Exceeding the ceiling MUST
// emit dispatch_deferred{reason='per_daemon_ceiling_exhausted'} (NOT the
// cross-daemon machine_ceiling_exhausted reason of ON-041, which is the
// cross-daemon counterpart)."
func TestPL014a_DispatchDeferred(t *testing.T) {
	t.Parallel()

	t.Run("event-shape/reason-field-is-per-daemon-not-machine", func(t *testing.T) {
		t.Parallel()

		evt := supervisionFixtureMakeDispatchDeferred()
		if evt.Reason != "per_daemon_ceiling_exhausted" {
			t.Errorf("PL-014a dispatch_deferred: Reason = %q, want %q",
				evt.Reason, "per_daemon_ceiling_exhausted")
		}
		// Confirm it is NOT the cross-daemon reason.
		if evt.Reason == "machine_ceiling_exhausted" {
			t.Errorf("PL-014a dispatch_deferred: Reason must not be %q (cross-daemon ON-041 reason); use per_daemon_ceiling_exhausted",
				evt.Reason)
		}
	})

	t.Run("ceiling-exhausted/emits-deferred-when-at-limit", func(t *testing.T) {
		t.Parallel()

		const ceiling uint64 = 4
		// Exactly at the ceiling: deferred must fire.
		deferred, evt := supervisionFixtureCeilingExhaustedDetector(4, ceiling)
		if !deferred {
			t.Errorf("PL-014a: activeCount=ceiling=%d; expected deferred=true", ceiling)
		}
		if evt.Reason != supervisionFixtureDispatchDeferredReason {
			t.Errorf("PL-014a: deferred event Reason = %q, want %q", evt.Reason, supervisionFixtureDispatchDeferredReason)
		}
	})

	t.Run("ceiling-exhausted/emits-deferred-when-above-limit", func(t *testing.T) {
		t.Parallel()

		const ceiling uint64 = 4
		// Above the ceiling: deferred must fire.
		deferred, evt := supervisionFixtureCeilingExhaustedDetector(5, ceiling)
		if !deferred {
			t.Errorf("PL-014a: activeCount=5 > ceiling=%d; expected deferred=true", ceiling)
		}
		if evt.Reason != supervisionFixtureDispatchDeferredReason {
			t.Errorf("PL-014a: deferred event Reason = %q, want %q", evt.Reason, supervisionFixtureDispatchDeferredReason)
		}
	})

	t.Run("ceiling-exhausted/no-deferred-when-below-limit", func(t *testing.T) {
		t.Parallel()

		const ceiling uint64 = 4
		// Below the ceiling: dispatch is allowed; no deferred event.
		deferred, _ := supervisionFixtureCeilingExhaustedDetector(3, ceiling)
		if deferred {
			t.Errorf("PL-014a: activeCount=3 < ceiling=%d; expected deferred=false (dispatch allowed)", ceiling)
		}
	})

	t.Run("ceiling-exhausted/zero-ceiling-always-defers", func(t *testing.T) {
		t.Parallel()

		// A ceiling of 0 means no agents may run — every dispatch is deferred.
		deferred, evt := supervisionFixtureCeilingExhaustedDetector(0, 0)
		if !deferred {
			t.Error("PL-014a: ceiling=0; expected deferred=true (no agent slots available)")
		}
		if evt.Reason != supervisionFixtureDispatchDeferredReason {
			t.Errorf("PL-014a: deferred event Reason = %q, want %q", evt.Reason, supervisionFixtureDispatchDeferredReason)
		}
	})

	t.Run("ceiling-exhausted/fallback-cap-ceiling", func(t *testing.T) {
		t.Parallel()

		// Use the FALLBACK_CAP as the ceiling; activeCount = FALLBACK_CAP → deferred.
		ceiling := uint64(supervisionFixtureFallbackCap)
		deferred, evt := supervisionFixtureCeilingExhaustedDetector(ceiling, ceiling)
		if !deferred {
			t.Errorf("PL-014a: activeCount=FALLBACK_CAP=%d; expected deferred=true", ceiling)
		}
		if evt.Reason != supervisionFixtureDispatchDeferredReason {
			t.Errorf("PL-014a: FALLBACK_CAP exhausted event Reason = %q, want %q",
				evt.Reason, supervisionFixtureDispatchDeferredReason)
		}
	})
}

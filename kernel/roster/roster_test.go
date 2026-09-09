package roster

import (
	"testing"
	"time"

	kernelv1 "github.com/gregberns/harmonik/contract/gen/harmonik/kernel/v1"
)

// t0 is a fixed instant the tests pass in place of a clock read. Its only job is
// to prove LastSeen is carried from the argument, not fetched from time.Now.
var t0 = time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)

func TestEvaluate(t *testing.T) {
	cases := []struct {
		name       string
		obs        Observation
		wantState  kernelv1.Liveness_State
		wantReason kernelv1.Liveness_Reason
	}{
		{
			name:       "never probed is UNKNOWN",
			obs:        Observation{Probed: false},
			wantState:  kernelv1.Liveness_STATE_UNKNOWN,
			wantReason: kernelv1.Liveness_REASON_UNSPECIFIED,
		},
		{
			name:       "probed, zero failures is ALIVE",
			obs:        Observation{Probed: true, ConsecutiveFailures: 0},
			wantState:  kernelv1.Liveness_STATE_ALIVE,
			wantReason: kernelv1.Liveness_REASON_UNSPECIFIED,
		},
		{
			name:       "two failures is still ALIVE (below the suspect line)",
			obs:        Observation{Probed: true, ConsecutiveFailures: 2},
			wantState:  kernelv1.Liveness_STATE_ALIVE,
			wantReason: kernelv1.Liveness_REASON_UNSPECIFIED,
		},
		{
			name:       "three failures is SUSPECT",
			obs:        Observation{Probed: true, ConsecutiveFailures: SuspectAfter},
			wantState:  kernelv1.Liveness_STATE_SUSPECT,
			wantReason: kernelv1.Liveness_REASON_PROBE_TIMEOUT,
		},
		{
			name:       "five failures is still SUSPECT (below the dead line)",
			obs:        Observation{Probed: true, ConsecutiveFailures: DeadAfter - 1},
			wantState:  kernelv1.Liveness_STATE_SUSPECT,
			wantReason: kernelv1.Liveness_REASON_PROBE_TIMEOUT,
		},
		{
			name:       "six failures is DEAD",
			obs:        Observation{Probed: true, ConsecutiveFailures: DeadAfter},
			wantState:  kernelv1.Liveness_STATE_DEAD,
			wantReason: kernelv1.Liveness_REASON_PROBE_TIMEOUT,
		},
		{
			name:       "far past six is still DEAD",
			obs:        Observation{Probed: true, ConsecutiveFailures: 99},
			wantState:  kernelv1.Liveness_STATE_DEAD,
			wantReason: kernelv1.Liveness_REASON_PROBE_TIMEOUT,
		},
		{
			name:       "announced sleep is DEAD/ANNOUNCED_SLEEP without consuming probe failures",
			obs:        Observation{Probed: true, ConsecutiveFailures: 0, Intent: kernelv1.Node_INTENT_SLEEPING},
			wantState:  kernelv1.Liveness_STATE_DEAD,
			wantReason: kernelv1.Liveness_REASON_ANNOUNCED_SLEEP,
		},
		{
			name:       "announced drain is DEAD/ANNOUNCED_DRAIN without consuming probe failures",
			obs:        Observation{Probed: true, ConsecutiveFailures: 0, Intent: kernelv1.Node_INTENT_DRAINING},
			wantState:  kernelv1.Liveness_STATE_DEAD,
			wantReason: kernelv1.Liveness_REASON_ANNOUNCED_DRAIN,
		},
		{
			name:       "announced sleep wins even over a live peer",
			obs:        Observation{Probed: true, ConsecutiveFailures: 1, Intent: kernelv1.Node_INTENT_SLEEPING},
			wantState:  kernelv1.Liveness_STATE_DEAD,
			wantReason: kernelv1.Liveness_REASON_ANNOUNCED_SLEEP,
		},
		{
			name:       "INTENT_UP is not a departure, so state is probe-derived",
			obs:        Observation{Probed: true, ConsecutiveFailures: 0, Intent: kernelv1.Node_INTENT_UP},
			wantState:  kernelv1.Liveness_STATE_ALIVE,
			wantReason: kernelv1.Liveness_REASON_UNSPECIFIED,
		},
	}

	for _, tc := range cases {
		got := Evaluate(tc.obs)
		if got.State != tc.wantState {
			t.Errorf("%s: State = %v, want %v", tc.name, got.State, tc.wantState)
		}
		if got.Reason != tc.wantReason {
			t.Errorf("%s: Reason = %v, want %v", tc.name, got.Reason, tc.wantReason)
		}
	}
}

// TestRecordFoldsProbeResults drives Record across the whole transition ladder
// plus a flap, asserting the Evaluate verdict after each step. It exercises the
// "fail, fail, ok resets the counter" case the acceptance names, and confirms
// the instant comes from the argument (Record never reads a clock).
func TestRecordFoldsProbeResults(t *testing.T) {
	var obs Observation

	// Six failures in a row walk ALIVE-ish -> SUSPECT -> DEAD.
	wantStates := []kernelv1.Liveness_State{
		kernelv1.Liveness_STATE_ALIVE, // 1 failure
		kernelv1.Liveness_STATE_ALIVE, // 2 failures
		kernelv1.Liveness_STATE_SUSPECT,
		kernelv1.Liveness_STATE_SUSPECT,
		kernelv1.Liveness_STATE_SUSPECT,
		kernelv1.Liveness_STATE_DEAD,
	}
	for i, want := range wantStates {
		obs = Record(obs, false, time.Time{})
		if got := Evaluate(obs).State; got != want {
			t.Fatalf("after %d failures: State = %v, want %v", i+1, got, want)
		}
	}

	// A single success resets the counter to ALIVE and stamps LastSeen with the
	// supplied instant.
	obs = Record(obs, true, t0)
	if got := Evaluate(obs); got.State != kernelv1.Liveness_STATE_ALIVE {
		t.Fatalf("after recovery: State = %v, want ALIVE", got.State)
	}
	if obs.ConsecutiveFailures != 0 {
		t.Fatalf("recovery did not reset the counter: %d", obs.ConsecutiveFailures)
	}
	if !obs.LastSeen.Equal(t0) {
		t.Fatalf("LastSeen = %v, want the supplied instant %v", obs.LastSeen, t0)
	}
}

// TestRecordFlapResetsCounter is the exact acceptance sequence: fail x2 -> ok
// returns the peer to ALIVE with a zero counter.
func TestRecordFlapResetsCounter(t *testing.T) {
	var obs Observation
	obs = Record(obs, false, time.Time{})
	obs = Record(obs, false, time.Time{})
	if obs.ConsecutiveFailures != 2 {
		t.Fatalf("two failures should count 2, got %d", obs.ConsecutiveFailures)
	}
	obs = Record(obs, true, t0)
	if obs.ConsecutiveFailures != 0 {
		t.Fatalf("ok after a flap should reset the counter, got %d", obs.ConsecutiveFailures)
	}
	if got := Evaluate(obs).State; got != kernelv1.Liveness_STATE_ALIVE {
		t.Fatalf("flap-then-ok should be ALIVE, got %v", got)
	}
}

// TestEvaluateCarriesLastSeen confirms the verdict passes the observed instant
// through untouched — the caller stamps the wire timestamp, this package never
// fabricates one.
func TestEvaluateCarriesLastSeen(t *testing.T) {
	v := Evaluate(Observation{Probed: true, LastSeen: t0})
	if !v.LastSeen.Equal(t0) {
		t.Fatalf("Verdict.LastSeen = %v, want %v", v.LastSeen, t0)
	}
}

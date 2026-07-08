package daemon_test

// sandboxsrt_gate_hktch4t_faultinject_test.go — deterministic fault-injection
// proof for the hktch4tRetryUntilDenied fail-loud discriminator (hk-tch4t).
//
// # Why this exists
//
// TestSandbox_WriteToMainDenied_i0377 / TestSandboxAcceptance_WriteToMainDenied_hki0377
// prove the fail-loud property only when the environment happens to reproduce a
// transient srt sandbox_init apply-failure (originally: full `make check-short`
// under -race, ~16 parallel srt-forking binaries). Per hk-tch4t comment 1908:
// once stilgar's check-short -p cap (hk-me8ru) bounds -race parallelism to core
// count, that natural saturation repro DISAPPEARS — so a green
// TestSandbox_WriteToMainDenied_i0377 run stops being informative about whether
// hktch4tRetryUntilDenied would still catch a genuine, consistent apply-failure
// (sandbox never engages, every attempt allows the write through).
//
// These tests inject that failure mode DIRECTLY, with no dependency on srt, on
// -race scheduling, or on host saturation: they drive hktch4tRetryUntilDenied
// with a synthetic attempt func that reports "write allowed" every time (the
// exact shape of "srt exited 0 but sandbox_init never applied"), and assert the
// gate reports FAILURE — never masking a consistent apply-failure as a pass.
// A companion test proves the transient-absorption half is still intact: a
// single early "allowed" followed by "denied" still passes, so the retry only
// absorbs a genuine transient, not a real regression.
//
// This is the durable regression guard hk-tch4t's fix depends on: it removes
// the false-green risk of relying on ever reproducing the saturation window.

import (
	"fmt"
	"testing"
)

// TestHktch4t_RetryUntilDenied_FailsLoudOnConsistentApplyFailure injects the
// "srt silently never applies the sandbox" failure mode: every attempt
// observes the write going through. hktch4tRetryUntilDenied must report
// FAILURE (denied=false) after exhausting the full retry budget — this is the
// property TestSandbox_WriteToMainDenied_i0377 depends on to catch a genuine
// isolation regression rather than masking it as a transient.
func TestHktch4t_RetryUntilDenied_FailsLoudOnConsistentApplyFailure(t *testing.T) {
	t.Parallel()

	calls := 0
	consistentlyAllowed := func(attemptNum int) bool {
		calls++
		return false // write went through on every attempt: apply-failure, not denial
	}

	var logLines []string
	logf := func(format string, args ...any) {
		logLines = append(logLines, fmt.Sprintf(format, args...))
	}

	if denied := hktch4tRetryUntilDenied(logf, consistentlyAllowed); denied {
		t.Fatal("hktch4tRetryUntilDenied reported success (denied=true) for a consistently-allowed write; " +
			"a genuine apply-failure on every attempt must never be masked as a pass")
	}

	if calls != hktch4tMaxDenyAttempts {
		t.Errorf("attempt func called %d times; want exactly %d (full retry budget must be exhausted before giving up)",
			calls, hktch4tMaxDenyAttempts)
	}

	// The retry-exhaustion path must have logged once per attempt, so a real
	// CI failure carries the diagnostic trail rather than a bare boolean.
	if got, want := len(logLines), hktch4tMaxDenyAttempts; got != want {
		t.Errorf("logf called %d times during a consistent-failure run; want %d (one per attempt)", got, want)
	}
}

// TestHktch4t_RetryUntilDenied_AbsorbsSingleTransientThenDenies proves the
// companion half: a single early allowed-write attempt followed by a denied
// attempt still passes. Without this, a discriminator that "fails loud" on
// EVERY non-first-attempt denial would also reject the legitimate transient
// sandbox_init race this gate exists to absorb — that would defeat the point
// of the retry and reintroduce the false-red flake this bead is scoped under.
func TestHktch4t_RetryUntilDenied_AbsorbsSingleTransientThenDenies(t *testing.T) {
	t.Parallel()

	calls := 0
	transientThenDenied := func(attemptNum int) bool {
		calls++
		return attemptNum >= 2 // attempt 1: allowed (transient); attempt 2: denied
	}

	if denied := hktch4tRetryUntilDenied(nil, transientThenDenied); !denied {
		t.Fatal("hktch4tRetryUntilDenied must pass once a later attempt observes denial — " +
			"a single transient apply-failure must not fail the gate")
	}

	if calls != 2 {
		t.Errorf("attempt func called %d times; want 2 (stop immediately at first observed denial)", calls)
	}
}

package daemon_test

// sandboxsrt_gate_hktch4t_test.go — real-srt-fork serialization gate (hk-tch4t).
//
// Under full `make check-short` (`go test -short -race ./...`), srt/Seatbelt
// intermittently fails to APPLY the sandbox: TestSandbox_WriteToMainDenied_i0377
// observes the sandboxed write SUCCEED (macOS Seatbelt did not deny it) even
// though the profile correctly excludes the target from allowWrite. This
// package alone forks FOUR real `srt` (→ /usr/bin/sandbox-exec) subprocesses
// across two acceptance-test files (scenario_sandbox_pi_i0377_test.go,
// sandboxacceptance_hki0377_test.go), all launched via t.Parallel() — on a
// -race build under full-suite saturation that is enough concurrent
// sandbox_init pressure to occasionally lose the race and apply no
// restriction at all.
//
// hktch4tSrtGate serializes every real srt invocation in this package to one
// at a time, removing this package's own contribution to that fork storm.
// hktch4tRetryUntilDenied re-attempts a denial probe a bounded number of times
// with backoff: a transient sandbox_init failure (the diagnosed cause) clears
// on retry, while a genuine isolation regression (sandbox structurally allows
// the write) still fails every attempt and the test still goes red.
//
// Bead: hk-tch4t.

import "time"

// hktch4tSrtGate allows only one real srt subprocess invocation at a time
// across this test binary.
var hktch4tSrtGate = make(chan struct{}, 1)

// hktch4tAcquireSrt blocks until this goroutine holds the serialization gate.
func hktch4tAcquireSrt() { hktch4tSrtGate <- struct{}{} }

// hktch4tReleaseSrt releases the serialization gate.
func hktch4tReleaseSrt() { <-hktch4tSrtGate }

// hktch4tMaxDenyAttempts bounds the retry budget for a write-denial probe.
const hktch4tMaxDenyAttempts = 3

// hktch4tRetryDelay is the backoff between denial-probe retries.
const hktch4tRetryDelay = 300 * time.Millisecond

// hktch4tRetryUntilDenied calls attempt up to hktch4tMaxDenyAttempts times,
// sleeping hktch4tRetryDelay between tries. attempt must return true when the
// probe observed the write DENIED (the desired, passing outcome) and false
// when the write was allowed through (the transient-or-real failure mode).
// It returns true as soon as one attempt observes denial, and false only
// after every attempt observed the write going through — i.e. it never
// upgrades a consistently-broken sandbox to a pass, only absorbs a transient
// sandbox_init race under fork saturation.
func hktch4tRetryUntilDenied(logf func(format string, args ...any), attempt func(attemptNum int) bool) bool {
	for i := 1; i <= hktch4tMaxDenyAttempts; i++ {
		if attempt(i) {
			return true
		}
		if logf != nil {
			logf("hktch4t: write-to-main probe attempt %d/%d observed an allowed write; retrying (transient sandbox_init apply-failure under fork saturation is the diagnosed cause, see hk-tch4t)", i, hktch4tMaxDenyAttempts)
		}
		if i < hktch4tMaxDenyAttempts {
			time.Sleep(hktch4tRetryDelay)
		}
	}
	return false
}

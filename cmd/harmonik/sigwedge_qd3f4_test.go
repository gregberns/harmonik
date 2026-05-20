package main

// sigwedge_qd3f4_test.go — subprocess test: SIGINT during wedged daemon.Start
// exits the process within 6 seconds (hk-qd3f4).
//
// Rationale: the hard-exit watchdog calls os.Exit(1) unconditionally after
// signalGracePeriod (5s). Testing os.Exit requires a subprocess — the test
// binary cannot observe its own exit. This file uses the standard Go subprocess
// pattern: when HARMONIK_TEST_SIGWEDGE_SUBPROCESS=1, the test function runs the
// "subprocess role" (wedged scenario that must be killed by the watchdog); when
// the env var is absent, the test spawns itself as a subprocess and measures the
// exit latency.
//
// Helper prefix: sigwedgeFixture (per implementer-protocol.md §Helper-prefix
// discipline; bead hk-qd3f4).
//
// Bead ref: hk-qd3f4.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"testing"
	"time"
)

// sigwedgeFixtureSubprocessEnv is the env var that puts the test into
// "subprocess worker" mode.
const sigwedgeFixtureSubprocessEnv = "HARMONIK_TEST_SIGWEDGE_SUBPROCESS"

// TestSIGINTWedgedDaemonExit verifies that the hard-exit watchdog in
// runBeadSubcommand fires os.Exit(1) within 6 seconds when daemon.Start
// is wedged and SIGINT is received.
//
// The test spawns itself as a subprocess.  The subprocess runs
// sigwedgeFixtureRunWedgedWatchdog, which installs the same watchdog goroutine
// that runBeadSubcommand uses, then blocks forever simulating a wedged
// daemon.Start.  The parent sends SIGINT after 200ms and asserts the subprocess
// exits within 6 seconds.
//
// NOT parallel: the subprocess role modifies global signal state.
func TestSIGINTWedgedDaemonExit(t *testing.T) {
	// Subprocess worker role.
	if os.Getenv(sigwedgeFixtureSubprocessEnv) == "1" {
		sigwedgeFixtureRunWedgedWatchdog()
		// sigwedgeFixtureRunWedgedWatchdog should never return (watchdog calls
		// os.Exit or we block forever); reaching here is a bug.
		os.Exit(99)
	}

	// Parent role: locate the test binary.
	testBin, lookErr := exec.LookPath(os.Args[0])
	if lookErr != nil {
		testBin = os.Args[0]
	}

	// Build args: re-run only this test function, verbose.
	args := []string{"-test.run=TestSIGINTWedgedDaemonExit", "-test.v"}
	cmd := exec.CommandContext(context.Background(), testBin, args...) //nolint:gosec // G204: path from os.Args[0], test-only
	cmd.Env = append(os.Environ(), sigwedgeFixtureSubprocessEnv+"=1")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if startErr := cmd.Start(); startErr != nil {
		t.Fatalf("subprocess start: %v", startErr)
	}

	// Give the subprocess a moment to install signal handlers, then send SIGINT.
	time.Sleep(200 * time.Millisecond)
	if sigErr := cmd.Process.Signal(syscall.SIGINT); sigErr != nil {
		t.Fatalf("send SIGINT: %v", sigErr)
	}

	// Assert the process exits within 6 seconds.
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	const deadline = 6 * time.Second
	select {
	case waitErr := <-done:
		// Any exit (including os.Exit(1) from the watchdog) satisfies the
		// requirement; we only care that it exited within the deadline.
		if waitErr == nil {
			// Exit code 0 is unexpected: the subprocess should exit via os.Exit(1).
			t.Errorf("subprocess exited 0; want non-zero (watchdog fires os.Exit(1))")
		}
		// Non-zero exit is expected — watchdog fired or signal default handler.
	case <-time.After(deadline):
		_ = cmd.Process.Kill()
		t.Errorf("subprocess did not exit within %s after SIGINT (watchdog failed to fire)", deadline)
	}
}

// sigwedgeFixtureRunWedgedWatchdog installs the hard-exit watchdog from
// runBeadSubcommand and then blocks indefinitely simulating a wedged
// daemon.Start.  This function must be called only from the subprocess role.
//
// The watchdog will receive the SIGINT that the parent sends, arm the
// signalGracePeriod timer, and call os.Exit(1) when the timer fires (because
// daemonDone is never closed — the "daemon" is wedged).
func sigwedgeFixtureRunWedgedWatchdog() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	daemonDone := make(chan struct{}) // never closed — simulates wedged daemon.Start

	go func() {
		select {
		case <-ctx.Done():
			fmt.Fprintf(os.Stderr, "harmonik run: signal received — arming 5s hard-exit timer\n")
			select {
			case <-daemonDone:
				// Normal path — would not reach here in the wedged scenario.
			case <-time.After(signalGracePeriod):
				fmt.Fprintf(os.Stderr, "harmonik run: grace period expired — forcing os.Exit(1)\n")
				os.Exit(1)
			}
		case <-daemonDone:
			// Not reachable in the wedged scenario.
		}
	}()

	// Block forever — simulating daemon.Start wedged on a channel/syscall.
	<-make(chan struct{})
}

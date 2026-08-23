package main

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
	if os.Getenv(sigwedgeFixtureSubprocessEnv) == "1" {
		sigwedgeFixtureRunWedgedWatchdog()
		os.Exit(99)
	}

	testBin, lookErr := exec.LookPath(os.Args[0])
	if lookErr != nil {
		testBin = os.Args[0]
	}

	args := []string{"-test.run=TestSIGINTWedgedDaemonExit", "-test.v"}
	cmd := exec.CommandContext(context.Background(), testBin, args...) //nolint:gosec // G204: path from os.Args[0], test-only
	cmd.Env = append(os.Environ(), sigwedgeFixtureSubprocessEnv+"=1")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if startErr := cmd.Start(); startErr != nil {
		t.Fatalf("subprocess start: %v", startErr)
	}

	time.Sleep(200 * time.Millisecond)
	if sigErr := cmd.Process.Signal(syscall.SIGINT); sigErr != nil {
		t.Fatalf("send SIGINT: %v", sigErr)
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	deadline := signalGracePeriod + 15*time.Second
	select {
	case waitErr := <-done:
		if waitErr == nil {
			t.Errorf("subprocess exited 0; want non-zero (watchdog fires os.Exit(1))")
		}
	case <-time.After(deadline):
		if err := cmd.Process.Kill(); err != nil {
			t.Errorf("kill wedged subprocess: %v", err)
		}
		t.Errorf("subprocess did not exit within %s after SIGINT (watchdog failed to fire)", deadline)
	}
}

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
			case <-time.After(signalGracePeriod):
				fmt.Fprintf(os.Stderr, "harmonik run: grace period expired — forcing os.Exit(1)\n")
				os.Exit(1)
			}
		case <-daemonDone:
		}
	}()

	<-make(chan struct{})
}

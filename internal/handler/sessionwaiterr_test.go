package handler_test

// sessionwaiterr_test.go — regression tests for Session.Wait's exit-error
// identity.
//
// Helper prefix: sessionWaitErrFixture (per implementer-protocol.md
// §Helper-prefix discipline).
//
// lifecycle.WaitOwner used to broadcast the exit error over a buffered(1)
// channel that was written once and then closed — so it yielded the error to
// its FIRST reader only; every later read observed the closed-channel zero
// value (nil). Two production paths hit that:
//
//   - Any caller that calls sess.Wait twice, or two goroutines that both wait
//     on the same session: exactly one sees the *exec.ExitError, the other sees
//     a clean nil and concludes the run succeeded.
//   - Session.Kill spawned a reap-observer goroutine that called
//     waitOwner.Wait() purely for its "process reaped" edge and discarded the
//     value. Whenever that goroutine won the race it consumed the only delivery,
//     so a post-Kill sess.Wait returned nil for a subprocess that had in fact
//     exited non-zero or been signalled.
//
// Fixed at the root in hk-qun49: WaitOwner now stores the exit error and Wait
// re-delivers it to every caller, and Kill selects on WaitOwner.Done() — an
// edge that broadcasts and consumes nothing — instead of draining the result.
//
// Two of the three tests below (IsStableAcrossCalls, SurvivesConcurrentWaiters)
// fail if Wait goes back to delivering once; SurvivesKill does not, and says so
// in its own doc comment.

import (
	"bufio"
	"context"
	"errors"
	"os/exec"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/handler"
)

// sessionWaitErrFixtureAwaitReady blocks until the child has written its first
// line to stdout, proving the shell reached the command after `trap` and so has
// the handler installed.
func sessionWaitErrFixtureAwaitReady(t *testing.T, sess handler.Session) {
	t.Helper()
	line := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(sess.Stdout())
		if scanner.Scan() {
			line <- scanner.Text()
			return
		}
		close(line)
	}()
	select {
	case got, ok := <-line:
		if !ok {
			t.Fatal("child stdout reached EOF before the readiness line")
		}
		if got != "ready" {
			t.Fatalf("child readiness line = %q, want \"ready\"", got)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for the child readiness line")
	}
}

// TestSession_Wait_ExitErrorIsStableAcrossCalls verifies that every Wait call on
// a session whose subprocess exited non-zero reports that failure — not just the
// first caller to read WaitOwner's memoized exit error.
func TestSession_Wait_ExitErrorIsStableAcrossCalls(t *testing.T) {
	t.Parallel()

	// Child exits 3 immediately.
	cmd := sessionFixtureCmd(t, "exit 3")

	sess, err := handler.NewSession(t.Context(), cmd)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	first := sess.Wait(t.Context())
	second := sess.Wait(t.Context())
	third := sess.Wait(t.Context())

	for i, waitErr := range []error{first, second, third} {
		var exitErr *exec.ExitError
		if !errors.As(waitErr, &exitErr) {
			t.Fatalf("Wait call %d: want *exec.ExitError, got %v", i+1, waitErr)
		}
		if got := exitErr.ExitCode(); got != 3 {
			t.Errorf("Wait call %d: exit code = %d, want 3", i+1, got)
		}
	}
}

// TestSession_Wait_ExitErrorSurvivesConcurrentWaiters verifies that concurrent
// waiters all observe the exit error. Under the old implementation exactly one
// of them received it and the rest silently saw nil.
func TestSession_Wait_ExitErrorSurvivesConcurrentWaiters(t *testing.T) {
	t.Parallel()

	cmd := sessionFixtureCmd(t, "exit 7")

	sess, err := handler.NewSession(t.Context(), cmd)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	const waiters = 5
	var wg sync.WaitGroup
	errs := make([]error, waiters)
	for i := range waiters {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = sess.Wait(t.Context())
		}()
	}
	wg.Wait()

	for i, waitErr := range errs {
		var exitErr *exec.ExitError
		if !errors.As(waitErr, &exitErr) {
			t.Errorf("waiter %d: want *exec.ExitError, got %v", i, waitErr)
			continue
		}
		if got := exitErr.ExitCode(); got != 7 {
			t.Errorf("waiter %d: exit code = %d, want 7", i, got)
		}
	}
}

// TestSession_Wait_ExitErrorSurvivesKill verifies that a Wait following a Kill
// still reports the subprocess failure. Kill originally spawned a reap-observer
// goroutine that drained the WaitOwner result channel, so this Wait returned nil
// whenever that goroutine got there first; Kill now selects on WaitOwner.Done().
//
// Unlike the other two tests in this file, this one no longer pins the caching:
// because Kill consumes nothing, a hypothetical single-delivery Wait still
// leaves this Wait as the FIRST reader and the test passes (measured with a
// go test -overlay that made Wait deliver once). It is kept as defence in depth
// against Kill regrowing a consuming observer, not as a caching regression
// guard — TestSession_Wait_ExitErrorIsStableAcrossCalls and
// ...SurvivesConcurrentWaiters are the two that fail without the root fix.
func TestSession_Wait_ExitErrorSurvivesKill(t *testing.T) {
	t.Parallel()

	// Child exits 5 on SIGTERM so the post-Kill exit is a non-zero status rather
	// than a signal kill, making the lost-error case unambiguous. Two shell
	// details are load-bearing:
	//
	//   - The sleep is backgrounded and joined with `wait`, because a POSIX shell
	//     only runs a trap between commands — blocked in a FOREGROUND sleep it
	//     takes the signal's default disposition and dies without ever reaching
	//     the handler.
	//   - It echoes "ready" only AFTER installing the trap, and the test blocks
	//     on that line before signalling. Without the handshake the SIGTERM can
	//     beat the freshly-forked shell to the `trap` builtin, and the child dies
	//     signalled (exit code -1) instead of exiting 5.
	cmd := sessionFixtureCmd(t, "trap 'exit 5' TERM; echo ready; sleep 30 & wait")

	sess, err := handler.NewSession(t.Context(), cmd)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	sessionWaitErrFixtureAwaitReady(t, sess)

	killCtx, killCancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer killCancel()
	if killErr := sess.Kill(killCtx); killErr != nil {
		t.Fatalf("Kill: %v", killErr)
	}

	waitErr := sess.Wait(t.Context())
	var exitErr *exec.ExitError
	if !errors.As(waitErr, &exitErr) {
		t.Fatalf("Wait after Kill: want *exec.ExitError, got %v", waitErr)
	}
	if got := exitErr.ExitCode(); got != 5 {
		t.Errorf("Wait after Kill: exit code = %d, want 5", got)
	}
}

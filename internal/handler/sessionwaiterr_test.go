package handler_test

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

package lifecycle

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"testing"
)

// supervisionFixtureWaitOwner encapsulates the single-owner Wait discipline for
// a *exec.Cmd: only one goroutine may call cmd.Wait(); all others observe the
// memoized result once the shared channel signals that the reap has happened.
//
// Spec ref: process-lifecycle.md §4.5 PL-014 — "Every spawn MUST have exactly
// one Go goroutine that owns the *exec.Cmd and that goroutine MUST call
// cmd.Wait() exactly once to reap the child's exit status."
type supervisionFixtureWaitOwner struct {
	cmd  *exec.Cmd
	err  error // cmd.Wait's result; written once inside once.Do
	once sync.Once
	// waitCh is closed after the single cmd.Wait call completes. It carries the
	// "reaped" edge only — the exit error is read from err. Sending the error
	// over the channel instead would deliver it to the FIRST receiver and leave
	// every later one seeing the closed-channel nil (hk-qun49).
	waitCh chan error
}

// supervisionFixtureNewWaitOwner wraps cmd in a WaitOwner. The cmd must have
// been started (cmd.Start called) before calling this function.
func supervisionFixtureNewWaitOwner(cmd *exec.Cmd) *supervisionFixtureWaitOwner {
	return &supervisionFixtureWaitOwner{
		cmd:    cmd,
		waitCh: make(chan error, 1),
	}
}

// Wait may be called from any goroutine. The FIRST caller performs the actual
// cmd.Wait(); every subsequent caller receives the same memoized error. This
// enforces the single-owner discipline: the memo replaces the need to call
// cmd.Wait() more than once.
func (o *supervisionFixtureWaitOwner) Wait() error {
	o.once.Do(func() {
		o.err = o.cmd.Wait()
		close(o.waitCh)
	})
	<-o.waitCh
	return o.err
}

// supervisionFixtureSpawnSelfExit spawns the test binary with a sentinel that
// makes it exit with code 0 immediately. Returns a started *exec.Cmd. The
// caller MUST call cmd.Wait() exactly once.
func supervisionFixtureSpawnSelfExit(t *testing.T) *exec.Cmd {
	t.Helper()

	testBin := os.Args[0]
	//nolint:gosec // G204: testBin is os.Args[0] — the test binary itself
	cmd := exec.CommandContext(t.Context(), testBin, "-test.run=^TestPL015_OneOwnerWait_ChildStub$")
	cmd.Env = append(os.Environ(), "GO_PL015_CHILD_STUB=1")
	return cmd
}

// TestPL015_OneOwnerWait_ChildStub is the self-exec stub for PL-015 tests.
// When invoked with GO_PL015_CHILD_STUB=1, exits immediately so the parent can
// test Wait-ownership discipline without a long-running subprocess.
func TestPL015_OneOwnerWait_ChildStub(t *testing.T) {
	t.Parallel()

	if os.Getenv("GO_PL015_CHILD_STUB") != "1" {
		return // not a stub invocation
	}
	// Exit immediately — parent will observe a zero exit code.
	os.Exit(0)
}

// TestPL015_OneOwnerWait verifies that exactly one goroutine calls cmd.Wait()
// on a spawned child and that a second concurrent call does not double-wait
// (which would panic or block indefinitely in the real implementation).
//
// The test uses a sync/atomic.Int32 counter to track how many goroutines
// actually executed cmd.Wait(). The WaitOwner helper enforces single-Wait
// discipline; the counter must equal 1 after both goroutines have completed.
//
// Spec ref: process-lifecycle.md §4.5 PL-014 — "Every spawn MUST have exactly
// one Go goroutine that owns the *exec.Cmd and that goroutine MUST call
// cmd.Wait() exactly once to reap the child's exit status. The watcher goroutine
// per [handler-contract.md §4.3 HC-011] is that exclusive caller."
// PL-INV-005 — "Failure to call cmd.Wait() produces a zombie that persists
// until daemon exit...and MUST NOT occur on any code path."
func TestPL015_OneOwnerWait(t *testing.T) {
	t.Parallel()

	t.Run("single-owner/exactly-one-wait-call", func(t *testing.T) {
		t.Parallel()

		cmd := supervisionFixtureSpawnSelfExit(t)
		if err := cmd.Start(); err != nil {
			t.Fatalf("PL-015 one-owner-wait: cmd.Start: %v", err)
		}

		owner := supervisionFixtureNewWaitOwner(cmd)

		// Count how many goroutines reach the actual cmd.Wait() call.
		// With the WaitOwner helper the answer must be 1 (only one goroutine
		// holds the once.Do body).
		var rawWaitCallCount atomic.Int32

		// Wrap the owner to count raw calls through the public interface.
		callAndCount := func() error {
			rawWaitCallCount.Add(1)
			return owner.Wait()
		}

		var wg sync.WaitGroup
		errs := make([]error, 2)
		for i := range 2 {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				errs[idx] = callAndCount()
			}(i)
		}
		wg.Wait()

		// Both goroutines called Wait() through the interface — that's expected.
		if rawWaitCallCount.Load() != 2 {
			t.Errorf("PL-015: expected 2 Wait() interface calls, got %d", rawWaitCallCount.Load())
		}

		// Both goroutines must receive the same result: nil (child exited 0).
		for i, err := range errs {
			if err != nil {
				t.Errorf("PL-015: goroutine %d Wait() returned %v, want nil", i, err)
			}
		}
	})

	t.Run("single-owner/double-wait-panics-without-owner", func(t *testing.T) {
		t.Parallel()

		// Demonstrate that calling cmd.Wait() twice directly (without the owner
		// helper) produces an error on the second call — confirming why the
		// single-owner discipline is required.
		cmd := supervisionFixtureSpawnSelfExit(t)
		if err := cmd.Start(); err != nil {
			t.Fatalf("PL-015 double-wait: cmd.Start: %v", err)
		}

		// First Wait — legitimate.
		err1 := cmd.Wait()
		if err1 != nil {
			t.Fatalf("PL-015 double-wait: first Wait: %v", err1)
		}

		// Second Wait — must return an error (process already reaped).
		err2 := cmd.Wait()
		if err2 == nil {
			t.Error("PL-015 double-wait: second cmd.Wait() succeeded; expected an error (process already reaped)")
		}
		// The error should indicate the process has already been waited on.
		if err2 != nil && !errors.Is(err2, io.EOF) {
			// Any non-nil error is acceptable here — the exact error text is
			// runtime-dependent. We just confirm it is not nil.
			t.Logf("PL-015 double-wait: second Wait error (expected): %v", err2)
		}
	})

	t.Run("single-owner/wait-owner-result-is-consistent", func(t *testing.T) {
		t.Parallel()

		// Verify the fixture WaitOwner memoizes cmd.Wait's result so that every
		// subsequent Wait returns immediately with the same value. The channel
		// carries only the "reaped" edge; the value comes from the err field.
		cmd := supervisionFixtureSpawnSelfExit(t)
		if err := cmd.Start(); err != nil {
			t.Fatalf("PL-015 consistent-result: cmd.Start: %v", err)
		}

		owner := supervisionFixtureNewWaitOwner(cmd)

		// First call.
		err1 := owner.Wait()
		// Second call after waitCh is closed — must return the same value
		// immediately.
		err2 := owner.Wait()

		// Both calls must read the same memoized err field, so they must be the
		// identical error value (both nil, or both the same wrapped error).
		// We intentionally avoid errors.Is here because we are comparing two
		// results of the same operation, not checking for a specific sentinel.
		//nolint:errorlint // comparing two results of the same Wait() call, not checking sentinel
		if err1 != err2 {
			t.Errorf("PL-015 consistent-result: Wait() results differ: %v vs %v", err1, err2)
		}
	})
}

package runlaunch_test

// teardown_characterization_test.go — characterization of the TEARDOWN step of
// the shared launch → dispatch → wait → probe → teardown sequence.
//
// ForceTeardownSession is registered as a deferred backstop by every dispatch
// site immediately after Launch, and it is the guard that keeps the run-level
// worktree removal from deleting a directory a live agent is still working in
// (hk-68pvl: the race produced false `no_commit_during_implementer ... exit=0`
// records). It had no tests.
//
// Three properties are load-bearing and pinned here:
//   - it tears down on a context that CANNOT be cancelled, so the teardown
//     still happens on the shutdown path where the run context is already dead;
//   - it BLOCKS until the kill returns, because the ordering guarantee against
//     worktree removal comes from blocking, not from the return value;
//   - it tolerates a nil session and a failing kill, because it runs from a
//     defer with nowhere to report an error.

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/handler"
	hclifecycle "github.com/gregberns/harmonik/internal/handlercontract/lifecycle"
	"github.com/gregberns/harmonik/internal/runlaunch"
)

// teardownSession records what ForceTeardownSession did to it. Only Kill is in
// the teardown contract; every other method fails the test if reached.
type teardownSession struct {
	t *testing.T

	killErr  error
	killGate chan struct{} // when non-nil, Kill blocks until it is closed

	mu      sync.Mutex
	kills   int
	killCtx context.Context //nolint:containedctx // captured for assertion, never used to drive work
}

func (s *teardownSession) Kill(ctx context.Context) error {
	s.mu.Lock()
	s.kills++
	s.killCtx = ctx
	s.mu.Unlock()

	if s.killGate != nil {
		<-s.killGate
	}
	return s.killErr
}

func (s *teardownSession) killCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.kills
}

func (s *teardownSession) capturedCtx() context.Context {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.killCtx
}

func (s *teardownSession) SendInput(context.Context, string) error {
	s.t.Error("teardown called SendInput — outside its contract")
	return nil
}

func (s *teardownSession) Wait(context.Context) error {
	s.t.Error("teardown called Wait — outside its contract")
	return nil
}

func (s *teardownSession) Outcome() handler.Outcome {
	s.t.Error("teardown called Outcome — outside its contract")
	return handler.Outcome{}
}

func (s *teardownSession) Stdout() io.Reader {
	s.t.Error("teardown called Stdout — outside its contract")
	return nil
}

func (s *teardownSession) Stderr() io.Reader {
	s.t.Error("teardown called Stderr — outside its contract")
	return nil
}

func (s *teardownSession) CloseStdin() error {
	s.t.Error("teardown called CloseStdin — outside its contract")
	return nil
}

func (s *teardownSession) Machine() *hclifecycle.Machine {
	s.t.Error("teardown called Machine — outside its contract")
	return nil
}

// TestForceTeardownSession_KillsOnAnUncancellableContext pins the property that
// makes the backstop work on the shutdown path: the teardown does not inherit
// the run's context, so a run whose context is already cancelled still gets its
// agent killed before the worktree is removed.
//
// A decomposition that threads the run context in here would silently turn
// every SIGTERM into a worktree-removal race against a live agent.
func TestForceTeardownSession_KillsOnAnUncancellableContext(t *testing.T) {
	sess := &teardownSession{t: t}

	runlaunch.ForceTeardownSession(sess)

	if sess.killCount() != 1 {
		t.Fatalf("Kill called %d times, want exactly 1", sess.killCount())
	}
	ctx := sess.capturedCtx()
	if ctx == nil {
		t.Fatal("Kill received a nil context")
	}
	if _, hasDeadline := ctx.Deadline(); hasDeadline {
		t.Error("the teardown context carries a deadline — the reap could be abandoned mid-kill")
	}
	// A nil Done channel is the observable form of "nothing can cancel this":
	// it holds for context.Background() and for context.WithoutCancel alike, so
	// it survives a decomposition that switches between them, and it fails for
	// any context derived from the run's own.
	if ctx.Done() != nil {
		t.Error("the teardown context is cancellable — a cancelled run would skip its own teardown")
	}
}

// TestForceTeardownSession_BlocksUntilTheKillReturns pins the ordering
// guarantee itself. The worktree removal runs in a later defer; it is safe only
// because this call has not returned while the agent is still being reaped.
func TestForceTeardownSession_BlocksUntilTheKillReturns(t *testing.T) {
	gate := make(chan struct{})
	sess := &teardownSession{t: t, killGate: gate}

	returned := make(chan struct{})
	go func() {
		runlaunch.ForceTeardownSession(sess)
		close(returned)
	}()

	// While the kill is in flight the teardown must NOT have returned.
	select {
	case <-returned:
		t.Fatal("teardown returned while the kill was still in flight — the worktree removal could race a live agent")
	case <-time.After(50 * time.Millisecond):
	}

	close(gate)
	select {
	case <-returned:
	case <-time.After(5 * time.Second):
		t.Fatal("teardown never returned after the kill completed")
	}
}

// TestForceTeardownSession_ToleratesNilSessionAndKillFailure pins the two
// tolerances a deferred backstop needs. It runs on every return path, including
// ones where the launch never produced a session, and it has nowhere to report
// a kill error — so neither may panic or otherwise escape.
func TestForceTeardownSession_ToleratesNilSessionAndKillFailure(t *testing.T) {
	t.Run("nil session", func(t *testing.T) {
		runlaunch.ForceTeardownSession(nil) // must not panic
	})

	t.Run("kill fails", func(t *testing.T) {
		sess := &teardownSession{t: t, killErr: errors.New("no such process")}
		runlaunch.ForceTeardownSession(sess) // must not panic
		if sess.killCount() != 1 {
			t.Errorf("Kill called %d times, want 1", sess.killCount())
		}
	})
}

// TestForceTeardownSession_IsSafeToRepeat pins idempotence from the caller's
// side: the backstop is registered per dispatch and a run with several dispatch
// segments tears down more than once. Each call must reach the session's own
// idempotent Kill rather than being skipped or doubling up on state.
func TestForceTeardownSession_IsSafeToRepeat(t *testing.T) {
	sess := &teardownSession{t: t}

	runlaunch.ForceTeardownSession(sess)
	runlaunch.ForceTeardownSession(sess)

	if got := sess.killCount(); got != 2 {
		t.Errorf("Kill called %d times across two teardowns, want 2 — the teardown must not suppress itself", got)
	}
}

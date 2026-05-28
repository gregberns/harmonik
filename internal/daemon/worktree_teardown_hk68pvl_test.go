package daemon_test

// worktree_teardown_hk68pvl_test.go — regression test for hk-68pvl.
//
// Bug: in a review-loop harmonik run, the daemon could call
// `git worktree remove --force` on an implementer's worktree WHILE the
// implementer claude session was still live (mid `go test`), because on the
// tmux substrate path tmuxSubstrateSession.Wait returns ctx.Err() the instant
// the run ctx is cancelled even though the hosted process is still alive. The
// agent's `git add`/commit then landed in a deleted directory and the run was
// recorded as a false `no_commit_during_implementer ... exit=0`.
//
// Fix (load-bearing): forceTeardownSession force-kills the session and blocks
// until the hosted process is reaped, and the drivers register it as a deferred
// backstop so it always runs before the beadRunOne-level wtCleanup removes the
// worktree.
//
// This test asserts the ordering invariant on the seam the fix introduced:
// when the worktree removal is sequenced after forceTeardownSession (as the
// drivers now arrange via defer LIFO), the removal NEVER runs while the session
// still reports a live process — even when the run ctx is already cancelled
// (the production trigger).

import (
	"context"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/handler"
)

// hk68pvlRacySession models the production substrate session for this race:
//
//   - alive starts true (the hosted claude is running, e.g. mid `go test`).
//   - Wait returns ctx.Err() immediately when ctx is cancelled WITHOUT making
//     the process dead — exactly the tmuxSubstrateSession.Wait early-return that
//     caused the bug.
//   - Kill blocks for a short, observable interval (simulating
//     killProcessWithGrace) and only then marks the process dead. This is the
//     synchronous teardown the fix relies on.
type hk68pvlRacySession struct {
	alive     atomic.Bool
	killCalls atomic.Int32
	killDelay time.Duration
}

func newHk68pvlRacySession(killDelay time.Duration) *hk68pvlRacySession {
	s := &hk68pvlRacySession{killDelay: killDelay}
	s.alive.Store(true)
	return s
}

func (s *hk68pvlRacySession) IsAlive() bool { return s.alive.Load() }

func (s *hk68pvlRacySession) SendInput(_ context.Context, _ string) error { return nil }

// Kill blocks until the (simulated) process group is reaped, then marks the
// session dead. Idempotent: subsequent calls are no-ops once dead.
func (s *hk68pvlRacySession) Kill(_ context.Context) error {
	s.killCalls.Add(1)
	if !s.alive.Load() {
		return nil
	}
	// Simulate killProcessWithGrace's synchronous SIGTERM→grace→SIGKILL window.
	time.Sleep(s.killDelay)
	s.alive.Store(false)
	return nil
}

// Wait mimics tmuxSubstrateSession.Wait: returns the instant ctx is cancelled
// while leaving the process alive (the racy early return). With a live ctx it
// blocks until the process is dead.
func (s *hk68pvlRacySession) Wait(ctx context.Context) error {
	for {
		if !s.alive.Load() {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Millisecond):
		}
	}
}

func (s *hk68pvlRacySession) Outcome() handler.Outcome { return handler.Outcome{} }
func (s *hk68pvlRacySession) Stdout() io.Reader        { return nil }
func (s *hk68pvlRacySession) Stderr() io.Reader        { return nil }
func (s *hk68pvlRacySession) CloseStdin() error        { return nil }

var _ handler.Session = (*hk68pvlRacySession)(nil)

// TestForceTeardownSession_RemovalNeverRacesLiveSession asserts the core fix
// invariant: when the worktree removal is sequenced after forceTeardownSession
// (the LIFO-defer arrangement the drivers now use), the removal observes the
// session already dead — even though the run ctx was cancelled, which is what
// waitWithSocketGrace's sess.Wait(ctx) would have seen on the substrate path.
func TestForceTeardownSession_RemovalNeverRacesLiveSession(t *testing.T) {
	sess := newHk68pvlRacySession(40 * time.Millisecond)

	// Run ctx is already cancelled — the production trigger. A bare sess.Wait
	// here returns immediately while the process is still alive.
	runCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sess.Wait(runCtx); err == nil {
		t.Fatal("precondition: Wait should return ctx.Err() while the session is still alive")
	}
	if !sess.IsAlive() {
		t.Fatal("precondition: cancelled Wait must NOT have killed the live session")
	}

	// Model the driver+beadRunOne arrangement: teardown runs, THEN removal.
	var aliveAtRemoval bool
	removeWorktree := func() { aliveAtRemoval = sess.IsAlive() }

	daemon.ExportedForceTeardownSession(sess) // deferred backstop in production
	removeWorktree()                          // deferred wtCleanup in production

	if aliveAtRemoval {
		t.Fatal("worktree removal ran while the session was still live — hk-68pvl race not closed")
	}
	if sess.killCalls.Load() == 0 {
		t.Fatal("forceTeardownSession did not Kill the session")
	}
}

// TestForceTeardownSession_NilIsNoop guards the nil-session path so the deferred
// backstop is safe to register before a session is confirmed launched.
func TestForceTeardownSession_NilIsNoop(t *testing.T) {
	// Must not panic.
	daemon.ExportedForceTeardownSession(nil)
}

// TestForceTeardownSession_Idempotent confirms the backstop is safe to call
// more than once (the drivers may Kill in the happy path and again via defer).
func TestForceTeardownSession_Idempotent(t *testing.T) {
	sess := newHk68pvlRacySession(0)
	daemon.ExportedForceTeardownSession(sess)
	daemon.ExportedForceTeardownSession(sess)
	if sess.IsAlive() {
		t.Fatal("session should be dead after teardown")
	}
	if got := sess.killCalls.Load(); got < 2 {
		t.Fatalf("expected Kill called on each invocation, got %d", got)
	}
}

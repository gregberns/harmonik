package runlease

import (
	"errors"
	"sync"
	"testing"
	"time"
)

// counter records how many times a give-back call was made, so a test can say
// "exactly once" rather than "at least once".
type counter struct {
	mu    sync.Mutex
	calls int
	err   error
}

func (c *counter) release() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	return c.err
}

func (c *counter) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

func TestALeaseGivesItsResourceBackExactlyOnce(t *testing.T) {
	t.Parallel()

	var c counter
	l := Hold(Worktree, c.release)

	for i := range 5 {
		if err := l.Release(); err != nil {
			t.Fatalf("Release call %d: %v", i, err)
		}
	}
	if got := c.count(); got != 1 {
		t.Errorf("five Release calls made %d give-back calls, want 1", got)
	}
}

func TestConcurrentReleasesStillGiveTheResourceBackOnce(t *testing.T) {
	t.Parallel()

	// The release sites for a session are spread across goroutines: a watcher,
	// a deferred cleanup, and the run's own tail can all reach one lease.
	var c counter
	l := Hold(AgentSession, c.release)

	var wg sync.WaitGroup
	for range 64 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := l.Release(); err != nil {
				t.Errorf("concurrent Release: %v", err)
			}
		}()
	}
	wg.Wait()

	if got := c.count(); got != 1 {
		t.Errorf("64 concurrent Release calls made %d give-back calls, want 1", got)
	}
}

func TestAFailedReleaseIsSpentAndIsNotRetried(t *testing.T) {
	t.Parallel()

	// Retrying a give-back call that cannot succeed is how one stuck resource
	// becomes a loop. The error reaches the caller once and the lease is done.
	wantErr := errors.New("worktree busy")
	c := counter{err: wantErr}
	l := Hold(Worktree, c.release)

	if err := l.Release(); !errors.Is(err, wantErr) {
		t.Fatalf("first Release returned %v, want %v", err, wantErr)
	}
	if err := l.Release(); err != nil {
		t.Errorf("second Release returned %v, want nil — the lease was already spent", err)
	}
	if got := c.count(); got != 1 {
		t.Errorf("a failed release was retried %d times, want 1 call total", got)
	}
	if !l.spent() {
		t.Error("a lease whose release failed reports itself unspent")
	}
}

func TestALeaseWithNothingToCallIsBornSpent(t *testing.T) {
	t.Parallel()

	// A caller with an optional cleanup passes nil rather than branching at
	// every release site.
	l := Hold(Worktree, nil)

	if !l.spent() {
		t.Error("a lease with no give-back call reports itself unspent")
	}
	if err := l.Release(); err != nil {
		t.Errorf("Release on a lease with nothing to call returned %v, want nil", err)
	}
	if l.Resource() != Worktree {
		t.Errorf("Resource() = %s, want worktree — a spent lease still names what it held", l.Resource())
	}
}

func TestALeaseNamesWhatItHolds(t *testing.T) {
	t.Parallel()

	for _, r := range allResources {
		if got := Hold(r, nil).Resource(); got != r {
			t.Errorf("Hold(%s).Resource() = %s", r, got)
		}
	}
}

func TestAnUnreleasedLeaseIsNotYetSpent(t *testing.T) {
	t.Parallel()

	var c counter
	l := Hold(TunnelProcess, c.release)

	if l.spent() {
		t.Fatal("a lease reports itself spent before anything released it")
	}
	if got := c.count(); got != 0 {
		t.Errorf("Hold made %d give-back calls; it must make none", got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Give — the early give-back that reads the run's answer
// ─────────────────────────────────────────────────────────────────────────────

func TestGiveHandsBackAResourceTheDispositionReleases(t *testing.T) {
	t.Parallel()

	// The hook session is given back at the end of an ordinary launch, before
	// the scope holding it closes.
	var c counter
	l := Hold(HookSession, c.release)

	rep := l.Give(Reclaim)
	if got := c.count(); got != 1 {
		t.Errorf("Give(reclaim) made %d give-back calls, want 1", got)
	}
	if len(rep.Released) != 1 || rep.Released[0] != HookSession {
		t.Errorf("Give(reclaim) reported Released=%v, want [hook-session]", rep.Released)
	}
	if len(rep.Kept) != 0 {
		t.Errorf("Give(reclaim) reported Kept=%v, want none", rep.Kept)
	}
}

func TestGiveKeepsAResourceTheDispositionKeepsAndDisarmsIt(t *testing.T) {
	t.Parallel()

	// This is the whole reason Give exists. Release would hand the hook session
	// back here, and the surviving agent would keep a session it can no longer
	// report through.
	var c counter
	l := Hold(HookSession, c.release)

	rep := l.Give(Survive)
	if got := c.count(); got != 0 {
		t.Errorf("Give(survive) made %d give-back calls on the hook session, want 0", got)
	}
	if len(rep.Kept) != 1 || rep.Kept[0] != HookSession {
		t.Errorf("Give(survive) reported Kept=%v, want [hook-session]", rep.Kept)
	}
	if !l.spent() {
		t.Error("a kept lease is left armed. A later caller could still give back what the run decided to leave standing")
	}
	// Disarmed means disarmed: the scope's own close finds nothing to do.
	if rep2 := l.Give(Reclaim); len(rep2.Released) != 0 || len(rep2.Kept) != 0 {
		t.Errorf("a second Give reported %+v, want an empty report", rep2)
	}
	if got := c.count(); got != 0 {
		t.Errorf("a later reclaim gave back a kept resource (%d calls), want 0", got)
	}
}

func TestGiveActsAtMostOnceAcrossRepeats(t *testing.T) {
	t.Parallel()

	var c counter
	l := Hold(HookSession, c.release)

	first := l.Give(Reclaim)
	second := l.Give(Reclaim)
	if got := c.count(); got != 1 {
		t.Errorf("two Give calls made %d give-back calls, want 1", got)
	}
	if len(first.Released) != 1 {
		t.Errorf("the first Give reported Released=%v, want one resource", first.Released)
	}
	if len(second.Released) != 0 {
		t.Errorf("the second Give reported Released=%v, want none — the lease was already spent", second.Released)
	}
}

func TestGiveReportsAFailedGiveBackAndSpendsTheLease(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("hook store closed")
	c := counter{err: wantErr}
	l := Hold(HookSession, c.release)

	rep := l.Give(Reclaim)
	if !errors.Is(rep.Err(), wantErr) {
		t.Errorf("Give reported %v, want %v", rep.Err(), wantErr)
	}
	if len(rep.Failures) != 1 || rep.Failures[0].Resource != HookSession {
		t.Errorf("Give reported Failures=%v, want one on the hook session", rep.Failures)
	}
	if got := c.count(); got != 1 {
		t.Errorf("a failed give-back was retried (%d calls), want 1", got)
	}
}

func TestGiveOnALeaseWithNothingToCallReportsNothing(t *testing.T) {
	t.Parallel()

	// A launch with no hook store holds a lease born spent, so the give-back
	// site needs no test for whether there is anything to give.
	l := Hold(HookSession, nil)

	for _, d := range []Disposition{Reclaim, Survive, RetainEvidence} {
		rep := l.Give(d)
		if len(rep.Released) != 0 || len(rep.Kept) != 0 || len(rep.Failures) != 0 {
			t.Errorf("Give(%s) on a lease with nothing to call reported %+v, want an empty report", d, rep)
		}
	}
}

func TestAScopeCloseFindsNothingLeftAfterAnEarlyGive(t *testing.T) {
	t.Parallel()

	// The shape the launch uses: the hook session is held by the scope AND given
	// back early. The close must not make a second call.
	var c counter
	var s Scope
	l := s.Hold(HookSession, c.release)

	l.Give(Reclaim)
	rep := s.Close(Reclaim)

	if got := c.count(); got != 1 {
		t.Errorf("an early Give plus a scope close made %d give-back calls, want 1", got)
	}
	if len(rep.Released) != 0 {
		t.Errorf("the close reported Released=%v, want none — the lease was already spent", rep.Released)
	}
}

func TestAGiveBackCallReachesTheWorldOutsideTheLeaseLock(t *testing.T) {
	t.Parallel()

	// Same property as the scope's, at the lease. A release that re-enters its
	// own lease must find it already spent and return, not block on the lock
	// its own caller is holding.
	var l *Lease
	var again error
	l = Hold(AgentSession, func() error {
		again = l.Release()
		return nil
	})

	done := make(chan error, 1)
	go func() { done <- l.Release() }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Release: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Release did not return: the give-back call ran under the lease lock")
	}
	if again != nil {
		t.Errorf("the re-entrant Release returned %v, want nil", again)
	}
}

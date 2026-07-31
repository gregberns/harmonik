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

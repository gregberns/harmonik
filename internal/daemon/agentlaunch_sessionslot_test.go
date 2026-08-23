package daemon

import (
	"context"
	"io"
	"sync"
	"testing"

	"github.com/gregberns/harmonik/internal/handler"
	hclifecycle "github.com/gregberns/harmonik/internal/handlercontract/lifecycle"
)

type countingSession struct {
	mu           sync.Mutex
	kills        int
	killsUnbound int // kills whose context carried no deadline
}

var _ handler.Session = (*countingSession)(nil)

func (s *countingSession) Kill(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.kills++
	if _, ok := ctx.Deadline(); !ok {
		s.killsUnbound++
	}
	return nil
}

func (s *countingSession) killCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.kills
}

func (s *countingSession) unboundedKillCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.killsUnbound
}

func (s *countingSession) SendInput(context.Context, string) error { return nil }
func (s *countingSession) Wait(context.Context) error              { return nil }
func (s *countingSession) Outcome() handler.Outcome                { return handler.Outcome{} }
func (s *countingSession) Stdout() io.Reader                       { return nil }
func (s *countingSession) Stderr() io.Reader                       { return nil }
func (s *countingSession) CloseStdin() error                       { return nil }
func (s *countingSession) Machine() *hclifecycle.Machine           { return nil }

func TestSessionSlotKillAnnouncedBeforeTheSessionExists(t *testing.T) {
	var slot sessionSlot
	sess := &countingSession{}

	if issued := slot.killOrLatch(); issued {
		t.Fatal("killOrLatch reported a kill against an empty slot; there is nothing there to kill")
	}
	if got := sess.killCount(); got != 0 {
		t.Fatalf("kills before the launch handed a session back = %d, want 0", got)
	}

	slot.set(sess)

	if got := sess.killCount(); got != 1 {
		t.Fatalf("kills after the launch handed the session back = %d, want 1 — the held announcement kill was dropped", got)
	}
}

func TestSessionSlotKillAnnouncedAfterTheSessionExists(t *testing.T) {
	var slot sessionSlot
	sess := &countingSession{}
	slot.set(sess)

	if got := sess.killCount(); got != 0 {
		t.Fatalf("set killed a session with no announcement behind it: kills = %d, want 0", got)
	}
	if issued := slot.killOrLatch(); !issued {
		t.Fatal("killOrLatch did not report the kill it issued against a filled slot")
	}
	if got := sess.killCount(); got != 1 {
		t.Fatalf("kills = %d, want exactly 1", got)
	}
}

// A launch that failed hands back no session, so there is nothing to kill and
// the held announcement must not survive to kill a later one.
func TestSessionSlotFailedLaunchDropsTheHeldKill(t *testing.T) {
	var slot sessionSlot

	slot.killOrLatch()
	slot.set(nil) // what the Launch closure stores when Launch returns an error

	if got := slot.get(); got != nil {
		t.Fatalf("get() after a failed launch = %v, want nil", got)
	}

	later := &countingSession{}
	slot.set(later)
	if got := later.killCount(); got != 0 {
		t.Fatalf("a dropped announcement killed a later session: kills = %d, want 0", got)
	}
}

// The two goroutines of the real launch: the dispatch goroutine fills the slot
// while the stdout interceptor's goroutine announces into it. Whichever order
// they land in, the agent is killed exactly once. Run under -race (the nightly
// lane) this is also the cover for the read that used to race the write.
func TestSessionSlotConcurrentAnnouncementAndSet(t *testing.T) {
	for i := 0; i < 200; i++ {
		var slot sessionSlot
		sess := &countingSession{}

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			if !slot.killOrLatch() {
				return // held; set fires it
			}
		}()
		go func() {
			defer wg.Done()
			slot.set(sess)
		}()
		wg.Wait()

		if got := sess.killCount(); got != 1 {
			t.Fatalf("iteration %d: kills = %d, want exactly 1", i, got)
		}
		if slot.get() != sess {
			t.Fatalf("iteration %d: the slot did not hold the session it was given", i)
		}
	}
}

// Session.Kill sends SIGTERM and escalates to SIGKILL only when its context
// expires. So a kill on an unbounded context waits for ever on an agent that
// ignores SIGTERM — and an agent that announces it has finished and then does
// not exit is exactly the agent this kill exists for. Unbounded, the held kill
// wedges the launch it fires inside and the direct kill wedges the goroutine
// that drains the agent's stdout.
//
// Both orderings are checked, because they reach Session.Kill by different
// routes and each one has been written with context.Background() at some point.
func TestSessionSlotKillsOnABoundedContext(t *testing.T) {
	t.Run("the held kill", func(t *testing.T) {
		var slot sessionSlot
		sess := &countingSession{}

		slot.killOrLatch()
		slot.set(sess)

		if got := sess.killCount(); got != 1 {
			t.Fatalf("kills = %d, want 1", got)
		}
		if got := sess.unboundedKillCount(); got != 0 {
			t.Fatalf("%d of the kills carried no deadline; Session.Kill can never escalate to SIGKILL on one of those", got)
		}
	})

	t.Run("the direct kill", func(t *testing.T) {
		var slot sessionSlot
		sess := &countingSession{}

		slot.set(sess)
		slot.killOrLatch()

		if got := sess.killCount(); got != 1 {
			t.Fatalf("kills = %d, want 1", got)
		}
		if got := sess.unboundedKillCount(); got != 0 {
			t.Fatalf("%d of the kills carried no deadline; Session.Kill can never escalate to SIGKILL on one of those", got)
		}
	})
}

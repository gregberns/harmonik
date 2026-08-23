package daemon_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/handler"
)

const terminalOversubCap = 2

func terminalOversubFixtureSubstrate(t *testing.T, acquireTimeout time.Duration) (handler.Substrate, []handler.SubstrateSession) {
	t.Helper()

	const capN = terminalOversubCap

	sub := daemon.NewTmuxSubstrate(&terminalReserveFixtureAdapter{}, "termres-session",
		daemon.WithSpawnCap(capN),
		daemon.WithSpawnAcquireTimeout(acquireTimeout))

	held := make([]handler.SubstrateSession, 0, capN+1)
	for i := 0; i <= capN; i++ {
		sess, err := terminalReserveFixtureSpawnTerminal(context.Background(), sub)
		if err != nil {
			t.Fatalf("terminal spawn %d failed: %v", i, err)
		}
		held = append(held, sess)
	}
	if got, want := daemon.ExportedSpawnSlotsInUse(sub), capN+1; got != want {
		t.Fatalf("spawnSem not saturated by terminal spawns: in use %d, want %d", got, want)
	}
	return sub, held
}

// TestSpawnCapTerminalOversub_OrdinarySpawnTimesOutInsteadOfFailingStructurally
// is the red test for the bead. Before the fix the spawn below returns
// instantly with the structural "unexpected spawnSem saturation" error and the
// run dies. After it, the spawn waits out its budget and reports a timeout.
func TestSpawnCapTerminalOversub_OrdinarySpawnTimesOutInsteadOfFailingStructurally(t *testing.T) {
	t.Parallel()

	const acquireTimeout = 300 * time.Millisecond
	sub, _ := terminalOversubFixtureSubstrate(t, acquireTimeout)

	type result struct {
		err     error
		elapsed time.Duration
	}
	done := make(chan result, 1)
	go func() {
		start := time.Now()
		_, err := terminalReserveFixtureSpawnNonTerminal(context.Background(), sub)
		done <- result{err: err, elapsed: time.Since(start)}
	}()

	select {
	case got := <-done:
		if got.err == nil {
			t.Fatal("ordinary spawn succeeded with every spawn slot held — the cap is not being enforced")
		}
		if !errors.Is(got.err, daemon.ErrSpawnCapTimeout) {
			t.Errorf("want ErrSpawnCapTimeout (the observable spawn-cap class), got: %v", got.err)
		}
		if !errors.Is(got.err, handler.ErrStructural) {
			t.Errorf("want the error to wrap ErrStructural like every other spawn failure, got: %v", got.err)
		}
		if got.elapsed < acquireTimeout {
			t.Errorf("spawn gave up after %s, which is inside its %s budget — it refused rather than waited", got.elapsed, acquireTimeout)
		}
		for _, want := range []string{"spawn_sem_cap=3", "spawn_sem_in_use=3", "non_terminal_in_use=0"} {
			if !strings.Contains(got.err.Error(), want) {
				t.Errorf("error does not report %s, so the cause is not legible from it: %v", want, got.err)
			}
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ordinary spawn never returned — the wait is unbounded, which is the bug hk-4l7zs removed")
	}
}

// TestSpawnCapTerminalOversub_OrdinarySpawnSucceedsWhenATerminalReleases is the
// point of waiting at all: the condition is transient, so a spawn that waits
// gets its slot. Under the old assertion this spawn failed its run instead.
func TestSpawnCapTerminalOversub_OrdinarySpawnSucceedsWhenATerminalReleases(t *testing.T) {
	t.Parallel()

	sub, held := terminalOversubFixtureSubstrate(t, 5*time.Second)

	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		close(started)
		_, err := terminalReserveFixtureSpawnNonTerminal(context.Background(), sub)
		done <- err
	}()

	<-started
	time.Sleep(50 * time.Millisecond)
	if err := held[0].Kill(context.Background()); err != nil {
		t.Fatalf("killing a terminal session failed: %v", err)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ordinary spawn failed although a slot was released well inside its budget: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ordinary spawn did not wake when a terminal session released its slot")
	}
}

// TestSpawnCapTerminalOversub_TicketGoesBackOnTimeout guards the release path.
// The waiting spawn holds a non-terminal ticket while it waits, and every
// failing arm has to give it back exactly once. Leak it and the cap quietly
// drops by one for the life of the daemon; release it twice and the spawn takes
// a slot from someone else, because Release only refuses to go negative and
// does not panic.
//
// The count is read through behaviour rather than through the semaphore: after
// the timeout, the full cap must still be spawnable and no more.
//
// Honest about its reach: this catches a LEAKED ticket, not a doubly released
// one. With no other holder in flight the second Release is a no-op, because
// Release refuses to go negative — the damage needs a concurrent holder to
// steal from. Reaching that state deterministically needs a seam inside
// Acquire, so the double release is held by the shape of the code (one Release
// per failing arm, each followed by a return) and not by this test.
func TestSpawnCapTerminalOversub_TicketGoesBackOnTimeout(t *testing.T) {
	t.Parallel()

	const capN = terminalOversubCap
	sub, held := terminalOversubFixtureSubstrate(t, 200*time.Millisecond)

	if _, err := terminalReserveFixtureSpawnNonTerminal(context.Background(), sub); err == nil {
		t.Fatal("ordinary spawn succeeded with every spawn slot held")
	}

	for i, sess := range held {
		if err := sess.Kill(context.Background()); err != nil {
			t.Fatalf("killing terminal session %d failed: %v", i, err)
		}
	}
	if got := daemon.ExportedSpawnSlotsInUse(sub); got != 0 {
		t.Fatalf("slots still held after every session was killed: %d — the timed-out spawn leaked one", got)
	}

	for i := 0; i < capN; i++ {
		if _, err := terminalReserveFixtureSpawnNonTerminal(context.Background(), sub); err != nil {
			t.Fatalf("ordinary spawn %d of %d failed on an idle substrate: %v (the timed-out spawn kept its ticket)", i+1, capN, err)
		}
	}
	if _, err := terminalReserveFixtureSpawnNonTerminal(context.Background(), sub); err == nil {
		t.Fatal("a spawn past the cap succeeded — the cap is not being enforced")
	}
}

// TestSpawnCapTerminalOversub_SpentBudgetDoesNotWaitForever covers the case
// where the first wait consumes the whole acquire budget and still succeeds,
// which Acquire allows because it tests saturation before it tests its own
// timer.
//
// The hazard is silent. Acquire documents a non-positive timeout as "no
// timeout", so passing the remaining arithmetic straight through would turn
// this into the indefinite block hk-4l7zs removed. The code branches on the
// configured value instead and takes the timeout arm with no wait at all.
//
// A one-nanosecond budget reaches it through the ordinary spawn path: the
// nonTerminalSem acquire takes its uncontended fast path before any timer runs,
// so the budget is nearly always spent by the time the second wait is computed.
// No test seam is needed, and driving the real entry point means the ticket
// really is held when the arm releases it.
//
// "Nearly always" is the honest word. The clock has about 40 ns of granularity
// here, so a replica of this exact sequence measured a remainder still ABOVE
// zero in roughly one run in three thousand. Such a run takes the neighbouring
// arm — a one-nanosecond Acquire, which times out at once — and returns the
// same sentinel, so this test passes either way. What it holds is the outcome:
// a spent budget fails promptly and reportably instead of waiting forever. The
// negative control is what ties that outcome to the guard, and it carries the
// same one-in-three-thousand caveat.
func TestSpawnCapTerminalOversub_SpentBudgetDoesNotWaitForever(t *testing.T) {
	t.Parallel()

	sub, _ := terminalOversubFixtureSubstrate(t, time.Nanosecond)

	done := make(chan error, 1)
	go func() {
		_, err := terminalReserveFixtureSpawnNonTerminal(context.Background(), sub)
		done <- err
	}()

	select {
	case err := <-done:
		if !errors.Is(err, daemon.ErrSpawnCapTimeout) {
			t.Errorf("want ErrSpawnCapTimeout for a spent budget, got: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a spent budget produced an unbounded wait — the remaining time was passed to Acquire, which reads <= 0 as no timeout")
	}
}

// TestSpawnCapTerminalOversub_CancelledContextReturnsTheTicket covers the third
// failing arm. A cancelled context is not a spawn-cap timeout and must not be
// reported as one — they name different operator problems — and it carries the
// same obligation to give the non-terminal ticket back.
//
// It asserts context.Canceled, and that is what makes it a discriminator. The
// weaker assertions alone (non-nil, not a spawn-cap timeout, wraps
// ErrStructural) are all satisfied by the OLD instant refusal too, so a review
// measured this test passing against the unfixed code. Requiring the cancel
// cause ties it to the arm it is named for.
func TestSpawnCapTerminalOversub_CancelledContextReturnsTheTicket(t *testing.T) {
	t.Parallel()

	sub, held := terminalOversubFixtureSubstrate(t, time.Minute)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := terminalReserveFixtureSpawnNonTerminal(ctx, sub)
		done <- err
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("spawn succeeded on a cancelled context with every slot held")
		}
		if errors.Is(err, daemon.ErrSpawnCapTimeout) {
			t.Errorf("a cancelled context was reported as a spawn-cap timeout, which names a different problem: %v", err)
		}
		if !errors.Is(err, context.Canceled) {
			t.Errorf("want the cancellation to survive in the error, got: %v "+
				"(without this the test also passes on the old instant refusal)", err)
		}
		if !errors.Is(err, handler.ErrStructural) {
			t.Errorf("want the error to wrap ErrStructural, got: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("spawn did not return when its context was cancelled")
	}

	for i, sess := range held {
		if err := sess.Kill(context.Background()); err != nil {
			t.Fatalf("killing terminal session %d failed: %v", i, err)
		}
	}
	recoverCtx, cancelRecover := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelRecover()
	for i := 0; i < terminalOversubCap; i++ {
		if _, err := terminalReserveFixtureSpawnNonTerminal(recoverCtx, sub); err != nil {
			t.Fatalf("ordinary spawn %d of %d failed on an idle substrate: %v (the cancelled spawn kept its ticket)", i+1, terminalOversubCap, err)
		}
	}
}

// TestSpawnCapTerminalOversub_UnboundedTimeoutStillWaits holds the other side
// of that branch. An operator who disables the acquire timeout is asking for
// the pre-hk-4l7zs behaviour, and the spawn must wait for a slot rather than
// read the disabled timeout as an expired one.
func TestSpawnCapTerminalOversub_UnboundedTimeoutStillWaits(t *testing.T) {
	t.Parallel()

	sub, held := terminalOversubFixtureSubstrate(t, -1)

	done := make(chan error, 1)
	go func() {
		_, err := terminalReserveFixtureSpawnNonTerminal(context.Background(), sub)
		done <- err
	}()

	select {
	case err := <-done:
		t.Fatalf("spawn returned %v while every slot was held — an unbounded wait was read as an expired one", err)
	case <-time.After(200 * time.Millisecond):
	}

	if err := held[0].Kill(context.Background()); err != nil {
		t.Fatalf("killing a terminal session failed: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("spawn failed after a slot was released: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("spawn did not wake when a slot was released")
	}
}

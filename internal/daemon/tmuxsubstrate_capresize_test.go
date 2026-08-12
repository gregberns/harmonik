package daemon_test

// tmuxsubstrate_capresize_test.go — regression tests for the two defects an
// independent review found underneath the live spawn-cap resize (hk-ad79i).
//
// # The bugs
//
// Two semaphores gate a spawn. nonTerminalSem holds the operator's cap n;
// spawnSem holds n+1, and the extra slot is the reserve that lets the merge
// node of a finished run start even when every ordinary slot is busy (hk-x882o).
// The terminal path waits for that slot with NO timeout, on purpose, so that
// reviewed work is never thrown away.
//
//   - hk-6yrs9 (P1): lowering the cap set spawnSem's capacity to n+1 with no
//     regard for what was in flight. Drop from 16 to 2 with 16 sessions running
//     and the capacity (3) sits below the in-use count (16). The reserve is
//     gone, and every merge node waits — unbounded — until fourteen sessions
//     drain. New with the cap-lowering fix, because before it the cap only ever
//     went up. It arrives exactly when the operator is throttling a box that is
//     already overloaded.
//
//   - hk-pcjkp (P2): the two capacities moved in a fixed order, non-terminal
//     first, and SetCapacity broadcasts. On a RAISE that wakes every blocked
//     spawn against a spawnSem that still holds the OLD capacity; the woken
//     spawns then miss the fast-path-only TryAcquire in acquireSpawnSlot and
//     fail with a structural error. Asking for MORE capacity was the case that
//     refused spawns.
//
// # Fix
//
// SetSpawnCap orders the two moves by direction — widen from the inside out,
// narrow from the outside in — so spawnSem.Capacity() >= cap+1 holds at every
// instant rather than only at the ends; and it clamps the spawnSem target so a
// shrink never takes the reserve from work already in flight. The clamp is
// given back on the release path as those sessions drain.
//
// # What is tested
//
//   - TestSpawnCapResize_InvariantHoldsInsideTheResizeWindow: the cheapest red
//     test for hk-pcjkp. It reads both capacities from INSIDE the window
//     between the two moves, in both directions. Pre-fix the raise reports a
//     spawn capacity below the non-terminal cap.
//   - TestSpawnCapRaise_WokenSpawnsAreNotRefused: the failure a user sees — three
//     spawns blocked at the cap, the cap raised, and none of them refused.
//   - TestSpawnCapShrink_TerminalReserveSurvivesInFlightSessions: the hk-6yrs9
//     incident. Pre-fix the terminal spawn never returns.
//   - TestSpawnCapShrink_ReserveCountsTerminalSessionsToo: the reserve is
//     measured against every slot in flight, merge nodes included. The first
//     version of the fix counted only the ordinary sessions; this fails at the
//     exact numbers that version produces.
//   - TestSpawnCapShrink_ReserveIsGivenBackAsSessionsDrain: the clamp is not
//     permanent — once the excess sessions are gone the capacity is back at the
//     operator's cap plus one, so terminal spawns cannot oversubscribe the box
//     the operator was throttling.
//   - TestSpawnCapResize_SettingTheCapItAlreadyHoldsChangesNothing: a ratchet in
//     the clamp. Setting the cap to the number it already holds ran the shrink
//     path, which re-clamped one slot higher whenever a merge node held the
//     reserve, so repeating the call climbed away from the operator's cap.
//
// # Helper prefix
//
// Helpers use the prefix "capResizeFixture" per implementer-protocol.md. The
// fake tmux adapter and the two spawn helpers are reused from
// tmuxsubstrate_terminalreserve_test.go — same package, same subject.
//
// # Beads
//
//   - hk-6yrs9 (a shrink removes the terminal reserve)
//   - hk-pcjkp (a raise refuses a spawn while the two bounds disagree)

import (
	"context"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/handler"
)

// capResizeFixtureSubstrate builds a capped substrate over the shared fake tmux
// adapter, with an acquire timeout generous enough that a blocked non-terminal
// spawn waits for the resize rather than timing out first.
func capResizeFixtureSubstrate(capN int, acquireTimeout time.Duration) handler.Substrate {
	return daemon.NewTmuxSubstrate(&terminalReserveFixtureAdapter{}, "capresize-session",
		daemon.WithSpawnCap(capN),
		daemon.WithSpawnAcquireTimeout(acquireTimeout))
}

// capResizeFixtureSaturate holds n non-terminal sessions and returns them, so a
// test can drain them one at a time.
func capResizeFixtureSaturate(t *testing.T, sub handler.Substrate, n int) []handler.SubstrateSession {
	t.Helper()
	held := make([]handler.SubstrateSession, 0, n)
	for i := 0; i < n; i++ {
		sess, err := terminalReserveFixtureSpawnNonTerminal(context.Background(), sub)
		if err != nil {
			t.Fatalf("saturating non-terminal spawn %d failed: %v", i, err)
		}
		held = append(held, sess)
	}
	return held
}

// TestSpawnCapResize_InvariantHoldsInsideTheResizeWindow reads both capacities
// from inside the window between the two moves. The invariant the whole design
// rests on is that spawnSem always has at least one more slot than the
// non-terminal cap; hk-pcjkp is exactly that invariant being false for the
// duration of a raise, and a broadcast wakes waiters into it on purpose.
func TestSpawnCapResize_InvariantHoldsInsideTheResizeWindow(t *testing.T) {
	t.Parallel()

	sub := capResizeFixtureSubstrate(2, time.Second)

	type reading struct{ nonTerminal, spawn int }
	var seen []reading
	daemon.ExportedSetCapResizeMid(sub, func() {
		seen = append(seen, reading{
			nonTerminal: daemon.ExportedSpawnCapSize(sub),
			spawn:       daemon.ExportedSpawnSemCapacity(sub),
		})
	})

	daemon.ExportedSetSpawnCap(sub, 64) // raise
	daemon.ExportedSetSpawnCap(sub, 3)  // lower

	if len(seen) != 2 {
		t.Fatalf("mid-resize seam ran %d times, want 2 (once per resize)", len(seen))
	}
	for i, r := range seen {
		if r.spawn < r.nonTerminal+1 {
			t.Errorf("resize %d: inside the window spawn capacity=%d, non-terminal cap=%d — "+
				"the reserved slot does not exist there, so a spawn woken by the first move is refused (hk-pcjkp)",
				i, r.spawn, r.nonTerminal)
		}
	}
}

// TestSpawnCapRaise_WokenSpawnsAreNotRefused is the user-visible half of
// hk-pcjkp: spawns blocked at a cap of 1, the cap raised to 64, and every one
// of them must start. Pre-fix the first move wakes them all against a spawn
// semaphore that still holds the old capacity, and every one past the first is
// refused with a structural error.
//
// The mid-resize seam is what makes this deterministic. The window is
// microseconds wide in production, so without holding it open the test would
// pass by luck most runs.
func TestSpawnCapRaise_WokenSpawnsAreNotRefused(t *testing.T) {
	t.Parallel()

	const waiters = 3
	sub := capResizeFixtureSubstrate(1, 10*time.Second)

	// One session holds the only slot; the waiters below all block on it.
	capResizeFixtureSaturate(t, sub, 1)

	started := make(chan struct{}, waiters)
	// Hold the window open until every waiter has entered SpawnWindow, so the
	// broadcast from the first move lands while the second has not happened.
	daemon.ExportedSetCapResizeMid(sub, func() {
		for i := 0; i < waiters; i++ {
			<-started
		}
		time.Sleep(50 * time.Millisecond)
	})

	errs := make(chan error, waiters)
	for i := 0; i < waiters; i++ {
		go func() {
			started <- struct{}{}
			_, err := terminalReserveFixtureSpawnNonTerminal(context.Background(), sub)
			errs <- err
		}()
	}

	daemon.ExportedSetSpawnCap(sub, 64)

	deadline := time.After(15 * time.Second)
	for i := 0; i < waiters; i++ {
		select {
		case err := <-errs:
			if err != nil {
				t.Fatalf("a spawn was refused while the cap was being RAISED: %v\n"+
					"(want nil — raising the cap must never fail a spawn: hk-pcjkp)", err)
			}
		case <-deadline:
			t.Fatal("a blocked spawn never returned after the cap was raised")
		}
	}
}

// TestSpawnCapShrink_TerminalReserveSurvivesInFlightSessions is the hk-6yrs9
// incident. Four sessions are in flight, the operator throttles the cap down to
// one, and the merge node of a finished run must still start. Pre-fix the spawn
// semaphore's capacity drops below its in-use count, there is no reserve, and
// the terminal path — which has no timeout at all — blocks until the box
// drains.
func TestSpawnCapShrink_TerminalReserveSurvivesInFlightSessions(t *testing.T) {
	t.Parallel()

	const capN = 4
	sub := capResizeFixtureSubstrate(capN, 200*time.Millisecond)
	capResizeFixtureSaturate(t, sub, capN)

	if got := daemon.ExportedSpawnSlotsInUse(sub); got != capN {
		t.Fatalf("pool not saturated: slots in use = %d, want %d", got, capN)
	}

	daemon.ExportedSetSpawnCap(sub, 1)

	if got, want := daemon.ExportedSpawnSemCapacity(sub), capN+1; got < want {
		t.Errorf("after the shrink, spawn capacity = %d with %d sessions in flight; want at least %d "+
			"so one slot is still free for a terminal spawn (hk-6yrs9)", got, capN, want)
	}
	// The operator's cap DID come down — the clamp must not quietly refuse the
	// throttle for ordinary work.
	if got := daemon.ExportedSpawnCapSize(sub); got != 1 {
		t.Errorf("non-terminal cap = %d after lowering to 1 — the throttle did not take effect", got)
	}

	done := make(chan error, 1)
	go func() {
		_, err := terminalReserveFixtureSpawnTerminal(context.Background(), sub)
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("terminal spawn failed after the cap was lowered: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("terminal spawn never returned after the cap was lowered — lowering the cap took the " +
			"reserved slot away from work that was already finished, and the terminal path has no timeout (hk-6yrs9)")
	}
}

// TestSpawnCapShrink_ReserveCountsTerminalSessionsToo pins down WHICH count the
// reserve is measured against. Counting only the non-terminal sessions is the
// intuitive choice and it is wrong: terminal sessions hold spawn slots too, and
// they are invisible to that count, so a shrink taken against it can still land
// a capacity below what is in flight — no reserve, and the next merge node
// waits with no timeout. Here three ordinary sessions and two merge nodes are
// in flight when the cap drops to one.
func TestSpawnCapShrink_ReserveCountsTerminalSessionsToo(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sub := capResizeFixtureSubstrate(4, 200*time.Millisecond)
	capResizeFixtureSaturate(t, sub, 3)
	for i := 0; i < 2; i++ {
		if _, err := terminalReserveFixtureSpawnTerminal(ctx, sub); err != nil {
			t.Fatalf("terminal spawn %d before the shrink failed: %v", i, err)
		}
	}
	inFlight := daemon.ExportedSpawnSlotsInUse(sub)
	if inFlight != 5 {
		t.Fatalf("slots in use = %d before the shrink, want 5 (3 ordinary + 2 merge)", inFlight)
	}

	daemon.ExportedSetSpawnCap(sub, 1)

	if got := daemon.ExportedSpawnSemCapacity(sub); got <= inFlight {
		t.Fatalf("after the shrink, spawn capacity = %d with %d slots in flight; want more than %d so one "+
			"is still free. A reserve measured against the ordinary sessions alone reads %d here and "+
			"leaves the pool oversubscribed (hk-6yrs9)", got, inFlight, inFlight, 4)
	}

	done := make(chan error, 1)
	go func() {
		_, err := terminalReserveFixtureSpawnTerminal(ctx, sub)
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("terminal spawn failed after the shrink: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("terminal spawn never returned after the cap was lowered with merge nodes already in flight")
	}
}

// TestSpawnCapShrink_ReserveIsGivenBackAsSessionsDrain holds the other half of
// the clamp honest. Clamping the shrink is only safe if it is temporary: a cap
// of one that keeps admitting seventeen sessions is not a throttle. As the
// sessions the clamp was protecting drain away, the capacity must come back
// down to the operator's cap plus the one reserved slot.
func TestSpawnCapShrink_ReserveIsGivenBackAsSessionsDrain(t *testing.T) {
	t.Parallel()

	const capN = 4
	sub := capResizeFixtureSubstrate(capN, 200*time.Millisecond)
	held := capResizeFixtureSaturate(t, sub, capN)

	daemon.ExportedSetSpawnCap(sub, 1)

	ctx := context.Background()
	for i, sess := range held {
		if err := sess.Kill(ctx); err != nil {
			t.Fatalf("killing held session %d: %v", i, err)
		}
	}

	if got := daemon.ExportedSpawnSlotsInUse(sub); got != 0 {
		t.Fatalf("slots in use = %d after every session was killed, want 0", got)
	}
	if got, want := daemon.ExportedSpawnSemCapacity(sub), 2; got != want {
		t.Errorf("spawn capacity = %d once the drain finished, want %d (cap 1 plus the reserved slot); "+
			"the clamp that protected in-flight work outlived the work and now oversubscribes the box "+
			"the operator was throttling", got, want)
	}
}

// TestSpawnCapResize_SettingTheCapItAlreadyHoldsChangesNothing covers a ratchet
// that a second review found in the clamp itself. The clamp reads the slots in
// use, and a merge node holding the reserved slot is one of them, so running the
// shrink path while that slot is occupied lands the capacity one ABOVE where it
// already was. Nothing about that path checks whether the cap actually moved, so
// asking for the cap the substrate already holds walked it upward — and every
// repeat walked it up again, away from the number the operator set. An operator
// setting a cap to the value it already has must get a substrate that did
// nothing.
//
// Bead ref: hk-6yrs9.
func TestSpawnCapResize_SettingTheCapItAlreadyHoldsChangesNothing(t *testing.T) {
	t.Parallel()

	const capN = 4
	ctx := context.Background()
	sub := capResizeFixtureSubstrate(capN, 200*time.Millisecond)
	capResizeFixtureSaturate(t, sub, capN)
	if _, err := terminalReserveFixtureSpawnTerminal(ctx, sub); err != nil {
		t.Fatalf("terminal spawn into the reserved slot failed: %v", err)
	}

	inUseBefore := daemon.ExportedSpawnSlotsInUse(sub)
	capBefore := daemon.ExportedSpawnSemCapacity(sub)
	if inUseBefore != capN+1 || capBefore != capN+1 {
		t.Fatalf("slots in use = %d, spawn capacity = %d before the no-op resize; want %d and %d "+
			"(every ordinary slot busy and a merge node on the reserved one)",
			inUseBefore, capBefore, capN+1, capN+1)
	}

	for i := 0; i < 3; i++ {
		daemon.ExportedSetSpawnCap(sub, capN)

		if got := daemon.ExportedSpawnSemCapacity(sub); got != capBefore {
			t.Fatalf("spawn capacity = %d after setting the cap to %d — the value it already held — "+
				"%d time(s); want %d, unchanged. Re-running the shrink clamp while a merge node holds "+
				"the reserved slot admits one more session per call, so a repeated `set-concurrency %d` "+
				"climbs away from the cap the operator set (hk-6yrs9)",
				got, capN, i+1, capBefore, capN)
		}
		if got := daemon.ExportedSpawnSlotsInUse(sub); got != inUseBefore {
			t.Fatalf("slots in use = %d after a no-op resize, want %d", got, inUseBefore)
		}
	}
}

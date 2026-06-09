package daemon_test

// workloopeventsource_hk37giq_test.go — regression guard for the concurrent-
// dispatch wedge (hk-37giq): the per-run event tap MUST fan every event out to
// every independent subscriber, NOT route each event to exactly one competing
// consumer.
//
// Root cause (converged by 3 independent investigations + commit 7641f648):
// a single per-run channel (tapCh) was shared by TWO consumers — the
// chanAgentEventSource feeding waitAgentReady, and pasteInjectQuitOnCommit's
// launch/heartbeat watchdog. A Go channel receive is EXCLUSIVE (not broadcast),
// so under 2+ concurrent runs waitAgentReady's drain goroutine stayed hot and
// consumed every agent_heartbeat; pasteInjectQuitOnCommit never observed
// firstHeartbeatSeen, reset launchDeadline forever, and the run wedged at launch
// (launch_stall_detected → run_stale). The fix makes the tap a fan-out: each
// Subscribe() returns an independent buffered channel and every Emit writes a
// COPY to every subscriber.
//
// This test reproduces the competing-consumer condition: TWO subscribers, one
// drained aggressively by a hot goroutine, and asserts the OTHER (passive)
// subscriber still receives EVERY emitted heartbeat. On the OLD single-channel
// design the hot drainer would steal most/all receives, so the passive consumer
// would observe far fewer than the emitted count and this test would FAIL. On
// the fan-out it PASSES. Run with -race.
//
// Bead: hk-37giq.

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

func hk37giqRunID(t *testing.T) core.RunID {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("uuid.NewV7: %v", err)
	}
	return core.RunID(id)
}

// TestPerRunEventTap_FanOut_PassiveSubscriberNotStarvedByAggressiveDrain is the
// core hk-37giq regression. It registers two independent subscribers (mirroring
// waitAgentReady and the pasteInjectQuitOnCommit watchdog), spins a hot goroutine
// that drains subscriber A as fast as possible, and asserts subscriber B receives
// EVERY emitted heartbeat. On the pre-fix single-shared-channel design the hot
// drain on A would consume the events B never saw → B's count < emitted → FAIL.
func TestPerRunEventTap_FanOut_PassiveSubscriberNotStarvedByAggressiveDrain(t *testing.T) {
	t.Parallel()

	runID := hk37giqRunID(t)
	tap, subA := daemon.ExportedNewPerRunEventTap(runID)
	subB := tap.ExportedSubscribe()

	// Emit fewer events than the per-subscriber buffer (perRunEventTapBufSize=64)
	// so the fan-out NEVER drops on B even though B is not drained until after all
	// emits. Any count > 0 and <= 64 makes the assertion deterministic on the
	// fan-out and clearly violated on the old single-channel design.
	const emitCount = 50

	ctx := context.Background()

	// Hot drainer on A: receive as fast as possible until the test signals stop.
	// On the OLD shared-channel design this goroutine wins most receives, starving
	// B. On the fan-out it cannot affect B's independent buffer at all.
	stop := make(chan struct{})
	var aGot int
	var aMu sync.Mutex
	var drainWG sync.WaitGroup
	drainWG.Add(1)
	go func() {
		defer drainWG.Done()
		for {
			select {
			case <-stop:
				// Final non-blocking drain of anything buffered.
				for {
					select {
					case <-subA:
						aMu.Lock()
						aGot++
						aMu.Unlock()
					default:
						return
					}
				}
			case <-subA:
				aMu.Lock()
				aGot++
				aMu.Unlock()
			}
		}
	}()

	// Emit emitCount heartbeats through the production Emit (fan-out) path.
	for i := 0; i < emitCount; i++ {
		if err := tap.ExportedEmit(ctx, core.EventTypeAgentHeartbeat); err != nil {
			t.Fatalf("emit %d: %v", i, err)
		}
	}

	// Give the hot drainer a moment to steal events on the OLD design (it would
	// drain A and, on a shared channel, those receives would be the SAME events B
	// needed). On the fan-out this changes nothing for B.
	time.Sleep(50 * time.Millisecond)

	// Now drain B and count. B must have received EVERY emitted heartbeat.
	bGot := 0
	deadline := time.After(2 * time.Second)
loop:
	for bGot < emitCount {
		select {
		case ev, ok := <-subB:
			if !ok {
				break loop
			}
			if core.EventType(ev.Type) == core.EventTypeAgentHeartbeat {
				bGot++
			}
		case <-deadline:
			break loop
		}
	}

	close(stop)
	drainWG.Wait()

	if bGot != emitCount {
		t.Fatalf("passive subscriber B received %d/%d heartbeats — the competing "+
			"consumer (aggressive drain on A) starved it; tap is NOT fanning out "+
			"to independent subscribers (hk-37giq wedge)", bGot, emitCount)
	}

	// Sanity: A also received its own independent copies (it was drained hot, so
	// it should have seen all of them too — proving fan-out, not steal).
	aMu.Lock()
	finalA := aGot
	aMu.Unlock()
	if finalA != emitCount {
		t.Errorf("aggressive subscriber A received %d/%d — fan-out should deliver "+
			"every event to A as well (independent copies)", finalA, emitCount)
	}
}

// TestPerRunEventTap_InitialSubscriberStillReceives is a focused guard that the
// channel returned by newPerRunEventTap (the waitAgentReady channel in
// production) continues to receive every event after the fan-out refactor — i.e.
// the first subscriber's behaviour is preserved exactly (hk-37giq constraint:
// "preserve waitAgentReady's existing behaviour").
func TestPerRunEventTap_InitialSubscriberStillReceives(t *testing.T) {
	t.Parallel()

	runID := hk37giqRunID(t)
	tap, initial := daemon.ExportedNewPerRunEventTap(runID)

	const n = 10
	for i := 0; i < n; i++ {
		if err := tap.ExportedEmit(context.Background(), core.EventTypeAgentHeartbeat); err != nil {
			t.Fatalf("emit %d: %v", i, err)
		}
	}

	got := 0
	deadline := time.After(time.Second)
	for got < n {
		select {
		case ev := <-initial:
			if core.EventType(ev.Type) == core.EventTypeAgentHeartbeat {
				got++
			}
		case <-deadline:
			t.Fatalf("initial subscriber received only %d/%d events", got, n)
		}
	}
}

// TestPerRunEventTap_FanOut_RunIDStamped verifies the synthetic envelope carries
// the run id on the fan-out path (the per-run scoping contract waitAgentReady and
// the watchdog rely on). Belt-and-braces for the refactor.
func TestPerRunEventTap_FanOut_RunIDStamped(t *testing.T) {
	t.Parallel()

	runID := hk37giqRunID(t)
	tap, subA := daemon.ExportedNewPerRunEventTap(runID)
	subB := tap.ExportedSubscribe()

	if err := tap.ExportedEmit(context.Background(), core.EventTypeAgentHeartbeat); err != nil {
		t.Fatalf("emit: %v", err)
	}

	for name, ch := range map[string]<-chan core.EventEnvelope{"A": subA, "B": subB} {
		select {
		case ev := <-ch:
			if ev.RunID == nil || *ev.RunID != runID {
				t.Errorf("subscriber %s: run id not stamped on fan-out envelope (got %v, want %v)", name, ev.RunID, runID)
			}
		case <-time.After(time.Second):
			t.Errorf("subscriber %s: no event delivered on fan-out", name)
		}
	}
}

package daemon_test

// vn4_keystone_demo_hkukhzu_test.go — deterministic keystone demonstration of the
// hk-37giq regression-guard property at the per-run-tap MECHANISM level
// (validation-net VN4 keystone, bead hk-ukhzu).
//
// # Why this exists (read the VN4 flagship docstring first)
//
// The literal VN4 ask — "revert 53ead2aa, re-run the end-to-end test, confirm it
// FAILS" — is structurally unsatisfiable for an IN-PROCESS daemon scenario test,
// because the hk-37giq wedge requires the pasteInjectQuitOnCommit watchdog to run
// as a SECOND consumer of the per-run tap, and that watchdog only launches on the
// tmux-substrate path (runPasteTarget must be a quitSender). The substrate path
// has a nil stdout watcher, so agent_ready and the run outcome arrive over the
// hook-bridge UNIX socket, NOT stdout — a fake-adapter scenario test times out at
// agent_ready BEFORE the watchdog phase. Driving it requires real tmux + a real
// hook-bridge socket relay, which the validation-net brief forbids on the shared
// box. (Both facts were established empirically — see the VN4 flagship file.)
//
// So the deterministic keystone is at the tap MECHANISM level: the wedge is a
// competing-consumer starve on a single shared channel. This test reconstructs
// BOTH designs as local mimics and asserts the contrasting behaviour:
//
//   - The PRE-FIX single-shared-channel design (one buffered channel consumed by
//     two goroutines — a hot drainer mimicking waitAgentReady, and a passive
//     consumer mimicking the watchdog) STARVES the passive consumer: a Go channel
//     receive is exclusive, so the hot drainer steals the events the watchdog
//     needed → the watchdog would never see firstHeartbeatSeen → wedge.
//   - The POST-FIX fan-out design (the REAL perRunEventTap via the exported test
//     seam: each Subscribe() returns an independent channel, every Emit copies to
//     every subscriber) delivers EVERY event to the passive consumer even while
//     the other is drained hot.
//
// This is the exact failure mode the hk-37giq incident lacked a guard for, made
// deterministic and self-contained. The shipped channel-level test
// (workloopeventsource_hk37giq_test.go) proves the GOOD design; this proves the
// BAD design fails, closing the regression-guard loop. Runs under -race.
//
// Helper prefix: vn4ks (vn4-keystone). Bead: hk-ukhzu. Refs: hk-37giq.

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

func vn4ksRunID(t *testing.T) core.RunID {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("uuid.NewV7: %v", err)
	}
	return core.RunID(id)
}

// TestVN4Keystone_SingleSharedChannel_StarvesPassiveConsumer demonstrates the
// PRE-FIX wedge: with ONE shared channel and two consumers (a hot drainer + a
// passive consumer), the passive consumer is starved of nearly all events. This
// is the mechanism behind the launch-suppression reset-forever wedge.
//
// The single shared channel here is a faithful mimic of the pre-53ead2aa
// perRunEventTap (one `ch chan core.EventEnvelope` written non-blocking by Emit,
// consumed by whichever goroutine wins the receive). We assert the passive
// consumer receives strictly FEWER than the emitted count — proving the starve.
func TestVN4Keystone_SingleSharedChannel_StarvesPassiveConsumer(t *testing.T) {
	t.Parallel()

	const bufSize = 64
	const emitCount = 50

	// Pre-fix mimic: a single shared channel.
	shared := make(chan core.EventEnvelope, bufSize)

	// Hot drainer (mimics waitAgentReady's drain goroutine staying hot after
	// readyCancel until it happens to select ctx.Done()). It races the passive
	// consumer for every receive on the SHARED channel.
	stop := make(chan struct{})
	var drainWG sync.WaitGroup
	var hotGot int
	var hotMu sync.Mutex
	drainWG.Add(1)
	go func() {
		defer drainWG.Done()
		for {
			select {
			case <-stop:
				return
			case <-shared:
				hotMu.Lock()
				hotGot++
				hotMu.Unlock()
			}
		}
	}()

	// Emit emitCount events onto the single shared channel (non-blocking, as the
	// real tap does).
	for i := 0; i < emitCount; i++ {
		env := core.EventEnvelope{Type: string(core.EventTypeAgentHeartbeat)}
		select {
		case shared <- env:
		default:
		}
		// Tiny pause lets the hot drainer keep up, maximising the starve — the
		// real waitAgentReady drainer is similarly hot relative to the slow
		// watchdog poll loop.
		time.Sleep(time.Millisecond)
	}

	// Now let the "passive consumer" (the watchdog mimic) try to read. By the
	// time the watchdog's slow poll loop would check, the hot drainer has already
	// consumed the events from the single shared channel.
	time.Sleep(20 * time.Millisecond)
	passiveGot := 0
	drainDeadline := time.After(200 * time.Millisecond)
draining:
	for {
		select {
		case <-shared:
			passiveGot++
		case <-drainDeadline:
			break draining
		default:
			break draining
		}
	}
	close(stop)
	drainWG.Wait()

	// The keystone assertion: the passive consumer is STARVED — it sees strictly
	// fewer than the emitted events because the hot drainer stole them from the
	// single shared channel. (In practice passiveGot is ~0.)
	if passiveGot >= emitCount {
		t.Fatalf("single-shared-channel mimic did NOT starve the passive consumer "+
			"(passive got %d/%d) — the wedge precondition was not reproduced",
			passiveGot, emitCount)
	}
	t.Logf("VN4 keystone (pre-fix mimic): passive consumer STARVED — got %d/%d "+
		"(hot drainer stole %d); this is the launch-suppression wedge precondition",
		passiveGot, emitCount, func() int { hotMu.Lock(); defer hotMu.Unlock(); return hotGot }())
}

// TestVN4Keystone_FanOutTap_DeliversToBothConsumers demonstrates the POST-FIX
// behaviour against the REAL perRunEventTap (via the exported test seam): with
// the fan-out, the passive consumer receives EVERY emitted event even while the
// other subscriber is drained hot. This is the same property the shipped
// workloopeventsource_hk37giq_test.go guards; reproduced here so the keystone
// file shows both halves of the contrast in one place.
func TestVN4Keystone_FanOutTap_DeliversToBothConsumers(t *testing.T) {
	t.Parallel()

	runID := vn4ksRunID(t)
	tap, subA := daemon.ExportedNewPerRunEventTap(runID)
	subB := tap.ExportedSubscribe() // the watchdog's independent subscription

	const emitCount = 50

	// Hot drainer on A (mimics waitAgentReady).
	stop := make(chan struct{})
	var drainWG sync.WaitGroup
	drainWG.Add(1)
	go func() {
		defer drainWG.Done()
		for {
			select {
			case <-stop:
				return
			case <-subA:
			}
		}
	}()

	ctx := context.Background()
	for i := 0; i < emitCount; i++ {
		if err := tap.ExportedEmit(ctx, core.EventTypeAgentHeartbeat); err != nil {
			t.Fatalf("emit %d: %v", i, err)
		}
	}

	// The passive consumer B must receive EVERY event despite A being drained hot.
	bGot := 0
	deadline := time.After(2 * time.Second)
loop:
	for bGot < emitCount {
		select {
		case <-subB:
			bGot++
		case <-deadline:
			break loop
		}
	}
	close(stop)
	drainWG.Wait()

	if bGot != emitCount {
		t.Fatalf("fan-out tap: passive consumer B received %d/%d — the fix should "+
			"deliver every event to the watchdog's independent subscription", bGot, emitCount)
	}
	t.Logf("VN4 keystone (post-fix, real tap): passive consumer received %d/%d — "+
		"no starve; the watchdog observes the heartbeat and advances", bGot, emitCount)
}

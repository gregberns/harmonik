package daemon

// workingphasewatchdog_rt19c_test.go — the Working-phase watchdogs, driven on a
// FakeClock (P2 E5 RT19c).
//
// These tests could not exist before RT19c. Each one exercises a timeout branch
// whose real duration is minutes (7-minute post-ready hang, 10-minute gate-file
// verdict wait, 90-minute commit hard ceiling) and each completes in microseconds
// of wall time, because every deadline, ticker and kill grace inside the watchdog
// now reads the injected substrate.ClockPort rather than package time.
//
// The wall-clock assertion in each test is the load-bearing one: it fails if any
// site in the converted loop regresses to a raw time.After / time.Now, because a
// mixed clock leaves the loop comparing virtual time against wall time and it
// never terminates within the bound.
//
// Helper prefix: rt19c* (implementer-protocol.md §Helper-prefix discipline).

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/substrate"
)

// rt19cFakeClockEpoch is an arbitrary fixed virtual start instant. Nothing in the
// watchdogs reads absolute wall time, so the value only has to be stable.
var rt19cFakeClockEpoch = time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)

// rt19cWallBudget bounds the REAL time each test may take. The virtual waits
// being driven are 7–90 minutes; anything close to this bound means a site in
// the loop is still on the wall clock.
const rt19cWallBudget = 30 * time.Second

// rt19cOvershoot is a single VIRTUAL jump larger than any deadline these
// watchdogs can hold — production's longest is the 90-minute commit hard
// ceiling, and every test that shrinks those package vars only ever shrinks
// them. Overshooting lets a test drive a timeout branch WITHOUT rewriting the
// shared timing globals, which is what makes these tests safe to run beside the
// millisecond-tuned ones. It costs no wall time; it only sets how many ticker
// boundaries FakeClock.Advance walks.
const rt19cOvershoot = 4 * time.Hour

// ─────────────────────────────────────────────────────────────────────────────
// Stubs
// ─────────────────────────────────────────────────────────────────────────────

// rt19cQuitKiller satisfies quitSender and sessionKiller and NOTHING else, so
// the optional paneLivenessChecker / paneOutputSizer / commandRunnerProvider /
// enterSender branches all stay disabled.
type rt19cQuitKiller struct {
	mu       sync.Mutex
	quits    int
	kills    int
	quitSent chan struct{}
	killed   chan struct{}
}

func newRT19cQuitKiller() *rt19cQuitKiller {
	return &rt19cQuitKiller{
		quitSent: make(chan struct{}, 1),
		killed:   make(chan struct{}, 1),
	}
}

func (q *rt19cQuitKiller) SendQuitToLastPane(context.Context) error {
	q.mu.Lock()
	q.quits++
	q.mu.Unlock()
	select {
	case q.quitSent <- struct{}{}:
	default:
	}
	return nil
}

func (q *rt19cQuitKiller) Kill(context.Context) error {
	q.mu.Lock()
	q.kills++
	q.mu.Unlock()
	select {
	case q.killed <- struct{}{}:
	default:
	}
	return nil
}

func (q *rt19cQuitKiller) counts() (quits, kills int) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.quits, q.kills
}

// rt19cAwait waits for ch with a real-time bound, failing the test on timeout.
func rt19cAwait(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(rt19cWallBudget):
		t.Fatalf("timed out (%v of REAL time) waiting for %s — a converted site is probably still on the wall clock", rt19cWallBudget, what)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// postreadyhang.go — waitPostAgentReadyProgress
// ─────────────────────────────────────────────────────────────────────────────

// TestRT19cPostAgentReadyHang_FiresOnFakeClock drives the post-agent_ready hang
// bound (7 minutes in production) to completion in virtual time.
func TestRT19cPostAgentReadyHang_FiresOnFakeClock(t *testing.T) {
	t.Parallel()

	const timeout = 7 * time.Minute
	clk := substrate.NewFakeClock(rt19cFakeClockEpoch)
	eventCh := make(chan core.EventEnvelope) // never receives

	errCh := make(chan error, 1)
	go func() {
		errCh <- waitPostAgentReadyProgress(context.Background(), clk, eventCh, timeout)
	}()

	wallStart := time.Now()
	clk.BlockUntil(1) // the substrate.After sleeper is armed
	clk.Advance(timeout + time.Second)

	select {
	case err := <-errCh:
		if !errors.Is(err, ErrPostAgentReadyHang) {
			t.Fatalf("waitPostAgentReadyProgress = %v; want ErrPostAgentReadyHang", err)
		}
	case <-time.After(rt19cWallBudget):
		t.Fatalf("waitPostAgentReadyProgress did not return within %v of REAL time", rt19cWallBudget)
	}

	if wall := time.Since(wallStart); wall > rt19cWallBudget {
		t.Fatalf("drove a %v virtual timeout in %v of REAL time; want well under %v", timeout, wall, rt19cWallBudget)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// dot_gate.go — pasteInjectQuitOnGateFile
// ─────────────────────────────────────────────────────────────────────────────

// TestRT19cQuitOnGateFile_TimeoutFiresOnFakeClock drives the cognition-gate
// verdict wait (gateFileTimeout, 10 minutes in production) past its deadline and
// asserts the full /quit → grace → Kill sequence fires.
//
// Note this path has NO live production traffic today (the default workflow.dot
// uses a tool-command commit_gate), so before RT19c its timeout branch had no
// automated coverage at all — the plan's §8 risk 3.
func TestRT19cQuitOnGateFile_TimeoutFiresOnFakeClock(t *testing.T) {
	t.Parallel()

	clk := substrate.NewFakeClock(rt19cFakeClockEpoch)
	qk := newRT19cQuitKiller()
	wtPath := t.TempDir() // gate-verdict.json intentionally absent

	// gateFileTimeout / gateFilePollInterval are the only two timing vars in this
	// package that no test rewrites, so reading them is safe and keeps the test
	// self-documenting. noChangeKillDelay IS rewritten by the pasteinject tests,
	// so the kill grace below uses rt19cOvershoot instead of reading it.
	verdictTimeout := gateFileTimeout
	pollInterval := gateFilePollInterval

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		// nil runner ⇒ gateVerdictExistsVia falls back to os.Stat, which never
		// finds a verdict in the empty temp worktree.
		pasteInjectQuitOnGateFile(ctx, clk, nil, qk, qk, wtPath, nil)
	}()

	wallStart := time.Now()

	clk.BlockUntil(1) // poll ticker armed
	clk.Advance(verdictTimeout + pollInterval)
	rt19cAwait(t, qk.quitSent, "/quit after the gate-verdict timeout")

	clk.BlockUntil(2) // ticker + the post-quit kill-grace sleeper
	clk.Advance(rt19cOvershoot)
	rt19cAwait(t, qk.killed, "Kill after the post-quit grace")
	rt19cAwait(t, done, "pasteInjectQuitOnGateFile to return")

	if quits, kills := qk.counts(); quits != 1 || kills != 1 {
		t.Fatalf("quits=%d kills=%d; want exactly 1 each", quits, kills)
	}
	if wall := time.Since(wallStart); wall > rt19cWallBudget {
		t.Fatalf("drove a %v virtual gate timeout in %v of REAL time; want well under %v", verdictTimeout, wall, rt19cWallBudget)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// pasteinject.go — pasteInjectQuitOnCommit
// ─────────────────────────────────────────────────────────────────────────────

// TestRT19cQuitOnCommit_HardCeilingFiresOnFakeClock drives the hk-9vp51 absolute
// commit hard ceiling — the 90-minute backstop that bounds a truly-hung-but-
// pane-active implementer — entirely in virtual time.
//
// This is the branch the concurrent-dispatch wedge history keeps landing on
// (hk-37giq, hk-jgxqc, hk-trjef, hk-7srrd). Before RT19c a test could only reach
// it by shrinking commitHardCeiling to milliseconds and racing the real clock,
// which is exactly the shape of the two load-sensitive flakes in this package.
func TestRT19cQuitOnCommit_HardCeilingFiresOnFakeClock(t *testing.T) {
	t.Parallel()

	clk := substrate.NewFakeClock(rt19cFakeClockEpoch)
	qk := newRT19cQuitKiller()
	wtPath := t.TempDir()
	noChangeTimeoutCh := make(chan struct{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		// briefDelivered and eventCh are nil: no brief gate, and no heartbeat
		// source, so the launch-verification and staleness branches stay off and
		// only a budget kill can fire.
		pasteInjectQuitOnCommit(ctx, clk, qk, qk, wtPath, "deadbeef",
			noChangeTimeoutCh, nil, nil, nil, core.RunID{})
	}()

	wallStart := time.Now()

	// rt19cOvershoot rather than a read of commitHardCeiling: this test
	// deliberately mutates NO package timing var. Every other pasteInjectQuitOnCommit
	// test rewrites those globals to millisecond values and restores them in
	// Cleanup, and a fake clock makes joining that scramble unnecessary — a single
	// oversized virtual jump clears any ceiling those globals can hold, at zero
	// wall cost. One jump also means the loop evaluates the hardDeadline check
	// (which is FIRST in the tick body) at a `now` already past every deadline, so
	// the hard-ceiling branch is the one taken, deterministically.
	clk.BlockUntil(1) // poll ticker armed
	clk.Advance(rt19cOvershoot)
	rt19cAwait(t, qk.quitSent, "/quit after the commit hard ceiling")

	clk.BlockUntil(2) // ticker + the noChange kill-delay sleeper
	clk.Advance(rt19cOvershoot)
	rt19cAwait(t, qk.killed, "Kill after the noChange kill delay")
	rt19cAwait(t, noChangeTimeoutCh, "noChangeTimeoutCh to close")
	rt19cAwait(t, done, "pasteInjectQuitOnCommit to return")

	if quits, kills := qk.counts(); quits != 1 || kills != 1 {
		t.Fatalf("quits=%d kills=%d; want exactly 1 each", quits, kills)
	}
	if wall := time.Since(wallStart); wall > rt19cWallBudget {
		t.Fatalf("drove a %v virtual budget in %v of REAL time; want well under %v",
			rt19cOvershoot, wall, rt19cWallBudget)
	}
}

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
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/runloop"
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

// rt19cBlockUntil is substrate.FakeClock.BlockUntil under the same rt19cWallBudget
// rt19cAwait enforces.
//
// The raw BlockUntil spins with NO deadline, which makes it the one place these
// tests can lose their own wall-clock assertion: if a converted site regresses to
// package time it never registers a FakeClock sleeper or ticker, the count never
// reaches n, and the test blocks until the whole test binary panics on its
// -timeout — taking every other internal/daemon test down with it. A partial
// regression should fail ONE test with the diagnostic below, not detonate the
// package, so the wait is raced against the budget here too.
func rt19cBlockUntil(t *testing.T, clk *substrate.FakeClock, n int, what string) {
	t.Helper()
	armed := make(chan struct{})
	go func() {
		clk.BlockUntil(n)
		close(armed)
	}()
	select {
	case <-armed:
	case <-time.After(rt19cWallBudget):
		t.Fatalf("timed out (%v of REAL time) waiting for %d FakeClock sleeper(s)/ticker(s) to arm (%s) — a converted site is probably still on the wall clock", rt19cWallBudget, n, what)
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
		errCh <- runloop.WaitPostAgentReadyProgress(context.Background(), clk, eventCh, timeout)
	}()

	wallStart := time.Now()
	// The hang bound is a one-shot clk.NewTicker, not a substrate.After sleeper —
	// RT19c's deliberate delta, because a ticker is the only ClockPort deadline
	// that can still be Stop()ped on an early return.
	rt19cBlockUntil(t, clk, 1, "the hang-bound ticker")
	clk.Advance(timeout + time.Second)

	select {
	case err := <-errCh:
		if !errors.Is(err, runloop.ErrPostAgentReadyHang) {
			t.Fatalf("waitPostAgentReadyProgress = %v; want ErrPostAgentReadyHang", err)
		}
	case <-time.After(rt19cWallBudget):
		t.Fatalf("waitPostAgentReadyProgress did not return within %v of REAL time", rt19cWallBudget)
	}

	if wall := time.Since(wallStart); wall > rt19cWallBudget {
		t.Fatalf("drove a %v virtual timeout in %v of REAL time; want well under %v", timeout, wall, rt19cWallBudget)
	}
}

// TestRT19cPostAgentReadyHang_NonPositiveDefaultStillErrors pins the one place
// RT19c's timer→ticker swap was NOT behaviour-preserving.
//
// waitPostAgentReadyProgress substitutes defaultPostAgentReadyHangTimeout for a
// non-positive caller timeout but did not re-check the substituted value — and
// that default is a MUTABLE package var, exposed to tests as
// ExportedDefaultPostAgentReadyHangTimeout and already rewritten by
// postreadyhang_hka2okh_test.go. The pre-RT19c time.NewTimer(0) fired
// immediately; time.NewTicker(0) PANICS, so a zero default turned a documented
// error return into a daemon panic (and, on a FakeClock, into a permanent block,
// since FakeClock.nextEventBefore only considers instants strictly after now).
//
// SystemClock deliberately: the panic is time.NewTicker's and only the real
// clock reaches it. The call runs in a goroutine with recover so a regression
// fails THIS test with a readable message instead of aborting the test binary.
func TestRT19cPostAgentReadyHang_NonPositiveDefaultStillErrors(t *testing.T) {
	// Deliberately NOT t.Parallel: this rewrites the same shared package var that
	// TestPostReadyHang_zeroTimeoutUsesDefault rewrites, and a sequential test
	// never overlaps this binary's parallel ones.
	orig := runloop.DefaultPostAgentReadyHangTimeout
	runloop.DefaultPostAgentReadyHangTimeout = 0
	t.Cleanup(func() { runloop.DefaultPostAgentReadyHangTimeout = orig })

	ctx, cancel := context.WithTimeout(context.Background(), rt19cWallBudget)
	defer cancel()
	eventCh := make(chan core.EventEnvelope) // never receives

	errCh := make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				errCh <- fmt.Errorf("waitPostAgentReadyProgress panicked: %v", r)
			}
		}()
		errCh <- runloop.WaitPostAgentReadyProgress(ctx, substrate.SystemClock{}, eventCh, 0)
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, runloop.ErrPostAgentReadyHang) {
			t.Fatalf("waitPostAgentReadyProgress with a zero default = %v; want ErrPostAgentReadyHang", err)
		}
	case <-time.After(rt19cWallBudget):
		t.Fatalf("waitPostAgentReadyProgress did not return within %v of REAL time with a zero default", rt19cWallBudget)
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

	rt19cBlockUntil(t, clk, 1, "the gate-verdict poll ticker")
	clk.Advance(verdictTimeout + pollInterval)
	rt19cAwait(t, qk.quitSent, "/quit after the gate-verdict timeout")

	rt19cBlockUntil(t, clk, 2, "the poll ticker plus the post-quit kill-grace sleeper")
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
	rt19cBlockUntil(t, clk, 1, "the commit poll ticker")
	clk.Advance(rt19cOvershoot)
	rt19cAwait(t, qk.quitSent, "/quit after the commit hard ceiling")

	rt19cBlockUntil(t, clk, 2, "the poll ticker plus the noChange kill-delay sleeper")
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

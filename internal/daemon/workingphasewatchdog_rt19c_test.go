package daemon

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/substrate"
)

var rt19cFakeClockEpoch = time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)

const rt19cWallBudget = 30 * time.Second

const rt19cOvershoot = 4 * time.Hour

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

func rt19cAwait(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(rt19cWallBudget):
		t.Fatalf("timed out (%v of REAL time) waiting for %s — a converted site is probably still on the wall clock", rt19cWallBudget, what)
	}
}

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

	verdictTimeout := gateFileTimeout
	pollInterval := gateFilePollInterval

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
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
		pasteInjectQuitOnCommit(ctx, clk, qk, qk, wtPath, "deadbeef",
			noChangeTimeoutCh, nil, nil, nil, core.RunID{})
	}()

	wallStart := time.Now()

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

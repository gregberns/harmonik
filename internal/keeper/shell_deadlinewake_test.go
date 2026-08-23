package keeper_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/keeper"
	"github.com/gregberns/harmonik/internal/substrate"
)

type delayedTickClock struct {
	substrate.SystemClock
	delayInterval time.Duration
	firstDelay    time.Duration
}

type delayedTicker struct {
	ch   chan time.Time
	stop chan struct{}
}

func (c *delayedTickClock) NewTicker(d time.Duration) substrate.Ticker {
	if d != c.delayInterval {
		return c.SystemClock.NewTicker(d)
	}
	t := &delayedTicker{ch: make(chan time.Time, 1), stop: make(chan struct{})}
	go func() {
		select {
		case <-time.After(c.firstDelay):
		case <-t.stop:
			return
		}
		tk := time.NewTicker(d)
		defer tk.Stop()
		select {
		case t.ch <- time.Now():
		default:
		}
		for {
			select {
			case at := <-tk.C:
				select {
				case t.ch <- at:
				default:
				}
			case <-t.stop:
				return
			}
		}
	}()
	return t
}

func (t *delayedTicker) C() <-chan time.Time { return t.ch }
func (t *delayedTicker) Stop()               { close(t.stop) }

func TestCycler_DelayedPollTick_HandoffTimeoutStaysPunctual(t *testing.T) {
	t.Skip("keeper-checkpoint-handshake: the deadline is now an observation wake, not an abort")
	t.Parallel()

	const (
		agent   = "deadline-wake-agent"
		cycleID = "cyc-deadline-wake"
		sid     = "sess-deadline-wake"
	)

	em := &keeper.RecordingEmitter{}
	spy := &cycleSpyInjector{}
	jc := &journalCapture{}

	noopGauge := func(_, _ string) (*keeper.CtxFile, time.Time, error) {
		return &keeper.CtxFile{Pct: 97.0, SessionID: sid}, time.Now(), nil
	}

	const (
		forceRetryInterval = 150 * time.Millisecond
		pollInterval       = 5 * time.Millisecond
		tickWithheldFor    = 200 * time.Millisecond
	)
	cfgOverrides := testCycleOverrides{CycleIDs:

	// First detection tick withheld until AFTER ForceRetryInterval: on the
	// pre-fix drive loop the handoff timeout is then detected only at
	// ~200ms, call-1's wall time crosses 150ms, and call-2 wrongly fires.

	func() string { return cycleID }, HandoffPath: func(_, a string) string { return "/tmp/HANDOFF-" + a + ".md" }, HandoffRead: handoffNeverReturnsNonce, HandoffScrub: // always abort
	func(_ string) error { return nil }, Inject:   spy.inject, Gauge: noopGauge, JournalWrite: jc.write}
	cfg := keeper.CyclerConfig{
		AgentName:          agent,
		ProjectDir:         t.TempDir(),
		TmuxTarget:         "fake-pane",
		ActPct:             90.0,
		WarnPct:            80.0,
		ForceActPct:        95.0,
		ForceRetryInterval: forceRetryInterval,
		HandoffTimeout:     30 * time.Millisecond,
		ClearSettle:        10 * time.Millisecond,
		PollInterval:       pollInterval,

		Clock: &delayedTickClock{delayInterval: pollInterval, firstDelay: tickWithheldFor},
	}
	cycler := mustNewCyclerWithOverridesAndBusyPane(cfg, em, cfgOverrides)

	cf := &keeper.CtxFile{Pct: 97.0, SessionID: sid}
	start := time.Now()
	if err := cycler.MaybeRun(context.Background(), cf); err != nil {
		t.Fatalf("MaybeRun #1: %v", err)
	}
	wall := time.Since(start)
	abortedAfter1 := len(em.EventsOfType(core.EventTypeSessionKeeperCycleAborted))
	if abortedAfter1 != 1 {
		t.Fatalf("want 1 cycle_aborted after first call; got %d", abortedAfter1)
	}

	if err := cycler.MaybeRun(context.Background(), cf); err != nil {
		t.Fatalf("MaybeRun #2 (immediate): %v", err)
	}
	abortedAfter2 := len(em.EventsOfType(core.EventTypeSessionKeeperCycleAborted))
	if abortedAfter2 != abortedAfter1 {
		t.Errorf("Gate 6 defeated: call-2 fired after call-1 wall=%s (poll tick withheld %s); want suppression within ForceRetryInterval=%s",
			wall, tickWithheldFor, forceRetryInterval)
	}
}

// TestCycler_ClearingElapsedBackstop_NoHotSpin is the hk-n8yha regression. The
// drive loop's punctual deadline wake is a repeating substrate.Ticker used as a
// one-shot. In the Clearing phase, pollClearing consults the wall-clock backstop
// ONLY when the settle window has also expired (it never cuts a settle window
// short). So when the backstop deadline is already elapsed while a settle window
// is still open — the exact condition the bead describes, reproduced here
// deterministically with ClearConfirmBackstop < ClearSettle — the nearest armed
// deadline is the elapsed backstop, nearestDeadline clamps its non-positive
// remaining to 1ns, and the pre-fix repeating NewTicker(1ns) re-fires every
// iteration while pollClearing no-ops, calling ReadGauge on every spin: a tight
// CPU + disk-read storm for a whole settle window.
//
// The fix makes the deadline wake fire at most once per generation, so detection
// falls back to the PollInterval ticker and the gauge is polled a bounded handful
// of times. This test asserts ReadGauge is called far below what a spin produces;
// a large safety-valve cap guarantees a regressed (spinning) build still
// terminates instead of hammering the gauge for the whole window.
func TestCycler_ClearingElapsedBackstop_NoHotSpin(t *testing.T) {
	t.Parallel()

	const (
		agent   = "backstop-spin-agent"
		cycleID = "cyc-backstop-spin"
		prevSID = "sess-before-spin"
		newSID  = "sess-after-spin"
	)

	em := &keeper.RecordingEmitter{}
	spy := &cycleSpyInjector{}
	jc := &journalCapture{}

	nonce := "<!-- KEEPER:" + cycleID + " -->"
	readHandoff := handoffReturnsNonceAfter(0, nonce) // nonce present immediately

	const spinCap = 100_000
	var gaugeMu sync.Mutex
	var gaugeCalls int
	readGaugeFn := func(_, _ string) (*keeper.CtxFile, time.Time, error) {
		gaugeMu.Lock()
		gaugeCalls++
		sid := prevSID
		if gaugeCalls > spinCap {
			sid = newSID // safety valve: end a regressed spin
		}
		gaugeMu.Unlock()
		return &keeper.CtxFile{Pct: 95.0, SessionID: sid}, time.Now(), nil
	}
	cfgOverrides := testCycleOverrides{CycleIDs:

	// Backstop deadline falls due almost immediately — long before the 60ms
	// settle window ends — so the whole settle window runs with the backstop
	// elapsed but not-yet-fired: nearestDeadline clamps its remaining to 1ns
	// and (pre-fix) the repeating deadline ticker spins.

	func() string { return cycleID }, HandoffPath: func(_, a string) string { return "/tmp/HANDOFF-" + a + ".md" }, HandoffRead: readHandoff, HandoffScrub: func(_ string) error { return nil }, Inject: spy.inject, Gauge: readGaugeFn, JournalWrite: jc.write}
	cfg := keeper.CyclerConfig{
		AgentName:      agent,
		ProjectDir:     t.TempDir(),
		TmuxTarget:     "fake-pane",
		ActPct:         90.0,
		WarnPct:        80.0,
		HandoffTimeout: 500 * time.Millisecond,
		ClearSettle:    60 * time.Millisecond,
		PollInterval:   10 * time.Millisecond,

		ClearConfirmBackstop: 50 * time.Microsecond,
		ClearConfirmRetries:  5,
	}
	cycler := mustNewCyclerWithOverrides(cfg, em, cfgOverrides)

	cf := &keeper.CtxFile{Pct: 95.0, SessionID: prevSID}
	if err := cycler.MaybeRun(context.Background(), cf); err != nil {
		t.Fatalf("MaybeRun: %v", err)
	}

	if got := len(em.EventsOfType(core.EventTypeSessionKeeperClearUnconfirmed)); got != 1 {
		t.Fatalf("clear_unconfirmed events = %d; want 1 (cycle should reach the backstop)", got)
	}

	gaugeMu.Lock()
	calls := gaugeCalls
	gaugeMu.Unlock()

	if calls >= 100 {
		t.Errorf("ReadGauge called %d times during one Clearing settle window; a one-shot deadline wake keeps this well under 100 — a count this high is the hk-n8yha 1ns-ticker hot-spin", calls)
	}
}

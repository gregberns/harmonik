package keeper_test

// Regression test for the T7 poll-quantized-timeout defect: the drive loop
// (shell.go) detected an armed timer's expiry only ON a detection-poll tick,
// so a scheduler-delayed tick stretched a forced-clear cycle's wall time past
// ForceRetryInterval and defeated Gate 6's suppression (a 3rd cycle_aborted
// in TestCycler_ForcedClear_RetryAfterInterval under full-package parallel
// load; T6's context.WithTimeout was punctual). The fix arms a dedicated
// deadline wake at the nearest armed deadline. This test forces the failure
// deterministically: the 5ms detection ticker's first tick is withheld until
// past ForceRetryInterval, so ONLY the punctual deadline wake can fire the
// 30ms handoff timeout on time and keep Gate 6 intact.

import (
	"context"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/keeper"
	"github.com/gregberns/harmonik/internal/substrate"
)

// delayedTickClock wraps SystemClock, except that a NewTicker whose interval
// equals delayInterval has its first tick withheld for firstDelay (simulating
// a scheduler-starved detection ticker under parallel-test contention).
// Tickers at any other interval — including the deadline wake — are real.
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

	// Margins are deliberately wide so THIS test stays robust under the same
	// full-package parallel contention it guards against: post-fix, call-1
	// aborts at ~30ms via the deadline wake, leaving ~120ms of tolerated
	// scheduler jitter before ForceRetryInterval; pre-fix, the withheld tick
	// quantizes the timeout to ~200ms, decisively past ForceRetryInterval.
	const (
		forceRetryInterval = 150 * time.Millisecond
		pollInterval       = 5 * time.Millisecond
		tickWithheldFor    = 200 * time.Millisecond
	)

	cfg := keeper.CyclerConfig{
		IdleMarkerModTimeFn: idleMarkerFreshNow, // Stop hook wired: model-done on first AwaitModelDone poll (T8)
		AgentName:           agent,
		ProjectDir:          t.TempDir(),
		TmuxTarget:          "fake-pane",
		ActPct:              90.0,
		WarnPct:             80.0,
		ForceActPct:         95.0,
		ForceRetryInterval:  forceRetryInterval,
		HandoffTimeout:      30 * time.Millisecond,
		ClearSettle:         10 * time.Millisecond,
		PollInterval:        pollInterval,
		// First detection tick withheld until AFTER ForceRetryInterval: on the
		// pre-fix drive loop the handoff timeout is then detected only at
		// ~200ms, call-1's wall time crosses 150ms, and call-2 wrongly fires.
		Clock:               &delayedTickClock{delayInterval: pollInterval, firstDelay: tickWithheldFor},
		CycleIDGen:          func() string { return cycleID },
		IsManagedFn:         func(_, _ string) bool { return true },
		HandoffFilePath:     func(_, a string) string { return "/tmp/HANDOFF-" + a + ".md" },
		ReadHandoff:         handoffNeverReturnsNonce, // always abort
		TruncateHandoffFn:   func(_ string) error { return nil },
		InjectFn:            spy.inject,
		ReadGaugeFn:         noopGauge,
		CrispIdleFn:         func(_, _ string) bool { return false },
		HoldingDispatchFn:   func(_, _ string) bool { return false },
		WriteJournalFn:      jc.write,
		SetTmuxEnvFn:        func(_ context.Context, _, _, _ string) error { return nil },
		SetManagedSessionFn: func(_, _, _ string) error { return nil },
	}
	cycler := keeper.NewCycler(cfg, em)

	// Call 1: fires (above force) and must abort on the PUNCTUAL 30ms handoff
	// timeout — the deadline wake, not the starved 65ms detection tick.
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

	// Call 2 (immediately): Gate 6 must still suppress. Pre-fix, the starved
	// tick quantized the timeout past ForceRetryInterval and this fired.
	if err := cycler.MaybeRun(context.Background(), cf); err != nil {
		t.Fatalf("MaybeRun #2 (immediate): %v", err)
	}
	abortedAfter2 := len(em.EventsOfType(core.EventTypeSessionKeeperCycleAborted))
	if abortedAfter2 != abortedAfter1 {
		t.Errorf("Gate 6 defeated: call-2 fired after call-1 wall=%s (poll tick withheld %s); want suppression within ForceRetryInterval=%s",
			wall, time.Duration(tickWithheldFor), forceRetryInterval)
	}
}

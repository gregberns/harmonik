package keeper_test

// cycle_postcycle_grace_hkpvrfx_test.go — the hk-pvrfx post-cycle grace at the
// SHELL level: the same defect exercised end-to-end through Cycler.MaybeRun
// (gauge ladder → cycle → terminal → next MaybeRun), not just the pure reactor.
//
// The reactor-level cases (act/force bands, the abort terminal, the
// legitimate-new-session guard) live in step_postcycle_grace_hkpvrfx_test.go.

import (
	"context"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/keeper"
)

// TestCycler_PostCycleGrace_NoImmediateRefireAfterTerminal drives the chani
// shape through the live MaybeRun path: a cycle that entered BELOW the force
// threshold (so LastForcedAttemptAt is never stamped) reaches a terminal, and
// the very next gauge reading — same session id, now above the force threshold
// — must NOT open another cycle. It must still fire once ForceRetryInterval has
// elapsed since that terminal.
func TestCycler_PostCycleGrace_NoImmediateRefireAfterTerminal(t *testing.T) {
	t.Parallel()

	const (
		agent = "chani"
		sid   = "sess-chani"
		// ActPct < 92 < ForceActPct: entry never stamps LastForcedAttemptAt.
		entryPct = 92.0
		// >= ForceActPct: the force-retry escape hatch is in play on the re-fire.
		forcePct = 97.0
		// Large in VIRTUAL time so it dwarfs the virtual time the abort drive
		// loop consumes (same deflake shape as TestCycler_ForcedClear_RetryAfterInterval).
		forceRetryInterval = 5 * time.Second
	)

	em := &keeper.RecordingEmitter{}
	spy := &cycleSpyInjector{}
	jc := &journalCapture{}
	clock := newSteppingAdvanceClock(time.Unix(1_700_000_000, 0), 5*time.Millisecond)

	cfg := keeper.CyclerConfig{
		Clock:               clock,
		IdleMarkerModTimeFn: idleMarkerFreshNow,
		AgentName:           agent,
		ProjectDir:          t.TempDir(),
		TmuxTarget:          "fake-pane",
		ActPct:              90.0,
		WarnPct:             80.0,
		ForceActPct:         95.0,
		ForceRetryInterval:  forceRetryInterval,
		HandoffTimeout:      30 * time.Millisecond, // short → deterministic abort terminal
		ClearSettle:         10 * time.Millisecond,
		PollInterval:        5 * time.Millisecond,
		CycleIDGen:          func() string { return "cyc-pcg" },
		IsManagedFn:         func(_, _ string) bool { return true },
		HandoffFilePath:     func(_, a string) string { return "/tmp/HANDOFF-" + a + ".md" },
		ReadHandoff:         handoffNeverReturnsNonce,
		TruncateHandoffFn:   func(_ string) error { return nil },
		InjectFn:            spy.inject,
		ReadGaugeFn: func(_, _ string) (*keeper.CtxFile, time.Time, error) {
			return &keeper.CtxFile{Pct: forcePct, SessionID: sid}, clock.Now(), nil
		},
		CrispIdleFn:         func(_, _ string) bool { return true },
		HoldingDispatchFn:   func(_, _ string) bool { return false },
		WriteJournalFn:      jc.write,
		SetTmuxEnvFn:        func(_ context.Context, _, _, _ string) error { return nil },
		SetManagedSessionFn: func(_, _, _ string) error { return nil },
	}
	cycler := keeper.NewCycler(cfg, em)
	ctx := context.Background()

	started := func() int {
		return len(em.EventsOfType(core.EventTypeSessionKeeperHandoffStarted))
	}

	// Call 1: 92% (above act, below force) → fires, then aborts on the nonce
	// timeout. LastForcedAttemptAt is NOT stamped (entry was below force).
	if err := cycler.MaybeRun(ctx, &keeper.CtxFile{Pct: entryPct, SessionID: sid}); err != nil {
		t.Fatalf("MaybeRun #1: %v", err)
	}
	if n := started(); n != 1 {
		t.Fatalf("want 1 handoff_started after the first call; got %d", n)
	}

	// Call 2, immediately after the terminal, same session id, now above the
	// force threshold: MUST be suppressed by the post-cycle grace. Pre-fix the
	// zero LastForcedAttemptAt read as "not suppressed" and this re-fired into a
	// session that was still booting.
	if err := cycler.MaybeRun(ctx, &keeper.CtxFile{Pct: forcePct, SessionID: sid}); err != nil {
		t.Fatalf("MaybeRun #2 (immediate re-fire): %v", err)
	}
	if n := started(); n != 1 {
		t.Errorf("want still 1 handoff_started immediately after the terminal; got %d (post-cycle grace did not hold)", n)
	}

	// Cross ForceRetryInterval in virtual time (no real sleep).
	clock.Advance(forceRetryInterval + 10*time.Millisecond)

	// Call 3: the grace has expired — a session genuinely stuck above the force
	// threshold must still get its retry (the hk-220lv direction).
	if err := cycler.MaybeRun(ctx, &keeper.CtxFile{Pct: forcePct, SessionID: sid}); err != nil {
		t.Fatalf("MaybeRun #3 (after interval): %v", err)
	}
	if n := started(); n != 2 {
		t.Errorf("want 2 handoff_started once the grace elapsed; got %d (the keeper must never wedge)", n)
	}
}

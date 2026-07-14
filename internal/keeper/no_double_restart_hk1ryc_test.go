package keeper_test

// no_double_restart_hk1ryc_test.go — hk-1ryc.
//
// Two contracts are pinned here:
//
//  1. The keeper's automatic act/force cycle does NOT race (double-restart) the
//     agent's own `harmonik keeper restart-now` self-service restart. The
//     suppression mechanism is the IMPLICIT gauge signal, NOT a cross-process
//     marker file (the hk-5da7 lesson: a `.restarted` marker written by the CLI
//     under a different project dir than the watcher polled silently never
//     suppressed). `harmonik keeper restart-now` runs synchronously and injects
//     /clear in-process; that drops the gauge below the act threshold, so the
//     Cycler's Gate-3 (belowActThreshold) suppresses the auto-cycle on the very
//     next tick. This test simulates warn → restart-now → /clear → gauge drops
//     below act, and asserts the Cycler does NOT fire a second cycle (no second
//     /clear) on the next tick.
//
//  2. The operator-attached guard on the WARN-inject path (mirroring the Cycler's
//     act-path Gate-7). When a human operator is attached to the pane mid-keystroke
//     the ACTIONABLE self-service restart instruction is NOT selected — the lighter
//     finish-the-turn advisory is used instead, so the warn is still delivered but
//     the self-restart command is never injected over an operator's in-flight turn.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/keeper"
)

// gaugeBelowActAfterClear models the gauge a restart-now /clear produces: the
// first tick reads high context (the cycle that armed the warn fired), then after
// the synchronous restart-now /clear the gauge reads a small post-clear context
// for every subsequent tick. The Cycler's ReadGaugeFn is only consulted INSIDE a
// running cycle (waitForNewSessionID); the per-tick MaybeRun reads the CtxFile the
// caller passes, so the test drives the level via the cf argument and uses this
// only to keep the cycle internals satisfied.
func gaugeBelowActAfterClear(sid string) func(string, string) (*keeper.CtxFile, time.Time, error) {
	return func(_, _ string) (*keeper.CtxFile, time.Time, error) {
		return &keeper.CtxFile{Tokens: 40_000, WindowSize: 200_000, Pct: 20, SessionID: sid}, time.Now(), nil
	}
}

// TestCycler_RestartNowDropsGauge_NoSecondCycle proves the no-double-restart
// contract: after a self-service restart-now drops the gauge below the act
// threshold, the next MaybeRun tick is suppressed by Gate-3 (belowActThreshold)
// and fires NO second cycle — without any cross-process marker file.
func TestCycler_RestartNowDropsGauge_NoSecondCycle(t *testing.T) {
	t.Parallel()

	const (
		agent   = "no-double-agent"
		cycleID = "cyc-no-double"
		sid     = "11111111-2222-4333-8444-000000000001"
	)

	em := &keeper.RecordingEmitter{}
	spy := &cycleSpyInjector{}
	jc := &journalCapture{}

	nonce := "<!-- KEEPER:" + cycleID + " -->"
	alwaysNonce := func(_ string) (string, error) { return "# Handoff\n\n" + nonce + "\n", nil }

	cfg := keeper.CyclerConfig{
		IdleMarkerModTimeFn: idleMarkerFreshNow, // Stop hook wired: model-done on first AwaitModelDone poll (T8)
		AgentName:           agent,
		ProjectDir:          t.TempDir(),
		TmuxTarget:          "fake-pane",
		ActAbsTokens:        200_000, // act threshold (abs-token path active: cf has Tokens+WindowSize)
		WarnAbsTokens:       170_000,
		HandoffTimeout:      200 * time.Millisecond,
		ClearSettle:         20 * time.Millisecond,
		PollInterval:        5 * time.Millisecond,
		// This test's gauge fake never rotates the session_id (it models a
		// same-session restart-now, not a /clear-confirmation race), so the
		// hk-vdqe2 hard-gate retry loop would otherwise keep re-injecting /clear
		// for the full default backstop. Pin it to a single attempt — this test
		// is about Gate-3 double-restart suppression, not clear confirmation.
		ClearConfirmRetries: 1,
		CycleIDGen:          func() string { return cycleID },
		IsManagedFn:         func(_, _ string) bool { return true },
		HandoffFilePath: func(_, a string) string {
			return "/tmp/HANDOFF-" + a + ".md"
		},
		ReadHandoff:        alwaysNonce,
		TruncateHandoffFn:  func(_ string) error { return nil },
		InjectFn:           spy.inject,
		ReadGaugeFn:        gaugeBelowActAfterClear(sid),
		CrispIdleFn:        func(_, _ string) bool { return true },
		HoldingDispatchFn:  func(_, _ string) bool { return false },
		WriteJournalFn:     jc.write,
		SetTmuxEnvFn:       func(_ context.Context, _, _, _ string) error { return nil },
		OperatorAttachedFn: func(_ string) bool { return false },
	}
	cycler := keeper.NewCycler(cfg, em)
	ctx := context.Background()

	// Tick 1: context is ABOVE the act threshold and the agent has NOT yet
	// self-restarted — the keeper auto-cycle would fire here. We let it fire so the
	// scenario is "auto-cycle already happened once". (In production the warn-inject
	// nudges the agent to self-restart; here we just establish one cycle ran.)
	high := &keeper.CtxFile{Tokens: 210_000, WindowSize: 200_000, Pct: 95, SessionID: sid}
	if err := cycler.MaybeRun(ctx, high); err != nil {
		t.Fatalf("MaybeRun(high): %v", err)
	}
	clearsAfterFirst := countClears(spy.texts())
	if clearsAfterFirst != 1 {
		t.Fatalf("setup: want exactly 1 /clear from the first cycle; got %d (%v)", clearsAfterFirst, spy.texts())
	}

	// The agent now runs `harmonik keeper restart-now`, which synchronously injects
	// /clear and drops the gauge below the act threshold. Tick 2 reads that dropped
	// gauge — same session_id, low context.
	low := &keeper.CtxFile{Tokens: 40_000, WindowSize: 200_000, Pct: 20, SessionID: sid}
	if err := cycler.MaybeRun(ctx, low); err != nil {
		t.Fatalf("MaybeRun(low): %v", err)
	}

	// Gate-3 (belowActThreshold) must have suppressed a SECOND cycle: no new /clear.
	if got := countClears(spy.texts()); got != 1 {
		t.Fatalf("no-double-restart violated: want still 1 /clear after restart-now dropped the gauge; got %d (%v)", got, spy.texts())
	}
	// Exactly one cycle_complete total — the auto-cycle did not double-fire.
	if evts := em.EventsOfType(core.EventTypeSessionKeeperCycleComplete); len(evts) != 1 {
		t.Fatalf("want exactly 1 cycle_complete (no double restart); got %d", len(evts))
	}
}

// countClears counts how many injected texts are exactly "/clear".
func countClears(texts []string) int {
	n := 0
	for _, tx := range texts {
		if strings.TrimSpace(tx) == "/clear" {
			n++
		}
	}
	return n
}

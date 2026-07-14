package keeper_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/keeper"
)

// TestCycler_EmptyTarget_StaleHandoff_Aborts is a T7 parity regression
// (hk-fi78d): with an EMPTY tmux target no handoff command is injected, so the
// freshness-recovery anchor (handoffInjectedAt) must still be stamped per
// firing cycle — the pre-rebuild runCycle stamped it UNCONDITIONALLY before the
// `if TmuxTarget != ""` injection branch.
//
// A pre-existing, STALE handoff (real content, mtime well before the cycle
// starts) must therefore read as NOT-fresh at handoff-timeout, so the cycle
// ABORTS (cycle_aborted, LastFireWasAbort=true, never /clear). Without the
// per-cycle anchor the sampler would compare the stale mtime against a zero
// anchor, judge the stale handoff "fresh", and wrongly take the RECOVERY path
// (cycle_recovered + clear_unconfirmed + cycle_complete) — flipping subsequent
// same-SID escape-hatch behavior. This test fails on that regression.
func TestCycler_EmptyTarget_StaleHandoff_Aborts(t *testing.T) {
	t.Parallel()

	const (
		agent   = "empty-target-agent"
		cycleID = "cyc-empty-001"
		sid     = "sess-empty"
	)

	// mtime well in the past → strictly before the cycle's injection instant.
	staleMtime := time.Now().Add(-1 * time.Hour)

	em := &keeper.RecordingEmitter{}
	spy := &cycleSpyInjector{}
	jc := &journalCapture{}

	gauge := func(_, _ string) (*keeper.CtxFile, time.Time, error) {
		return &keeper.CtxFile{Pct: 95.0, SessionID: sid}, time.Now(), nil
	}

	cfg := keeper.CyclerConfig{
		AgentName:      agent,
		ProjectDir:     t.TempDir(),
		TmuxTarget:     "", // the crux: no live tmux session → no injection
		ActPct:         90.0,
		WarnPct:        80.0,
		HandoffTimeout: 60 * time.Millisecond,
		ClearSettle:    30 * time.Millisecond,
		PollInterval:   10 * time.Millisecond,
		CycleIDGen:     func() string { return cycleID },
		IsManagedFn:    func(_, _ string) bool { return true },
		HandoffFilePath: func(_, a string) string {
			return "/tmp/HANDOFF-" + a + ".md"
		},
		// Real content but NO current-cycle nonce → the poll never confirms and
		// the cycle runs to the handoff timeout.
		ReadHandoff:       func(_ string) (string, error) { return "stale handoff body from a prior cycle\n", nil },
		HandoffModTimeFn:  func(_ string) (time.Time, bool) { return staleMtime, true },
		TruncateHandoffFn: func(_ string) error { return nil },
		InjectFn:          spy.inject,
		ReadGaugeFn:       gauge,
		CrispIdleFn:       func(_, _ string) bool { return true },
		HoldingDispatchFn: func(_, _ string) bool { return false },
		WriteJournalFn:    jc.write,
		SetTmuxEnvFn:      func(_ context.Context, _, _, _ string) error { return nil },
	}
	cycler := keeper.NewCycler(cfg, em)

	cf := &keeper.CtxFile{Pct: 95.0, SessionID: sid}
	if err := cycler.MaybeRun(context.Background(), cf); err != nil {
		t.Fatalf("MaybeRun: %v", err)
	}

	// (a) Journal must end in "aborted", not the recovery-path "complete".
	phases := jc.snapshot()
	if len(phases) == 0 {
		t.Fatal("no journal phases recorded — cycle did not fire")
	}
	if last := phases[len(phases)-1]; last != "aborted" {
		t.Errorf("last journal phase = %q; want \"aborted\" (stale handoff must not read as fresh)", last)
	}

	// (b) cycle_aborted emitted with the handoff_timeout reason.
	abortedEvts := em.EventsOfType(core.EventTypeSessionKeeperCycleAborted)
	if len(abortedEvts) != 1 {
		t.Errorf("want 1 cycle_aborted; got %d", len(abortedEvts))
	} else {
		var p core.SessionKeeperCycleAbortedPayload
		if err := json.Unmarshal(abortedEvts[0].Payload, &p); err != nil {
			t.Fatalf("unmarshal cycle_aborted: %v", err)
		}
		if p.Reason != "handoff_timeout" {
			t.Errorf("cycle_aborted.reason = %q; want \"handoff_timeout\"", p.Reason)
		}
	}

	// (c) The recovery path must NOT have been taken.
	if n := len(em.EventsOfType(core.EventTypeSessionKeeperCycleRecovered)); n != 0 {
		t.Errorf("want 0 cycle_recovered (stale handoff is not a recovery); got %d", n)
	}
	if n := len(em.EventsOfType(core.EventTypeSessionKeeperClearUnconfirmed)); n != 0 {
		t.Errorf("want 0 clear_unconfirmed on abort; got %d", n)
	}
	if n := len(em.EventsOfType(core.EventTypeSessionKeeperCycleComplete)); n != 0 {
		t.Errorf("want 0 cycle_complete on abort; got %d", n)
	}
}

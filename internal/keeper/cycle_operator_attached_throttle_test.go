package keeper_test

// cycle_operator_attached_throttle_test.go — -short unit tests for the
// poll-tick throttle on session_keeper_operator_attached emission (hk-2yvx,
// logmine F55). Gate 7 re-checks live tmux on every watcher tick (~5s). Before
// the throttle, every tick while an operator stayed attached persisted a durable
// operator_attached event — 51% of one observed events.jsonl window. The cycle
// path must now sample those emissions (default once per minute) instead of
// writing one per poll tick. The transient gauge read is not worth a durable
// event on every tick.

import (
	"context"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/keeper"
)

// newAttachTestCyclerSample mirrors newAttachTestCycler but threads a custom
// OperatorAttachedSampleInterval so the re-emit-after-window behaviour can be
// exercised without sleeping the full default minute.
func newAttachTestCyclerSample(
	agent, projectDir, cycleID string,
	em keeper.Emitter,
	spy *cycleSpyInjector,
	jc *journalCapture,
	readHandoff func(string) (string, error),
	readGaugeFn func(string, string) (*keeper.CtxFile, time.Time, error),
	attachFn func(string) bool,
	sample time.Duration,
) *keeper.Cycler {
	cfg := keeper.CyclerConfig{
		AgentName:      agent,
		ProjectDir:     projectDir,
		TmuxTarget:     "fake-pane",
		ActPct:         90.0,
		WarnPct:        80.0,
		HandoffTimeout: 200 * time.Millisecond,
		ClearSettle:    50 * time.Millisecond,
		PollInterval:   5 * time.Millisecond,
		CycleIDGen:     func() string { return cycleID },
		IsManagedFn:    func(_, _ string) bool { return true },
		HandoffFilePath: func(_, a string) string {
			return "/tmp/HANDOFF-" + a + ".md"
		},
		ReadHandoff:                    readHandoff,
		TruncateHandoffFn:              func(_ string) error { return nil },
		InjectFn:                       spy.inject,
		ReadGaugeFn:                    readGaugeFn,
		CrispIdleFn:                    func(_, _ string) bool { return true },
		HoldingDispatchFn:              func(_, _ string) bool { return false },
		WriteJournalFn:                 jc.write,
		AppendHandoffFn:                func(_, _ string) error { return nil },
		SetTmuxEnvFn:                   func(_ context.Context, _, _, _ string) error { return nil },
		OperatorAttachedFn:             attachFn,
		OperatorAttachedSampleInterval: sample,
	}
	return keeper.NewCycler(cfg, em)
}

// TestCycler_OperatorAttached_ThrottledAcrossTicks verifies that many MaybeRun
// ticks while the operator stays attached collapse to a SINGLE operator_attached
// event within the sample window — not one event per tick.
func TestCycler_OperatorAttached_ThrottledAcrossTicks(t *testing.T) {
	t.Parallel()

	const (
		agent   = "attach-throttle-agent"
		cycleID = "cyc-attach-throttle"
		sid     = "sess-throttle"
	)

	em := &keeper.RecordingEmitter{}
	spy := &cycleSpyInjector{}
	jc := &journalCapture{}
	attach := &fakeAttach{attached: true}

	nonce := "<!-- KEEPER:" + cycleID + " -->"
	alwaysNonce := func(_ string) (string, error) { return "# Handoff\n\n" + nonce + "\n", nil }
	noopGauge := func(_, _ string) (*keeper.CtxFile, time.Time, error) {
		return &keeper.CtxFile{Pct: 95.0, SessionID: sid}, time.Now(), nil
	}

	cycler := newAttachTestCycler(agent, t.TempDir(), cycleID, em, spy, jc, alwaysNonce, noopGauge, attach.fn)

	cf := &keeper.CtxFile{Pct: 95.0, SessionID: sid}
	// Simulate a burst of ~5s poll ticks (all well within the default 1-minute
	// sample window).
	for i := 0; i < 8; i++ {
		if err := cycler.MaybeRun(context.Background(), cf); err != nil {
			t.Fatalf("MaybeRun tick %d: %v", i, err)
		}
	}

	// No injection ever happened (warn-only the whole time).
	if n := len(spy.texts()); n != 0 {
		t.Errorf("want 0 inject calls while operator attached; got %d", n)
	}
	// Exactly ONE operator_attached event despite 8 ticks — the rest are throttled.
	if oa := em.EventsOfType(core.EventTypeSessionKeeperOperatorAttached); len(oa) != 1 {
		t.Fatalf("want 1 throttled operator_attached event across 8 ticks; got %d", len(oa))
	}
}

// TestCycler_OperatorAttached_ReEmitsAfterInterval verifies the throttle is a
// SAMPLE, not a one-shot: once the sample interval elapses, a still-attached
// session emits again so the digest resolver's attached-source stays fresh.
func TestCycler_OperatorAttached_ReEmitsAfterInterval(t *testing.T) {
	t.Parallel()

	const (
		agent   = "attach-reemit-agent"
		cycleID = "cyc-attach-reemit"
		sid     = "sess-reemit"
	)

	em := &keeper.RecordingEmitter{}
	spy := &cycleSpyInjector{}
	jc := &journalCapture{}
	attach := &fakeAttach{attached: true}

	nonce := "<!-- KEEPER:" + cycleID + " -->"
	alwaysNonce := func(_ string) (string, error) { return "# Handoff\n\n" + nonce + "\n", nil }
	noopGauge := func(_, _ string) (*keeper.CtxFile, time.Time, error) {
		return &keeper.CtxFile{Pct: 95.0, SessionID: sid}, time.Now(), nil
	}

	cycler := newAttachTestCyclerSample(agent, t.TempDir(), cycleID, em, spy, jc, alwaysNonce, noopGauge, attach.fn, 20*time.Millisecond)

	cf := &keeper.CtxFile{Pct: 95.0, SessionID: sid}

	// First tick emits.
	if err := cycler.MaybeRun(context.Background(), cf); err != nil {
		t.Fatalf("MaybeRun tick 1: %v", err)
	}
	// Same-window tick is throttled.
	if err := cycler.MaybeRun(context.Background(), cf); err != nil {
		t.Fatalf("MaybeRun tick 2: %v", err)
	}
	if oa := em.EventsOfType(core.EventTypeSessionKeeperOperatorAttached); len(oa) != 1 {
		t.Fatalf("want 1 event before interval elapses; got %d", len(oa))
	}

	// Let the sample window elapse, then tick again while still attached.
	time.Sleep(30 * time.Millisecond)
	if err := cycler.MaybeRun(context.Background(), cf); err != nil {
		t.Fatalf("MaybeRun tick 3: %v", err)
	}
	if oa := em.EventsOfType(core.EventTypeSessionKeeperOperatorAttached); len(oa) != 2 {
		t.Fatalf("want 2 events after sample interval elapses; got %d", len(oa))
	}
}

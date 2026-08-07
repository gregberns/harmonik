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
		IdleMarkerModTimeFn: idleMarkerFreshNow, // Stop hook wired: model-done on first AwaitModelDone poll (T8)
		AgentName:           agent,
		ProjectDir:          projectDir,
		TmuxTarget:          "fake-pane",
		ActPct:              90.0,
		WarnPct:             80.0,
		HandoffTimeout:      200 * time.Millisecond,
		ClearSettle:         50 * time.Millisecond,
		PollInterval:        5 * time.Millisecond,
		CycleIDGen:          func() string { return cycleID },
		IsManagedFn:         func(_, _ string) bool { return true },
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
		SetTmuxEnvFn:                   func(_ context.Context, _, _, _ string) error { return nil },
		OperatorAttachedFn:             attachFn,
		OperatorAttachedSampleInterval: sample,
	}
	return keeper.NewCycler(cfg, em)
}

// TestCycler_OperatorAttached_ThrottledAcrossTicks is DISABLED. It cannot fail.
//
// Its name and the comment this replaced both sell throttle coverage: many ticks
// collapsing to a single operator_attached event. Its assertion is that the event
// fired ZERO times, and nothing in the keeper can emit that event — every keeper
// emission is an ActEmit action in step.go and there is none for this type. So
// the test has been green since it was written and always will be.
//
// Skipped rather than deleted, per the operator's 2026-08-06 rule: a test that
// cannot fail should not cost a run, and must not disappear either. A skip is
// named in tools/testreport's NOT RUN section on EVERY run, including a green
// one, so this stays in front of whoever reads the next report.
//
// Do not re-enable by making the assertion pass. Answer hk-61urw first: decide
// whether the keeper should emit this event at all. If it should not, delete this
// test and the dead attached-source branch in internal/digest/resolver.go. If it
// should, this test is a specification waiting for an implementation.
func TestCycler_OperatorAttached_ThrottledAcrossTicks(t *testing.T) {
	t.Skip("hk-61urw: asserts an event the keeper cannot emit; green since written, cannot fail")
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
	// operator_attached is no longer persisted (logmine TA3 / finish F55).
	if oa := em.EventsOfType(core.EventTypeSessionKeeperOperatorAttached); len(oa) != 0 {
		t.Errorf("want 0 operator_attached events (non-durable since TA3); got %d", len(oa))
	}
}

// TestCycler_OperatorAttached_ReEmitsAfterInterval is DISABLED, for the same
// reason as the throttle test above and with the same terms. Its name promises
// that a still-attached session emits again once the sample interval elapses.
// Its assertion is that the event fired zero times, and nothing can emit it.
//
// See the note on TestCycler_OperatorAttached_ThrottledAcrossTicks. hk-61urw
// owns the decision that lets either test come back.
func TestCycler_OperatorAttached_ReEmitsAfterInterval(t *testing.T) {
	t.Skip("hk-61urw: asserts an event the keeper cannot emit; green since written, cannot fail")
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
	// operator_attached is no longer persisted (logmine TA3 / finish F55).
	if oa := em.EventsOfType(core.EventTypeSessionKeeperOperatorAttached); len(oa) != 0 {
		t.Errorf("want 0 operator_attached events before interval (non-durable since TA3); got %d", len(oa))
	}

	// Let the sample window elapse, then tick again while still attached.
	time.Sleep(30 * time.Millisecond)
	if err := cycler.MaybeRun(context.Background(), cf); err != nil {
		t.Fatalf("MaybeRun tick 3: %v", err)
	}
	// Still 0 — emission is permanently suppressed.
	if oa := em.EventsOfType(core.EventTypeSessionKeeperOperatorAttached); len(oa) != 0 {
		t.Errorf("want 0 operator_attached events after interval (non-durable since TA3); got %d", len(oa))
	}
}

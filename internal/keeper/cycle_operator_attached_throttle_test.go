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

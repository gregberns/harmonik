package keeper_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/keeper"
)

type fakeAttach struct {
	mu       sync.Mutex
	attached bool
	calls    int
}

func (f *fakeAttach) fn(_ string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.attached
}

func (f *fakeAttach) set(v bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.attached = v
}

func newAttachTestCycler(
	agent, projectDir, cycleID string,
	em keeper.Emitter,
	spy *cycleSpyInjector,
	jc *journalCapture,
	readHandoff func(string) (string, error),
	readGaugeFn func(string, string) (*keeper.CtxFile, time.Time, error),
	attachFn func(string) bool,
) *keeper.Cycler {
	cfgOverrides := testCycleOverrides{CycleIDs: func() string { return cycleID }, HandoffPath: func(_, a string) string {
		return "/tmp/HANDOFF-" + a + ".md"
	}, HandoffRead: readHandoff, HandoffScrub: func(_ string) error { return nil }, Inject: spy.inject, Gauge: readGaugeFn, JournalWrite: jc.write}
	cfg := keeper.CyclerConfig{
		AgentName:      agent,
		ProjectDir:     projectDir,
		TmuxTarget:     "fake-pane",
		ActPct:         90.0,
		WarnPct:        80.0,
		HandoffTimeout: 200 * time.Millisecond,
		ClearSettle:    50 * time.Millisecond,
		PollInterval:   5 * time.Millisecond,
	}
	return mustNewCyclerWithOverridesAndDeps(cfg, em, cfgOverrides, func(deps *keeper.CycleDeps) {
		deps.Operator = testOperatorProbe(attachFn)
	})
}

// TestCycler_OperatorAttached_SuppressesInjection verifies that when the
// operator is attached, MaybeRun suppresses ALL injection (warn-only) and emits
// session_keeper_operator_attached instead of running the cycle.
func TestCycler_OperatorAttached_SuppressesInjection(t *testing.T) {
	t.Parallel()

	const (
		agent   = "attach-suppress-agent"
		cycleID = "cyc-attach-suppress"
		sid     = "sess-attached"
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
	if err := cycler.MaybeRun(context.Background(), cf); err != nil {
		t.Fatalf("MaybeRun: %v", err)
	}

	if n := len(spy.texts()); n != 0 {
		t.Errorf("want 0 inject calls while operator attached; got %d: %v", n, spy.texts())
	}
	if evts := em.EventsOfType(core.EventTypeSessionKeeperHandoffStarted); len(evts) != 0 {
		t.Errorf("want 0 handoff_started while operator attached; got %d", len(evts))
	}
	if evts := em.EventsOfType(core.EventTypeSessionKeeperOperatorAttached); len(evts) != 0 {
		t.Errorf("want 0 operator_attached events (non-durable since TA3); got %d", len(evts))
	}
	if attach.calls == 0 {
		t.Error("OperatorAttachedFn was never consulted")
	}
}

// TestCycler_OperatorDetached_Proceeds verifies the not-attached path behaves
// exactly as before: the full cycle runs (injection proceeds, cycle_complete).
func TestCycler_OperatorDetached_Proceeds(t *testing.T) {
	t.Parallel()

	const (
		agent   = "attach-proceed-agent"
		cycleID = "cyc-attach-proceed"
		prevSID = "sess-before"
		newSID  = "sess-after"
	)

	em := &keeper.RecordingEmitter{}
	spy := &cycleSpyInjector{}
	jc := &journalCapture{}
	attach := &fakeAttach{attached: false} // operator NOT attached

	nonce := "<!-- KEEPER:" + cycleID + " -->"
	readHandoff := handoffReturnsNonceAfter(1, nonce)
	readGaugeFn := gaugeReturnsNewSIDAfter(1, prevSID, newSID)

	cycler := newAttachTestCycler(agent, t.TempDir(), cycleID, em, spy, jc, readHandoff, readGaugeFn, attach.fn)

	cf := &keeper.CtxFile{Pct: 95.0, SessionID: prevSID}
	if err := cycler.MaybeRun(context.Background(), cf); err != nil {
		t.Fatalf("MaybeRun: %v", err)
	}

	texts := spy.texts()
	if len(texts) < 3 {
		t.Fatalf("want >=3 inject calls when detached; got %d: %v", len(texts), texts)
	}
	if evts := em.EventsOfType(core.EventTypeSessionKeeperCycleComplete); len(evts) != 1 {
		t.Errorf("want 1 cycle_complete when detached; got %d", len(evts))
	}
	if evts := em.EventsOfType(core.EventTypeSessionKeeperOperatorAttached); len(evts) != 0 {
		t.Errorf("want 0 operator_attached when detached; got %d", len(evts))
	}
}

// TestCycler_OperatorDetachThenResume verifies the detach TRANSITION: a first
// MaybeRun while attached is suppressed (warn-only); after the operator detaches
// a second MaybeRun on the SAME session proceeds and completes the cycle.
func TestCycler_OperatorDetachThenResume(t *testing.T) {
	t.Parallel()

	const (
		agent   = "attach-transition-agent"
		cycleID = "cyc-attach-transition"
		prevSID = "sess-trans-before"
		newSID  = "sess-trans-after"
	)

	em := &keeper.RecordingEmitter{}
	spy := &cycleSpyInjector{}
	jc := &journalCapture{}
	attach := &fakeAttach{attached: true} // attached at first

	nonce := "<!-- KEEPER:" + cycleID + " -->"
	alwaysNonce := func(_ string) (string, error) { return "# Handoff\n\n" + nonce + "\n", nil }
	readGaugeFn := gaugeReturnsNewSIDAfter(1, prevSID, newSID)

	cycler := newAttachTestCycler(agent, t.TempDir(), cycleID, em, spy, jc, alwaysNonce, readGaugeFn, attach.fn)

	cf := &keeper.CtxFile{Pct: 95.0, SessionID: prevSID}

	if err := cycler.MaybeRun(context.Background(), cf); err != nil {
		t.Fatalf("MaybeRun(attached): %v", err)
	}
	if n := len(spy.texts()); n != 0 {
		t.Fatalf("want 0 inject calls on attached tick; got %d", n)
	}
	if evts := em.EventsOfType(core.EventTypeSessionKeeperOperatorAttached); len(evts) != 0 {
		t.Errorf("want 0 operator_attached (non-durable since TA3); got %d", len(evts))
	}

	attach.set(false)

	if err := cycler.MaybeRun(context.Background(), cf); err != nil {
		t.Fatalf("MaybeRun(detached): %v", err)
	}
	texts := spy.texts()
	if len(texts) < 3 {
		t.Fatalf("want >=3 inject calls after detach; got %d: %v", len(texts), texts)
	}
	if evts := em.EventsOfType(core.EventTypeSessionKeeperCycleComplete); len(evts) != 1 {
		t.Errorf("want 1 cycle_complete after detach; got %d", len(evts))
	}
	if evts := em.EventsOfType(core.EventTypeSessionKeeperOperatorAttached); len(evts) != 0 {
		t.Errorf("want 0 operator_attached total (non-durable since TA3); got %d", len(evts))
	}
}

// TestCycler_Precompact_OperatorAttached_Suppresses verifies the PreCompact
// act-path also honours the guard: when attached, RunForPrecompact suppresses
// the cycle, emits operator_attached, and STILL clears the precompact marker
// (bounded-fallback contract).
func TestCycler_Precompact_OperatorAttached_Suppresses(t *testing.T) {
	t.Parallel()

	const (
		agent   = "attach-precompact-agent"
		cycleID = "cyc-attach-precompact"
		sid     = "sess-pc-attached"
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

	var cleared int
	cfgOverrides := testCycleOverrides{CycleIDs: func() string { return cycleID }, HandoffPath: func(_, a string) string {
		return "/tmp/HANDOFF-" + a + ".md"
	}, HandoffRead: alwaysNonce, HandoffScrub: func(_ string) error { return nil }, Inject: spy.inject, Gauge: noopGauge, JournalWrite: jc.write}
	cfg := keeper.CyclerConfig{
		AgentName:      agent,
		ProjectDir:     t.TempDir(),
		TmuxTarget:     "fake-pane",
		ActPct:         90.0,
		WarnPct:        80.0,
		HandoffTimeout: 200 * time.Millisecond,
		ClearSettle:    50 * time.Millisecond,
		PollInterval:   5 * time.Millisecond,
	}
	cycler := mustNewCyclerWithOverridesAndDeps(cfg, em, cfgOverrides, func(deps *keeper.CycleDeps) {
		deps.Context = testContextWithClear{ContextStore: deps.Context, clear: func() error { cleared++; return nil }}
		deps.Operator = testOperatorProbe(attach.fn)
	})

	cf := &keeper.CtxFile{Pct: 95.0, SessionID: sid}
	if err := cycler.RunForPrecompact(context.Background(), cf); err != nil {
		t.Fatalf("RunForPrecompact: %v", err)
	}

	if n := len(spy.texts()); n != 0 {
		t.Errorf("want 0 inject calls under precompact while attached; got %d", n)
	}
	if cleared != 1 {
		t.Errorf("want precompact marker cleared exactly once (bounded-fallback); got %d", cleared)
	}
	if evts := em.EventsOfType(core.EventTypeSessionKeeperOperatorAttached); len(evts) != 0 {
		t.Errorf("want 0 operator_attached under precompact (non-durable since TA3); got %d", len(evts))
	}
	pcb := em.EventsOfType(core.EventTypeSessionKeeperPrecompactBlocked)
	if len(pcb) != 1 {
		t.Fatalf("want 1 precompact_blocked; got %d", len(pcb))
	}
	var pp core.SessionKeeperPrecompactBlockedPayload
	if err := json.Unmarshal(pcb[0].Payload, &pp); err != nil {
		t.Fatalf("unmarshal precompact_blocked: %v", err)
	}
	if pp.Action != "operator_attached" {
		t.Errorf("precompact_blocked.action = %q; want \"operator_attached\"", pp.Action)
	}
}

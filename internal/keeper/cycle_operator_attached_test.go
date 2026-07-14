package keeper_test

// cycle_operator_attached_test.go — -short unit tests for the operator-attached
// guard on the keeper act-path (hk-6qf).
//
// When a human operator is attached to the managed tmux session, the keeper's
// reset-cycle injection (/session-handoff, /clear, agent brief) would race
// the operator's own keystrokes and could clobber an in-flight turn. The guard
// makes the act-path warn-only while attached: it SUPPRESSES the destructive
// injection (no inject calls, no handoff_started event) and emits a
// session_keeper_operator_attached event. Once the operator detaches the cycle
// PROCEEDS exactly as before.
//
// These tests use a FAKE OperatorAttachedFn (no real tmux) so they run under
// `go test -short`. The real `tmux list-clients` path is covered by the
// integration test in cycle_operator_attached_integration_test.go.

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/keeper"
)

// fakeAttach is a controllable OperatorAttachedFn whose return value can be
// flipped between calls to simulate an operator detaching mid-session.
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

// newAttachTestCycler builds a Cycler wired with test fakes whose
// OperatorAttachedFn is the supplied attach probe. Mirrors newTestCyclerManaged
// but threads OperatorAttachedFn so the guard can be exercised deterministically.
func newAttachTestCycler(
	agent, projectDir, cycleID string,
	em keeper.Emitter,
	spy *cycleSpyInjector,
	jc *journalCapture,
	readHandoff func(string) (string, error),
	readGaugeFn func(string, string) (*keeper.CtxFile, time.Time, error),
	attachFn func(string) bool,
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
		ReadHandoff:        readHandoff,
		TruncateHandoffFn:  func(_ string) error { return nil },
		InjectFn:           spy.inject,
		ReadGaugeFn:        readGaugeFn,
		CrispIdleFn:        func(_, _ string) bool { return true },
		HoldingDispatchFn:  func(_, _ string) bool { return false },
		WriteJournalFn:     jc.write,
		SetTmuxEnvFn:       func(_ context.Context, _, _, _ string) error { return nil },
		OperatorAttachedFn: attachFn,
	}
	return keeper.NewCycler(cfg, em)
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

	// Handoff/gauge fakes that WOULD let a normal cycle complete — proving the
	// only thing stopping it is the attach guard.
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

	// No injection at all (warn-only).
	if n := len(spy.texts()); n != 0 {
		t.Errorf("want 0 inject calls while operator attached; got %d: %v", n, spy.texts())
	}
	// No handoff_started (the cycle never opened).
	if evts := em.EventsOfType(core.EventTypeSessionKeeperHandoffStarted); len(evts) != 0 {
		t.Errorf("want 0 handoff_started while operator attached; got %d", len(evts))
	}
	// operator_attached is no longer persisted (logmine TA3 / finish F55).
	if evts := em.EventsOfType(core.EventTypeSessionKeeperOperatorAttached); len(evts) != 0 {
		t.Errorf("want 0 operator_attached events (non-durable since TA3); got %d", len(evts))
	}
	// The guard probe was consulted.
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
	readGaugeFn := gaugeReturnsNewSIDAfter(1, "", agent, prevSID, newSID)

	cycler := newAttachTestCycler(agent, t.TempDir(), cycleID, em, spy, jc, readHandoff, readGaugeFn, attach.fn)

	cf := &keeper.CtxFile{Pct: 95.0, SessionID: prevSID}
	if err := cycler.MaybeRun(context.Background(), cf); err != nil {
		t.Fatalf("MaybeRun: %v", err)
	}

	// Cycle ran: handoff, /clear, agent brief (T8/I1).
	texts := spy.texts()
	if len(texts) < 3 {
		t.Fatalf("want >=3 inject calls when detached; got %d: %v", len(texts), texts)
	}
	// cycle_complete emitted; operator_attached NOT emitted.
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
	// Handoff returns the nonce immediately once polled; gauge flips to newSID.
	alwaysNonce := func(_ string) (string, error) { return "# Handoff\n\n" + nonce + "\n", nil }
	readGaugeFn := gaugeReturnsNewSIDAfter(1, "", agent, prevSID, newSID)

	cycler := newAttachTestCycler(agent, t.TempDir(), cycleID, em, spy, jc, alwaysNonce, readGaugeFn, attach.fn)

	cf := &keeper.CtxFile{Pct: 95.0, SessionID: prevSID}

	// First tick: attached → suppressed.
	if err := cycler.MaybeRun(context.Background(), cf); err != nil {
		t.Fatalf("MaybeRun(attached): %v", err)
	}
	if n := len(spy.texts()); n != 0 {
		t.Fatalf("want 0 inject calls on attached tick; got %d", n)
	}
	// operator_attached is no longer persisted (logmine TA3 / finish F55).
	if evts := em.EventsOfType(core.EventTypeSessionKeeperOperatorAttached); len(evts) != 0 {
		t.Errorf("want 0 operator_attached (non-durable since TA3); got %d", len(evts))
	}

	// Operator detaches.
	attach.set(false)

	// Second tick on the SAME session: cycle now proceeds (anti-loop did NOT
	// latch because the first tick never fired a real cycle).
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
	// operator_attached is no longer persisted (logmine TA3 / finish F55).
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
	cfg := keeper.CyclerConfig{
		IdleMarkerModTimeFn: idleMarkerFreshNow, // Stop hook wired: model-done on first AwaitModelDone poll (T8)
		AgentName:           agent,
		ProjectDir:          t.TempDir(),
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
		ReadHandoff:              alwaysNonce,
		TruncateHandoffFn:        func(_ string) error { return nil },
		InjectFn:                 spy.inject,
		ReadGaugeFn:              noopGauge,
		CrispIdleFn:              func(_, _ string) bool { return true },
		HoldingDispatchFn:        func(_, _ string) bool { return false },
		WriteJournalFn:           jc.write,
		SetTmuxEnvFn:             func(_ context.Context, _, _, _ string) error { return nil },
		ClearPrecompactTriggerFn: func(_, _ string) error { cleared++; return nil },
		OperatorAttachedFn:       attach.fn,
	}
	cycler := keeper.NewCycler(cfg, em)

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
	// operator_attached is no longer persisted (logmine TA3 / finish F55).
	if evts := em.EventsOfType(core.EventTypeSessionKeeperOperatorAttached); len(evts) != 0 {
		t.Errorf("want 0 operator_attached under precompact (non-durable since TA3); got %d", len(evts))
	}
	// precompact_blocked recorded the operator_attached action.
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

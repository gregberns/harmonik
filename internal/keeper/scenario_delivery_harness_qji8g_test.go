package keeper_test

// scenario_delivery_harness_qji8g_test.go — T9 MANDATORY scenario-test suite
// (bead hk-keeper-delivery-scenario-tests-qji8g), HARNESS tier. Black-box
// (package keeper_test) scenarios built on the mature keeper harnesses:
//   - the offline reactive session fake (cycle_reactive_harness_test.go) for the
//     late-handoff ABORT (b) and the FORCE-ACT never-idle cut (e);
//   - the operator-attach Cycler harness (cycle_operator_attached_test.go) for the
//     mid-wait operator re-check (d) adjunct;
//   - the foreign-session watcher config (backstop_test.go) for the hard-ceiling
//     backstop (e).
//
// No build tag — these run under `go test ./internal/keeper/`, deterministically.
//
// Scenario→function map:
//	(b) late-handoff after 300s (T5/T6) →
//	     TestScenario_LateHandoffAborts_NoClear_qji8g          (real-clock harness abort)
//	     TestScenario_LateHandoff300sFakeClock_Aborts_qji8g    (virtual-time 300s window)
//	(d) operator-present misread cycler adjunct (T8) →
//	     TestScenario_OperatorAttachesMidWait_HoldsClear_qji8g
//	(e) FORCE-ACT still cuts a never-idle session (SK-028 / NG1) →
//	     TestScenario_ForceAct_NeverIdleStillCut_qji8g
//	     TestScenario_HardCeilingBackstop_NotWeakened_qji8g
//	     TestScenario_NoThresholdConstantChanged_qji8g

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/keeper"
	"github.com/gregberns/harmonik/internal/substrate"
)

// (b) LATE-HANDOFF ABORT — the agent never writes the nonce within the handoff
// window (writeNonce=false), so the nonce poll cannot confirm and the cycle
// ABORTS on the handoff timeout. The destructive /clear is NEVER injected on an
// unconfirmed handoff (SK-INV-001). Fail-before: a path that cleared without a
// fresh, confirmed handoff would have injected /clear here. Pass-after:
// cycle_aborted{handoff_timeout}, no /clear, no cycle_complete. Validates T5/T6
// (the abort leaves the 300s watch window; the T+301 restart-now leg is the
// integration test scenario_restartnow_integration_qji8g_test.go).
func TestScenario_LateHandoffAborts_NoClear_qji8g(t *testing.T) {
	t.Parallel()

	const (
		agent   = "qji8g-lateabort-agent"
		cycleID = "cyc-qji8g-lateabort-001"
	)
	s1, s2 := reactiveSIDs()

	em := &keeper.RecordingEmitter{}
	jc := &journalCapture{}
	var managedBinding string

	// writeNonce=false: /session-handoff is injected but the nonce is never
	// written → the poll times out → abort BEFORE any /clear.
	rs := newReactiveSession(s1, s2, false /*writeNonce*/, true /*flipOnClear*/)

	cycler := newReactiveCycler(
		agent, t.TempDir(), cycleID, rs, em, jc, &managedBinding,
		40*time.Millisecond, // handoffTimeout (a few 5ms poll intervals)
		30*time.Millisecond, // clearSettle (unreached)
	)

	cf := &keeper.CtxFile{Pct: 95.0, Tokens: 320_000, WindowSize: 1_000_000, SessionID: s1}
	if err := cycler.MaybeRun(context.Background(), cf); err != nil {
		t.Fatalf("MaybeRun: %v", err)
	}

	// SK-INV-001: /clear must NEVER be injected on an unconfirmed handoff.
	if rs.sawClear() {
		t.Fatal("/clear was injected on the late-handoff abort path — SK-INV-001 violated")
	}
	// cycle_aborted{handoff_timeout} emitted; cycle_complete NOT.
	aborted := em.EventsOfType(core.EventTypeSessionKeeperCycleAborted)
	if len(aborted) != 1 {
		t.Fatalf("want 1 cycle_aborted; got %d", len(aborted))
	}
	var ap core.SessionKeeperCycleAbortedPayload
	if err := json.Unmarshal(aborted[0].Payload, &ap); err != nil {
		t.Fatalf("unmarshal cycle_aborted: %v", err)
	}
	if ap.Reason != "handoff_timeout" {
		t.Errorf("cycle_aborted.reason = %q; want \"handoff_timeout\"", ap.Reason)
	}
	if n := len(em.EventsOfType(core.EventTypeSessionKeeperCycleComplete)); n != 0 {
		t.Errorf("want 0 cycle_complete on abort; got %d", n)
	}
	// The gauge SID never rotated (the flip is gated behind /clear, which never ran).
	if rs.liveSID() != s1 {
		t.Errorf("gauge SID = %q after abort; want %q (never rotated)", rs.liveSID(), s1)
	}
}

// (b) LATE-HANDOFF ABORT — VIRTUAL 300s WINDOW. Wires substrate.FakeClock into
// CyclerConfig.Clock with the production HandoffTimeout (300s) and drives the abort
// entirely in virtual time: the cycle opens (AwaitingHandoff), the nonce is never
// written, virtual time jumps past 300s, and the handoff-timeout edge aborts with
// NO /clear. This proves the abort keys off the real 300s window (DefaultHandoffTimeout)
// without a 5-minute wall-clock wait, and that the cycle timing path is fully on the
// ClockPort (a residual time.Now would never trip under a manual-advance clock).
func TestScenario_LateHandoff300sFakeClock_Aborts_qji8g(t *testing.T) {
	t.Parallel()

	const (
		agent   = "qji8g-fakeclock-agent"
		cycleID = "cyc-qji8g-fakeclock-001"
	)
	s1, s2 := reactiveSIDs()

	em := &keeper.RecordingEmitter{}
	jc := &journalCapture{}

	rs := newReactiveSession(s1, s2, false /*writeNonce*/, true /*flipOnClear*/)
	clock := substrate.NewFakeClock(time.Unix(1_700_000_000, 0))

	cfg := keeper.CyclerConfig{
		AgentName:      agent,
		ProjectDir:     t.TempDir(),
		TmuxTarget:     "fake-pane",
		Clock:          clock,
		ActPct:         90.0,
		WarnPct:        80.0,
		HandoffTimeout: keeper.DefaultHandoffTimeout, // the real 300s K2 window
		ClearSettle:    10 * time.Second,             // unreached
		PollInterval:   30 * time.Second,             // coarse virtual cadence
		CycleIDGen:     func() string { return cycleID },
		IsManagedFn:    func(_, _ string) bool { return true },
		HandoffFilePath: func(_, a string) string {
			return "/tmp/HANDOFF-" + a + ".md"
		},
		ReadHandoff:         rs.readHandoff,
		HandoffModTimeFn:    rs.handoffModTime,
		TruncateHandoffFn:   rs.truncate,
		InjectFn:            rs.inject,
		ReadGaugeFn:         rs.readGauge,
		CrispIdleFn:         func(_, _ string) bool { return true },
		HoldingDispatchFn:   func(_, _ string) bool { return false },
		WriteJournalFn:      jc.write,
		SetTmuxEnvFn:        func(_ context.Context, _, _, _ string) error { return nil },
		OperatorAttachedFn:  func(string) bool { return false }, // deterministic, no real tmux
		IdleMarkerModTimeFn: func(_, _ string) (time.Time, bool) { return clock.Now(), true },
	}
	cycler := keeper.NewCycler(cfg, em)

	errCh := make(chan error, 1)
	go func() {
		errCh <- cycler.MaybeRun(context.Background(),
			&keeper.CtxFile{Pct: 95.0, Tokens: 320_000, WindowSize: 1_000_000, SessionID: s1})
	}()

	// Wait for the drive loop to arm its detection + deadline tickers, then jump
	// virtual time past the 300s handoff window so the timeout edge trips.
	clock.BlockUntil(2)
	clock.Advance(keeper.DefaultHandoffTimeout + time.Second)

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("MaybeRun: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("MaybeRun did not return after the virtual 300s advance (residual real-time dependency in the cycle timing path?)")
	}

	if rs.sawClear() {
		t.Fatal("/clear injected on the 300s-timeout abort path — SK-INV-001 violated")
	}
	aborted := em.EventsOfType(core.EventTypeSessionKeeperCycleAborted)
	if len(aborted) != 1 {
		t.Fatalf("want 1 cycle_aborted after the 300s window; got %d", len(aborted))
	}
	var ap core.SessionKeeperCycleAbortedPayload
	if err := json.Unmarshal(aborted[0].Payload, &ap); err != nil {
		t.Fatalf("unmarshal cycle_aborted: %v", err)
	}
	if ap.Reason != "handoff_timeout" {
		t.Errorf("cycle_aborted.reason = %q; want \"handoff_timeout\"", ap.Reason)
	}
	if n := len(em.EventsOfType(core.EventTypeSessionKeeperCycleComplete)); n != 0 {
		t.Errorf("want 0 cycle_complete on the 300s abort; got %d", n)
	}
}

// (d) OPERATOR-PRESENT MISREAD — CYCLER ADJUNCT. An operator who attaches AFTER
// cycle entry but DURING the handoff wait is respected: the in-cycle re-check
// (SK-035) holds the /clear so the destructive reset never lands over the
// operator's in-flight turn — whereas the single entry-time Gate-7 sample would
// have missed it. attachFn is false on the FIRST probe (cycle-entry Gate-7, so the
// cycle opens and /session-handoff injects) and true thereafter (the wait polls).
// The handoff nonce is ALWAYS present, so the ONLY thing withholding /clear is the
// re-check. Validates T8 (SK-035); companion to the operatorActiveSince unit in
// scenario_delivery_qji8g_test.go.
func TestScenario_OperatorAttachesMidWait_HoldsClear_qji8g(t *testing.T) {
	t.Parallel()

	const (
		agent   = "qji8g-midwait-agent"
		cycleID = "cyc-qji8g-midwait-001"
		sid     = "sess-qji8g-midwait"
	)

	em := &keeper.RecordingEmitter{}
	spy := &cycleSpyInjector{}
	jc := &journalCapture{}

	var probes int
	attachFn := func(string) bool { probes++; return probes > 1 } // absent at entry, present during the wait

	nonce := "<!-- KEEPER:" + cycleID + " -->"
	alwaysNonce := func(string) (string, error) { return "# Handoff\n\n" + nonce + "\n", nil }
	gauge := func(_, _ string) (*keeper.CtxFile, time.Time, error) {
		return &keeper.CtxFile{Pct: 95.0, SessionID: sid}, time.Now(), nil
	}

	cycler := newAttachTestCycler(agent, t.TempDir(), cycleID, em, spy, jc, alwaysNonce, gauge, attachFn)

	if err := cycler.MaybeRun(context.Background(), &keeper.CtxFile{Pct: 95.0, SessionID: sid}); err != nil {
		t.Fatalf("MaybeRun: %v", err)
	}

	texts := spy.texts()
	if len(texts) == 0 {
		t.Fatal("cycle did not open — expected the /session-handoff inject before the wait")
	}
	for _, tx := range texts {
		if strings.Contains(tx, "/clear") {
			t.Fatalf("/clear injected over a mid-wait operator attach (SK-035 violated): %v", texts)
		}
	}
	if evts := em.EventsOfType(core.EventTypeSessionKeeperCycleComplete); len(evts) != 0 {
		t.Errorf("cycle_complete emitted despite a held /clear; want 0 (aborted), got %d", len(evts))
	}
	if probes < 2 {
		t.Errorf("operator-attached re-check not consulted during the wait (probes=%d)", probes)
	}
}

// (e) FORCE-ACT STILL CUTS A NEVER-IDLE SESSION. A perpetually-busy session
// (CrispIdleFn always false) above the FORCE threshold must be cut UNCONDITIONALLY:
// the CrispIdle gate is bypassed on the force path, the cycle fires, and /clear is
// STILL gated on a confirmed nonce (the deferral machinery does NOT relax the
// safety gate). Proves the K2 leader-defer work did not weaken the FORCE-ACT
// backstop (SK-028 / NG1).
func TestScenario_ForceAct_NeverIdleStillCut_qji8g(t *testing.T) {
	t.Parallel()

	const (
		agent   = "qji8g-force-agent"
		cycleID = "cyc-qji8g-force-001"
	)
	s1, s2 := reactiveSIDs()

	em := &keeper.RecordingEmitter{}
	jc := &journalCapture{}
	var mu sync.Mutex
	var managedBinding string

	// writeNonce=true so /clear is reachable (proving the nonce gate is NOT skipped
	// on the force path); flipOnClear=true so /clear causally rotates S1→S2.
	rs := newReactiveSession(s1, s2, true /*writeNonce*/, true /*flipOnClear*/)

	cfg := keeper.CyclerConfig{
		// Stop hook wired and freshly fired (T8, SK-014): ModelDone lands on the
		// first AwaitModelDone poll so the force cycle does not stall the phase.
		IdleMarkerModTimeFn: idleMarkerFreshNow,
		AgentName:           agent,
		ProjectDir:          t.TempDir(),
		TmuxTarget:          "fake-pane",
		ActPct:              90.0,
		WarnPct:             80.0,
		ForceActPct:         95.0,
		HandoffTimeout:      500 * time.Millisecond,
		ClearSettle:         300 * time.Millisecond,
		PollInterval:        5 * time.Millisecond,
		CycleIDGen:          func() string { return cycleID },
		IsManagedFn:         func(_, _ string) bool { return true },
		HandoffFilePath: func(_, a string) string {
			return "/tmp/HANDOFF-" + a + ".md"
		},
		ReadHandoff:       rs.readHandoff,
		HandoffModTimeFn:  rs.handoffModTime,
		TruncateHandoffFn: rs.truncate,
		InjectFn:          rs.inject,
		ReadGaugeFn:       rs.readGauge,
		CrispIdleFn:       func(_, _ string) bool { return false }, // NEVER idle → force path
		HoldingDispatchFn: func(_, _ string) bool { return false },
		WriteJournalFn:    jc.write,
		SetTmuxEnvFn:      func(_ context.Context, _, _, _ string) error { return nil },
		SendEscapeFn:      func(_ context.Context, _ string) error { return nil }, // no-op escape (force path)
		SetManagedSessionFn: func(_, _, sid string) error {
			mu.Lock()
			defer mu.Unlock()
			managedBinding = sid
			return nil
		},
	}
	cycler := keeper.NewCycler(cfg, em)

	// Tokens well above the default ForceActAbsTokens (240K) with CrispIdle=false.
	cf := &keeper.CtxFile{Pct: 97.0, Tokens: 390_000, WindowSize: 1_000_000, SessionID: s1}
	if err := cycler.MaybeRun(context.Background(), cf); err != nil {
		t.Fatalf("MaybeRun: %v", err)
	}

	// The never-idle session was cut anyway: the full cycle completed.
	if n := len(em.EventsOfType(core.EventTypeSessionKeeperCycleComplete)); n != 1 {
		t.Fatalf("want 1 cycle_complete (never-idle session must still be cut on the force path); got %d", n)
	}
	// /clear was STILL gated on the nonce (ran only because the handoff confirmed).
	if !rs.sawClear() {
		t.Fatal("/clear never injected on the force path — nonce gate may have been skipped")
	}
	if cause := rs.flipCause(); cause != "/clear" {
		t.Errorf("SID flip caused by %q; want \"/clear\" (force path must still gate /clear on the nonce)", cause)
	}
	if rs.sidViolatedCausality() {
		t.Error("a new SID appeared before /clear — nonce gate / causality violated on the force path")
	}
	mu.Lock()
	got := managedBinding
	mu.Unlock()
	if got != s2 {
		t.Errorf("managed binding = %q; want %q (S2 — the never-idle session was cut)", got, s2)
	}
}

// (e) HARD-CEILING BACKSTOP NOT WEAKENED. The SID-independent hard-ceiling
// failsafe fires at 290K tokens on a foreign-session gauge even with the restart
// machinery present — the K2 deferral does not weaken this last-resort trip-wire.
// A control at 270K (below the 280K ceiling) does NOT fire. Mirrors backstop_test.go.
func TestScenario_HardCeilingBackstop_NotWeakened_qji8g(t *testing.T) {
	t.Parallel()

	t.Run("fires_at_290K", func(t *testing.T) {
		t.Parallel()
		em := &keeper.RecordingEmitter{}
		spy := &restartSpy{}

		cfg := foreignSessionConfig(t, t.TempDir(), "qji8g-ceiling-290k", 290_000)
		cfg.HardCeilingMode = keeper.HardCeilingModeRestart
		cfg.HardCeilingRestartFn = spy.restart
		cfg.HardCeilingCooldown = 10 * time.Second

		runWatcherFor(context.Background(), cfg, em, 80*time.Millisecond)

		if spy.count() == 0 {
			t.Error("want >=1 hard-ceiling restart at 290K tokens; got 0 (backstop weakened)")
		}
		if len(em.EventsOfType(core.EventTypeSessionKeeperHardCeiling)) == 0 {
			t.Error("want >=1 session_keeper_hard_ceiling event at 290K; got 0")
		}
	})

	t.Run("does_not_fire_at_270K", func(t *testing.T) {
		t.Parallel()
		em := &keeper.RecordingEmitter{}
		spy := &restartSpy{}

		cfg := foreignSessionConfig(t, t.TempDir(), "qji8g-ceiling-270k", 270_000)
		cfg.HardCeilingMode = keeper.HardCeilingModeRestart
		cfg.HardCeilingRestartFn = spy.restart

		runWatcherFor(context.Background(), cfg, em, 80*time.Millisecond)

		if n := spy.count(); n != 0 {
			t.Errorf("want 0 hard-ceiling restarts at 270K (below the ceiling); got %d", n)
		}
	})
}

// (e) NO THRESHOLD CONSTANT CHANGED. The guardrail regression (SK-028 / NG1): the
// keeper-restart-delivery work must not alter any warn/act/force-act/hard-ceiling
// value, the handoff window, or the settle window. Pin the EXPORTED single-source
// constants to their locked values — any diff that moves one fails here.
func TestScenario_NoThresholdConstantChanged_qji8g(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		got  int64
		want int64
	}{
		{"DefaultWarnAbsTokens", keeper.DefaultWarnAbsTokens, 200_000},
		{"DefaultActAbsTokens", keeper.DefaultActAbsTokens, 215_000},
		{"DefaultForceActAbsOffset", keeper.DefaultForceActAbsOffset, 25_000},
		{"DefaultHardCeilingTokens", keeper.DefaultHardCeilingTokens, 280_000},
		{"HardCeilingAbsTokens", keeper.HardCeilingAbsTokens, 280_000},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s = %d; want %d (threshold constant changed — SK-028 violated)", tc.name, tc.got, tc.want)
		}
	}
	// force_act derives as act + offset = 240K; assert the derivation is intact.
	if got := keeper.DefaultActAbsTokens + keeper.DefaultForceActAbsOffset; got != 240_000 {
		t.Errorf("derived force_act = %d; want 240000", got)
	}
	// The 300s handoff window (hk-4xni9 K2) and 10s clear-settle are unchanged.
	if keeper.DefaultHandoffTimeout != 300*time.Second {
		t.Errorf("DefaultHandoffTimeout = %v; want 300s", keeper.DefaultHandoffTimeout)
	}
	if keeper.DefaultClearSettle != 10*time.Second {
		t.Errorf("DefaultClearSettle = %v; want 10s", keeper.DefaultClearSettle)
	}
}

package keeper_test

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
// window (writeNonce=false). The observation wake returns control and keeps the
// request pending. It does not clear, abort, or rotate the session.
func TestScenario_HandoffObservationWake_ParksWithoutClear(t *testing.T) {
	t.Parallel()

	const (
		agent   = "qji8g-lateabort-agent"
		cycleID = "cyc-qji8g-lateabort-001"
	)
	s1, s2 := reactiveSIDs()

	em := &keeper.RecordingEmitter{}
	jc := &journalCapture{}
	var managedBinding string

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

	if rs.sawClear() {
		t.Fatal("/clear was injected without a confirmed handoff")
	}
	parked := em.EventsOfType(core.EventTypeSessionKeeperCycleParked)
	if len(parked) != 1 {
		t.Fatalf("want 1 cycle_parked; got %d", len(parked))
	}
	var pp core.SessionKeeperCycleParkedPayload
	if err := json.Unmarshal(parked[0].Payload, &pp); err != nil {
		t.Fatalf("unmarshal cycle_parked: %v", err)
	}
	if pp.Reason != "handoff_pending" {
		t.Errorf("cycle_parked.reason = %q; want handoff_pending", pp.Reason)
	}
	if pp.CycleID != cycleID {
		t.Errorf("cycle_parked.cycle_id = %q; want %q (a park must keep the request id)", pp.CycleID, cycleID)
	}
	if n := len(em.EventsOfType(core.EventTypeSessionKeeperCycleComplete)); n != 0 {
		t.Errorf("want 0 cycle_complete while pending; got %d", n)
	}
	if rs.liveSID() != s1 {
		t.Errorf("gauge SID = %q while pending; want %q", rs.liveSID(), s1)
	}
	if got := jc.lastJournal(); got == nil || got.Phase != "pending" || got.CycleID != cycleID {
		t.Fatalf("last journal = %+v; want pending request %s", got, cycleID)
	}
}

func TestScenario_LateMarkedHandoff_ResumesOriginalRequest(t *testing.T) {
	t.Parallel()
	const cycleID = "cyc-late-resume-001"
	s1, s2 := reactiveSIDs()
	em := &keeper.RecordingEmitter{}
	jc := &journalCapture{}
	var managedBinding string
	rs := newReactiveSession(s1, s2, false, true)
	cycler := newReactiveCycler("late-resume-agent", t.TempDir(), cycleID, rs, em, jc, &managedBinding, 40*time.Millisecond, 30*time.Millisecond)
	cf := &keeper.CtxFile{Pct: 95, Tokens: 320_000, WindowSize: 1_000_000, SessionID: s1}

	if err := cycler.MaybeRun(context.Background(), cf); err != nil {
		t.Fatalf("first MaybeRun: %v", err)
	}
	rs.writeMarkedHandoff(cycleID)
	if err := cycler.MaybeRun(context.Background(), cf); err != nil {
		t.Fatalf("resume MaybeRun: %v", err)
	}

	if !rs.sawClear() || rs.liveSID() != s2 {
		t.Fatalf("late handoff did not complete clear: clear=%v sid=%q", rs.sawClear(), rs.liveSID())
	}
	completed := em.EventsOfType(core.EventTypeSessionKeeperCycleComplete)
	if len(completed) != 1 {
		t.Fatalf("cycle_complete count = %d; want 1", len(completed))
	}

	parked := em.EventsOfType(core.EventTypeSessionKeeperCycleParked)
	if len(parked) != 1 {
		t.Fatalf("cycle_parked count = %d; want 1 (the first pass must suspend)", len(parked))
	}
	var pp core.SessionKeeperCycleParkedPayload
	if err := json.Unmarshal(parked[0].Payload, &pp); err != nil {
		t.Fatalf("unmarshal cycle_parked: %v", err)
	}
	if pp.Reason != "handoff_pending" {
		t.Errorf("cycle_parked.reason = %q; want handoff_pending (the resumable flavor)", pp.Reason)
	}
	var cp core.SessionKeeperCycleCompletePayload
	if err := json.Unmarshal(completed[0].Payload, &cp); err != nil {
		t.Fatalf("unmarshal cycle_complete: %v", err)
	}
	if pp.CycleID != cycleID || cp.CycleID != cycleID {
		t.Errorf("parked id = %q, complete id = %q; want both %q (one request, suspended then resumed)",
			pp.CycleID, cp.CycleID, cycleID)
	}
	if got := jc.lastJournal(); got == nil || got.Phase != "complete" || got.CycleID != cycleID {
		t.Fatalf("last journal = %+v; want complete original request", got)
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
	t.Skip("keeper-checkpoint-handshake: the 300s deadline is now an observation wake, not an abort")
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
	cfgOverrides := testCycleOverrides{CycleIDs:

	// the real 300s K2 window
	// unreached
	// coarse virtual cadence
	func() string { return cycleID }, HandoffPath: func(_, a string) string {
		return "/tmp/HANDOFF-" + a + ".md"
	}, HandoffRead: rs.readHandoff, HandoffScrub: rs.truncate, Inject: rs.inject, Gauge: rs.readGauge, JournalWrite: jc.write}
	cfg := keeper.CyclerConfig{
		AgentName:      agent,
		ProjectDir:     t.TempDir(),
		TmuxTarget:     "fake-pane",
		Clock:          clock,
		ActPct:         90.0,
		WarnPct:        80.0,
		HandoffTimeout: keeper.DefaultHandoffTimeout,
		ClearSettle:    10 * time.Second,
		PollInterval:   30 * time.Second,
	}
	cycler := mustNewCyclerWithOverridesAndDeps(cfg, em, cfgOverrides, func(deps *keeper.CycleDeps) {
		deps.Handoff = testHandoffWithModTime{HandoffDocument: deps.Handoff, modTime: rs.handoffModTime}
		deps.Activity = testActivityWithIdle{ActivityProbe: deps.Activity, idleMarker: func() (time.Time, bool) { return clock.Now(), true }}
	})

	errCh := make(chan error, 1)
	go func() {
		errCh <- cycler.MaybeRun(context.Background(),
			&keeper.CtxFile{Pct: 95.0, Tokens: 320_000, WindowSize: 1_000_000, SessionID: s1})
	}()

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
func TestScenario_ClientActivityMidWait_DoesNotHideHandoff_qji8g(t *testing.T) {
	t.Parallel()

	const (
		agent   = "qji8g-midwait-agent"
		cycleID = "cyc-qji8g-midwait-001"
		sid     = "sess-qji8g-midwait"
		newSID  = "sess-qji8g-midwait-next"
	)

	em := &keeper.RecordingEmitter{}
	spy := &cycleSpyInjector{}
	jc := &journalCapture{}

	var probes int
	attachFn := func(string) bool { probes++; return probes > 1 } // absent at entry, present during the wait

	nonce := "<!-- KEEPER:" + cycleID + " -->"
	alwaysNonce := func(string) (string, error) { return "# Handoff\n\n" + nonce + "\n", nil }
	gauge := gaugeReturnsNewSIDAfter(1, sid, newSID)

	cycler := newAttachTestCycler(agent, t.TempDir(), cycleID, em, spy, jc, alwaysNonce, gauge, attachFn)

	if err := cycler.MaybeRun(context.Background(), &keeper.CtxFile{Pct: 95.0, SessionID: sid}); err != nil {
		t.Fatalf("MaybeRun: %v", err)
	}

	texts := spy.texts()
	if len(texts) == 0 {
		t.Fatal("cycle did not open — expected the /session-handoff inject before the wait")
	}
	clearSeen := false
	for _, tx := range texts {
		if strings.Contains(tx, "/clear") {
			clearSeen = true
		}
	}
	if !clearSeen {
		t.Fatalf("written handoff was hidden by client activity: %v", texts)
	}
	if evts := em.EventsOfType(core.EventTypeSessionKeeperCycleComplete); len(evts) != 1 {
		t.Errorf("cycle_complete count = %d; want 1", len(evts))
	}
	if probes != 1 {
		t.Errorf("tmux client probe count = %d; want entry probe only", probes)
	}
}

// (e) FORCE-ACT STILL CUTS A NEVER-IDLE SESSION. A perpetually-busy session
// (IdleProbe always false) above the FORCE threshold must be cut UNCONDITIONALLY:
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

	rs := newReactiveSession(s1, s2, true /*writeNonce*/, true /*flipOnClear*/)
	cfgOverrides := testCycleOverrides{CycleIDs:

	// Stop hook wired and freshly fired (T8, SK-014): ModelDone lands on the
	// first AwaitModelDone poll so the force cycle does not stall the phase.

	func() string { return cycleID }, HandoffPath: func(_, a string) string {
		return "/tmp/HANDOFF-" + a + ".md"
	}, HandoffRead: rs.readHandoff, HandoffScrub: rs.truncate, Inject: rs.inject, Gauge: rs.readGauge, JournalWrite: jc.write}
	cfg := keeper.CyclerConfig{
		AgentName:      agent,
		ProjectDir:     t.TempDir(),
		TmuxTarget:     "fake-pane",
		ActPct:         90.0,
		WarnPct:        80.0,
		ForceActPct:    95.0,
		HandoffTimeout: 500 * time.Millisecond,
		ClearSettle:    300 * time.Millisecond,
		PollInterval:   5 * time.Millisecond,
	}
	cycler := mustNewCyclerWithOverridesAndDeps(cfg, em, cfgOverrides, func(deps *keeper.CycleDeps) {
		deps.Handoff = testHandoffWithModTime{HandoffDocument: deps.Handoff, modTime: rs.handoffModTime}
		deps.Idle = testIdleProbe(false)
		deps.Context = testContextWithManaged{ContextStore: deps.Context, setManaged: func(sid string) error {
			mu.Lock()
			defer mu.Unlock()
			managedBinding = sid
			return nil
		}}
	})

	cf := &keeper.CtxFile{Pct: 97.0, Tokens: 390_000, WindowSize: 1_000_000, SessionID: s1}
	if err := cycler.MaybeRun(context.Background(), cf); err != nil {
		t.Fatalf("MaybeRun: %v", err)
	}

	if n := len(em.EventsOfType(core.EventTypeSessionKeeperCycleComplete)); n != 1 {
		t.Fatalf("want 1 cycle_complete (never-idle session must still be cut on the force path); got %d", n)
	}
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
		{"DefaultWarnAbsTokens", keeper.DefaultWarnAbsTokens, 170_000},
		{"DefaultActAbsTokens", keeper.DefaultActAbsTokens, 200_000},
		{"DefaultForceActAbsOffset", keeper.DefaultForceActAbsOffset, 20_000},
		{"DefaultHardCeilingTokens", keeper.DefaultHardCeilingTokens, 280_000},
		{"HardCeilingAbsTokens", keeper.HardCeilingAbsTokens, 280_000},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s = %d; want %d (threshold constant changed — SK-028 violated)", tc.name, tc.got, tc.want)
		}
	}
	if got := keeper.DefaultActAbsTokens + keeper.DefaultForceActAbsOffset; got != 220_000 {
		t.Errorf("derived force_act = %d; want 220000", got)
	}
	if keeper.DefaultHandoffTimeout != 300*time.Second {
		t.Errorf("DefaultHandoffTimeout = %v; want 300s", keeper.DefaultHandoffTimeout)
	}
	if keeper.DefaultClearSettle != 10*time.Second {
		t.Errorf("DefaultClearSettle = %v; want 10s", keeper.DefaultClearSettle)
	}
}

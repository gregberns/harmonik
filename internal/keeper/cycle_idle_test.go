package keeper_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/keeper"
)

func newIdleCycler(
	t *testing.T,
	projectDir string,
	em keeper.Emitter,
	crispIdle bool,
	holdingDispatch bool,
	idleRestartCooldown time.Duration,
	readHandoff func(string) (string, error),
	readGaugeFn func(string, string) (*keeper.CtxFile, time.Time, error),
) *keeper.Cycler {
	t.Helper()

	spy := &cycleSpyInjector{}
	jc := &journalCapture{}
	const cycleID = "cyc-idle-test"
	nonce := "<!-- KEEPER:" + cycleID + " -->"

	if readHandoff == nil {
		readHandoff = func(_ string) (string, error) {
			return "# Handoff\n\n" + nonce + "\n", nil
		}
	}
	if readGaugeFn == nil {
		readGaugeFn = func(_, _ string) (*keeper.CtxFile, time.Time, error) {
			return &keeper.CtxFile{Pct: 10.0, Tokens: 5_000, WindowSize: 200_000, SessionID: "sess-new"}, time.Now(), nil
		}
	}
	cfgOverrides := testCycleOverrides{CycleIDs: func() string { return cycleID }, HandoffPath: func(_, agent string) string {
		return filepath.Join(projectDir, "HANDOFF-"+agent+".md")
	}, HandoffRead: readHandoff, HandoffScrub: func(_ string) error { return nil }, Inject: spy.inject, Gauge: readGaugeFn, JournalWrite: jc.write}
	cfg := keeper.CyclerConfig{
		AgentName:      "idle-agent",
		ProjectDir:     projectDir,
		TmuxTarget:     "fake-pane",
		ActAbsTokens:   actAbsForIdleTests,
		ActPct:         90.0,
		WarnPct:        80.0,
		HandoffTimeout: 500 * time.Millisecond,
		ClearSettle:    50 * time.Millisecond,
		PollInterval:   10 * time.Millisecond,

		IdleRestartAbsTokens: defaultIdleTokenThreshold,
		IdleRestartCooldown:  idleRestartCooldown,
	}
	return mustNewCyclerWithOverridesAndDeps(cfg, em, cfgOverrides, func(deps *keeper.CycleDeps) {
		deps.Context = testContextWithClear{ContextStore: deps.Context, clear: func() error { return nil }}
		deps.Dispatch = testDispatchProbe(holdingDispatch)
		deps.Idle = testIdleProbe(crispIdle)
	})
}

const defaultIdleTokenThreshold = 150_000

const aboveIdleButBelowAct = 200_000

const actAbsForIdleTests = 300_000

// TestCycler_RunForIdle_EmitsEventBelowThreshold verifies that when tokens are
// below IdleRestartAbsTokens, RunForIdle emits session_keeper_idle_crew and
// does NOT trigger a handoff cycle.
func TestCycler_RunForIdle_EmitsEventBelowThreshold(t *testing.T) {
	t.Parallel()

	em := &keeper.RecordingEmitter{}
	cycler := newIdleCycler(t, t.TempDir(), em,
		true,  // crispIdle
		false, // holdingDispatch
		30*time.Minute,
		nil, nil,
	)

	cf := &keeper.CtxFile{Pct: 10.0, Tokens: 100_000, WindowSize: 200_000, SessionID: "sess-idle"}
	if err := cycler.RunForIdle(context.Background(), cf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	idleEvents := em.EventsOfType(core.EventTypeSessionKeeperIdleCrew)
	if len(idleEvents) != 1 {
		t.Fatalf("want 1 session_keeper_idle_crew event, got %d", len(idleEvents))
	}
	var payload map[string]any
	if err := json.Unmarshal(idleEvents[0].Payload, &payload); err != nil {
		t.Fatalf("unmarshal idle_crew payload: %v", err)
	}
	if got, ok := payload["reason"]; !ok || got != "below_idle_threshold" {
		t.Errorf("payload[reason] = %v, want %q", got, "below_idle_threshold")
	}

	if got := em.EventsOfType(core.EventTypeSessionKeeperHandoffStarted); len(got) != 0 {
		t.Errorf("handoff_started emitted unexpectedly: %d events", len(got))
	}
}

// TestCycler_RunForIdle_FiresAboveThreshold verifies that when tokens are above
// IdleRestartAbsTokens but below the act threshold, and the session is idle and
// not holding dispatch, RunForIdle triggers the full handoff cycle.
func TestCycler_RunForIdle_FiresAboveThreshold(t *testing.T) {
	t.Parallel()

	em := &keeper.RecordingEmitter{}
	const (
		prevSID = "sess-idle-prev"
		newSID  = "sess-idle-after"
	)
	const cycleID = "cyc-idle-test"
	nonce := "<!-- KEEPER:" + cycleID + " -->"
	readHandoff := func(_ string) (string, error) {
		return "# Handoff\n\n" + nonce + "\n", nil
	}
	callCount := 0
	readGaugeFn := func(_, _ string) (*keeper.CtxFile, time.Time, error) {
		callCount++
		sid := prevSID
		if callCount >= 5 {
			sid = newSID
		}
		return &keeper.CtxFile{Pct: 10.0, Tokens: 5_000, WindowSize: 200_000, SessionID: sid}, time.Now(), nil
	}

	cycler := newIdleCycler(t, t.TempDir(), em,
		true,  // crispIdle
		false, // holdingDispatch
		30*time.Minute,
		readHandoff, readGaugeFn,
	)

	cf := &keeper.CtxFile{Pct: 50.0, Tokens: aboveIdleButBelowAct, WindowSize: 1_000_000, SessionID: prevSID}
	if err := cycler.RunForIdle(context.Background(), cf); err != nil {
		t.Fatalf("RunForIdle: %v", err)
	}

	if got := em.EventsOfType(core.EventTypeSessionKeeperHandoffStarted); len(got) == 0 {
		t.Error("expected handoff_started event; got none")
	}
	if got := em.EventsOfType(core.EventTypeSessionKeeperCycleComplete); len(got) == 0 {
		t.Error("expected cycle_complete event; got none")
	}

	if got := em.EventsOfType(core.EventTypeSessionKeeperIdleCrew); len(got) != 0 {
		t.Errorf("session_keeper_idle_crew emitted unexpectedly: %d events", len(got))
	}
}

// TestCycler_RunForIdle_SkipsAboveActThreshold verifies that when tokens are
// above the act threshold, RunForIdle skips (defers to MaybeRun).
func TestCycler_RunForIdle_SkipsAboveActThreshold(t *testing.T) {
	t.Parallel()

	em := &keeper.RecordingEmitter{}
	cycler := newIdleCycler(t, t.TempDir(), em,
		true,  // crispIdle
		false, // holdingDispatch
		30*time.Minute,
		nil, nil,
	)

	cf := &keeper.CtxFile{Pct: 95.0, Tokens: 350_000, WindowSize: 1_000_000, SessionID: "sess-above-act"}
	if err := cycler.RunForIdle(context.Background(), cf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := em.EventsOfType(core.EventTypeSessionKeeperHandoffStarted); len(got) != 0 {
		t.Errorf("handoff_started emitted unexpectedly: %d events", len(got))
	}
	if got := em.EventsOfType(core.EventTypeSessionKeeperIdleCrew); len(got) != 0 {
		t.Errorf("session_keeper_idle_crew emitted unexpectedly: %d events", len(got))
	}
}

// TestCycler_RunForIdle_SkipsWhenHoldingDispatch verifies that HoldingDispatch
// suppresses the idle restart (fail-closed).
func TestCycler_RunForIdle_SkipsWhenHoldingDispatch(t *testing.T) {
	t.Parallel()

	em := &keeper.RecordingEmitter{}
	cycler := newIdleCycler(t, t.TempDir(), em,
		true, // crispIdle
		true, // holdingDispatch — should suppress
		30*time.Minute,
		nil, nil,
	)

	cf := &keeper.CtxFile{Pct: 50.0, Tokens: aboveIdleButBelowAct, WindowSize: 1_000_000, SessionID: "sess-holding"}
	if err := cycler.RunForIdle(context.Background(), cf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := em.EventsOfType(core.EventTypeSessionKeeperHandoffStarted); len(got) != 0 {
		t.Errorf("handoff_started emitted unexpectedly when HoldingDispatch=true")
	}
}

// TestCycler_RunForIdle_RespectsCooldown verifies that a second call within the
// cooldown window is suppressed.
func TestCycler_RunForIdle_RespectsCooldown(t *testing.T) {
	t.Parallel()

	em := &keeper.RecordingEmitter{}
	const prevSID = "sess-cool-prev"
	const newSID = "sess-cool-after"
	const cycleID = "cyc-idle-test"
	nonce := "<!-- KEEPER:" + cycleID + " -->"
	readHandoff := func(_ string) (string, error) {
		return "# Handoff\n\n" + nonce + "\n", nil
	}
	callCount := 0
	readGaugeFn := func(_, _ string) (*keeper.CtxFile, time.Time, error) {
		callCount++
		sid := prevSID
		if callCount >= 5 {
			sid = newSID
		}
		return &keeper.CtxFile{Pct: 10.0, Tokens: 5_000, WindowSize: 200_000, SessionID: sid}, time.Now(), nil
	}

	cycler := newIdleCycler(t, t.TempDir(), em,
		true,        // crispIdle
		false,       // holdingDispatch
		1*time.Hour, // long cooldown
		readHandoff, readGaugeFn,
	)

	cf := &keeper.CtxFile{Pct: 50.0, Tokens: aboveIdleButBelowAct, WindowSize: 1_000_000, SessionID: prevSID}
	ctx := context.Background()

	if err := cycler.RunForIdle(ctx, cf); err != nil {
		t.Fatalf("first RunForIdle: %v", err)
	}
	firstFires := len(em.EventsOfType(core.EventTypeSessionKeeperHandoffStarted))
	if firstFires == 0 {
		t.Fatal("expected first RunForIdle to fire a cycle")
	}

	cf2 := &keeper.CtxFile{Pct: 50.0, Tokens: aboveIdleButBelowAct, WindowSize: 1_000_000, SessionID: "sess-cool-after2"}
	if err := cycler.RunForIdle(ctx, cf2); err != nil {
		t.Fatalf("second RunForIdle: %v", err)
	}
	secondFires := len(em.EventsOfType(core.EventTypeSessionKeeperHandoffStarted))
	if secondFires != firstFires {
		t.Errorf("cooldown should suppress second call; handoff_started count went from %d to %d", firstFires, secondFires)
	}
}

// TestCycler_RunForIdle_AbortDoesNotArmCooldown verifies that an idle-restart
// attempt that ABORTS (handoff nonce never confirmed → runCycle returns without
// issuing /clear) does NOT arm IdleRestartCooldown. A start-stamped cooldown
// would suppress every retry for the full window (30 min default), wedging the
// still-large-context idle crew on a single failed attempt. After the fix the
// next tick must be free to attempt again. Refs: hk-4i0s.
func TestCycler_RunForIdle_AbortDoesNotArmCooldown(t *testing.T) {
	t.Skip("keeper-checkpoint-handshake: timeout abort is retired; pending idle requests need a new policy decision")
	t.Parallel()

	em := &keeper.RecordingEmitter{}
	readHandoff := func(_ string) (string, error) {
		return "# Handoff\n\n(no nonce here)\n", nil
	}
	readGaugeFn := func(_, _ string) (*keeper.CtxFile, time.Time, error) {
		return &keeper.CtxFile{Pct: 10.0, Tokens: 5_000, WindowSize: 200_000, SessionID: "sess-abort-gauge"}, time.Now(), nil
	}

	cycler := newIdleCycler(t, t.TempDir(), em,
		true,        // crispIdle
		false,       // holdingDispatch
		1*time.Hour, // long cooldown
		readHandoff, readGaugeFn,
	)
	ctx := context.Background()

	cf1 := &keeper.CtxFile{Pct: 50.0, Tokens: aboveIdleButBelowAct, WindowSize: 1_000_000, SessionID: "sess-abort-1"}
	if err := cycler.RunForIdle(ctx, cf1); err != nil {
		t.Fatalf("first RunForIdle: %v", err)
	}
	if got := len(em.EventsOfType(core.EventTypeSessionKeeperHandoffStarted)); got != 1 {
		t.Fatalf("first attempt: want 1 handoff_started, got %d", got)
	}
	if got := len(em.EventsOfType(core.EventTypeSessionKeeperCycleAborted)); got != 1 {
		t.Fatalf("first attempt should ABORT: want 1 cycle_aborted, got %d", got)
	}
	if got := len(em.EventsOfType(core.EventTypeSessionKeeperCycleComplete)); got != 0 {
		t.Fatalf("first attempt must not complete: got %d cycle_complete", got)
	}

	cf2 := &keeper.CtxFile{Pct: 50.0, Tokens: aboveIdleButBelowAct, WindowSize: 1_000_000, SessionID: "sess-abort-2"}
	if err := cycler.RunForIdle(ctx, cf2); err != nil {
		t.Fatalf("second RunForIdle: %v", err)
	}
	if got := len(em.EventsOfType(core.EventTypeSessionKeeperHandoffStarted)); got != 2 {
		t.Errorf("aborted attempt must NOT arm cooldown: want 2 handoff_started after retry, got %d", got)
	}
}

// TestCycler_RunForIdle_AntiLoop verifies that a RunForIdle call with the same
// session_id as lastFiredSID (set by a prior MaybeRun cycle) is suppressed.
func TestCycler_RunForIdle_AntiLoop(t *testing.T) {
	t.Parallel()

	em := &keeper.RecordingEmitter{}
	const (
		prevSID = "sess-antiloop"
		newSID  = "sess-antiloop-after"
	)
	const cycleID = "cyc-idle-test"
	nonce := "<!-- KEEPER:" + cycleID + " -->"
	readHandoff := func(_ string) (string, error) {
		return "# Handoff\n\n" + nonce + "\n", nil
	}
	readGaugeFn := func(_, _ string) (*keeper.CtxFile, time.Time, error) {
		return &keeper.CtxFile{Pct: 10.0, Tokens: 5_000, WindowSize: 200_000, SessionID: newSID}, time.Now(), nil
	}

	cycler := newIdleCycler(t, t.TempDir(), em,
		true,  // crispIdle
		false, // holdingDispatch
		0,     // no cooldown
		readHandoff, readGaugeFn,
	)

	keeper.SetCyclerLastFiredSID(cycler, prevSID)

	cf := &keeper.CtxFile{Pct: 50.0, Tokens: aboveIdleButBelowAct, WindowSize: 1_000_000, SessionID: prevSID}
	if err := cycler.RunForIdle(context.Background(), cf); err != nil {
		t.Fatalf("RunForIdle: %v", err)
	}

	if got := em.EventsOfType(core.EventTypeSessionKeeperHandoffStarted); len(got) != 0 {
		t.Errorf("anti-loop should suppress cycle; got %d handoff_started events", len(got))
	}
}

// TestCycler_RunForIdle_DeduplicatesIdleBelowThreshold verifies that repeated
// RunForIdle calls with the same session_id and tokens below the idle-restart
// floor emit session_keeper_idle_crew only once (transition, not per-poll).
// Refs: hk-qshh8.
func TestCycler_RunForIdle_DeduplicatesIdleBelowThreshold(t *testing.T) {
	t.Parallel()

	em := &keeper.RecordingEmitter{}
	cycler := newIdleCycler(t, t.TempDir(), em,
		true,  // crispIdle
		false, // holdingDispatch
		30*time.Minute,
		nil, nil,
	)

	cf := &keeper.CtxFile{Pct: 10.0, Tokens: 100_000, WindowSize: 200_000, SessionID: "sess-dedup"}
	ctx := context.Background()

	for i := range 3 {
		if err := cycler.RunForIdle(ctx, cf); err != nil {
			t.Fatalf("call %d: unexpected error: %v", i+1, err)
		}
	}

	got := em.EventsOfType(core.EventTypeSessionKeeperIdleCrew)
	if len(got) != 1 {
		t.Fatalf("want 1 session_keeper_idle_crew event (transition-only), got %d", len(got))
	}
}

// TestCycler_RunForIdle_ReemitsOnNewSID verifies that a new session_id below
// the idle threshold does trigger a fresh session_keeper_idle_crew emission
// even if the previous session already received one. Refs: hk-qshh8.
func TestCycler_RunForIdle_ReemitsOnNewSID(t *testing.T) {
	t.Parallel()

	em := &keeper.RecordingEmitter{}
	cycler := newIdleCycler(t, t.TempDir(), em,
		true,  // crispIdle
		false, // holdingDispatch
		30*time.Minute,
		nil, nil,
	)

	ctx := context.Background()
	cfA := &keeper.CtxFile{Pct: 10.0, Tokens: 100_000, WindowSize: 200_000, SessionID: "sess-a"}
	cfB := &keeper.CtxFile{Pct: 10.0, Tokens: 80_000, WindowSize: 200_000, SessionID: "sess-b"}

	if err := cycler.RunForIdle(ctx, cfA); err != nil {
		t.Fatalf("sess-a RunForIdle: %v", err)
	}
	if err := cycler.RunForIdle(ctx, cfA); err != nil {
		t.Fatalf("sess-a second poll: %v", err)
	}
	if got := em.EventsOfType(core.EventTypeSessionKeeperIdleCrew); len(got) != 1 {
		t.Fatalf("after sess-a: want 1 idle_crew event, got %d", len(got))
	}

	if err := cycler.RunForIdle(ctx, cfB); err != nil {
		t.Fatalf("sess-b RunForIdle: %v", err)
	}
	if got := em.EventsOfType(core.EventTypeSessionKeeperIdleCrew); len(got) != 2 {
		t.Fatalf("after sess-b: want 2 idle_crew events total, got %d", len(got))
	}
}

// TestCycler_RunForIdle_SkipsWhenNotIdle verifies that a non-quiescent pane
// suppresses the idle restart.
func TestCycler_RunForIdle_SkipsWhenNotIdle(t *testing.T) {
	t.Parallel()

	em := &keeper.RecordingEmitter{}
	cycler := newIdleCycler(t, t.TempDir(), em,
		false, // crispIdle = false → pane busy
		false, // holdingDispatch
		30*time.Minute,
		nil, nil,
	)

	cf := &keeper.CtxFile{Pct: 50.0, Tokens: aboveIdleButBelowAct, WindowSize: 1_000_000, SessionID: "sess-busy"}
	if err := cycler.RunForIdle(context.Background(), cf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := em.EventsOfType(core.EventTypeSessionKeeperHandoffStarted); len(got) != 0 {
		t.Errorf("handoff_started emitted unexpectedly when CrispIdle=false")
	}
}

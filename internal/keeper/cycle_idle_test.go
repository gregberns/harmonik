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

// newIdleCycler builds a Cycler for idle-restart tests. Mirrors the
// newPrecompactCycler / newTestCycler helpers: same fakes, no real disk/tmux.
// actAbsTokens is passed explicitly so tests can push tokens above/below the
// act threshold without depending on the production default.
func newIdleCycler(
	t *testing.T,
	projectDir string,
	em keeper.Emitter,
	crispIdle bool,
	holdingDispatch bool,
	actAbsTokens int64,
	idleRestartAbsTokens int64,
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

	cfg := keeper.CyclerConfig{
		AgentName:      "idle-agent",
		ProjectDir:     projectDir,
		TmuxTarget:     "fake-pane",
		ActAbsTokens:   actAbsTokens,
		ActPct:         90.0,
		WarnPct:        80.0,
		HandoffTimeout: 500 * time.Millisecond,
		ClearSettle:    50 * time.Millisecond,
		PollInterval:   10 * time.Millisecond,
		CycleIDGen:     func() string { return cycleID },
		IsManagedFn:    func(_, _ string) bool { return true },
		HandoffFilePath: func(_, agent string) string {
			return filepath.Join(projectDir, "HANDOFF-"+agent+".md")
		},
		ReadHandoff:              readHandoff,
		TruncateHandoffFn:        func(_ string) error { return nil },
		InjectFn:                 spy.inject,
		ReadGaugeFn:              readGaugeFn,
		CrispIdleFn:              func(_, _ string) bool { return crispIdle },
		HoldingDispatchFn:        func(_, _ string) bool { return holdingDispatch },
		WriteJournalFn:           jc.write,
		AppendHandoffFn:          func(_, _ string) error { return nil },
		SetTmuxEnvFn:             func(_ context.Context, _, _, _ string) error { return nil },
		ClearPrecompactTriggerFn: func(_, _ string) error { return nil },
		IdleRestartAbsTokens:     idleRestartAbsTokens,
		IdleRestartCooldown:      idleRestartCooldown,
	}
	return keeper.NewCycler(cfg, em)
}

// defaultIdleTokenThreshold is the default IdleRestartAbsTokens (150_000).
const defaultIdleTokenThreshold = 150_000

// aboveIdleButBelowAct is a token count above the idle threshold but below
// the act threshold used in tests (200_000 < 300_000).
const aboveIdleButBelowAct = 200_000

// actAbsForIdleTests is the act threshold used in idle tests. Set higher than
// aboveIdleButBelowAct so tests can control the above/below split.
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
		actAbsForIdleTests,
		defaultIdleTokenThreshold,
		30*time.Minute,
		nil, nil,
	)

	// tokens = 100K < 150K threshold → notification, no restart.
	cf := &keeper.CtxFile{Pct: 10.0, Tokens: 100_000, WindowSize: 200_000, SessionID: "sess-idle"}
	if err := cycler.RunForIdle(context.Background(), cf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Expect session_keeper_idle_crew event.
	idleEvents := em.EventsOfType(core.EventTypeSessionKeeperIdleCrew)
	if len(idleEvents) != 1 {
		t.Fatalf("want 1 session_keeper_idle_crew event, got %d", len(idleEvents))
	}
	// Verify payload fields.
	var payload map[string]any
	if err := json.Unmarshal(idleEvents[0].Payload, &payload); err != nil {
		t.Fatalf("unmarshal idle_crew payload: %v", err)
	}
	if got, ok := payload["reason"]; !ok || got != "below_idle_threshold" {
		t.Errorf("payload[reason] = %v, want %q", got, "below_idle_threshold")
	}

	// Cycle must NOT have fired.
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
		actAbsForIdleTests,
		defaultIdleTokenThreshold,
		30*time.Minute,
		readHandoff, readGaugeFn,
	)

	// tokens = 200K → above 150K threshold, below 300K act threshold.
	cf := &keeper.CtxFile{Pct: 50.0, Tokens: aboveIdleButBelowAct, WindowSize: 1_000_000, SessionID: prevSID}
	if err := cycler.RunForIdle(context.Background(), cf); err != nil {
		t.Fatalf("RunForIdle: %v", err)
	}

	// Cycle must have fired.
	if got := em.EventsOfType(core.EventTypeSessionKeeperHandoffStarted); len(got) == 0 {
		t.Error("expected handoff_started event; got none")
	}
	if got := em.EventsOfType(core.EventTypeSessionKeeperCycleComplete); len(got) == 0 {
		t.Error("expected cycle_complete event; got none")
	}

	// No idle_crew event should be emitted (tokens >= threshold).
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
		actAbsForIdleTests,
		defaultIdleTokenThreshold,
		30*time.Minute,
		nil, nil,
	)

	// tokens = 350K → above act threshold (300K).
	cf := &keeper.CtxFile{Pct: 95.0, Tokens: 350_000, WindowSize: 1_000_000, SessionID: "sess-above-act"}
	if err := cycler.RunForIdle(context.Background(), cf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// No handoff cycle.
	if got := em.EventsOfType(core.EventTypeSessionKeeperHandoffStarted); len(got) != 0 {
		t.Errorf("handoff_started emitted unexpectedly: %d events", len(got))
	}
	// No idle_crew event (tokens >= threshold).
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
		actAbsForIdleTests,
		defaultIdleTokenThreshold,
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

	// Use a 1-hour cooldown so the second call is always within the window.
	cycler := newIdleCycler(t, t.TempDir(), em,
		true,  // crispIdle
		false, // holdingDispatch
		actAbsForIdleTests,
		defaultIdleTokenThreshold,
		1*time.Hour, // long cooldown
		readHandoff, readGaugeFn,
	)

	cf := &keeper.CtxFile{Pct: 50.0, Tokens: aboveIdleButBelowAct, WindowSize: 1_000_000, SessionID: prevSID}
	ctx := context.Background()

	// First call: should fire.
	if err := cycler.RunForIdle(ctx, cf); err != nil {
		t.Fatalf("first RunForIdle: %v", err)
	}
	firstFires := len(em.EventsOfType(core.EventTypeSessionKeeperHandoffStarted))
	if firstFires == 0 {
		t.Fatal("expected first RunForIdle to fire a cycle")
	}

	// Second call immediately after: cooldown suppresses.
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
	t.Parallel()

	em := &keeper.RecordingEmitter{}
	// readHandoff returns NO keeper nonce, so pollForNonce times out and runCycle
	// ABORTS (the handoff_timeout path) on every call.
	readHandoff := func(_ string) (string, error) {
		return "# Handoff\n\n(no nonce here)\n", nil
	}
	readGaugeFn := func(_, _ string) (*keeper.CtxFile, time.Time, error) {
		return &keeper.CtxFile{Pct: 10.0, Tokens: 5_000, WindowSize: 200_000, SessionID: "sess-abort-gauge"}, time.Now(), nil
	}

	// 1-hour cooldown: if the abort start-stamped it, the second call would be
	// gated for the whole window. The fix unwinds the stamp on abort.
	cycler := newIdleCycler(t, t.TempDir(), em,
		true,  // crispIdle
		false, // holdingDispatch
		actAbsForIdleTests,
		defaultIdleTokenThreshold,
		1*time.Hour, // long cooldown
		readHandoff, readGaugeFn,
	)
	ctx := context.Background()

	// First call: attempts, then aborts (no nonce confirmed).
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

	// Second call on the next tick with a DIFFERENT session_id (clears Gate-7
	// anti-loop, isolating the cooldown gate). With the start-stamp bug this is
	// suppressed for the whole cooldown; after the fix it attempts again.
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

	// Use a zero cooldown so cooldown doesn't interfere.
	cycler := newIdleCycler(t, t.TempDir(), em,
		true,  // crispIdle
		false, // holdingDispatch
		actAbsForIdleTests,
		defaultIdleTokenThreshold,
		0, // no cooldown
		readHandoff, readGaugeFn,
	)

	// Set lastFiredSID to prevSID by pre-arming via the export helper.
	keeper.SetCyclerLastFiredSID(cycler, prevSID)

	// Call RunForIdle with the same session_id → anti-loop should suppress.
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
		actAbsForIdleTests,
		defaultIdleTokenThreshold,
		30*time.Minute,
		nil, nil,
	)

	cf := &keeper.CtxFile{Pct: 10.0, Tokens: 100_000, WindowSize: 200_000, SessionID: "sess-dedup"}
	ctx := context.Background()

	// Three consecutive polls with the same session_id below threshold.
	for i := range 3 {
		if err := cycler.RunForIdle(ctx, cf); err != nil {
			t.Fatalf("call %d: unexpected error: %v", i+1, err)
		}
	}

	// Expect exactly ONE event — transition, not per-poll.
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
		actAbsForIdleTests,
		defaultIdleTokenThreshold,
		30*time.Minute,
		nil, nil,
	)

	ctx := context.Background()
	cfA := &keeper.CtxFile{Pct: 10.0, Tokens: 100_000, WindowSize: 200_000, SessionID: "sess-a"}
	cfB := &keeper.CtxFile{Pct: 10.0, Tokens: 80_000, WindowSize: 200_000, SessionID: "sess-b"}

	// First session below threshold — expect 1 event.
	if err := cycler.RunForIdle(ctx, cfA); err != nil {
		t.Fatalf("sess-a RunForIdle: %v", err)
	}
	if err := cycler.RunForIdle(ctx, cfA); err != nil {
		t.Fatalf("sess-a second poll: %v", err)
	}
	if got := em.EventsOfType(core.EventTypeSessionKeeperIdleCrew); len(got) != 1 {
		t.Fatalf("after sess-a: want 1 idle_crew event, got %d", len(got))
	}

	// New session, also below threshold — must emit again.
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
		actAbsForIdleTests,
		defaultIdleTokenThreshold,
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

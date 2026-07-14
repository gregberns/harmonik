package keeper_test

// sleep_gate_hkl3gs_test.go — unit tests for M3 sleep gate
// (WatcherConfig.SleepingCheckFn + CyclerConfig.SleepingCheckFn).
// Bead: hk-l3gs (Keeper sleep-gate: suppress pane-injection while slept).
// Refs: hk-l3gs, hk-jeby (M1 .sleeping marker contract).

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/keeper"
)

// writeSleepMarker creates .harmonik/.sleeping.<sessionID> to simulate the
// QuiesceArbiter parking a session.
func writeSleepMarker(t *testing.T, projectDir, sessionID string) {
	t.Helper()
	dir := filepath.Join(projectDir, ".harmonik")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll harmonik dir: %v", err)
	}
	path := filepath.Join(dir, ".sleeping."+sessionID)
	if err := os.WriteFile(path, []byte(`{"session_id":"`+sessionID+`"}`), 0o644); err != nil {
		t.Fatalf("write sleep marker: %v", err)
	}
}

// removeSleepMarker removes .harmonik/.sleeping.<sessionID>.
func removeSleepMarker(t *testing.T, projectDir, sessionID string) {
	t.Helper()
	path := filepath.Join(projectDir, ".harmonik", ".sleeping."+sessionID)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		t.Fatalf("remove sleep marker: %v", err)
	}
}

// ── IsSleeping unit tests ─────────────────────────────────────────────────────

// TestIsSleeping_FalseWhenNoMarker verifies IsSleeping returns false when no
// .sleeping.<sessionID> file exists.
func TestIsSleeping_FalseWhenNoMarker(t *testing.T) {
	t.Parallel()
	projectDir := t.TempDir()
	if keeper.IsSleeping(projectDir, "no-such-session") {
		t.Error("IsSleeping: want false when marker absent")
	}
}

// TestIsSleeping_TrueWhenMarkerPresent verifies IsSleeping returns true when
// .harmonik/.sleeping.<sessionID> exists.
func TestIsSleeping_TrueWhenMarkerPresent(t *testing.T) {
	t.Parallel()
	projectDir := t.TempDir()
	sessionID := "sess-sleeping-123"
	writeSleepMarker(t, projectDir, sessionID)
	if !keeper.IsSleeping(projectDir, sessionID) {
		t.Error("IsSleeping: want true when marker exists")
	}
}

// TestIsSleeping_TrueWhenEmptySessionID verifies IsSleeping returns true
// (FAIL-CLOSED, hk-uord) when sessionID is empty — state is indeterminate, so
// the keeper must defer rather than inject into a session it cannot identify.
func TestIsSleeping_TrueWhenEmptySessionID(t *testing.T) {
	t.Parallel()
	projectDir := t.TempDir()
	if !keeper.IsSleeping(projectDir, "") {
		t.Error("IsSleeping: want true (fail-closed) for empty sessionID")
	}
}

// TestIsSleeping_TrueWhenEmptyProjectDir verifies IsSleeping returns true
// (FAIL-CLOSED, hk-uord) when projectDir is empty.
func TestIsSleeping_TrueWhenEmptyProjectDir(t *testing.T) {
	t.Parallel()
	if !keeper.IsSleeping("", "some-session") {
		t.Error("IsSleeping: want true (fail-closed) for empty projectDir")
	}
}

// TestIsSleeping_FalseAfterMarkerRemoved verifies IsSleeping returns false
// once the marker is cleared (simulating M1 waking the session).
func TestIsSleeping_FalseAfterMarkerRemoved(t *testing.T) {
	t.Parallel()
	projectDir := t.TempDir()
	sessionID := "sess-wake-test"
	writeSleepMarker(t, projectDir, sessionID)
	if !keeper.IsSleeping(projectDir, sessionID) {
		t.Fatal("pre-condition: IsSleeping should be true before remove")
	}
	removeSleepMarker(t, projectDir, sessionID)
	if keeper.IsSleeping(projectDir, sessionID) {
		t.Error("IsSleeping: want false after marker removed")
	}
}

// ── Watcher inject gate ───────────────────────────────────────────────────────

// spySleepInjector records inject calls AND whether the session was sleeping
// on each call — used to verify the sleeping gate works.
type spySleepInjector struct {
	mu    sync.Mutex
	calls int
}

func (s *spySleepInjector) inject(_ context.Context, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	return nil
}

func (s *spySleepInjector) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

// TestWatcher_InjectSuppressedWhenSleeping verifies that when the session is
// sleeping (.sleeping.<sessionID> present), the keeper does NOT deliver the
// warn injection even after the gauge quiesces above the warn threshold. The
// warn event IS emitted (state machine intact) but tmux delivery is gated.
func TestWatcher_InjectSuppressedWhenSleeping(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	agent := "sleepy-agent"
	sessionID := "sess-sleep-inject"

	// Park the session.
	writeSleepMarker(t, projectDir, sessionID)

	spy := &spySleepInjector{}

	em := &keeper.RecordingEmitter{}
	cfg := keeper.WatcherConfig{
		AgentName:    agent,
		ProjectDir:   projectDir,
		PollInterval: 10 * time.Millisecond,
		WarnPct:      80.0,
		IdleQuiesce:  1 * time.Millisecond,
		Staleness:    120 * time.Second,
		TmuxTarget:   "fake-pane:0.0", // non-empty so pendingInject is set
		InjectFn:     spy.inject,
	}

	// Write gauge above threshold; use session that is sleeping.
	writeCtxFile(t, projectDir, agent, 85.0, sessionID)

	// Run watcher long enough for multiple ticks — inject should never fire.
	runWatcherFor(context.Background(), cfg, em, 80*time.Millisecond)

	if spy.count() != 0 {
		t.Errorf("inject delivered %d time(s) while session sleeping; want 0", spy.count())
	}
}

// TestWatcher_InjectDeliveredAfterWake verifies that once the sleeping marker
// is removed (session woken), the keeper delivers the pending injection on the
// next quiesced tick.
func TestWatcher_InjectDeliveredAfterWake(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	agent := "wake-agent"
	sessionID := "sess-wake-inject"

	// Park the session.
	writeSleepMarker(t, projectDir, sessionID)

	spy := &spySleepInjector{}

	em := &keeper.RecordingEmitter{}
	cfg := keeper.WatcherConfig{
		AgentName:    agent,
		ProjectDir:   projectDir,
		PollInterval: 10 * time.Millisecond,
		WarnPct:      80.0,
		IdleQuiesce:  1 * time.Millisecond,
		Staleness:    120 * time.Second,
		TmuxTarget:   "fake-pane:0.0",
		InjectFn:     spy.inject,
	}

	writeCtxFile(t, projectDir, agent, 85.0, sessionID)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		w := keeper.NewWatcher(cfg, em)
		_ = w.Run(ctx)
	}()

	// Let a few ticks fire while sleeping — no inject should happen.
	time.Sleep(50 * time.Millisecond)
	if spy.count() != 0 {
		t.Errorf("inject delivered %d time(s) while sleeping; want 0", spy.count())
	}

	// Wake the session — remove the marker.
	removeSleepMarker(t, projectDir, sessionID)

	// Allow time for the watcher to pick up the wake and deliver the inject.
	time.Sleep(100 * time.Millisecond)
	cancel()
	<-done

	if spy.count() < 1 {
		t.Error("inject not delivered after session woke; want ≥1")
	}
}

// ── Cycler MaybeRun gate ──────────────────────────────────────────────────────

// TestCyclerMaybeRun_DeferredWhenSleeping verifies that MaybeRun returns
// without running the cycle when SleepingCheckFn returns true, even when all
// other gates (act threshold, CrispIdle, HoldingDispatch) are satisfied.
func TestCyclerMaybeRun_DeferredWhenSleeping(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	agent := "cycler-sleepy"
	sessionID := "sess-cycler-sleep"

	// Managed marker required for Gate 1.
	keeperDirPath := filepath.Join(projectDir, ".harmonik", "keeper")
	if err := os.MkdirAll(keeperDirPath, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	managedPath := filepath.Join(keeperDirPath, agent+".managed")
	if err := os.WriteFile(managedPath, []byte(sessionID+"\n"), 0o600); err != nil {
		t.Fatalf("write managed: %v", err)
	}

	// .idle marker required for CrispIdle (Gate 4).
	idlePath := filepath.Join(keeperDirPath, agent+".idle")
	if err := os.WriteFile(idlePath, []byte{}, 0o600); err != nil {
		t.Fatalf("write idle: %v", err)
	}

	cycleAttempts := 0

	em := &keeper.RecordingEmitter{}
	cfg := keeper.CyclerConfig{
		IdleMarkerModTimeFn: idleMarkerFreshNow, // Stop hook wired: model-done on first AwaitModelDone poll (T8)
		AgentName:           agent,
		ProjectDir:          projectDir,
		TmuxTarget:          "", // no real injection
		ActPct:              80.0,
		WarnPct:             70.0,
		// Gate overrides to ensure all gates except sleeping pass.
		IsManagedFn:       func(_, _ string) bool { return true },
		CrispIdleFn:       func(_, _ string) bool { return true },
		HoldingDispatchFn: func(_, _ string) bool { return false },
		// SleepingCheckFn returns true — session is sleeping.
		SleepingCheckFn: func(_, _ string) bool { return true },
		// InjectFn should never be called; count calls to detect a cycle firing.
		InjectFn: func(_ context.Context, _, _ string) error {
			cycleAttempts++
			return nil
		},
	}

	cycler := keeper.NewCycler(cfg, em)
	cf := &keeper.CtxFile{
		Pct:       90.0,
		SessionID: sessionID,
		Ts:        time.Now().UTC().Format(time.RFC3339),
	}

	if err := cycler.MaybeRun(context.Background(), cf); err != nil {
		t.Fatalf("MaybeRun returned error: %v", err)
	}

	if cycleAttempts != 0 {
		t.Errorf("cycle fired %d time(s) while session sleeping; want 0", cycleAttempts)
	}
}

// TestCyclerMaybeRun_SleepingGateReachedWhenAwake verifies that when the
// session is NOT sleeping, MaybeRun passes Gate 5b and proceeds to runCycle.
// We use a cancelled context so runCycle returns immediately, and track whether
// SleepingCheckFn was actually called (= Gate 5b was reached).
func TestCyclerMaybeRun_SleepingGateReachedWhenAwake(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	agent := "cycler-awake"
	sessionID := "sess-cycler-awake"

	keeperDirPath := filepath.Join(projectDir, ".harmonik", "keeper")
	if err := os.MkdirAll(keeperDirPath, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	managedPath := filepath.Join(keeperDirPath, agent+".managed")
	if err := os.WriteFile(managedPath, []byte(sessionID+"\n"), 0o600); err != nil {
		t.Fatalf("write managed: %v", err)
	}

	sleepingCalled := false

	em := &keeper.RecordingEmitter{}
	cfg := keeper.CyclerConfig{
		IdleMarkerModTimeFn: idleMarkerFreshNow, // Stop hook wired: model-done on first AwaitModelDone poll (T8)
		AgentName:           agent,
		ProjectDir:          projectDir,
		TmuxTarget:          "", // no real tmux; runCycle skips handoff injection
		ActPct:              80.0,
		WarnPct:             70.0,
		// All gates pass; session is NOT sleeping.
		IsManagedFn:       func(_, _ string) bool { return true },
		CrispIdleFn:       func(_, _ string) bool { return true },
		HoldingDispatchFn: func(_, _ string) bool { return false },
		SleepingCheckFn: func(_, _ string) bool {
			sleepingCalled = true
			return false // awake
		},
		// Stub out functions that would block or require filesystem.
		ReadHandoff: func(_ string) (string, error) {
			// Return immediately so runCycle exits the nonce-poll fast path.
			return "", nil
		},
		HandoffFilePath: func(_, agentName string) string {
			return filepath.Join(projectDir, "HANDOFF-"+agentName+".md")
		},
	}

	// Write a handoff file (empty — nonce poll will not match and runCycle
	// hits its HandoffTimeout; we use a very short timeout via context).
	handoffPath := filepath.Join(projectDir, "HANDOFF-"+agent+".md")
	if err := os.WriteFile(handoffPath, []byte{}, 0o600); err != nil {
		t.Fatalf("write handoff: %v", err)
	}

	cycler := keeper.NewCycler(cfg, em)
	cf := &keeper.CtxFile{
		Pct:       90.0,
		SessionID: sessionID,
		Ts:        time.Now().UTC().Format(time.RFC3339),
	}

	// Use a pre-cancelled context so runCycle exits immediately on the first
	// ctx.Done() check without blocking on the nonce poll.
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	_ = cycler.MaybeRun(ctx, cf)

	// SleepingCheckFn must have been called: Gate 5b was reached, which
	// confirms the sleeping gate is on the hot path and not bypassed.
	if !sleepingCalled {
		t.Error("SleepingCheckFn not called; Gate 5b was not reached (sleeping check bypassed?)")
	}
}

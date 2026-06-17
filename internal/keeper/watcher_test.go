package keeper_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/keeper"
)

// spyInjector records injection calls without spawning real tmux processes.
type spyInjector struct {
	mu    sync.Mutex
	calls int
}

func (s *spyInjector) inject(_ context.Context, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	return nil
}

func (s *spyInjector) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

// writeCtxFile writes a .ctx gauge file for the given agent under projectDir.
func writeCtxFile(t *testing.T, projectDir, agent string, pct float64, sessionID string) {
	t.Helper()
	keeperDir := filepath.Join(projectDir, ".harmonik", "keeper")
	if err := os.MkdirAll(keeperDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	data, err := json.Marshal(keeper.CtxFile{
		Pct:       pct,
		SessionID: sessionID,
		Ts:        time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("json.Marshal CtxFile: %v", err)
	}
	path := filepath.Join(keeperDir, agent+".ctx")
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatalf("WriteFile ctx: %v", err)
	}
}

// writeCtxFileTokens writes a .ctx gauge file carrying an absolute token count
// and an explicit window size (either may be 0) in addition to the percentage.
// Used by the F45 regression test to exercise the Tokens-vs-Pct gate path.
func writeCtxFileTokens(t *testing.T, projectDir, agent string, pct float64, tokens, windowSize int64, sessionID string) {
	t.Helper()
	keeperDir := filepath.Join(projectDir, ".harmonik", "keeper")
	if err := os.MkdirAll(keeperDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	data, err := json.Marshal(keeper.CtxFile{
		Pct:        pct,
		Tokens:     tokens,
		WindowSize: windowSize,
		SessionID:  sessionID,
		Ts:         time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("json.Marshal CtxFile: %v", err)
	}
	path := filepath.Join(keeperDir, agent+".ctx")
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatalf("WriteFile ctx: %v", err)
	}
}

// runWatcherFor starts the watcher and cancels it after dur.
func runWatcherFor(ctx context.Context, cfg keeper.WatcherConfig, em keeper.Emitter, dur time.Duration) {
	ctx2, cancel := context.WithTimeout(ctx, dur)
	defer cancel()
	w := keeper.NewWatcher(cfg, em)
	_ = w.Run(ctx2) //nolint:errcheck // context.DeadlineExceeded is expected
}

// TestWatcher_EmitsOneWarnOnUpwardCrossing verifies that exactly one
// session_keeper_warn is emitted when the gauge crosses from below to above
// the warn threshold, and none on subsequent ticks while above.
func TestWatcher_EmitsOneWarnOnUpwardCrossing(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	agent := "test-agent"

	// Create managed marker so keep is a no-op guard check is bypassed.
	keeperDir := filepath.Join(projectDir, ".harmonik", "keeper")
	if err := os.MkdirAll(keeperDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	em := &keeper.RecordingEmitter{}

	cfg := keeper.WatcherConfig{
		AgentName:    agent,
		ProjectDir:   projectDir,
		PollInterval: 10 * time.Millisecond,
		WarnPct:      80.0,
		IdleQuiesce:  1 * time.Millisecond, // minimal for tests
		Staleness:    120 * time.Second,    // generous to avoid stale hits
		TmuxTarget:   "",                   // no real injection in tests
	}

	// Write a gauge file above the threshold.
	writeCtxFile(t, projectDir, agent, 85.0, "sess-abc")

	// Run the watcher for a few poll intervals.
	runWatcherFor(context.Background(), cfg, em, 80*time.Millisecond)

	warns := em.EventsOfType(core.EventTypeSessionKeeperWarn)
	if len(warns) != 1 {
		t.Errorf("want exactly 1 session_keeper_warn; got %d", len(warns))
		return
	}

	// Verify payload fields.
	var payload core.SessionKeeperWarnPayload
	if err := json.Unmarshal(warns[0].Payload, &payload); err != nil {
		t.Fatalf("unmarshal warn payload: %v", err)
	}
	if payload.AgentName != agent {
		t.Errorf("warn payload.AgentName = %q; want %q", payload.AgentName, agent)
	}
	if payload.Pct != 85.0 {
		t.Errorf("warn payload.Pct = %v; want 85.0", payload.Pct)
	}
	if payload.WarnPct != 80.0 {
		t.Errorf("warn payload.WarnPct = %v; want 80.0", payload.WarnPct)
	}
	if payload.SessionID != "sess-abc" {
		t.Errorf("warn payload.SessionID = %q; want %q", payload.SessionID, "sess-abc")
	}
}

// TestWatcher_NoWarnWhenGaugeIsStale verifies that no session_keeper_warn is
// emitted when the gauge file's mod-time is older than the staleness window.
func TestWatcher_NoWarnWhenGaugeIsStale(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	agent := "stale-agent"

	keeperDir := filepath.Join(projectDir, ".harmonik", "keeper")
	if err := os.MkdirAll(keeperDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	em := &keeper.RecordingEmitter{}

	cfg := keeper.WatcherConfig{
		AgentName:    agent,
		ProjectDir:   projectDir,
		PollInterval: 10 * time.Millisecond,
		WarnPct:      80.0,
		IdleQuiesce:  1 * time.Millisecond,
		Staleness:    1 * time.Millisecond, // so the file is immediately stale
		TmuxTarget:   "",
	}

	// Write a gauge file above the threshold.
	writeCtxFile(t, projectDir, agent, 90.0, "sess-stale")

	// Sleep so the gauge file is stale before the watcher starts.
	time.Sleep(5 * time.Millisecond)

	// Run the watcher.
	runWatcherFor(context.Background(), cfg, em, 60*time.Millisecond)

	warns := em.EventsOfType(core.EventTypeSessionKeeperWarn)
	if len(warns) != 0 {
		t.Errorf("want 0 session_keeper_warn when gauge is stale; got %d", len(warns))
	}
}

// TestWatcher_EmitsNoGaugeWhenFileAbsent verifies that session_keeper_no_gauge
// is emitted at boot when the gauge file is absent.
func TestWatcher_EmitsNoGaugeWhenFileAbsent(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	agent := "absent-agent"

	em := &keeper.RecordingEmitter{}

	cfg := keeper.WatcherConfig{
		AgentName:    agent,
		ProjectDir:   projectDir,
		PollInterval: 10 * time.Millisecond,
		WarnPct:      80.0,
		IdleQuiesce:  1 * time.Millisecond,
		Staleness:    120 * time.Second,
		TmuxTarget:   "",
	}

	// Do NOT write a gauge file — it is absent.

	// Run the watcher for a few ticks.
	runWatcherFor(context.Background(), cfg, em, 60*time.Millisecond)

	noGauge := em.EventsOfType(core.EventTypeSessionKeeperNoGauge)
	if len(noGauge) == 0 {
		t.Error("want at least one session_keeper_no_gauge when gauge file is absent; got 0")
		return
	}

	// Verify payload.
	var payload core.SessionKeeperNoGaugePayload
	if err := json.Unmarshal(noGauge[0].Payload, &payload); err != nil {
		t.Fatalf("unmarshal no_gauge payload: %v", err)
	}
	if payload.AgentName != agent {
		t.Errorf("no_gauge payload.AgentName = %q; want %q", payload.AgentName, agent)
	}
	if payload.Reason != "absent" {
		t.Errorf("no_gauge payload.Reason = %q; want %q", payload.Reason, "absent")
	}
}

// TestWatcher_NoWarnWhenBelowThreshold verifies that no warn is emitted when
// the gauge percentage is below the warn threshold throughout the run.
func TestWatcher_NoWarnWhenBelowThreshold(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	agent := "below-agent"

	em := &keeper.RecordingEmitter{}

	cfg := keeper.WatcherConfig{
		AgentName:    agent,
		ProjectDir:   projectDir,
		PollInterval: 10 * time.Millisecond,
		WarnPct:      80.0,
		IdleQuiesce:  1 * time.Millisecond,
		Staleness:    120 * time.Second,
		TmuxTarget:   "",
	}

	// Write a gauge file well below the threshold.
	writeCtxFile(t, projectDir, agent, 50.0, "sess-below")

	runWatcherFor(context.Background(), cfg, em, 60*time.Millisecond)

	if warns := em.EventsOfType(core.EventTypeSessionKeeperWarn); len(warns) != 0 {
		t.Errorf("want 0 session_keeper_warn below threshold; got %d", len(warns))
	}
}

// TestWatcher_NoWarnBelowPctWhenWindowUnknown is the regression test for logmine
// F45 (hk-jgzg): keeper "warn" fired BELOW the configured warn_pct. The watcher's
// belowWarnThreshold substituted FallbackWindowSize (200k) whenever the gauge
// reported Tokens>0 but WindowSize==0, applying the 0.70 pct-ceil to a FABRICATED
// window → an effective 140k-token threshold. On a real large-window session whose
// statusline reports tokens but no window_size, that fired warn at ~140k tokens —
// well below the configured pct threshold, so the warn event recorded pct < warn_pct.
// The cycler's identically-named belowWarnThreshold never did this (it requires
// WindowSize>0, falling back to Pct otherwise), so warn and cycle gated on different
// bases: the Tokens-vs-Pct split-brain. The fix unifies the watcher with the cycler.
//
// FAILS on old code (warn fires off the fabricated 200k window) and PASSES on the
// fix (WindowSize==0 → Pct path → 27<80 → below → no warn).
// Refs: hk-jgzg (F45), codename:keeper-redesign.
func TestWatcher_NoWarnBelowPctWhenWindowUnknown(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	agent := "f45-agent"

	em := &keeper.RecordingEmitter{}

	cfg := keeper.WatcherConfig{
		AgentName:    agent,
		ProjectDir:   projectDir,
		PollInterval: 10 * time.Millisecond,
		WarnPct:      80.0,
		IdleQuiesce:  1 * time.Millisecond,
		Staleness:    120 * time.Second,
		TmuxTarget:   "",
	}

	// Tokens (270k) exceed the FallbackWindowSize-derived 140k threshold, but Pct
	// (27%) is far below WarnPct (80%) and the gauge reports NO window_size. With a
	// known window this would be a legitimate abs-token warn; with the window
	// unknown the watcher must defer to the pct comparison, exactly as the cycler does.
	writeCtxFileTokens(t, projectDir, agent, 27.0, 270_000, 0, "sess-f45")

	runWatcherFor(context.Background(), cfg, em, 60*time.Millisecond)

	if warns := em.EventsOfType(core.EventTypeSessionKeeperWarn); len(warns) != 0 {
		t.Errorf("want 0 session_keeper_warn when pct(27)<warn_pct(80) and window unknown; got %d (F45 Tokens-vs-Pct split-brain)", len(warns))
	}
}

// TestWatcher_InjectDeliveredAfterQuiescence is the regression test for BUG-1
// (hk-g4ei7): the spy must receive EXACTLY ONE injection even when the gauge
// file is freshly written (non-quiesced) on the crossing tick. The fix defers
// inject via pendingInject and retries on a later quiesced tick.
//
// This test FAILS on old code (warnFired latched before inject, gaugeQuiesced
// false on crossing tick → inject permanently skipped) and PASSES on the fix.
func TestWatcher_InjectDeliveredAfterQuiescence(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	agent := "inject-quiesce-agent"

	keeperDir := filepath.Join(projectDir, ".harmonik", "keeper")
	if err := os.MkdirAll(keeperDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	em := &keeper.RecordingEmitter{}
	spy := &spyInjector{}

	const (
		pollInterval = 10 * time.Millisecond
		idleQuiesce  = 25 * time.Millisecond // realistic: > 1 poll cycle
	)

	cfg := keeper.WatcherConfig{
		AgentName:    agent,
		ProjectDir:   projectDir,
		PollInterval: pollInterval,
		WarnPct:      80.0,
		IdleQuiesce:  idleQuiesce,
		Staleness:    120 * time.Second,
		TmuxTarget:   "fake-pane", // non-empty → injection enabled
		InjectFn:     spy.inject,
	}

	// Write the gauge BEFORE the watcher starts so modTime is established.
	// On tick 1: lastModTime is zero → gaugeQuiesced = false (crossing detected,
	// pendingInject set). On tick 3+: file unchanged → gaugeQuiesced = true →
	// inject delivered.
	writeCtxFile(t, projectDir, agent, 85.0, "sess-inject")

	// Run long enough for the crossing tick + ≥2 quiescence ticks.
	runWatcherFor(context.Background(), cfg, em, 150*time.Millisecond)

	// (a) Exactly one session_keeper_warn event emitted.
	warns := em.EventsOfType(core.EventTypeSessionKeeperWarn)
	if len(warns) != 1 {
		t.Errorf("want exactly 1 session_keeper_warn; got %d", len(warns))
	}

	// (b) Exactly one inject delivered (not zero, not more than one).
	if n := spy.count(); n != 1 {
		t.Errorf("want exactly 1 spy inject call; got %d (BUG-1 regression: crossing-tick non-quiescence must retry)", n)
	}
}

// TestWatcher_WarnResetOnDropBelow verifies that after the percentage drops
// below the threshold and rises again, a second warn is emitted.
func TestWatcher_WarnResetOnDropBelow(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	agent := "reset-agent"

	em := &keeper.RecordingEmitter{}

	cfg := keeper.WatcherConfig{
		AgentName:    agent,
		ProjectDir:   projectDir,
		PollInterval: 5 * time.Millisecond,
		WarnPct:      80.0,
		IdleQuiesce:  1 * time.Millisecond,
		Staleness:    120 * time.Second,
		TmuxTarget:   "",
	}

	keeperDir := filepath.Join(projectDir, ".harmonik", "keeper")
	ctxPath := filepath.Join(keeperDir, agent+".ctx")
	if err := os.MkdirAll(keeperDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	writeCtxFile := func(pct float64) {
		data, _ := json.Marshal(keeper.CtxFile{ //nolint:errcheck // test helper
			Pct: pct,
			Ts:  time.Now().UTC().Format(time.RFC3339),
		})
		_ = os.WriteFile(ctxPath, append(data, '\n'), 0o600) //nolint:errcheck // test helper
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		w := keeper.NewWatcher(cfg, em)
		_ = w.Run(ctx) //nolint:errcheck // context.Canceled expected
	}()

	// Tick 1: above threshold → first warn.
	writeCtxFile(85.0)
	time.Sleep(30 * time.Millisecond)

	// Tick 2: drop below threshold → reset.
	writeCtxFile(70.0)
	time.Sleep(30 * time.Millisecond)

	// Tick 3: cross upward again → second warn.
	writeCtxFile(90.0)
	time.Sleep(30 * time.Millisecond)

	cancel()
	<-done

	warns := em.EventsOfType(core.EventTypeSessionKeeperWarn)
	if len(warns) < 2 {
		t.Errorf("want ≥2 session_keeper_warn after two upward crossings; got %d", len(warns))
	}
}

// TestWatcher_IgnoresForeignSessionGauge verifies that when the managed session
// binding in .managed is "sess-expected" and the gauge carries "sess-foreign",
// the watcher treats the gauge as absent and emits NO warn event.
// Refs: hk-igt (session_id clobber — two same-agent sessions writing to .ctx).
func TestWatcher_IgnoresForeignSessionGauge(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	agent := "binding-agent"

	keeperDir := filepath.Join(projectDir, ".harmonik", "keeper")
	if err := os.MkdirAll(keeperDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	em := &keeper.RecordingEmitter{}
	cfg := keeper.WatcherConfig{
		AgentName:    agent,
		ProjectDir:   projectDir,
		PollInterval: 10 * time.Millisecond,
		WarnPct:      80.0,
		IdleQuiesce:  1 * time.Millisecond,
		Staleness:    120 * time.Second,
		TmuxTarget:   "",
		// Pre-set binding to "sess-expected".
		ReadManagedSessionFn:  func(_, _ string) (string, error) { return "sess-expected", nil },
		WriteManagedSessionFn: func(_, _, _ string) error { return nil },
	}

	// Write gauge with a DIFFERENT session_id — foreign session.
	writeCtxFile(t, projectDir, agent, 90.0, "sess-foreign")

	runWatcherFor(context.Background(), cfg, em, 80*time.Millisecond)

	// No warn — gauge belongs to a different session.
	if warns := em.EventsOfType(core.EventTypeSessionKeeperWarn); len(warns) != 0 {
		t.Errorf("want 0 session_keeper_warn for foreign session; got %d", len(warns))
	}
}

// TestWatcher_LatchesFirstSessionID verifies that when .managed has no session_id
// binding and the gauge has one, the watcher calls WriteManagedSessionFn to latch
// the first-seen session_id.
// Refs: hk-igt (session_id clobber — two same-agent sessions writing to .ctx).
func TestWatcher_LatchesFirstSessionID(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	agent := "latch-agent"

	keeperDir := filepath.Join(projectDir, ".harmonik", "keeper")
	if err := os.MkdirAll(keeperDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	var latchedSID string
	latchCalled := 0
	em := &keeper.RecordingEmitter{}
	cfg := keeper.WatcherConfig{
		AgentName:    agent,
		ProjectDir:   projectDir,
		PollInterval: 10 * time.Millisecond,
		WarnPct:      80.0,
		IdleQuiesce:  1 * time.Millisecond,
		Staleness:    120 * time.Second,
		TmuxTarget:   "",
		// No binding yet.
		ReadManagedSessionFn: func(_, _ string) (string, error) { return "", nil },
		WriteManagedSessionFn: func(_, _, sessionID string) error {
			latchedSID = sessionID
			latchCalled++
			return nil
		},
	}

	writeCtxFile(t, projectDir, agent, 85.0, "sess-first")
	runWatcherFor(context.Background(), cfg, em, 80*time.Millisecond)

	if latchCalled == 0 {
		t.Error("WriteManagedSessionFn never called; want latch on first valid session_id")
	}
	if latchedSID != "sess-first" {
		t.Errorf("latched session_id = %q; want %q", latchedSID, "sess-first")
	}
}

// TestWatcher_AcceptsManagedSession verifies that when .managed has a session_id
// that matches the gauge, normal warn behaviour fires as expected.
// Refs: hk-igt (session_id clobber — two same-agent sessions writing to .ctx).
func TestWatcher_AcceptsManagedSession(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	agent := "match-agent"

	keeperDir := filepath.Join(projectDir, ".harmonik", "keeper")
	if err := os.MkdirAll(keeperDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	em := &keeper.RecordingEmitter{}
	cfg := keeper.WatcherConfig{
		AgentName:    agent,
		ProjectDir:   projectDir,
		PollInterval: 10 * time.Millisecond,
		WarnPct:      80.0,
		IdleQuiesce:  1 * time.Millisecond,
		Staleness:    120 * time.Second,
		TmuxTarget:   "",
		// Binding already set to the same session as the gauge.
		ReadManagedSessionFn:  func(_, _ string) (string, error) { return "sess-mine", nil },
		WriteManagedSessionFn: func(_, _, _ string) error { return nil },
	}

	writeCtxFile(t, projectDir, agent, 90.0, "sess-mine")
	runWatcherFor(context.Background(), cfg, em, 80*time.Millisecond)

	warns := em.EventsOfType(core.EventTypeSessionKeeperWarn)
	if len(warns) != 1 {
		t.Errorf("want exactly 1 session_keeper_warn for matching session; got %d", len(warns))
	}
}

// TestWatcher_RespawnFiredWhenGaugeAbsentAndPaneIdle verifies that the respawn
// command is executed once the gauge has been absent for at least RespawnGrace
// and the pane-idle check returns true.
// Refs: hk-3w2.
func TestWatcher_RespawnFiredWhenGauseAbsentAndPaneIdle(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	agent := "respawn-agent"

	// Track respawn attempts via the spy pane-idle fn and a channel.
	respawnCh := make(chan struct{}, 5)

	em := &keeper.RecordingEmitter{}

	cfg := keeper.WatcherConfig{
		AgentName:    agent,
		ProjectDir:   projectDir,
		PollInterval: 10 * time.Millisecond,
		Staleness:    5 * time.Millisecond, // very short so gauge is immediately stale
		RespawnGrace: 5 * time.Millisecond, // very short for test speed
		// RespawnCooldown left at default (90s) — only one attempt expected.
		RespawnCmd: "true", // sh -c true always succeeds
		TmuxTarget: "dummy-pane",
		// Pane is always idle in this test.
		IsPaneIdleFn: func(_ context.Context, _ string) bool { return true },
		// Spy InjectFn to suppress real tmux calls on warn.
		InjectFn: func(_ context.Context, _ string) error { return nil },
	}

	// Deliberately write NO gauge file so the gauge is immediately absent.
	keeperDir := filepath.Join(projectDir, ".harmonik", "keeper")
	if err := os.MkdirAll(keeperDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Replace respawn command with one that signals via a temp file. Since
	// we can't easily intercept exec.Command("sh","-c","true"), we verify
	// via the emitted event instead.
	_ = respawnCh // not used — events are the observable

	runWatcherFor(context.Background(), cfg, em, 200*time.Millisecond)

	events := em.EventsOfType(core.EventTypeSessionKeeperRespawnAttempted)
	if len(events) == 0 {
		t.Fatal("want at least 1 session_keeper_respawn_attempted event; got 0")
	}
	var payload core.SessionKeeperRespawnAttemptedPayload
	if err := json.Unmarshal(events[0].Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.AgentName != agent {
		t.Errorf("payload.AgentName = %q; want %q", payload.AgentName, agent)
	}
	if payload.Outcome != "ok" {
		t.Errorf("payload.Outcome = %q; want \"ok\" (respawn cmd was 'true')", payload.Outcome)
	}
}

// TestWatcher_RespawnSkippedWhenPaneNotIdle verifies that the respawn command
// is NOT fired when the pane-idle check returns false (agent is still running).
// Refs: hk-3w2.
func TestWatcher_RespawnSkippedWhenPaneNotIdle(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	agent := "respawn-skip-agent"

	em := &keeper.RecordingEmitter{}

	cfg := keeper.WatcherConfig{
		AgentName:    agent,
		ProjectDir:   projectDir,
		PollInterval: 10 * time.Millisecond,
		Staleness:    5 * time.Millisecond,
		RespawnGrace: 5 * time.Millisecond,
		RespawnCmd:   "true",
		TmuxTarget:   "dummy-pane",
		// Pane is NOT idle — agent is still running.
		IsPaneIdleFn: func(_ context.Context, _ string) bool { return false },
		InjectFn:     func(_ context.Context, _ string) error { return nil },
	}

	keeperDir := filepath.Join(projectDir, ".harmonik", "keeper")
	if err := os.MkdirAll(keeperDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	runWatcherFor(context.Background(), cfg, em, 200*time.Millisecond)

	events := em.EventsOfType(core.EventTypeSessionKeeperRespawnAttempted)
	if len(events) != 0 {
		t.Errorf("want 0 session_keeper_respawn_attempted events (pane not idle); got %d", len(events))
	}
}

// TestWatcher_RespawnCooldownPreventsDoubleSpawn verifies that only one respawn
// fires during a run even across multiple stale ticks (cooldown holds).
// Refs: hk-3w2.
func TestWatcher_RespawnCooldownPreventsDoubleSpawn(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	agent := "respawn-cooldown-agent"

	em := &keeper.RecordingEmitter{}

	cfg := keeper.WatcherConfig{
		AgentName:       agent,
		ProjectDir:      projectDir,
		PollInterval:    10 * time.Millisecond,
		Staleness:       5 * time.Millisecond,
		RespawnGrace:    5 * time.Millisecond,
		RespawnCooldown: 10 * time.Second, // long cooldown — only one attempt allowed
		RespawnCmd:      "true",
		TmuxTarget:      "dummy-pane",
		IsPaneIdleFn:    func(_ context.Context, _ string) bool { return true },
		InjectFn:        func(_ context.Context, _ string) error { return nil },
	}

	keeperDir := filepath.Join(projectDir, ".harmonik", "keeper")
	if err := os.MkdirAll(keeperDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Run for 300ms — many poll ticks but cooldown should hold to 1 attempt.
	runWatcherFor(context.Background(), cfg, em, 300*time.Millisecond)

	events := em.EventsOfType(core.EventTypeSessionKeeperRespawnAttempted)
	if len(events) != 1 {
		t.Errorf("want exactly 1 session_keeper_respawn_attempted (cooldown); got %d", len(events))
	}
}

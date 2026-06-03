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

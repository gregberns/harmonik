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

// TestWatcher_SkipsUUIDv7LatchWhenManagedEmpty verifies that when .managed has no
// session_id binding, the watcher does NOT latch a UUIDv7 session_id (daemon
// implementer) into .managed. Instead it emits no_gauge events until a UUIDv4
// session_id appears. (Refs: hk-lap — clear->resume latch race fix)
func TestWatcher_SkipsUUIDv7LatchWhenManagedEmpty(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	agent := "uuid7-skip-agent"

	keeperDir := filepath.Join(projectDir, ".harmonik", "keeper")
	if err := os.MkdirAll(keeperDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

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
		// No binding — simulates cleared state after a clear->resume timeout.
		ReadManagedSessionFn: func(_, _ string) (string, error) { return "", nil },
		WriteManagedSessionFn: func(_, _, _ string) error {
			latchCalled++
			return nil
		},
	}

	// UUIDv7 gauge (daemon implementer — version digit '7' at index 14).
	writeCtxFile(t, projectDir, agent, 50.0, "019ebb07-0000-7000-8000-000000000001")
	runWatcherFor(context.Background(), cfg, em, 80*time.Millisecond)

	if latchCalled != 0 {
		t.Errorf("WriteManagedSessionFn called %d time(s); want 0 — UUIDv7 must not be latched", latchCalled)
	}
	noGaugeEvents := em.EventsOfType(core.EventTypeSessionKeeperNoGauge)
	if len(noGaugeEvents) == 0 {
		t.Error("want at least one session_keeper_no_gauge for UUIDv7 skip; got none")
	}
}

// TestWatcher_LatchesUUIDv4AfterSkippingUUIDv7 verifies that the watcher
// correctly latches the first UUIDv4 session_id it sees after previously
// ignoring UUIDv7 gauges. (Refs: hk-lap)
func TestWatcher_LatchesUUIDv4AfterSkippingUUIDv7(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	agent := "uuid4-latch-after-uuid7-agent"

	keeperDir := filepath.Join(projectDir, ".harmonik", "keeper")
	if err := os.MkdirAll(keeperDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	var latchedSID string
	latchCalled := 0
	em := &keeper.RecordingEmitter{}

	// Gauge sequence: first write a UUIDv7, then a UUIDv4. The test relies on
	// the watcher looping long enough to see both gauges.
	const v7SID = "019ebb07-0000-7000-8000-000000000001"
	const v4SID = "f7e7210d-1234-4abc-8000-000000000002"

	writeCtxFile(t, projectDir, agent, 50.0, v7SID)

	cfg := keeper.WatcherConfig{
		AgentName:    agent,
		ProjectDir:   projectDir,
		PollInterval: 10 * time.Millisecond,
		WarnPct:      80.0,
		IdleQuiesce:  1 * time.Millisecond,
		Staleness:    120 * time.Second,
		TmuxTarget:   "",
		ReadManagedSessionFn: func(_, _ string) (string, error) { return "", nil },
		WriteManagedSessionFn: func(_, _, sessionID string) error {
			latchedSID = sessionID
			latchCalled++
			return nil
		},
	}

	// Switch gauge to UUIDv4 after a brief delay so watcher sees UUIDv7 first.
	go func() {
		time.Sleep(30 * time.Millisecond)
		writeCtxFile(t, projectDir, agent, 50.0, v4SID)
	}()

	runWatcherFor(context.Background(), cfg, em, 120*time.Millisecond)

	if latchCalled == 0 {
		t.Error("WriteManagedSessionFn never called; want latch on first valid UUIDv4 session_id")
	}
	if latchedSID != v4SID {
		t.Errorf("latched session_id = %q; want %q (UUIDv4)", latchedSID, v4SID)
	}
}

// TestWatcher_SelfHealsStaleUUIDv7InManaged verifies that when .managed already
// holds a UUIDv7 (legacy pre-hk-lap state — e.g. daemon implementer SID was
// latched before the latch-time guard landed), the watcher clears it, re-binds
// to the live UUIDv4 gauge, and emits a normal warn event rather than staying
// stuck in no_gauge:foreign_session forever. (Refs: hk-6mp, hk-lap)
func TestWatcher_SelfHealsStaleUUIDv7InManaged(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	agent := "self-heal-agent"

	keeperDir := filepath.Join(projectDir, ".harmonik", "keeper")
	if err := os.MkdirAll(keeperDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	const staleV7SID = "019ebb07-0000-7000-8000-000000000001" // UUIDv7 in .managed
	const liveV4SID = "f7e7210d-1234-4abc-8000-000000000002"  // UUIDv4 in gauge

	// Simulate .managed already holding a stale UUIDv7.
	storedSID := staleV7SID
	var clearedToEmpty bool
	var latchedSID string

	em := &keeper.RecordingEmitter{}
	cfg := keeper.WatcherConfig{
		AgentName:    agent,
		ProjectDir:   projectDir,
		PollInterval: 10 * time.Millisecond,
		WarnPct:      80.0,
		IdleQuiesce:  1 * time.Millisecond,
		Staleness:    120 * time.Second,
		TmuxTarget:   "",
		ReadManagedSessionFn: func(_, _ string) (string, error) {
			return storedSID, nil
		},
		WriteManagedSessionFn: func(_, _, sessionID string) error {
			if sessionID == "" {
				clearedToEmpty = true
				storedSID = "" // reflect the clear for subsequent reads
			} else {
				latchedSID = sessionID
				storedSID = sessionID
			}
			return nil
		},
	}

	// Write a UUIDv4 gauge above threshold — this is the live captain session.
	writeCtxFile(t, projectDir, agent, 85.0, liveV4SID)

	runWatcherFor(context.Background(), cfg, em, 120*time.Millisecond)

	// The stale UUIDv7 must have been cleared.
	if !clearedToEmpty {
		t.Error("WriteManagedSessionFn was not called with empty sessionID — stale UUIDv7 not cleared")
	}
	// The live UUIDv4 must have been latched after the clear.
	if latchedSID != liveV4SID {
		t.Errorf("latched session_id = %q; want %q (live UUIDv4 after self-heal)", latchedSID, liveV4SID)
	}
	// And a warn must have fired (not stuck in no_gauge:foreign_session).
	warns := em.EventsOfType(core.EventTypeSessionKeeperWarn)
	if len(warns) == 0 {
		t.Error("want at least one session_keeper_warn after self-heal; got 0 (keeper stuck in foreign_session?)")
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

// TestWatcher_StaleBindingAutoRecovery verifies that when .managed holds a
// session_id that never matches the live gauge (stale/mismatched binding, e.g.
// a conversation-id written instead of a session-id), the watcher auto-clears
// .managed after StaleBindingThreshold consecutive foreign_session ticks, allowing
// the next valid gauge to re-latch.
// Refs: hk-mejt (stale .managed / foreign_session root cause).
func TestWatcher_StaleBindingAutoRecovery(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	agent := "stale-binding-agent"

	keeperDir := filepath.Join(projectDir, ".harmonik", "keeper")
	if err := os.MkdirAll(keeperDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Simulate .managed holding a stale conversation-id (UUIDv4 but wrong UUID).
	const staleSID = "5b5bf51b-dde1-4ec7-8e60-9c9121900f7e" // conversation-id
	const liveSID = "c0a1c545-1234-4abc-8000-000000000001"  // real session-id in gauge

	storedSID := staleSID
	var clearedToEmpty bool
	var latchedSID string

	em := &keeper.RecordingEmitter{}
	cfg := keeper.WatcherConfig{
		AgentName:            agent,
		ProjectDir:           projectDir,
		PollInterval:         10 * time.Millisecond,
		WarnPct:              80.0,
		IdleQuiesce:          1 * time.Millisecond,
		Staleness:            120 * time.Second,
		TmuxTarget:           "",
		StaleBindingThreshold: 3, // clear after 3 consecutive foreign ticks
		ReadManagedSessionFn: func(_, _ string) (string, error) {
			return storedSID, nil
		},
		WriteManagedSessionFn: func(_, _, sessionID string) error {
			if sessionID == "" {
				clearedToEmpty = true
				storedSID = ""
			} else {
				latchedSID = sessionID
				storedSID = sessionID
			}
			return nil
		},
	}

	// Live gauge always has the real session_id — never matches stale .managed.
	writeCtxFile(t, projectDir, agent, 85.0, liveSID)

	// Run long enough for 3+ foreign ticks (3 * 10ms = 30ms) plus re-latch.
	runWatcherFor(context.Background(), cfg, em, 200*time.Millisecond)

	if !clearedToEmpty {
		t.Error("want .managed auto-cleared after StaleBindingThreshold foreign ticks; WriteManagedSessionFn(\"\",...) never called")
	}
	if latchedSID != liveSID {
		t.Errorf("want re-latched to live session_id %q after auto-clear; got %q", liveSID, latchedSID)
	}
	// After re-latch, a warn should fire (gauge pct 85 > threshold 80).
	warns := em.EventsOfType(core.EventTypeSessionKeeperWarn)
	if len(warns) == 0 {
		t.Error("want at least one session_keeper_warn after stale-binding recovery and re-latch; got 0")
	}
}

// TestWatcher_StaleBindingCounterResetsOnMatchingGauge verifies that the
// consecutive-foreign-tick counter resets when a gauge tick matches .managed,
// so a legitimate brief foreign-tick burst (e.g. daemon transient) does not
// trigger auto-clear.
// Refs: hk-mejt.
func TestWatcher_StaleBindingCounterResetsOnMatchingGauge(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	agent := "foreign-reset-agent"

	keeperDir := filepath.Join(projectDir, ".harmonik", "keeper")
	if err := os.MkdirAll(keeperDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	const managedSID = "c0a1c545-1234-4abc-8000-000000000001"
	const foreignSID = "aaaaaaaa-0000-4000-8000-000000000000"

	// Gauge alternates: foreign → managed → foreign → managed …
	// The counter should never reach the threshold because each matching tick resets it.
	tick := 0
	clearCalled := false

	em := &keeper.RecordingEmitter{}
	cfg := keeper.WatcherConfig{
		AgentName:            agent,
		ProjectDir:           projectDir,
		PollInterval:         10 * time.Millisecond,
		WarnPct:              80.0,
		IdleQuiesce:          1 * time.Millisecond,
		Staleness:            120 * time.Second,
		TmuxTarget:           "",
		StaleBindingThreshold: 3,
		ReadManagedSessionFn: func(_, _ string) (string, error) {
			return managedSID, nil
		},
		WriteManagedSessionFn: func(_, _, sessionID string) error {
			if sessionID == "" {
				clearCalled = true
			}
			return nil
		},
	}

	// Alternate gauge between foreign and managed on each poll.
	go func() {
		sids := []string{foreignSID, managedSID, foreignSID, managedSID, foreignSID, managedSID}
		for _, sid := range sids {
			writeCtxFile(t, projectDir, agent, 85.0, sid)
			tick++
			time.Sleep(15 * time.Millisecond)
		}
	}()

	runWatcherFor(context.Background(), cfg, em, 200*time.Millisecond)

	if clearCalled {
		t.Error("want .managed NOT auto-cleared when foreign ticks are interrupted by matching ticks; got auto-clear")
	}
}

// TestWatcher_SkipsNoGaugeOnTransientUUIDv7WhenManagedIsUUIDv4 verifies that
// when .managed holds a UUIDv4 (the real captain session) and captain.ctx is
// transiently overwritten with a UUIDv7 (daemon implementer dispatch), the
// watcher skips the tick without emitting no_gauge:foreign_session — retaining
// the last good gauge state instead. (Refs: hk-y1h, hk-3js5m)
func TestWatcher_SkipsNoGaugeOnTransientUUIDv7WhenManagedIsUUIDv4(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	agent := "uuid4-managed-uuid7-ctx-agent"

	keeperDir := filepath.Join(projectDir, ".harmonik", "keeper")
	if err := os.MkdirAll(keeperDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	const managedV4SID = "f7e7210d-1234-4abc-8000-000000000002" // UUIDv4 in .managed
	const ctxV7SID = "019ebb07-0000-7000-8000-000000000001"     // UUIDv7 in .ctx (transient daemon overwrite)

	em := &keeper.RecordingEmitter{}
	cfg := keeper.WatcherConfig{
		AgentName:    agent,
		ProjectDir:   projectDir,
		PollInterval: 10 * time.Millisecond,
		WarnPct:      80.0,
		IdleQuiesce:  1 * time.Millisecond,
		Staleness:    120 * time.Second,
		TmuxTarget:   "",
		// .managed already bound to the captain's UUIDv4.
		ReadManagedSessionFn:  func(_, _ string) (string, error) { return managedV4SID, nil },
		WriteManagedSessionFn: func(_, _, _ string) error { return nil },
	}

	// Write a gauge with a UUIDv7 — simulates daemon dispatch polluting captain.ctx.
	writeCtxFile(t, projectDir, agent, 85.0, ctxV7SID)
	runWatcherFor(context.Background(), cfg, em, 80*time.Millisecond)

	// Must emit NO no_gauge events — skip-and-retain means the last good gauge is kept.
	noGaugeEvents := em.EventsOfType(core.EventTypeSessionKeeperNoGauge)
	if len(noGaugeEvents) != 0 {
		t.Errorf("want 0 session_keeper_no_gauge on transient UUIDv7 when managed is UUIDv4; got %d", len(noGaugeEvents))
	}
	// Must also emit no warn — the gauge pct (85) is above threshold but we skipped
	// processing entirely, so warnArmed is not advanced and no warn fires.
	warns := em.EventsOfType(core.EventTypeSessionKeeperWarn)
	if len(warns) != 0 {
		t.Errorf("want 0 session_keeper_warn on transient UUIDv7 skip; got %d", len(warns))
	}
}

// TestWatcher_SkipsUppercaseSessionIDLatch verifies that the watcher does NOT
// latch an uppercase session_id into .managed. Claude Code may occasionally emit
// the conversation/transcript-dir UUID (uppercase UUIDv4) as session_id; latching
// it would poison .managed and cause permanent foreign_session noise until the
// operator ran 'keeper rebind'. The primary fix is lowercase normalisation in
// keeper-statusline.sh; this guard is defense-in-depth. Refs: hk-mzdm.
func TestWatcher_SkipsUppercaseSessionIDLatch(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	agent := "uppercase-skip-agent"

	keeperDir := filepath.Join(projectDir, ".harmonik", "keeper")
	if err := os.MkdirAll(keeperDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

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
		// No binding — simulates post-clear state.
		ReadManagedSessionFn: func(_, _ string) (string, error) { return "", nil },
		WriteManagedSessionFn: func(_, _, _ string) error {
			latchCalled++
			return nil
		},
	}

	// Uppercase UUIDv4 — the conversation/transcript-dir UUID format Claude Code
	// may write as session_id (incident value: 5B5BF51B-..., hk-mzdm).
	writeCtxFile(t, projectDir, agent, 50.0, "5B5BF51B-ABCD-4DEF-8000-000000000001")
	runWatcherFor(context.Background(), cfg, em, 80*time.Millisecond)

	if latchCalled != 0 {
		t.Errorf("WriteManagedSessionFn called %d time(s); want 0 — uppercase session_id must not be latched", latchCalled)
	}
	noGaugeEvents := em.EventsOfType(core.EventTypeSessionKeeperNoGauge)
	if len(noGaugeEvents) == 0 {
		t.Error("want at least one session_keeper_no_gauge for uppercase session_id skip; got none")
	}
}

// TestWatcher_FlapCooldownSuppressesRelatch verifies that when multiple rapid
// auto-clears fire within AutoClearCooldown (flap thrashing — two alternating
// non-daemon UUIDv4 sessions overwriting .ctx), latch is suppressed after
// AutoClearMaxAttempts clears. The watcher emits foreign_session instead of
// silently re-latching the wrong session. Refs: hk-mzdm.
func TestWatcher_FlapCooldownSuppressesRelatch(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	agent := "flap-agent"

	keeperDir := filepath.Join(projectDir, ".harmonik", "keeper")
	if err := os.MkdirAll(keeperDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	var mu sync.Mutex
	storedSID := "sess-A" // initial bound session
	clearCount := 0
	var latchedSIDs []string

	readFn := func(_, _ string) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		return storedSID, nil
	}
	writeFn := func(_, _, sid string) error {
		mu.Lock()
		defer mu.Unlock()
		if sid == "" {
			clearCount++
			storedSID = ""
		} else {
			latchedSIDs = append(latchedSIDs, sid)
			storedSID = sid
		}
		return nil
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
		// threshold=1: one foreign tick → auto-clear.
		StaleBindingThreshold: 1,
		// Long cooldown (500ms) so both auto-clears are within the rapid window.
		AutoClearCooldown:    500 * time.Millisecond,
		AutoClearMaxAttempts: 2,
		// High threshold so the 50ms window cannot trigger self-recovery
		// (would need 100×10ms = 1s). This test exercises the INITIAL suppression
		// window; TestWatcher_FlapCooldownSelfRecovers covers self-recovery.
		SuppressRecoverThreshold: 100,
		ReadManagedSessionFn:     readFn,
		WriteManagedSessionFn:    writeFn,
		// No-ops: test uses in-memory readFn/writeFn; no real file needed.
		WriteSuppressFn: func(_, _ string) error { return nil },
		ClearSuppressFn: func(_, _ string) error { return nil },
		ReadSuppressFn:  func(_, _ string) bool { return false },
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		w := keeper.NewWatcher(cfg, em)
		_ = w.Run(ctx) //nolint:errcheck // context.Canceled expected
	}()

	// Phase 1: "sess-B" is foreign relative to "sess-A" → auto-clear + re-latch.
	writeCtxFile(t, projectDir, agent, 50.0, "sess-B")
	time.Sleep(50 * time.Millisecond) // enough for clear + re-latch (2 ticks)

	// Phase 2: "sess-C" is now foreign relative to "sess-B" → second rapid auto-clear
	// → latch suppression activates (consecutiveRapidClears=2 ≥ maxAttempts=2).
	writeCtxFile(t, projectDir, agent, 50.0, "sess-C")
	time.Sleep(50 * time.Millisecond)

	// Phase 3: write a new UUIDv4 session — should NOT be latched (suppressed;
	// SuppressRecoverThreshold=100 means ~1s needed for self-recovery, far beyond 50ms).
	const goodSID = "f7e7210d-1234-4abc-8000-000000000002"
	writeCtxFile(t, projectDir, agent, 50.0, goodSID)
	time.Sleep(50 * time.Millisecond)

	cancel()
	<-done

	mu.Lock()
	cc := clearCount
	ls := append([]string(nil), latchedSIDs...)
	mu.Unlock()

	// At least 2 auto-clears must have fired to trigger suppression.
	if cc < 2 {
		t.Errorf("want ≥2 auto-clears for flap detection; got %d", cc)
	}

	// goodSID must NOT have been latched — flap suppression should be active
	// within the 50ms window (SuppressRecoverThreshold=100 prevents self-recovery).
	for _, sid := range ls {
		if sid == goodSID {
			t.Errorf("goodSID %q was latched despite flap-cooldown suppression — latch must be blocked after rapid clears", goodSID)
		}
	}
}

// TestWatcher_FlapCooldownSelfRecovers verifies that after latch suppression is
// triggered by rapid auto-clears, the keeper auto-clears the suppression and
// re-latches once the same clean session_id appears for SuppressRecoverThreshold
// consecutive ticks. This allows autonomous captain/crew sessions to self-recover
// from a flap-cooldown trip without a human 'keeper rebind'. Refs: hk-0tvm.
func TestWatcher_FlapCooldownSelfRecovers(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	agent := "flap-selfheal-agent"

	keeperDir := filepath.Join(projectDir, ".harmonik", "keeper")
	if err := os.MkdirAll(keeperDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	var mu sync.Mutex
	storedSID := "sess-A"
	var latchedSIDs []string

	readFn := func(_, _ string) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		return storedSID, nil
	}
	writeFn := func(_, _, sid string) error {
		mu.Lock()
		defer mu.Unlock()
		if sid == "" {
			storedSID = ""
		} else {
			latchedSIDs = append(latchedSIDs, sid)
			storedSID = sid
		}
		return nil
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
		// threshold=1 → fast auto-clears; cooldown=500ms → both fit in rapid window.
		StaleBindingThreshold: 1,
		AutoClearCooldown:     500 * time.Millisecond,
		AutoClearMaxAttempts:  2,
		// SuppressRecoverThreshold=3 → needs 3 consecutive stable ticks (30ms at
		// 10ms poll) to self-recover. The 150ms window in phase 3 is ample.
		SuppressRecoverThreshold: 3,
		ReadManagedSessionFn:     readFn,
		WriteManagedSessionFn:    writeFn,
		WriteSuppressFn:          func(_, _ string) error { return nil },
		ClearSuppressFn:          func(_, _ string) error { return nil },
		ReadSuppressFn:           func(_, _ string) bool { return false },
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		w := keeper.NewWatcher(cfg, em)
		_ = w.Run(ctx) //nolint:errcheck // context.Canceled expected
	}()

	// Phase 1: trigger two rapid auto-clears to activate flap suppression.
	writeCtxFile(t, projectDir, agent, 50.0, "sess-B")
	time.Sleep(50 * time.Millisecond)
	writeCtxFile(t, projectDir, agent, 50.0, "sess-C")
	time.Sleep(50 * time.Millisecond)

	// Phase 2: write a stable clean UUIDv4 session_id for 150ms. With
	// SuppressRecoverThreshold=3 and PollInterval=10ms, 3 consecutive stable ticks
	// take ~30ms, well within the 150ms window. Self-recovery should fire.
	const stableSID = "aabb1234-1234-4abc-8000-000000000099"
	writeCtxFile(t, projectDir, agent, 50.0, stableSID)
	time.Sleep(150 * time.Millisecond) // >>3 ticks for self-recovery

	cancel()
	<-done

	mu.Lock()
	ls := append([]string(nil), latchedSIDs...)
	mu.Unlock()

	// stableSID must have been latched after self-recovery.
	found := false
	for _, sid := range ls {
		if sid == stableSID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("stableSID %q was not latched after self-recovery (SuppressRecoverThreshold=3, 150ms window) — want self-recover; latched: %v", stableSID, ls)
	}
}

// TestWatcher_FlapCooldownClearsOnStableBind verifies that flap-cooldown
// suppression is automatically cleared when the watcher confirms a stable
// session binding (gauge session_id matches .managed). This allows normal latch
// behaviour to resume after 'keeper rebind' restores a correct binding.
// Refs: hk-mzdm.
func TestWatcher_FlapCooldownClearsOnStableBind(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	agent := "flap-clear-agent"

	keeperDir := filepath.Join(projectDir, ".harmonik", "keeper")
	if err := os.MkdirAll(keeperDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	var mu sync.Mutex
	storedSID := "sess-A"
	clearCount := 0
	var latchedSIDs []string

	readFn := func(_, _ string) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		return storedSID, nil
	}
	writeFn := func(_, _, sid string) error {
		mu.Lock()
		defer mu.Unlock()
		if sid == "" {
			clearCount++
			storedSID = ""
		} else {
			latchedSIDs = append(latchedSIDs, sid)
			storedSID = sid
		}
		return nil
	}

	em := &keeper.RecordingEmitter{}
	cfg := keeper.WatcherConfig{
		AgentName:             agent,
		ProjectDir:            projectDir,
		PollInterval:          10 * time.Millisecond,
		WarnPct:               80.0,
		IdleQuiesce:           1 * time.Millisecond,
		Staleness:             120 * time.Second,
		TmuxTarget:            "",
		StaleBindingThreshold: 1,
		AutoClearCooldown:     500 * time.Millisecond,
		AutoClearMaxAttempts:  2,
		ReadManagedSessionFn:  readFn,
		WriteManagedSessionFn: writeFn,
		WriteSuppressFn:       func(_, _ string) error { return nil },
		ClearSuppressFn:       func(_, _ string) error { return nil },
		ReadSuppressFn:        func(_, _ string) bool { return false },
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		w := keeper.NewWatcher(cfg, em)
		_ = w.Run(ctx) //nolint:errcheck // context.Canceled expected
	}()

	// Trigger two rapid auto-clears to activate flap suppression.
	writeCtxFile(t, projectDir, agent, 50.0, "sess-B")
	time.Sleep(50 * time.Millisecond)
	writeCtxFile(t, projectDir, agent, 50.0, "sess-C")
	time.Sleep(50 * time.Millisecond)

	// Simulate operator 'keeper rebind': write known-good SID directly into .managed.
	// The watcher will see managedSID="good-sess" and when gauge also has "good-sess",
	// the stable-bind path fires and clears latchSuppressed.
	// Note: with SuppressRecoverThreshold=1 (default from StaleBindingThreshold),
	// the watcher may self-recover before this rebind. Both paths result in ≥3
	// total auto-clears, which is what this test verifies.
	const goodSID = "aabbccdd-1234-4abc-8000-000000000003"
	mu.Lock()
	storedSID = goodSID // simulate rebind writing into .managed
	mu.Unlock()
	writeCtxFile(t, projectDir, agent, 50.0, goodSID) // gauge matches managed
	time.Sleep(50 * time.Millisecond)

	// After stable bind, write another "foreign" session and verify it causes a
	// normal auto-clear (not blocked by stale latchSuppressed).
	writeCtxFile(t, projectDir, agent, 50.0, "sess-D")
	time.Sleep(50 * time.Millisecond)

	cancel()
	<-done

	mu.Lock()
	cc := clearCount
	mu.Unlock()

	// After stable bind clears suppression, a new foreign session should cause
	// a new auto-clear (normal behaviour resuming). So total clears ≥ 3.
	if cc < 3 {
		t.Errorf("want ≥3 auto-clears (2 for suppression + 1 post-stable-clear); got %d — latch suppression may not have cleared on stable bind", cc)
	}
}

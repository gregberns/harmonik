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
	"github.com/gregberns/harmonik/internal/substrate"
)

func driveWatcherFakeClockFrom(t *testing.T, start time.Time, cfg keeper.WatcherConfig, em keeper.Emitter, ticks int) {
	t.Helper()
	driveWatcherLockstep(t, substrate.NewFakeClock(start), cfg, em, ticks)
}

func driveWatcherLockstep(t *testing.T, fake *substrate.FakeClock, cfg keeper.WatcherConfig, em keeper.Emitter, ticks int) {
	t.Helper()

	cfg.Clock = fake
	interval := cfg.PollInterval
	if interval <= 0 {
		interval = 5 * time.Millisecond
	}

	tickCh := make(chan struct{})
	prev := cfg.OnPollTickFn
	cfg.OnPollTickFn = func() {
		if prev != nil {
			prev()
		}
		tickCh <- struct{}{}
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		w := keeper.NewWatcher(cfg, em)
		_ = w.Run(ctx) //nolint:errcheck // context.Canceled is expected
	}()

	fake.BlockUntil(1)
	for i := 0; i <= ticks; i++ {
		fake.Advance(interval)
		<-tickCh
	}
	cancel()
	go func() {
		for {
			select {
			case <-tickCh:
			case <-done:
				return
			}
		}
	}()
	<-done
}

type spyInjector struct {
	mu    sync.Mutex
	calls int
}

func stubWatcherInjectors(cfg *keeper.WatcherConfig, warn func(context.Context, string) error) {
	cfg.InjectFn = warn
	stubText := func(context.Context, string, string) error { return nil }
	cfg.SelfHintInjectFn = stubText
	cfg.MessageInjectFn = stubText
	cfg.DashboardNagInjectFn = stubText
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

func writeCtxFile(t *testing.T, projectDir, agent string, pct float64, sessionID string) {
	t.Helper()
	keeperDir := filepath.Join(projectDir, ".harmonik", "keeper")
	if err := os.MkdirAll(keeperDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	cf := keeper.CtxFile{
		Pct:       pct,
		SessionID: sessionID,
		Ts:        time.Now().UTC().Format(time.RFC3339),
	}
	if err := keeper.WriteCtxFile(projectDir, agent, &cf); err != nil {
		t.Fatalf("WriteCtxFile: %v", err)
	}
}

func writeCtxFileTokens(t *testing.T, projectDir, agent string, pct float64, tokens, windowSize int64, sessionID string) {
	t.Helper()
	keeperDir := filepath.Join(projectDir, ".harmonik", "keeper")
	if err := os.MkdirAll(keeperDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	cf := keeper.CtxFile{
		Pct:        pct,
		Tokens:     tokens,
		WindowSize: windowSize,
		SessionID:  sessionID,
		Ts:         time.Now().UTC().Format(time.RFC3339),
	}
	if err := keeper.WriteCtxFile(projectDir, agent, &cf); err != nil {
		t.Fatalf("WriteCtxFile: %v", err)
	}
}

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

	keeperDir := filepath.Join(projectDir, ".harmonik", "keeper")
	if err := os.MkdirAll(keeperDir, 0o700); err != nil {
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

	writeCtxFile(t, projectDir, agent, 85.0, "sess-abc")

	runWatcherFor(context.Background(), cfg, em, 80*time.Millisecond)

	warns := em.EventsOfType(core.EventTypeSessionKeeperWarn)
	if len(warns) != 1 {
		t.Errorf("want exactly 1 session_keeper_warn; got %d", len(warns))
		return
	}

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
	if err := os.MkdirAll(keeperDir, 0o700); err != nil {
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

	writeCtxFile(t, projectDir, agent, 90.0, "sess-stale")

	time.Sleep(5 * time.Millisecond)

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

	runWatcherFor(context.Background(), cfg, em, 60*time.Millisecond)

	noGauge := em.EventsOfType(core.EventTypeSessionKeeperNoGauge)
	if len(noGauge) == 0 {
		t.Error("want at least one session_keeper_no_gauge when gauge file is absent; got 0")
		return
	}

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
	if err := os.MkdirAll(keeperDir, 0o700); err != nil {
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
	}
	stubWatcherInjectors(&cfg, spy.inject)

	writeCtxFile(t, projectDir, agent, 85.0, "sess-inject")

	runWatcherFor(context.Background(), cfg, em, 150*time.Millisecond)

	warns := em.EventsOfType(core.EventTypeSessionKeeperWarn)
	if len(warns) != 1 {
		t.Errorf("want exactly 1 session_keeper_warn; got %d", len(warns))
	}

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
		// WarnCooldown: set to 1ms so the dip-rise cooldown (hk-sol6) doesn't
		// suppress the second upward crossing in this test. Production default is 30s.
		WarnCooldown: 1 * time.Millisecond,
	}

	keeperDir := filepath.Join(projectDir, ".harmonik", "keeper")
	ctxPath := filepath.Join(keeperDir, agent+".ctx")
	if err := os.MkdirAll(keeperDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	writeCtxFile := func(pct float64) {
		t.Helper()
		data, marshalErr := json.Marshal(keeper.CtxFile{
			Pct: pct,
			Ts:  time.Now().UTC().Format(time.RFC3339),
		})
		if marshalErr != nil {
			t.Fatalf("marshal gauge: %v", marshalErr)
		}
		if writeErr := os.WriteFile(ctxPath, append(data, '\n'), 0o600); writeErr != nil {
			t.Fatalf("write gauge: %v", writeErr)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		w := keeper.NewWatcher(cfg, em)
		_ = w.Run(ctx) //nolint:errcheck // context.Canceled expected
	}()

	writeCtxFile(85.0)
	time.Sleep(30 * time.Millisecond)

	writeCtxFile(70.0)
	time.Sleep(30 * time.Millisecond)

	writeCtxFile(90.0)
	time.Sleep(30 * time.Millisecond)

	cancel()
	<-done

	warns := em.EventsOfType(core.EventTypeSessionKeeperWarn)
	if len(warns) < 2 {
		t.Errorf("want ≥2 session_keeper_warn after two upward crossings; got %d", len(warns))
	}
}

// TestWatcher_IgnoresForeignSessionGauge verifies that when the managed binding
// is "sess-expected", the gauge carries "sess-foreign", and the authoritative
// .sid also carries "sess-expected" (matching managed, not the gauge), the
// watcher treats the gauge as absent and emits NO warn event. This is a TRUE
// concurrent foreign session: the .sid does not endorse the gauge's session_id.
// Refs: hk-igt, hk-1tn2.
func TestWatcher_IgnoresForeignSessionGauge(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	agent := "binding-agent"

	keeperDir := filepath.Join(projectDir, ".harmonik", "keeper")
	if err := os.MkdirAll(keeperDir, 0o700); err != nil {
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
		// .sid endorses "sess-expected" (the managed sid), NOT the gauge's sid.
		// This is the true-foreign case: two concurrent sessions, the authoritative
		// .sid does not match the gauge's session_id.
		ReadSidFn: func(_, _ string) (string, time.Time, error) {
			return "sess-expected", time.Time{}, nil
		},
	}

	writeCtxFile(t, projectDir, agent, 90.0, "sess-foreign")

	runWatcherFor(context.Background(), cfg, em, 80*time.Millisecond)

	if warns := em.EventsOfType(core.EventTypeSessionKeeperWarn); len(warns) != 0 {
		t.Errorf("want 0 session_keeper_warn for foreign session; got %d", len(warns))
	}
}

// TestWatcher_AdoptsSameAgentNewSidAfterExternalClear verifies that when the
// managed binding is stale (old-sid from a prior session) but the authoritative
// .sid and the live gauge both carry a new UUIDv4 (the same agent after an
// external /clear), the watcher re-resolves .managed to the new sid and
// continues monitoring — emitting a warn at the high-pct gauge rather than
// treating it as foreign. Refs: hk-1tn2.
func TestWatcher_AdoptsSameAgentNewSidAfterExternalClear(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	agent := "adopt-agent"

	keeperDir := filepath.Join(projectDir, ".harmonik", "keeper")
	if err := os.MkdirAll(keeperDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	const oldSID = "aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa"
	const newSID = "bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb"

	var adoptedSID string
	em := &keeper.RecordingEmitter{}
	cfg := keeper.WatcherConfig{
		AgentName:    agent,
		ProjectDir:   projectDir,
		PollInterval: 10 * time.Millisecond,
		WarnPct:      80.0,
		IdleQuiesce:  1 * time.Millisecond,
		Staleness:    120 * time.Second,
		TmuxTarget:   "",
		// Managed binding is stale (prior session).
		ReadManagedSessionFn: func(_, _ string) (string, error) { return oldSID, nil },
		WriteManagedSessionFn: func(_, _, sessionID string) error {
			adoptedSID = sessionID
			return nil
		},
		// .sid endorses the NEW session_id — same agent, new session after /clear.
		ReadSidFn: func(_, _ string) (string, time.Time, error) {
			return newSID, time.Time{}, nil
		},
	}

	writeCtxFile(t, projectDir, agent, 90.0, newSID)

	runWatcherFor(context.Background(), cfg, em, 80*time.Millisecond)

	if adoptedSID != newSID {
		t.Errorf("want adopted sid %q; got %q", newSID, adoptedSID)
	}

	if warns := em.EventsOfType(core.EventTypeSessionKeeperWarn); len(warns) == 0 {
		t.Errorf("want ≥1 session_keeper_warn after adopt; got 0")
	}
}

// TestWatcher_RejectsConcurrentDifferentSession verifies that when the managed
// binding is "sess-managed", the gauge carries "sess-gauge-other", and the
// authoritative .sid carries "sess-sid-third" (all three differ), the watcher
// rejects the gauge as a true concurrent foreign session and emits NO warn.
// Refs: hk-1tn2.
func TestWatcher_RejectsConcurrentDifferentSession(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	agent := "concurrent-agent"

	keeperDir := filepath.Join(projectDir, ".harmonik", "keeper")
	if err := os.MkdirAll(keeperDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	const managedSID = "cccccccc-cccc-4ccc-cccc-cccccccccccc"
	const gaugeSID = "dddddddd-dddd-4ddd-dddd-dddddddddddd"
	const sidSID = "eeeeeeee-eeee-4eee-eeee-eeeeeeeeeeee"

	em := &keeper.RecordingEmitter{}
	cfg := keeper.WatcherConfig{
		AgentName:             agent,
		ProjectDir:            projectDir,
		PollInterval:          10 * time.Millisecond,
		WarnPct:               80.0,
		IdleQuiesce:           1 * time.Millisecond,
		Staleness:             120 * time.Second,
		TmuxTarget:            "",
		ReadManagedSessionFn:  func(_, _ string) (string, error) { return managedSID, nil },
		WriteManagedSessionFn: func(_, _, _ string) error { return nil },
		// .sid does NOT endorse the gauge's session_id.
		ReadSidFn: func(_, _ string) (string, time.Time, error) {
			return sidSID, time.Time{}, nil
		},
	}

	writeCtxFile(t, projectDir, agent, 90.0, gaugeSID)

	runWatcherFor(context.Background(), cfg, em, 80*time.Millisecond)

	if warns := em.EventsOfType(core.EventTypeSessionKeeperWarn); len(warns) != 0 {
		t.Errorf("want 0 session_keeper_warn for concurrent foreign session; got %d", len(warns))
	}
}

// TestWatcher_NoAdoptWhenSidMalformed verifies that when the managed binding is
// stale but the .sid file carries a malformed value (not a valid UUIDv4), the
// watcher does NOT adopt the new gauge session_id — it rejects as foreign.
// A corrupt or absent .sid fails closed. Refs: hk-1tn2.
func TestWatcher_NoAdoptWhenSidMalformed(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	agent := "malformed-sid-agent"

	keeperDir := filepath.Join(projectDir, ".harmonik", "keeper")
	if err := os.MkdirAll(keeperDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	const oldSID = "ffffffff-ffff-4fff-ffff-ffffffffffff"
	const newSID = "11111111-1111-4111-1111-111111111111"

	adoptCalled := false
	em := &keeper.RecordingEmitter{}
	cfg := keeper.WatcherConfig{
		AgentName:            agent,
		ProjectDir:           projectDir,
		PollInterval:         10 * time.Millisecond,
		WarnPct:              80.0,
		IdleQuiesce:          1 * time.Millisecond,
		Staleness:            120 * time.Second,
		TmuxTarget:           "",
		ReadManagedSessionFn: func(_, _ string) (string, error) { return oldSID, nil },
		WriteManagedSessionFn: func(_, _, _ string) error {
			adoptCalled = true
			return nil
		},
		// .sid is malformed — fails the UUIDv4 check.
		ReadSidFn: func(_, _ string) (string, time.Time, error) {
			return "not-a-uuid", time.Time{}, nil
		},
	}

	writeCtxFile(t, projectDir, agent, 90.0, newSID)

	runWatcherFor(context.Background(), cfg, em, 80*time.Millisecond)

	if adoptCalled {
		t.Errorf("want no adopt on malformed .sid; WriteManagedSessionFn was called")
	}

	if warns := em.EventsOfType(core.EventTypeSessionKeeperWarn); len(warns) != 0 {
		t.Errorf("want 0 session_keeper_warn for malformed-sid case; got %d", len(warns))
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
	if err := os.MkdirAll(keeperDir, 0o700); err != nil {
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
func TestWatcher_RespawnFiredWhenGaugeAbsentAndPaneIdle(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	agent := "respawn-agent"

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
	}
	stubWatcherInjectors(&cfg, func(context.Context, string) error { return nil })

	keeperDir := filepath.Join(projectDir, ".harmonik", "keeper")
	if err := os.MkdirAll(keeperDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

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
	}
	stubWatcherInjectors(&cfg, func(context.Context, string) error { return nil })

	keeperDir := filepath.Join(projectDir, ".harmonik", "keeper")
	if err := os.MkdirAll(keeperDir, 0o700); err != nil {
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
	}
	stubWatcherInjectors(&cfg, func(context.Context, string) error { return nil })

	keeperDir := filepath.Join(projectDir, ".harmonik", "keeper")
	if err := os.MkdirAll(keeperDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	runWatcherFor(context.Background(), cfg, em, 300*time.Millisecond)

	events := em.EventsOfType(core.EventTypeSessionKeeperRespawnAttempted)
	if len(events) != 1 {
		t.Errorf("want exactly 1 session_keeper_respawn_attempted (cooldown); got %d", len(events))
	}
}

// TestWatcher_ForeignSessionEmitsNoGauge is the alarm-path complement of
// TestWatcher_IgnoresForeignSessionGauge: a true concurrent foreign session
// must not only suppress the warn — it must actively emit
// session_keeper_no_gauge with reason "foreign_session" so the operator can
// see the blind-keeper event rather than a silent pass.
//
// This closes the structural test gap noted in the hk-zole/hk-nlio audit:
// the prior test checked warn==0 but not that the alarm channel fired.
func TestWatcher_ForeignSessionEmitsNoGauge(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	agent := "foreign-alarm-agent"

	keeperDir := filepath.Join(projectDir, ".harmonik", "keeper")
	if err := os.MkdirAll(keeperDir, 0o700); err != nil {
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
		// Managed binding is "sess-mine"; .sid endorses "sess-mine"; gauge carries
		// "sess-foreign" — true concurrent foreign session.
		ReadManagedSessionFn:  func(_, _ string) (string, error) { return "sess-mine", nil },
		WriteManagedSessionFn: func(_, _, _ string) error { return nil },
		ReadSidFn: func(_, _ string) (string, time.Time, error) {
			return "sess-mine", time.Time{}, nil
		},
	}

	writeCtxFile(t, projectDir, agent, 90.0, "sess-foreign")

	runWatcherFor(context.Background(), cfg, em, 80*time.Millisecond)

	if warns := em.EventsOfType(core.EventTypeSessionKeeperWarn); len(warns) != 0 {
		t.Errorf("want 0 session_keeper_warn for foreign session; got %d", len(warns))
	}

	noGauge := em.EventsOfType(core.EventTypeSessionKeeperNoGauge)
	foreignCount := 0
	for _, ev := range noGauge {
		var p core.SessionKeeperNoGaugePayload
		if err := json.Unmarshal(ev.Payload, &p); err == nil && p.Reason == "foreign_session" {
			foreignCount++
		}
	}
	if foreignCount == 0 {
		t.Errorf("want ≥1 no_gauge:foreign_session event (blind-keeper alarm); got 0 (total no_gauge events: %d)", len(noGauge))
	}
}

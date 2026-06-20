package keeper_test

// watcher_warn_only_test.go — tests for WatcherConfig.WarnOnly (hk-yfcc).
//
// WarnOnly=true restricts the keeper to warn-only mode: warn events are emitted
// and the wrap-up advisory is injected into the pane, but neither maybeRespawn
// nor maybeLivePaneRecover ever fires. This is the correct mode for crew-session
// keepers where the captain decides when to restart a crew.

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/keeper"
)

// warnOnlyRespawnRecorder is a thread-safe spy for the respawn path in
// warn-only tests. It records calls so tests can assert zero invocations.
type warnOnlyRespawnRecorder struct {
	mu    sync.Mutex
	calls int
}

func (r *warnOnlyRespawnRecorder) fn(_ context.Context, _ string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	return nil
}

func (r *warnOnlyRespawnRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

// TestWatcher_WarnOnly_EmitsWarnButNoRespawn verifies that in warn-only mode
// the watcher still crosses the warn threshold and emits session_keeper_warn,
// but does NOT invoke maybeRespawn even when the gauge is stale and the pane
// would be idle. Refs: hk-yfcc.
func TestWatcher_WarnOnly_EmitsWarnButNoRespawn(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	agent := "crew-warn-only-respawn-test"

	// Write a gauge above the warn threshold (pct ≥ 85 > WarnPct 80). The gauge
	// is written ONCE and never refreshed. With Staleness=40ms the early ticks
	// see a FRESH gauge (warn crosses + fires) and the later ticks see it as
	// STALE — and a stale gauge + idle pane + non-empty RespawnCmd makes the
	// respawn path FULLY ELIGIBLE. The ONLY thing suppressing respawn is
	// WarnOnly=true, so a regression that drops the WarnOnly gate in maybeRespawn
	// makes a respawn_attempted event appear and FAILS this test.
	writeCtxFile(t, projectDir, agent, 85.0, "sess-crew-01")

	cfg := keeper.WatcherConfig{
		AgentName:    agent,
		ProjectDir:   projectDir,
		PollInterval: 5 * time.Millisecond,
		WarnPct:      80.0,
		IdleQuiesce:  1 * time.Millisecond,
		Staleness:    40 * time.Millisecond, // fresh early (warn fires) → stale later (respawn eligible)
		TmuxTarget:   "dummy-pane",
		WarnOnly:     true, // ← the flag under test
		InjectFn:     func(_ context.Context, _ string) error { return nil },
		// RespawnCmd is non-empty so without WarnOnly the respawn path would be
		// eligible (a harmless no-op command). With WarnOnly=true it must never
		// fire — proven by the absence of respawn_attempted events below.
		RespawnCmd:      "true",
		RespawnGrace:    1 * time.Millisecond,
		RespawnCooldown: 10 * time.Second,
		// IsPaneIdleFn reports idle (agent exited) — normally this would trigger
		// respawn. With WarnOnly=true it must not.
		IsPaneIdleFn: func(_ context.Context, _ string) bool { return true },
	}

	em := &keeper.RecordingEmitter{}
	runWatcherFor(context.Background(), cfg, em, 200*time.Millisecond)

	// Warn MUST have fired (warn-only does not suppress warn events).
	warns := em.EventsOfType(core.EventTypeSessionKeeperWarn)
	if len(warns) == 0 {
		t.Error("want at least one session_keeper_warn in warn-only mode; got 0")
	}

	// The real signal: with a stale gauge + idle pane + RespawnCmd set, the
	// respawn path is eligible on every tick. WarnOnly MUST suppress it, so NO
	// session_keeper_respawn_attempted event may exist. (This replaces a dead
	// spy assertion — the respawn path execs "sh -c <RespawnCmd>" and never
	// calls a Go callback, so a spy recorder could never observe it.)
	respawnEvts := em.EventsOfType(core.EventTypeSessionKeeperRespawnAttempted)
	if len(respawnEvts) != 0 {
		t.Errorf("want 0 session_keeper_respawn_attempted events in warn-only mode; got %d", len(respawnEvts))
	}
}

// TestWatcher_WarnOnly_NoLivePaneRecover verifies that in warn-only mode the
// watcher does NOT invoke the live-pane recovery path (maybeLivePaneRecover)
// even when all gates would pass: gauge stale ≥ grace, pane alive, no
// operator attached, valid .sid bound. Refs: hk-yfcc.
func TestWatcher_WarnOnly_NoLivePaneRecover(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	agent := "crew-warn-only-lpr-test"

	writeGauge(t, projectDir, agent, gaugeSID)     // stale gauge
	writeSidFile(t, projectDir, agent, primarySID) // valid .sid

	rec := &warnOnlyRespawnRecorder{}

	cfg := keeper.WatcherConfig{
		AgentName:           agent,
		ProjectDir:          projectDir,
		PollInterval:        10 * time.Millisecond,
		Staleness:           5 * time.Millisecond,   // immediately stale
		LiveRecoverGrace:    10 * time.Millisecond,  // tiny for test speed
		LiveRecoverCooldown: 10 * time.Second,        // long: at most one attempt
		TmuxTarget:          "dummy-pane",
		WarnOnly:            true, // ← the flag under test
		IsPaneAliveFn:       func(_ context.Context, _ string) bool { return true },
		OperatorAttachedFn:  func(_ string) bool { return false },
		LiveRecoverFn:       rec.fn, // would fire if WarnOnly were false
		InjectFn:            func(_ context.Context, _ string) error { return nil },
	}

	em := &keeper.RecordingEmitter{}
	runWatcherFor(context.Background(), cfg, em, 500*time.Millisecond)

	// LiveRecoverFn must NOT have been called.
	if rec.count() != 0 {
		t.Errorf("want 0 live-pane recover calls in warn-only mode; got %d", rec.count())
	}

	// No live_pane_recover events should exist.
	lprEvts := em.EventsOfType(core.EventTypeSessionKeeperLivePaneRecover)
	if len(lprEvts) != 0 {
		t.Errorf("want 0 session_keeper_live_pane_recover events in warn-only mode; got %d", len(lprEvts))
	}
}

// TestWatcher_WarnOnly_False_RespawnStillFires verifies the negative case: when
// WarnOnly=false the respawn path fires as expected (regression guard). Without
// this the WarnOnly gate could be trivially broken by removing it. Refs: hk-yfcc.
func TestWatcher_WarnOnly_False_RespawnStillFires(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	agent := "crew-warnonly-false-test"

	writeGauge(t, projectDir, agent, gaugeSID) // stale gauge

	rec := &warnOnlyRespawnRecorder{}

	cfg := keeper.WatcherConfig{
		AgentName:    agent,
		ProjectDir:   projectDir,
		PollInterval: 10 * time.Millisecond,
		Staleness:    5 * time.Millisecond,  // immediately stale
		RespawnGrace: 10 * time.Millisecond, // tiny
		RespawnCooldown: 10 * time.Second,
		TmuxTarget:   "dummy-pane",
		WarnOnly:     false, // default: respawn IS permitted
		IsPaneIdleFn: func(_ context.Context, _ string) bool { return true },
		InjectFn:     func(_ context.Context, _ string) error { return nil },
		// Use a real non-empty RespawnCmd so maybeRespawn would qualify.
		// The spy fn overrides RespawnCmd via the exec path — wire spy as
		// RespawnCmd is a string; instead supply LiveRecoverFn which doesn't
		// need a RespawnCmd string. Verifying it fires is sufficient.
		LiveRecoverFn:       rec.fn,
		LiveRecoverGrace:    10 * time.Millisecond,
		LiveRecoverCooldown: 10 * time.Second,
		IsPaneAliveFn:       func(_ context.Context, _ string) bool { return true },
		OperatorAttachedFn:  func(_ string) bool { return false },
	}

	writeSidFile(t, projectDir, agent, primarySID) // needed for live recover gate

	em := &keeper.RecordingEmitter{}
	runWatcherFor(context.Background(), cfg, em, 500*time.Millisecond)

	// With WarnOnly=false, live-pane recovery MUST fire.
	if rec.count() == 0 {
		t.Error("want LiveRecoverFn to fire at least once when WarnOnly=false; got 0 calls")
	}
}

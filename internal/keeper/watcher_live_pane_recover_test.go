package keeper_test

// watcher_live_pane_recover_test.go — unit tests for gauge-INDEPENDENT live-pane
// recovery (hk-75mr). When the gauge is stale but the tmux pane is still ALIVE
// (the agent is hung mid-turn, not exited), neither the threshold cycle nor the
// idle-respawn path can recover it, and a /clear inject cannot reach a hung
// turn. The watcher's last resort is a GATED ForceRestart, fired ONLY when ALL
// gates hold (every gate fail-closed):
//   - stale ≥ LiveRecoverGrace (>> RespawnGrace, anti-premature-reap);
//   - pane alive (IsPaneAliveFn);
//   - NOT operator-attached (OperatorAttachedFn, hk-0t5s keystroke recency);
//   - NOT blocked on an open decision (hitl-decisions K6);
//   - cooldown elapsed (LiveRecoverCooldown);
//   - bound .sid identity is a valid UUIDv4 (hk-8prq) — absent/invalid → no-op.
//
// The observable is the session_keeper_live_pane_recover event (and a spy
// LiveRecoverFn counter). Helper prefix: "lpr".
//
// Reuses writeGauge/writeSidFile/primarySID/gaugeSID (sessionid_test.go),
// runWatcherFor/RecordingEmitter (watcher_test.go), and k6Emit* helpers
// (watcher_decision_exempt_test.go) — all package keeper_test.

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/keeper"
)

// lprRecorder is a thread-safe spy for the LiveRecoverFn action.
type lprRecorder struct {
	mu    sync.Mutex
	calls int
	err   error // returned to the watcher to drive the outcome field
}

func (r *lprRecorder) fn(_ context.Context, _ string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	return r.err
}

func (r *lprRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

// lprConfig builds a WatcherConfig whose live-pane-recovery gates ALL pass by
// default (stale gauge ≥ grace, pane alive, no operator, valid .sid bound). The
// RespawnCmd is intentionally empty so the idle-respawn path is inert and the
// ONLY recovery that can fire is live-pane recovery. Callers flip individual
// gates to test fail-closed behavior. EventsJSONLPath is left to applyDefaults.
func lprConfig(projectDir, agent string, recover func(context.Context, string) error) keeper.WatcherConfig {
	return keeper.WatcherConfig{
		AgentName:           agent,
		ProjectDir:          projectDir,
		PollInterval:        10 * time.Millisecond,
		Staleness:           5 * time.Millisecond,  // gauge immediately stale
		LiveRecoverGrace:    10 * time.Millisecond, // tiny for test speed
		LiveRecoverCooldown: 10 * time.Second,      // long: at most one attempt per run
		TmuxTarget:          "dummy-pane",
		IsPaneAliveFn:       func(_ context.Context, _ string) bool { return true },
		OperatorAttachedFn:  func(_ string) bool { return false },
		LiveRecoverFn:       recover,
		InjectFn:            func(_ context.Context, _ string) error { return nil },
	}
}

func lprEvents(em *keeper.RecordingEmitter) []keeper.EmittedEvent {
	return em.EventsOfType(core.EventTypeSessionKeeperLivePaneRecover)
}

// TestWatcher_LivePaneRecover_FiresWhenStalePaneAliveValidSid is the positive
// case: gauge stale ≥ grace, pane alive, no operator attached, valid .sid bound
// → recovery fires exactly once. The event carries outcome="ok" and the bound
// UUIDv4 session_id.
func TestWatcher_LivePaneRecover_FiresWhenStalePaneAliveValidSid(t *testing.T) {
	t.Parallel()
	projectDir := t.TempDir()
	agent := "lpr-fire-agent"

	writeGauge(t, projectDir, agent, gaugeSID)     // .ctx exists → STALE branch (not absent)
	writeSidFile(t, projectDir, agent, primarySID) // valid UUIDv4 bound identity

	rec := &lprRecorder{}
	em := &keeper.RecordingEmitter{}
	// Generous run window so even under CPU contention (the gate runs all
	// packages' tests in parallel) several poll ticks elapse past the tiny grace.
	runWatcherFor(context.Background(), lprConfig(projectDir, agent, rec.fn), em, 1500*time.Millisecond)

	if rec.count() == 0 {
		t.Fatal("want LiveRecoverFn to fire at least once; got 0 calls")
	}
	events := lprEvents(em)
	if len(events) == 0 {
		t.Fatal("want ≥1 session_keeper_live_pane_recover event; got 0")
	}
	var pl core.SessionKeeperLivePaneRecoverPayload
	if err := json.Unmarshal(events[0].Payload, &pl); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if pl.AgentName != agent {
		t.Errorf("payload.AgentName = %q; want %q", pl.AgentName, agent)
	}
	if pl.Outcome != "ok" {
		t.Errorf("payload.Outcome = %q; want \"ok\"", pl.Outcome)
	}
	if pl.SessionID != primarySID {
		t.Errorf("payload.SessionID = %q; want bound .sid %q", pl.SessionID, primarySID)
	}
}

// TestWatcher_LivePaneRecover_SkippedWhenPaneIdle: a pane that is NOT alive (the
// agent has exited → idle-respawn territory, not live-hang) must not trigger a
// live-pane ForceRestart.
func TestWatcher_LivePaneRecover_SkippedWhenPaneIdle(t *testing.T) {
	t.Parallel()
	projectDir := t.TempDir()
	agent := "lpr-idle-agent"
	writeGauge(t, projectDir, agent, gaugeSID)
	writeSidFile(t, projectDir, agent, primarySID)

	rec := &lprRecorder{}
	em := &keeper.RecordingEmitter{}
	cfg := lprConfig(projectDir, agent, rec.fn)
	cfg.IsPaneAliveFn = func(_ context.Context, _ string) bool { return false } // pane idle
	runWatcherFor(context.Background(), cfg, em, 200*time.Millisecond)

	if rec.count() != 0 {
		t.Errorf("pane not alive: want 0 LiveRecoverFn calls; got %d", rec.count())
	}
	if n := len(lprEvents(em)); n != 0 {
		t.Errorf("pane not alive: want 0 recover events; got %d", n)
	}
}

// TestWatcher_LivePaneRecover_SkippedWhenOperatorAttached: never force-restart a
// pane a human operator is actively driving (hk-0t5s keystroke recency).
func TestWatcher_LivePaneRecover_SkippedWhenOperatorAttached(t *testing.T) {
	t.Parallel()
	projectDir := t.TempDir()
	agent := "lpr-operator-agent"
	writeGauge(t, projectDir, agent, gaugeSID)
	writeSidFile(t, projectDir, agent, primarySID)

	rec := &lprRecorder{}
	em := &keeper.RecordingEmitter{}
	cfg := lprConfig(projectDir, agent, rec.fn)
	cfg.OperatorAttachedFn = func(_ string) bool { return true } // operator typing
	runWatcherFor(context.Background(), cfg, em, 200*time.Millisecond)

	if rec.count() != 0 {
		t.Errorf("operator attached: want 0 LiveRecoverFn calls; got %d", rec.count())
	}
	if n := len(lprEvents(em)); n != 0 {
		t.Errorf("operator attached: want 0 recover events; got %d", n)
	}
}

// TestWatcher_LivePaneRecover_FailsClosedWhenSidAbsent: with NO .sid channel the
// bound identity is unknown → recovery fails CLOSED (no force-restart).
func TestWatcher_LivePaneRecover_FailsClosedWhenSidAbsent(t *testing.T) {
	t.Parallel()
	projectDir := t.TempDir()
	agent := "lpr-nosid-agent"
	writeGauge(t, projectDir, agent, gaugeSID) // gauge present, but NO .sid written

	rec := &lprRecorder{}
	em := &keeper.RecordingEmitter{}
	runWatcherFor(context.Background(), lprConfig(projectDir, agent, rec.fn), em, 200*time.Millisecond)

	if rec.count() != 0 {
		t.Errorf("absent .sid: must fail closed; want 0 LiveRecoverFn calls, got %d", rec.count())
	}
	if n := len(lprEvents(em)); n != 0 {
		t.Errorf("absent .sid: want 0 recover events; got %d", n)
	}
}

// TestWatcher_LivePaneRecover_FailsClosedWhenSidInvalid: a present-but-invalid
// .sid (UUIDv7 daemon implementer id) is NOT a trustworthy bound identity →
// recovery fails CLOSED.
func TestWatcher_LivePaneRecover_FailsClosedWhenSidInvalid(t *testing.T) {
	t.Parallel()
	projectDir := t.TempDir()
	agent := "lpr-badsid-agent"
	writeGauge(t, projectDir, agent, gaugeSID)
	writeSidFile(t, projectDir, agent, "33333333-3333-7333-8333-333333333333") // UUIDv7

	rec := &lprRecorder{}
	em := &keeper.RecordingEmitter{}
	runWatcherFor(context.Background(), lprConfig(projectDir, agent, rec.fn), em, 200*time.Millisecond)

	if rec.count() != 0 {
		t.Errorf("invalid .sid: must fail closed; want 0 LiveRecoverFn calls, got %d", rec.count())
	}
	if n := len(lprEvents(em)); n != 0 {
		t.Errorf("invalid .sid: want 0 recover events; got %d", n)
	}
}

// TestWatcher_LivePaneRecover_SkippedBeforeGrace: a stale gauge that has NOT yet
// aged past LiveRecoverGrace must not trigger recovery (anti-premature-reap).
func TestWatcher_LivePaneRecover_SkippedBeforeGrace(t *testing.T) {
	t.Parallel()
	projectDir := t.TempDir()
	agent := "lpr-grace-agent"
	writeGauge(t, projectDir, agent, gaugeSID)
	writeSidFile(t, projectDir, agent, primarySID)

	rec := &lprRecorder{}
	em := &keeper.RecordingEmitter{}
	cfg := lprConfig(projectDir, agent, rec.fn)
	cfg.LiveRecoverGrace = 10 * time.Second // never reached within the 200ms run
	runWatcherFor(context.Background(), cfg, em, 200*time.Millisecond)

	if rec.count() != 0 {
		t.Errorf("before grace: want 0 LiveRecoverFn calls; got %d", rec.count())
	}
	if n := len(lprEvents(em)); n != 0 {
		t.Errorf("before grace: want 0 recover events; got %d", n)
	}
}

// TestWatcher_LivePaneRecover_CooldownPreventsDouble: across many stale ticks the
// long cooldown allows exactly one recovery attempt.
func TestWatcher_LivePaneRecover_CooldownPreventsDouble(t *testing.T) {
	t.Parallel()
	projectDir := t.TempDir()
	agent := "lpr-cooldown-agent"
	writeGauge(t, projectDir, agent, gaugeSID)
	writeSidFile(t, projectDir, agent, primarySID)

	rec := &lprRecorder{}
	em := &keeper.RecordingEmitter{}
	cfg := lprConfig(projectDir, agent, rec.fn)
	cfg.LiveRecoverCooldown = 10 * time.Second // only one attempt allowed in the run
	// Generous window so recovery reliably fires once even under contention; the
	// 10s cooldown (>> window) guarantees it cannot fire a second time.
	runWatcherFor(context.Background(), cfg, em, 1500*time.Millisecond)

	if rec.count() != 1 {
		t.Errorf("cooldown: want exactly 1 LiveRecoverFn call across the run; got %d", rec.count())
	}
	if n := len(lprEvents(em)); n != 1 {
		t.Errorf("cooldown: want exactly 1 recover event; got %d", n)
	}
}

// TestWatcher_LivePaneRecover_ExemptWhenBlockedOnDecision: an agent that is the
// blocked_agent of an OPEN decision with a fresh (Online) heartbeat is BLOCKED,
// not hung — the live-pane reaper must skip it (hitl-decisions K6 complement).
func TestWatcher_LivePaneRecover_ExemptWhenBlockedOnDecision(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	projectDir := t.TempDir()
	agent := "lpr-blocked-agent"
	writeGauge(t, projectDir, agent, gaugeSID)
	writeSidFile(t, projectDir, agent, primarySID)

	// Open decision for this agent + a fresh presence beat (now → Online).
	k6EmitNeeded(t, ctx, projectDir, agent)
	k6EmitPresence(t, ctx, projectDir, agent, core.AgentPresenceStatusOnline, time.Now(), core.AgentPresenceReasonRefresh)

	rec := &lprRecorder{}
	em := &keeper.RecordingEmitter{}
	runWatcherFor(ctx, lprConfig(projectDir, agent, rec.fn), em, 200*time.Millisecond)

	if rec.count() != 0 {
		t.Errorf("blocked-on-decision: want 0 LiveRecoverFn calls (exempt); got %d", rec.count())
	}
	if n := len(lprEvents(em)); n != 0 {
		t.Errorf("blocked-on-decision: want 0 recover events; got %d", n)
	}
}

package keeper_test

// watcher_b3_restall_hk9cqtm_test.go — Acceptance corpus #4: B3 watch re-stall
// auto-heals via gated ForceRestart (hk-9cqtm).
//
// Scenario: stale gauge over an ALIVE pane whose TmuxTarget is MANGLED (stale
// session name, wrong format — the hk-5266t class that caused #2…#32 alerts with
// no self-healing path, BUGS.md B3). The watcher must:
//   (a) re-resolve the target via ResolveTmuxTargetFn,
//   (b) fire gated ForceRestart exactly once (cooldown + valid-SID gates),
//   (c) NOT loop, and
//   (d) leave SuppressNoGauge flood suppression (hk-F21) intact.
//
// Layer: L-fake-tmux (IsPaneAliveFn / ResolveTmuxTargetFn are injectable seams;
// no real tmux required). The L-twin loop proof is in
// watcher_b3_restall_hk9cqtm_integration_test.go.
//
// Helper reuse: writeGauge / writeSidFile / primarySID / gaugeSID
// (sessionid_test.go), runWatcherFor / RecordingEmitter (watcher_test.go),
// lprRecorder / lprEvents (watcher_live_pane_recover_test.go).

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/keeper"
)

// b3MangledTarget is the stale/wrong TmuxTarget stored in the watcher config,
// modelling a session that was killed and recreated with a new name.
const b3MangledTarget = "stale-old-session:agent"

// b3ResolvedTarget is the canonical target that ResolveTmuxTargetFn returns
// after re-deriving it from projectDir+agentName.
const b3ResolvedTarget = "harmonik-b3test000000-testwatch:agent"

// b3Config builds a WatcherConfig for the B3 re-stall scenario (acceptance
// corpus #4). TmuxTarget is mangled; IsPaneAliveFn returns false for the
// mangled target but true for the re-resolved canonical one; ResolveTmuxTargetFn
// returns the canonical target. All other live-recover gates pass by default.
// RespawnCmd is intentionally empty so idle-respawn is inert.
func b3Config(projectDir, agent string, recover func(context.Context, string) error) keeper.WatcherConfig {
	return keeper.WatcherConfig{
		AgentName:           agent,
		ProjectDir:          projectDir,
		PollInterval:        10 * time.Millisecond,
		Staleness:           5 * time.Millisecond,  // gauge immediately stale
		LiveRecoverGrace:    10 * time.Millisecond, // tiny for test speed
		LiveRecoverCooldown: 10 * time.Second,      // long: at most one attempt per run
		TmuxTarget:          b3MangledTarget,
		IsPaneAliveFn: func(_ context.Context, target string) bool {
			return target == b3ResolvedTarget // false for mangled, true for resolved
		},
		ResolveTmuxTargetFn: func(_, _ string) string {
			return b3ResolvedTarget
		},
		OperatorAttachedFn: func(_ string) bool { return false },
		LiveRecoverFn:      recover,
		InjectFn:           func(_ context.Context, _ string) error { return nil },
	}
}

// TestWatcher_B3_ReStall_FiresViaReResolvedTarget is the RED→GREEN gate for
// corpus #4: stale gauge + alive pane whose stored TmuxTarget is MANGLED →
// the watcher re-resolves the target and fires ForceRestart at least once.
//
// RED with old code (returns on !IsPaneAliveFn(TmuxTarget) without trying
// re-resolution). GREEN after hk-9cqtm adds ResolveTmuxTargetFn re-try.
func TestWatcher_B3_ReStall_FiresViaReResolvedTarget(t *testing.T) {
	t.Parallel()
	projectDir := t.TempDir()
	agent := "b3-fire-agent"

	writeGauge(t, projectDir, agent, gaugeSID)
	writeSidFile(t, projectDir, agent, primarySID)

	rec := &lprRecorder{}
	em := &keeper.RecordingEmitter{}
	runWatcherFor(context.Background(), b3Config(projectDir, agent, rec.fn), em, 1500*time.Millisecond)

	if rec.count() == 0 {
		t.Fatal("B3 re-stall: want LiveRecoverFn to fire at least once via re-resolved target; got 0 calls")
	}
	events := lprEvents(em)
	if len(events) == 0 {
		t.Fatal("B3 re-stall: want >=1 session_keeper_live_pane_recover event; got 0")
	}
	var pl core.SessionKeeperLivePaneRecoverPayload
	if err := json.Unmarshal(events[0].Payload, &pl); err != nil {
		t.Fatalf("unmarshal live_pane_recover payload: %v", err)
	}
	if pl.Outcome != "ok" {
		t.Errorf("payload.Outcome = %q; want \"ok\"", pl.Outcome)
	}
	if pl.SessionID != primarySID {
		t.Errorf("payload.SessionID = %q; want bound .sid %q", pl.SessionID, primarySID)
	}
}

// TestWatcher_B3_ReStall_CooldownPreventsLoop: across many stale ticks the long
// cooldown allows exactly one recovery attempt even via the re-resolved path.
// Verifies the "no loop" invariant from corpus #4.
func TestWatcher_B3_ReStall_CooldownPreventsLoop(t *testing.T) {
	t.Parallel()
	projectDir := t.TempDir()
	agent := "b3-cooldown-agent"

	writeGauge(t, projectDir, agent, gaugeSID)
	writeSidFile(t, projectDir, agent, primarySID)

	rec := &lprRecorder{}
	em := &keeper.RecordingEmitter{}
	cfg := b3Config(projectDir, agent, rec.fn)
	// cooldown already 10s in b3Config; generous window ensures exactly 1 fire.
	runWatcherFor(context.Background(), cfg, em, 1500*time.Millisecond)

	if rec.count() != 1 {
		t.Errorf("B3 cooldown: want exactly 1 LiveRecoverFn call; got %d", rec.count())
	}
	if n := len(lprEvents(em)); n != 1 {
		t.Errorf("B3 cooldown: want exactly 1 recover event; got %d", n)
	}
}

// TestWatcher_B3_ReStall_SuppressNoGaugeIntact: with SuppressNoGauge=true the
// no_gauge flood suppression (hk-F21) remains intact — zero session_keeper_no_gauge
// events — while re-resolved ForceRestart still fires. Ensures the suppression
// seam does NOT block recovery.
func TestWatcher_B3_ReStall_SuppressNoGaugeIntact(t *testing.T) {
	t.Parallel()
	projectDir := t.TempDir()
	agent := "b3-suppress-agent"

	writeGauge(t, projectDir, agent, gaugeSID)
	writeSidFile(t, projectDir, agent, primarySID)

	rec := &lprRecorder{}
	em := &keeper.RecordingEmitter{}
	cfg := b3Config(projectDir, agent, rec.fn)
	cfg.SuppressNoGauge = true // F21: suppress no_gauge flood
	runWatcherFor(context.Background(), cfg, em, 1500*time.Millisecond)

	if n := len(em.EventsOfType(core.EventTypeSessionKeeperNoGauge)); n != 0 {
		t.Errorf("SuppressNoGauge: want 0 no_gauge events; got %d", n)
	}
	if rec.count() == 0 {
		t.Error("SuppressNoGauge: recovery must still fire when SuppressNoGauge=true; got 0 calls")
	}
}

// TestWatcher_B3_ReStall_SkippedWhenReResolveAlsoFails: when BOTH the initial
// TmuxTarget and the re-resolved target fail IsPaneAliveFn, recovery is skipped
// (fail-closed). The re-solve path must not fire on a genuinely dead pane.
func TestWatcher_B3_ReStall_SkippedWhenReResolveAlsoFails(t *testing.T) {
	t.Parallel()
	projectDir := t.TempDir()
	agent := "b3-deadpane-agent"

	writeGauge(t, projectDir, agent, gaugeSID)
	writeSidFile(t, projectDir, agent, primarySID)

	rec := &lprRecorder{}
	em := &keeper.RecordingEmitter{}
	cfg := b3Config(projectDir, agent, rec.fn)
	// Both targets fail the alive check → neither mangled nor resolved is alive.
	cfg.IsPaneAliveFn = func(_ context.Context, _ string) bool { return false }
	runWatcherFor(context.Background(), cfg, em, 200*time.Millisecond)

	if rec.count() != 0 {
		t.Errorf("re-resolve also fails: want 0 LiveRecoverFn calls (fail-closed); got %d", rec.count())
	}
	if n := len(lprEvents(em)); n != 0 {
		t.Errorf("re-resolve also fails: want 0 recover events; got %d", n)
	}
}

// TestWatcher_B3_ReStall_SkippedWhenOperatorAttachedToResolvedTarget: if the
// operator is attached to the RE-RESOLVED target, recovery is still suppressed —
// never force-restart a pane a human is driving, even after re-resolution.
func TestWatcher_B3_ReStall_SkippedWhenOperatorAttachedToResolvedTarget(t *testing.T) {
	t.Parallel()
	projectDir := t.TempDir()
	agent := "b3-opattached-agent"

	writeGauge(t, projectDir, agent, gaugeSID)
	writeSidFile(t, projectDir, agent, primarySID)

	rec := &lprRecorder{}
	em := &keeper.RecordingEmitter{}
	cfg := b3Config(projectDir, agent, rec.fn)
	// Operator attached to the resolved canonical target → must suppress recovery.
	cfg.OperatorAttachedFn = func(target string) bool {
		return target == b3ResolvedTarget
	}
	runWatcherFor(context.Background(), cfg, em, 200*time.Millisecond)

	if rec.count() != 0 {
		t.Errorf("operator attached to resolved target: want 0 LiveRecoverFn calls; got %d", rec.count())
	}
	if n := len(lprEvents(em)); n != 0 {
		t.Errorf("operator attached to resolved target: want 0 recover events; got %d", n)
	}
}

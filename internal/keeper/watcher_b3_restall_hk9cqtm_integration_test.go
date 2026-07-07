//go:build integration

package keeper_test

// watcher_b3_restall_hk9cqtm_integration_test.go — L-twin loop proof for
// Acceptance corpus #4 (B3 watch re-stall auto-heals, hk-9cqtm).
//
// This test closes the "no self-healing path" gap (BUGS.md B3) end-to-end
// against a REAL tmux pane:
//
//  1. A twin session is started (its #{pane_current_command} is the binary —
//     alive, not a shell) and --suppress-statusline-after freezes its gauge
//     while the pane stays alive (the exact hung-mid-turn reality).
//  2. The watcher is configured with a WRONG TmuxTarget (models the mangled
//     target the hk-5266t class produces) and ResolveTmuxTargetFn=nil so the
//     real ResolveTmuxTarget derives the canonical "session:agent" target from
//     projectDir+agentName.
//  3. The watcher detects: stale gauge + IsPaneAlive(mangled)=false →
//     re-resolves → IsPaneAlive(canonical)=true → fires gated ForceRestart
//     exactly once (cooldown gate, valid-SID gate).
//  4. No second recovery fires within the run window (no loop).
//
// The LiveRecoverFn spy SIGNALS only — it never actually restarts the twin —
// so teardown stays deterministic.
//
// Helper prefix reuse: tw (twin harness, cycle_twin_e2e_integration_test.go);
// writeSidFile / primarySID (sessionid_test.go); lprIntRecorder / lprEvents
// (watcher_live_pane_recover_integration_test.go / watcher_live_pane_recover_test.go).
//
// Safety contract: this test creates and destroys ONLY its own uniquely-named
// throwaway tmux session (prefix "hkb3-twin-"). See
// cycle_twin_e2e_integration_test.go for the full safety contract.

import (
	"context"
	"fmt"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/keeper"
)

// TestIntegration_B3_ReStall_AutoHealsNoLoop proves the full B3 auto-recover
// path against a real tmux pane with a WRONG initial TmuxTarget:
//
//   - stale gauge (--suppress-statusline-after freezes the twin's emitter)
//   - IsPaneAlive(mangled-target) = false (wrong session name → tmux probe fails)
//   - ResolveTmuxTargetFn re-derives the canonical session:agent target
//   - IsPaneAlive(canonical) = true (real probe hits the live twin pane)
//   - gated ForceRestart fires exactly once; no second fire within the run window
func TestIntegration_B3_ReStall_AutoHealsNoLoop(t *testing.T) {
	twRequireTmux(t)

	project := t.TempDir()
	agent := fmt.Sprintf("b3twin%d", rand.Int64()) //nolint:gosec // G404: test-local uniqueness
	twin := twBuildTwin(t, project)
	statusline, idleHook := twScripts(t)

	const emitEvery = 150 * time.Millisecond
	_ = twStartTwin(t, twTwinSpec{
		project:       project,
		agent:         agent,
		twin:          twin,
		statusline:    statusline,
		idleHook:      idleHook,
		model:         "claude-opus-4-8 [1m]",
		window:        1_000_000,
		growth:        50_000,
		startTokens:   50_000,
		emitEvery:     emitEvery,
		suppressAfter: 1 * time.Second, // gauge freezes while pane stays alive
	})

	// Wait for the gauge to appear before the suppression deadline.
	if cf := twWaitForCtxTokens(t, project, agent, 50_000, 5*time.Second); cf == nil {
		t.Fatal("b3-twin: .ctx never appeared before suppression deadline")
	}

	// Bind a valid UUIDv4 identity; without it recovery fails closed.
	writeSidFile(t, project, agent, primarySID)

	// Build a WRONG TmuxTarget that does not match the live twin session.
	// ResolveTmuxTargetFn is nil → the real ResolveTmuxTarget is used; it
	// derives "harmonik-<hash>-<agent>:agent" and verifies it via has-session.
	wrongTarget := fmt.Sprintf("harmonik-000000000000-%s:agent", agent)

	rec := &lprIntRecorder{fired: make(chan struct{}, 4)}
	em := &keeper.RecordingEmitter{}
	cfg := keeper.WatcherConfig{
		AgentName:           agent,
		ProjectDir:          project,
		TmuxTarget:          wrongTarget, // intentionally wrong — mangled
		PollInterval:        150 * time.Millisecond,
		Staleness:           500 * time.Millisecond,
		LiveRecoverGrace:    400 * time.Millisecond,
		LiveRecoverCooldown: 10 * time.Second, // long: at most 1 attempt per run window
		OperatorAttachedFn:  func(_ string) bool { return false },
		LiveRecoverFn:       rec.fn,
		InjectFn:            func(_ context.Context, _ string) error { return nil },
		// IsPaneAliveFn = nil → real keeper.IsPaneAlive probes the twin pane.
		// ResolveTmuxTargetFn = nil → real ResolveTmuxTarget re-derives the target.
		// HeartbeatEnabled = false → gauge is allowed to go stale.
		// RespawnCmd = "" → idle-respawn is inert.
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	w := keeper.NewWatcher(cfg, em)
	go func() { _ = w.Run(ctx) }() //nolint:errcheck // context cancel is expected

	// ── gate 1: recovery fires ────────────────────────────────────────────────
	select {
	case <-rec.fired:
		// recovery fired via re-resolved target — good.
	case <-ctx.Done():
		t.Fatalf("b3-twin: live-pane recovery never fired within run window "+
			"(calls=%d); stale gauge over an alive pane + mangled TmuxTarget "+
			"must trigger it via re-resolution", rec.count())
	}

	// Poll for the event (emission races the watcher goroutine).
	deadline := time.Now().Add(2 * time.Second)
	for len(em.EventsOfType(core.EventTypeSessionKeeperLivePaneRecover)) == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}

	if n := len(em.EventsOfType(core.EventTypeSessionKeeperLivePaneRecover)); n == 0 {
		t.Error("b3-twin: want >=1 session_keeper_live_pane_recover event; got 0")
	}

	// ── gate 2: no loop — exactly 1 fire within the 10s cooldown window ───────
	// The watcher continues running to the context deadline. The 10s cooldown
	// must prevent a second attempt within this window. Poll briefly to confirm
	// no second fire lands in the first 2 seconds after the first fire.
	firstFire := rec.count()
	time.Sleep(500 * time.Millisecond)
	cancel()

	if n := rec.count(); n != firstFire {
		t.Errorf("b3-twin: no-loop gate failed: want %d total fires after first; got %d (cooldown must prevent loop)",
			firstFire, n)
	}
}

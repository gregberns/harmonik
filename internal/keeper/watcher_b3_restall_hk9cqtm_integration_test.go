//go:build integration

package keeper_test

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
	canonicalSession := keeper.HarmonikSessionName(project, agent)
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
		sessionName:   canonicalSession,
	})

	if cf := twWaitForCtxTokens(t, project, agent, 50_000, 5*time.Second); cf == nil {
		t.Fatal("b3-twin: .ctx never appeared before suppression deadline")
	}

	writeSidFile(t, project, agent, primarySID)

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
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	w := keeper.NewWatcher(cfg, em)
	go func() { _ = w.Run(ctx) }() //nolint:errcheck // context cancel is expected

	select {
	case <-rec.fired:
	case <-ctx.Done():
		t.Fatalf("b3-twin: live-pane recovery never fired within run window "+
			"(calls=%d); stale gauge over an alive pane + mangled TmuxTarget "+
			"must trigger it via re-resolution", rec.count())
	}

	deadline := time.Now().Add(2 * time.Second)
	for len(em.EventsOfType(core.EventTypeSessionKeeperLivePaneRecover)) == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}

	if n := len(em.EventsOfType(core.EventTypeSessionKeeperLivePaneRecover)); n == 0 {
		t.Error("b3-twin: want >=1 session_keeper_live_pane_recover event; got 0")
	}

	firstFire := rec.count()
	time.Sleep(500 * time.Millisecond)
	cancel()

	if n := rec.count(); n != firstFire {
		t.Errorf("b3-twin: no-loop gate failed: want %d total fires after first; got %d (cooldown must prevent loop)",
			firstFire, n)
	}
}

//go:build integration

package keeper_test

import (
	"context"
	"fmt"
	"math/rand/v2"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/keeper"
)

type lprIntRecorder struct {
	mu    sync.Mutex
	calls int
	fired chan struct{}
}

func (r *lprIntRecorder) fn(_ context.Context, _ string) error {
	r.mu.Lock()
	r.calls++
	r.mu.Unlock()
	select {
	case r.fired <- struct{}{}:
	default:
	}
	return nil // spy: signal only, never actually restart the twin
}

func (r *lprIntRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

// TestIntegration_LivePaneRecover_FiresOnStaleGaugeLivePane proves the full
// gauge-independent recovery path against a real tmux pane: a stale gauge over a
// genuinely ALIVE pane (real IsPaneAlive), no operator attached, and a valid
// .sid bound → the watcher fires the gated ForceRestart action and emits
// session_keeper_live_pane_recover.
//
// The live-pane recovery path (maybeLivePaneRecover) is now present in
// production, so this test is GREEN: a stale gauge over an alive pane with a
// valid .sid bound fires the gated recovery and emits the event.
func TestIntegration_LivePaneRecover_FiresOnStaleGaugeLivePane(t *testing.T) {
	twRequireTmux(t)

	project := t.TempDir()
	agent := fmt.Sprintf("twlpr%d", rand.Int64()) //nolint:gosec // G404: test-local agent-name uniqueness
	twin := twBuildTwin(t, project)
	statusline, idleHook := twScripts(t)

	const emitEvery = 150 * time.Millisecond
	session := twStartTwin(t, twTwinSpec{
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
		suppressAfter: 1 * time.Second, // gauge freezes while the pane stays alive
	})

	if cf := twWaitForCtxTokens(t, project, agent, 50_000, 5*time.Second); cf == nil {
		t.Fatal("tw: .ctx never appeared before suppression deadline")
	}

	writeSidFile(t, project, agent, primarySID)

	rec := &lprIntRecorder{fired: make(chan struct{}, 4)}
	em := &keeper.RecordingEmitter{}
	cfg := keeper.WatcherConfig{
		AgentName:           agent,
		ProjectDir:          project,
		TmuxTarget:          session, // the REAL twin session
		PollInterval:        150 * time.Millisecond,
		Staleness:           500 * time.Millisecond,
		LiveRecoverGrace:    400 * time.Millisecond,
		LiveRecoverCooldown: 10 * time.Second,
		OperatorAttachedFn:  func(_ string) bool { return false }, // no operator typing
		LiveRecoverFn:       rec.fn,
		InjectFn:            func(_ context.Context, _ string) error { return nil },
	}

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	w := keeper.NewWatcher(cfg, em)
	go func() { _ = w.Run(ctx) }() //nolint:errcheck // context cancel is expected

	select {
	case <-rec.fired:
	case <-ctx.Done():
		t.Fatalf("tw: live-pane recovery never fired within the run window (calls=%d); "+
			"a stale gauge over an alive pane with a valid .sid must trigger it", rec.count())
	}

	deadline := time.Now().Add(2 * time.Second)
	for len(em.EventsOfType(core.EventTypeSessionKeeperLivePaneRecover)) == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	cancel()

	if n := len(em.EventsOfType(core.EventTypeSessionKeeperLivePaneRecover)); n == 0 {
		t.Errorf("tw: want >=1 session_keeper_live_pane_recover event; got 0")
	}
}

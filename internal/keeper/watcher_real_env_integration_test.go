//go:build integration

package keeper_test

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/keeper"
)

// TestIntegration_Watcher_GaugeStaleAlive proves that a stale gauge over a live
// tmux pane:
//   - emits session_keeper_no_gauge with reason "stale"
//   - does NOT trigger a respawn (pane is alive, not idle)
//
// Setup: start the twin with --suppress-statusline-after so the gauge freezes
// while the pane stays alive. Then run the Watcher (with RespawnCmd wired to a
// spy, RespawnGrace short, HeartbeatEnabled off) and assert the watcher sees
// "stale" but never calls the respawn spy.
//
// This validates hk-lal8 (gauge stale ≠ pane exited) and the key invariant that
// the keeper's stale-gauge path does not confuse a live-but-silent pane with an
// exited pane.
func TestIntegration_Watcher_GaugeStaleAlive(t *testing.T) {
	twRequireTmux(t)

	project := t.TempDir()
	agent := fmt.Sprintf("twwgs%d", rand.Int64()) //nolint:gosec // G404: test-local agent-name uniqueness
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
		growth:        0, // flat token count — we don't need high context for this test
		startTokens:   50_000,
		emitEvery:     emitEvery,
		suppressAfter: 800 * time.Millisecond, // gauge freezes quickly; pane stays alive
	})

	if cf := twWaitForCtxTokens(t, project, agent, 1, 5*time.Second); cf == nil {
		t.Fatal("tw: .ctx never appeared before suppression deadline")
	}
	time.Sleep(800*time.Millisecond + 6*emitEvery)

	var respawnCalled sync.WaitGroup
	respawnFired := false
	var respawnMu sync.Mutex

	em := &keeper.RecordingEmitter{}
	cfg := keeper.WatcherConfig{
		AgentName:    agent,
		ProjectDir:   project,
		TmuxTarget:   session, // real twin session
		PollInterval: 100 * time.Millisecond,
		// Staleness short enough that the already-stale gauge triggers immediately.
		Staleness:   300 * time.Millisecond,
		IdleQuiesce: 1 * time.Millisecond,
		WarnPct:     80.0,
		// RespawnCmd wired to a spy — will be called if the watcher fires respawn.
		RespawnCmd:   "echo respawn-fired",
		RespawnGrace: 200 * time.Millisecond, // short so it would fire quickly if wired
		// IsPaneIdleFn: nil → uses real IsPaneIdle (the twin's pane is NOT idle,
		// since it runs a non-shell binary — so the respawn gate should NOT fire).
		// HeartbeatEnabled: false (default) — lets the gauge go genuinely stale.
		// SuppressNoGauge: false — we WANT the no_gauge event.
		ReadManagedSessionFn:  func(_, _ string) (string, error) { return "", nil },
		WriteManagedSessionFn: func(_, _, _ string) error { return nil },
	}

	cfg.IsPaneIdleFn = func(ctx context.Context, target string) bool {
		idle := keeper.IsPaneIdle(ctx, target)
		if idle {
			respawnMu.Lock()
			respawnFired = true
			respawnMu.Unlock()
			respawnCalled.Done()
		}
		return idle
	}

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()

	w := keeper.NewWatcher(cfg, em)
	go func() { _ = w.Run(ctx) }() //nolint:errcheck // context cancel is expected

	time.Sleep(3 * time.Second)
	cancel()

	noGauge := em.EventsOfType(core.EventTypeSessionKeeperNoGauge)
	if len(noGauge) == 0 {
		t.Fatal("tw: want ≥1 session_keeper_no_gauge event for stale gauge; got 0")
	}
	foundStale := false
	for _, ev := range noGauge {
		var payload core.SessionKeeperNoGaugePayload
		if err := json.Unmarshal(ev.Payload, &payload); err != nil {
			continue
		}
		if payload.Reason == "stale" {
			foundStale = true
			break
		}
	}
	if !foundStale {
		t.Errorf("tw: want no_gauge reason=\"stale\" for a frozen gauge; got reasons: %v (twin pane is alive)", noGaugeReasons(noGauge))
	}

	respawnMu.Lock()
	fired := respawnFired
	respawnMu.Unlock()
	if fired {
		t.Error("tw: respawn fired on a LIVE pane — stale gauge must NOT trigger respawn when the pane is still running")
	}

	if n := len(em.EventsOfType(core.EventTypeSessionKeeperRespawnAttempted)); n != 0 {
		t.Errorf("tw: want 0 session_keeper_respawn_attempted for a live pane; got %d", n)
	}
}

func noGaugeReasons(events []keeper.EmittedEvent) []string {
	var out []string
	for _, ev := range events {
		var payload core.SessionKeeperNoGaugePayload
		if err := json.Unmarshal(ev.Payload, &payload); err == nil {
			out = append(out, payload.Reason)
		}
	}
	return out
}

// TestIntegration_Watcher_HighCtxWarnThreshold proves that the Watcher's
// absolute-token warn gate fires when the gauge reports tokens above WarnAbsTokens
// (200K) on a 1M window.
//
// Setup: start the twin growing tokens from 50K at a rate that crosses 200K
// within a few seconds. Run the Watcher watching that twin and assert
// session_keeper_warn fires.
//
// This validates the abs-token warn path end-to-end through the real gauge
// pipeline (not just the cycler table test) and confirms the watcher's
// belowWarnThreshold gate is correctly wired to the absolute-token branch.
func TestIntegration_Watcher_HighCtxWarnThreshold(t *testing.T) {
	twRequireTmux(t)

	project := t.TempDir()
	agent := fmt.Sprintf("twwhc%d", rand.Int64()) //nolint:gosec // G404: test-local agent-name uniqueness
	twin := twBuildTwin(t, project)
	statusline, idleHook := twScripts(t)

	const emitEvery = 150 * time.Millisecond
	sess := twStartTwin(t, twTwinSpec{
		project:     project,
		agent:       agent,
		twin:        twin,
		statusline:  statusline,
		idleHook:    idleHook,
		model:       "claude-opus-4-8 [1m]",
		window:      1_000_000, // 1M window so pctCeil fires at 700K; abs wins at 200K
		growth:      60_000,
		startTokens: 50_000,
		emitEvery:   emitEvery,
	})

	if cf := twWaitForCtxTokens(t, project, agent, 1, 5*time.Second); cf == nil {
		t.Fatal("tw: .ctx never appeared")
	}

	var injected []string
	var injectMu sync.Mutex

	em := &keeper.RecordingEmitter{}
	cfg := keeper.WatcherConfig{
		AgentName:    agent,
		ProjectDir:   project,
		TmuxTarget:   sess,
		PollInterval: 100 * time.Millisecond,
		Staleness:    30 * time.Second, // generous — gauge is fresh
		IdleQuiesce:  1 * time.Millisecond,
		WarnPct:      80.0,
		// WarnAbsTokens defaults to 200K (from thresholds.go); no override needed.
		// WarnPctCeil defaults to 0.70; on a 1M window that's 700K, so the abs gate
		// (200K) fires first — exactly the scenario we want to validate.
		InjectFn: func(_ context.Context, _ string) error {
			injectMu.Lock()
			injected = append(injected, "warn")
			injectMu.Unlock()
			return nil
		},
		ReadManagedSessionFn:  func(_, _ string) (string, error) { return "", nil },
		WriteManagedSessionFn: func(_, _, _ string) error { return nil },
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	warnFiredCh := make(chan struct{}, 1)

	type sigEmitter struct {
		*keeper.RecordingEmitter
		ch chan struct{}
	}
	go func() {
		tick := time.NewTicker(50 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				if len(em.EventsOfType(core.EventTypeSessionKeeperWarn)) > 0 {
					select {
					case warnFiredCh <- struct{}{}:
					default:
					}
				}
			}
		}
	}()

	w := keeper.NewWatcher(cfg, em)
	go func() { _ = w.Run(ctx) }() //nolint:errcheck // context cancel is expected

	select {
	case <-warnFiredCh:
	case <-ctx.Done():
		cf, _, _ := keeper.ReadCtxFile(project, agent)
		if cf != nil {
			t.Fatalf("tw: session_keeper_warn never fired (gauge at tokens=%d window=%d pct=%.1f); "+
				"want warn when tokens ≥ WarnAbsTokens (%d) on a %d-window",
				cf.Tokens, cf.WindowSize, cf.Pct, keeper.DefaultWarnAbsTokens, 1_000_000)
		}
		t.Fatalf("tw: session_keeper_warn never fired within timeout; gauge unreadable")
	}
	cancel()

	warns := em.EventsOfType(core.EventTypeSessionKeeperWarn)
	if len(warns) == 0 {
		t.Fatal("tw: want ≥1 session_keeper_warn; got 0")
	}

	var warnPayload core.SessionKeeperWarnPayload
	if err := json.Unmarshal(warns[0].Payload, &warnPayload); err != nil {
		t.Fatalf("tw: unmarshal session_keeper_warn payload: %v", err)
	}
	if warnPayload.AgentName != agent {
		t.Errorf("tw: warn payload.AgentName = %q; want %q", warnPayload.AgentName, agent)
	}

	if n := len(em.EventsOfType(core.EventTypeSessionKeeperNoGauge)); n > 0 {
		t.Errorf("tw: want 0 session_keeper_no_gauge events (gauge was fresh); got %d", n)
	}
}

// TestIntegration_Watcher_BlindAlarmFires proves that the blind-keeper alarm
// (session_keeper_blind) fires EXACTLY ONCE after BlindKeeperThreshold of
// continuous foreign_session rejection, and does NOT fire again on subsequent
// ticks.
//
// Setup: a static gauge carrying a foreign session_id (gauge SID != managed SID,
// .sid endorses the managed SID so the watcher rejects the gauge as truly foreign
// on every tick). BlindKeeperThreshold is set to 200ms (test-only; production is
// 5 minutes) so the alarm fires quickly.
//
// This validates hk-34ac (Backstop 1: blind-keeper alarm) with the configurable
// BlindKeeperThreshold field that hk-nlio adds so the alarm can be exercised in
// CI without a 5-minute wait.
func TestIntegration_Watcher_BlindAlarmFires(t *testing.T) {
	t.Parallel()

	project := t.TempDir()
	agent := "blind-alarm-real-env"

	writeCtxFileTokens(t, project, agent, 50.0, 50_000, 1_000_000, "sess-foreign")

	em := &keeper.RecordingEmitter{}

	cfg := keeper.WatcherConfig{
		AgentName:    agent,
		ProjectDir:   project,
		PollInterval: 20 * time.Millisecond,
		Staleness:    30 * time.Second, // generous — gauge stays fresh
		IdleQuiesce:  1 * time.Millisecond,
		WarnPct:      80.0,
		// Managed binding: "sess-managed". .sid endorses the same value.
		// The gauge carries "sess-foreign", so every tick is a foreign_session
		// rejection — the blind episode starts immediately.
		ReadManagedSessionFn:  func(_, _ string) (string, error) { return "sess-managed", nil },
		WriteManagedSessionFn: func(_, _, _ string) error { return nil },
		ReadSidFn: func(_, _ string) (string, time.Time, error) {
			return "sess-managed", time.Time{}, nil
		},
		// BlindKeeperThreshold: 200ms (test-only shortcut; production default = 5 min).
		BlindKeeperThreshold: 200 * time.Millisecond,
	}

	blindFiredCh := make(chan struct{}, 1)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() {
		tick := time.NewTicker(10 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				if len(em.EventsOfType(core.EventTypeSessionKeeperBlind)) > 0 {
					select {
					case blindFiredCh <- struct{}{}:
					default:
					}
				}
			}
		}
	}()

	w := keeper.NewWatcher(cfg, em)
	go func() { _ = w.Run(ctx) }() //nolint:errcheck // context cancel is expected

	select {
	case <-blindFiredCh:
	case <-ctx.Done():
		t.Fatalf("tw: session_keeper_blind never fired within timeout "+
			"(BlindKeeperThreshold=%s; verify the watcher is reaching the foreign_session path)",
			200*time.Millisecond)
	}

	time.Sleep(300 * time.Millisecond)
	cancel()

	blindEvents := em.EventsOfType(core.EventTypeSessionKeeperBlind)
	if len(blindEvents) == 0 {
		t.Fatal("tw: want ≥1 session_keeper_blind event; got 0 (alarm did not fire)")
	}
	if len(blindEvents) > 1 {
		t.Errorf("tw: want exactly 1 session_keeper_blind (latch prevents re-emit); got %d", len(blindEvents))
	}

	var payload core.SessionKeeperBlindPayload
	if err := json.Unmarshal(blindEvents[0].Payload, &payload); err != nil {
		t.Fatalf("tw: unmarshal session_keeper_blind payload: %v", err)
	}
	if payload.AgentName != agent {
		t.Errorf("tw: blind payload.AgentName = %q; want %q", payload.AgentName, agent)
	}
	if payload.ManagedSID != "sess-managed" {
		t.Errorf("tw: blind payload.ManagedSID = %q; want \"sess-managed\"", payload.ManagedSID)
	}
	if payload.LiveSID != "sess-foreign" {
		t.Errorf("tw: blind payload.LiveSID = %q; want \"sess-foreign\"", payload.LiveSID)
	}
	if payload.BlindSeconds < 0 {
		t.Errorf("tw: blind payload.BlindSeconds = %d; want ≥ 0", payload.BlindSeconds)
	}

	noGauge := em.EventsOfType(core.EventTypeSessionKeeperNoGauge)
	foundForeign := false
	for _, ev := range noGauge {
		var ng core.SessionKeeperNoGaugePayload
		if err := json.Unmarshal(ev.Payload, &ng); err == nil && ng.Reason == "foreign_session" {
			foundForeign = true
			break
		}
	}
	if !foundForeign {
		t.Error("tw: want ≥1 no_gauge:foreign_session event before the blind alarm; got 0")
	}
}

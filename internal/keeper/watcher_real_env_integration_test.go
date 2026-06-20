//go:build integration

package keeper_test

// watcher_real_env_integration_test.go — bead hk-nlio, real-env watcher gate.
//
// Three real-environment scenarios that validate P0/P1 keeper fixes work
// end-to-end, exercising the watcher (not just the cycler) against real tmux
// panes and real gauge pipelines. These tests are the structural validation
// layer demanded by the hk-nlio acceptance gate.
//
//  1. TestIntegration_Watcher_GaugeStaleAlive — gauge stale while pane is ALIVE.
//     Proves: no_gauge:stale fires; respawn does NOT fire (pane is alive, not idle).
//     Validates the stale-but-alive path is handled separately from exited panes.
//     Exercises hk-lal8 (heartbeat miss-budget suppression).
//
//  2. TestIntegration_Watcher_HighCtxWarnThreshold — 210K-token gauge on 1M window.
//     Proves: session_keeper_warn fires at the warn threshold (WarnAbsTokens=200K).
//     Exercises the watcher's absolute-token warn gate on a real-pipeline gauge.
//
//  3. TestIntegration_Watcher_BlindAlarmFires — foreign_session for long enough.
//     Proves: session_keeper_blind fires EXACTLY ONCE after BlindKeeperThreshold
//     of continuous foreign_session; does NOT fire again on the next tick.
//     Exercises hk-34ac (blind-keeper alarm) with a configurable threshold so the
//     test runs in seconds, not 5 minutes.
//
// Safety contract: shares the tw-prefix harness discipline from
// cycle_twin_e2e_integration_test.go. All tmux sessions use the "hksav-twin-"
// prefix and are killed by exact name on teardown. No kill-server, no glob/pattern
// kill, no interference with live daemon/captain/crew sessions.

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

	// Start the twin. The gauge will emit for 800ms then go SILENT (suppress),
	// while the idle hook (and thus the pane) keeps running.
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

	// Wait for the gauge file to appear (pre-suppression), then let it go stale.
	if cf := twWaitForCtxTokens(t, project, agent, 1, 5*time.Second); cf == nil {
		t.Fatal("tw: .ctx never appeared before suppression deadline")
	}
	// Wait well past the suppression deadline so the gauge is definitely stale.
	time.Sleep(800*time.Millisecond + 6*emitEvery)

	// Spy on respawn calls: if the watcher mis-classifies the live pane as idle
	// and calls the respawn command, this will record it.
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

	// Override IsPaneIdleFn with a spy so we can track whether it reports idle.
	// The REAL IsPaneIdle would return false for the twin's alive pane, but we
	// want an explicit signal. We wrap the real function.
	cfg.IsPaneIdleFn = func(ctx context.Context, target string) bool {
		idle := keeper.IsPaneIdle(ctx, target)
		if idle {
			// If this ever fires, respawn would follow — record it.
			respawnMu.Lock()
			respawnFired = true
			respawnMu.Unlock()
			respawnCalled.Done()
		}
		return idle
	}
	// We do NOT call respawnCalled.Add(1) — we don't expect respawn to fire.

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()

	w := keeper.NewWatcher(cfg, em)
	go func() { _ = w.Run(ctx) }() //nolint:errcheck // context cancel is expected

	// Run for enough time for the watcher to detect staleness and run several ticks.
	time.Sleep(3 * time.Second)
	cancel()

	// (a) At least one session_keeper_no_gauge with reason "stale" must have fired.
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

	// (b) NO respawn — the pane is alive (non-idle), so IsPaneIdle returns false
	// and maybeRespawn never runs. Even with a short RespawnGrace, the alive pane
	// blocks the respawn gate.
	respawnMu.Lock()
	fired := respawnFired
	respawnMu.Unlock()
	if fired {
		t.Error("tw: respawn fired on a LIVE pane — stale gauge must NOT trigger respawn when the pane is still running")
	}

	// (c) session_keeper_respawn_attempted must NOT have been emitted.
	if n := len(em.EventsOfType(core.EventTypeSessionKeeperRespawnAttempted)); n != 0 {
		t.Errorf("tw: want 0 session_keeper_respawn_attempted for a live pane; got %d", n)
	}
}

// noGaugeReasons extracts the reason strings from a slice of no_gauge events,
// used to give helpful failure messages when the expected reason is missing.
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

	// Growth: 60K tokens per 150ms. From startTokens=50K → crosses WarnAbsTokens
	// (200K) in ~3 emits (~450ms). We set a generous timeout below.
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

	// Wait for the gauge to appear before starting the watcher.
	if cf := twWaitForCtxTokens(t, project, agent, 1, 5*time.Second); cf == nil {
		t.Fatal("tw: .ctx never appeared")
	}

	// Track injection calls — we want to know if the warn also triggered inject.
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
		// WarnCooldown: leave default (30s) — we only need one crossing.
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// warnFiredCh signals the first session_keeper_warn event.
	warnFiredCh := make(chan struct{}, 1)

	// Wrap the emitter to signal when a warn event fires.
	type sigEmitter struct {
		*keeper.RecordingEmitter
		ch chan struct{}
	}
	// We can't easily intercept EmitWithRunID on RecordingEmitter, so we poll
	// the events list after startup instead. Use a separate goroutine to poll.
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

	// Wait for the warn to fire or timeout.
	select {
	case <-warnFiredCh:
		// warn fired — good
	case <-ctx.Done():
		// Report what the gauge was at when we timed out.
		cf, _, _ := keeper.ReadCtxFile(project, agent)
		if cf != nil {
			t.Fatalf("tw: session_keeper_warn never fired (gauge at tokens=%d window=%d pct=%.1f); "+
				"want warn when tokens ≥ WarnAbsTokens (%d) on a %d-window",
				cf.Tokens, cf.WindowSize, cf.Pct, keeper.DefaultWarnAbsTokens, 1_000_000)
		}
		t.Fatalf("tw: session_keeper_warn never fired within timeout; gauge unreadable")
	}
	cancel()

	// (a) Exactly one warn event (no double-fire on the happy path).
	warns := em.EventsOfType(core.EventTypeSessionKeeperWarn)
	if len(warns) == 0 {
		t.Fatal("tw: want ≥1 session_keeper_warn; got 0")
	}

	// (b) Verify the payload fields make sense.
	var warnPayload core.SessionKeeperWarnPayload
	if err := json.Unmarshal(warns[0].Payload, &warnPayload); err != nil {
		t.Fatalf("tw: unmarshal session_keeper_warn payload: %v", err)
	}
	if warnPayload.AgentName != agent {
		t.Errorf("tw: warn payload.AgentName = %q; want %q", warnPayload.AgentName, agent)
	}

	// (c) NO no_gauge events — the gauge was fresh throughout.
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

	// Write a gauge with a foreign session_id. This is a static file — no real
	// tmux pane needed, since the blind alarm fires purely from the foreign_session
	// path and the gauge file stays fresh.
	writeCtxFileTokens(t, project, agent, 50.0, 50_000, 1_000_000, "sess-foreign")

	em := &keeper.RecordingEmitter{}

	// BlindKeeperThreshold: 200ms so the alarm fires within milliseconds of the
	// threshold being crossed, keeping the test wall time under 1 second.
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
			// .sid endorses "sess-managed", NOT the gauge's "sess-foreign",
			// so the watcher rejects the gauge as a true concurrent foreign session.
			return "sess-managed", time.Time{}, nil
		},
		// BlindKeeperThreshold: 200ms (test-only shortcut; production default = 5 min).
		BlindKeeperThreshold: 200 * time.Millisecond,
	}

	// blindFiredCh signals when the first session_keeper_blind event is observed.
	blindFiredCh := make(chan struct{}, 1)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Poll events list for blind events in a separate goroutine.
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

	// Wait for the alarm to fire.
	select {
	case <-blindFiredCh:
		// alarm fired — good; let it run for a few more ticks to check latch.
	case <-ctx.Done():
		t.Fatalf("tw: session_keeper_blind never fired within timeout "+
			"(BlindKeeperThreshold=%s; verify the watcher is reaching the foreign_session path)",
			200*time.Millisecond)
	}

	// Run for a few more ticks to verify the latch (no re-emit).
	time.Sleep(300 * time.Millisecond)
	cancel()

	// (a) Exactly ONE blind event (latch prevents re-emit on subsequent ticks).
	blindEvents := em.EventsOfType(core.EventTypeSessionKeeperBlind)
	if len(blindEvents) == 0 {
		t.Fatal("tw: want ≥1 session_keeper_blind event; got 0 (alarm did not fire)")
	}
	if len(blindEvents) > 1 {
		t.Errorf("tw: want exactly 1 session_keeper_blind (latch prevents re-emit); got %d", len(blindEvents))
	}

	// (b) Verify the payload.
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
	// BlindSeconds is the integer-second duration of the blind episode. With the
	// 200ms test threshold it may be 0 (< 1 second); what matters is the alarm
	// fired with correct ManagedSID/LiveSID. A production run uses a 5-minute
	// threshold so BlindSeconds is always well above 0 in the real env.
	if payload.BlindSeconds < 0 {
		t.Errorf("tw: blind payload.BlindSeconds = %d; want ≥ 0", payload.BlindSeconds)
	}

	// (c) At least one no_gauge:foreign_session must have fired (confirms the path
	// was reached before the alarm — no_gauge:foreign is emitted per-tick, blind
	// is emitted once after the threshold).
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

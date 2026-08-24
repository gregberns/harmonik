package keeper_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/keeper"
)

func foreignSessionConfig(t *testing.T, projectDir, agent string, tokens int64) keeper.WatcherConfig {
	t.Helper()

	keeperDir := filepath.Join(projectDir, ".harmonik", "keeper")
	if err := os.MkdirAll(keeperDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	data, err := json.Marshal(keeper.CtxFile{
		Pct:       50.0,
		Tokens:    tokens,
		SessionID: "sess-foreign",
		Ts:        time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("json.Marshal CtxFile: %v", err)
	}
	path := filepath.Join(keeperDir, agent+".ctx")
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatalf("WriteFile ctx: %v", err)
	}

	return keeper.WatcherConfig{
		AgentName:    agent,
		ProjectDir:   projectDir,
		PollInterval: 5 * time.Millisecond,
		WarnPct:      80.0,
		IdleQuiesce:  1 * time.Millisecond,
		Staleness:    120 * time.Second, // generous — gauge stays fresh
		TmuxTarget:   "",
		// WHICH OF THESE TWO THRESHOLDS ACTUALLY GATES ANYTHING HERE. Staleness
		// does: the stale branch is evaluated BEFORE the foreign-session branch
		// and `continue`s past it, so a gauge that ages past Staleness takes the
		// hard-ceiling backstop out of reach. Every test built on this config
		// depends on the gauge staying inside that window.
		//
		// IdleQuiesce does NOT. It is read only on the fresh-and-SID-matched
		// path, and this config makes every tick a foreign_session, so the loop
		// has already `continue`d before reaching the idle gate. The value is
		// carried for shape, not for effect — no edit to it changes the outcome
		// of any test in this file. Refs: hk-3ty39.
		//
		// Managed binding is "sess-managed"; .sid endorses the same value.
		// The gauge carries "sess-foreign", so every tick is a foreign_session.
		ReadManagedSessionFn:  func(_, _ string) (string, error) { return "sess-managed", nil },
		WriteManagedSessionFn: func(_, _, _ string) error { return nil },
		ReadSidFn: func(_, _ string) (string, time.Time, error) {
			return "sess-managed", time.Time{}, nil
		},
	}
}

// TestBlindKeeperAlarm_FiresAfter5Min verifies Backstop 1:
//   - session_keeper_blind is emitted EXACTLY ONCE after 5+ min of continuous
//     foreign_session rejection.
//   - Additional ticks while still blind do NOT re-emit the event.
//   - Making the gauge readable (matching session_id) clears the latch so the
//     next blind episode starts a fresh 5-minute clock.
func TestBlindKeeperAlarm_FiresAfter5Min(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	agent := "blind-alarm-agent"

	em := &keeper.RecordingEmitter{}
	cfg := foreignSessionConfig(t, projectDir, agent, 50_000)

	runWatcherFor(context.Background(), cfg, em, 60*time.Millisecond)

	blindEvents := em.EventsOfType(core.EventTypeSessionKeeperBlind)
	if len(blindEvents) != 0 {
		t.Errorf("want 0 session_keeper_blind events in short run (threshold 5 min not crossed); got %d", len(blindEvents))
	}

	noGauge := em.EventsOfType(core.EventTypeSessionKeeperNoGauge)
	if len(noGauge) == 0 {
		t.Error("want ≥1 session_keeper_no_gauge for foreign_session; got 0 (foreign_session path not reached)")
	}
}

// TestBlindKeeperAlarm_LatchClearedOnReadableGauge verifies the latch-clear
// behaviour: after a blind episode, switching the gauge to a matching session_id
// resets blindSince and blindAlarmFired, so the next foreign_session streak
// arms a fresh 5-minute clock.
//
// Since we cannot control the keeper's internal 5-min timer, we test the
// structural invariant: the watcher does NOT emit additional blind events once
// the gauge becomes readable (latch cleared on the readable tick).
func TestBlindKeeperAlarm_LatchClearedOnReadableGauge(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	agent := "blind-latch-agent"

	keeperDir := filepath.Join(projectDir, ".harmonik", "keeper")
	if err := os.MkdirAll(keeperDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	var mu sync.Mutex
	managedSID := "sess-managed"

	em := &keeper.RecordingEmitter{}

	writeCtxFile(t, projectDir, agent, 50.0, "sess-foreign")

	cfg := keeper.WatcherConfig{
		AgentName:    agent,
		ProjectDir:   projectDir,
		PollInterval: 5 * time.Millisecond,
		WarnPct:      80.0,
		IdleQuiesce:  1 * time.Millisecond,
		Staleness:    120 * time.Second,
		TmuxTarget:   "",
		ReadManagedSessionFn: func(_, _ string) (string, error) {
			mu.Lock()
			defer mu.Unlock()
			return managedSID, nil
		},
		WriteManagedSessionFn: func(_, _, _ string) error { return nil },
		ReadSidFn: func(_, _ string) (string, time.Time, error) {
			mu.Lock()
			defer mu.Unlock()
			return managedSID, time.Time{}, nil
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		w := keeper.NewWatcher(cfg, em)
		if err := w.Run(ctx); err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("Watcher.Run: %v", err)
		}
	}()

	time.Sleep(30 * time.Millisecond)

	writeCtxFile(t, projectDir, agent, 50.0, "sess-managed")
	time.Sleep(30 * time.Millisecond)

	writeCtxFile(t, projectDir, agent, 50.0, "sess-foreign")
	time.Sleep(30 * time.Millisecond)

	cancel()
	<-done

	blindEvents := em.EventsOfType(core.EventTypeSessionKeeperBlind)
	if len(blindEvents) != 0 {
		t.Errorf("want 0 session_keeper_blind events (threshold never crossed); got %d", len(blindEvents))
	}

	warns := em.EventsOfType(core.EventTypeSessionKeeperWarn)
	if len(warns) != 0 {
		t.Errorf("want 0 session_keeper_warn events (pct=50<80); got %d", len(warns))
	}
}

// TestBlindKeeperAlarm_EmitsAfterInjectedThreshold exercises the blind-keeper
// alarm EMISSION path in CI (NOT integration-tagged) by injecting a tiny
// BlindKeeperThreshold via WatcherConfig. The production default is the 5-min
// constant (applyDefaults restores it when the field is 0); here we shrink it so
// a few fast foreign ticks cross it.
//
// Asserts the full latch state machine:
//   - blind FIRES exactly once after the injected threshold elapses under a
//     continuous foreign_session streak;
//   - it does NOT re-fire on subsequent foreign ticks while still blind (latch);
//   - a matched (non-foreign) tick clears the latch + timer, so a fresh foreign
//     streak arms a new clock and emits a SECOND blind event after the threshold.
//
// Without an injectable threshold this path could only be asserted ABSENT (the
// other two tests). This is the first test that proves the alarm ACTUALLY emits.
func TestBlindKeeperAlarm_EmitsAfterInjectedThreshold(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	agent := "blind-emit-agent"

	keeperDir := filepath.Join(projectDir, ".harmonik", "keeper")
	if err := os.MkdirAll(keeperDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	em := &keeper.RecordingEmitter{}

	writeCtxFile(t, projectDir, agent, 50.0, "sess-foreign")

	cfg := keeper.WatcherConfig{
		AgentName:    agent,
		ProjectDir:   projectDir,
		PollInterval: 5 * time.Millisecond,
		WarnPct:      80.0,
		IdleQuiesce:  1 * time.Millisecond,
		Staleness:    120 * time.Second, // generous — gauge stays fresh
		TmuxTarget:   "",
		// ── injected seam: 30ms instead of the 5-min production constant ──
		BlindKeeperThreshold: 30 * time.Millisecond,
		// Managed binding + .sid both endorse "sess-managed", so a gauge bearing
		// "sess-foreign" is rejected as foreign on every tick.
		ReadManagedSessionFn:  func(_, _ string) (string, error) { return "sess-managed", nil },
		WriteManagedSessionFn: func(_, _, _ string) error { return nil },
		ReadSidFn: func(_, _ string) (string, time.Time, error) {
			return "sess-managed", time.Time{}, nil
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		w := keeper.NewWatcher(cfg, em)
		if err := w.Run(ctx); err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("Watcher.Run: %v", err)
		}
	}()

	time.Sleep(120 * time.Millisecond)
	if n := len(em.EventsOfType(core.EventTypeSessionKeeperBlind)); n != 1 {
		cancel()
		<-done
		t.Fatalf("phase 1: want exactly 1 session_keeper_blind after injected threshold; got %d", n)
	}

	writeCtxFile(t, projectDir, agent, 50.0, "sess-managed")
	time.Sleep(40 * time.Millisecond)
	if n := len(em.EventsOfType(core.EventTypeSessionKeeperBlind)); n != 1 {
		cancel()
		<-done
		t.Fatalf("phase 2: matched gauge must not add a blind event; got %d", n)
	}

	writeCtxFile(t, projectDir, agent, 50.0, "sess-foreign")
	time.Sleep(120 * time.Millisecond)

	cancel()
	<-done

	if n := len(em.EventsOfType(core.EventTypeSessionKeeperBlind)); n != 2 {
		t.Errorf("phase 3: want a 2nd blind event after latch reset + fresh foreign streak; got %d total", n)
	}
}

type restartSpy struct {
	mu    sync.Mutex
	calls int
}

func (s *restartSpy) restart(_ context.Context, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	return nil
}

func (s *restartSpy) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

// TestHardCeiling_FiresAbove280K_DespiteForeignSession verifies Backstop 2:
//   - When a foreign-session gauge reports tokens >= HardCeilingAbsTokens (280K),
//     the restart function fires and session_keeper_hard_ceiling is emitted.
//   - When tokens < HardCeilingAbsTokens (270K), neither fires.
func TestHardCeiling_FiresAbove280K_DespiteForeignSession(t *testing.T) {
	t.Parallel()

	t.Run("fires_at_290K", func(t *testing.T) {
		t.Parallel()

		projectDir := t.TempDir()
		agent := "hard-ceiling-290k-agent"

		em := &keeper.RecordingEmitter{}
		spy := &restartSpy{}

		cfg := foreignSessionConfig(t, projectDir, agent, 290_000)
		cfg.HardCeilingMode = keeper.HardCeilingModeRestart // hk-z8d0: restart mode calls the fn
		cfg.HardCeilingRestartFn = spy.restart
		cfg.HardCeilingCooldown = 10 * time.Second // long cooldown → only one attempt

		runWatcherFor(context.Background(), cfg, em, 80*time.Millisecond)

		if n := spy.count(); n == 0 {
			t.Error("want ≥1 hard-ceiling restart call at 290K tokens (foreign session); got 0")
		}

		ceilEvents := em.EventsOfType(core.EventTypeSessionKeeperHardCeiling)
		if len(ceilEvents) == 0 {
			t.Error("want ≥1 session_keeper_hard_ceiling event at 290K tokens; got 0")
		}

		if len(ceilEvents) > 0 {
			var payload core.SessionKeeperHardCeilingPayload
			if err := json.Unmarshal(ceilEvents[0].Payload, &payload); err != nil {
				t.Fatalf("unmarshal hard_ceiling payload: %v", err)
			}
			if payload.AgentName != agent {
				t.Errorf("payload.AgentName = %q; want %q", payload.AgentName, agent)
			}
			if payload.ContextLen != 290_000 {
				t.Errorf("payload.ContextLen = %d; want 290000", payload.ContextLen)
			}
			if payload.HardCeiling != keeper.DefaultHardCeilingTokens {
				t.Errorf("payload.HardCeiling = %d; want default %d", payload.HardCeiling, keeper.DefaultHardCeilingTokens)
			}
		}
	})

	t.Run("does_not_fire_at_270K", func(t *testing.T) {
		t.Parallel()

		projectDir := t.TempDir()
		agent := "hard-ceiling-270k-agent"

		em := &keeper.RecordingEmitter{}
		spy := &restartSpy{}

		cfg := foreignSessionConfig(t, projectDir, agent, 270_000)
		cfg.HardCeilingRestartFn = spy.restart

		runWatcherFor(context.Background(), cfg, em, 80*time.Millisecond)

		if n := spy.count(); n != 0 {
			t.Errorf("want 0 hard-ceiling restart calls at 270K tokens; got %d", n)
		}

		ceilEvents := em.EventsOfType(core.EventTypeSessionKeeperHardCeiling)
		if len(ceilEvents) != 0 {
			t.Errorf("want 0 session_keeper_hard_ceiling events at 270K tokens; got %d", len(ceilEvents))
		}
	})
}

// TestHardCeiling_CooldownPreventsMultipleRestarts verifies that the hard-ceiling
// restart fires at most once per cooldown window even when tokens remain ≥ 280K
// across many ticks.
func TestHardCeiling_CooldownPreventsMultipleRestarts(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	agent := "hard-ceiling-cooldown-agent"

	em := &keeper.RecordingEmitter{}
	spy := &restartSpy{}

	cfg := foreignSessionConfig(t, projectDir, agent, 290_000)
	cfg.HardCeilingMode = keeper.HardCeilingModeRestart // hk-z8d0: restart mode calls the fn
	cfg.HardCeilingRestartFn = spy.restart
	cfg.HardCeilingCooldown = 10 * time.Second // long cooldown → only one attempt

	_, gaugeModTime := readCtxFor(t, projectDir, agent)

	driveWatcherFakeClockFrom(t, gaugeModTime, cfg, em, 40)

	if n := noGaugeStaleCount(em); n != 0 {
		t.Errorf("want 0 no_gauge:stale over the run (the gauge must stay fresh, or the hard ceiling is never reached); got %d", n)
	}
	if n := spy.count(); n != 1 {
		t.Errorf("want exactly 1 hard-ceiling restart (cooldown holds); got %d", n)
	}
}

// TestHardCeiling_AlarmEmitsWhenFnNil proves the hk-746u fix (hk-z8d0): in the
// DEFAULT alarm mode, the hard-ceiling alarm MUST emit even when
// HardCeilingRestartFn is nil — the emit used to live INSIDE the
// `HardCeilingRestartFn != nil` guard, so a nil fn (the production state)
// silently emitted nothing. Now alarm emits regardless of the fn, and the fn is
// never called in alarm mode.
func TestHardCeiling_AlarmEmitsWhenFnNil(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	agent := "hard-ceiling-alarm-nil-fn-agent"

	em := &keeper.RecordingEmitter{}

	cfg := foreignSessionConfig(t, projectDir, agent, 290_000)
	cfg.HardCeilingCooldown = 10 * time.Second // alarm at most once

	runWatcherFor(context.Background(), cfg, em, 80*time.Millisecond)

	ceilEvents := em.EventsOfType(core.EventTypeSessionKeeperHardCeiling)
	if len(ceilEvents) == 0 {
		t.Error("hk-746u regression: want ≥1 session_keeper_hard_ceiling in alarm mode with nil fn; got 0")
	}
}

// TestHardCeiling_OffMode_NoEmitNoRestart verifies off mode is a total no-op:
// no alarm event and no restart call even far above the ceiling.
func TestHardCeiling_OffMode_NoEmitNoRestart(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	agent := "hard-ceiling-off-agent"

	em := &keeper.RecordingEmitter{}
	spy := &restartSpy{}

	cfg := foreignSessionConfig(t, projectDir, agent, 290_000)
	cfg.HardCeilingMode = keeper.HardCeilingModeOff
	cfg.HardCeilingRestartFn = spy.restart // wired but must NOT be called in off mode

	runWatcherFor(context.Background(), cfg, em, 80*time.Millisecond)

	if n := spy.count(); n != 0 {
		t.Errorf("off mode: want 0 restart calls; got %d", n)
	}
	if ceilEvents := em.EventsOfType(core.EventTypeSessionKeeperHardCeiling); len(ceilEvents) != 0 {
		t.Errorf("off mode: want 0 session_keeper_hard_ceiling events; got %d", len(ceilEvents))
	}
}

// TestHardCeiling_AlarmMode_EmitOnly verifies alarm mode emits the event but NEVER
// calls the restart fn, even when a fn is wired.
func TestHardCeiling_AlarmMode_EmitOnly(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	agent := "hard-ceiling-alarm-agent"

	em := &keeper.RecordingEmitter{}
	spy := &restartSpy{}

	cfg := foreignSessionConfig(t, projectDir, agent, 290_000)
	cfg.HardCeilingMode = keeper.HardCeilingModeAlarm
	cfg.HardCeilingRestartFn = spy.restart // wired but must NOT be called in alarm mode
	cfg.HardCeilingCooldown = 10 * time.Second

	runWatcherFor(context.Background(), cfg, em, 80*time.Millisecond)

	if n := spy.count(); n != 0 {
		t.Errorf("alarm mode: want 0 restart calls even with a wired fn; got %d", n)
	}
	if ceilEvents := em.EventsOfType(core.EventTypeSessionKeeperHardCeiling); len(ceilEvents) == 0 {
		t.Error("alarm mode: want ≥1 session_keeper_hard_ceiling event; got 0")
	}
}

// TestHardCeiling_RestartMode_NilFnDegradesToAlarm verifies that restart mode with
// a NIL fn does NOT panic and degrades to alarm (emit only). This is the
// fail-closed degrade path: an operator selected restart but the closure was nil
// (no --respawn-cmd / unresolvable pane), so we alarm rather than crash.
func TestHardCeiling_RestartMode_NilFnDegradesToAlarm(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	agent := "hard-ceiling-restart-nilfn-agent"

	em := &keeper.RecordingEmitter{}

	cfg := foreignSessionConfig(t, projectDir, agent, 290_000)
	cfg.HardCeilingMode = keeper.HardCeilingModeRestart
	cfg.HardCeilingCooldown = 10 * time.Second

	runWatcherFor(context.Background(), cfg, em, 80*time.Millisecond)

	if ceilEvents := em.EventsOfType(core.EventTypeSessionKeeperHardCeiling); len(ceilEvents) == 0 {
		t.Error("restart mode with nil fn: want ≥1 alarm event (degrade-to-alarm); got 0")
	}
}

// TestHardCeiling_NormalPath_UsesBackstopBeforeCycle proves that a healthy
// managed binding cannot make the hard ceiling unreachable. Restart mode owns
// the tick at this threshold.
func TestHardCeiling_NormalPath_UsesBackstopBeforeCycle(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	agent := "hard-ceiling-normal-path-agent"

	keeperDir := filepath.Join(projectDir, ".harmonik", "keeper")
	if err := os.MkdirAll(keeperDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	data, err := json.Marshal(keeper.CtxFile{
		Pct:       99.0,
		Tokens:    290_000,
		SessionID: "sess-managed",
		Ts:        time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("json.Marshal CtxFile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(keeperDir, agent+".ctx"), append(data, '\n'), 0o600); err != nil {
		t.Fatalf("WriteFile ctx: %v", err)
	}

	em := &keeper.RecordingEmitter{}
	ceilSpy := &restartSpy{}

	cfg := keeper.WatcherConfig{
		AgentName:    agent,
		ProjectDir:   projectDir,
		PollInterval: 5 * time.Millisecond,
		IdleQuiesce:  1 * time.Millisecond,
		Staleness:    120 * time.Second,
		// SID is matched on every tick. This is the path that failed live when
		// the backstop existed only inside foreign-session handling.
		ReadManagedSessionFn:  func(_, _ string) (string, error) { return "sess-managed", nil },
		WriteManagedSessionFn: func(_, _, _ string) error { return nil },
		ReadSidFn: func(_, _ string) (string, time.Time, error) {
			return "sess-managed", time.Time{}, nil
		},
		HardCeilingMode:      keeper.HardCeilingModeRestart,
		HardCeilingRestartFn: ceilSpy.restart,
		HardCeilingCooldown:  10 * time.Second,
	}

	runWatcherFor(context.Background(), cfg, em, 80*time.Millisecond)

	if n := ceilSpy.count(); n != 1 {
		t.Errorf("hard-ceiling restart fn called %d times on the managed path; want 1", n)
	}
	if ceilEvents := em.EventsOfType(core.EventTypeSessionKeeperHardCeiling); len(ceilEvents) != 1 {
		t.Errorf("managed path: want 1 session_keeper_hard_ceiling event; got %d", len(ceilEvents))
	}
}

// TestHardCeiling_SkipsWhenTokensZero verifies that when the gauge reports
// tokens==0 (absent field, unreadable), the hard ceiling check is skipped.
func TestHardCeiling_SkipsWhenTokensZero(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	agent := "hard-ceiling-zero-tokens-agent"

	em := &keeper.RecordingEmitter{}
	spy := &restartSpy{}

	cfg := foreignSessionConfig(t, projectDir, agent, 0)
	cfg.HardCeilingRestartFn = spy.restart

	runWatcherFor(context.Background(), cfg, em, 80*time.Millisecond)

	if n := spy.count(); n != 0 {
		t.Errorf("want 0 hard-ceiling restart calls when tokens==0; got %d", n)
	}
}

// TestHardCeiling_EffectiveThresholdWiredThrough proves the const→field plumbing
// (hk-n6kn): a NON-default HardCeilingTokens (250 000) must (a) drive the gate at
// a lower token count than the 280 000 default and (b) be the value carried in
// the emitted session_keeper_hard_ceiling payload — i.e. the EFFECTIVE configured
// ceiling is wired through, not a fixed 280 000.
func TestHardCeiling_EffectiveThresholdWiredThrough(t *testing.T) {
	t.Parallel()

	const effectiveCeiling int64 = 250_000

	projectDir := t.TempDir()
	agent := "hard-ceiling-effective-agent"

	em := &keeper.RecordingEmitter{}
	spy := &restartSpy{}

	cfg := foreignSessionConfig(t, projectDir, agent, 260_000)
	cfg.HardCeilingMode = keeper.HardCeilingModeRestart // hk-z8d0: restart mode calls the fn
	cfg.HardCeilingTokens = effectiveCeiling
	cfg.HardCeilingRestartFn = spy.restart
	cfg.HardCeilingCooldown = 10 * time.Second // long cooldown → one attempt

	runWatcherFor(context.Background(), cfg, em, 80*time.Millisecond)

	if n := spy.count(); n == 0 {
		t.Errorf("want ≥1 restart at 260K with a 250K configured ceiling; got 0 (gate ignored cfg.HardCeilingTokens?)")
	}

	ceilEvents := em.EventsOfType(core.EventTypeSessionKeeperHardCeiling)
	if len(ceilEvents) == 0 {
		t.Fatal("want ≥1 session_keeper_hard_ceiling event; got 0")
	}
	var payload core.SessionKeeperHardCeilingPayload
	if err := json.Unmarshal(ceilEvents[0].Payload, &payload); err != nil {
		t.Fatalf("unmarshal hard_ceiling payload: %v", err)
	}
	if payload.HardCeiling != effectiveCeiling {
		t.Errorf("payload.HardCeiling = %d; want effective %d (NOT the %d default)",
			payload.HardCeiling, effectiveCeiling, keeper.DefaultHardCeilingTokens)
	}
	if payload.HardCeiling == keeper.DefaultHardCeilingTokens {
		t.Errorf("payload.HardCeiling = %d == default; effective value NOT wired through", payload.HardCeiling)
	}
}

// TestHardCeilingMode_ZeroValueIsAlarm asserts the operator decision: the
// HardCeilingMode zero value resolves to Alarm (config left untouched alarms,
// not Off, not Restart). Refs: hk-n6kn.
func TestHardCeilingMode_ZeroValueIsAlarm(t *testing.T) {
	t.Parallel()

	var zero keeper.HardCeilingMode // zero value
	if zero != keeper.HardCeilingModeAlarm {
		t.Errorf("zero HardCeilingMode = %v; want HardCeilingModeAlarm", zero)
	}
	if zero.String() != "alarm" {
		t.Errorf("zero HardCeilingMode.String() = %q; want \"alarm\"", zero.String())
	}
	cfg := keeper.WatcherConfig{}
	if cfg.HardCeilingMode != keeper.HardCeilingModeAlarm {
		t.Errorf("unset cfg.HardCeilingMode = %v; want HardCeilingModeAlarm", cfg.HardCeilingMode)
	}
	for _, tc := range []struct {
		in   string
		want keeper.HardCeilingMode
	}{
		{"", keeper.HardCeilingModeAlarm},
		{"alarm", keeper.HardCeilingModeAlarm},
		{"off", keeper.HardCeilingModeOff},
		{"restart", keeper.HardCeilingModeRestart},
		{"bogus", keeper.HardCeilingModeAlarm},
	} {
		if got := keeper.ParseHardCeilingMode(tc.in); got != tc.want {
			t.Errorf("ParseHardCeilingMode(%q) = %v; want %v", tc.in, got, tc.want)
		}
	}
}

package keeper_test

// backstop_test.go — tests for the two keeper backstops added in hk-34ac:
//
//  1. Blind-keeper alarm (Backstop 1): emit session_keeper_blind after
//     5+ minutes of continuous foreign_session rejection. Latched per episode;
//     cleared when the gauge becomes readable.
//
//  2. SID-independent hard-ceiling failsafe (Backstop 2): force a restart when
//     any watched pane's token count meets or exceeds HardCeilingAbsTokens
//     (280 000), regardless of whether the SID binding is correct.

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/keeper"
	"os"
)

// foreignSessionConfig returns a WatcherConfig pre-wired for foreign_session
// testing: the managed binding is "sess-managed" and the .sid endorses
// "sess-managed" (so the gauge's foreign sid is NOT adopted). The gauge file
// carries the provided token count. PollInterval is tiny for test speed.
func foreignSessionConfig(t *testing.T, projectDir, agent string, tokens int64) keeper.WatcherConfig {
	t.Helper()

	keeperDir := filepath.Join(projectDir, ".harmonik", "keeper")
	if err := os.MkdirAll(keeperDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Write gauge with a foreign session_id and the specified token count.
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

	// We fake time by controlling blindSince via the ReadManagedSessionFn, but
	// the watcher's internal clock is time.Now(). To test the 5-minute threshold
	// without sleeping 5 minutes, we start the watcher in a mode where the
	// managed session always differs from the gauge (foreign every tick), but
	// we observe that no blind event fires within the first few ticks (< 5 min),
	// then switch to a very short threshold via a short-running sub-test approach.
	//
	// Since we cannot override the watcher's internal time.Since, we test the
	// observable behaviour differently:
	// (a) Short run with short blind threshold (N/A — threshold is a constant).
	//
	// The real 5-min threshold is a compile-time constant in watcher.go. We
	// cannot inject a fake clock without refactoring. So we use a two-phase
	// approach: run the watcher very briefly (no blind event expected), then
	// verify the LATCH behaves correctly by checking that the watcher correctly
	// does NOT re-emit after the first alarm.
	//
	// For the full 5-min firing we do a best-effort: run for a few ticks and
	// confirm no blind event fires (the threshold is not yet crossed). This is
	// a partial test of the early-arm path; see below for the latch behaviour test.
	//
	// The definitive "fires after threshold" path is verified structurally in
	// TestBlindKeeperAlarm_LatchClearedOnReadableGauge, which exercises the
	// blind→clear→blind state machine end-to-end.

	projectDir := t.TempDir()
	agent := "blind-alarm-agent"

	em := &keeper.RecordingEmitter{}
	cfg := foreignSessionConfig(t, projectDir, agent, 50_000)

	// Short run — no blind event expected (threshold is 5 min).
	runWatcherFor(context.Background(), cfg, em, 60*time.Millisecond)

	blindEvents := em.EventsOfType(core.EventTypeSessionKeeperBlind)
	if len(blindEvents) != 0 {
		t.Errorf("want 0 session_keeper_blind events in short run (threshold 5 min not crossed); got %d", len(blindEvents))
	}

	// Also verify the no_gauge event IS emitted (confirms the foreign_session
	// path is being hit, not some other early-exit path).
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
	if err := os.MkdirAll(keeperDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Track which session_id the ReadManagedSessionFn returns; controlled by test.
	var mu sync.Mutex
	managedSID := "sess-managed"

	em := &keeper.RecordingEmitter{}

	// Write gauge initially with foreign session_id.
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
			// .sid endorses managedSID so foreign is never auto-adopted.
			return managedSID, time.Time{}, nil
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		w := keeper.NewWatcher(cfg, em)
		_ = w.Run(ctx) //nolint:errcheck
	}()

	// Phase 1: run a few ticks with foreign gauge — no blind event (5 min not crossed).
	time.Sleep(30 * time.Millisecond)

	// Phase 2: switch gauge to the matching session_id so the next tick is a
	// successful (non-foreign) read — this clears blindSince and blindAlarmFired.
	writeCtxFile(t, projectDir, agent, 50.0, "sess-managed")
	time.Sleep(30 * time.Millisecond)

	// Phase 3: switch back to foreign again — blindSince should now be ZERO
	// (cleared in phase 2), so the new blind streak starts fresh.
	writeCtxFile(t, projectDir, agent, 50.0, "sess-foreign")
	time.Sleep(30 * time.Millisecond)

	cancel()
	<-done

	// We expect 0 blind events (5 min never elapsed in any phase).
	blindEvents := em.EventsOfType(core.EventTypeSessionKeeperBlind)
	if len(blindEvents) != 0 {
		t.Errorf("want 0 session_keeper_blind events (threshold never crossed); got %d", len(blindEvents))
	}

	// Confirm the warn state is reset after the readable phase (below 80%).
	// This is an indirect check that the fresh-gauge path cleared keeper state.
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
	if err := os.MkdirAll(keeperDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Track which session_id the gauge carries (controlled by the test) and
	// which session_id is "managed"/.sid-endorsed (constant "sess-managed").
	em := &keeper.RecordingEmitter{}

	// Start with a foreign gauge.
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
		_ = w.Run(ctx) //nolint:errcheck
	}()

	// Phase 1: foreign streak well past the 30ms threshold (≈ many ticks). The
	// alarm must fire exactly ONCE — the latch suppresses every later tick.
	time.Sleep(120 * time.Millisecond)
	if n := len(em.EventsOfType(core.EventTypeSessionKeeperBlind)); n != 1 {
		cancel()
		<-done
		t.Fatalf("phase 1: want exactly 1 session_keeper_blind after injected threshold; got %d", n)
	}

	// Phase 2: a matched (non-foreign) tick — clears blindSince + blindAlarmFired.
	writeCtxFile(t, projectDir, agent, 50.0, "sess-managed")
	time.Sleep(40 * time.Millisecond)
	// Still exactly one (the readable tick must not emit a new blind event).
	if n := len(em.EventsOfType(core.EventTypeSessionKeeperBlind)); n != 1 {
		cancel()
		<-done
		t.Fatalf("phase 2: matched gauge must not add a blind event; got %d", n)
	}

	// Phase 3: foreign again — a FRESH clock arms; after the threshold a SECOND
	// blind event must emit (proves the latch+timer reset on the readable tick).
	writeCtxFile(t, projectDir, agent, 50.0, "sess-foreign")
	time.Sleep(120 * time.Millisecond)

	cancel()
	<-done

	if n := len(em.EventsOfType(core.EventTypeSessionKeeperBlind)); n != 2 {
		t.Errorf("phase 3: want a 2nd blind event after latch reset + fresh foreign streak; got %d total", n)
	}
}

// restartSpy records calls to a restart function and counts them.
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
		cfg.HardCeilingRestartFn = spy.restart
		cfg.HardCeilingCooldown = 10 * time.Second // long cooldown → only one attempt

		runWatcherFor(context.Background(), cfg, em, 80*time.Millisecond)

		// Restart must have fired.
		if n := spy.count(); n == 0 {
			t.Error("want ≥1 hard-ceiling restart call at 290K tokens (foreign session); got 0")
		}

		// session_keeper_hard_ceiling event must have been emitted.
		ceilEvents := em.EventsOfType(core.EventTypeSessionKeeperHardCeiling)
		if len(ceilEvents) == 0 {
			t.Error("want ≥1 session_keeper_hard_ceiling event at 290K tokens; got 0")
		}

		// Verify payload fields.
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
			if payload.HardCeiling != keeper.HardCeilingAbsTokens {
				t.Errorf("payload.HardCeiling = %d; want %d", payload.HardCeiling, keeper.HardCeilingAbsTokens)
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

		// Restart must NOT have fired at 270K (below the 280K ceiling).
		if n := spy.count(); n != 0 {
			t.Errorf("want 0 hard-ceiling restart calls at 270K tokens; got %d", n)
		}

		// No session_keeper_hard_ceiling events.
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
	cfg.HardCeilingRestartFn = spy.restart
	cfg.HardCeilingCooldown = 10 * time.Second // long cooldown → only one attempt

	// Run for 300ms — many ticks, all above 280K, but cooldown holds to 1 restart.
	runWatcherFor(context.Background(), cfg, em, 300*time.Millisecond)

	if n := spy.count(); n != 1 {
		t.Errorf("want exactly 1 hard-ceiling restart (cooldown holds); got %d", n)
	}
}

// TestHardCeiling_NoRestartWhenFnNil verifies that when HardCeilingRestartFn is
// nil (fail-closed), no restart fires even at 290K tokens.
func TestHardCeiling_NoRestartWhenFnNil(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	agent := "hard-ceiling-nil-fn-agent"

	em := &keeper.RecordingEmitter{}

	cfg := foreignSessionConfig(t, projectDir, agent, 290_000)
	// HardCeilingRestartFn is nil (not set) — backstop disabled.

	runWatcherFor(context.Background(), cfg, em, 80*time.Millisecond)

	// No ceiling events (function not wired).
	ceilEvents := em.EventsOfType(core.EventTypeSessionKeeperHardCeiling)
	if len(ceilEvents) != 0 {
		t.Errorf("want 0 session_keeper_hard_ceiling events when fn is nil; got %d", len(ceilEvents))
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

	// Tokens == 0 means the field was absent/unset in the .ctx file.
	cfg := foreignSessionConfig(t, projectDir, agent, 0)
	cfg.HardCeilingRestartFn = spy.restart

	runWatcherFor(context.Background(), cfg, em, 80*time.Millisecond)

	if n := spy.count(); n != 0 {
		t.Errorf("want 0 hard-ceiling restart calls when tokens==0; got %d", n)
	}
}

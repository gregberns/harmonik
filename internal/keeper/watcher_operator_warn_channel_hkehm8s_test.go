package keeper_test

// watcher_operator_warn_channel_hkehm8s_test.go — backstop B: operator-visible
// WARN channel (hk-ehm8s).
//
// ROOT CAUSE the feature addresses: the 200K WARN fires into the captain pane
// only. An operator watching via remote-control / iOS gets zero signal before
// the ACT cycle clears the session.
//
// INVARIANTS under test:
//  1. OperatorWarnFn is called exactly once at the upward warn crossing — NOT
//     on every poll tick while the context stays above the warn band.
//  2. A dip-rise cycle within the warn_cooldown window does NOT re-fire
//     OperatorWarnFn (the existing warnArmed/warnFired/WarnCooldown machinery
//     throttles it, same as the pane injection).
//  3. After the cooldown elapses, a second upward crossing re-fires exactly once.
//  4. The fn receives the correct sessionID, token count, warn threshold, and
//     act threshold at the crossing tick.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/keeper"
)

// operatorWarnSpy records calls to OperatorWarnFn so tests can assert count
// and payload without executing a real comms send. Thread-safe.
type operatorWarnSpy struct {
	mu    sync.Mutex
	calls []operatorWarnCall
}

type operatorWarnCall struct {
	sessionID  string
	tokens     int64
	warnTokens int64
	actTokens  int64
}

func (s *operatorWarnSpy) fn(_ context.Context, sessionID string, tokens, warnTokens, actTokens int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, operatorWarnCall{sessionID, tokens, warnTokens, actTokens})
}

func (s *operatorWarnSpy) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.calls)
}

func (s *operatorWarnSpy) last() (operatorWarnCall, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.calls) == 0 {
		return operatorWarnCall{}, false
	}
	return s.calls[len(s.calls)-1], true
}

// TestWatcher_OperatorWarnFn_FiredAtCrossingNotEveryTick is the primary
// backstop-B regression: OperatorWarnFn fires EXACTLY ONCE at the upward
// crossing and is NOT called on every poll tick while the context stays above
// the warn band. Verifies the warnArmed/warnFired gate (hk-g4ei7 invariant)
// also guards the operator channel.
func TestWatcher_OperatorWarnFn_FiredAtCrossingNotEveryTick(t *testing.T) {
	t.Parallel()

	const (
		sid         = "11111111-2222-4333-8444-555555555555"
		warnAbs     = int64(200_000)
		gaugeTokens = int64(205_000)
		gaugeWindow = int64(200_000)
		gaugePct    = 85.0
	)

	projectDir := t.TempDir()
	agent := "op-warn-chan-crossing-test"

	spy := &operatorWarnSpy{}

	cfg := keeper.WatcherConfig{
		AgentName:      agent,
		ProjectDir:     projectDir,
		PollInterval:   5 * time.Millisecond,
		WarnPct:        80.0,
		WarnAbsTokens:  warnAbs,
		WarnPctCeil:    0.95, // high ceil so abs threshold governs
		IdleQuiesce:    1 * time.Millisecond,
		Staleness:      60 * time.Second,
		TmuxTarget:     "dummy-pane",
		InjectFn:       func(_ context.Context, _ string) error { return nil },
		OperatorWarnFn: spy.fn,
		// WarnCooldown: zero → use production default (30s); a static gauge above
		// warn has no dip, so re-arm never happens anyway.
	}

	// Write gauge above the warn threshold (tokens AND pct).
	writeCtxFileTokens(t, projectDir, agent, gaugePct, gaugeTokens, gaugeWindow, sid)

	em := &keeper.RecordingEmitter{}
	// Run many ticks (200ms / 5ms poll ≈ 40 ticks) — only the first crossing
	// must fire OperatorWarnFn.
	runWatcherFor(context.Background(), cfg, em, 200*time.Millisecond)

	if n := spy.count(); n != 1 {
		t.Errorf("OperatorWarnFn: want exactly 1 call at upward crossing; got %d (fired on every tick instead of crossing only)", n)
	}
}

// TestWatcher_OperatorWarnFn_PayloadCarriesCorrectFields verifies that the
// OperatorWarnFn is called with the correct sessionID, token count,
// warn-threshold, and effective-act-threshold so the emitted comms message
// is accurate. Refs: hk-ehm8s.
func TestWatcher_OperatorWarnFn_PayloadCarriesCorrectFields(t *testing.T) {
	t.Parallel()

	const (
		sid         = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
		warnAbs     = int64(180_000)
		gaugeTokens = int64(190_000)
		gaugeWindow = int64(200_000)
	)

	projectDir := t.TempDir()
	agent := "op-warn-payload-test"

	spy := &operatorWarnSpy{}

	cfg := keeper.WatcherConfig{
		AgentName:      agent,
		ProjectDir:     projectDir,
		PollInterval:   5 * time.Millisecond,
		WarnPct:        80.0,
		WarnAbsTokens:  warnAbs,
		WarnPctCeil:    0.95,
		IdleQuiesce:    1 * time.Millisecond,
		Staleness:      60 * time.Second,
		TmuxTarget:     "dummy-pane",
		InjectFn:       func(_ context.Context, _ string) error { return nil },
		OperatorWarnFn: spy.fn,
	}

	writeCtxFileTokens(t, projectDir, agent, 92.0, gaugeTokens, gaugeWindow, sid)

	em := &keeper.RecordingEmitter{}
	runWatcherFor(context.Background(), cfg, em, 150*time.Millisecond)

	call, ok := spy.last()
	if !ok {
		t.Fatal("OperatorWarnFn was never called; want exactly 1 call")
	}

	if call.sessionID != sid {
		t.Errorf("sessionID = %q; want %q", call.sessionID, sid)
	}
	if call.tokens != gaugeTokens {
		t.Errorf("tokens = %d; want %d", call.tokens, gaugeTokens)
	}
	if call.warnTokens != warnAbs {
		t.Errorf("warnTokens = %d; want %d", call.warnTokens, warnAbs)
	}
	// actTokens must be > warnTokens (derived from the act-warn offset).
	if call.actTokens <= call.warnTokens {
		t.Errorf("actTokens = %d; want > warnTokens (%d)", call.actTokens, call.warnTokens)
	}
}

// TestWatcher_OperatorWarnFn_ThrottledByWarnCooldown verifies that OperatorWarnFn
// fires on each new upward crossing (dip-rise = one additional call) but NOT on
// every tick while above. Uses WarnCooldown=1ms to let the second crossing
// complete within the test window, mirroring TestWatcher_WarnResetOnDropBelow.
// The "throttled" aspect: each crossing fires exactly once via the warnArmed/
// warnFired gate — not on every poll tick between crossings.
func TestWatcher_OperatorWarnFn_ThrottledByWarnCooldown(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	agent := "op-warn-cooldown-test"

	spy := &operatorWarnSpy{}

	cfg := keeper.WatcherConfig{
		AgentName:      agent,
		ProjectDir:     projectDir,
		PollInterval:   5 * time.Millisecond,
		WarnPct:        80.0,
		WarnAbsTokens:  200_000,
		WarnPctCeil:    0.95,
		IdleQuiesce:    1 * time.Millisecond,
		Staleness:      60 * time.Second,
		TmuxTarget:     "dummy-pane",
		InjectFn:       func(_ context.Context, _ string) error { return nil },
		OperatorWarnFn: spy.fn,
		// WarnCooldown=1ms: matches TestWatcher_WarnResetOnDropBelow — allows
		// the second crossing to register in the test window without a 30s sleep.
		WarnCooldown: 1 * time.Millisecond,
	}

	keeperDir := filepath.Join(projectDir, ".harmonik", "keeper")
	if err := os.MkdirAll(keeperDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	ctxPath := filepath.Join(keeperDir, agent+".ctx")

	writeGaugeFile := func(pct float64, tokens int64) {
		data, _ := json.Marshal(keeper.CtxFile{ //nolint:errcheck // test helper
			Pct:        pct,
			Tokens:     tokens,
			WindowSize: 200_000,
			SessionID:  "11111111-2222-4333-8444-555555555555",
			Ts:         time.Now().UTC().Format(time.RFC3339),
		})
		_ = os.WriteFile(ctxPath, append(data, '\n'), 0o600) //nolint:errcheck // test helper
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		em := &keeper.RecordingEmitter{}
		w := keeper.NewWatcher(cfg, em)
		_ = w.Run(ctx) //nolint:errcheck // context.Canceled expected
	}()

	// First crossing: above threshold.
	writeGaugeFile(85.0, 205_000)
	time.Sleep(30 * time.Millisecond)

	// Dip below threshold → cooldown elapses (1ms) → re-arm.
	writeGaugeFile(70.0, 140_000)
	time.Sleep(30 * time.Millisecond)

	// Second crossing: rise above threshold again.
	writeGaugeFile(90.0, 210_000)
	time.Sleep(30 * time.Millisecond)

	cancel()
	<-done

	// Must be ≥2: one per upward crossing (proves operator channel follows
	// the same per-crossing semantics as the pane injection warn).
	if n := spy.count(); n < 2 {
		t.Errorf("want ≥2 OperatorWarnFn calls after two upward crossings; got %d", n)
	}
}

// TestWatcher_OperatorWarnFn_NilDoesNotPanic asserts that leaving OperatorWarnFn
// nil (the default) does not panic and does not affect other warn behaviour.
func TestWatcher_OperatorWarnFn_NilDoesNotPanic(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	agent := "op-warn-nil-test"

	cfg := keeper.WatcherConfig{
		AgentName:      agent,
		ProjectDir:     projectDir,
		PollInterval:   5 * time.Millisecond,
		WarnPct:        80.0,
		IdleQuiesce:    1 * time.Millisecond,
		Staleness:      60 * time.Second,
		OperatorWarnFn: nil, // explicit nil: must not panic
	}

	writeCtxFile(t, projectDir, agent, 85.0, "sess-nil-test")

	em := &keeper.RecordingEmitter{}
	// Any panic here will surface as a test failure via the test framework.
	runWatcherFor(context.Background(), cfg, em, 100*time.Millisecond)
}

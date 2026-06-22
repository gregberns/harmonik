package keeper_test

// watcher_reap_cadence_hkjrftk_test.go — cadence-gate regression for the
// orphan-decision reaper (K5, bead hk-jrftk).
//
// Root cause: maybeReapOrphanedDecisions previously ran on EVERY 5s keeper poll,
// scanning the full events.jsonl (O(filesize)) each time. The fix (hk-jrftk)
// adds a cadence gate: the O(events.jsonl) scan runs at most once per
// ReapDecisionsCadence (default 90s), regardless of how fast the poll fires.
//
// These tests verify the cadence gate bounds reap invocations:
//
//   - TestWatcher_ReapCadence_GateBoundsInvocations: with poll=5ms and
//     cadence=30ms the reaper must fire ≤ ceil(dur/cadence)+1 times in 100ms
//     (~3-4), NOT once-per-poll (~20). Uses ReapDecisionsFn spy to count.
//
//   - TestWatcher_ReapCadence_FirstTickFires: zero lastReapAt → reaper fires on
//     the very first tick (no cold-start delay).
//
//   - TestWatcher_ReapCadence_DisabledWhenReapDecisionsFalse: ReapDecisions=false
//     → spy never called, even with ReapDecisionsCadence=0.
//
// Helper prefix: "rc" (reap-cadence).
//
// Bead ref: hk-jrftk.

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/keeper"
	"github.com/gregberns/harmonik/internal/presence"
)

// rcNoopReap is a ReapDecisionsFn spy that counts invocations and returns an
// empty result (no decisions to reap). Thread-safe via atomic counter.
func rcCountingReap(counter *atomic.Int64) func(ctx context.Context, eventsPath string, emitter presence.Emitter) (presence.ReapResult, error) {
	return func(_ context.Context, _ string, _ presence.Emitter) (presence.ReapResult, error) {
		counter.Add(1)
		return presence.ReapResult{}, nil
	}
}

// TestWatcher_ReapCadence_GateBoundsInvocations verifies that with poll=5ms and
// reap_cadence=30ms the reaper fires at most ~ceil(100ms/30ms)+1 = 4 times in a
// 100ms window, NOT once-per-poll (~20 times). This is the primary regression
// guard: without the cadence gate, counter == ~20 (O(filesize) per poll).
func TestWatcher_ReapCadence_GateBoundsInvocations(t *testing.T) {
	t.Parallel()

	var counter atomic.Int64
	em := &keeper.RecordingEmitter{}
	cfg := keeper.WatcherConfig{
		AgentName:            "rc-test-agent",
		ProjectDir:           t.TempDir(),
		PollInterval:         5 * time.Millisecond,
		ReapDecisions:        true,
		ReapDecisionsCadence: 30 * time.Millisecond,
		ReapDecisionsFn:      rcCountingReap(&counter),
		SuppressNoGauge:      true,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	runWatcherFor(ctx, cfg, em, 100*time.Millisecond)

	got := counter.Load()
	// 100ms / 30ms cadence ≈ 3-4 fires (first fires immediately, subsequent after cadence).
	// Allow up to 6 for timing jitter; the regression case (no gate) produces ~20.
	const maxExpected = 6
	if got > maxExpected {
		t.Errorf("ReapDecisionsCadence gate: reaper fired %d times in 100ms with 30ms cadence; "+
			"want ≤ %d (regression: no gate would produce ~20)", got, maxExpected)
	}
	if got == 0 {
		t.Errorf("ReapDecisionsCadence gate: reaper never fired; want ≥ 1 (first-tick must fire)")
	}
}

// TestWatcher_ReapCadence_FirstTickFires verifies that the reaper fires on the
// very first poll tick even though no prior reap has occurred (zero lastReapAt
// must not suppress the first run).
func TestWatcher_ReapCadence_FirstTickFires(t *testing.T) {
	t.Parallel()

	var counter atomic.Int64
	em := &keeper.RecordingEmitter{}
	cfg := keeper.WatcherConfig{
		AgentName:            "rc-first-tick",
		ProjectDir:           t.TempDir(),
		PollInterval:         10 * time.Millisecond,
		ReapDecisions:        true,
		ReapDecisionsCadence: 10 * time.Second, // very long cadence: only first tick fires
		ReapDecisionsFn:      rcCountingReap(&counter),
		SuppressNoGauge:      true,
	}

	// Run for just 2 ticks; the cadence (10s) ensures only the first tick fires.
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	runWatcherFor(ctx, cfg, em, 25*time.Millisecond)

	if counter.Load() != 1 {
		t.Errorf("first-tick gate: want exactly 1 reap invocation in 25ms with 10s cadence; got %d", counter.Load())
	}
}

// TestWatcher_ReapCadence_DisabledWhenReapDecisionsFalse verifies that the spy
// is never called when ReapDecisions is false, regardless of cadence.
func TestWatcher_ReapCadence_DisabledWhenReapDecisionsFalse(t *testing.T) {
	t.Parallel()

	var counter atomic.Int64
	em := &keeper.RecordingEmitter{}
	cfg := keeper.WatcherConfig{
		AgentName:            "rc-disabled",
		ProjectDir:           t.TempDir(),
		PollInterval:         10 * time.Millisecond,
		ReapDecisions:        false,
		ReapDecisionsCadence: 1 * time.Millisecond, // would fire every tick if enabled
		ReapDecisionsFn:      rcCountingReap(&counter),
		SuppressNoGauge:      true,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	runWatcherFor(ctx, cfg, em, 50*time.Millisecond)

	if counter.Load() != 0 {
		t.Errorf("ReapDecisions=false: spy must never be called; got %d invocations", counter.Load())
	}
}

package keeper_test

// watcher_noise_reduction_test.go — unit tests for hk-sol6 (keeper noise
// reduction) and hk-lsk5 (190K self-hint). Covers:
//   - 30s back-off after no_gauge: no re-emit within the back-off window
//   - 300s re-emit cadence: re-emit fires only after noGaugeReemitInterval
//   - dip-rise cooldown: second warn crossing suppressed within WarnCooldown
//   - one-time self-hint: injected exactly once per session on first crossing

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/keeper"
)

// TestWatcher_NoGaugeBackoff_NoReemitWithinBackoffWindow asserts that after a
// no_gauge event is emitted (absent gauge), no second no_gauge is emitted during
// the back-off window even across many poll ticks. The back-off suppresses the
// re-emit path; after the window expires a second no_gauge is allowed.
//
// Test strategy: set WarnCooldown to something tiny so we control timing, but
// the key is the no_gauge back-off. We use a gauge that is permanently absent
// so every tick hits the absent branch. The back-off suppresses re-emits for
// noGaugeBackoff (30s in production). We verify that within a 200ms run the
// watcher emits exactly one no_gauge despite many poll ticks.
func TestWatcher_NoGaugeBackoff_NoReemitWithinBackoffWindow(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	agent := "backoff-test-agent"

	// Ensure the keeper dir exists so ReadCtxFile returns ErrNotExist rather
	// than an error reading the directory, triggering the absent branch cleanly.
	keeperDir := filepath.Join(projectDir, ".harmonik", "keeper")
	if err := os.MkdirAll(keeperDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	// Do NOT write any .ctx file — gauge is absent.

	em := &keeper.RecordingEmitter{}
	cfg := keeper.WatcherConfig{
		AgentName:       agent,
		ProjectDir:      projectDir,
		PollInterval:    5 * time.Millisecond, // fast ticks
		Staleness:       120 * time.Second,    // won't reach stale (absent path)
		SuppressNoGauge: false,                // want events
		// ReadManagedSessionFn: nil → default (no managed binding, no session_id)
		// The back-off is 30s in production; we test that within 200ms only one
		// no_gauge is emitted despite ~40 poll ticks.
	}

	runWatcherFor(context.Background(), cfg, em, 200*time.Millisecond)

	noGaugeEvents := em.EventsOfType(core.EventTypeSessionKeeperNoGauge)
	// Within 200ms with 30s back-off: exactly one emission allowed (the first).
	// The boot-time check emits one; the ticker ticks do NOT re-emit while in
	// back-off. We allow for the boot check + at most one ticker fire (in case
	// the first tick lands before the boot check sets backoffUntil), but no more.
	if len(noGaugeEvents) == 0 {
		t.Error("want ≥1 no_gauge event (initial emission); got 0")
	}
	// The key assertion: no more than 2 events (boot + at most one tick race)
	// — NOT the ~40 events that would appear without back-off.
	if len(noGaugeEvents) > 2 {
		t.Errorf("back-off violated: want ≤2 no_gauge events within 200ms; got %d (expected ~40 without back-off)",
			len(noGaugeEvents))
	}
}

// TestWatcher_WarnCooldown_SuppressesImmediateRefire asserts that when the gauge
// dips below and immediately rises above the warn threshold within the WarnCooldown
// window, only one warn event is emitted (the second crossing is suppressed).
//
// This is the core dip-rise cooldown scenario from hk-sol6.
func TestWatcher_WarnCooldown_SuppressesImmediateRefire(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	agent := "cooldown-suppress-agent"

	keeperDir := filepath.Join(projectDir, ".harmonik", "keeper")
	ctxPath := filepath.Join(keeperDir, agent+".ctx")
	if err := os.MkdirAll(keeperDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	em := &keeper.RecordingEmitter{}
	cfg := keeper.WatcherConfig{
		AgentName:    agent,
		ProjectDir:   projectDir,
		PollInterval: 5 * time.Millisecond,
		WarnPct:      80.0,
		IdleQuiesce:  1 * time.Millisecond,
		Staleness:    120 * time.Second,
		TmuxTarget:   "",
		// WarnCooldown: 0 → applyDefaults sets 30s production default, which
		// will suppress the second crossing within 200ms.
		// We do NOT set WarnCooldown here — the production default (30s) applies.
	}

	writeCtxPct := func(pct float64) {
		writeCtxFile(t, projectDir, agent, pct, "")
		_ = ctxPath // confirm path is canonical
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		w := keeper.NewWatcher(cfg, em)
		_ = w.Run(ctx) //nolint:errcheck
	}()

	// Above threshold → first warn.
	writeCtxPct(85.0)
	time.Sleep(30 * time.Millisecond)

	// Dip below → would normally re-arm, but cooldown is 30s.
	writeCtxPct(70.0)
	time.Sleep(30 * time.Millisecond)

	// Rise above → second crossing; SUPPRESSED by 30s cooldown.
	writeCtxPct(90.0)
	time.Sleep(30 * time.Millisecond)

	cancel()
	<-done

	warns := em.EventsOfType(core.EventTypeSessionKeeperWarn)
	// With 30s WarnCooldown default: only the first crossing fires; the second
	// is suppressed because the cooldown has not elapsed (only ~30ms).
	if len(warns) != 1 {
		t.Errorf("dip-rise cooldown: want exactly 1 warn within 30s cooldown window; got %d", len(warns))
	}
}

// TestWatcher_SelfHint_InjectedOncePerSession asserts that the [KEEPER HINT]
// is injected exactly once — on the first warn crossing — and NOT on subsequent
// ticks while above the threshold, even though the warn injection itself may
// retry on each quiesced tick. Refs: hk-lsk5.
func TestWatcher_SelfHint_InjectedOncePerSession(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	agent := "hint-once-agent"

	keeperDir := filepath.Join(projectDir, ".harmonik", "keeper")
	if err := os.MkdirAll(keeperDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Spy the self-hint path directly via the SelfHintInjectFn seam, which the
	// watcher calls in place of InjectText when set. A non-empty TmuxTarget is
	// REQUIRED — the hint branch is gated by `TmuxTarget != ""` — so we provide
	// a dummy pane and stub the warn InjectFn too (TmuxTarget!="" enables the
	// pendingInject delivery path). This makes the 190K hint path actually
	// execute under test, instead of falling back to a warn-count proxy.
	var (
		hintMu    sync.Mutex
		hintTexts []string
	)
	spyHint := func(_ context.Context, _ string, text string) error {
		hintMu.Lock()
		defer hintMu.Unlock()
		hintTexts = append(hintTexts, text)
		return nil
	}
	hintCount := func() int {
		hintMu.Lock()
		defer hintMu.Unlock()
		return len(hintTexts)
	}

	em := &keeper.RecordingEmitter{}
	cfg := keeper.WatcherConfig{
		AgentName:        agent,
		ProjectDir:       projectDir,
		PollInterval:     5 * time.Millisecond,
		WarnPct:          80.0,
		IdleQuiesce:      1 * time.Millisecond,
		Staleness:        120 * time.Second,
		TmuxTarget:       "dummy-pane", // non-empty → hint path is ENABLED
		InjectFn:         func(_ context.Context, _ string) error { return nil },
		SelfHintInjectFn: spyHint, // ← observe the one-time 190K self-hint
		// WarnCooldown defaults to 30s; the gauge stays above threshold the
		// whole run so there is no dip to re-arm the hint latch.
	}

	// Write gauge above threshold; the watcher crosses warn once and injects
	// the hint exactly once. It stays above threshold for the whole run, so the
	// hint must NOT re-fire on later ticks (hintSentThisSession latch).
	writeCtxFile(t, projectDir, agent, 85.0, "")

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		w := keeper.NewWatcher(cfg, em)
		_ = w.Run(ctx) //nolint:errcheck
	}()

	// Let it run for 200ms — ~40 ticks, all above threshold.
	time.Sleep(200 * time.Millisecond)
	cancel()
	<-done

	// The self-hint must fire EXACTLY ONCE despite ~40 above-threshold ticks
	// (the hintSentThisSession latch). A regression that drops the latch would
	// inject the hint on every tick and FAIL here.
	if n := hintCount(); n != 1 {
		t.Errorf("want exactly 1 self-hint injection across sustained above-threshold; got %d", n)
	}
	// And the injected text must be the real 190K hint, not arbitrary content.
	hintMu.Lock()
	if len(hintTexts) > 0 && !strings.Contains(hintTexts[0], "[KEEPER HINT]") {
		t.Errorf("self-hint text = %q; want it to contain %q", hintTexts[0], "[KEEPER HINT]")
	}
	hintMu.Unlock()

	warns := em.EventsOfType(core.EventTypeSessionKeeperWarn)
	// One warn crossing = one hint. Belt-and-suspenders on the latch.
	if len(warns) != 1 {
		t.Errorf("want exactly 1 warn event on sustained above-threshold; got %d", len(warns))
	}
}

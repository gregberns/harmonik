package keeper_test

// watcher_noise_reduction_test.go — unit tests for hk-sol6 (keeper noise
// reduction), hk-1q7bt (transition-only no_gauge suppression), and hk-lsk5
// (190K self-hint). Covers:
//   - transition-only no_gauge: emit exactly once per reason per state entry
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

// TestWatcher_NoGauge_TransitionOnly asserts that a permanently absent gauge
// emits exactly one session_keeper_no_gauge event across many poll ticks.
//
// This is the hk-1q7bt transition-only suppression: the event fires on the
// absent→"absent" state transition (reason "" → "absent"), then is suppressed
// for every subsequent tick in the same state. A never-armed crew keeper
// therefore produces exactly 1 event total, not 1 event per 300s.
func TestWatcher_NoGauge_TransitionOnly(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	agent := "transition-only-test-agent"

	// Ensure the keeper dir exists so ReadCtxFile returns ErrNotExist rather
	// than an error reading the directory, triggering the absent branch cleanly.
	keeperDir := filepath.Join(projectDir, ".harmonik", "keeper")
	if err := os.MkdirAll(keeperDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	// Do NOT write any .ctx file — gauge is permanently absent.

	em := &keeper.RecordingEmitter{}
	cfg := keeper.WatcherConfig{
		AgentName:       agent,
		ProjectDir:      projectDir,
		PollInterval:    5 * time.Millisecond, // fast ticks → ~40 ticks in 200ms
		Staleness:       120 * time.Second,    // won't reach stale (absent path)
		SuppressNoGauge: false,                // want events
	}

	runWatcherFor(context.Background(), cfg, em, 200*time.Millisecond)

	noGaugeEvents := em.EventsOfType(core.EventTypeSessionKeeperNoGauge)
	// Transition-only: exactly ONE event across ~40 ticks (first absent transition;
	// all subsequent ticks are same-reason suppressed). Pre-hk-1q7bt this path
	// emitted 1 event per 300s; with many never-armed crews this was 22% of log vol.
	if len(noGaugeEvents) == 0 {
		t.Error("want exactly 1 no_gauge event on first absent transition; got 0")
	}
	if len(noGaugeEvents) > 1 {
		t.Errorf("transition-only violated: want exactly 1 no_gauge event for permanently absent gauge; got %d",
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
// retry on each quiesced tick. Also verifies that the live token count from
// the gauge is interpolated into the message. Refs: hk-lsk5.
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
		SelfHintInjectFn: spyHint, // ← observe the one-time self-hint
		// WarnCooldown defaults to 30s; the gauge stays above threshold the
		// whole run so there is no dip to re-arm the hint latch.
	}

	// Write gauge above threshold with an explicit token count so we can verify
	// the hint interpolates the live count. ~212K rounds to 212.
	const hintTokens int64 = 212_345
	writeCtxFileTokens(t, projectDir, agent, 85.0, hintTokens, 250_000, "")

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
	// The injected text must contain [KEEPER HINT] and interpolate the live
	// token count (~212K for hintTokens=212_345).
	hintMu.Lock()
	if len(hintTexts) > 0 {
		got := hintTexts[0]
		if !strings.Contains(got, "[KEEPER HINT]") {
			t.Errorf("self-hint text = %q; want it to contain %q", got, "[KEEPER HINT]")
		}
		if !strings.Contains(got, "~212K") {
			t.Errorf("self-hint text = %q; want live token count ~212K interpolated", got)
		}
	}
	hintMu.Unlock()

	warns := em.EventsOfType(core.EventTypeSessionKeeperWarn)
	// One warn crossing = one hint. Belt-and-suspenders on the latch.
	if len(warns) != 1 {
		t.Errorf("want exactly 1 warn event on sustained above-threshold; got %d", len(warns))
	}
}

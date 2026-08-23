package keeper_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
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

	keeperDir := filepath.Join(projectDir, ".harmonik", "keeper")
	if err := os.MkdirAll(keeperDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

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
	if err := os.MkdirAll(keeperDir, 0o700); err != nil {
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
		if err := w.Run(ctx); err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("Watcher.Run: %v", err)
		}
	}()

	writeCtxPct(85.0)
	time.Sleep(30 * time.Millisecond)

	writeCtxPct(70.0)
	time.Sleep(30 * time.Millisecond)

	writeCtxPct(90.0)
	time.Sleep(30 * time.Millisecond)

	cancel()
	<-done

	warns := em.EventsOfType(core.EventTypeSessionKeeperWarn)
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
	if err := os.MkdirAll(keeperDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

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
		// Session is awake — this test exercises the once-per-session latch, not
		// the sleep gate. Set explicitly because the default IsSleeping
		// fail-closes to true on the empty session id this fixture uses (hk-bzol4
		// sleep-gates the hint, so an indeterminate sid would suppress it).
		SleepingCheckFn: func(_, _ string) bool { return false },
	}

	const hintTokens int64 = 212_345
	writeCtxFileTokens(t, projectDir, agent, 85.0, hintTokens, 250_000, "")

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		w := keeper.NewWatcher(cfg, em)
		if err := w.Run(ctx); err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("Watcher.Run: %v", err)
		}
	}()

	time.Sleep(200 * time.Millisecond)
	cancel()
	<-done

	if n := hintCount(); n != 1 {
		t.Errorf("want exactly 1 self-hint injection across sustained above-threshold; got %d", n)
	}
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
	if len(warns) != 1 {
		t.Errorf("want exactly 1 warn event on sustained above-threshold; got %d", len(warns))
	}
}

// TestWatcher_SelfHint_SleepGated is the hk-bzol4 regression: the one-time
// self-hint inject must honor the M3 sleep gate, exactly like the warn advisory
// does. The QuiesceArbiter parks a session by writing .harmonik/.sleeping.<sid>;
// while that marker is present the keeper MUST NOT type [KEEPER HINT] into the
// pane, or it wakes the very session it just put to sleep (violating the
// SleepingCheckFn contract: "suppresses BOTH warn pane-injection AND cycle
// dispatch so the sleeping session is not woken by its own keeper").
//
// The bug: the self-hint fired with no SleepingCheckFn guard, so a session that
// crossed warn while parked got its pane woken by the hint even though the warn
// advisory 12 lines below was correctly suppressed.
//
// This test drives the gauge above warn with the session marked sleeping and
// asserts hint count == 0 throughout; then it clears the sleeping marker and
// asserts the deferred hint is delivered exactly once on wake (retry-after-wake,
// mirroring the pendingInject deferral).
func TestWatcher_SelfHint_SleepGated(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	agent := "hint-sleep-agent"

	keeperDir := filepath.Join(projectDir, ".harmonik", "keeper")
	if err := os.MkdirAll(keeperDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

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

	var sleeping atomic.Bool
	sleeping.Store(true)

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
		SelfHintInjectFn: spyHint, // ← observe (and gate) the one-time self-hint
		SleepingCheckFn: func(_ /*projectDir*/, _ /*sessionID*/ string) bool {
			return sleeping.Load()
		},
	}

	writeCtxFileTokens(t, projectDir, agent, 85.0, 212_345, 250_000, "")

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		w := keeper.NewWatcher(cfg, em)
		if err := w.Run(ctx); err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("Watcher.Run: %v", err)
		}
	}()

	time.Sleep(200 * time.Millisecond)
	if n := hintCount(); n != 0 {
		t.Fatalf("self-hint fired %d time(s) while the session was parked; want 0 (sleep gate must suppress it)", n)
	}

	sleeping.Store(false)
	deadline := time.Now().Add(1 * time.Second)
	for hintCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done

	if n := hintCount(); n != 1 {
		t.Errorf("after wake, want exactly 1 deferred self-hint delivery; got %d", n)
	}
	hintMu.Lock()
	if len(hintTexts) > 0 && !strings.Contains(hintTexts[0], "[KEEPER HINT]") {
		t.Errorf("self-hint text = %q; want it to contain %q", hintTexts[0], "[KEEPER HINT]")
	}
	hintMu.Unlock()
}

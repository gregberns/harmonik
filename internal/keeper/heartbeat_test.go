package keeper_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/keeper"
)

// readCtxFor reads and parses <projectDir>/.harmonik/keeper/<agent>.ctx and
// returns the parsed gauge plus the file mod-time.
func readCtxFor(t *testing.T, projectDir, agent string) (keeper.CtxFile, time.Time) {
	t.Helper()
	path := filepath.Join(projectDir, ".harmonik", "keeper", agent+".ctx")
	//nolint:gosec // G304: path is constructed under this test's t.TempDir fixture.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read ctx: %v", err)
	}
	var cf keeper.CtxFile
	if err := json.Unmarshal(raw, &cf); err != nil {
		t.Fatalf("unmarshal ctx: %v", err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat ctx: %v", err)
	}
	return cf, st.ModTime()
}

// writeStaleCtx writes a .ctx whose mod-time is set old enough to be past the
// HeartbeatThreshold (and Staleness) immediately, exercising the heartbeat /
// stale branches on the first tick.
func writeStaleCtx(t *testing.T, projectDir, agent string, cf keeper.CtxFile, age time.Duration) {
	t.Helper()
	keeperDir := filepath.Join(projectDir, ".harmonik", "keeper")
	if err := os.MkdirAll(keeperDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	path := filepath.Join(keeperDir, agent+".ctx")
	if err := keeper.WriteCtxFile(projectDir, agent, &cf); err != nil {
		t.Fatalf("WriteCtxFile: %v", err)
	}
	old := time.Now().Add(-age)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
}

func noGaugeStaleCount(em *keeper.RecordingEmitter) int {
	n := 0
	for _, ev := range em.EventsOfType(core.EventTypeSessionKeeperNoGauge) {
		var p core.SessionKeeperNoGaugePayload
		if err := json.Unmarshal(ev.Payload, &p); err == nil && p.Reason == "stale" {
			n++
		}
	}
	return n
}

// TestHeartbeat_KeepsLiveGaugeFresh is the load-bearing assertion for hk-81wk:
// on a LIVE pane (agent process running), an aging gauge must be re-written by
// the keeper-side heartbeat so it NEVER reaches the stale branch. Without the
// heartbeat the gauge ages past Staleness and the watcher emits
// no_gauge:stale, continuing past BOTH triggers.
//
// TIME IS VIRTUAL HERE, and that is the repair (hk-vp02y). The watcher runs on
// a clock anchored to the seeded gauge's real mod-time, so the boot-time age is
// EXACTLY the seeded age and each tick adds a whole PollInterval. A gauge
// mod-time is a real file time and cannot carry virtual time, so once the first
// heartbeat write lands the age tracks the virtual time elapsed since the run
// began. Every crossing is therefore a fixed tick number:
//
//	boot     age 3s  — below Staleness, so the boot-time check is silent
//	tick 1   age 4s  — above HeartbeatThreshold, so the heartbeat writes
//	tick 5   age 5s  — the age reaches Staleness, and from here the only thing
//	                   that keeps the gauge out of the stale branch is the
//	                   heartbeat writing on the same pass
//
// Nothing above is a rate. The boot-time age is exactly seedAge — the anchor
// puts no real clock on that path at all. The real clock enters once, after the
// first write: the file lands at wall-clock time while virtual time keeps
// running ahead, so the age reads as virtual elapsed MINUS the real time the
// run has spent. A slow box therefore makes the gauge look YOUNGER, which is
// the direction this test wants, and cannot manufacture the stale event it
// asserts against.
func TestHeartbeat_KeepsLiveGaugeFresh(t *testing.T) {
	t.Parallel()

	const (
		// seedAge sits above HeartbeatThreshold, so the heartbeat is due on the
		// first tick, and below Staleness, so the boot-time check stays silent.
		seedAge            = 3 * time.Second
		heartbeatThreshold = 2500 * time.Millisecond
		staleness          = 5 * time.Second
		pollInterval       = 1 * time.Second
		// 12 ticks is twelve virtual seconds, more than twice Staleness. With
		// the heartbeat removed the gauge freezes at the seed and reaches
		// Staleness on tick 2.
		ticks = 12
	)

	projectDir := t.TempDir()
	agent := "test-agent"
	// Latch a real UUIDv4 so the heartbeat stamps it back into the gauge.
	managedSID := "11111111-2222-4333-8444-555555555555"
	keeperDir := filepath.Join(projectDir, ".harmonik", "keeper")
	if err := os.MkdirAll(keeperDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := keeper.WriteManagedSessionID(projectDir, agent, managedSID); err != nil {
		t.Fatalf("WriteManagedSessionID: %v", err)
	}

	// Seed a gauge that is already past the heartbeat threshold (but not yet
	// stale) so the heartbeat fires on the first tick.
	writeStaleCtx(t, projectDir, agent, keeper.CtxFile{
		Pct:       50.0,
		Tokens:    100_000,
		SessionID: managedSID,
		Ts:        time.Now().UTC().Format(time.RFC3339),
	}, seedAge)
	_, seededModTime := readCtxFor(t, projectDir, agent)

	em := &keeper.RecordingEmitter{}
	cfg := keeper.WatcherConfig{
		AgentName:          agent,
		ProjectDir:         projectDir,
		PollInterval:       pollInterval,
		WarnPct:            80.0,
		IdleQuiesce:        1 * time.Millisecond,
		Staleness:          staleness,
		HeartbeatEnabled:   true,
		HeartbeatThreshold: heartbeatThreshold,
		TmuxTarget:         "fake:0.0",
		// Pane is alive (agent running) → heartbeat must keep the gauge fresh.
		IsPaneIdleFn: func(context.Context, string) bool { return false },
		// No transcript on disk → heartbeat carries last-good tokens forward.
		TranscriptDir: filepath.Join(projectDir, "no-such-transcript-dir"),
		// The derive-miss budget is a different contract, tested by
		// TestHeartbeat_DeriveMissBudget_SuppressesCarryForward. Set it above
		// the tick count so it cannot end the run early and change the subject.
		HeartbeatMaxMisses: 1000,
	}

	driveWatcherFakeClockFrom(t, seededModTime.Add(seedAge), cfg, em, ticks)

	if got := noGaugeStaleCount(em); got != 0 {
		t.Fatalf("expected 0 no_gauge:stale events on a live pane, got %d", got)
	}
	cf, modTime := readCtxFor(t, projectDir, agent)
	// An OCCURRENCE assertion, not a rate: did the heartbeat write at all? The
	// old form asked whether the gauge was younger than Staleness by the real
	// clock, measured from outside the watcher after the run had ended. That
	// reads the test harness's own scheduling delay, not the product.
	if !modTime.After(seededModTime) {
		t.Fatalf("gauge was not refreshed: mod-time %v is still the seeded %v", modTime, seededModTime)
	}
	if cf.SessionID != managedSID {
		t.Fatalf("heartbeat stamped session_id %q, want managed %q", cf.SessionID, managedSID)
	}
	if cf.Tokens != 100_000 {
		t.Fatalf("heartbeat lost last-good tokens: got %d, want 100000", cf.Tokens)
	}
}

// TestHeartbeat_IdlePaneAllowsStale verifies the pane-alive gate is load-bearing:
// when the pane is idle (agent exited), the heartbeat must NOT fire, so the gauge
// is allowed to go stale and the respawn path can take over.
func TestHeartbeat_IdlePaneAllowsStale(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	agent := "test-agent"

	writeStaleCtx(t, projectDir, agent, keeper.CtxFile{
		Pct:       50.0,
		SessionID: "11111111-2222-4333-8444-555555555555",
		Ts:        time.Now().UTC().Format(time.RFC3339),
	}, 200*time.Millisecond)

	em := &keeper.RecordingEmitter{}
	cfg := keeper.WatcherConfig{
		AgentName:          agent,
		ProjectDir:         projectDir,
		PollInterval:       10 * time.Millisecond,
		WarnPct:            80.0,
		IdleQuiesce:        1 * time.Millisecond,
		Staleness:          120 * time.Millisecond,
		HeartbeatEnabled:   true,
		HeartbeatThreshold: 60 * time.Millisecond,
		TmuxTarget:         "fake:0.0",
		// Pane is IDLE (agent exited) → heartbeat must NOT fire.
		IsPaneIdleFn: func(context.Context, string) bool { return true },
	}

	runWatcherFor(context.Background(), cfg, em, 200*time.Millisecond)

	if got := noGaugeStaleCount(em); got == 0 {
		t.Fatalf("expected no_gauge:stale on an idle pane (heartbeat must not mask a dead agent), got 0")
	}
}

// TestDeriveContextTokens checks the transcript token derivation: the LAST
// usage-bearing assistant turn wins, summing input + cache_read + cache_creation + output.
func TestDeriveContextTokens(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	sid := "abcdabcd-1234-4567-89ab-abcdabcdabcd"
	lines := []string{
		`{"type":"assistant","message":{"usage":{"input_tokens":10,"cache_read_input_tokens":1000,"cache_creation_input_tokens":500,"output_tokens":50}}}`,
		`{"type":"user","message":{"content":"hi"}}`,
		`{"type":"assistant","message":{"usage":{"input_tokens":20,"cache_read_input_tokens":3000,"cache_creation_input_tokens":1000,"output_tokens":80}}}`,
		`not-json`,
	}
	path := filepath.Join(dir, sid+".jsonl")
	if err := os.WriteFile(path, []byte(joinLines(lines)), 0o600); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	got, ok := keeper.DeriveContextTokensForTest(t.Context(), dir, sid)
	if !ok {
		t.Fatalf("expected derivation to succeed")
	}
	if want := int64(20 + 3000 + 1000 + 80); got != want {
		t.Fatalf("derived tokens = %d, want %d (last usage turn: input+cache_read+cache_creation+output)", got, want)
	}

	if _, ok := keeper.DeriveContextTokensForTest(t.Context(), dir, "no-such-session"); ok {
		t.Fatalf("expected derivation to fail for a missing transcript")
	}
}

func joinLines(ls []string) string {
	out := ""
	for _, l := range ls {
		out += l + "\n"
	}
	return out
}

// writeTranscriptTokens writes a minimal transcript JSONL with a single
// usage-bearing assistant turn reporting the given token count.
func writeTranscriptTokens(t *testing.T, dir, sid string, tokens int64) {
	t.Helper()
	line := fmt.Sprintf(
		`{"type":"assistant","message":{"usage":{"input_tokens":%d,"output_tokens":0}}}`+"\n",
		tokens,
	)
	path := filepath.Join(dir, sid+".jsonl")
	if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
}

// TestDeriveContextTokens_TailWindow_LargeFile verifies that deriveContextTokens
// correctly returns the LAST usage-bearing line even when the file is larger than
// the 512 KB tail window. A large padding block pushes old usage outside the window;
// only the fresh usage near EOF must be returned. Refs: hk-div6c.
func TestDeriveContextTokens_TailWindow_LargeFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	sid := "bbbbbbbb-1234-4567-89ab-bbbbbbbbbbbb"

	// Build a file > 512 KB with old usage at the top and fresh usage at the tail.
	var sb strings.Builder
	// Old usage (should be outside the tail window after padding).
	sb.WriteString(`{"type":"assistant","message":{"usage":{"input_tokens":1,"output_tokens":1}}}` + "\n")
	// Padding: ~600 KB of non-usage lines to exceed the 512 KB tail window.
	pad := `{"type":"user","message":{"content":"` + strings.Repeat("x", 200) + `"}}` + "\n"
	for i := 0; i < 3000; i++ {
		sb.WriteString(pad) // 3000 × ~215 B ≈ 645 KB
	}
	// Fresh usage near EOF — must be returned by the tail scan.
	sb.WriteString(`{"type":"assistant","message":{"usage":{"input_tokens":77777,"output_tokens":222}}}` + "\n")

	path := filepath.Join(dir, sid+".jsonl")
	if err := os.WriteFile(path, []byte(sb.String()), 0o600); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	got, ok := keeper.DeriveContextTokensForTest(t.Context(), dir, sid)
	if !ok {
		t.Fatalf("expected derivation to succeed on large file")
	}
	const want = int64(77777 + 222)
	if got != want {
		t.Fatalf("derived tokens = %d, want %d (last usage in tail must win)", got, want)
	}
}

// TestHeartbeat_Cache_SkipsRederiveWithinTTL verifies that the heartbeat uses a
// cached token count within DeriveCacheTTL rather than re-scanning the transcript.
// Observable: after the first successful derive, the transcript is replaced with
// a different token count; within the cache window the gauge still reflects the
// original (cached) value. Refs: hk-div6c.
func TestHeartbeat_Cache_SkipsRederiveWithinTTL(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	transcriptDir := t.TempDir()
	agent := "cache-test-agent"
	managedSID := "cccccccc-1111-4222-8333-444444444444"

	keeperDir := filepath.Join(projectDir, ".harmonik", "keeper")
	if err := os.MkdirAll(keeperDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := keeper.WriteManagedSessionID(projectDir, agent, managedSID); err != nil {
		t.Fatalf("WriteManagedSessionID: %v", err)
	}

	// First transcript: tokens = 12345.
	writeTranscriptTokens(t, transcriptDir, managedSID, 12345)

	// Seed a gauge aged past HeartbeatThreshold so the heartbeat fires immediately.
	writeStaleCtx(t, projectDir, agent, keeper.CtxFile{
		Pct:       50.0,
		Tokens:    0,
		SessionID: managedSID,
		Ts:        time.Now().UTC().Format(time.RFC3339),
	}, 20*time.Millisecond)

	em := &keeper.RecordingEmitter{}
	cfg := keeper.WatcherConfig{
		AgentName:          agent,
		ProjectDir:         projectDir,
		TranscriptDir:      transcriptDir,
		PollInterval:       5 * time.Millisecond,
		Staleness:          300 * time.Millisecond,
		HeartbeatThreshold: 15 * time.Millisecond,
		HeartbeatEnabled:   true,
		TmuxTarget:         "fake:0.0",
		IsPaneIdleFn:       func(context.Context, string) bool { return false },
		// Long enough cache to outlive the test; allows re-aging without expiry.
		DeriveCacheTTL: 10 * time.Second,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		w := keeper.NewWatcher(cfg, em)
		done <- w.Run(ctx)
	}()

	// Wait for the first heartbeat to fire and cache tokens=12345.
	time.Sleep(60 * time.Millisecond)

	cf1, _ := readCtxFor(t, projectDir, agent)
	if cf1.Tokens != 12345 {
		t.Fatalf("first heartbeat: tokens = %d, want 12345", cf1.Tokens)
	}

	// Replace transcript with a DIFFERENT token count (99999).
	// Within the cache TTL the watcher must NOT re-scan and must carry 12345.
	writeTranscriptTokens(t, transcriptDir, managedSID, 99999)

	// Re-age the gauge so a second heartbeat fires (while cache is still valid).
	writeStaleCtx(t, projectDir, agent, cf1, 20*time.Millisecond)

	time.Sleep(60 * time.Millisecond)

	cf2, _ := readCtxFor(t, projectDir, agent)
	// Cache hit → 12345 is reused even though the transcript now has 99999.
	// Carry-forward alone would also yield 12345 (from cf1.Tokens), but the
	// cache is the only path that avoids re-scanning the transcript file.
	if cf2.Tokens != 12345 {
		t.Fatalf("second heartbeat (cache): tokens = %d, want 12345 (cache must suppress re-scan)", cf2.Tokens)
	}

	cancel()
	<-done
}

// TestHeartbeat_DeriveMissBudget_SuppressesCarryForward verifies hk-lal8: when
// deriveContextTokens persistently returns false (transcript absent), the heartbeat
// stops writing the gauge file after HeartbeatMaxMisses consecutive misses. Once
// writes stop, the gauge ages to genuine staleness and the watcher emits
// no_gauge:stale — restoring the safety signal that the carry-forward write was
// silently suppressing.
//
// The test uses HeartbeatMaxMisses=2 so the budget is exceeded in a few ticks.
// The contrast assertion (MaxMisses=100) confirms the existing behaviour on a
// live pane is unchanged while the budget is large.
//
// BOTH ARMS RUN ON VIRTUAL TIME (hk-vp02y). The FakeClock starts at the seeded
// gauge's real mod-time plus the seeded age, so the boot-time age is exactly
// the seeded age and each tick adds exactly one PollInterval. No assertion here
// reads a rate: the one real-time term subtracts from the age, so a slow box
// can only delay a crossing into the spare ticks each arm leaves for it.
func TestHeartbeat_DeriveMissBudget_SuppressesCarryForward(t *testing.T) {
	t.Parallel()

	// ── sub-test: budget exceeded → gauge goes stale → no_gauge:stale fires ──
	t.Run("budget_exceeded_emits_stale", func(t *testing.T) {
		t.Parallel()

		// The whole sequence is a fixed tick number:
		//
		//	boot     age 3s  — below Staleness, so the boot-time check is silent
		//	tick 1   age 4s  — heartbeat due, derive miss 1, gauge written
		//	tick 3   age 3s  — heartbeat due, derive miss 2, gauge written
		//	tick 4   age 4s  — heartbeat due, miss 3 busts the budget of 2,
		//	                   so no write and the mod-time now stops moving
		//	tick 5   age 5s  — the frozen gauge reaches Staleness → no_gauge:stale
		//
		// The real clock enters only as a term that SUBTRACTS from the age (a
		// written file lands at wall-clock time while virtual time runs ahead),
		// so a slow box can only DELAY the tick-5 crossing this test wants.
		// Eleven spare ticks is eleven virtual seconds of room for that.
		const (
			seedAge            = 3 * time.Second
			heartbeatThreshold = 2500 * time.Millisecond
			staleness          = 5 * time.Second
			pollInterval       = 1 * time.Second
			// 16 ticks is three times what the sequence above needs.
			ticks = 16
		)

		projectDir := t.TempDir()
		agent := "test-agent"

		// Seed an initial gauge aged past HeartbeatThreshold so the heartbeat
		// fires on the very first tick.
		writeStaleCtx(t, projectDir, agent, keeper.CtxFile{
			Pct:       50.0,
			Tokens:    180_000,
			SessionID: "11111111-2222-4333-8444-555555555555",
			Ts:        time.Now().UTC().Format(time.RFC3339),
		}, seedAge)
		_, seededModTime := readCtxFor(t, projectDir, agent)

		em := &keeper.RecordingEmitter{}
		cfg := keeper.WatcherConfig{
			AgentName:  agent,
			ProjectDir: projectDir,

			PollInterval:       pollInterval,
			Staleness:          staleness,
			HeartbeatThreshold: heartbeatThreshold,
			HeartbeatEnabled:   true,
			// Small budget: after 2 consecutive derive-misses the heartbeat stops
			// writing, allowing the gauge to age past Staleness.
			HeartbeatMaxMisses: 2,

			TmuxTarget:   "fake:0.0",
			WarnPct:      80.0,
			IdleQuiesce:  1 * time.Millisecond,
			IsPaneIdleFn: func(context.Context, string) bool { return false }, // pane alive

			// No transcript on disk → derive always returns false.
			TranscriptDir: filepath.Join(projectDir, "no-such-transcript-dir"),
		}

		driveWatcherFakeClockFrom(t, seededModTime.Add(seedAge), cfg, em, ticks)

		if got := noGaugeStaleCount(em); got == 0 {
			t.Fatalf("expected ≥1 no_gauge:stale after derive-miss budget exceeded, got 0 (heartbeat is still papering over stale count)")
		}
	})

	// ── sub-test: budget NOT exceeded → gauge stays fresh (existing behaviour) ──
	t.Run("within_budget_keeps_gauge_fresh", func(t *testing.T) {
		t.Parallel()

		// Same arithmetic as TestHeartbeat_KeepsLiveGaugeFresh: the boot check
		// is silent at 3s, the heartbeat writes from tick 1, and from tick 5 the
		// carry-forward write is the only thing keeping the gauge out of the
		// stale branch. With that write removed the gauge goes stale on tick 2.
		const (
			seedAge            = 3 * time.Second
			heartbeatThreshold = 2500 * time.Millisecond
			staleness          = 5 * time.Second
			pollInterval       = 1 * time.Second
			ticks              = 12
		)

		projectDir := t.TempDir()
		agent := "test-agent"
		managedSID := "22222222-3333-4444-8555-666666666666"
		keeperDir := filepath.Join(projectDir, ".harmonik", "keeper")
		if err := os.MkdirAll(keeperDir, 0o700); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if err := keeper.WriteManagedSessionID(projectDir, agent, managedSID); err != nil {
			t.Fatalf("WriteManagedSessionID: %v", err)
		}

		writeStaleCtx(t, projectDir, agent, keeper.CtxFile{
			Pct:       50.0,
			Tokens:    180_000,
			SessionID: managedSID,
			Ts:        time.Now().UTC().Format(time.RFC3339),
		}, seedAge)
		_, seededModTime := readCtxFor(t, projectDir, agent)

		em := &keeper.RecordingEmitter{}
		cfg := keeper.WatcherConfig{
			AgentName:  agent,
			ProjectDir: projectDir,

			PollInterval:       pollInterval,
			Staleness:          staleness,
			HeartbeatThreshold: heartbeatThreshold,
			HeartbeatEnabled:   true,
			// Large budget: carry-forward continues for a long time — gauge stays fresh.
			HeartbeatMaxMisses: 100,

			TmuxTarget:   "fake:0.0",
			WarnPct:      80.0,
			IdleQuiesce:  1 * time.Millisecond,
			IsPaneIdleFn: func(context.Context, string) bool { return false },

			TranscriptDir: filepath.Join(projectDir, "no-such-transcript-dir"),
		}

		driveWatcherFakeClockFrom(t, seededModTime.Add(seedAge), cfg, em, ticks)

		if got := noGaugeStaleCount(em); got != 0 {
			t.Fatalf("expected 0 no_gauge:stale while within miss budget on a live pane, got %d", got)
		}
		// An OCCURRENCE assertion, not a rate. The old form measured the gauge
		// age with the real clock from outside the watcher after the run had
		// ended, which reads the harness's scheduling delay, not the product.
		_, modTime := readCtxFor(t, projectDir, agent)
		if !modTime.After(seededModTime) {
			t.Fatalf("gauge was not refreshed: mod-time %v is still the seeded %v", modTime, seededModTime)
		}
	})
}

// TestHeartbeat_SIDChange_ResetsMissBudget verifies hk-4xni9 K1: when the
// heartbeat has exhausted its derive-miss budget against a prior session's
// transcript, writing a new session_id to the .sid channel must reset the miss
// count and resume gauge writes — preventing the 23h gauge-death seen on leto
// after a ClearSettle-timeout cycle cleared .managed.
//
// Scenario:
//  1. Heartbeat runs with HeartbeatMaxMisses=2 and no transcript → budget exhausted → gauge goes stale.
//  2. A new UUIDv4 is written to the .sid channel (simulating the new session after /clear).
//  3. The gauge must recover: the heartbeat detects the SID change, resets its miss
//     count, and resumes carry-forward writes so the gauge stays below Staleness.
func TestHeartbeat_SIDChange_ResetsMissBudget(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	agent := "test-agent"
	newSID := "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"

	keeperDir := filepath.Join(projectDir, ".harmonik", "keeper")
	if err := os.MkdirAll(keeperDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	// .managed is empty (cleared by ClearSettle-timeout), mirroring the K1 scenario.

	// Seed an aged gauge with the old SID so the heartbeat fires immediately.
	writeStaleCtx(t, projectDir, agent, keeper.CtxFile{
		Pct:       50.0,
		Tokens:    180_000,
		SessionID: "11111111-2222-4333-8444-555555555555",
		Ts:        time.Now().UTC().Format(time.RFC3339),
	}, 60*time.Millisecond)

	em := &keeper.RecordingEmitter{}
	cfg := keeper.WatcherConfig{
		AgentName:  agent,
		ProjectDir: projectDir,

		PollInterval:       5 * time.Millisecond,
		Staleness:          80 * time.Millisecond,
		HeartbeatThreshold: 30 * time.Millisecond,
		HeartbeatEnabled:   true,
		// Small budget so it exhausts quickly without long wall-clock delays.
		HeartbeatMaxMisses: 2,

		TmuxTarget:   "fake:0.0",
		WarnPct:      80.0,
		IdleQuiesce:  1 * time.Millisecond,
		IsPaneIdleFn: func(context.Context, string) bool { return false }, // pane alive

		// No transcript → derive always fails for both SIDs.
		TranscriptDir: filepath.Join(projectDir, "no-such-transcript-dir"),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		runWatcherFor(ctx, cfg, em, 2*time.Second)
	}()

	// Phase 1: let the budget exhaust — gauge goes stale.
	time.Sleep(300 * time.Millisecond)
	if noGaugeStaleCount(em) == 0 {
		cancel()
		<-done
		t.Fatalf("phase 1: expected ≥1 no_gauge:stale after budget exceeded, got 0")
	}

	// Phase 2: write new SID to .sid channel — heartbeat must detect the SID
	// change, reset its miss count, and resume carry-forward writes.
	sidWriteTime := time.Now()
	sidPath := filepath.Join(keeperDir, agent+".sid")
	if err := os.WriteFile(sidPath, []byte(newSID+"\n"), 0o600); err != nil {
		cancel()
		<-done
		t.Fatalf("write .sid: %v", err)
	}
	// Re-age the gauge so the heartbeat threshold fires immediately on the next tick.
	writeStaleCtx(t, projectDir, agent, keeper.CtxFile{
		Pct:       50.0,
		Tokens:    180_000,
		SessionID: newSID,
		Ts:        time.Now().UTC().Format(time.RFC3339),
	}, 60*time.Millisecond)

	// Allow time for one heartbeat tick (PollInterval=5ms, HeartbeatThreshold=30ms).
	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done

	// After the SID change the heartbeat must have written at least one carry-forward:
	// the gauge mod-time must be AFTER the moment the new SID was written.
	_, modTime := readCtxFor(t, projectDir, agent)
	if !modTime.After(sidWriteTime) {
		t.Fatalf("phase 2: gauge not refreshed after SID change: mod-time %v is not after sidWriteTime %v — heartbeat miss-count was not reset on SID change", modTime, sidWriteTime)
	}
}

// TestHeartbeat_CrossingPassDoesNotCallItsOwnFreshGaugeStale pins hk-oduuc: on
// the pass where the gauge age first reaches Staleness, the watcher refreshes a
// live agent's gauge and must NOT then declare that same, just-written gauge
// stale.
//
// THE DEFECT. Watcher.Run reads modTime once at the top of a pass, calls
// maybeHeartbeat (which may replace the gauge), and then re-tests staleness
// against that same pre-write modTime. Before the fix the crossing pass emitted
// a false no_gauge:stale for a healthy agent and `continue`d past session_id
// binding, the warn ladder, idle-quiesce and cycle triggering.
//
// WHY THIS TEST CANNOT BE FAILED BY A BUSY BOX, which is the whole point — the
// three gate failures that led here (hk-vp02y) were this defect being tripped by
// scheduler starvation, and a test that reproduces it by ALSO relying on timing
// would be the same defect in a new place. Every inequality below is one-sided
// in the safe direction:
//
//   - PollInterval (3s) is deliberately LONGER than Staleness (2s). The crossing
//     therefore happens by arithmetic, not by luck: at the first tick the gauge
//     age is at least seedAge+PollInterval = 3.4s, against a 2s window. A slower
//     box makes that age LARGER, so load can only make the crossing more certain.
//   - The boot-time check in Run emits no_gauge:stale before any heartbeat can
//     run, so the seeded gauge must start BELOW Staleness or the test measures
//     nothing. seedAge (400ms) sits 1.6s under the window — a margin the box
//     would have to stall through between two adjacent statements to erase.
//
// The idle-pane arm is what stops this test from being one-directional. Merely
// suppressing the alarm everywhere would pass the live arm and break the respawn
// path; the idle arm fails if the fix over-suppresses.
func TestHeartbeat_CrossingPassDoesNotCallItsOwnFreshGaugeStale(t *testing.T) {
	t.Parallel()

	const (
		seedAge            = 400 * time.Millisecond
		staleness          = 2 * time.Second
		pollInterval       = 3 * time.Second // > staleness, so the crossing is guaranteed
		heartbeatThreshold = 200 * time.Millisecond
		// runFor must cover the single tick at t=3s with enough margin that a
		// starved box still SERVICES that tick — if ctx.Done wins the select the
		// tick never runs and both arms fail. 5s leaves ~2s of service margin and
		// still cannot admit a second tick, which would arrive at t=6s.
		runFor = 5 * time.Second
	)

	cases := []struct {
		name string
		// paneIdle reports whether the agent process has exited.
		paneIdle bool
		// wantStale is the number of no_gauge:stale events the pass must emit.
		wantStale int
		// wantRefreshed is whether the heartbeat must have re-written the gauge.
		wantRefreshed bool
		why           string
	}{
		{
			name:          "live_pane_is_refreshed_not_called_stale",
			paneIdle:      false,
			wantStale:     0,
			wantRefreshed: true,
			why:           "the heartbeat wrote a fresh gauge on this very pass, so the gauge is not stale",
		},
		{
			name:          "idle_pane_still_goes_stale_so_respawn_can_fire",
			paneIdle:      true,
			wantStale:     1,
			wantRefreshed: false,
			why:           "the agent has exited, the heartbeat must not write, and the alarm is the respawn trigger",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			projectDir := t.TempDir()
			agent := "test-agent"
			managedSID := "11111111-2222-4333-8444-555555555555"
			if err := os.MkdirAll(filepath.Join(projectDir, ".harmonik", "keeper"), 0o700); err != nil {
				t.Fatalf("MkdirAll: %v", err)
			}
			if err := keeper.WriteManagedSessionID(projectDir, agent, managedSID); err != nil {
				t.Fatalf("WriteManagedSessionID: %v", err)
			}

			// Seed a gauge that is already past HeartbeatThreshold but still
			// comfortably inside the Staleness window, so the boot-time check is
			// silent and the FIRST TICK is the crossing pass.
			writeStaleCtx(t, projectDir, agent, keeper.CtxFile{
				Pct:       50.0,
				Tokens:    100_000,
				SessionID: managedSID,
				Ts:        time.Now().UTC().Format(time.RFC3339),
			}, seedAge)
			_, seededModTime := readCtxFor(t, projectDir, agent)

			em := &keeper.RecordingEmitter{}
			cfg := keeper.WatcherConfig{
				AgentName:          agent,
				ProjectDir:         projectDir,
				PollInterval:       pollInterval,
				WarnPct:            80.0,
				IdleQuiesce:        1 * time.Millisecond,
				Staleness:          staleness,
				HeartbeatEnabled:   true,
				HeartbeatThreshold: heartbeatThreshold,
				TmuxTarget:         "fake:0.0",
				IsPaneIdleFn:       func(context.Context, string) bool { return tc.paneIdle },
				// No transcript on disk → the heartbeat carries last-good tokens
				// forward rather than deriving. Keeps this test about the ordering.
				TranscriptDir: filepath.Join(projectDir, "no-such-transcript-dir"),
			}

			runWatcherFor(context.Background(), cfg, em, runFor)

			if got := noGaugeStaleCount(em); got != tc.wantStale {
				t.Errorf("no_gauge:stale count = %d, want %d — %s", got, tc.wantStale, tc.why)
			}

			_, modTime := readCtxFor(t, projectDir, agent)
			// An OCCURRENCE assertion, not a rate: did the heartbeat write at all?
			// Deliberately not "the gauge is younger than Staleness", which is the
			// wall-clock shape this whole sweep exists to remove.
			if refreshed := modTime.After(seededModTime); refreshed != tc.wantRefreshed {
				t.Errorf("gauge refreshed = %v, want %v (seeded mod-time %v, now %v)",
					refreshed, tc.wantRefreshed, seededModTime, modTime)
			}
		})
	}
}

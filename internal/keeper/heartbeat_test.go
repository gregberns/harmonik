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
	if err := os.MkdirAll(keeperDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	raw, err := json.Marshal(cf)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	path := filepath.Join(keeperDir, agent+".ctx")
	if err := os.WriteFile(path, append(raw, '\n'), 0o600); err != nil {
		t.Fatalf("write ctx: %v", err)
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
func TestHeartbeat_KeepsLiveGaugeFresh(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	agent := "test-agent"
	// Latch a real UUIDv4 so the heartbeat stamps it back into the gauge.
	managedSID := "11111111-2222-4333-8444-555555555555"
	keeperDir := filepath.Join(projectDir, ".harmonik", "keeper")
	if err := os.MkdirAll(keeperDir, 0o755); err != nil {
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
	}, 70*time.Millisecond)

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
		// Pane is alive (agent running) → heartbeat must keep the gauge fresh.
		IsPaneIdleFn: func(context.Context, string) bool { return false },
		// No transcript on disk → heartbeat carries last-good tokens forward.
		TranscriptDir: filepath.Join(projectDir, "no-such-transcript-dir"),
	}

	runWatcherFor(context.Background(), cfg, em, 300*time.Millisecond)

	if got := noGaugeStaleCount(em); got != 0 {
		t.Fatalf("expected 0 no_gauge:stale events on a live pane, got %d", got)
	}
	cf, modTime := readCtxFor(t, projectDir, agent)
	if time.Since(modTime) >= cfg.Staleness {
		t.Fatalf("gauge was not refreshed: mod-time age %v >= Staleness %v", time.Since(modTime), cfg.Staleness)
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

	got, ok := keeper.DeriveContextTokensForTest(dir, sid)
	if !ok {
		t.Fatalf("expected derivation to succeed")
	}
	if want := int64(20 + 3000 + 1000 + 80); got != want {
		t.Fatalf("derived tokens = %d, want %d (last usage turn: input+cache_read+cache_creation+output)", got, want)
	}

	if _, ok := keeper.DeriveContextTokensForTest(dir, "no-such-session"); ok {
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

	got, ok := keeper.DeriveContextTokensForTest(dir, sid)
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
	if err := os.MkdirAll(keeperDir, 0o755); err != nil {
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
// The test uses HeartbeatMaxMisses=2 so the budget is exceeded quickly without
// long wall-clock delays. The contrast assertion (MaxMisses=100) confirms the
// existing behaviour on a live pane is unchanged while the budget is large.
func TestHeartbeat_DeriveMissBudget_SuppressesCarryForward(t *testing.T) {
	t.Parallel()

	// ── sub-test: budget exceeded → gauge goes stale → no_gauge:stale fires ──
	t.Run("budget_exceeded_emits_stale", func(t *testing.T) {
		t.Parallel()

		projectDir := t.TempDir()
		agent := "test-agent"

		// Seed an initial gauge aged past HeartbeatThreshold so the heartbeat
		// fires on the very first tick.
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
			HeartbeatThreshold: 40 * time.Millisecond,
			HeartbeatEnabled:   true,
			// Small budget: after 2 consecutive derive-misses the heartbeat stops
			// writing, allowing the gauge to age past Staleness (80ms).
			HeartbeatMaxMisses: 2,

			TmuxTarget:   "fake:0.0",
			WarnPct:      80.0,
			IdleQuiesce:  1 * time.Millisecond,
			IsPaneIdleFn: func(context.Context, string) bool { return false }, // pane alive

			// No transcript on disk → derive always returns false.
			TranscriptDir: filepath.Join(projectDir, "no-such-transcript-dir"),
		}

		// Run long enough for: 2 heartbeat writes (within budget) + budget exceeded
		// + gauge ages past Staleness (80ms) → no_gauge:stale fires.
		runWatcherFor(context.Background(), cfg, em, 600*time.Millisecond)

		if got := noGaugeStaleCount(em); got == 0 {
			t.Fatalf("expected ≥1 no_gauge:stale after derive-miss budget exceeded, got 0 (heartbeat is still papering over stale count)")
		}
	})

	// ── sub-test: budget NOT exceeded → gauge stays fresh (existing behaviour) ──
	t.Run("within_budget_keeps_gauge_fresh", func(t *testing.T) {
		t.Parallel()

		projectDir := t.TempDir()
		agent := "test-agent"
		managedSID := "22222222-3333-4444-8555-666666666666"
		keeperDir := filepath.Join(projectDir, ".harmonik", "keeper")
		if err := os.MkdirAll(keeperDir, 0o755); err != nil {
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
		}, 60*time.Millisecond)

		em := &keeper.RecordingEmitter{}
		cfg := keeper.WatcherConfig{
			AgentName:  agent,
			ProjectDir: projectDir,

			PollInterval:       5 * time.Millisecond,
			Staleness:          120 * time.Millisecond,
			HeartbeatThreshold: 40 * time.Millisecond,
			HeartbeatEnabled:   true,
			// Large budget: carry-forward continues for a long time — gauge stays fresh.
			HeartbeatMaxMisses: 100,

			TmuxTarget:   "fake:0.0",
			WarnPct:      80.0,
			IdleQuiesce:  1 * time.Millisecond,
			IsPaneIdleFn: func(context.Context, string) bool { return false },

			TranscriptDir: filepath.Join(projectDir, "no-such-transcript-dir"),
		}

		runWatcherFor(context.Background(), cfg, em, 200*time.Millisecond)

		if got := noGaugeStaleCount(em); got != 0 {
			t.Fatalf("expected 0 no_gauge:stale while within miss budget on a live pane, got %d", got)
		}
		_, modTime := readCtxFor(t, projectDir, agent)
		if time.Since(modTime) >= cfg.Staleness {
			t.Fatalf("gauge was not refreshed: mod-time age %v >= Staleness %v", time.Since(modTime), cfg.Staleness)
		}
	})
}

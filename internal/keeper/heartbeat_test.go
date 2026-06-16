package keeper_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
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
// usage-bearing assistant turn wins, summing input + cache_read + cache_creation.
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
	if want := int64(20 + 3000 + 1000); got != want {
		t.Fatalf("derived tokens = %d, want %d (last usage turn)", got, want)
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

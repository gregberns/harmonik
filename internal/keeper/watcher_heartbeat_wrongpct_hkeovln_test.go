package keeper_test

// watcher_heartbeat_wrongpct_hkeovln_test.go — regression test for hk-eovln:
// maybeHeartbeat substituted FallbackWindowSize (200k) when window_size=0,
// producing a wrong-high pct (e.g. 210k/200k=105%) for large-context sessions.
// This overwrote the statusline's authoritative pct and caused belowWarnThreshold
// to fire session_keeper_warn far below the configured warn_pct. Refs: hk-eovln.

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

// TestWatcher_Heartbeat_NoWarnWhenWindowSizeZero is the regression test for hk-eovln.
//
// Scenario: a 1M-context admiral session where the statusline has NOT yet detected
// the window size (window_size=0 in gauge). The gauge carries pct=21 (well below
// warn_pct=80). The heartbeat derives tokens=201700 from the transcript.
//
// BUGGY path (before fix): heartbeat substitutes FallbackWindowSize=200k, computes
// pct=201700/200000*100=100.85, writes it to gauge. Next tick reads pct=100.85
// with window_size=0, belowWarnThreshold returns 100.85 < 80 = false → warn fires
// despite actual context being at ~21%.
//
// FIXED path: heartbeat skips pct recompute when window_size=0, carries pct=21
// forward. belowWarnThreshold returns 21 < 80 = true → no warn.
func TestWatcher_Heartbeat_NoWarnWhenWindowSizeZero(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	transcriptDir := t.TempDir()
	agent := "hkeovln-agent"
	sessionID := "sess-eovln-1m"

	// Gauge: pct=21, tokens=201700, window_size=0 (statusline didn't detect 1M window).
	writeCtxFileTokens(t, projectDir, agent, 21.0, 201_700, 0, sessionID)

	// Transcript: write a fake Claude Code JSONL with a usage entry summing to 201700.
	writeTranscriptForHeartbeat(t, transcriptDir, sessionID, 201_700)

	cfg := keeper.WatcherConfig{
		AgentName:     agent,
		ProjectDir:    projectDir,
		TranscriptDir: transcriptDir,
		PollInterval:  5 * time.Millisecond,
		WarnPct:       80.0,
		IdleQuiesce:   1 * time.Millisecond,
		Staleness:     120 * time.Second, // generous — gauge stays fresh
		TmuxTarget:    "hkeovln-fake-pane",
		// Heartbeat fires immediately (threshold=1ms, age will always exceed it).
		HeartbeatEnabled:   true,
		HeartbeatThreshold: 1 * time.Millisecond,
		HeartbeatMaxMisses: 10,
		DeriveCacheTTL:     1 * time.Millisecond, // short TTL so re-derives happen
		// Pane is NOT idle — heartbeat should fire on every tick.
		IsPaneIdleFn: func(_ context.Context, _ string) bool { return false },
		WarnCooldown: 0,
	}

	em := &keeper.RecordingEmitter{}
	runWatcherFor(context.Background(), cfg, em, 120*time.Millisecond)

	warns := em.EventsOfType(core.EventTypeSessionKeeperWarn)
	if len(warns) != 0 {
		t.Errorf("hk-eovln: want 0 session_keeper_warn at pct=21 < warn_pct=80 (window_size=0, tokens=201700); got %d — heartbeat overwrote pct with FallbackWindowSize-derived false-high value", len(warns))
		for i, w := range warns {
			t.Logf("  warn[%d] payload: %s", i, w.Payload)
		}
	}
}

// writeTranscriptForHeartbeat writes a minimal Claude Code transcript JSONL that
// deriveContextTokens will parse, returning the given total token count.
func writeTranscriptForHeartbeat(t *testing.T, transcriptDir, sessionID string, totalTokens int64) {
	t.Helper()
	if err := os.MkdirAll(transcriptDir, 0o755); err != nil {
		t.Fatalf("MkdirAll transcriptDir: %v", err)
	}
	// The JSONL format matches the inner structure deriveContextTokens expects:
	// {"message":{"usage":{"input_tokens":N,...}}}
	type usage struct {
		InputTokens int64 `json:"input_tokens"`
	}
	type message struct {
		Usage usage `json:"usage"`
	}
	type line struct {
		Message message `json:"message"`
	}
	row := line{Message: message{Usage: usage{InputTokens: totalTokens}}}
	data, err := json.Marshal(row)
	if err != nil {
		t.Fatalf("json.Marshal transcript line: %v", err)
	}
	path := filepath.Join(transcriptDir, sessionID+".jsonl")
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatalf("WriteFile transcript: %v", err)
	}
}

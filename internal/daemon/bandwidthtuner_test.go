package daemon

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
)

// writeTestJSONL writes a JSONL file with usage records at specified token counts
// and timestamps relative to now.
func writeTestJSONL(t *testing.T, dir string, records []struct {
	age    time.Duration // how far in the past; 0 = now
	input  int64
	output int64
	create int64
}) string {
	t.Helper()
	path := filepath.Join(dir, "transcript.jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create transcript: %v", err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	for _, r := range records {
		ts := time.Now().Add(-r.age).UTC().Format(time.RFC3339Nano)
		line := map[string]interface{}{
			"type":      "assistant",
			"timestamp": ts,
			"message": map[string]interface{}{
				"usage": map[string]interface{}{
					"input_tokens":               r.input,
					"output_tokens":              r.output,
					"cache_creation_input_tokens": r.create,
					"cache_read_input_tokens":    9999, // should be excluded
				},
			},
		}
		if err := enc.Encode(line); err != nil {
			t.Fatalf("encode: %v", err)
		}
	}
	return path
}

func TestTranscriptTokensUsed_SumsWindow(t *testing.T) {
	home := t.TempDir()
	projDir := filepath.Join(home, ".claude", "projects", "proj1")
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatal(err)
	}

	writeTestJSONL(t, projDir, []struct {
		age    time.Duration
		input  int64
		output int64
		create int64
	}{
		{age: 1 * time.Hour, input: 100, output: 50, create: 200},  // in window
		{age: 4 * time.Hour, input: 300, output: 100, create: 400}, // in window
		{age: 6 * time.Hour, input: 999, output: 999, create: 999}, // outside window
	})

	since := time.Now().Add(-5 * time.Hour)
	got, err := transcriptTokensUsed(home, since)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// want = (100+50+200) + (300+100+400) = 350 + 800 = 1150 (cache_read excluded)
	want := int64(1150)
	if got != want {
		t.Errorf("transcriptTokensUsed = %d, want %d", got, want)
	}
}

func TestTranscriptTokensUsed_MissingDir(t *testing.T) {
	home := t.TempDir()
	// ~/.claude/projects does not exist
	got, err := transcriptTokensUsed(home, time.Now().Add(-5*time.Hour))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 0 {
		t.Errorf("expected 0 for missing dir, got %d", got)
	}
}

func TestBandwidthTuner_tick_FullHeadroom(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".claude", "projects"), 0o755); err != nil {
		t.Fatal(err)
	}

	ctrl := NewConcurrencyController(4)
	tuner := NewBandwidthTuner(ctrl, 4, 1_000_000, home)
	// No usage → full headroom → effectiveMax should be 4.
	tuner.tick()
	if got := ctrl.Get(); got != 4 {
		t.Errorf("expected ceiling=4 at full headroom, got %d", got)
	}
}

func TestBandwidthTuner_tick_HalfUsed(t *testing.T) {
	home := t.TempDir()
	projDir := filepath.Join(home, ".claude", "projects", "p")
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Use exactly half the ceiling.
	writeTestJSONL(t, projDir, []struct {
		age    time.Duration
		input  int64
		output int64
		create int64
	}{
		{age: 1 * time.Hour, input: 500_000, output: 0, create: 0},
	})

	ctrl := NewConcurrencyController(4)
	tuner := NewBandwidthTuner(ctrl, 4, 1_000_000, home)
	tuner.tick()
	// headroom = 500k/1M → ratio=0.5 → round(4*0.5) = 2
	if got := ctrl.Get(); got != 2 {
		t.Errorf("expected ceiling=2 at half headroom, got %d", got)
	}
}

func TestBandwidthTuner_tick_CeilingExhausted(t *testing.T) {
	home := t.TempDir()
	projDir := filepath.Join(home, ".claude", "projects", "p")
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Consume more than the ceiling.
	writeTestJSONL(t, projDir, []struct {
		age    time.Duration
		input  int64
		output int64
		create int64
	}{
		{age: 1 * time.Hour, input: 2_000_000, output: 0, create: 0},
	})

	ctrl := NewConcurrencyController(4)
	tuner := NewBandwidthTuner(ctrl, 4, 1_000_000, home)
	tuner.tick()
	// headroom ≤ 0 → clamp to 1
	if got := ctrl.Get(); got != 1 {
		t.Errorf("expected ceiling=1 when exhausted, got %d", got)
	}
}

// TestBandwidthTunerBackstop_NilTuner verifies that the backstop handler is a
// no-op when no tuner has been wired (SetTuner not called).
func TestBandwidthTunerBackstop_NilTuner(t *testing.T) {
	t.Parallel()
	b := &bandwidthTunerBackstop{}
	// Should not panic; returns nil.
	evt := core.Event{Payload: json.RawMessage(`{"retry_after_seconds": 60}`)}
	if err := b.handle(context.Background(), evt); err != nil {
		t.Errorf("handle with nil tuner: unexpected error %v", err)
	}
}

// TestBandwidthTunerBackstop_ForwardsNotify verifies that the backstop calls
// tuner.NotifyRateLimit when a tuner is wired and an agent_rate_limited event
// carries a retry_after_seconds field.
func TestBandwidthTunerBackstop_ForwardsNotify(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".claude", "projects"), 0o755); err != nil {
		t.Fatal(err)
	}

	ctrl := NewConcurrencyController(4)
	tuner := NewBandwidthTuner(ctrl, 4, 1_000_000, home)

	b := &bandwidthTunerBackstop{}
	b.SetTuner(tuner)

	payload, _ := json.Marshal(map[string]int{"retry_after_seconds": 120})
	evt := core.Event{Payload: json.RawMessage(payload)}
	if err := b.handle(context.Background(), evt); err != nil {
		t.Fatalf("handle: %v", err)
	}
	// NotifyRateLimit should have snapped concurrency to 1.
	if got := ctrl.Get(); got != 1 {
		t.Errorf("concurrency after backstop notify = %d, want 1", got)
	}
	// tick should not raise the ceiling during backoff.
	tuner.tick()
	if got := ctrl.Get(); got != 1 {
		t.Errorf("concurrency still expected 1 during backoff, got %d", got)
	}
}

// TestBandwidthTunerBackstop_ZeroRetryAfter verifies that a missing or zero
// retry_after_seconds in the event payload still calls NotifyRateLimit
// (which applies a conservative 5-minute default).
func TestBandwidthTunerBackstop_ZeroRetryAfter(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".claude", "projects"), 0o755); err != nil {
		t.Fatal(err)
	}

	ctrl := NewConcurrencyController(4)
	tuner := NewBandwidthTuner(ctrl, 4, 1_000_000, home)

	b := &bandwidthTunerBackstop{}
	b.SetTuner(tuner)

	// No retry_after_seconds field.
	evt := core.Event{Payload: json.RawMessage(`{}`)}
	if err := b.handle(context.Background(), evt); err != nil {
		t.Fatalf("handle: %v", err)
	}
	// NotifyRateLimit uses conservative default → should still snap to 1.
	if got := ctrl.Get(); got != 1 {
		t.Errorf("concurrency after backstop notify (no retry hint) = %d, want 1", got)
	}
}

func TestBandwidthTuner_NotifyRateLimit_SnapsToOne(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".claude", "projects"), 0o755); err != nil {
		t.Fatal(err)
	}

	ctrl := NewConcurrencyController(4)
	tuner := NewBandwidthTuner(ctrl, 4, 1_000_000, home)

	tuner.NotifyRateLimit(2 * time.Minute)
	if got := ctrl.Get(); got != 1 {
		t.Errorf("expected ceiling=1 after rate limit, got %d", got)
	}

	// tick should NOT raise ceiling during backoff
	tuner.tick()
	if got := ctrl.Get(); got != 1 {
		t.Errorf("expected ceiling still 1 during backoff, got %d", got)
	}
}

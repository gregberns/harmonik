package daemon

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
)

// writeTestJSONL writes a JSONL file with usage records at specified token counts
// and timestamps relative to now.
func writeTestJSONL(t *testing.T, dir string, records []struct {
	age    time.Duration // how far in the past; 0 = now
	input  int64
	output int64
	create int64
},
) string {
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
					"input_tokens":                r.input,
					"output_tokens":               r.output,
					"cache_creation_input_tokens": r.create,
					"cache_read_input_tokens":     9999, // should be excluded
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

// backstopActivePayload builds a serialised AgentRateLimitStatusPayload with
// status=active for use in backstop unit tests.
func backstopActivePayload(t *testing.T, retryAfterSec *int) json.RawMessage {
	t.Helper()
	runID := core.RunID(uuid.MustParse("01960084-0000-7000-8000-000000000001"))
	pl := core.AgentRateLimitStatusPayload{
		RunID:             runID,
		SessionID:         "test-session",
		Status:            core.AgentRateLimitStatusActive,
		RetryAfterSeconds: retryAfterSec,
		ChangedAt:         time.Now().UTC().Format("2006-01-02T15:04:05.000Z07:00"),
	}
	b, err := json.Marshal(pl)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return b
}

// TestBandwidthTunerBackstop_NilTuner verifies that the backstop handler is a
// no-op when no tuner has been wired (SetTuner not called).
func TestBandwidthTunerBackstop_NilTuner(t *testing.T) {
	t.Parallel()
	b := &bandwidthTunerBackstop{}
	// Should not panic; returns nil even with a well-formed status=active payload.
	retry := 60
	evt := core.Event{Payload: backstopActivePayload(t, &retry)}
	if err := b.handle(context.Background(), evt); err != nil {
		t.Errorf("handle with nil tuner: unexpected error %v", err)
	}
}

// TestBandwidthTunerBackstop_ForwardsNotify verifies that the backstop calls
// tuner.NotifyRateLimit when a tuner is wired and an agent_rate_limit_status
// active event carries a retry_after_seconds field.
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

	retry := 120
	evt := core.Event{Payload: backstopActivePayload(t, &retry)}
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

// TestBandwidthTunerBackstop_ClearedIgnored verifies that a status=cleared event
// does NOT call NotifyRateLimit (only the active transition triggers the backstop).
func TestBandwidthTunerBackstop_ClearedIgnored(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".claude", "projects"), 0o755); err != nil {
		t.Fatal(err)
	}

	ctrl := NewConcurrencyController(4)
	tuner := NewBandwidthTuner(ctrl, 4, 1_000_000, home)

	b := &bandwidthTunerBackstop{}
	b.SetTuner(tuner)

	runID := core.RunID(uuid.MustParse("01960084-0000-7000-8000-000000000002"))
	pl := core.AgentRateLimitStatusPayload{
		RunID:     runID,
		SessionID: "test-session",
		Status:    core.AgentRateLimitStatusCleared,
		ChangedAt: time.Now().UTC().Format("2006-01-02T15:04:05.000Z07:00"),
	}
	plBytes, _ := json.Marshal(pl)
	evt := core.Event{Payload: plBytes}
	if err := b.handle(context.Background(), evt); err != nil {
		t.Fatalf("handle: %v", err)
	}
	// Concurrency must NOT have been snapped to 1 — cleared events are ignored.
	if got := ctrl.Get(); got != 4 {
		t.Errorf("concurrency after cleared event = %d, want 4 (unchanged)", got)
	}
}

// TestBandwidthTunerBackstop_ZeroRetryAfter verifies that a status=active event
// with no retry_after_seconds still calls NotifyRateLimit (conservative default).
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

	// No retry_after_seconds field — passes nil.
	evt := core.Event{Payload: backstopActivePayload(t, nil)}
	if err := b.handle(context.Background(), evt); err != nil {
		t.Fatalf("handle: %v", err)
	}
	// NotifyRateLimit uses conservative default → should still snap to 1.
	if got := ctrl.Get(); got != 1 {
		t.Errorf("concurrency after backstop notify (no retry hint) = %d, want 1", got)
	}
}

// TestBandwidthTunerBackstop_EndToEndBusDelivery verifies the full path:
//
//	dispatchHookRelayEnvelope(agent_rate_limited)
//	  → emitRateLimitStatus → bus.Emit(agent_rate_limit_status{active})
//	  → backstop handler → tuner.NotifyRateLimit
//
// This catches the iter-1 wrong-subscription bug where the backstop was
// subscribed to "agent_rate_limited" (a progress-stream type never on the bus)
// rather than "agent_rate_limit_status" (the bus event type).
func TestBandwidthTunerBackstop_EndToEndBusDelivery(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".claude", "projects"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Build and seal a real event bus.
	bus := eventbus.NewBusImpl()

	b := &bandwidthTunerBackstop{}
	if err := b.Subscribe(bus); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if err := bus.Seal(); err != nil {
		t.Fatalf("Seal: %v", err)
	}

	// Construct the tuner and arm the backstop AFTER sealing (matching daemon init order).
	ctrl := NewConcurrencyController(4)
	tuner := NewBandwidthTuner(ctrl, 4, 1_000_000, home)
	b.SetTuner(tuner)

	// Build a hook store with the bus wired in.
	store := newHookSessionStore()
	store.SetEmitter(bus)

	// Simulate a StopFailure{rate_limit} arriving on the socket.
	runID := uuid.New()
	retry := 90
	relayPayload, _ := json.Marshal(map[string]int{"retry_after_seconds": retry})
	env := hookRelayEnvelope{
		Type:             "agent_rate_limited",
		RunID:            runID.String(),
		ClaudeSessionID:  "claude-sess-1",
		HandlerSessionID: "handler-sess-1",
		Payload:          relayPayload,
	}
	ack := store.dispatchHookRelayEnvelope(env)
	if ack.Status != "ok" {
		t.Fatalf("dispatchHookRelayEnvelope: want ok, got %q (%s)", ack.Status, ack.Reason)
	}

	// Allow the asynchronous bus worker pool to deliver the event.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if ctrl.Get() == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := ctrl.Get(); got != 1 {
		t.Errorf("end-to-end: concurrency = %d after 2s, want 1 (NotifyRateLimit not called)", got)
	}
}

// TestBandwidthTunerBackstop_Pi_EventSkipsGlobalTuner verifies PI-073: a
// rate-limit event from a Pi run MUST NOT snap the global concurrency ceiling.
// The backstop must skip NotifyRateLimit when the RunHandle's agent type is Pi.
func TestBandwidthTunerBackstop_Pi_EventSkipsGlobalTuner(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".claude", "projects"), 0o755); err != nil {
		t.Fatal(err)
	}

	ctrl := NewConcurrencyController(4)
	tuner := NewBandwidthTuner(ctrl, 4, 1_000_000, home)

	// Register a Pi run in the registry.
	piRunID := core.RunID(uuid.MustParse("01960084-0000-7000-8000-000000000010"))
	reg := NewRunRegistry()
	handle := &RunHandle{}
	handle.SetAgentType(core.AgentTypePi)
	reg.Register(piRunID, handle)

	b := &bandwidthTunerBackstop{}
	b.SetTuner(tuner)
	b.SetRunRegistry(reg)

	// Build a status=active payload for the Pi run.
	retry := 60
	pl := core.AgentRateLimitStatusPayload{
		RunID:             piRunID,
		SessionID:         "pi-test-session",
		Status:            core.AgentRateLimitStatusActive,
		RetryAfterSeconds: &retry,
		ChangedAt:         time.Now().UTC().Format("2006-01-02T15:04:05.000Z07:00"),
	}
	plBytes, _ := json.Marshal(pl)
	evt := core.Event{Payload: plBytes}

	if err := b.handle(context.Background(), evt); err != nil {
		t.Fatalf("handle: %v", err)
	}
	// Concurrency MUST NOT have been snapped — Pi events are isolated (PI-073).
	if got := ctrl.Get(); got != 4 {
		t.Errorf("concurrency after Pi rate-limit event = %d, want 4 (Pi event must not reach global tuner)", got)
	}
}

// TestBandwidthTunerBackstop_NonPi_EventReachesGlobalTuner verifies PI-073
// complementary case: a non-Pi run's rate-limit event still reaches the
// global tuner and snaps concurrency to 1.
func TestBandwidthTunerBackstop_NonPi_EventReachesGlobalTuner(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".claude", "projects"), 0o755); err != nil {
		t.Fatal(err)
	}

	ctrl := NewConcurrencyController(4)
	tuner := NewBandwidthTuner(ctrl, 4, 1_000_000, home)

	// Register a Claude (non-Pi) run.
	claudeRunID := core.RunID(uuid.MustParse("01960084-0000-7000-8000-000000000011"))
	reg := NewRunRegistry()
	handle := &RunHandle{}
	handle.SetAgentType(core.AgentTypeClaudeCode)
	reg.Register(claudeRunID, handle)

	b := &bandwidthTunerBackstop{}
	b.SetTuner(tuner)
	b.SetRunRegistry(reg)

	retry := 60
	pl := core.AgentRateLimitStatusPayload{
		RunID:             claudeRunID,
		SessionID:         "claude-test-session",
		Status:            core.AgentRateLimitStatusActive,
		RetryAfterSeconds: &retry,
		ChangedAt:         time.Now().UTC().Format("2006-01-02T15:04:05.000Z07:00"),
	}
	plBytes, _ := json.Marshal(pl)
	evt := core.Event{Payload: plBytes}

	if err := b.handle(context.Background(), evt); err != nil {
		t.Fatalf("handle: %v", err)
	}
	// Concurrency MUST be snapped to 1 for a non-Pi run.
	if got := ctrl.Get(); got != 1 {
		t.Errorf("concurrency after Claude rate-limit event = %d, want 1", got)
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

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
	"github.com/gregberns/harmonik/internal/runregistry"
)

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
	got, err := transcriptTokensUsed(filepath.Join(home, ".claude", "projects"), since)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := int64(1150)
	if got != want {
		t.Errorf("transcriptTokensUsed = %d, want %d", got, want)
	}
}

func TestTranscriptTokensUsed_MissingDir(t *testing.T) {
	home := t.TempDir()
	got, err := transcriptTokensUsed(filepath.Join(home, ".claude", "projects"), time.Now().Add(-5*time.Hour))
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
	tuner := NewBandwidthTuner(ctrl, 4, 1_000_000, filepath.Join(home, ".claude", "projects"))
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

	writeTestJSONL(t, projDir, []struct {
		age    time.Duration
		input  int64
		output int64
		create int64
	}{
		{age: 1 * time.Hour, input: 500_000, output: 0, create: 0},
	})

	ctrl := NewConcurrencyController(4)
	tuner := NewBandwidthTuner(ctrl, 4, 1_000_000, filepath.Join(home, ".claude", "projects"))
	tuner.tick()
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

	writeTestJSONL(t, projDir, []struct {
		age    time.Duration
		input  int64
		output int64
		create int64
	}{
		{age: 1 * time.Hour, input: 2_000_000, output: 0, create: 0},
	})

	ctrl := NewConcurrencyController(4)
	tuner := NewBandwidthTuner(ctrl, 4, 1_000_000, filepath.Join(home, ".claude", "projects"))
	tuner.tick()
	if got := ctrl.Get(); got != 1 {
		t.Errorf("expected ceiling=1 when exhausted, got %d", got)
	}
}

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
	tuner := NewBandwidthTuner(ctrl, 4, 1_000_000, filepath.Join(home, ".claude", "projects"))

	b := &bandwidthTunerBackstop{}
	b.SetTuner(tuner)

	retry := 120
	evt := core.Event{Payload: backstopActivePayload(t, &retry)}
	if err := b.handle(context.Background(), evt); err != nil {
		t.Fatalf("handle: %v", err)
	}
	if got := ctrl.Get(); got != 1 {
		t.Errorf("concurrency after backstop notify = %d, want 1", got)
	}
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
	tuner := NewBandwidthTuner(ctrl, 4, 1_000_000, filepath.Join(home, ".claude", "projects"))

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
	tuner := NewBandwidthTuner(ctrl, 4, 1_000_000, filepath.Join(home, ".claude", "projects"))

	b := &bandwidthTunerBackstop{}
	b.SetTuner(tuner)

	evt := core.Event{Payload: backstopActivePayload(t, nil)}
	if err := b.handle(context.Background(), evt); err != nil {
		t.Fatalf("handle: %v", err)
	}
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

	bus := eventbus.NewBusImpl()

	b := &bandwidthTunerBackstop{}
	if err := b.Subscribe(bus); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if err := bus.Seal(); err != nil {
		t.Fatalf("Seal: %v", err)
	}

	ctrl := NewConcurrencyController(4)
	tuner := NewBandwidthTuner(ctrl, 4, 1_000_000, filepath.Join(home, ".claude", "projects"))
	b.SetTuner(tuner)

	store := newHookSessionStore()
	store.SetEmitter(bus)

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
// The backstop must skip NotifyRateLimit when the runregistry.RunHandle's agent type is Pi.
func TestBandwidthTunerBackstop_Pi_EventSkipsGlobalTuner(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".claude", "projects"), 0o750); err != nil {
		t.Fatal(err)
	}

	ctrl := NewConcurrencyController(4)
	tuner := NewBandwidthTuner(ctrl, 4, 1_000_000, filepath.Join(home, ".claude", "projects"))

	piRunID := core.RunID(uuid.MustParse("01960084-0000-7000-8000-000000000010"))
	reg := runregistry.NewRunRegistry()
	handle := &runregistry.RunHandle{}
	handle.SetAgentType(core.AgentTypePi)
	reg.Register(piRunID, handle)

	b := &bandwidthTunerBackstop{}
	b.SetTuner(tuner)
	b.SetRunRegistry(reg)

	retry := 60
	pl := core.AgentRateLimitStatusPayload{
		RunID:             piRunID,
		SessionID:         "pi-test-session",
		Status:            core.AgentRateLimitStatusActive,
		RetryAfterSeconds: &retry,
		ChangedAt:         time.Now().UTC().Format("2006-01-02T15:04:05.000Z07:00"),
	}
	plBytes, err := json.Marshal(pl)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	evt := core.Event{Payload: plBytes}

	if err := b.handle(context.Background(), evt); err != nil {
		t.Fatalf("handle: %v", err)
	}
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
	if err := os.MkdirAll(filepath.Join(home, ".claude", "projects"), 0o750); err != nil {
		t.Fatal(err)
	}

	ctrl := NewConcurrencyController(4)
	tuner := NewBandwidthTuner(ctrl, 4, 1_000_000, filepath.Join(home, ".claude", "projects"))

	claudeRunID := core.RunID(uuid.MustParse("01960084-0000-7000-8000-000000000011"))
	reg := runregistry.NewRunRegistry()
	handle := &runregistry.RunHandle{}
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
	plBytes, err := json.Marshal(pl)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	evt := core.Event{Payload: plBytes}

	if err := b.handle(context.Background(), evt); err != nil {
		t.Fatalf("handle: %v", err)
	}
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
	tuner := NewBandwidthTuner(ctrl, 4, 1_000_000, filepath.Join(home, ".claude", "projects"))

	tuner.NotifyRateLimit(2 * time.Minute)
	if got := ctrl.Get(); got != 1 {
		t.Errorf("expected ceiling=1 after rate limit, got %d", got)
	}

	tuner.tick()
	if got := ctrl.Get(); got != 1 {
		t.Errorf("expected ceiling still 1 during backoff, got %d", got)
	}
}

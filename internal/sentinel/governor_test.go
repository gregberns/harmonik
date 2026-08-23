package sentinel_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/digest"
	"github.com/gregberns/harmonik/internal/sentinel"
)

func writeEvent(t *testing.T, path string, evType core.EventType, ts time.Time, payload []byte) {
	t.Helper()
	v7, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("uuid.NewV7: %v", err)
	}
	id := core.EventID(v7)
	ev := core.Event{
		EventID:         id,
		SchemaVersion:   1,
		Type:            evType,
		TimestampWall:   ts,
		SourceSubsystem: "test",
		Payload:         payload,
	}
	line, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	//nolint:gosec // G304: path is built beneath the test fixture's t.TempDir project.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open events file: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := f.Close(); closeErr != nil {
			t.Errorf("close events file: %v", closeErr)
		}
	})
	_, err = f.Write(append(line, '\n'))
	if err != nil {
		t.Fatalf("write event: %v", err)
	}
}

func makeEventsFile(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	harmonikDir := filepath.Join(dir, ".harmonik", "events")
	if err := os.MkdirAll(harmonikDir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	return dir // return project dir (parent of .harmonik/)
}

func TestGovernor_NoEvents_Watching(t *testing.T) {
	projectDir := makeEventsFile(t)
	now := time.Now()

	state := &sentinel.GovernorState{}
	input := sentinel.GovernorInput{
		ProjectDir:    projectDir,
		Now:           now,
		HasReadyBeads: true,
	}
	cfg := sentinel.Config{
		Window:           30 * time.Minute,
		WarmupWindow:     0, // disable warmup gate for this test
		SustainedWindows: 2,
	}
	sig := sentinel.Evaluate(context.Background(), state, input, cfg)

	if sig.Level != sentinel.ActivationWatching {
		t.Errorf("expected WATCHING, got %s (consecutive=%d)", sig.Level, sig.ConsecutiveLowWindows)
	}
	if sig.ConsecutiveLowWindows != 1 {
		t.Errorf("expected 1 consecutive low window, got %d", sig.ConsecutiveLowWindows)
	}
}

func TestGovernor_SustainedLow_TripsAfterTwoWindows(t *testing.T) {
	projectDir := makeEventsFile(t)
	now := time.Now()

	state := &sentinel.GovernorState{}
	cfg := sentinel.Config{
		Window:           30 * time.Minute,
		WarmupWindow:     0, // disable warmup gate
		SustainedWindows: 2,
	}
	input := sentinel.GovernorInput{
		ProjectDir:    projectDir,
		Now:           now,
		HasReadyBeads: true,
	}

	sig1 := sentinel.Evaluate(context.Background(), state, input, cfg)
	if sig1.Level != sentinel.ActivationWatching {
		t.Errorf("first eval: expected WATCHING, got %s", sig1.Level)
	}

	sig2 := sentinel.Evaluate(context.Background(), state, input, cfg)
	if sig2.Level != sentinel.ActivationActive {
		t.Errorf("second eval: expected ACTIVE, got %s (consecutive=%d)", sig2.Level, sig2.ConsecutiveLowWindows)
	}
}

func TestGovernor_BeadClosedEvent_Dormant(t *testing.T) {
	projectDir := makeEventsFile(t)
	eventsPath := filepath.Join(projectDir, ".harmonik", "events", "events.jsonl")
	now := time.Now()

	writeEvent(t, eventsPath, core.EventTypeBeadClosed, now.Add(-5*time.Minute), json.RawMessage(`{}`))

	state := &sentinel.GovernorState{}
	cfg := sentinel.Config{
		Window:       30 * time.Minute,
		WarmupWindow: 0,
	}
	input := sentinel.GovernorInput{
		ProjectDir:    projectDir,
		Now:           now,
		HasReadyBeads: true,
	}
	sig := sentinel.Evaluate(context.Background(), state, input, cfg)

	if sig.Level != sentinel.ActivationDormant {
		t.Errorf("expected DORMANT after bead_closed, got %s (score=%d)", sig.Level, sig.Sample.MovementScore)
	}
	if sig.Sample.MovementScore < sentinel.DefaultHighWeight {
		t.Errorf("expected score >= %d, got %d", sentinel.DefaultHighWeight, sig.Sample.MovementScore)
	}
}

func TestGovernor_RunCompletedEvent_Dormant(t *testing.T) {
	projectDir := makeEventsFile(t)
	eventsPath := filepath.Join(projectDir, ".harmonik", "events", "events.jsonl")
	now := time.Now()

	writeEvent(t, eventsPath, core.EventTypeRunCompleted, now.Add(-10*time.Minute), json.RawMessage(`{"run_id":"00000000-0000-0000-0000-000000000001","terminal_state_id":"00000000-0000-0000-0000-000000000002","ended_at":"2026-01-01T00:00:00Z"}`))

	state := &sentinel.GovernorState{}
	cfg := sentinel.Config{
		Window:       30 * time.Minute,
		WarmupWindow: 0,
	}
	input := sentinel.GovernorInput{
		ProjectDir:    projectDir,
		Now:           now,
		HasReadyBeads: true,
	}
	sig := sentinel.Evaluate(context.Background(), state, input, cfg)

	if sig.Level != sentinel.ActivationDormant {
		t.Errorf("expected DORMANT after run_completed, got %s", sig.Level)
	}
}

func TestGovernor_ReviewerVerdictApprove_Dormant(t *testing.T) {
	projectDir := makeEventsFile(t)
	eventsPath := filepath.Join(projectDir, ".harmonik", "events", "events.jsonl")
	now := time.Now()

	payload, err := json.Marshal(map[string]interface{}{
		"run_id":            "00000000-0000-0000-0000-000000000001",
		"workflow_mode":     "review-loop",
		"session_id":        "sess-1",
		"claude_session_id": "claude-1",
		"iteration_count":   1,
		"schema_version":    1,
		"verdict":           "APPROVE",
		"flags":             []string{},
		"notes":             "looks good",
	})
	if err != nil {
		t.Fatalf("marshal reviewer verdict payload: %v", err)
	}
	writeEvent(t, eventsPath, core.EventTypeReviewerVerdict, now.Add(-3*time.Minute), payload)

	state := &sentinel.GovernorState{}
	cfg := sentinel.Config{
		Window:       30 * time.Minute,
		WarmupWindow: 0,
	}
	input := sentinel.GovernorInput{
		ProjectDir:    projectDir,
		Now:           now,
		HasReadyBeads: true,
	}
	sig := sentinel.Evaluate(context.Background(), state, input, cfg)

	if sig.Level != sentinel.ActivationDormant {
		t.Errorf("expected DORMANT after reviewer_verdict{APPROVE}, got %s", sig.Level)
	}
}

func TestGovernor_ReviewerVerdictRequestChanges_NotCounted(t *testing.T) {
	projectDir := makeEventsFile(t)
	eventsPath := filepath.Join(projectDir, ".harmonik", "events", "events.jsonl")
	now := time.Now()

	payload, err := json.Marshal(map[string]interface{}{
		"run_id":            "00000000-0000-0000-0000-000000000001",
		"workflow_mode":     "review-loop",
		"session_id":        "sess-1",
		"claude_session_id": "claude-1",
		"iteration_count":   1,
		"schema_version":    1,
		"verdict":           "REQUEST_CHANGES",
		"flags":             []string{},
		"notes":             "needs work",
	})
	if err != nil {
		t.Fatalf("marshal reviewer verdict payload: %v", err)
	}
	writeEvent(t, eventsPath, core.EventTypeReviewerVerdict, now.Add(-3*time.Minute), payload)

	state := &sentinel.GovernorState{}
	cfg := sentinel.Config{
		Window:           30 * time.Minute,
		WarmupWindow:     0,
		SustainedWindows: 2,
	}
	input := sentinel.GovernorInput{
		ProjectDir:    projectDir,
		Now:           now,
		HasReadyBeads: true,
	}
	sig := sentinel.Evaluate(context.Background(), state, input, cfg)
	if sig.Level != sentinel.ActivationWatching {
		t.Errorf("REQUEST_CHANGES should not count; expected WATCHING, got %s (score=%d)",
			sig.Level, sig.Sample.MovementScore)
	}
	if sig.Sample.MovementScore != 0 {
		t.Errorf("expected score=0 for REQUEST_CHANGES, got %d", sig.Sample.MovementScore)
	}
}

func TestGovernor_NoOpportunity_Suppressed(t *testing.T) {
	projectDir := makeEventsFile(t)
	now := time.Now()

	state := &sentinel.GovernorState{ConsecutiveLowWindows: 5}
	cfg := sentinel.Config{
		Window:           30 * time.Minute,
		WarmupWindow:     0,
		SustainedWindows: 2,
	}
	input := sentinel.GovernorInput{
		ProjectDir:        projectDir,
		Now:               now,
		HasReadyBeads:     false,
		HasUndeployedTail: false,
	}
	sig := sentinel.Evaluate(context.Background(), state, input, cfg)

	if sig.Level == sentinel.ActivationActive {
		t.Error("should not trip when no opportunity exists")
	}
	if sig.SuppressedBy != "no_opportunity" {
		t.Errorf("expected suppressed_by=no_opportunity, got %q", sig.SuppressedBy)
	}
}

func TestGovernor_UndeployedTail_CountsAsOpportunity(t *testing.T) {
	projectDir := makeEventsFile(t)
	now := time.Now()

	state := &sentinel.GovernorState{}
	cfg := sentinel.Config{
		Window:           30 * time.Minute,
		WarmupWindow:     0,
		SustainedWindows: 2,
	}
	input := sentinel.GovernorInput{
		ProjectDir:        projectDir,
		Now:               now,
		HasReadyBeads:     false,
		HasUndeployedTail: true,
	}
	sig1 := sentinel.Evaluate(context.Background(), state, input, cfg)
	sig2 := sentinel.Evaluate(context.Background(), state, input, cfg)
	_ = sig1

	if sig2.Level != sentinel.ActivationActive {
		t.Errorf("undeployed tail should satisfy opportunity gate; expected ACTIVE, got %s", sig2.Level)
	}
}

func TestGovernor_WarmupGate_Suppresses(t *testing.T) {
	projectDir := makeEventsFile(t)
	now := time.Now()

	state := &sentinel.GovernorState{
		DaemonStartedAt:       now.Add(-5 * time.Minute), // started 5 min ago
		ConsecutiveLowWindows: 5,                         // already saturated
	}
	cfg := sentinel.Config{
		Window:           30 * time.Minute,
		WarmupWindow:     30 * time.Minute, // needs 30m; only 5m elapsed
		SustainedWindows: 2,
	}
	input := sentinel.GovernorInput{
		ProjectDir:    projectDir,
		Now:           now,
		HasReadyBeads: true,
	}
	sig := sentinel.Evaluate(context.Background(), state, input, cfg)

	if sig.Level == sentinel.ActivationActive {
		t.Error("should not trip during warmup period")
	}
	if sig.SuppressedBy == "" {
		t.Error("expected SuppressedBy to be set during warmup")
	}
}

func TestGovernor_WarmupElapsed_DoesNotSuppress(t *testing.T) {
	projectDir := makeEventsFile(t)
	now := time.Now()

	state := &sentinel.GovernorState{
		DaemonStartedAt:       now.Add(-60 * time.Minute), // started 60 min ago
		ConsecutiveLowWindows: 2,                          // sustained gate already met
	}
	cfg := sentinel.Config{
		Window:           30 * time.Minute,
		WarmupWindow:     30 * time.Minute, // satisfied: 60m > 30m
		SustainedWindows: 2,
	}
	input := sentinel.GovernorInput{
		ProjectDir:    projectDir,
		Now:           now,
		HasReadyBeads: true,
	}
	sig := sentinel.Evaluate(context.Background(), state, input, cfg)

	if sig.Level != sentinel.ActivationActive {
		t.Errorf("warmup elapsed + sustained met; expected ACTIVE, got %s (suppressed=%s)",
			sig.Level, sig.SuppressedBy)
	}
}

func TestGovernor_EventOutsideWindow_NotCounted(t *testing.T) {
	projectDir := makeEventsFile(t)
	eventsPath := filepath.Join(projectDir, ".harmonik", "events", "events.jsonl")
	now := time.Now()

	writeEvent(t, eventsPath, core.EventTypeBeadClosed, now.Add(-45*time.Minute), json.RawMessage(`{}`))

	state := &sentinel.GovernorState{}
	cfg := sentinel.Config{
		Window:           30 * time.Minute,
		WarmupWindow:     0,
		SustainedWindows: 2,
	}
	input := sentinel.GovernorInput{
		ProjectDir:    projectDir,
		Now:           now,
		HasReadyBeads: true,
	}
	sig := sentinel.Evaluate(context.Background(), state, input, cfg)

	if sig.Sample.MovementScore != 0 {
		t.Errorf("event outside window should not be counted; got score=%d", sig.Sample.MovementScore)
	}
	if sig.Level != sentinel.ActivationWatching {
		t.Errorf("expected WATCHING (event outside window), got %s", sig.Level)
	}
}

func TestGovernor_HighMovementResetsConsecutiveCount(t *testing.T) {
	projectDir := makeEventsFile(t)
	eventsPath := filepath.Join(projectDir, ".harmonik", "events", "events.jsonl")
	now := time.Now()

	cfg := sentinel.Config{
		Window:           30 * time.Minute,
		WarmupWindow:     0,
		SustainedWindows: 2,
	}

	state := &sentinel.GovernorState{}
	input := sentinel.GovernorInput{
		ProjectDir:    projectDir,
		Now:           now,
		HasReadyBeads: true,
	}

	sentinel.Evaluate(context.Background(), state, input, cfg)
	sig := sentinel.Evaluate(context.Background(), state, input, cfg)
	if sig.Level != sentinel.ActivationActive {
		t.Errorf("setup: expected ACTIVE, got %s", sig.Level)
	}

	writeEvent(t, eventsPath, core.EventTypeBeadClosed, now.Add(-1*time.Minute), json.RawMessage(`{}`))

	sigHigh := sentinel.Evaluate(context.Background(), state, input, cfg)
	if sigHigh.Level != sentinel.ActivationDormant {
		t.Errorf("expected DORMANT after high window, got %s", sigHigh.Level)
	}
	if state.ConsecutiveLowWindows != 0 {
		t.Errorf("expected consecutive count reset to 0 after high window, got %d",
			state.ConsecutiveLowWindows)
	}
}

func TestGovernor_ActivationLevelString(t *testing.T) {
	cases := []struct {
		level sentinel.ActivationLevel
		want  string
	}{
		{sentinel.ActivationDormant, "dormant"},
		{sentinel.ActivationWatching, "watching"},
		{sentinel.ActivationActive, "active"},
		{sentinel.ActivationHalt, "halt"},
	}
	for _, tc := range cases {
		if got := tc.level.String(); got != tc.want {
			t.Errorf("ActivationLevel(%d).String() = %q, want %q", int(tc.level), got, tc.want)
		}
	}
}

func TestGLiveness_Disabled_NeverHalts(t *testing.T) {
	projectDir := makeEventsFile(t)
	now := time.Now()

	state := &sentinel.GovernorState{}
	cfg := sentinel.Config{
		Window:              30 * time.Minute,
		WarmupWindow:        0,
		LivenessNoProgressN: 0, // disabled
	}
	input := sentinel.GovernorInput{
		ProjectDir:    projectDir,
		Now:           now,
		HasReadyBeads: true,
	}

	for i := 0; i < 10; i++ {
		sig := sentinel.Evaluate(context.Background(), state, input, cfg)
		if sig.Level == sentinel.ActivationHalt {
			t.Fatalf("iteration %d: G-liveness disabled but got ActivationHalt", i)
		}
		if sig.LivenessViolated {
			t.Fatalf("iteration %d: G-liveness disabled but LivenessViolated=true", i)
		}
	}
}

func TestGLiveness_TripsAfterNZeroCycles(t *testing.T) {
	projectDir := makeEventsFile(t)
	now := time.Now()

	state := &sentinel.GovernorState{}
	cfg := sentinel.Config{
		Window:              30 * time.Minute,
		WarmupWindow:        0,
		LivenessNoProgressN: 3,
	}
	input := sentinel.GovernorInput{
		ProjectDir:    projectDir,
		Now:           now,
		HasReadyBeads: true,
	}

	var sig sentinel.GovernorSignal
	for i := 1; i <= 3; i++ {
		sig = sentinel.Evaluate(context.Background(), state, input, cfg)
		if i < 3 {
			if sig.Level == sentinel.ActivationHalt {
				t.Errorf("cycle %d: premature ActivationHalt (expected after cycle 3)", i)
			}
			if sig.LivenessViolated {
				t.Errorf("cycle %d: premature LivenessViolated=true", i)
			}
		}
	}

	if sig.Level != sentinel.ActivationHalt {
		t.Errorf("cycle 3: expected ActivationHalt, got %s", sig.Level)
	}
	if !sig.LivenessViolated {
		t.Error("cycle 3: expected LivenessViolated=true")
	}
	if sig.ConsecutiveZeroCycles != 3 {
		t.Errorf("cycle 3: expected ConsecutiveZeroCycles=3, got %d", sig.ConsecutiveZeroCycles)
	}
}

// TestGLiveness_ConfiguredN_TripsAfterN proves the gate trips after N zero-cycles
// when N is supplied through the PRODUCTION config bridge (hk-drygf FIX-B): the
// Config is built from a real .harmonik/config.yaml via
// digest.LoadSentinelConfig(...).GovernorConfig(), NOT a hand-set
// sentinel.Config{LivenessNoProgressN: N}. Hand-set Config is exactly why the
// pre-FIX-B G-liveness tests passed while production stayed broken — the bridge
// dropped the value. With the bridge fixed, a configured N must reach the gate.
func TestGLiveness_ConfiguredN_TripsAfterN(t *testing.T) {
	const n = 5

	projectDir := makeEventsFile(t)
	configPath := filepath.Join(projectDir, ".harmonik", "config.yaml")
	configYAML := fmt.Sprintf("sentinel:\n  window: 30m\n  liveness_no_progress_n: %d\n", n)
	if err := os.WriteFile(configPath, []byte(configYAML), 0o600); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}

	sentinelCfg, err := digest.LoadSentinelConfig(projectDir)
	if err != nil {
		t.Fatalf("LoadSentinelConfig: %v", err)
	}
	cfg, err := sentinelCfg.GovernorConfig()
	if err != nil {
		t.Fatalf("GovernorConfig: %v", err)
	}
	if cfg.LivenessNoProgressN != n {
		t.Fatalf("production bridge dropped the value: LivenessNoProgressN=%d, want %d", cfg.LivenessNoProgressN, n)
	}

	now := time.Now()
	state := &sentinel.GovernorState{}
	input := sentinel.GovernorInput{
		ProjectDir:    projectDir,
		Now:           now,
		HasReadyBeads: true,
	}

	var sig sentinel.GovernorSignal
	for i := 1; i <= n; i++ {
		sig = sentinel.Evaluate(context.Background(), state, input, cfg)
		if i < n && sig.LivenessViolated {
			t.Errorf("cycle %d: premature LivenessViolated (expected after cycle %d)", i, n)
		}
	}

	if !sig.LivenessViolated {
		t.Errorf("cycle %d: expected LivenessViolated=true via production-configured N", n)
	}
	if sig.Level != sentinel.ActivationHalt {
		t.Errorf("cycle %d: expected Level=ActivationHalt, got %s", n, sig.Level)
	}
}

func TestGLiveness_ResetOnProgress(t *testing.T) {
	projectDir := makeEventsFile(t)
	eventsPath := filepath.Join(projectDir, ".harmonik", "events", "events.jsonl")
	now := time.Now()

	state := &sentinel.GovernorState{}
	cfg := sentinel.Config{
		Window:              30 * time.Minute,
		WarmupWindow:        0,
		LivenessNoProgressN: 3,
	}
	input := sentinel.GovernorInput{
		ProjectDir:    projectDir,
		Now:           now,
		HasReadyBeads: true,
	}

	for i := 0; i < 2; i++ {
		sig := sentinel.Evaluate(context.Background(), state, input, cfg)
		if sig.Level == sentinel.ActivationHalt {
			t.Fatalf("premature halt at zero cycle %d", i)
		}
	}
	if state.ConsecutiveZeroCycles != 2 {
		t.Fatalf("expected ConsecutiveZeroCycles=2, got %d", state.ConsecutiveZeroCycles)
	}

	writeEvent(t, eventsPath, core.EventTypeBeadClosed, now.Add(-1*time.Minute), json.RawMessage(`{}`))
	sig := sentinel.Evaluate(context.Background(), state, input, cfg)
	if sig.Level == sentinel.ActivationHalt {
		t.Error("progress event should have reset G-liveness counter; got ActivationHalt")
	}
	if state.ConsecutiveZeroCycles != 0 {
		t.Errorf("expected ConsecutiveZeroCycles reset to 0 after progress, got %d",
			state.ConsecutiveZeroCycles)
	}

	freshDir := makeEventsFile(t)
	freshInput := sentinel.GovernorInput{
		ProjectDir:    freshDir,
		Now:           now,
		HasReadyBeads: true,
	}
	for i := 1; i <= 2; i++ {
		sig2 := sentinel.Evaluate(context.Background(), state, freshInput, cfg)
		if sig2.Level == sentinel.ActivationHalt {
			t.Errorf("cycle %d after reset: unexpected ActivationHalt (need %d more for N=3)", i, 3-i)
		}
	}
}

func TestGLiveness_WarmupSuppressesHalt(t *testing.T) {
	projectDir := makeEventsFile(t)
	now := time.Now()

	state := &sentinel.GovernorState{
		DaemonStartedAt: now.Add(-5 * time.Minute), // started 5 min ago
	}
	cfg := sentinel.Config{
		Window:              30 * time.Minute,
		WarmupWindow:        30 * time.Minute, // 30m required; only 5m elapsed
		LivenessNoProgressN: 2,
	}
	input := sentinel.GovernorInput{
		ProjectDir:    projectDir,
		Now:           now,
		HasReadyBeads: true,
	}

	for i := 0; i < 5; i++ {
		sig := sentinel.Evaluate(context.Background(), state, input, cfg)
		if sig.Level == sentinel.ActivationHalt {
			t.Fatalf("cycle %d: G-liveness fired during warmup window", i)
		}
		if sig.LivenessViolated {
			t.Fatalf("cycle %d: LivenessViolated=true during warmup window", i)
		}
	}
}

func TestGLiveness_OperatorPause_DoesNotAccumulate(t *testing.T) {
	projectDir := makeEventsFile(t)
	now := time.Now()

	state := &sentinel.GovernorState{}
	cfg := sentinel.Config{
		Window:              30 * time.Minute,
		WarmupWindow:        0,
		LivenessNoProgressN: 3,
	}
	pausedInput := sentinel.GovernorInput{
		ProjectDir:     projectDir,
		Now:            now,
		HasReadyBeads:  true,
		OperatorPaused: true,
	}

	for i := 0; i < 10; i++ {
		sig := sentinel.Evaluate(context.Background(), state, pausedInput, cfg)
		if sig.Level == sentinel.ActivationHalt {
			t.Fatalf("cycle %d: G-liveness fired during operator-pause (must not)", i)
		}
		if sig.LivenessViolated {
			t.Fatalf("cycle %d: LivenessViolated=true during operator-pause", i)
		}
		if state.ConsecutiveZeroCycles != 0 {
			t.Fatalf("cycle %d: ConsecutiveZeroCycles=%d during operator-pause (must stay 0)",
				i, state.ConsecutiveZeroCycles)
		}
	}

	resumedInput := sentinel.GovernorInput{
		ProjectDir:     projectDir,
		Now:            now,
		HasReadyBeads:  true,
		OperatorPaused: false,
	}
	for i := 1; i <= 3; i++ {
		sig := sentinel.Evaluate(context.Background(), state, resumedInput, cfg)
		if i < 3 && sig.LivenessViolated {
			t.Errorf("post-resume cycle %d: premature LivenessViolated=true", i)
		}
		if i == 3 {
			if !sig.LivenessViolated {
				t.Error("post-resume cycle 3: expected LivenessViolated=true after N zero cycles")
			}
			if sig.Level != sentinel.ActivationHalt {
				t.Errorf("post-resume cycle 3: expected ActivationHalt, got %s", sig.Level)
			}
		}
	}
}

func TestGLiveness_WarmupElapsed_Halts(t *testing.T) {
	projectDir := makeEventsFile(t)
	now := time.Now()

	state := &sentinel.GovernorState{
		DaemonStartedAt:       now.Add(-60 * time.Minute), // started 60 min ago; warmup done
		ConsecutiveZeroCycles: 2,                          // already at N-1
	}
	cfg := sentinel.Config{
		Window:              30 * time.Minute,
		WarmupWindow:        30 * time.Minute, // satisfied: 60m > 30m
		LivenessNoProgressN: 3,
	}
	input := sentinel.GovernorInput{
		ProjectDir:    projectDir,
		Now:           now,
		HasReadyBeads: true,
	}

	sig := sentinel.Evaluate(context.Background(), state, input, cfg)
	if sig.Level != sentinel.ActivationHalt {
		t.Errorf("warmup elapsed + ConsecutiveZeroCycles=3: expected ActivationHalt, got %s", sig.Level)
	}
	if !sig.LivenessViolated {
		t.Error("expected LivenessViolated=true when halt fires")
	}
}

func TestGLiveness_ZeroCyclesCounter_TrackedInSignal(t *testing.T) {
	projectDir := makeEventsFile(t)
	now := time.Now()

	state := &sentinel.GovernorState{}
	cfg := sentinel.Config{
		Window:              30 * time.Minute,
		WarmupWindow:        0,
		LivenessNoProgressN: 10, // high threshold so we don't halt during this test
	}
	input := sentinel.GovernorInput{
		ProjectDir:    projectDir,
		Now:           now,
		HasReadyBeads: true,
	}

	for i := 1; i <= 5; i++ {
		sig := sentinel.Evaluate(context.Background(), state, input, cfg)
		if sig.ConsecutiveZeroCycles != i {
			t.Errorf("cycle %d: expected ConsecutiveZeroCycles=%d, got %d", i, i, sig.ConsecutiveZeroCycles)
		}
	}
}

func TestGovernor_HasOpportunity_ReflectsInput(t *testing.T) {
	projectDir := makeEventsFile(t)
	now := time.Now()

	for _, hasReady := range []bool{true, false} {
		for _, hasTail := range []bool{true, false} {
			state := &sentinel.GovernorState{}
			input := sentinel.GovernorInput{
				ProjectDir:        projectDir,
				Now:               now,
				HasReadyBeads:     hasReady,
				HasUndeployedTail: hasTail,
			}
			sig := sentinel.Evaluate(context.Background(), state, input, sentinel.Config{
				Window:       30 * time.Minute,
				WarmupWindow: 0,
			})
			want := hasReady || hasTail
			if sig.HasOpportunity != want {
				t.Errorf("hasReady=%v hasTail=%v → HasOpportunity=%v, want %v",
					hasReady, hasTail, sig.HasOpportunity, want)
			}
		}
	}
}

func makeGitProjectFixture(t *testing.T) (projectDir string, pushNewCommit func()) {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "--initial-branch=main")
	run("config", "user.email", "test@harmonik.local")
	run("config", "user.name", "Harmonik Test")
	if err := os.WriteFile(filepath.Join(dir, "f"), []byte("init\n"), 0o600); err != nil {
		t.Fatalf("makeGitProjectFixture: WriteFile: %v", err)
	}
	run("add", ".")
	run("commit", "-m", "init")

	originDir := t.TempDir()
	//nolint:gosec // G204: originDir is this test's t.TempDir fixture and all git arguments are literals.
	if out, err := exec.CommandContext(t.Context(), "git", "init", "--bare", "--initial-branch=main", originDir).CombinedOutput(); err != nil {
		t.Fatalf("makeGitProjectFixture: git init --bare: %v\n%s", err, out)
	}
	run("remote", "add", "origin", originDir)
	run("push", "origin", "main")

	if err := os.MkdirAll(filepath.Join(dir, ".harmonik", "events"), 0o750); err != nil {
		t.Fatalf("makeGitProjectFixture: mkdir events: %v", err)
	}

	counter := 0
	pushNewCommit = func() {
		counter++
		fname := filepath.Join(dir, fmt.Sprintf("commit%d", counter))
		if err := os.WriteFile(fname, []byte("work\n"), 0o600); err != nil {
			t.Fatalf("makeGitProjectFixture: WriteFile commit%d: %v", counter, err)
		}
		run("add", ".")
		run("commit", "-m", fmt.Sprintf("work %d", counter))
		run("push", "origin", "main")
	}
	return dir, pushNewCommit
}

// TestGLiveness_HaltSignalHasPageArtifactFields verifies that when G-liveness
// fires (ActivationHalt), the returned GovernorSignal carries
// ConsecutiveZeroCycles == N so that the workloop can build the liveness_halt
// page event payload without reading state again.
func TestGLiveness_HaltSignalHasPageArtifactFields(t *testing.T) {
	t.Parallel()
	projectDir := makeEventsFile(t)
	now := time.Now()
	const N = 4

	state := &sentinel.GovernorState{}
	cfg := sentinel.Config{
		Window:              30 * time.Minute,
		WarmupWindow:        0,
		LivenessNoProgressN: N,
	}
	input := sentinel.GovernorInput{
		ProjectDir:    projectDir,
		Now:           now,
		HasReadyBeads: true,
	}

	var sig sentinel.GovernorSignal
	for i := 1; i <= N; i++ {
		sig = sentinel.Evaluate(context.Background(), state, input, cfg)
	}

	if sig.Level != sentinel.ActivationHalt {
		t.Fatalf("expected ActivationHalt after %d zero cycles, got %s", N, sig.Level)
	}
	if !sig.LivenessViolated {
		t.Error("LivenessViolated should be true when ActivationHalt fires")
	}
	if sig.ConsecutiveZeroCycles != N {
		t.Errorf("ConsecutiveZeroCycles in halt signal = %d, want %d (page artifact field)",
			sig.ConsecutiveZeroCycles, N)
	}
}

// TestGLiveness_HeadAdvanceResetsZeroCycles verifies that a commit on
// origin/main within the window gives MovementScore > 0 and resets
// ConsecutiveZeroCycles to 0. This is the git-counterpart to
// TestGLiveness_ResetOnProgress (events.jsonl).
//
// Strategy: use `now` = 2 hours from now so the fixture's initial commit
// (which is at real wall-clock time) is within the 30m window only after we
// push a fresh commit anchored near `now`. For the setup cycles we use
// `setupNow` = 2h30m from now so the real-time commits are outside THAT window
// → zero cycles. Then we switch to `now` = real time + push a new commit → within.
//
// Simpler approach: use a fake "past" now for the setup (far enough that no
// real commits fall in the window), then use real now after the push.
func TestGLiveness_HeadAdvanceResetsZeroCycles(t *testing.T) {
	t.Parallel()
	projectDir, pushNewCommit := makeGitProjectFixture(t)

	setupNow := time.Now().Add(2 * time.Hour)

	state := &sentinel.GovernorState{}
	cfg := sentinel.Config{
		Window:              30 * time.Minute,
		WarmupWindow:        0,
		LivenessNoProgressN: 10, // high N so we don't halt during setup
	}
	setupInput := sentinel.GovernorInput{
		ProjectDir:    projectDir,
		Now:           setupNow,
		HasReadyBeads: true,
	}

	for i := 0; i < 2; i++ {
		sig := sentinel.Evaluate(context.Background(), state, setupInput, cfg)
		if sig.Level == sentinel.ActivationHalt {
			t.Fatalf("unexpected ActivationHalt at setup cycle %d", i)
		}
	}
	if state.ConsecutiveZeroCycles != 2 {
		t.Fatalf("setup: expected ConsecutiveZeroCycles=2, got %d", state.ConsecutiveZeroCycles)
	}

	pushNewCommit()

	realNow := time.Now()
	finalInput := sentinel.GovernorInput{
		ProjectDir:    projectDir,
		Now:           realNow,
		HasReadyBeads: true,
	}

	sig := sentinel.Evaluate(context.Background(), state, finalInput, cfg)
	if sig.Sample.HeadAdvanceCount == 0 {
		t.Error("HeadAdvanceCount should be > 0 after pushing a commit within the window")
	}
	if sig.Sample.MovementScore == 0 {
		t.Error("MovementScore should be > 0 after HEAD advance")
	}
	if sig.ConsecutiveZeroCycles != 0 {
		t.Errorf("ConsecutiveZeroCycles should reset to 0 after HEAD advance, got %d",
			sig.ConsecutiveZeroCycles)
	}
	if state.ConsecutiveZeroCycles != 0 {
		t.Errorf("state.ConsecutiveZeroCycles should reset to 0 after HEAD advance, got %d",
			state.ConsecutiveZeroCycles)
	}
}

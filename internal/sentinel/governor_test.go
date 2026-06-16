package sentinel_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/sentinel"
)

// writeEvent appends one JSONL event line to path.
func writeEvent(t *testing.T, path string, evType core.EventType, ts time.Time, payload []byte) {
	t.Helper()
	id := core.EventID(uuid.New())
	ev := core.Event{
		EventID:         id,
		SchemaVersion:   1,
		Type:            string(evType),
		TimestampWall:   ts,
		SourceSubsystem: "test",
		Payload:         payload,
	}
	line, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open events file: %v", err)
	}
	defer func() { _ = f.Close() }()
	_, err = f.Write(append(line, '\n'))
	if err != nil {
		t.Fatalf("write event: %v", err)
	}
}

// makeEventsFile creates a temporary events.jsonl file and returns its path.
func makeEventsFile(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	harmonikDir := filepath.Join(dir, ".harmonik", "events")
	if err := os.MkdirAll(harmonikDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	return dir // return project dir (parent of .harmonik/)
}

func TestGovernor_NoEvents_Watching(t *testing.T) {
	// An empty events.jsonl with no git project → low movement → WATCHING
	// (not ACTIVE because sustained gate requires 2 consecutive windows by default).
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
	// Two consecutive low windows with ready beads and no warmup → ACTIVE.
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

	// First evaluation → WATCHING (consecutive=1, need 2)
	sig1 := sentinel.Evaluate(context.Background(), state, input, cfg)
	if sig1.Level != sentinel.ActivationWatching {
		t.Errorf("first eval: expected WATCHING, got %s", sig1.Level)
	}

	// Second evaluation → ACTIVE (consecutive=2, satisfied)
	sig2 := sentinel.Evaluate(context.Background(), state, input, cfg)
	if sig2.Level != sentinel.ActivationActive {
		t.Errorf("second eval: expected ACTIVE, got %s (consecutive=%d)", sig2.Level, sig2.ConsecutiveLowWindows)
	}
}

func TestGovernor_BeadClosedEvent_Dormant(t *testing.T) {
	// A bead_closed event within the window → high movement → DORMANT.
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
	// A run_completed event within the window → high movement → DORMANT.
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
	// A reviewer_verdict{APPROVE} within the window → high movement → DORMANT.
	projectDir := makeEventsFile(t)
	eventsPath := filepath.Join(projectDir, ".harmonik", "events", "events.jsonl")
	now := time.Now()

	payload, _ := json.Marshal(map[string]interface{}{
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
	// reviewer_verdict{REQUEST_CHANGES} does NOT count as terminal progress.
	projectDir := makeEventsFile(t)
	eventsPath := filepath.Join(projectDir, ".harmonik", "events", "events.jsonl")
	now := time.Now()

	payload, _ := json.Marshal(map[string]interface{}{
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
	// First window: REQUEST_CHANGES should not count → low → WATCHING
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
	// Low movement but no ready beads and no undeployed tail → suppressed by opportunity gate.
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
	// HasUndeployedTail=true satisfies the opportunity gate even with no ready beads.
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
	// First window
	sig1 := sentinel.Evaluate(context.Background(), state, input, cfg)
	// Second window → should trip
	sig2 := sentinel.Evaluate(context.Background(), state, input, cfg)
	_ = sig1

	if sig2.Level != sentinel.ActivationActive {
		t.Errorf("undeployed tail should satisfy opportunity gate; expected ACTIVE, got %s", sig2.Level)
	}
}

func TestGovernor_WarmupGate_Suppresses(t *testing.T) {
	// Daemon just started; warmup window has not elapsed → suppressed.
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
	// Warmup window has elapsed → not suppressed; sustained gate satisfied → ACTIVE.
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
	// The empty events.jsonl gives score=0 → low window → consecutive increments to 3.
	sig := sentinel.Evaluate(context.Background(), state, input, cfg)

	if sig.Level != sentinel.ActivationActive {
		t.Errorf("warmup elapsed + sustained met; expected ACTIVE, got %s (suppressed=%s)",
			sig.Level, sig.SuppressedBy)
	}
}

func TestGovernor_EventOutsideWindow_NotCounted(t *testing.T) {
	// An event older than the window should not count toward movement.
	projectDir := makeEventsFile(t)
	eventsPath := filepath.Join(projectDir, ".harmonik", "events", "events.jsonl")
	now := time.Now()

	// bead_closed 45 minutes ago → outside a 30-minute window
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
	// First low window → WATCHING
	if sig.Level != sentinel.ActivationWatching {
		t.Errorf("expected WATCHING (event outside window), got %s", sig.Level)
	}
}

func TestGovernor_HighMovementResetsConsecutiveCount(t *testing.T) {
	// After two low windows, a high window should reset the consecutive count.
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

	// Two low windows → would trip on the second
	sentinel.Evaluate(context.Background(), state, input, cfg)
	sig := sentinel.Evaluate(context.Background(), state, input, cfg)
	if sig.Level != sentinel.ActivationActive {
		t.Errorf("setup: expected ACTIVE, got %s", sig.Level)
	}

	// Now write a bead_closed event in the window
	writeEvent(t, eventsPath, core.EventTypeBeadClosed, now.Add(-1*time.Minute), json.RawMessage(`{}`))

	// High window → DORMANT; consecutive count resets to 0
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
	}
	for _, tc := range cases {
		if got := tc.level.String(); got != tc.want {
			t.Errorf("ActivationLevel(%d).String() = %q, want %q", int(tc.level), got, tc.want)
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

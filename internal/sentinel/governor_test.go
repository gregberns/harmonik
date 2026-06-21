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
		{sentinel.ActivationHalt, "halt"},
	}
	for _, tc := range cases {
		if got := tc.level.String(); got != tc.want {
			t.Errorf("ActivationLevel(%d).String() = %q, want %q", int(tc.level), got, tc.want)
		}
	}
}

// --- G-liveness self-kill gate tests (spec §6.1, bead hk-2do3) ---

func TestGLiveness_Disabled_NeverHalts(t *testing.T) {
	// LivenessNoProgressN == 0 → G-liveness gate disabled; even many zero cycles
	// must not produce ActivationHalt.
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
	// N=3 consecutive zero-progress cycles → ActivationHalt + LivenessViolated.
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

func TestGLiveness_ResetOnProgress(t *testing.T) {
	// After N-1 zero cycles, a terminal-progress event resets the counter.
	// Subsequent zero cycles must count from 0 again.
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

	// Two zero-progress cycles (N-1 = 2; one short of tripping).
	for i := 0; i < 2; i++ {
		sig := sentinel.Evaluate(context.Background(), state, input, cfg)
		if sig.Level == sentinel.ActivationHalt {
			t.Fatalf("premature halt at zero cycle %d", i)
		}
	}
	if state.ConsecutiveZeroCycles != 2 {
		t.Fatalf("expected ConsecutiveZeroCycles=2, got %d", state.ConsecutiveZeroCycles)
	}

	// Write a bead_closed event → progress resets the counter.
	writeEvent(t, eventsPath, core.EventTypeBeadClosed, now.Add(-1*time.Minute), json.RawMessage(`{}`))
	sig := sentinel.Evaluate(context.Background(), state, input, cfg)
	if sig.Level == sentinel.ActivationHalt {
		t.Error("progress event should have reset G-liveness counter; got ActivationHalt")
	}
	if state.ConsecutiveZeroCycles != 0 {
		t.Errorf("expected ConsecutiveZeroCycles reset to 0 after progress, got %d",
			state.ConsecutiveZeroCycles)
	}

	// Remove the event so subsequent cycles are zero again; must restart from 0.
	// Re-use same eventsPath but write to a fresh project dir to clear events.
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
	// G-liveness does not fire during the daemon warmup window even if N
	// zero-progress cycles have elapsed.
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

	// Run N cycles with zero progress; warmup must suppress the halt.
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

func TestGLiveness_WarmupElapsed_Halts(t *testing.T) {
	// Once the warmup window elapses, G-liveness fires after N zero-progress cycles.
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

	// Empty events.jsonl → score=0 → ConsecutiveZeroCycles reaches 3 → halt.
	sig := sentinel.Evaluate(context.Background(), state, input, cfg)
	if sig.Level != sentinel.ActivationHalt {
		t.Errorf("warmup elapsed + ConsecutiveZeroCycles=3: expected ActivationHalt, got %s", sig.Level)
	}
	if !sig.LivenessViolated {
		t.Error("expected LivenessViolated=true when halt fires")
	}
}

func TestGLiveness_ZeroCyclesCounter_TrackedInSignal(t *testing.T) {
	// ConsecutiveZeroCycles in the returned signal reflects the post-update count.
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

// ---------------------------------------------------------------------------
// makeGitProjectFixture — helper for HEAD-advance tests
// ---------------------------------------------------------------------------

// makeGitProjectFixture initialises a local git repo with an origin remote.
// Returns the project directory and a pushNewCommit closure that adds a new
// commit to origin/main each time it is called.
func makeGitProjectFixture(t *testing.T) (projectDir string, pushNewCommit func()) {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		//nolint:gosec // G204: git args are test-internal literals
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "--initial-branch=main")
	run("config", "user.email", "test@harmonik.local")
	run("config", "user.name", "Harmonik Test")
	if err := os.WriteFile(filepath.Join(dir, "f"), []byte("init\n"), 0o644); err != nil {
		t.Fatalf("makeGitProjectFixture: WriteFile: %v", err)
	}
	run("add", ".")
	run("commit", "-m", "init")

	originDir := t.TempDir()
	//nolint:gosec // G204: git args are test-internal literals
	if out, err := exec.Command("git", "init", "--bare", "--initial-branch=main", originDir).CombinedOutput(); err != nil {
		t.Fatalf("makeGitProjectFixture: git init --bare: %v\n%s", err, out)
	}
	run("remote", "add", "origin", originDir)
	run("push", "origin", "main")

	// Create events dir so the projectDir is valid for Evaluate.
	if err := os.MkdirAll(filepath.Join(dir, ".harmonik", "events"), 0o755); err != nil {
		t.Fatalf("makeGitProjectFixture: mkdir events: %v", err)
	}

	counter := 0
	pushNewCommit = func() {
		counter++
		fname := filepath.Join(dir, fmt.Sprintf("commit%d", counter))
		if err := os.WriteFile(fname, []byte("work\n"), 0o644); err != nil {
			t.Fatalf("makeGitProjectFixture: WriteFile commit%d: %v", counter, err)
		}
		run("add", ".")
		run("commit", "-m", fmt.Sprintf("work %d", counter))
		run("push", "origin", "main")
	}
	return dir, pushNewCommit
}

// ---------------------------------------------------------------------------
// B4-1: G-liveness halt signal carries ConsecutiveZeroCycles
// ---------------------------------------------------------------------------

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
	// The signal must carry the exact ConsecutiveZeroCycles count so the workloop
	// can embed it in the liveness_halt page artifact without a second state read.
	if sig.ConsecutiveZeroCycles != N {
		t.Errorf("ConsecutiveZeroCycles in halt signal = %d, want %d (page artifact field)",
			sig.ConsecutiveZeroCycles, N)
	}
}

// ---------------------------------------------------------------------------
// B4-2: HEAD-advance resets ConsecutiveZeroCycles
// ---------------------------------------------------------------------------

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

	// Use a "past" window for setup so the initial commit is outside it.
	// The initial commit was created at real time; we look 2h in the future
	// so real-time commits are 2h before `setupNow` → well outside the 30m window.
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

	// Two zero-cycle evaluations: window is [setupNow-30m, setupNow] which is
	// entirely in the future relative to the fixture's initial commit → score=0.
	for i := 0; i < 2; i++ {
		sig := sentinel.Evaluate(context.Background(), state, setupInput, cfg)
		if sig.Level == sentinel.ActivationHalt {
			t.Fatalf("unexpected ActivationHalt at setup cycle %d", i)
		}
	}
	if state.ConsecutiveZeroCycles != 2 {
		t.Fatalf("setup: expected ConsecutiveZeroCycles=2, got %d", state.ConsecutiveZeroCycles)
	}

	// Push a new commit to origin/main. Its committer date is ~real-now.
	pushNewCommit()

	// Evaluate with setupNow as window end: [setupNow-30m, setupNow].
	// The new commit was made at real-now which is ~2h before setupNow, so still
	// outside the future window. We need to use real now for the final eval so
	// the new commit falls inside the window.
	realNow := time.Now()
	finalInput := sentinel.GovernorInput{
		ProjectDir:    projectDir,
		Now:           realNow,
		HasReadyBeads: true,
	}

	// Next evaluation with real now: HEAD advance gives MovementScore > 0 → counter resets.
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

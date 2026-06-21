package sentinel_test

// governor_bt1_test.go — BT1 unit-gap tests for the movement governor.
//
// Covers: HEAD-advance counting (was 0 coverage), weight-0-chatter vs a
// populated events.jsonl, boundary-equality/staircase reproducibility,
// and the "single-low-window stays WATCHING" invariant.
//
// Bead: hk-tbg8 (flywheel-BT1). Epic: hk-0oca (codename:flywheel).

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/sentinel"
)

// setupGitRepoWithCommit creates a minimal git repo whose origin/main branch
// has exactly one commit. The commit's committer date is set to commitTime so
// window-filter tests work correctly. Returns the projectDir, which already
// contains .harmonik/events/.
func setupGitRepoWithCommit(t *testing.T, commitTime time.Time) string {
	t.Helper()
	tmp := t.TempDir()

	bareDir := filepath.Join(tmp, "origin.git")
	if out, err := exec.Command("git", "init", "--bare", bareDir).CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v\n%s", err, out)
	}

	projectDir := filepath.Join(tmp, "project")
	if out, err := exec.Command("git", "init", projectDir).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}

	runGit := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	runGit(projectDir, "config", "user.email", "test@example.com")
	runGit(projectDir, "config", "user.name", "Test User")
	runGit(projectDir, "remote", "add", "origin", bareDir)

	if err := os.WriteFile(filepath.Join(projectDir, "x.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	runGit(projectDir, "add", "x.txt")

	dateStr := commitTime.UTC().Format(time.RFC3339)
	cmd := exec.Command("git", "-C", projectDir, "commit", "-m", "test commit")
	cmd.Env = append(os.Environ(),
		"GIT_COMMITTER_DATE="+dateStr,
		"GIT_AUTHOR_DATE="+dateStr,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}
	// Push whatever branch is checked out to origin/main.
	runGit(projectDir, "push", "origin", "HEAD:main")

	if err := os.MkdirAll(filepath.Join(projectDir, ".harmonik", "events"), 0o755); err != nil {
		t.Fatalf("mkdir events: %v", err)
	}
	return projectDir
}

// TestGovernor_HeadAdvance_InWindow_IncreasesScore verifies that a commit on
// origin/main whose committer date falls within the scoring window contributes
// to HeadAdvanceCount and the movement score.
// This was the 0-coverage path identified in the BT1 audit.
func TestGovernor_HeadAdvance_InWindow_IncreasesScore(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	now := time.Now()
	projectDir := setupGitRepoWithCommit(t, now.Add(-5*time.Minute))
	eventsPath := filepath.Join(projectDir, ".harmonik", "events", "events.jsonl")

	sample := sentinel.ComputeWindowMovement(
		context.Background(),
		eventsPath,
		now.Add(-30*time.Minute),
		now,
		sentinel.DefaultWeights,
		"git",
		projectDir,
	)

	if sample.HeadAdvanceCount < 1 {
		t.Errorf("expected >= 1 HEAD advance for commit within window; got HeadAdvanceCount=%d", sample.HeadAdvanceCount)
	}
	if sample.MovementScore < sentinel.DefaultHighWeight {
		t.Errorf("HEAD advance should contribute >= %d to score; got score=%d",
			sentinel.DefaultHighWeight, sample.MovementScore)
	}
}

// TestGovernor_HeadAdvance_OutsideWindow_NotCounted verifies that a commit
// whose committer date lies outside the scoring window is not counted.
// Boundary case: commit at windowStart-15m vs window of 30m.
func TestGovernor_HeadAdvance_OutsideWindow_NotCounted(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	now := time.Now()
	projectDir := setupGitRepoWithCommit(t, now.Add(-45*time.Minute)) // outside 30m window
	eventsPath := filepath.Join(projectDir, ".harmonik", "events", "events.jsonl")

	sample := sentinel.ComputeWindowMovement(
		context.Background(),
		eventsPath,
		now.Add(-30*time.Minute),
		now,
		sentinel.DefaultWeights,
		"git",
		projectDir,
	)

	if sample.HeadAdvanceCount != 0 {
		t.Errorf("commit outside window must not be counted; got HeadAdvanceCount=%d", sample.HeadAdvanceCount)
	}
	if sample.MovementScore != 0 {
		t.Errorf("commit outside window must not add to score; got MovementScore=%d", sample.MovementScore)
	}
}

// TestGovernor_Weight0Chatter_PopulatedFile verifies that a populated
// events.jsonl containing only weight-0 "chatter" events (non-terminal:
// run_started, state_entered, hook_fired, etc.) produces zero movement score
// and zero terminal event count.
//
// Spec §1.1: only bead_closed, run_completed, and reviewer_verdict{APPROVE}
// carry weight; all other event types carry 0.
func TestGovernor_Weight0Chatter_PopulatedFile(t *testing.T) {
	projectDir := makeEventsFile(t)
	eventsPath := filepath.Join(projectDir, ".harmonik", "events", "events.jsonl")
	now := time.Now()

	// Write 20 weight-0 chatter events across five non-terminal types.
	chatterTypes := []core.EventType{
		core.EventTypeRunStarted,
		core.EventTypeStateEntered,
		core.EventTypeStateExited,
		core.EventTypeHookFired,
		core.EventTypeTransitionEvent,
	}
	for i, et := range chatterTypes {
		for j := 0; j < 4; j++ {
			writeEvent(t, eventsPath, et,
				now.Add(-time.Duration(i*4+j)*time.Minute),
				json.RawMessage(`{}`))
		}
	}

	// Also write a reviewer_verdict{REQUEST_CHANGES} — weight-0 per spec §1.1.
	rcPayload, _ := json.Marshal(map[string]interface{}{
		"verdict":        "REQUEST_CHANGES",
		"schema_version": 1,
		"flags":          []string{},
		"notes":          "needs work",
	})
	writeEvent(t, eventsPath, core.EventTypeReviewerVerdict, now.Add(-1*time.Minute), rcPayload)

	sample := sentinel.ComputeWindowMovement(
		context.Background(),
		eventsPath,
		now.Add(-30*time.Minute),
		now,
		sentinel.DefaultWeights,
		"",
		"",
	)

	if sample.MovementScore != 0 {
		t.Errorf("weight-0 chatter must not contribute to score; got MovementScore=%d", sample.MovementScore)
	}
	if sample.TerminalEventCount != 0 {
		t.Errorf("weight-0 events must not be counted as terminal; got TerminalEventCount=%d", sample.TerminalEventCount)
	}
}

// TestGovernor_BoundaryEquality_AtHighThreshold_Dormant verifies that a movement
// score exactly equal to highThreshold produces ActivationDormant.
// The staircase uses >=, so the boundary itself must be DORMANT, not WATCHING.
func TestGovernor_BoundaryEquality_AtHighThreshold_Dormant(t *testing.T) {
	projectDir := makeEventsFile(t)
	eventsPath := filepath.Join(projectDir, ".harmonik", "events", "events.jsonl")
	now := time.Now()

	const thresh = 7
	weights := map[core.EventType]int{
		core.EventTypeBeadClosed: thresh, // one event scores exactly thresh
	}
	writeEvent(t, eventsPath, core.EventTypeBeadClosed, now.Add(-1*time.Minute), json.RawMessage(`{}`))

	state := &sentinel.GovernorState{}
	cfg := sentinel.Config{
		Window:        30 * time.Minute,
		WarmupWindow:  0,
		HighThreshold: thresh,
		LowThreshold:  thresh,
		Weights:       weights,
	}
	sig := sentinel.Evaluate(context.Background(), state, sentinel.GovernorInput{
		ProjectDir:    projectDir,
		Now:           now,
		HasReadyBeads: true,
	}, cfg)

	if sig.Level != sentinel.ActivationDormant {
		t.Errorf("score == highThreshold (%d) must be DORMANT (>= boundary); got %s (score=%d)",
			thresh, sig.Level, sig.Sample.MovementScore)
	}
	if sig.Sample.MovementScore != thresh {
		t.Errorf("expected MovementScore=%d; got %d", thresh, sig.Sample.MovementScore)
	}
}

// TestGovernor_BoundaryEquality_JustBelowHigh_NotDormant verifies that a score
// one below highThreshold does NOT produce DORMANT, confirming the strict upper
// boundary of the staircase's high-movement zone.
func TestGovernor_BoundaryEquality_JustBelowHigh_NotDormant(t *testing.T) {
	projectDir := makeEventsFile(t)
	eventsPath := filepath.Join(projectDir, ".harmonik", "events", "events.jsonl")
	now := time.Now()

	const thresh = 7
	weights := map[core.EventType]int{
		core.EventTypeBeadClosed: thresh - 1, // one event scores thresh-1, below boundary
	}
	writeEvent(t, eventsPath, core.EventTypeBeadClosed, now.Add(-1*time.Minute), json.RawMessage(`{}`))

	state := &sentinel.GovernorState{}
	cfg := sentinel.Config{
		Window:        30 * time.Minute,
		WarmupWindow:  0,
		HighThreshold: thresh,
		LowThreshold:  thresh,
		Weights:       weights,
	}
	sig := sentinel.Evaluate(context.Background(), state, sentinel.GovernorInput{
		ProjectDir:    projectDir,
		Now:           now,
		HasReadyBeads: true,
	}, cfg)

	if sig.Level == sentinel.ActivationDormant {
		t.Errorf("score < highThreshold (%d < %d) must NOT be DORMANT; got DORMANT",
			sig.Sample.MovementScore, thresh)
	}
	if sig.Sample.MovementScore != thresh-1 {
		t.Errorf("expected MovementScore=%d; got %d", thresh-1, sig.Sample.MovementScore)
	}
}

// TestGovernor_StaircaseReproducibility verifies that the staircase produces
// identical activation levels when called with identical inputs and state.
// An operator must be able to reproduce the activation level by hand from the
// events window alone (spec §1.2 "DISCRETE activation — a step/staircase
// function; an operator MUST be able to reproduce it by hand").
func TestGovernor_StaircaseReproducibility(t *testing.T) {
	projectDir := makeEventsFile(t)
	now := time.Now()

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

	// Run Evaluate twice from identical initial state; outputs must match.
	state1 := &sentinel.GovernorState{ConsecutiveLowWindows: 1}
	state2 := &sentinel.GovernorState{ConsecutiveLowWindows: 1}

	sig1 := sentinel.Evaluate(context.Background(), state1, input, cfg)
	sig2 := sentinel.Evaluate(context.Background(), state2, input, cfg)

	if sig1.Level != sig2.Level {
		t.Errorf("staircase must be deterministic: same input → same level; got %s vs %s",
			sig1.Level, sig2.Level)
	}
	if sig1.Sample.MovementScore != sig2.Sample.MovementScore {
		t.Errorf("staircase must be deterministic: same input → same score; got %d vs %d",
			sig1.Sample.MovementScore, sig2.Sample.MovementScore)
	}
	if sig1.ConsecutiveLowWindows != sig2.ConsecutiveLowWindows {
		t.Errorf("staircase must be deterministic: same consecutive count; got %d vs %d",
			sig1.ConsecutiveLowWindows, sig2.ConsecutiveLowWindows)
	}
}

// TestGovernor_SingleLowWindow_StaysWatching verifies that a single low window
// does NOT trip the governor when SustainedWindows > 1. Only reaching the
// sustained-windows count escalates to ACTIVE.
// Spec §1.4 "sustained-low gate".
func TestGovernor_SingleLowWindow_StaysWatching(t *testing.T) {
	projectDir := makeEventsFile(t)
	now := time.Now()
	state := &sentinel.GovernorState{}
	cfg := sentinel.Config{
		Window:           30 * time.Minute,
		WarmupWindow:     0,
		SustainedWindows: 3, // need 3 consecutive windows to trip
	}
	input := sentinel.GovernorInput{
		ProjectDir:    projectDir,
		Now:           now,
		HasReadyBeads: true,
	}

	sig := sentinel.Evaluate(context.Background(), state, input, cfg)

	if sig.Level != sentinel.ActivationWatching {
		t.Errorf("single low window (SustainedWindows=%d) should be WATCHING; got %s (consecutive=%d)",
			cfg.SustainedWindows, sig.Level, sig.ConsecutiveLowWindows)
	}
	if sig.ConsecutiveLowWindows != 1 {
		t.Errorf("expected ConsecutiveLowWindows=1 after first low window; got %d",
			sig.ConsecutiveLowWindows)
	}
	if sig.Level == sentinel.ActivationActive {
		t.Error("single low window must not trip (ACTIVE) with SustainedWindows=3")
	}
}

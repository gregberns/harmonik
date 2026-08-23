//go:build scenario

package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/sentinel"
)

func bt4ProjectDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, sub := range []string{
		".harmonik/events",
		".harmonik/decision_acks",
	} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatalf("bt4ProjectDir: mkdir %s: %v", sub, err)
		}
	}
	evPath := filepath.Join(dir, ".harmonik", "events", "events.jsonl")
	if err := os.WriteFile(evPath, nil, 0o644); err != nil {
		t.Fatalf("bt4ProjectDir: create events.jsonl: %v", err)
	}
	return dir
}

func bt4WriteMoveEvent(t *testing.T, projectDir string, evType core.EventType, ts time.Time) {
	t.Helper()
	evPath := filepath.Join(projectDir, ".harmonik", "events", "events.jsonl")
	id := core.EventID(uuid.New())
	ev := core.Event{
		EventID:         id,
		SchemaVersion:   1,
		Type:            evType,
		TimestampWall:   ts,
		SourceSubsystem: "test",
		Payload:         json.RawMessage(`{}`),
	}
	line, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("bt4WriteMoveEvent: marshal: %v", err)
	}
	f, err := os.OpenFile(evPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("bt4WriteMoveEvent: open: %v", err)
	}
	defer func() { _ = f.Close() }()
	if _, werr := f.Write(append(line, '\n')); werr != nil {
		t.Fatalf("bt4WriteMoveEvent: write: %v", werr)
	}
}

func bt4AckFile(t *testing.T, projectDir, ackToken string) map[string]interface{} {
	t.Helper()
	path := filepath.Join(projectDir, ".harmonik", "decision_acks", ackToken)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("bt4AckFile: read %s: %v", path, err)
	}
	var m map[string]interface{}
	if jsonErr := json.Unmarshal(data, &m); jsonErr != nil {
		t.Fatalf("bt4AckFile: parse: %v", jsonErr)
	}
	return m
}

func bt4CountEventType(t *testing.T, projectDir, evType string) int {
	t.Helper()
	evPath := filepath.Join(projectDir, ".harmonik", "events", "events.jsonl")
	data, err := os.ReadFile(evPath)
	if err != nil {
		t.Fatalf("bt4CountEventType: read events.jsonl: %v", err)
	}
	count := 0
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var ev map[string]interface{}
		if jsonErr := json.Unmarshal([]byte(line), &ev); jsonErr != nil {
			continue
		}
		if ev["type"] == evType {
			count++
		}
	}
	return count
}

func bt4BlockerFor(t *testing.T, projectDir string) *DecisionBlocker {
	t.Helper()
	blocker := NewDecisionBlocker()
	if err := LoadDecisionAckState(context.Background(), projectDir, blocker); err != nil {
		t.Fatalf("bt4BlockerFor: LoadDecisionAckState: %v", err)
	}
	return blocker
}

func bt4TripConfig() sentinel.Config {
	return sentinel.Config{
		Window:           30 * time.Minute,
		WarmupWindow:     30 * time.Minute, // satisfied by state.DaemonStartedAt = now-1h
		SustainedWindows: 2,
	}
}

func bt4WarmState(now time.Time) *sentinel.GovernorState {
	return &sentinel.GovernorState{
		DaemonStartedAt: now.Add(-time.Hour),
	}
}

func bt4TripInput(projectDir string, readyBeadIDs []string, now time.Time) sentinel.GovernorInput {
	return sentinel.GovernorInput{
		ProjectDir:    projectDir,
		Now:           now,
		HasReadyBeads: len(readyBeadIDs) > 0,
	}
}

func bt4GitProject(t *testing.T) (projectDir string, pushCommit func(commitTime time.Time)) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	root := t.TempDir()

	bareDir := filepath.Join(root, "origin.git")
	if out, err := exec.Command("git", "init", "--bare", bareDir).CombinedOutput(); err != nil {
		t.Fatalf("bt4GitProject: git init --bare: %v\n%s", err, out)
	}

	projectDir = filepath.Join(root, "project")
	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", projectDir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("bt4GitProject: git %v: %v\n%s", args, err, out)
		}
	}
	if out, err := exec.Command("git", "init", projectDir).CombinedOutput(); err != nil {
		t.Fatalf("bt4GitProject: git init: %v\n%s", err, out)
	}
	runGit("config", "user.email", "bt4@test.local")
	runGit("config", "user.name", "BT4 Test")
	runGit("remote", "add", "origin", bareDir)

	if err := os.WriteFile(filepath.Join(projectDir, "init.txt"), []byte("init"), 0o644); err != nil {
		t.Fatalf("bt4GitProject: write init.txt: %v", err)
	}
	runGit("add", "init.txt")
	runGit("commit", "-m", "init")
	runGit("push", "origin", "HEAD:main")

	for _, sub := range []string{".harmonik/events", ".harmonik/decision_acks"} {
		if err := os.MkdirAll(filepath.Join(projectDir, sub), 0o755); err != nil {
			t.Fatalf("bt4GitProject: mkdir %s: %v", sub, err)
		}
	}
	evPath := filepath.Join(projectDir, ".harmonik", "events", "events.jsonl")
	if err := os.WriteFile(evPath, nil, 0o644); err != nil {
		t.Fatalf("bt4GitProject: create events.jsonl: %v", err)
	}

	counter := 0
	pushCommit = func(commitTime time.Time) {
		counter++
		fname := filepath.Join(projectDir, fmt.Sprintf("w%d.txt", counter))
		if err := os.WriteFile(fname, []byte("work"), 0o644); err != nil {
			t.Fatalf("bt4GitProject pushCommit: write: %v", err)
		}
		runGit("add", fmt.Sprintf("w%d.txt", counter))
		dateStr := commitTime.UTC().Format(time.RFC3339)
		cmd := exec.Command("git", "-C", projectDir, "commit", "-m", fmt.Sprintf("work %d", counter))
		cmd.Env = append(os.Environ(),
			"GIT_COMMITTER_DATE="+dateStr,
			"GIT_AUTHOR_DATE="+dateStr,
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("bt4GitProject pushCommit: git commit: %v\n%s", err, out)
		}
		runGit("push", "origin", "HEAD:main")
	}
	return projectDir, pushCommit
}

// TestScenario_Sentinel_BT4_B9_TripNamesBeadIDs_BlocksAllClear exercises B9:
//
//   - Two consecutive zero-movement windows → governor reaches ActivationActive.
//   - EmitTrip writes the ready bead IDs into the exception reason and into the
//     ack-state file on disk.
//   - LoadDecisionAckState restores the sentinel block; IsQueueBlocked("sentinel")
//     returns true — the dispatch path (all-clear) is structurally closed.
func TestScenario_Sentinel_BT4_B9_TripNamesBeadIDs_BlocksAllClear(t *testing.T) {
	ctx := context.Background()
	projectDir := bt4ProjectDir(t)
	now := time.Date(2026, 1, 1, 14, 0, 0, 0, time.UTC)

	state := bt4WarmState(now)
	cfg := bt4TripConfig()
	readyIDs := []string{"hk-bt4-alpha", "hk-bt4-beta"}
	input := bt4TripInput(projectDir, readyIDs, now)

	sig1 := sentinel.Evaluate(ctx, state, input, cfg)
	if sig1.Level != sentinel.ActivationWatching {
		t.Fatalf("B9: window 1: expected WATCHING, got %s", sig1.Level)
	}
	sig2 := sentinel.Evaluate(ctx, state, input, cfg)
	if sig2.Level != sentinel.ActivationActive {
		t.Fatalf("B9: window 2: expected ACTIVE (trip), got %s (consecutive=%d)",
			sig2.Level, sig2.ConsecutiveLowWindows)
	}
	if sig2.SuppressedBy != "" {
		t.Errorf("B9: trip should not be suppressed; got suppressed_by=%q", sig2.SuppressedBy)
	}

	tok, err := sentinel.EmitTrip(ctx, sentinel.TripInput{
		ProjectDir:   projectDir,
		ReadyBeadIDs: readyIDs,
		Now:          now,
	})
	if err != nil {
		t.Fatalf("B9: EmitTrip: %v", err)
	}
	if tok == "" {
		t.Fatal("B9: EmitTrip returned empty ack_token")
	}

	ack := bt4AckFile(t, projectDir, tok)
	if ack["status"] != "pending" {
		t.Errorf("B9: ack status: got %q, want %q", ack["status"], "pending")
	}
	if ack["subject_kind"] != "queue" {
		t.Errorf("B9: subject_kind: got %q, want %q", ack["subject_kind"], "queue")
	}
	if ack["subject_id"] != "sentinel" {
		t.Errorf("B9: subject_id: got %q, want %q", ack["subject_id"], "sentinel")
	}
	reason, _ := ack["reason"].(string)
	for _, id := range readyIDs {
		if !strings.Contains(reason, id) {
			t.Errorf("B9: ack reason must name ready bead %q; got %q", id, reason)
		}
	}

	if n := bt4CountEventType(t, projectDir, "decision_required"); n != 1 {
		t.Errorf("B9: expected 1 decision_required event; got %d", n)
	}

	blocker := bt4BlockerFor(t, projectDir)
	if !blocker.IsQueueBlocked(sentinelSubjectIDACT) {
		t.Error("B9: IsQueueBlocked(sentinel) must be true after EmitTrip — all-clear is blocked")
	}

	t.Logf("B9 PASS: governor tripped (consecutive=%d), bead IDs named in reason, IsQueueBlocked=true",
		sig2.ConsecutiveLowWindows)
}

// TestScenario_Sentinel_BT4_B10A_BeadClosed_ClearsTrip exercises B10 part A:
// a bead_closed event in the movement window makes Evaluate return DORMANT;
// ClearTrip marks the ack acknowledged; a fresh DecisionBlocker is unblocked.
func TestScenario_Sentinel_BT4_B10A_BeadClosed_ClearsTrip(t *testing.T) {
	ctx := context.Background()
	projectDir := bt4ProjectDir(t)
	now := time.Date(2026, 1, 1, 14, 0, 0, 0, time.UTC)

	state := bt4WarmState(now)
	cfg := bt4TripConfig()
	input := bt4TripInput(projectDir, []string{"hk-bt4-gamma"}, now)

	sentinel.Evaluate(ctx, state, input, cfg)        // window 1: WATCHING
	sig := sentinel.Evaluate(ctx, state, input, cfg) // window 2: ACTIVE
	if sig.Level != sentinel.ActivationActive {
		t.Fatalf("B10A setup: expected ACTIVE, got %s", sig.Level)
	}

	tok, err := sentinel.EmitTrip(ctx, sentinel.TripInput{
		ProjectDir:   projectDir,
		ReadyBeadIDs: []string{"hk-bt4-gamma"},
		Now:          now,
	})
	if err != nil || tok == "" {
		t.Fatalf("B10A setup: EmitTrip: tok=%q err=%v", tok, err)
	}

	if !bt4BlockerFor(t, projectDir).IsQueueBlocked(sentinelSubjectIDACT) {
		t.Fatal("B10A setup: IsQueueBlocked should be true before movement")
	}

	moveTime := now.Add(-1 * time.Minute) // within the 30m window
	bt4WriteMoveEvent(t, projectDir, core.EventTypeBeadClosed, moveTime)

	sigAfter := sentinel.Evaluate(ctx, state, input, cfg)
	if sigAfter.Level != sentinel.ActivationDormant {
		t.Fatalf("B10A: expected DORMANT after bead_closed; got %s (score=%d)",
			sigAfter.Level, sigAfter.Sample.MovementScore)
	}

	clearTime := now.Add(time.Minute)
	if clearErr := sentinel.ClearTrip(ctx, projectDir, tok, clearTime); clearErr != nil {
		t.Fatalf("B10A: ClearTrip: %v", clearErr)
	}

	ack := bt4AckFile(t, projectDir, tok)
	if ack["status"] != "acknowledged" {
		t.Errorf("B10A: ack status after ClearTrip: got %q, want %q", ack["status"], "acknowledged")
	}

	if n := bt4CountEventType(t, projectDir, "decision_acknowledged"); n != 1 {
		t.Errorf("B10A: expected 1 decision_acknowledged event after ClearTrip; got %d", n)
	}

	blocker := bt4BlockerFor(t, projectDir)
	if blocker.IsQueueBlocked(sentinelSubjectIDACT) {
		t.Error("B10A: IsQueueBlocked(sentinel) must be false after ClearTrip — all-clear restored")
	}

	t.Log("B10A PASS: bead_closed → DORMANT → ClearTrip → acknowledged → IsQueueBlocked=false")
}

// TestScenario_Sentinel_BT4_B10B_RunCompleted_ClearsTrip exercises B10 part B:
// run_completed is also a terminal-progress event; it makes Evaluate return DORMANT
// and allows ClearTrip to lift the sentinel block.
func TestScenario_Sentinel_BT4_B10B_RunCompleted_ClearsTrip(t *testing.T) {
	ctx := context.Background()
	projectDir := bt4ProjectDir(t)
	now := time.Date(2026, 1, 1, 15, 0, 0, 0, time.UTC)

	state := bt4WarmState(now)
	cfg := bt4TripConfig()
	input := bt4TripInput(projectDir, []string{"hk-bt4-delta"}, now)

	sentinel.Evaluate(ctx, state, input, cfg)
	if sig := sentinel.Evaluate(ctx, state, input, cfg); sig.Level != sentinel.ActivationActive {
		t.Fatalf("B10B setup: expected ACTIVE, got %s", sig.Level)
	}

	tok, err := sentinel.EmitTrip(ctx, sentinel.TripInput{
		ProjectDir:   projectDir,
		ReadyBeadIDs: []string{"hk-bt4-delta"},
		Now:          now,
	})
	if err != nil || tok == "" {
		t.Fatalf("B10B setup: EmitTrip: tok=%q err=%v", tok, err)
	}

	bt4WriteMoveEvent(t, projectDir, core.EventTypeRunCompleted, now.Add(-2*time.Minute))

	sigAfter := sentinel.Evaluate(ctx, state, input, cfg)
	if sigAfter.Level != sentinel.ActivationDormant {
		t.Fatalf("B10B: expected DORMANT after run_completed; got %s (score=%d)",
			sigAfter.Level, sigAfter.Sample.MovementScore)
	}

	if clearErr := sentinel.ClearTrip(ctx, projectDir, tok, now.Add(time.Minute)); clearErr != nil {
		t.Fatalf("B10B: ClearTrip: %v", clearErr)
	}

	ack := bt4AckFile(t, projectDir, tok)
	if ack["status"] != "acknowledged" {
		t.Errorf("B10B: ack status: got %q, want %q", ack["status"], "acknowledged")
	}

	if bt4BlockerFor(t, projectDir).IsQueueBlocked(sentinelSubjectIDACT) {
		t.Error("B10B: IsQueueBlocked must be false after ClearTrip")
	}

	t.Log("B10B PASS: run_completed → DORMANT → ClearTrip → acknowledged → unblocked")
}

// TestScenario_Sentinel_BT4_B10C_HeadAdvance_ClearsTrip exercises B10 part C:
// a commit on origin/main within the window produces HeadAdvanceCount > 0 →
// DORMANT → ClearTrip → unblocked.
func TestScenario_Sentinel_BT4_B10C_HeadAdvance_ClearsTrip(t *testing.T) {
	ctx := context.Background()
	projectDir, pushCommit := bt4GitProject(t)
	now := time.Now()

	setupNow := now.Add(2 * time.Hour)
	state := bt4WarmState(setupNow)
	cfg := bt4TripConfig()

	setupInput := sentinel.GovernorInput{
		ProjectDir:    projectDir,
		Now:           setupNow,
		HasReadyBeads: true,
	}

	sentinel.Evaluate(ctx, state, setupInput, cfg)
	sig := sentinel.Evaluate(ctx, state, setupInput, cfg)
	if sig.Level != sentinel.ActivationActive {
		t.Fatalf("B10C setup: expected ACTIVE at setupNow, got %s (score=%d)",
			sig.Level, sig.Sample.MovementScore)
	}

	tok, err := sentinel.EmitTrip(ctx, sentinel.TripInput{
		ProjectDir:   projectDir,
		ReadyBeadIDs: []string{"hk-bt4-epsilon"},
		Now:          setupNow,
	})
	if err != nil || tok == "" {
		t.Fatalf("B10C setup: EmitTrip: tok=%q err=%v", tok, err)
	}
	if !bt4BlockerFor(t, projectDir).IsQueueBlocked(sentinelSubjectIDACT) {
		t.Fatal("B10C setup: should be blocked after EmitTrip")
	}

	commitTime := now.Add(-5 * time.Minute)
	pushCommit(commitTime)

	realInput := sentinel.GovernorInput{
		ProjectDir:    projectDir,
		Now:           now,
		HasReadyBeads: true,
	}
	sigAfter := sentinel.Evaluate(ctx, state, realInput, cfg)
	if sigAfter.Sample.HeadAdvanceCount == 0 {
		t.Fatalf("B10C: expected HeadAdvanceCount > 0 after commit within window (score=%d)",
			sigAfter.Sample.MovementScore)
	}
	if sigAfter.Level != sentinel.ActivationDormant {
		t.Fatalf("B10C: expected DORMANT after HEAD advance; got %s (score=%d)",
			sigAfter.Level, sigAfter.Sample.MovementScore)
	}

	if clearErr := sentinel.ClearTrip(ctx, projectDir, tok, now); clearErr != nil {
		t.Fatalf("B10C: ClearTrip: %v", clearErr)
	}

	ack := bt4AckFile(t, projectDir, tok)
	if ack["status"] != "acknowledged" {
		t.Errorf("B10C: ack status: got %q, want %q", ack["status"], "acknowledged")
	}

	if bt4BlockerFor(t, projectDir).IsQueueBlocked(sentinelSubjectIDACT) {
		t.Error("B10C: IsQueueBlocked must be false after ClearTrip")
	}

	t.Logf("B10C PASS: HEAD advance (%d commits) → DORMANT → ClearTrip → unblocked",
		sigAfter.Sample.HeadAdvanceCount)
}

// TestScenario_Sentinel_BT4_B10D_BareSelfAck_DoesNotClear exercises B10 part D:
// writing a decision_acknowledged event directly to events.jsonl — without
// calling ClearTrip — does NOT mark the ack file acknowledged.  The ack FILE
// is the durability authority (EV-043a); events.jsonl is the observational
// record only.  A fresh DecisionBlocker loaded via LoadDecisionAckState remains
// blocked.  Evaluate also still returns ActivationActive (no real movement).
//
// Spec: flywheel-motion.md §2.2 "never bare self-ack". Bead hk-jvul (A8/AC4).
func TestScenario_Sentinel_BT4_B10D_BareSelfAck_DoesNotClear(t *testing.T) {
	ctx := context.Background()
	projectDir := bt4ProjectDir(t)
	now := time.Date(2026, 1, 1, 16, 0, 0, 0, time.UTC)

	state := bt4WarmState(now)
	cfg := bt4TripConfig()
	input := bt4TripInput(projectDir, []string{"hk-bt4-zeta"}, now)

	sentinel.Evaluate(ctx, state, input, cfg)
	if sig := sentinel.Evaluate(ctx, state, input, cfg); sig.Level != sentinel.ActivationActive {
		t.Fatalf("B10D setup: expected ACTIVE, got %s", sig.Level)
	}

	tok, err := sentinel.EmitTrip(ctx, sentinel.TripInput{
		ProjectDir:   projectDir,
		ReadyBeadIDs: []string{"hk-bt4-zeta"},
		Now:          now,
	})
	if err != nil || tok == "" {
		t.Fatalf("B10D setup: EmitTrip: tok=%q err=%v", tok, err)
	}

	evPath := filepath.Join(projectDir, ".harmonik", "events", "events.jsonl")
	fakePayload, _ := json.Marshal(map[string]interface{}{
		"ack_token":  tok,
		"subject":    map[string]interface{}{"kind": "queue", "id": "sentinel"},
		"ack_method": "self_ack",
		"acked_at":   now.UTC().Format(time.RFC3339),
	})
	fakeEvent, _ := json.Marshal(map[string]interface{}{
		"event_id":         "00000000-0000-0000-0000-000000000099",
		"schema_version":   1,
		"type":             "decision_acknowledged",
		"timestamp_wall":   now.UTC().Format(time.RFC3339),
		"source_subsystem": "self_ack_test",
		"payload":          json.RawMessage(fakePayload),
	})
	f, openErr := os.OpenFile(evPath, os.O_APPEND|os.O_WRONLY, 0o644)
	if openErr != nil {
		t.Fatalf("B10D: open events.jsonl: %v", openErr)
	}
	fmt.Fprintf(f, "%s\n", fakeEvent)
	_ = f.Close()

	ack := bt4AckFile(t, projectDir, tok)
	if ack["status"] != "pending" {
		t.Errorf("B10D: bare self-ack MUST NOT change ack file; got status=%q, want %q",
			ack["status"], "pending")
	}

	blocker := bt4BlockerFor(t, projectDir)
	if !blocker.IsQueueBlocked(sentinelSubjectIDACT) {
		t.Error("B10D: IsQueueBlocked must remain true — bare self-ack has no authority over the ack file")
	}

	tok2, emitErr := sentinel.EmitTrip(ctx, sentinel.TripInput{
		ProjectDir:   projectDir,
		ReadyBeadIDs: []string{"hk-bt4-zeta"},
		Now:          now.Add(time.Minute),
	})
	if emitErr != nil {
		t.Fatalf("B10D: second EmitTrip after self-ack: %v", emitErr)
	}
	if tok2 != tok {
		t.Errorf("B10D: EmitTrip after self-ack returned new token %q (want %q) — trip was incorrectly cleared",
			tok2, tok)
	}

	sigAfter := sentinel.Evaluate(ctx, state, input, cfg)
	if sigAfter.Level == sentinel.ActivationDormant {
		t.Errorf("B10D: Evaluate must NOT return DORMANT after bare self-ack; got %s (score=%d)",
			sigAfter.Level, sigAfter.Sample.MovementScore)
	}

	t.Log("B10D PASS: bare self-ack has no effect on ack file, IsQueueBlocked=true, Evaluate still ACTIVE")
}

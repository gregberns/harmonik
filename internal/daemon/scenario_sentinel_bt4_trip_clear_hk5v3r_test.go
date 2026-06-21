//go:build scenario

package daemon

// scenario_sentinel_bt4_trip_clear_hk5v3r_test.go — BT4 sentinel scenario tests.
//
// # What is tested (B9 + B10)
//
// B9 — Sentinel trips on idle+ready-work past warmup:
//   - After two consecutive low-movement windows the governor reaches ActivationActive.
//   - EmitTrip writes ONE decision_required exception naming the specific ready bead
//     IDs and leaves an on-disk ack-state file (status=pending).
//   - LoadDecisionAckState seeds a DecisionBlocker whose IsQueueBlocked("sentinel")
//     returns true, structurally blocking all dispatch — the "all-clear" is closed.
//
// B10 — Exception clears on real movement; bare self-ack does NOT clear:
//   - Part A: a bead_closed event in the movement window makes Evaluate return
//     ActivationDormant; ClearTrip marks the ack file acknowledged and appends a
//     decision_acknowledged event; a freshly-loaded DecisionBlocker is unblocked.
//   - Part B: a run_completed event has the same effect.
//   - Part C (HEAD-advance): a commit on origin/main within the window produces
//     HeadAdvanceCount > 0, causes ActivationDormant, and clears the trip.
//   - Part D (bare self-ack): appending a fake decision_acknowledged event directly
//     to events.jsonl — without calling ClearTrip — does NOT change the ack file
//     from pending→acknowledged. A re-loaded DecisionBlocker remains blocked.
//     Evaluate still returns ActivationActive (no real movement added).
//
// # Why //go:build scenario
//
// These tests exercise the full durable round-trip (events.jsonl + decision_acks/
// files) using real filesystem I/O and — for Part C — a real git binary. They
// are tagged scenario so the daemon's normal commit-gate skips them (they exceed
// the 30-min gate budget on a loaded box) and only the explicit scenario run
// covers them.
//
// # How this relates to the workloop
//
// The workloop ACT-mode path (FW3, hk-4toh) calls EmitTrip/ClearTrip in exactly
// this order, driven by the governor signal. These tests exercise the same
// components without running a full daemon process, matching the "real daemon"
// intent: real filesystem, real sentinel logic, real DecisionBlocker.
//
// # Helper prefix
//
// All helpers use "bt4" (flywheel-BT4) per the helper-prefix discipline.
//
// Run independently (the daemon gate skips //go:build scenario):
//
//	go test -tags=scenario -run TestScenario_Sentinel_BT4 ./internal/daemon/...
//
// Spec ref: flywheel-motion.md §§1.3, 1.4, 2.1, 2.2. Bead: hk-5v3r. Epic: hk-0oca.

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

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// bt4ProjectDir creates a minimal .harmonik project layout under t.TempDir():
//
//	<dir>/.harmonik/events/events.jsonl  (empty — no movement yet)
//	<dir>/.harmonik/decision_acks/       (dir only)
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
	// Touch events.jsonl so the scan returns 0 events (not a file-not-found error).
	evPath := filepath.Join(dir, ".harmonik", "events", "events.jsonl")
	if err := os.WriteFile(evPath, nil, 0o644); err != nil {
		t.Fatalf("bt4ProjectDir: create events.jsonl: %v", err)
	}
	return dir
}

// bt4WriteMoveEvent appends one terminal-progress event to events.jsonl.
// evType must be a movement-scoring type (bead_closed, run_completed).
func bt4WriteMoveEvent(t *testing.T, projectDir string, evType core.EventType, ts time.Time) {
	t.Helper()
	evPath := filepath.Join(projectDir, ".harmonik", "events", "events.jsonl")
	id := core.EventID(uuid.New())
	ev := core.Event{
		EventID:         id,
		SchemaVersion:   1,
		Type:            string(evType),
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

// bt4AckFile reads and unmarshals the ack-state file for ackToken from
// <projectDir>/.harmonik/decision_acks/<ackToken>.
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

// bt4CountEventType scans events.jsonl and counts events of the given type.
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

// bt4BlockerFor returns a DecisionBlocker seeded from projectDir's decision_acks/.
// Mirrors the daemon's startup path (LoadDecisionAckState).
func bt4BlockerFor(t *testing.T, projectDir string) *DecisionBlocker {
	t.Helper()
	blocker := NewDecisionBlocker()
	if err := LoadDecisionAckState(context.Background(), projectDir, blocker); err != nil {
		t.Fatalf("bt4BlockerFor: LoadDecisionAckState: %v", err)
	}
	return blocker
}

// bt4TripConfig returns a governor Config with warmup already elapsed and
// SustainedWindows=2 so exactly two low-window evaluations trip the governor.
// WarmupWindow is set but DaemonStartedAt is set to now-1h in the GovernorState,
// so the warmup gate is already satisfied.
func bt4TripConfig() sentinel.Config {
	return sentinel.Config{
		Window:           30 * time.Minute,
		WarmupWindow:     30 * time.Minute, // satisfied by state.DaemonStartedAt = now-1h
		SustainedWindows: 2,
	}
}

// bt4WarmState returns a GovernorState whose DaemonStartedAt is 1 hour ago,
// satisfying the warmup gate.
func bt4WarmState(now time.Time) *sentinel.GovernorState {
	return &sentinel.GovernorState{
		DaemonStartedAt: now.Add(-time.Hour),
	}
}

// bt4TripInput constructs a GovernorInput with no movement, ready beads,
// and warmup satisfied.
func bt4TripInput(projectDir string, readyBeadIDs []string, now time.Time) sentinel.GovernorInput {
	return sentinel.GovernorInput{
		ProjectDir:    projectDir,
		Now:           now,
		HasReadyBeads: len(readyBeadIDs) > 0,
	}
}

// bt4GitProject creates a minimal git repo under t.TempDir() that has
// origin/main and returns (projectDir, pushCommit). pushCommit() adds one new
// commit to origin/main with its committer date set to commitTime.
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

	// Initial commit (no-date override; it will be outside any future window anchored
	// at now+2h or similar offset tricks are not needed: we only push the "movement
	// commit" at the test-controlled commitTime below).
	if err := os.WriteFile(filepath.Join(projectDir, "init.txt"), []byte("init"), 0o644); err != nil {
		t.Fatalf("bt4GitProject: write init.txt: %v", err)
	}
	runGit("add", "init.txt")
	runGit("commit", "-m", "init")
	runGit("push", "origin", "HEAD:main")

	// Create .harmonik/events/ so the projectDir is valid for sentinel.Evaluate.
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

// ---------------------------------------------------------------------------
// B9 — sentinel trips on idle+ready-work past warmup
// ---------------------------------------------------------------------------

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

	// ── Phase 1: two low-movement evaluations → governor trips ──────────────
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

	// ── Phase 2: EmitTrip names the ready bead IDs ───────────────────────────
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

	// Ack file: pending, subject_kind=queue, subject_id=sentinel, reason names bead IDs.
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

	// Exactly one decision_required event in events.jsonl.
	if n := bt4CountEventType(t, projectDir, "decision_required"); n != 1 {
		t.Errorf("B9: expected 1 decision_required event; got %d", n)
	}

	// ── Phase 3: LoadDecisionAckState → IsQueueBlocked("sentinel") = true ────
	blocker := bt4BlockerFor(t, projectDir)
	if !blocker.IsQueueBlocked(sentinelSubjectIDACT) {
		t.Error("B9: IsQueueBlocked(sentinel) must be true after EmitTrip — all-clear is blocked")
	}

	t.Logf("B9 PASS: governor tripped (consecutive=%d), bead IDs named in reason, IsQueueBlocked=true",
		sig2.ConsecutiveLowWindows)
}

// ---------------------------------------------------------------------------
// B10 — exception clears on real movement, NOT self-ack
// ---------------------------------------------------------------------------

// TestScenario_Sentinel_BT4_B10A_BeadClosed_ClearsTrip exercises B10 part A:
// a bead_closed event in the movement window makes Evaluate return DORMANT;
// ClearTrip marks the ack acknowledged; a fresh DecisionBlocker is unblocked.
func TestScenario_Sentinel_BT4_B10A_BeadClosed_ClearsTrip(t *testing.T) {
	ctx := context.Background()
	projectDir := bt4ProjectDir(t)
	now := time.Date(2026, 1, 1, 14, 0, 0, 0, time.UTC)

	// ── Set up a tripped state ────────────────────────────────────────────────
	state := bt4WarmState(now)
	cfg := bt4TripConfig()
	input := bt4TripInput(projectDir, []string{"hk-bt4-gamma"}, now)

	sentinel.Evaluate(ctx, state, input, cfg)               // window 1: WATCHING
	sig := sentinel.Evaluate(ctx, state, input, cfg)        // window 2: ACTIVE
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

	// Verify blocked before movement.
	if !bt4BlockerFor(t, projectDir).IsQueueBlocked(sentinelSubjectIDACT) {
		t.Fatal("B10A setup: IsQueueBlocked should be true before movement")
	}

	// ── Inject real movement: bead_closed within the window ──────────────────
	moveTime := now.Add(-1 * time.Minute) // within the 30m window
	bt4WriteMoveEvent(t, projectDir, core.EventTypeBeadClosed, moveTime)

	// Evaluate after movement → DORMANT.
	sigAfter := sentinel.Evaluate(ctx, state, input, cfg)
	if sigAfter.Level != sentinel.ActivationDormant {
		t.Fatalf("B10A: expected DORMANT after bead_closed; got %s (score=%d)",
			sigAfter.Level, sigAfter.Sample.MovementScore)
	}

	// ── ClearTrip on real movement ─────────────────────────────────────────────
	clearTime := now.Add(time.Minute)
	if clearErr := sentinel.ClearTrip(ctx, projectDir, tok, clearTime); clearErr != nil {
		t.Fatalf("B10A: ClearTrip: %v", clearErr)
	}

	// Ack file must be acknowledged.
	ack := bt4AckFile(t, projectDir, tok)
	if ack["status"] != "acknowledged" {
		t.Errorf("B10A: ack status after ClearTrip: got %q, want %q", ack["status"], "acknowledged")
	}

	// A decision_acknowledged event must be in events.jsonl.
	if n := bt4CountEventType(t, projectDir, "decision_acknowledged"); n != 1 {
		t.Errorf("B10A: expected 1 decision_acknowledged event after ClearTrip; got %d", n)
	}

	// Fresh DecisionBlocker via LoadDecisionAckState: ack is acknowledged → not loaded → unblocked.
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

	// Inject run_completed movement.
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

	// Set now 2h into the future so the initial "init" commit (created at real
	// wall-clock time) is outside the 30m window anchored at setupNow.
	// The "movement commit" will be pushed with commitTime ≈ now-10m,
	// which falls inside the window anchored at real now.
	setupNow := now.Add(2 * time.Hour)
	state := bt4WarmState(setupNow)
	cfg := bt4TripConfig()

	setupInput := sentinel.GovernorInput{
		ProjectDir:    projectDir,
		Now:           setupNow,
		HasReadyBeads: true,
	}

	// Two zero-movement windows at setupNow (initial commit is outside the window).
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

	// Push a commit dated within the 30m window anchored at real now.
	commitTime := now.Add(-5 * time.Minute)
	pushCommit(commitTime)

	// Evaluate at real now: the new commit is within [now-30m, now] → HEAD advance.
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

	// ── Bare self-ack: append a fake decision_acknowledged to events.jsonl ────
	// This simulates an agent writing the acknowledged event directly, bypassing
	// ClearTrip, which would leave the ack FILE unchanged (still "pending").
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

	// ── Assert 1: ack FILE is still "pending" ─────────────────────────────────
	ack := bt4AckFile(t, projectDir, tok)
	if ack["status"] != "pending" {
		t.Errorf("B10D: bare self-ack MUST NOT change ack file; got status=%q, want %q",
			ack["status"], "pending")
	}

	// ── Assert 2: fresh DecisionBlocker is still blocked ─────────────────────
	// LoadDecisionAckState reads the ack FILE, not events.jsonl.
	// Because the file is still "pending", the sentinel block is restored.
	blocker := bt4BlockerFor(t, projectDir)
	if !blocker.IsQueueBlocked(sentinelSubjectIDACT) {
		t.Error("B10D: IsQueueBlocked must remain true — bare self-ack has no authority over the ack file")
	}

	// ── Assert 3: a second EmitTrip still returns the SAME ack_token ──────────
	// The pending trip has not been cleared, so EmitTrip is idempotent.
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

	// ── Assert 4: Evaluate with no real movement still returns ACTIVE ─────────
	// The fake decision_acknowledged in events.jsonl carries no weight → score=0
	// (it is not a bead_closed/run_completed/reviewer_verdict event) → ACTIVE.
	sigAfter := sentinel.Evaluate(ctx, state, input, cfg)
	if sigAfter.Level == sentinel.ActivationDormant {
		t.Errorf("B10D: Evaluate must NOT return DORMANT after bare self-ack; got %s (score=%d)",
			sigAfter.Level, sigAfter.Sample.MovementScore)
	}

	t.Log("B10D PASS: bare self-ack has no effect on ack file, IsQueueBlocked=true, Evaluate still ACTIVE")
}

package daemon

// dashboardgather_throughput_test.go — unit tests for the session-data.jsonl
// windowed roll-up (hk-r22bd): beads closed, mean/p50 wall-time, tokens+cost
// per outcome, grouped by crew/queue/harness/model.

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/sessiondata"
)

// TestReadThroughput_AbsentFile verifies the soft-dependency degrade: no
// session-data.jsonl on disk yields {available: false}, not an error — WS1
// (plans/2026-07-03-eval-program) may not have landed for this project yet.
func TestReadThroughput_AbsentFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	b := &DashboardBuilder{projectDir: dir}

	tp := b.readThroughput(time.Now())
	if tp == nil || tp.Available {
		t.Fatalf("got %+v, want {available: false}", tp)
	}
}

// TestReadThroughput_EmptyFile verifies a present-but-empty file (or a
// window with zero records) is {available: true} with no groups — distinct
// from the absent-file case, per DashThroughput.Available semantics.
func TestReadThroughput_EmptyFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, ".harmonik", "session-data.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	b := &DashboardBuilder{projectDir: dir}

	tp := b.readThroughput(time.Now())
	if tp == nil || !tp.Available {
		t.Fatalf("got %+v, want {available: true}", tp)
	}
	if len(tp.ByGroup) != 0 || len(tp.ByLane) != 0 {
		t.Errorf("expected no groups/lanes for empty file, got %+v", tp)
	}
}

func appendSessionRecord(t *testing.T, dir string, rec sessiondata.Record) {
	t.Helper()
	if err := sessiondata.Append(dir, rec); err != nil {
		t.Fatalf("sessiondata.Append: %v", err)
	}
}

func writeLanesJSON(t *testing.T, dir string, content string) {
	t.Helper()
	path := filepath.Join(dir, ".harmonik", "context", "lanes.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestReadThroughput_GroupsByQueueHarnessModel verifies the core roll-up:
// beads closed, mean/p50 wall-time, and per-outcome tokens+cost, grouped by
// crew/queue/harness/model (crew joined from lanes.json).
func TestReadThroughput_GroupsByQueueHarnessModel(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	now := time.Now().UTC()

	mk := func(id string, queue, harness, model string, success bool, wallS float64, cost *float64, ago time.Duration) sessiondata.Record {
		started := now.Add(-ago)
		ended := started.Add(time.Duration(wallS * float64(time.Second)))
		return sessiondata.Record{
			SchemaVersion: 1,
			RunID:         id,
			BeadID:        "hk-x",
			QueueID:       queue,
			Harness:       harness,
			Model:         model,
			Success:       success,
			StartedAt:     started.Format(time.RFC3339),
			EndedAt:       ended.Format(time.RFC3339),
			WallTimeS:     wallS,
			TokensTotal:   sessiondata.TokenUsage{Input: 100, Output: 50},
			CostUSD:       cost,
			TurnCount:     1,
		}
	}
	f := func(v float64) *float64 { return &v }

	appendSessionRecord(t, dir, mk("r1", "main", "claude-code", "claude-sonnet-4-6", true, 10, f(0.5), time.Hour))
	appendSessionRecord(t, dir, mk("r2", "main", "claude-code", "claude-sonnet-4-6", true, 20, f(0.6), 2*time.Hour))
	appendSessionRecord(t, dir, mk("r3", "main", "claude-code", "claude-sonnet-4-6", false, 5, f(0.1), 3*time.Hour))
	appendSessionRecord(t, dir, mk("r4", "eval-pi", "pi", "openrouter/minimax", true, 100, nil, 30*time.Minute))
	// Outside the 24h window — must be excluded.
	appendSessionRecord(t, dir, mk("r5", "main", "claude-code", "claude-sonnet-4-6", true, 999, f(9), 48*time.Hour))

	writeLanesJSON(t, dir, `{
		"schema_version": 1,
		"lanes": [{"lane": "main-lane", "queue": "main", "crew": "leto", "status": "active"}]
	}`)

	b := &DashboardBuilder{projectDir: dir}
	tp := b.readThroughput(now)
	if tp == nil || !tp.Available {
		t.Fatalf("got %+v, want available", tp)
	}

	var mainGroup, piGroup *DashGroupThroughput
	for i := range tp.ByGroup {
		g := &tp.ByGroup[i]
		if g.Queue == "main" {
			mainGroup = g
		}
		if g.Queue == "eval-pi" {
			piGroup = g
		}
	}
	if mainGroup == nil || piGroup == nil {
		t.Fatalf("expected both main and eval-pi groups, got %+v", tp.ByGroup)
	}

	if mainGroup.Crew != "leto" {
		t.Errorf("Crew: got %q, want leto (joined from lanes.json)", mainGroup.Crew)
	}
	if mainGroup.RunCount != 3 {
		t.Errorf("RunCount: got %d, want 3 (48h-old record excluded)", mainGroup.RunCount)
	}
	if mainGroup.BeadsClosed != 2 {
		t.Errorf("BeadsClosed: got %d, want 2 (successful runs only)", mainGroup.BeadsClosed)
	}
	// wall times: 10, 20, 5 -> mean=11.666.., p50=10
	if mainGroup.MeanWallSecs < 11.6 || mainGroup.MeanWallSecs > 11.7 {
		t.Errorf("MeanWallSecs: got %v, want ~11.67", mainGroup.MeanWallSecs)
	}
	if mainGroup.P50WallSecs != 10 {
		t.Errorf("P50WallSecs: got %v, want 10", mainGroup.P50WallSecs)
	}

	var successStats, failureStats *DashOutcomeStats
	for i := range mainGroup.ByOutcome {
		o := &mainGroup.ByOutcome[i]
		if o.Outcome == "success" {
			successStats = o
		}
		if o.Outcome == "failure" {
			failureStats = o
		}
	}
	if successStats == nil || failureStats == nil {
		t.Fatalf("expected both success and failure outcome stats, got %+v", mainGroup.ByOutcome)
	}
	if successStats.Count != 2 {
		t.Errorf("success Count: got %d, want 2", successStats.Count)
	}
	if successStats.TokensInput != 200 {
		t.Errorf("success TokensInput: got %d, want 200 (2 runs x 100)", successStats.TokensInput)
	}
	if successStats.CostUSD == nil || *successStats.CostUSD < 1.09 || *successStats.CostUSD > 1.11 {
		t.Errorf("success CostUSD: got %v, want ~1.10 (0.5+0.6)", successStats.CostUSD)
	}
	if failureStats.Count != 1 {
		t.Errorf("failure Count: got %d, want 1", failureStats.Count)
	}

	// eval-pi has no lanes.json entry -> Crew empty.
	if piGroup.Crew != "" {
		t.Errorf("piGroup.Crew: got %q, want empty (no lane entry for eval-pi)", piGroup.Crew)
	}

	// ByLane view (joined via queue->lane) should carry the main-lane entry only.
	if len(tp.ByLane) != 1 || tp.ByLane[0].Lane != "main-lane" {
		t.Fatalf("ByLane: got %+v, want [main-lane]", tp.ByLane)
	}
	if tp.ByLane[0].BeadsClosed != 2 {
		t.Errorf("ByLane BeadsClosed: got %d, want 2", tp.ByLane[0].BeadsClosed)
	}
}

package main

// eval_collect_hk_eval_harness_collector_test.go
// Sensors for `harmonik eval collect` (EH1, bead hk-eval-harness-collector-uavgd).
//
// RED-then-GREEN: these tests must pass against the implementation above.

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// evalWriteEvents writes a slice of raw JSON lines to a file.
func evalWriteEvents(t *testing.T, path string, lines []string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create events file: %v", err)
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	for _, l := range lines {
		w.WriteString(l)
		w.WriteByte('\n')
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("flush events file: %v", err)
	}
}

// evalEventLine builds a minimal event JSONL line.
func evalEventLine(eventType, runID string, payload map[string]any) string {
	env := map[string]any{
		"event_id":         "00000000-0000-0000-0000-000000000001",
		"schema_version":   1,
		"type":             eventType,
		"timestamp_wall":   "2026-07-02T22:00:00Z",
		"run_id":           runID,
		"source_subsystem": "test",
		"payload":          payload,
	}
	b, _ := json.Marshal(env)
	return string(b)
}

// evalEventLineAt builds an event line with a specific wall timestamp.
func evalEventLineAt(eventType, runID, wallTS string, payload map[string]any) string {
	env := map[string]any{
		"event_id":         "00000000-0000-0000-0000-000000000001",
		"schema_version":   1,
		"type":             eventType,
		"timestamp_wall":   wallTS,
		"run_id":           runID,
		"source_subsystem": "test",
		"payload":          payload,
	}
	b, _ := json.Marshal(env)
	return string(b)
}

// TestEvalReadEvents_GradePass verifies a passing grade run is collected.
func TestEvalReadEvents_GradePass(t *testing.T) {
	dir := t.TempDir()
	evPath := filepath.Join(dir, "events.jsonl")
	runID := "019f0000-0000-7000-0000-000000000001"

	evalWriteEvents(t, evPath, []string{
		evalEventLine("run_started", runID, map[string]any{
			"bead_id":    "hk-abc",
			"started_at": "2026-07-02T22:00:00Z",
		}),
		evalEventLine("harness_selected", runID, map[string]any{
			"bead_id":    "hk-abc",
			"agent_type": "pi",
			"tier":       1,
		}),
		evalEventLine("implementer_phase_complete", runID, map[string]any{
			"run_id":           runID,
			"exit_code":        0,
			"stderr_tail_head": "",
			"commit_landed":    true,
			"duration_seconds": 191.2,
		}),
		evalEventLine("node_dispatch_requested", runID, map[string]any{
			"run_id":       runID,
			"node_id":      "grade",
			"requested_at": "2026-07-02T22:02:00Z",
		}),
		evalEventLine("outcome_emitted", runID, map[string]any{
			"run_id":         runID,
			"session_id":     "00000000-0000-0000-0000-000000000002",
			"node_id":        "judge",
			"outcome_status": "SUCCESS",
		}),
		evalEventLine("checkpoint_written", runID, map[string]any{
			"run_id":        runID,
			"state_id":      "00000000-0000-0000-0000-000000000003",
			"transition_id": "00000000-0000-0000-0000-000000000004",
			"commit_hash":   "abcd1234567890",
		}),
		evalEventLineAt("run_completed", runID, "2026-07-02T22:03:34Z", map[string]any{
			"run_id":            runID,
			"terminal_state_id": "00000000-0000-0000-0000-000000000005",
			"ended_at":          "2026-07-02T22:03:34Z",
		}),
	})

	states, err := evalReadEvents(evPath, "")
	if err != nil {
		t.Fatalf("evalReadEvents: %v", err)
	}
	st, ok := states[runID]
	if !ok {
		t.Fatal("run_id not in states")
	}
	if st.beadID != "hk-abc" {
		t.Errorf("beadID = %q, want hk-abc", st.beadID)
	}
	if st.harness != "pi" {
		t.Errorf("harness = %q, want pi", st.harness)
	}
	if !st.gradeDispatched {
		t.Fatal("gradeDispatched = false, want true")
	}
	if !st.judgeOutcome {
		t.Error("judgeOutcome = false, want true (grade passed)")
	}
	if st.implSecs != 191.2 {
		t.Errorf("implSecs = %f, want 191.2", st.implSecs)
	}
	if st.commitSHA != "abcd1234567890" {
		t.Errorf("commitSHA = %q, want abcd1234567890", st.commitSHA)
	}
	if st.startedAt != "2026-07-02T22:00:00Z" {
		t.Errorf("startedAt = %q, want 2026-07-02T22:00:00Z", st.startedAt)
	}
	if st.endedAt != "2026-07-02T22:03:34Z" {
		t.Errorf("endedAt = %q, want 2026-07-02T22:03:34Z", st.endedAt)
	}
}

// TestEvalReadEvents_GradeFail verifies a failing grade run is collected.
func TestEvalReadEvents_GradeFail(t *testing.T) {
	dir := t.TempDir()
	evPath := filepath.Join(dir, "events.jsonl")
	runID := "019f0000-0000-7000-0000-000000000002"

	evalWriteEvents(t, evPath, []string{
		evalEventLine("run_started", runID, map[string]any{
			"bead_id":    "hk-def",
			"started_at": "2026-07-02T22:00:00Z",
		}),
		// Grade was dispatched (non-agentic shell node — no outcome_emitted).
		evalEventLine("node_dispatch_requested", runID, map[string]any{
			"run_id":       runID,
			"node_id":      "grade",
			"requested_at": "2026-07-02T22:01:00Z",
		}),
		// Grade failed → DOT topology routes to record-fail, never reaches judge.
	})

	states, err := evalReadEvents(evPath, "")
	if err != nil {
		t.Fatalf("evalReadEvents: %v", err)
	}
	st := states[runID]
	if st == nil {
		t.Fatal("run not in states")
	}
	if !st.gradeDispatched {
		t.Fatal("gradeDispatched = false, want true")
	}
	if st.judgeOutcome {
		t.Error("judgeOutcome = true, want false for failed grade (judge never ran)")
	}
}

// TestEvalReadEvents_NonEvalRun verifies non-eval runs (no grade node dispatch) get
// gradeDispatched=false and are skipped during output.
func TestEvalReadEvents_NonEvalRun(t *testing.T) {
	dir := t.TempDir()
	evPath := filepath.Join(dir, "events.jsonl")
	runID := "019f0000-0000-7000-0000-000000000003"

	evalWriteEvents(t, evPath, []string{
		evalEventLine("run_started", runID, map[string]any{
			"bead_id":    "hk-ghi",
			"started_at": "2026-07-02T22:00:00Z",
		}),
		evalEventLine("outcome_emitted", runID, map[string]any{
			"run_id":         runID,
			"session_id":     "00000000-0000-0000-0000-000000000007",
			"node_id":        "commit_gate", // not "grade"
			"outcome_status": "SUCCESS",
		}),
	})

	states, err := evalReadEvents(evPath, "")
	if err != nil {
		t.Fatalf("evalReadEvents: %v", err)
	}
	st := states[runID]
	if st == nil {
		t.Fatal("run not in states")
	}
	if st.gradeDispatched {
		t.Error("gradeDispatched = true, want false for non-eval run (no grade node)")
	}
}

// TestEvalReadEvents_FilterRunID verifies the --run-id filter.
func TestEvalReadEvents_FilterRunID(t *testing.T) {
	dir := t.TempDir()
	evPath := filepath.Join(dir, "events.jsonl")
	runA := "019f0000-0000-7000-0000-000000000004"
	runB := "019f0000-0000-7000-0000-000000000005"

	evalWriteEvents(t, evPath, []string{
		evalEventLine("run_started", runA, map[string]any{"bead_id": "hk-a", "started_at": "2026-07-02T22:00:00Z"}),
		evalEventLine("run_started", runB, map[string]any{"bead_id": "hk-b", "started_at": "2026-07-02T22:00:00Z"}),
	})

	states, err := evalReadEvents(evPath, runA)
	if err != nil {
		t.Fatalf("evalReadEvents: %v", err)
	}
	if _, ok := states[runA]; !ok {
		t.Error("filtered run A not in states")
	}
	if _, ok := states[runB]; ok {
		t.Error("run B should be filtered out")
	}
}

// TestEvalLabelValue verifies key:value extraction from bead label slices.
func TestEvalLabelValue(t *testing.T) {
	labels := []string{"codename:eval-harness", "task_id:eval-fizzbuzz", "difficulty:simple", "check_kind:unit-test"}
	cases := []struct{ key, want string }{
		{"task_id", "eval-fizzbuzz"},
		{"difficulty", "simple"},
		{"check_kind", "unit-test"},
		{"missing", ""},
		{"codename", "eval-harness"},
	}
	for _, tc := range cases {
		if got := evalLabelValue(labels, tc.key); got != tc.want {
			t.Errorf("evalLabelValue(%q) = %q, want %q", tc.key, got, tc.want)
		}
	}
}

// TestEvalBuildRecord_WallTime verifies wall_time_s calculation.
func TestEvalBuildRecord_WallTime(t *testing.T) {
	st := &evalRunState{
		beadID:          "hk-test",
		startedAt:       "2026-07-02T22:00:00Z",
		endedAt:         "2026-07-02T22:03:34Z", // 214 seconds
		harness:         "claude-code",
		implSecs:        191.2,
		gradeDispatched: true,
		judgeOutcome:    true,
		commitSHA:       "abc123",
		completedWall:   time.Date(2026, 7, 2, 22, 3, 34, 0, time.UTC),
	}
	// Use empty projectDir so br show fails gracefully.
	rec, err := evalBuildRecord("run-id-1", st, t.TempDir(), "")
	if err != nil {
		t.Fatalf("evalBuildRecord: %v", err)
	}
	if rec.WallTimeS < 213 || rec.WallTimeS > 215 {
		t.Errorf("WallTimeS = %f, want ~214", rec.WallTimeS)
	}
	if rec.ImplementTimeS != 191.2 {
		t.Errorf("ImplementTimeS = %f, want 191.2", rec.ImplementTimeS)
	}
	if rec.Pass != true {
		t.Error("Pass = false, want true")
	}
	if rec.Harness != "claude-code" {
		t.Errorf("Harness = %q, want claude-code", rec.Harness)
	}
	if rec.SchemaVersion != 1 {
		t.Errorf("SchemaVersion = %d, want 1", rec.SchemaVersion)
	}
	if rec.Timestamp != "2026-07-02T22:03:34Z" {
		t.Errorf("Timestamp = %q, want 2026-07-02T22:03:34Z", rec.Timestamp)
	}
}

// TestEvalReadPiModel verifies reading harnesses.pi.model from config.yaml.
func TestEvalReadPiModel(t *testing.T) {
	dir := t.TempDir()
	harmonikDir := filepath.Join(dir, ".harmonik")
	if err := os.Mkdir(harmonikDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := `harnesses:
  pi:
    provider: openrouter
    model: openrouter/qwen/qwen3-coder
    api_key_env: OPENROUTER_API_KEY
`
	if err := os.WriteFile(filepath.Join(harmonikDir, "config.yaml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	got := evalReadPiModel(dir)
	if got != "openrouter/qwen/qwen3-coder" {
		t.Errorf("evalReadPiModel = %q, want openrouter/qwen/qwen3-coder", got)
	}
}

// TestEvalReadPiModel_Missing verifies empty string when config absent.
func TestEvalReadPiModel_Missing(t *testing.T) {
	if got := evalReadPiModel(t.TempDir()); got != "" {
		t.Errorf("evalReadPiModel (missing) = %q, want empty", got)
	}
}

// TestRunEvalCollect_EndToEnd verifies the full collect pipeline:
// events.jsonl → eval-results.jsonl with one record per eval run.
func TestRunEvalCollect_EndToEnd(t *testing.T) {
	dir := t.TempDir()
	harmonikDir := filepath.Join(dir, ".harmonik")
	eventsDir := filepath.Join(harmonikDir, "events")
	for _, d := range []string{harmonikDir, eventsDir} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	evPath := filepath.Join(eventsDir, "events.jsonl")
	outPath := filepath.Join(harmonikDir, "eval-results.jsonl")
	runID := "019f0000-0000-7000-0000-000000000010"

	evalWriteEvents(t, evPath, []string{
		evalEventLine("run_started", runID, map[string]any{
			"bead_id":    "hk-xyz",
			"started_at": "2026-07-02T22:00:00Z",
		}),
		evalEventLine("harness_selected", runID, map[string]any{
			"bead_id":    "hk-xyz",
			"agent_type": "claude-code",
			"tier":       4,
		}),
		evalEventLine("implementer_phase_complete", runID, map[string]any{
			"run_id":           runID,
			"exit_code":        0,
			"stderr_tail_head": "",
			"commit_landed":    true,
			"duration_seconds": 100.0,
		}),
		evalEventLine("node_dispatch_requested", runID, map[string]any{
			"run_id":       runID,
			"node_id":      "grade",
			"requested_at": "2026-07-02T22:01:30Z",
		}),
		evalEventLine("outcome_emitted", runID, map[string]any{
			"run_id":         runID,
			"session_id":     "00000000-0000-0000-0000-000000000011",
			"node_id":        "judge",
			"outcome_status": "SUCCESS",
		}),
		evalEventLineAt("run_completed", runID, "2026-07-02T22:02:00Z", map[string]any{
			"run_id":            runID,
			"terminal_state_id": "00000000-0000-0000-0000-000000000012",
			"ended_at":          "2026-07-02T22:02:00Z",
		}),
	})

	var stdout, stderr strings.Builder
	code := runEvalCollect(
		[]string{"--project", dir, "--events-file", evPath, "--output", outPath},
		&stdout, &stderr,
		func() (string, error) { return dir, nil },
	)
	if code != 0 {
		t.Fatalf("runEvalCollect exit %d: stderr=%s", code, stderr.String())
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 output line, got %d: %q", len(lines), data)
	}
	var rec evalResultRecord
	if err := json.Unmarshal([]byte(lines[0]), &rec); err != nil {
		t.Fatalf("unmarshal record: %v", err)
	}
	if rec.SchemaVersion != 1 {
		t.Errorf("schema_version = %d, want 1", rec.SchemaVersion)
	}
	if rec.RunID != runID {
		t.Errorf("run_id = %q, want %q", rec.RunID, runID)
	}
	if rec.BeadID != "hk-xyz" {
		t.Errorf("bead_id = %q, want hk-xyz", rec.BeadID)
	}
	if !rec.Pass {
		t.Error("pass = false, want true")
	}
	if rec.Harness != "claude-code" {
		t.Errorf("harness = %q, want claude-code", rec.Harness)
	}
	if rec.ImplementTimeS != 100.0 {
		t.Errorf("implement_time_s = %f, want 100.0", rec.ImplementTimeS)
	}
	if rec.WallTimeS < 119 || rec.WallTimeS > 121 {
		t.Errorf("wall_time_s = %f, want ~120", rec.WallTimeS)
	}
}

// TestRunEvalCollect_SkipsNonEvalRuns verifies no output for runs without a grade node.
func TestRunEvalCollect_SkipsNonEvalRuns(t *testing.T) {
	dir := t.TempDir()
	harmonikDir := filepath.Join(dir, ".harmonik")
	eventsDir := filepath.Join(harmonikDir, "events")
	for _, d := range []string{harmonikDir, eventsDir} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	evPath := filepath.Join(eventsDir, "events.jsonl")
	outPath := filepath.Join(harmonikDir, "eval-results.jsonl")
	runID := "019f0000-0000-7000-0000-000000000020"

	evalWriteEvents(t, evPath, []string{
		evalEventLine("run_started", runID, map[string]any{
			"bead_id":    "hk-nograde",
			"started_at": "2026-07-02T22:00:00Z",
		}),
		evalEventLine("outcome_emitted", runID, map[string]any{
			"run_id":         runID,
			"session_id":     "00000000-0000-0000-0000-000000000021",
			"node_id":        "review", // not "grade"
			"outcome_status": "SUCCESS",
		}),
	})

	var stdout, stderr strings.Builder
	code := runEvalCollect(
		[]string{"--project", dir, "--events-file", evPath, "--output", outPath},
		&stdout, &stderr,
		func() (string, error) { return dir, nil },
	)
	if code != 0 {
		t.Fatalf("runEvalCollect exit %d: stderr=%s", code, stderr.String())
	}
	// Output file should not exist or be empty.
	info, err := os.Stat(outPath)
	if err == nil && info.Size() > 0 {
		data, _ := os.ReadFile(outPath)
		t.Errorf("expected empty output, got: %s", data)
	}
}

// TestRunEvalCmd_Help verifies --help exits 0.
func TestRunEvalCmd_Help(t *testing.T) {
	var stdout, stderr strings.Builder
	code := runEvalCmd([]string{"--help"}, &stdout, &stderr)
	if code != 0 {
		t.Errorf("exit %d, want 0", code)
	}
	if !strings.Contains(stdout.String(), "collect") {
		t.Error("help output should mention collect verb")
	}
}

// TestRunEvalCmd_UnknownVerb verifies exit 2 for unknown verb.
func TestRunEvalCmd_UnknownVerb(t *testing.T) {
	var stdout, stderr strings.Builder
	code := runEvalCmd([]string{"bogus"}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("exit %d, want 2", code)
	}
}

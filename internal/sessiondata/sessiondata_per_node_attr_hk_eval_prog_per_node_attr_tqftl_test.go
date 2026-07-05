package sessiondata

// sessiondata_per_node_attr_hk_eval_prog_per_node_attr_tqftl_test.go
//
// Sensors for WS1e: per-node time+token attribution.
// Verifies that buildRunEventData collects node_dispatch_requested events and
// that Collect() uses them to compute per-node WallTimeS and filter transcript
// turns to the correct time window.
//
// Bead: hk-eval-prog-per-node-attr-tqftl

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeEventLines writes raw JSON lines to path, one per line.
func writeEventLines(t *testing.T, path string, lines []string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create events file: %v", err)
	}
	defer f.Close()
	for _, l := range lines {
		f.WriteString(l)
		f.WriteString("\n")
	}
}

// eventLine builds a minimal event JSONL line for the given type, run_id, and payload.
func eventLine(evType, runID string, wallTS string, payload map[string]any) string {
	env := map[string]any{
		"event_id":       "00000000-0000-0000-0000-000000000001",
		"schema_version": 1,
		"type":           evType,
		"timestamp_wall": wallTS,
		"run_id":         runID,
		"payload":        payload,
	}
	b, _ := json.Marshal(env)
	return string(b)
}

// transcriptLine builds a Claude transcript assistant-turn JSONL line.
func transcriptLine(ts string, inputTok, outputTok int64) string {
	entry := map[string]any{
		"type":      "assistant",
		"timestamp": ts,
		"message": map[string]any{
			"model": "claude-sonnet-4-6",
			"usage": map[string]any{
				"input_tokens":  inputTok,
				"output_tokens": outputTok,
			},
		},
	}
	b, _ := json.Marshal(entry)
	return string(b)
}

// TestBuildRunEventData_CollectsNodeDispatch verifies that buildRunEventData
// parses node_dispatch_requested events into NodeDispatchEvents.
func TestBuildRunEventData_CollectsNodeDispatch(t *testing.T) {
	dir := t.TempDir()
	evPath := filepath.Join(dir, "events.jsonl")
	runID := "019f0000-0000-7000-0000-000000000001"

	writeEventLines(t, evPath, []string{
		eventLine("run_started", runID, "2026-07-05T10:00:00Z", map[string]any{
			"bead_id": "hk-test", "started_at": "2026-07-05T10:00:00Z",
		}),
		eventLine("node_dispatch_requested", runID, "2026-07-05T10:00:01Z", map[string]any{
			"run_id": runID, "node_id": "implement", "requested_at": "2026-07-05T10:00:01Z", "origin": "workflow",
		}),
		eventLine("implementer_phase_complete", runID, "2026-07-05T10:03:11Z", map[string]any{
			"run_id": runID, "exit_code": 0, "duration_seconds": 190.5,
		}),
		eventLine("node_dispatch_requested", runID, "2026-07-05T10:03:12Z", map[string]any{
			"run_id": runID, "node_id": "grade", "requested_at": "2026-07-05T10:03:12Z", "origin": "workflow",
		}),
		eventLine("node_dispatch_requested", runID, "2026-07-05T10:03:16Z", map[string]any{
			"run_id": runID, "node_id": "judge", "requested_at": "2026-07-05T10:03:16Z", "origin": "workflow",
		}),
	})

	d, err := buildRunEventData(evPath, runID)
	if err != nil {
		t.Fatalf("buildRunEventData: %v", err)
	}

	if len(d.NodeDispatchEvents) != 3 {
		t.Fatalf("NodeDispatchEvents len = %d, want 3", len(d.NodeDispatchEvents))
	}
	if d.NodeDispatchEvents[0].NodeID != "implement" {
		t.Errorf("events[0].NodeID = %q, want implement", d.NodeDispatchEvents[0].NodeID)
	}
	if d.NodeDispatchEvents[1].NodeID != "grade" {
		t.Errorf("events[1].NodeID = %q, want grade", d.NodeDispatchEvents[1].NodeID)
	}
	if d.NodeDispatchEvents[2].NodeID != "judge" {
		t.Errorf("events[2].NodeID = %q, want judge", d.NodeDispatchEvents[2].NodeID)
	}
}

// TestBuildRunEventData_DeduplicatesNodeID verifies that a retried node_dispatch_requested
// for the same node_id is ignored — only the first dispatch is kept.
func TestBuildRunEventData_DeduplicatesNodeID(t *testing.T) {
	dir := t.TempDir()
	evPath := filepath.Join(dir, "events.jsonl")
	runID := "019f0000-0000-7000-0000-000000000007"

	writeEventLines(t, evPath, []string{
		// First dispatch of implement at T1.
		eventLine("node_dispatch_requested", runID, "2026-07-05T10:00:01Z", map[string]any{
			"run_id": runID, "node_id": "implement", "requested_at": "2026-07-05T10:00:01Z", "origin": "workflow",
		}),
		// Retry of implement at T2 (e.g. reconciliation re-dispatch) — must be ignored.
		eventLine("node_dispatch_requested", runID, "2026-07-05T10:00:30Z", map[string]any{
			"run_id": runID, "node_id": "implement", "requested_at": "2026-07-05T10:00:30Z", "origin": "reconciliation",
		}),
		eventLine("node_dispatch_requested", runID, "2026-07-05T10:03:00Z", map[string]any{
			"run_id": runID, "node_id": "grade", "requested_at": "2026-07-05T10:03:00Z", "origin": "workflow",
		}),
	})

	d, err := buildRunEventData(evPath, runID)
	if err != nil {
		t.Fatalf("buildRunEventData: %v", err)
	}
	if len(d.NodeDispatchEvents) != 2 {
		t.Fatalf("NodeDispatchEvents len = %d, want 2 (retry deduplicated)", len(d.NodeDispatchEvents))
	}
	if d.NodeDispatchEvents[0].NodeID != "implement" {
		t.Errorf("events[0].NodeID = %q, want implement", d.NodeDispatchEvents[0].NodeID)
	}
	// First occurrence must be kept (T1), not the retry (T2).
	wantT1 := time.Date(2026, 7, 5, 10, 0, 1, 0, time.UTC)
	if !d.NodeDispatchEvents[0].RequestedAt.Equal(wantT1) {
		t.Errorf("events[0].RequestedAt = %v, want %v", d.NodeDispatchEvents[0].RequestedAt, wantT1)
	}
	if d.NodeDispatchEvents[1].NodeID != "grade" {
		t.Errorf("events[1].NodeID = %q, want grade", d.NodeDispatchEvents[1].NodeID)
	}
}

// TestBuildRunEventData_FiltersByRunID verifies events for other run_ids are ignored.
func TestBuildRunEventData_FiltersByRunID(t *testing.T) {
	dir := t.TempDir()
	evPath := filepath.Join(dir, "events.jsonl")
	runA := "019f0000-0000-7000-0000-000000000002"
	runB := "019f0000-0000-7000-0000-000000000003"

	writeEventLines(t, evPath, []string{
		eventLine("node_dispatch_requested", runA, "2026-07-05T10:00:01Z", map[string]any{
			"run_id": runA, "node_id": "implement", "requested_at": "2026-07-05T10:00:01Z", "origin": "workflow",
		}),
		eventLine("node_dispatch_requested", runB, "2026-07-05T10:00:02Z", map[string]any{
			"run_id": runB, "node_id": "implement", "requested_at": "2026-07-05T10:00:02Z", "origin": "workflow",
		}),
	})

	d, err := buildRunEventData(evPath, runA)
	if err != nil {
		t.Fatalf("buildRunEventData: %v", err)
	}
	if len(d.NodeDispatchEvents) != 1 {
		t.Fatalf("NodeDispatchEvents len = %d, want 1 (filtered to runA only)", len(d.NodeDispatchEvents))
	}
	if d.NodeDispatchEvents[0].NodeID != "implement" {
		t.Errorf("NodeID = %q, want implement", d.NodeDispatchEvents[0].NodeID)
	}
}

// TestCollect_PerNodeWallTimeS verifies that when node_dispatch_requested events are
// present, NodeRecord.WallTimeS is computed from the dispatch time windows.
func TestCollect_PerNodeWallTimeS(t *testing.T) {
	dir := t.TempDir()
	harmonikDir := filepath.Join(dir, ".harmonik")
	eventsDir := filepath.Join(harmonikDir, "events")
	for _, d := range []string{harmonikDir, eventsDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	evPath := filepath.Join(eventsDir, "events.jsonl")

	// Write a transcript for the "implement" node.
	transcriptPath := filepath.Join(dir, "transcript.jsonl")
	writeEventLines(t, transcriptPath, []string{
		// One turn at 10:00:10 — inside the implement window [10:00:01, 10:03:12).
		transcriptLine("2026-07-05T10:00:10Z", 1000, 200),
	})

	runID := "019f0000-0000-7000-0000-000000000004"
	t0 := time.Date(2026, 7, 5, 10, 0, 0, 0, time.UTC)
	t4 := time.Date(2026, 7, 5, 10, 3, 36, 0, time.UTC)

	writeEventLines(t, evPath, []string{
		eventLine("run_started", runID, "2026-07-05T10:00:00Z", map[string]any{
			"bead_id": "hk-ws1e", "started_at": "2026-07-05T10:00:00Z",
		}),
		// session_log_location for "implement" → our transcript.
		eventLine("session_log_location", runID, "2026-07-05T10:00:05Z", map[string]any{
			"run_id":     runID,
			"session_id": "00000000-0000-0000-0000-000000000010",
			"node_id":    "implement",
			"agent_type": "claude-code",
			"log_path":   transcriptPath,
			"log_format": "claude-jsonl",
		}),
		eventLine("node_dispatch_requested", runID, "2026-07-05T10:00:01Z", map[string]any{
			"run_id": runID, "node_id": "implement", "requested_at": "2026-07-05T10:00:01Z", "origin": "workflow",
		}),
		eventLine("node_dispatch_requested", runID, "2026-07-05T10:03:12Z", map[string]any{
			"run_id": runID, "node_id": "grade", "requested_at": "2026-07-05T10:03:12Z", "origin": "workflow",
		}),
		eventLine("node_dispatch_requested", runID, "2026-07-05T10:03:16Z", map[string]any{
			"run_id": runID, "node_id": "judge", "requested_at": "2026-07-05T10:03:16Z", "origin": "workflow",
		}),
	})

	err := Collect(CollectParams{
		RunID:             runID,
		BeadID:            "hk-ws1e",
		Success:           true,
		StartedAt:         t0,
		EndedAt:           t4,
		ProjectDir:        dir,
		ClaudeProjectsDir: dir, // not used; transcript resolved directly
	})
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	recs, err := ReadAll(dir, "", "")
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("ReadAll: got %d records, want 1", len(recs))
	}
	rec := recs[0]

	if len(rec.Nodes) != 3 {
		t.Fatalf("nodes len = %d, want 3 (implement, grade, judge)", len(rec.Nodes))
	}

	// implement: window [10:00:01, 10:03:12) = 191 seconds.
	impl := rec.Nodes[0]
	if impl.NodeID != "implement" {
		t.Errorf("nodes[0].node_id = %q, want implement", impl.NodeID)
	}
	// implement window: 10:03:12 - 10:00:01 = 3min 11s = 191s.
	const wantImplWall = 191.0
	if impl.WallTimeS < wantImplWall-0.5 || impl.WallTimeS > wantImplWall+0.5 {
		t.Errorf("implement WallTimeS = %f, want ~%f", impl.WallTimeS, wantImplWall)
	}
	if impl.Tokens == nil {
		t.Fatal("implement.Tokens = nil, want non-nil (transcript turn inside window)")
	}
	if impl.Tokens.Input != 1000 {
		t.Errorf("implement.Tokens.Input = %d, want 1000", impl.Tokens.Input)
	}

	// grade: non-agentic, no tokens.
	grade := rec.Nodes[1]
	if grade.NodeID != "grade" {
		t.Errorf("nodes[1].node_id = %q, want grade", grade.NodeID)
	}
	if grade.Tokens != nil {
		t.Error("grade.Tokens should be nil (non-agentic shell node)")
	}
	// grade window: [10:03:12, 10:03:16) = 4 seconds.
	if grade.WallTimeS < 3.5 || grade.WallTimeS > 4.5 {
		t.Errorf("grade WallTimeS = %f, want ~4", grade.WallTimeS)
	}

	// judge: non-agentic in this test (no session_log_location).
	judge := rec.Nodes[2]
	if judge.NodeID != "judge" {
		t.Errorf("nodes[2].node_id = %q, want judge", judge.NodeID)
	}
	// judge window: [10:03:16, 10:03:36) = 20 seconds.
	if judge.WallTimeS < 19.5 || judge.WallTimeS > 20.5 {
		t.Errorf("judge WallTimeS = %f, want ~20", judge.WallTimeS)
	}
}

// TestCollect_TimestampFiltersTranscriptTurns verifies that transcript turns
// outside a node's dispatch window are excluded from that node's token count
// and attributed to the correct node via window matching.
func TestCollect_TimestampFiltersTranscriptTurns(t *testing.T) {
	dir := t.TempDir()
	harmonikDir := filepath.Join(dir, ".harmonik")
	eventsDir := filepath.Join(harmonikDir, "events")
	for _, d := range []string{harmonikDir, eventsDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	evPath := filepath.Join(eventsDir, "events.jsonl")

	// Transcript with three turns: one per node window.
	// implement window: [10:00:00, 10:02:00)
	// judge window:     [10:02:00, 10:03:00)
	transcriptPath := filepath.Join(dir, "transcript.jsonl")
	writeEventLines(t, transcriptPath, []string{
		transcriptLine("2026-07-05T10:00:30Z", 500, 100), // implement window
		transcriptLine("2026-07-05T10:01:00Z", 300, 50),  // implement window
		transcriptLine("2026-07-05T10:02:10Z", 800, 150), // judge window
	})

	runID := "019f0000-0000-7000-0000-000000000005"
	t0 := time.Date(2026, 7, 5, 10, 0, 0, 0, time.UTC)
	tEnd := time.Date(2026, 7, 5, 10, 3, 0, 0, time.UTC)

	// Both implement and judge share the same transcript (edge-case: same file
	// listed twice with different node_ids — simulates a resumed Claude session).
	writeEventLines(t, evPath, []string{
		eventLine("run_started", runID, "2026-07-05T10:00:00Z", map[string]any{
			"bead_id": "hk-ws1e-split", "started_at": "2026-07-05T10:00:00Z",
		}),
		eventLine("session_log_location", runID, "2026-07-05T10:00:05Z", map[string]any{
			"run_id":     runID,
			"session_id": "00000000-0000-0000-0000-000000000020",
			"node_id":    "implement",
			"agent_type": "claude-code",
			"log_path":   transcriptPath,
			"log_format": "claude-jsonl",
		}),
		eventLine("node_dispatch_requested", runID, "2026-07-05T10:00:00Z", map[string]any{
			"run_id": runID, "node_id": "implement", "requested_at": "2026-07-05T10:00:00Z", "origin": "workflow",
		}),
		eventLine("node_dispatch_requested", runID, "2026-07-05T10:02:00Z", map[string]any{
			"run_id": runID, "node_id": "judge", "requested_at": "2026-07-05T10:02:00Z", "origin": "workflow",
		}),
	})

	err := Collect(CollectParams{
		RunID:      runID,
		BeadID:     "hk-ws1e-split",
		Success:    true,
		StartedAt:  t0,
		EndedAt:    tEnd,
		ProjectDir: dir,
	})
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	recs, err := ReadAll(dir, "", "")
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("ReadAll: got %d records, want 1", len(recs))
	}
	rec := recs[0]

	if len(rec.Nodes) != 2 {
		t.Fatalf("nodes len = %d, want 2 (implement, judge)", len(rec.Nodes))
	}

	impl := rec.Nodes[0]
	if impl.NodeID != "implement" {
		t.Errorf("nodes[0].node_id = %q, want implement", impl.NodeID)
	}
	if impl.Tokens == nil {
		t.Fatal("implement.Tokens nil")
	}
	// Two turns in implement window: 500+300=800 input, 100+50=150 output.
	if impl.Tokens.Input != 800 {
		t.Errorf("implement input = %d, want 800", impl.Tokens.Input)
	}
	if impl.Tokens.Output != 150 {
		t.Errorf("implement output = %d, want 150", impl.Tokens.Output)
	}

	judge := rec.Nodes[1]
	if judge.NodeID != "judge" {
		t.Errorf("nodes[1].node_id = %q, want judge", judge.NodeID)
	}
	// judge has no session_log_location in this test, so tokens nil.
	if judge.Tokens != nil {
		t.Error("judge.Tokens should be nil (no session_log_location for judge)")
	}
}

// TestCollect_NoDispatchEvents_FallsThrough verifies that the existing behavior
// is preserved when there are no node_dispatch_requested events (non-DOT runs).
func TestCollect_NoDispatchEvents_FallsThrough(t *testing.T) {
	dir := t.TempDir()
	harmonikDir := filepath.Join(dir, ".harmonik")
	eventsDir := filepath.Join(harmonikDir, "events")
	for _, d := range []string{harmonikDir, eventsDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	evPath := filepath.Join(eventsDir, "events.jsonl")

	transcriptPath := filepath.Join(dir, "transcript.jsonl")
	writeEventLines(t, transcriptPath, []string{
		// No timestamps — should still be counted in the non-DOT path.
		func() string {
			entry := map[string]any{
				"type": "assistant",
				"message": map[string]any{
					"model": "claude-sonnet-4-6",
					"usage": map[string]any{
						"input_tokens":  int64(2000),
						"output_tokens": int64(400),
					},
				},
			}
			b, _ := json.Marshal(entry)
			return string(b)
		}(),
	})

	runID := "019f0000-0000-7000-0000-000000000006"
	t0 := time.Date(2026, 7, 5, 11, 0, 0, 0, time.UTC)
	tEnd := time.Date(2026, 7, 5, 11, 5, 0, 0, time.UTC)

	writeEventLines(t, evPath, []string{
		eventLine("run_started", runID, "2026-07-05T11:00:00Z", map[string]any{
			"bead_id": "hk-fallthrough", "started_at": "2026-07-05T11:00:00Z",
		}),
		eventLine("session_log_location", runID, "2026-07-05T11:00:05Z", map[string]any{
			"run_id":     runID,
			"session_id": "00000000-0000-0000-0000-000000000030",
			"node_id":    "implement",
			"agent_type": "claude-code",
			"log_path":   transcriptPath,
			"log_format": "claude-jsonl",
		}),
		// No node_dispatch_requested events.
	})

	err := Collect(CollectParams{
		RunID:      runID,
		BeadID:     "hk-fallthrough",
		Success:    true,
		StartedAt:  t0,
		EndedAt:    tEnd,
		ProjectDir: dir,
	})
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	recs, err := ReadAll(dir, "", "")
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("ReadAll: got %d records, want 1", len(recs))
	}
	rec := recs[0]

	if len(rec.Nodes) != 1 {
		t.Fatalf("nodes len = %d, want 1", len(rec.Nodes))
	}
	if rec.Nodes[0].NodeID != "implement" {
		t.Errorf("nodes[0].node_id = %q, want implement", rec.Nodes[0].NodeID)
	}
	if rec.Nodes[0].Tokens == nil {
		t.Fatal("tokens nil for non-DOT path")
	}
	if rec.Nodes[0].Tokens.Input != 2000 {
		t.Errorf("tokens.input = %d, want 2000", rec.Nodes[0].Tokens.Input)
	}
	// WallTimeS should be 0 (no dispatch windows, existing path doesn't set it).
	if rec.Nodes[0].WallTimeS != 0 {
		t.Errorf("WallTimeS = %f, want 0 for non-DOT path", rec.Nodes[0].WallTimeS)
	}
}

// TestReadTranscript_ParsesTimestamp verifies that transcript turn timestamps are
// parsed from the top-level "timestamp" field.
func TestReadTranscript_ParsesTimestamp(t *testing.T) {
	dir := t.TempDir()
	transcriptPath := filepath.Join(dir, "transcript.jsonl")
	writeEventLines(t, transcriptPath, []string{
		transcriptLine("2026-07-05T10:00:30.597Z", 100, 20),
		transcriptLine("2026-07-05T10:01:00Z", 200, 40),
	})

	turns, err := readTranscript(transcriptPath)
	if err != nil {
		t.Fatalf("readTranscript: %v", err)
	}
	if len(turns) != 2 {
		t.Fatalf("turns len = %d, want 2", len(turns))
	}
	if turns[0].Timestamp.IsZero() {
		t.Error("turns[0].Timestamp is zero, want 2026-07-05T10:00:30Z")
	}
	wantT0 := time.Date(2026, 7, 5, 10, 0, 30, 597000000, time.UTC)
	if !turns[0].Timestamp.Equal(wantT0) {
		t.Errorf("turns[0].Timestamp = %v, want %v", turns[0].Timestamp, wantT0)
	}
	if turns[1].Timestamp.IsZero() {
		t.Error("turns[1].Timestamp is zero")
	}
}

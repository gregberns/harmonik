package usage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestNormTS verifies timestamp normalization.
func TestNormTS(t *testing.T) {
	cases := []struct{ in, want string }{
		{"2026-06-21T15:00:00Z", "2026-06-21T15:00:00Z"},
		{"2026-06-21T15:00:00+00:00", "2026-06-21T15:00:00Z"},
		{"2026-06-21T15:00:00.123Z", "2026-06-21T15:00:00Z"},
		{"", ""},
	}
	for _, c := range cases {
		got := normTS(c.in)
		if got != c.want {
			t.Errorf("normTS(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestParseSince verifies duration shorthand and ISO parsing.
func TestParseSince(t *testing.T) {
	// Duration shorthand.
	got, err := ParseSince("24h")
	if err != nil {
		t.Fatalf("ParseSince(24h): %v", err)
	}
	if !strings.HasSuffix(got, "Z") {
		t.Errorf("ParseSince(24h) = %q, want Z-suffix", got)
	}
	// Must be roughly 24h ago.
	parsed, _ := time.Parse("2006-01-02T15:04:05Z", got)
	diff := time.Since(parsed)
	if diff < 23*time.Hour || diff > 25*time.Hour {
		t.Errorf("ParseSince(24h) diff=%v, want ~24h", diff)
	}

	// Day shorthand.
	got2, err := ParseSince("1d")
	if err != nil {
		t.Fatalf("ParseSince(1d): %v", err)
	}
	if !strings.HasSuffix(got2, "Z") {
		t.Errorf("ParseSince(1d) = %q, want Z-suffix", got2)
	}

	// ISO passthrough.
	iso := "2026-06-21T15:00:00Z"
	got3, err := ParseSince(iso)
	if err != nil {
		t.Fatalf("ParseSince(iso): %v", err)
	}
	if got3 != iso {
		t.Errorf("ParseSince(%q) = %q, want same", iso, got3)
	}

	// Bad input.
	_, err = ParseSince("notadate")
	if err == nil {
		t.Error("ParseSince(notadate): expected error, got nil")
	}
}

// TestRunAnalysis_NoData verifies that RunAnalysis succeeds even with no data.
func TestRunAnalysis_NoData(t *testing.T) {
	dir := t.TempDir()
	// Create empty events directory.
	evDir := filepath.Join(dir, ".harmonik", "events")
	//nolint:gosec // G301: 0755 matches .harmonik dir conventions; path is t.TempDir()-based.
	if err := os.MkdirAll(evDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// No events file → should not error, just return zero counts.
	cfg := Config{
		Since:             "2026-01-01T00:00:00Z",
		Until:             "2026-01-02T00:00:00Z",
		EventsFile:        filepath.Join(evDir, "events.jsonl"),
		ClaudeProjectsDir: filepath.Join(dir, "projects"),
	}
	result, err := RunAnalysis(cfg)
	if err != nil {
		t.Fatalf("RunAnalysis: %v", err)
	}
	if result.RunCount != 0 || result.BeadCount != 0 {
		t.Errorf("expected empty result, got runs=%d beads=%d", result.RunCount, result.BeadCount)
	}
}

// TestRunAnalysis_WithEvents verifies event parsing and transcript join.
func TestRunAnalysis_WithEvents(t *testing.T) {
	dir := t.TempDir()
	evDir := filepath.Join(dir, ".harmonik", "events")
	//nolint:gosec // G301: 0755 matches .harmonik dir conventions; path is t.TempDir()-based.
	if err := os.MkdirAll(evDir, 0o755); err != nil {
		t.Fatal(err)
	}
	projDir := filepath.Join(dir, "projects")
	sessionDir := filepath.Join(projDir, "-test-main")
	//nolint:gosec // G301: 0755 matches project dir conventions; path is t.TempDir()-based.
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Write a minimal events.jsonl.
	runID := "aabbccdd-0000-0000-0000-000000000001"
	beadID := "hk-test01"
	sessionID := "sess-0001"
	transcriptFile := filepath.Join(sessionDir, sessionID+".jsonl")

	eventsLines := []map[string]any{
		{
			"timestamp_wall": "2026-06-21T12:00:00Z",
			"run_id":         runID,
			"type":           "run_started",
			"payload": map[string]any{
				"bead_id":    beadID,
				"queue_id":   "q1",
				"started_at": "2026-06-21T12:00:00Z",
			},
		},
		{
			"timestamp_wall": "2026-06-21T12:01:00Z",
			"run_id":         runID,
			"type":           "session_log_location",
			"payload": map[string]any{
				"log_path": transcriptFile,
				"node_id":  "implement",
			},
		},
		{
			"timestamp_wall": "2026-06-21T12:30:00Z",
			"run_id":         runID,
			"type":           "run_completed",
			"payload": map[string]any{
				"bead_id":  beadID,
				"ended_at": "2026-06-21T12:30:00Z",
				"success":  true,
			},
		},
	}
	evF, _ := os.Create(filepath.Join(evDir, "events.jsonl"))
	enc := json.NewEncoder(evF)
	for _, ev := range eventsLines {
		_ = enc.Encode(ev)
	}
	evF.Close()

	// Write a transcript with one assistant turn.
	turnLine := map[string]any{
		"type":      "assistant",
		"timestamp": "2026-06-21T12:05:00Z",
		"gitBranch": "run/" + runID,
		"cwd":       "/tmp/work",
		"message": map[string]any{
			"model": "claude-sonnet-4-6",
			"usage": map[string]any{
				"input_tokens":                100,
				"output_tokens":               50,
				"cache_creation_input_tokens": 200,
				"cache_read_input_tokens":     5000,
			},
		},
	}
	tF, _ := os.Create(transcriptFile)
	_ = json.NewEncoder(tF).Encode(turnLine)
	tF.Close()

	cfg := Config{
		Since:             "2026-06-21T11:00:00Z",
		Until:             "2026-06-21T13:00:00Z",
		EventsFile:        filepath.Join(evDir, "events.jsonl"),
		ClaudeProjectsDir: projDir,
	}
	result, err := RunAnalysis(cfg)
	if err != nil {
		t.Fatalf("RunAnalysis: %v", err)
	}
	if result.RunCount != 1 {
		t.Errorf("RunCount = %d, want 1", result.RunCount)
	}
	if result.BeadCount != 1 {
		t.Errorf("BeadCount = %d, want 1", result.BeadCount)
	}
	if result.GlobalUsage.Input != 100 {
		t.Errorf("GlobalUsage.Input = %d, want 100", result.GlobalUsage.Input)
	}
	if result.GlobalUsage.CacheRead != 5000 {
		t.Errorf("GlobalUsage.CacheRead = %d, want 5000", result.GlobalUsage.CacheRead)
	}
	if result.TotalCostUSD <= 0 {
		t.Errorf("TotalCostUSD = %f, want > 0", result.TotalCostUSD)
	}
	if len(result.TopBeads) != 1 || result.TopBeads[0].BeadID != beadID {
		t.Errorf("TopBeads = %+v, want [{%s ...}]", result.TopBeads, beadID)
	}
}

// TestPrintSummary verifies that PrintSummary emits without panic for a zero result.
func TestPrintSummary(t *testing.T) {
	result := &AnalysisResult{
		ByModel: map[string]ModelStat{},
		ByTier:  map[string]TierStat{},
		ByHour:  map[string]HourStat{},
	}
	result.Window.Since = "2026-06-21T00:00:00Z"
	result.Window.Until = "2026-06-22T00:00:00Z"
	var sb strings.Builder
	PrintSummary(result, &sb)
	if !strings.Contains(sb.String(), "HARMONIK TOKEN USAGE ANALYSIS") {
		t.Errorf("PrintSummary output missing header: %q", sb.String())
	}
}

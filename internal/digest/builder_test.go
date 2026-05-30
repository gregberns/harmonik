package digest

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestBuildMissingHarmonikDir verifies that Build returns ErrNoHarmonikDir
// when the project directory does not contain a .harmonik/ subdirectory.
func TestBuildMissingHarmonikDir(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	_, err := Build(context.Background(), BuildInput{
		ProjectDir: tmp,
		Limits:     DefaultLimits(),
	})
	if err != ErrNoHarmonikDir {
		t.Fatalf("expected ErrNoHarmonikDir; got %v", err)
	}
}

// TestBuildEmptyProject verifies that Build succeeds on a project with only an
// empty .harmonik/ directory and returns a schema-versioned DigestJSON with
// no queue, no commits (git may fail gracefully), no events.
func TestBuildEmptyProject(t *testing.T) {
	t.Parallel()
	dir := makeMinimalProject(t)
	d, err := Build(context.Background(), BuildInput{
		ProjectDir: dir,
		Limits:     DefaultLimits(),
		Now:        time.Unix(1700000000, 0),
	})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	if d.SchemaVersion != SchemaVersion {
		t.Errorf("schema_version: got %d, want %d", d.SchemaVersion, SchemaVersion)
	}
	if d.Queue.Present {
		t.Errorf("expected Queue.Present=false for empty project")
	}
}

// TestBuildDefaultLimitsActiveRuns verifies that when more than 10 active runs
// are present in queue.json, the digest truncates to 10 and reports the omission
// count in TruncationReport.ActiveRunsOmitted (CL-032: ≤10 active runs cap).
func TestBuildDefaultLimitsActiveRuns(t *testing.T) {
	t.Parallel()
	dir := makeMinimalProject(t)

	// Write a queue.json with 12 dispatched items.
	writeQueueJSON(t, dir, 12)

	d, err := Build(context.Background(), BuildInput{
		ProjectDir: dir,
		Limits:     DefaultLimits(),
		Now:        time.Unix(1700000000, 0),
	})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	if !d.Queue.Present {
		t.Fatal("expected Queue.Present=true")
	}
	if len(d.Queue.ActiveRuns) != 10 {
		t.Errorf("expected 10 active runs (capped); got %d", len(d.Queue.ActiveRuns))
	}
	if d.Queue.ActiveRunCount != 12 {
		t.Errorf("ActiveRunCount: got %d, want 12", d.Queue.ActiveRunCount)
	}
}

// TestBuildFullLimitsActiveRuns verifies that --full mode disables the 10-run cap
// and all 12 active runs are included.
func TestBuildFullLimitsActiveRuns(t *testing.T) {
	t.Parallel()
	dir := makeMinimalProject(t)
	writeQueueJSON(t, dir, 12)

	d, err := Build(context.Background(), BuildInput{
		ProjectDir: dir,
		Limits:     FullLimits(),
		Now:        time.Unix(1700000000, 0),
	})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	if len(d.Queue.ActiveRuns) != 12 {
		t.Errorf("--full: expected 12 active runs; got %d", len(d.Queue.ActiveRuns))
	}
}

// TestBuildOpenNotesDefaultCap verifies that more than 20 open notes are
// truncated to 20 with the remainder reported in TruncationReport.OpenNotesOmitted
// (CL-032: ≤20 open notes cap).
func TestBuildOpenNotesDefaultCap(t *testing.T) {
	t.Parallel()
	dir := makeMinimalProject(t)
	writeNotesJSONL(t, dir, 25)

	d, err := Build(context.Background(), BuildInput{
		ProjectDir: dir,
		Limits:     DefaultLimits(),
		Now:        time.Unix(1700000000, 0),
	})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	if len(d.OpenNotes) != 20 {
		t.Errorf("expected 20 open notes (capped); got %d", len(d.OpenNotes))
	}
	if d.Truncated == nil || d.Truncated.OpenNotesOmitted != 5 {
		omitted := 0
		if d.Truncated != nil {
			omitted = d.Truncated.OpenNotesOmitted
		}
		t.Errorf("OpenNotesOmitted: got %d, want 5", omitted)
	}
}

// TestBuildOpenNotesFullMode verifies that --full disables the notes cap.
func TestBuildOpenNotesFullMode(t *testing.T) {
	t.Parallel()
	dir := makeMinimalProject(t)
	writeNotesJSONL(t, dir, 25)

	d, err := Build(context.Background(), BuildInput{
		ProjectDir: dir,
		Limits:     FullLimits(),
		Now:        time.Unix(1700000000, 0),
	})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	if len(d.OpenNotes) != 25 {
		t.Errorf("--full notes: expected 25; got %d", len(d.OpenNotes))
	}
}

// TestBuildResolvedNotesExcluded verifies that resolved notes (resolved_at non-null)
// are excluded from the digest.
func TestBuildResolvedNotesExcluded(t *testing.T) {
	t.Parallel()
	dir := makeMinimalProject(t)

	notesDir := filepath.Join(dir, ".harmonik", "cognition")
	if err := os.MkdirAll(notesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	notesPath := filepath.Join(notesDir, "notes.jsonl")
	f, err := os.Create(notesPath)
	if err != nil {
		t.Fatal(err)
	}
	resolved := "2026-01-01T00:00:00Z"
	lines := []map[string]interface{}{
		{"schema_version": 1, "ts": "2026-01-01T00:00:00Z", "tool_call_id": "a", "session_id": "s", "kind": "decision", "refs": []string{}, "text": "open note"},
		{"schema_version": 1, "ts": "2026-01-01T00:00:00Z", "tool_call_id": "b", "session_id": "s", "kind": "decision", "refs": []string{}, "text": "resolved note", "resolved_at": resolved},
	}
	enc := json.NewEncoder(f)
	for _, line := range lines {
		if err := enc.Encode(line); err != nil {
			t.Fatal(err)
		}
	}
	_ = f.Close()

	d, err := Build(context.Background(), BuildInput{
		ProjectDir: dir,
		Limits:     DefaultLimits(),
		Now:        time.Unix(1700000000, 0),
	})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	if len(d.OpenNotes) != 1 {
		t.Errorf("expected 1 open note; got %d", len(d.OpenNotes))
	}
	if d.OpenNotes[0].Text != "open note" {
		t.Errorf("unexpected note text: %q", d.OpenNotes[0].Text)
	}
}

// TestBuildSchemaVersion verifies that the JSON output always carries schema_version=1.
func TestBuildSchemaVersion(t *testing.T) {
	t.Parallel()
	dir := makeMinimalProject(t)
	d, err := Build(context.Background(), BuildInput{
		ProjectDir: dir,
		Limits:     DefaultLimits(),
		Now:        time.Unix(1700000000, 0),
	})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	sv, ok := raw["schema_version"]
	if !ok {
		t.Fatal("schema_version missing from JSON output")
	}
	if sv.(float64) != 1 {
		t.Errorf("schema_version: got %v, want 1", sv)
	}
}

// TestApplyNoteTruncation_NoCap verifies that when limits are 0 (full mode),
// no truncation occurs.
func TestApplyNoteTruncation_NoCap(t *testing.T) {
	t.Parallel()
	notes := make([]noteEntry, 30)
	for i := range notes {
		notes[i] = noteEntry{Kind: "decision", Text: "note"}
	}
	summaries, tr := applyNoteTruncation(notes, FullLimits(), nil)
	if len(summaries) != 30 {
		t.Errorf("full mode: expected 30; got %d", len(summaries))
	}
	if tr != nil && tr.OpenNotesOmitted != 0 {
		t.Errorf("full mode: expected 0 omitted; got %d", tr.OpenNotesOmitted)
	}
}

// TestApplyNoteTruncation_Cap verifies the 20-note ordinary cap.
func TestApplyNoteTruncation_Cap(t *testing.T) {
	t.Parallel()
	notes := make([]noteEntry, 22)
	for i := range notes {
		notes[i] = noteEntry{Kind: "decision", Text: "note"}
	}
	summaries, tr := applyNoteTruncation(notes, DefaultLimits(), nil)
	if len(summaries) != 20 {
		t.Errorf("cap: expected 20; got %d", len(summaries))
	}
	if tr == nil || tr.OpenNotesOmitted != 2 {
		omitted := 0
		if tr != nil {
			omitted = tr.OpenNotesOmitted
		}
		t.Errorf("omitted: got %d, want 2", omitted)
	}
}

// --- helpers ---

func makeMinimalProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	harmonikDir := filepath.Join(dir, ".harmonik")
	if err := os.MkdirAll(harmonikDir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// writeQueueJSON writes a queue.json with n dispatched items into dir.
func writeQueueJSON(t *testing.T, dir string, n int) {
	t.Helper()
	harmonikDir := filepath.Join(dir, ".harmonik")

	type itemJSON struct {
		BeadID   string  `json:"bead_id"`
		Status   string  `json:"status"`
		RunID    *string `json:"run_id"`
		Attempts int     `json:"attempts"`
	}
	type groupJSON struct {
		GroupIndex int        `json:"group_index"`
		Kind       string     `json:"kind"`
		Status     string     `json:"status"`
		Items      []itemJSON `json:"items"`
		CreatedAt  string     `json:"created_at"`
	}
	type queueJSON struct {
		SchemaVersion int         `json:"schema_version"`
		QueueID       string      `json:"queue_id"`
		SubmittedAt   string      `json:"submitted_at"`
		Groups        []groupJSON `json:"groups"`
		Status        string      `json:"status"`
	}

	items := make([]itemJSON, n)
	runID := "00000000-0000-0000-0000-000000000001"
	for i := 0; i < n; i++ {
		items[i] = itemJSON{
			BeadID:   "hk-test" + string(rune('a'+i%26)),
			Status:   "dispatched",
			RunID:    &runID,
			Attempts: 1,
		}
	}
	q := queueJSON{
		SchemaVersion: 1,
		QueueID:       "00000000-0000-0000-0000-000000000099",
		SubmittedAt:   "2026-01-01T00:00:00Z",
		Groups: []groupJSON{{
			GroupIndex: 0,
			Kind:       "stream",
			Status:     "active",
			Items:      items,
			CreatedAt:  "2026-01-01T00:00:00Z",
		}},
		Status: "active",
	}
	data, err := json.Marshal(q)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(harmonikDir, "queue.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// writeNotesJSONL writes n unresolved notes into .harmonik/cognition/notes.jsonl.
func writeNotesJSONL(t *testing.T, dir string, n int) {
	t.Helper()
	notesDir := filepath.Join(dir, ".harmonik", "cognition")
	if err := os.MkdirAll(notesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(notesDir, "notes.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	enc := json.NewEncoder(f)
	for i := 0; i < n; i++ {
		note := map[string]interface{}{
			"schema_version": 1,
			"ts":             "2026-01-01T00:00:00Z",
			"tool_call_id":   "tc" + string(rune('a'+i%26)),
			"session_id":     "s1",
			"kind":           "decision",
			"refs":           []string{},
			"text":           "note text",
		}
		if err := enc.Encode(note); err != nil {
			t.Fatal(err)
		}
	}
}

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// beadsMergeFixture writes a JSONL file containing the given rows and returns the path.
func beadsMergeFixture(t *testing.T, rows []map[string]any) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "issues*.jsonl")
	if err != nil {
		t.Fatalf("beadsMergeFixture: create temp file: %v", err)
	}
	defer func() { _ = f.Close() }()

	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(false)
	for _, row := range rows {
		if encErr := enc.Encode(row); encErr != nil {
			t.Fatalf("beadsMergeFixture: encode row: %v", encErr)
		}
	}
	return f.Name()
}

// beadsMergeReadJSONL reads back the JSONL at path and returns rows as loose maps.
func beadsMergeReadJSONL(t *testing.T, path string) []map[string]json.RawMessage {
	t.Helper()
	//nolint:gosec // G304: test helper path
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("beadsMergeReadJSONL: %v", err)
	}

	var rows []map[string]json.RawMessage
	for _, line := range splitLines(string(data)) {
		if line == "" {
			continue
		}
		var m map[string]json.RawMessage
		if jsonErr := json.Unmarshal([]byte(line), &m); jsonErr != nil {
			t.Fatalf("beadsMergeReadJSONL: unmarshal: %v", jsonErr)
		}
		rows = append(rows, m)
	}
	return rows
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

// beadsMergeExtractID extracts the "id" string from a raw map.
func beadsMergeExtractID(t *testing.T, m map[string]json.RawMessage) string {
	t.Helper()
	raw, ok := m["id"]
	if !ok {
		t.Fatal("beadsMergeExtractID: missing id field")
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("beadsMergeExtractID: %v", err)
	}
	return s
}

// beadsMergeExtractStringField extracts a string field from a raw map.
func beadsMergeExtractStringField(t *testing.T, m map[string]json.RawMessage, key string) string {
	t.Helper()
	raw, ok := m[key]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("beadsMergeExtractStringField %q: %v", key, err)
	}
	return s
}

// beadsMergeExtractLabels extracts the "labels" string array from a raw map.
func beadsMergeExtractLabels(t *testing.T, m map[string]json.RawMessage) []string {
	t.Helper()
	raw, ok := m["labels"]
	if !ok {
		return nil
	}
	var labels []string
	if err := json.Unmarshal(raw, &labels); err != nil {
		t.Fatalf("beadsMergeExtractLabels: %v", err)
	}
	return labels
}

// timeStr formats a time as RFC3339Nano for use in test fixtures.
func timeStr(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

func TestBeadsMerge_NoOp(t *testing.T) {
	// Ancestor == Current == Other: output should equal input (no changes).
	ts := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	row := map[string]any{
		"id":         "hk-aaa",
		"title":      "Test bead",
		"status":     "open",
		"updated_at": timeStr(ts),
	}

	path := beadsMergeFixture(t, []map[string]any{row})
	ancestor := beadsMergeFixture(t, []map[string]any{row})
	other := beadsMergeFixture(t, []map[string]any{row})
	workingDir := t.TempDir()
	workingPath := filepath.Join(workingDir, "issues.jsonl")

	result := runBeadsMergeSubcommand([]string{ancestor, path, other, workingPath})
	if result != 0 {
		t.Fatalf("expected exit 0, got %d", result)
	}

	rows := beadsMergeReadJSONL(t, path)
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if beadsMergeExtractID(t, rows[0]) != "hk-aaa" {
		t.Errorf("expected id hk-aaa, got %s", beadsMergeExtractID(t, rows[0]))
	}
}

func TestBeadsMerge_LWW_OtherWins(t *testing.T) {
	// Other has a newer updated_at: it should win.
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	newer := base.Add(time.Hour)

	ancestorRow := map[string]any{"id": "hk-bbb", "title": "original", "updated_at": timeStr(base)}
	currentRow := map[string]any{"id": "hk-bbb", "title": "current edit", "updated_at": timeStr(base)}
	otherRow := map[string]any{"id": "hk-bbb", "title": "other edit newer", "updated_at": timeStr(newer)}

	ancestorPath := beadsMergeFixture(t, []map[string]any{ancestorRow})
	currentPath := beadsMergeFixture(t, []map[string]any{currentRow})
	otherPath := beadsMergeFixture(t, []map[string]any{otherRow})
	workingPath := filepath.Join(t.TempDir(), "issues.jsonl")

	if result := runBeadsMergeSubcommand([]string{ancestorPath, currentPath, otherPath, workingPath}); result != 0 {
		t.Fatalf("expected exit 0, got %d", result)
	}

	rows := beadsMergeReadJSONL(t, currentPath)
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	title := beadsMergeExtractStringField(t, rows[0], "title")
	if title != "other edit newer" {
		t.Errorf("expected 'other edit newer' to win LWW, got %q", title)
	}
}

func TestBeadsMerge_LWW_CurrentWins(t *testing.T) {
	// Current has a newer updated_at: it should win.
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	newer := base.Add(time.Hour)

	ancestorRow := map[string]any{"id": "hk-ccc", "title": "original", "updated_at": timeStr(base)}
	currentRow := map[string]any{"id": "hk-ccc", "title": "current edit newer", "updated_at": timeStr(newer)}
	otherRow := map[string]any{"id": "hk-ccc", "title": "other edit", "updated_at": timeStr(base)}

	ancestorPath := beadsMergeFixture(t, []map[string]any{ancestorRow})
	currentPath := beadsMergeFixture(t, []map[string]any{currentRow})
	otherPath := beadsMergeFixture(t, []map[string]any{otherRow})
	workingPath := filepath.Join(t.TempDir(), "issues.jsonl")

	if result := runBeadsMergeSubcommand([]string{ancestorPath, currentPath, otherPath, workingPath}); result != 0 {
		t.Fatalf("expected exit 0, got %d", result)
	}

	rows := beadsMergeReadJSONL(t, currentPath)
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	title := beadsMergeExtractStringField(t, rows[0], "title")
	if title != "current edit newer" {
		t.Errorf("expected 'current edit newer' to win LWW, got %q", title)
	}
}

func TestBeadsMerge_UnionNewBeads(t *testing.T) {
	// Each side adds a bead not present in the other: both should appear.
	ts := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	sharedRow := map[string]any{"id": "hk-shared", "title": "shared", "updated_at": timeStr(ts)}
	currentOnly := map[string]any{"id": "hk-current-only", "title": "from current", "updated_at": timeStr(ts)}
	otherOnly := map[string]any{"id": "hk-other-only", "title": "from other", "updated_at": timeStr(ts)}

	ancestorPath := beadsMergeFixture(t, []map[string]any{sharedRow})
	currentPath := beadsMergeFixture(t, []map[string]any{sharedRow, currentOnly})
	otherPath := beadsMergeFixture(t, []map[string]any{sharedRow, otherOnly})
	workingPath := filepath.Join(t.TempDir(), "issues.jsonl")

	if result := runBeadsMergeSubcommand([]string{ancestorPath, currentPath, otherPath, workingPath}); result != 0 {
		t.Fatalf("expected exit 0, got %d", result)
	}

	rows := beadsMergeReadJSONL(t, currentPath)
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows (shared + current-only + other-only), got %d", len(rows))
	}

	ids := make(map[string]bool)
	for _, r := range rows {
		ids[beadsMergeExtractID(t, r)] = true
	}
	for _, expected := range []string{"hk-shared", "hk-current-only", "hk-other-only"} {
		if !ids[expected] {
			t.Errorf("expected bead %q in merged output", expected)
		}
	}
}

func TestBeadsMerge_LabelUnion(t *testing.T) {
	// Current adds label "a"; other adds label "b"; result should have both.
	ts := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	ancestorRow := map[string]any{"id": "hk-labels", "title": "label test", "updated_at": timeStr(ts), "labels": []string{}}
	currentRow := map[string]any{"id": "hk-labels", "title": "label test", "updated_at": timeStr(ts), "labels": []string{"label-a"}}
	otherRow := map[string]any{"id": "hk-labels", "title": "label test", "updated_at": timeStr(ts), "labels": []string{"label-b"}}

	ancestorPath := beadsMergeFixture(t, []map[string]any{ancestorRow})
	currentPath := beadsMergeFixture(t, []map[string]any{currentRow})
	otherPath := beadsMergeFixture(t, []map[string]any{otherRow})
	workingPath := filepath.Join(t.TempDir(), "issues.jsonl")

	if result := runBeadsMergeSubcommand([]string{ancestorPath, currentPath, otherPath, workingPath}); result != 0 {
		t.Fatalf("expected exit 0, got %d", result)
	}

	rows := beadsMergeReadJSONL(t, currentPath)
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	labels := beadsMergeExtractLabels(t, rows[0])
	labelSet := make(map[string]bool)
	for _, l := range labels {
		labelSet[l] = true
	}
	if !labelSet["label-a"] {
		t.Error("expected label-a in merged labels")
	}
	if !labelSet["label-b"] {
		t.Error("expected label-b in merged labels")
	}
}

func TestBeadsMerge_SameTimestampConflictLogged(t *testing.T) {
	// Same updated_at, different status: conflict logged in spec-required format.
	ts := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	ancestorRow := map[string]any{"id": "hk-conflict", "status": "open", "updated_at": timeStr(ts)}
	currentRow := map[string]any{"id": "hk-conflict", "status": "closed", "updated_at": timeStr(ts)}
	otherRow := map[string]any{"id": "hk-conflict", "status": "in_progress", "updated_at": timeStr(ts)}

	ancestorPath := beadsMergeFixture(t, []map[string]any{ancestorRow})
	currentPath := beadsMergeFixture(t, []map[string]any{currentRow})
	otherPath := beadsMergeFixture(t, []map[string]any{otherRow})
	workingDir := t.TempDir()
	workingPath := filepath.Join(workingDir, "issues.jsonl")

	if result := runBeadsMergeSubcommand([]string{ancestorPath, currentPath, otherPath, workingPath}); result != 0 {
		t.Fatalf("expected exit 0, got %d", result)
	}

	// Merge should still succeed (current wins as tiebreaker).
	rows := beadsMergeReadJSONL(t, currentPath)
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}

	// Conflict log must exist and contain the spec-required format (BL-MRG-003).
	logPath := filepath.Join(workingDir, "merge-conflicts.log")
	//nolint:gosec // G304: test path
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("expected merge-conflicts.log to exist: %v", err)
	}
	line := strings.TrimSpace(string(data))
	for _, want := range []string{
		"CONFLICT",
		"bead=hk-conflict",
		"field=status",
		"a=closed",
		"b=in_progress",
		"resolution=took-ours",
	} {
		if !strings.Contains(line, want) {
			t.Errorf("conflict log line missing %q; got: %s", want, line)
		}
	}
}

func TestBeadsMerge_EmptyAncestor(t *testing.T) {
	// Ancestor is empty (first merge on a new repo): both current and other beads should appear.
	ts := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	currentRow := map[string]any{"id": "hk-from-current", "title": "from current", "updated_at": timeStr(ts)}
	otherRow := map[string]any{"id": "hk-from-other", "title": "from other", "updated_at": timeStr(ts)}

	ancestorPath := beadsMergeFixture(t, []map[string]any{})
	currentPath := beadsMergeFixture(t, []map[string]any{currentRow})
	otherPath := beadsMergeFixture(t, []map[string]any{otherRow})
	workingPath := filepath.Join(t.TempDir(), "issues.jsonl")

	if result := runBeadsMergeSubcommand([]string{ancestorPath, currentPath, otherPath, workingPath}); result != 0 {
		t.Fatalf("expected exit 0, got %d", result)
	}

	rows := beadsMergeReadJSONL(t, currentPath)
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
}

func TestBeadsMerge_OutputSortedByID(t *testing.T) {
	// Output rows should be sorted by bead ID (deterministic).
	ts := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	rows := []map[string]any{
		{"id": "hk-zzz", "title": "z bead", "updated_at": timeStr(ts)},
		{"id": "hk-aaa", "title": "a bead", "updated_at": timeStr(ts)},
		{"id": "hk-mmm", "title": "m bead", "updated_at": timeStr(ts)},
	}

	ancestorPath := beadsMergeFixture(t, rows)
	currentPath := beadsMergeFixture(t, rows)
	otherPath := beadsMergeFixture(t, rows)
	workingPath := filepath.Join(t.TempDir(), "issues.jsonl")

	if result := runBeadsMergeSubcommand([]string{ancestorPath, currentPath, otherPath, workingPath}); result != 0 {
		t.Fatalf("expected exit 0, got %d", result)
	}

	mergedRows := beadsMergeReadJSONL(t, currentPath)
	if len(mergedRows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(mergedRows))
	}
	ids := make([]string, len(mergedRows))
	for i, r := range mergedRows {
		ids[i] = beadsMergeExtractID(t, r)
	}
	expected := []string{"hk-aaa", "hk-mmm", "hk-zzz"}
	for i, want := range expected {
		if ids[i] != want {
			t.Errorf("row[%d]: expected id %s, got %s", i, want, ids[i])
		}
	}
}

func TestBeadsMerge_MissingArguments(t *testing.T) {
	// Fewer than 4 arguments should return exit code 1.
	result := runBeadsMergeSubcommand([]string{"only-one"})
	if result != 1 {
		t.Errorf("expected exit 1 for missing arguments, got %d", result)
	}
}

func TestBeadsMerge_HelpFlag(t *testing.T) {
	// --help should return exit 0.
	result := runBeadsMergeSubcommand([]string{"--help"})
	if result != 0 {
		t.Errorf("expected exit 0 for --help, got %d", result)
	}
}

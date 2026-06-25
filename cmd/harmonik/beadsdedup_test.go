package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeJSONLFixture writes a JSONL file containing the given rows and returns
// the path.  It is a thin wrapper over beadsMergeFixture so both test suites
// share the same helper style.
func writeJSONLFixture(t *testing.T, rows []map[string]any) string {
	t.Helper()
	return beadsMergeFixture(t, rows)
}

// readJSONLStatus reads back the "status" string for the single row in a JSONL
// file, failing the test if there is not exactly one row.
func readJSONLStatus(t *testing.T, path string) string {
	t.Helper()
	rows := beadsMergeReadJSONL(t, path)
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	return beadsMergeExtractStringField(t, rows[0], "status")
}

// readJSONLIDs reads all bead IDs from a JSONL file.
func readJSONLIDs(t *testing.T, path string) []string {
	t.Helper()
	rows := beadsMergeReadJSONL(t, path)
	ids := make([]string, len(rows))
	for i, r := range rows {
		ids[i] = beadsMergeExtractID(t, r)
	}
	return ids
}

func TestBeadsDedup_NoDuplicates(t *testing.T) {
	// Unsorted input, no dups: command must still sort and write 2 rows.
	ts := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	path := writeJSONLFixture(t, []map[string]any{
		{"id": "hk-bbb", "status": "open", "updated_at": timeStr(ts)},
		{"id": "hk-aaa", "status": "closed", "updated_at": timeStr(ts)},
	})

	if rc := runBeadsDedupSubcommand([]string{"--path", path}); rc != 0 {
		t.Fatalf("expected exit 0, got %d", rc)
	}

	ids := readJSONLIDs(t, path)
	if len(ids) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(ids))
	}
	if ids[0] != "hk-aaa" || ids[1] != "hk-bbb" {
		t.Errorf("expected sorted output [hk-aaa hk-bbb], got %v", ids)
	}
}

func TestBeadsDedup_OlderOpenThenNewerClosed(t *testing.T) {
	// File order: open (older) then closed (newer).  closed must survive.
	older := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC)
	path := writeJSONLFixture(t, []map[string]any{
		{"id": "hk-abc", "status": "open", "updated_at": timeStr(older)},
		{"id": "hk-abc", "status": "closed", "updated_at": timeStr(newer)},
	})

	if rc := runBeadsDedupSubcommand([]string{"--path", path}); rc != 0 {
		t.Fatalf("expected exit 0, got %d", rc)
	}

	status := readJSONLStatus(t, path)
	if status != "closed" {
		t.Errorf("expected closed to survive, got %q", status)
	}
}

func TestBeadsDedup_NewerClosedThenOlderOpen(t *testing.T) {
	// File order: closed (newer) then open (older).  closed must survive.
	older := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC)
	path := writeJSONLFixture(t, []map[string]any{
		{"id": "hk-abc", "status": "closed", "updated_at": timeStr(newer)},
		{"id": "hk-abc", "status": "open", "updated_at": timeStr(older)},
	})

	if rc := runBeadsDedupSubcommand([]string{"--path", path}); rc != 0 {
		t.Fatalf("expected exit 0, got %d", rc)
	}

	status := readJSONLStatus(t, path)
	if status != "closed" {
		t.Errorf("expected closed to survive, got %q", status)
	}
}

func TestBeadsDedup_MultipleIDsSomeWithDups(t *testing.T) {
	// Mix of unique beads and one duplicated bead.
	older := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC)
	path := writeJSONLFixture(t, []map[string]any{
		{"id": "hk-dup", "status": "open", "updated_at": timeStr(older)},
		{"id": "hk-unique1", "status": "closed", "updated_at": timeStr(older)},
		{"id": "hk-dup", "status": "closed", "updated_at": timeStr(newer)},
		{"id": "hk-unique2", "status": "open", "updated_at": timeStr(older)},
	})

	if rc := runBeadsDedupSubcommand([]string{"--path", path}); rc != 0 {
		t.Fatalf("expected exit 0, got %d", rc)
	}

	rows := beadsMergeReadJSONL(t, path)
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows after dedup, got %d", len(rows))
	}

	byID := make(map[string]map[string]json.RawMessage, len(rows))
	for _, r := range rows {
		id := beadsMergeExtractID(t, r)
		byID[id] = r
	}
	if beadsMergeExtractStringField(t, byID["hk-dup"], "status") != "closed" {
		t.Errorf("hk-dup should have survived as closed")
	}
}

func TestBeadsDedup_OutputSortedByID(t *testing.T) {
	ts := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	path := writeJSONLFixture(t, []map[string]any{
		{"id": "hk-zzz", "updated_at": timeStr(ts)},
		{"id": "hk-aaa", "updated_at": timeStr(ts)},
		{"id": "hk-mmm", "updated_at": timeStr(ts)},
	})

	if rc := runBeadsDedupSubcommand([]string{"--path", path}); rc != 0 {
		t.Fatalf("expected exit 0, got %d", rc)
	}

	ids := readJSONLIDs(t, path)
	expected := []string{"hk-aaa", "hk-mmm", "hk-zzz"}
	for i, want := range expected {
		if ids[i] != want {
			t.Errorf("row[%d]: expected %s, got %s", i, want, ids[i])
		}
	}
}

func TestBeadsDedup_DryRun_DoesNotModify(t *testing.T) {
	older := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC)
	path := writeJSONLFixture(t, []map[string]any{
		{"id": "hk-abc", "status": "open", "updated_at": timeStr(older)},
		{"id": "hk-abc", "status": "closed", "updated_at": timeStr(newer)},
	})

	//nolint:gosec // G304: test file path
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read before: %v", err)
	}

	if rc := runBeadsDedupSubcommand([]string{"--path", path, "--dry-run"}); rc != 0 {
		t.Fatalf("expected exit 0, got %d", rc)
	}

	//nolint:gosec // G304: test file path
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read after: %v", err)
	}

	if string(before) != string(after) {
		t.Error("--dry-run must not modify the file")
	}
}

func TestBeadsDedup_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "issues.jsonl")
	//nolint:gosec // G306: test file
	if err := os.WriteFile(path, []byte{}, 0o644); err != nil {
		t.Fatalf("write empty file: %v", err)
	}

	if rc := runBeadsDedupSubcommand([]string{"--path", path}); rc != 0 {
		t.Fatalf("expected exit 0 on empty file, got %d", rc)
	}
}

func TestBeadsDedup_HelpFlag(t *testing.T) {
	if rc := runBeadsDedupSubcommand([]string{"--help"}); rc != 0 {
		t.Errorf("expected exit 0 for --help, got %d", rc)
	}
}

func TestBeadsDedup_UnknownFlag(t *testing.T) {
	if rc := runBeadsDedupSubcommand([]string{"--unknown-flag"}); rc != 1 {
		t.Errorf("expected exit 1 for unknown flag, got %d", rc)
	}
}

func TestDeduplicateBeadRows_NewestWins(t *testing.T) {
	older := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC)
	rows := []beadRow{
		{id: "hk-x", updatedAt: older},
		{id: "hk-x", updatedAt: newer},
	}
	result := deduplicateBeadRows(rows)
	if len(result) != 1 {
		t.Fatalf("expected 1 row, got %d", len(result))
	}
	if !result[0].updatedAt.Equal(newer) {
		t.Errorf("expected newer timestamp to win")
	}
}

func TestRowsToMap_LWWWithinFile(t *testing.T) {
	// rowsToMap must now keep the newest updated_at, not the last file position.
	older := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC)

	// closed (newer) appears first, open (older) appears second.
	rows := []beadRow{
		{id: "hk-y", updatedAt: newer},
		{id: "hk-y", updatedAt: older},
	}
	m := rowsToMap(rows)
	if got := m["hk-y"].updatedAt; !got.Equal(newer) {
		t.Errorf("rowsToMap: expected newer timestamp (%v) to win, got %v", newer, got)
	}
}

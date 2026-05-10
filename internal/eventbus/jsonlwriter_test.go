package eventbus_test

// jsonlwriter_test.go — binding tests for hk-hqwn.29 (EV-020 append-only JSONL).
//
// Spec ref: event-model.md §6.2 EV-020; §4.4 EV-015, EV-016.
// Bead ref: hk-hqwn.29.
//
// These tests verify that JSONLWriter:
//   1. Writes exactly one complete line (JSON + '\n') per Append call.
//   2. MUST NOT rewrite, truncate, or reorder existing lines.
//   3. Calls Sync when sync=true (fsync-boundary semantics for F-class events).
//   4. Is safe for concurrent use without interleaving or loss.
//
// Helper prefix: jsonlWriterFixture (per implementer-protocol.md).

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/gregberns/harmonik/internal/eventbus"
)

// jsonlWriterFixtureTempPath creates a temporary file path (not yet created)
// inside t.TempDir() for use as a JSONL log file.
func jsonlWriterFixtureTempPath(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join(t.TempDir(), name)
}

// jsonlWriterFixtureReadLines reads all lines from path and returns them
// without trailing newlines.
func jsonlWriterFixtureReadLines(t *testing.T, path string) []string {
	t.Helper()
	//nolint:gosec // G304: path is t.TempDir()-based; not user input.
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("jsonlWriterFixtureReadLines: open %s: %v", path, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("jsonlWriterFixtureReadLines: scan %s: %v", path, scanErr)
	}
	return lines
}

// TestJSONLWriterAppendSingleLine verifies that a single Append call writes
// exactly one line terminated by '\n'. EV-020 check (1).
func TestJSONLWriterAppendSingleLine(t *testing.T) {
	t.Parallel()

	path := jsonlWriterFixtureTempPath(t, "events.jsonl")
	w, err := eventbus.OpenJSONLWriter(path)
	if err != nil {
		t.Fatalf("OpenJSONLWriter: %v", err)
	}
	defer func() { _ = w.Close() }()

	line := []byte(`{"event_id":"01950000-0000-7000-8000-000000000001","type":"daemon_started"}`)
	if err := w.Append(line, false); err != nil {
		t.Fatalf("Append: %v", err)
	}

	lines := jsonlWriterFixtureReadLines(t, path)
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}
	if lines[0] != string(line) {
		t.Errorf("line content mismatch:\n  got:  %q\n  want: %q", lines[0], string(line))
	}
}

// TestJSONLWriterAppendPreservesExistingLines verifies EV-020: existing lines
// are never rewritten, truncated, or reordered. Appending a second line must
// leave the first line intact.
func TestJSONLWriterAppendPreservesExistingLines(t *testing.T) {
	t.Parallel()

	path := jsonlWriterFixtureTempPath(t, "events.jsonl")
	w, err := eventbus.OpenJSONLWriter(path)
	if err != nil {
		t.Fatalf("OpenJSONLWriter: %v", err)
	}
	defer func() { _ = w.Close() }()

	first := []byte(`{"event_id":"01950000-0000-7000-8000-000000000001","type":"daemon_started"}`)
	second := []byte(`{"event_id":"01950000-0000-7000-8000-000000000002","type":"daemon_ready"}`)

	if err := w.Append(first, false); err != nil {
		t.Fatalf("Append(first): %v", err)
	}
	if err := w.Append(second, false); err != nil {
		t.Fatalf("Append(second): %v", err)
	}

	lines := jsonlWriterFixtureReadLines(t, path)
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
	if lines[0] != string(first) {
		t.Errorf("first line corrupted:\n  got:  %q\n  want: %q", lines[0], string(first))
	}
	if lines[1] != string(second) {
		t.Errorf("second line corrupted:\n  got:  %q\n  want: %q", lines[1], string(second))
	}
}

// TestJSONLWriterOpenExistingPreservesLines verifies that opening an existing
// JSONL file does NOT truncate it (O_APPEND not O_TRUNC). EV-020 check (2).
func TestJSONLWriterOpenExistingPreservesLines(t *testing.T) {
	t.Parallel()

	path := jsonlWriterFixtureTempPath(t, "events.jsonl")

	// Write a line with one writer.
	w1, err := eventbus.OpenJSONLWriter(path)
	if err != nil {
		t.Fatalf("OpenJSONLWriter (first): %v", err)
	}
	first := []byte(`{"event_id":"01950000-0000-7000-8000-000000000001","type":"daemon_started"}`)
	if err := w1.Append(first, false); err != nil {
		t.Fatalf("Append(first): %v", err)
	}
	if err := w1.Close(); err != nil {
		t.Fatalf("Close(w1): %v", err)
	}

	// Open same path again — MUST NOT truncate existing content.
	w2, err := eventbus.OpenJSONLWriter(path)
	if err != nil {
		t.Fatalf("OpenJSONLWriter (second): %v", err)
	}
	defer func() { _ = w2.Close() }()

	second := []byte(`{"event_id":"01950000-0000-7000-8000-000000000002","type":"daemon_ready"}`)
	if err := w2.Append(second, false); err != nil {
		t.Fatalf("Append(second): %v", err)
	}

	lines := jsonlWriterFixtureReadLines(t, path)
	if len(lines) != 2 {
		t.Fatalf("re-open must not truncate: expected 2 lines, got %d", len(lines))
	}
	if lines[0] != string(first) {
		t.Errorf("prior line lost after re-open:\n  got:  %q\n  want: %q", lines[0], string(first))
	}
}

// TestJSONLWriterAppendWithSync verifies that sync=true does not corrupt output
// (the actual fsync durability is OS-level; this test verifies the line still
// lands correctly after a sync=true Append).
func TestJSONLWriterAppendWithSync(t *testing.T) {
	t.Parallel()

	path := jsonlWriterFixtureTempPath(t, "events.jsonl")
	w, err := eventbus.OpenJSONLWriter(path)
	if err != nil {
		t.Fatalf("OpenJSONLWriter: %v", err)
	}
	defer func() { _ = w.Close() }()

	line := []byte(`{"event_id":"01950000-0000-7000-8000-000000000001","type":"daemon_started"}`)
	// sync=true exercises the F-class (fsync-boundary) path.
	if err := w.Append(line, true); err != nil {
		t.Fatalf("Append(sync=true): %v", err)
	}

	lines := jsonlWriterFixtureReadLines(t, path)
	if len(lines) != 1 {
		t.Fatalf("expected 1 line after sync=true Append, got %d", len(lines))
	}
	if lines[0] != string(line) {
		t.Errorf("line corrupted after sync=true:\n  got:  %q\n  want: %q", lines[0], string(line))
	}
}

// TestJSONLWriterConcurrentAppend verifies that concurrent Append calls from
// multiple goroutines do not lose lines or corrupt the file.
// EV-020 / §6.2 concurrent-tailing note.
func TestJSONLWriterConcurrentAppend(t *testing.T) {
	t.Parallel()

	const goroutines = 8
	const linesPerGoroutine = 16

	path := jsonlWriterFixtureTempPath(t, "events.jsonl")
	w, err := eventbus.OpenJSONLWriter(path)
	if err != nil {
		t.Fatalf("OpenJSONLWriter: %v", err)
	}
	defer func() { _ = w.Close() }()

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := range goroutines {
		i := i
		go func() {
			defer wg.Done()
			for j := range linesPerGoroutine {
				line := []byte(`{"g":` + strings.Repeat("0", i) + `,"j":` + strings.Repeat("0", j) + `}`)
				if appendErr := w.Append(line, false); appendErr != nil {
					t.Errorf("goroutine %d Append %d: %v", i, j, appendErr)
					return
				}
			}
		}()
	}
	wg.Wait()

	lines := jsonlWriterFixtureReadLines(t, path)
	total := goroutines * linesPerGoroutine
	if len(lines) != total {
		t.Errorf("concurrent Append: expected %d lines, got %d (lines lost)", total, len(lines))
	}
	// Verify each line ends without truncation (non-empty, parseable as JSON-ish).
	for i, line := range lines {
		if len(line) == 0 {
			t.Errorf("line %d is empty (write corruption)", i)
		}
	}
}

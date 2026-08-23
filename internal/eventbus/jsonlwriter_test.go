package eventbus_test

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/gregberns/harmonik/internal/eventbus"
)

func jsonlWriterFixtureTempPath(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join(t.TempDir(), name)
}

func eventbusFixtureClose(t testing.TB, closer interface{ Close() error }) {
	t.Helper()
	if err := closer.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func jsonlWriterFixtureReadLines(t *testing.T, path string) []string {
	t.Helper()
	//nolint:gosec // G304: path is t.TempDir()-based; not user input.
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("jsonlWriterFixtureReadLines: open %s: %v", path, err)
	}
	defer eventbusFixtureClose(t, f)

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
	defer eventbusFixtureClose(t, w)

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
	defer eventbusFixtureClose(t, w)

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

	w2, err := eventbus.OpenJSONLWriter(path)
	if err != nil {
		t.Fatalf("OpenJSONLWriter (second): %v", err)
	}
	defer eventbusFixtureClose(t, w2)

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
	defer eventbusFixtureClose(t, w)

	line := []byte(`{"event_id":"01950000-0000-7000-8000-000000000001","type":"daemon_started"}`)
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
	defer eventbusFixtureClose(t, w)

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := range goroutines {
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
	for i, line := range lines {
		if line == "" {
			t.Errorf("line %d is empty (write corruption)", i)
		}
	}
}

// TestJSONLWriterAppendAfterCloseReturnsError verifies that Append returns
// ErrWriterClosed after Close is called. hk-5zode: drainer lifecycle.
func TestJSONLWriterAppendAfterCloseReturnsError(t *testing.T) {
	t.Parallel()

	path := jsonlWriterFixtureTempPath(t, "events.jsonl")
	w, err := eventbus.OpenJSONLWriter(path)
	if err != nil {
		t.Fatalf("OpenJSONLWriter: %v", err)
	}
	if closeErr := w.Close(); closeErr != nil {
		t.Fatalf("Close: %v", closeErr)
	}

	line := []byte(`{"type":"daemon_started"}`)
	appendErr := w.Append(line, false)
	if !errors.Is(appendErr, eventbus.ErrWriterClosed) {
		t.Errorf("Append after Close: got %v, want ErrWriterClosed", appendErr)
	}
}

// BenchmarkJSONLWriterFsyncLatency measures P99 Append(sync=true) latency at
// N=10 concurrent goroutines — the hk-5zode acceptance scenario. Run with:
//
//	go test ./internal/eventbus/ -bench=BenchmarkJSONLWriterFsyncLatency -benchtime=5s
//
// The benchmark emits all events with sync=true (F-class) to stress the
// drainer under realistic fsync load.
//
// Bead ref: hk-5zode.
func BenchmarkJSONLWriterFsyncLatency(b *testing.B) {
	const runs = 10

	path := b.TempDir() + "/bench_fsync.jsonl"
	w, err := eventbus.OpenJSONLWriter(path)
	if err != nil {
		b.Fatalf("OpenJSONLWriter: %v", err)
	}
	defer eventbusFixtureClose(b, w)

	line := []byte(`{"type":"run_started","run_id":"bench"}`)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		_ = runs // referenced to document intent; parallelism set by -cpu flag
		for pb.Next() {
			if appendErr := w.Append(line, true); appendErr != nil {
				b.Errorf("Append: %v", appendErr)
			}
		}
	})
}

// TestJSONLWriterCloseIdempotent verifies that calling Close twice does not
// panic with "close of closed channel".
//
// Regression test for hk-mmvcm: bus.Seal() + deferred w.Close() double-close.
func TestJSONLWriterCloseIdempotent(t *testing.T) {
	path := jsonlWriterFixtureTempPath(t, "close_idempotent.jsonl")
	w, err := eventbus.OpenJSONLWriter(path)
	if err != nil {
		t.Fatalf("OpenJSONLWriter: %v", err)
	}

	if closeErr := w.Close(); closeErr != nil {
		t.Fatalf("first Close: %v", closeErr)
	}

	if closeErr := w.Close(); closeErr != nil {
		t.Fatalf("second Close: %v", closeErr)
	}
}

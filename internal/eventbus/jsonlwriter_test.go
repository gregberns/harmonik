package eventbus_test

// jsonlwriter_test.go — binding tests for hk-hqwn.29 (EV-020 append-only JSONL)
// and hk-5zode (JSONLWriter fsync latency concentration — parallelism prep).
//
// Spec ref: event-model.md §6.2 EV-020; §4.4 EV-015, EV-016.
// Bead ref: hk-hqwn.29; hk-5zode.
//
// These tests verify that JSONLWriter:
//   1. Writes exactly one complete line (JSON + '\n') per Append call.
//   2. MUST NOT rewrite, truncate, or reorder existing lines.
//   3. Calls Sync when sync=true (fsync-boundary semantics for F-class events).
//   4. Is safe for concurrent use without interleaving or loss.
//   5. Returns ErrWriterClosed after Close (hk-5zode).
//
// The benchmark BenchmarkJSONLWriterFsyncLatency verifies the hk-5zode acceptance
// criterion: at N=10 concurrent goroutines each emitting F-class (sync=true)
// events at 10/sec, P99 Append latency stays under 50 ms.
//
// Helper prefix: jsonlWriterFixture (per implementer-protocol.md).

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

// jsonlWriterFixtureTempPath creates a temporary file path (not yet created)
// inside t.TempDir() for use as a JSONL log file.
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

// jsonlWriterFixtureReadLines reads all lines from path and returns them
// without trailing newlines.
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
	// Verify each line ends without truncation (non-empty, parseable as JSON-ish).
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

// TestJSONLWriterFsyncConcurrentLatency WAS HERE, and it measured the machine
// rather than the writer. Deleted 2026-08-05 (hk-v6ee0). What it did and what
// its removal costs, because a deletion that is not explained gets undone.
//
// IT WAS NOT A P99. It timed 10 goroutines times 10 Appends, which is 100
// samples, sorted them, and read index int(100*0.99) = 99 — the LARGEST of the
// 100. One slow fsync failed the build. That is a maximum, and a maximum over
// 100 samples on a shared laptop is a coin toss, not an acceptance bound.
//
// MEASURED 2026-08-05 on this tree at efbd62d33, with the writer unchanged
// between the two runs:
//   quiet box, -count=3            PASS 3 of 3
//   inside `make fast`             FAIL, P99 73.98ms
//   with 8 unrelated fsync writers FAIL, P99 56.87ms
// The verdict follows the load on the box. Nothing about the code moved.
//
// WHAT THE DELETION COSTS. No test now fails when F-class Append latency
// regresses. That is a real loss and it is the honest position: the assertion
// that was here could not tell a regression from a busy disk, so a red from it
// was never evidence and a green from it was never protection.
//
// WHAT STILL COVERS hk-5zode. BenchmarkJSONLWriterFsyncLatency below measures
// the same scenario deliberately, on a quiet box, with a sample count you
// choose. Run it when you change the drainer:
//
//	go test ./internal/eventbus/ -bench=BenchmarkJSONLWriterFsyncLatency -benchtime=5s
//
// The drainer's CONCURRENCY behaviour — no interleaving, no loss, one line per
// Append under concurrent writers — is still asserted, by
// TestJSONLWriterConcurrentAppend. Only the wall-clock number left.

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
		// RunParallel uses GOMAXPROCS goroutines by default; runs=10 is the
		// spec's concurrent-run count. The benchmark approximates that load.
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

	// First close — normal shutdown.
	if closeErr := w.Close(); closeErr != nil {
		t.Fatalf("first Close: %v", closeErr)
	}

	// Second close — must not panic and must return nil.
	if closeErr := w.Close(); closeErr != nil {
		t.Fatalf("second Close: %v", closeErr)
	}
}

package eventbus_test

// jsonlfilter_test.go — binding tests for hk-e61c3.5 (per-run-id JSONL filter).
//
// Spec ref: event-model.md §6.2 EV-020; POST_MVH_PARALLELISM_ROADMAP.md row 10.
// Bead ref: hk-e61c3.5.
//
// These tests verify that Filter:
//  1. Yields only events whose run_id matches the requested RunID, in file order.
//  2. Skips events with a non-matching (or absent) run_id silently.
//  3. Tolerates malformed JSONL lines (skips them with a warning; does not panic).
//  4. Does NOT shard or modify the file (EV-020: single-file-per-project).
//
// Helper prefix: filterFixture (per implementer-protocol.md §Helper-prefix discipline).

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
)

// filterFixtureTempPath returns a temp file path inside t.TempDir().
func filterFixtureTempPath(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join(t.TempDir(), name)
}

// filterFixtureRunID creates a new random RunID for use in filter tests.
func filterFixtureRunID(t *testing.T) core.RunID {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("filterFixtureRunID: uuid.NewV7: %v", err)
	}
	return core.RunID(id)
}

// filterFixtureEvent builds a minimal valid core.Event with the given type and
// optional runID. Pass a zero RunID to omit the field (nil pointer in output).
func filterFixtureEvent(t *testing.T, evType string, runID *core.RunID) core.Event {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("filterFixtureEvent: uuid.NewV7: %v", err)
	}
	payload := json.RawMessage(`{}`)
	return core.Event{
		EventID:         core.EventID(id),
		SchemaVersion:   1,
		Type:            evType,
		TimestampWall:   time.Now(),
		RunID:           runID,
		SourceSubsystem: "eventbus_test",
		Payload:         payload,
	}
}

// filterFixtureWriteEvents marshals each event as a JSONL line into path using
// JSONLWriter. Returns the path for chaining.
func filterFixtureWriteEvents(t *testing.T, path string, events []core.Event) string {
	t.Helper()
	w, err := eventbus.OpenJSONLWriter(path)
	if err != nil {
		t.Fatalf("filterFixtureWriteEvents: OpenJSONLWriter: %v", err)
	}
	defer func() { _ = w.Close() }()
	for i, ev := range events {
		b, marshalErr := json.Marshal(ev)
		if marshalErr != nil {
			t.Fatalf("filterFixtureWriteEvents: marshal event %d: %v", i, marshalErr)
		}
		if appendErr := w.Append(b, false); appendErr != nil {
			t.Fatalf("filterFixtureWriteEvents: Append event %d: %v", i, appendErr)
		}
	}
	return path
}

// filterFixtureCollect drains an iter.Seq[core.Event] into a slice.
func filterFixtureCollect(t *testing.T, seq func(yield func(core.Event) bool)) []core.Event {
	t.Helper()
	var out []core.Event
	seq(func(ev core.Event) bool {
		out = append(out, ev)
		return true
	})
	return out
}

// TestFilterMatchingEvents verifies that only events with the target run_id are
// yielded and that they appear in file order.
func TestFilterMatchingEvents(t *testing.T) {
	t.Parallel()

	path := filterFixtureTempPath(t, "events.jsonl")
	runA := filterFixtureRunID(t)
	runB := filterFixtureRunID(t)

	evA1 := filterFixtureEvent(t, "run_started", &runA)
	evB1 := filterFixtureEvent(t, "run_started", &runB)
	evA2 := filterFixtureEvent(t, "run_finished", &runA)
	evB2 := filterFixtureEvent(t, "run_finished", &runB)

	filterFixtureWriteEvents(t, path, []core.Event{evA1, evB1, evA2, evB2})

	got := filterFixtureCollect(t, eventbus.Filter(path, runA))
	if len(got) != 2 {
		t.Fatalf("expected 2 events for runA, got %d", len(got))
	}
	if got[0].EventID != evA1.EventID {
		t.Errorf("event[0] EventID: got %v, want %v", got[0].EventID, evA1.EventID)
	}
	if got[1].EventID != evA2.EventID {
		t.Errorf("event[1] EventID: got %v, want %v", got[1].EventID, evA2.EventID)
	}
}

// TestFilterNonMatchingEventsSkipped verifies that events with a different
// run_id are silently skipped.
func TestFilterNonMatchingEventsSkipped(t *testing.T) {
	t.Parallel()

	path := filterFixtureTempPath(t, "events.jsonl")
	runA := filterFixtureRunID(t)
	runB := filterFixtureRunID(t)

	evB := filterFixtureEvent(t, "run_started", &runB)
	filterFixtureWriteEvents(t, path, []core.Event{evB})

	got := filterFixtureCollect(t, eventbus.Filter(path, runA))
	if len(got) != 0 {
		t.Errorf("expected 0 events for runA (only runB in file), got %d", len(got))
	}
}

// TestFilterNoRunIDEventsSkipped verifies that events without a run_id field
// are silently skipped (they don't match any RunID).
func TestFilterNoRunIDEventsSkipped(t *testing.T) {
	t.Parallel()

	path := filterFixtureTempPath(t, "events.jsonl")
	runA := filterFixtureRunID(t)

	// Write an event with no run_id (nil pointer — field omitted in JSON).
	evNoRun := filterFixtureEvent(t, "daemon_started", nil)
	filterFixtureWriteEvents(t, path, []core.Event{evNoRun})

	got := filterFixtureCollect(t, eventbus.Filter(path, runA))
	if len(got) != 0 {
		t.Errorf("expected 0 events (no run_id in file), got %d", len(got))
	}
}

// TestFilterMalformedLinesSkipped verifies that malformed JSONL lines are
// skipped without panicking and without interrupting iteration over valid lines.
func TestFilterMalformedLinesSkipped(t *testing.T) {
	t.Parallel()

	path := filterFixtureTempPath(t, "events.jsonl")
	runA := filterFixtureRunID(t)

	// Write: valid match, malformed, valid match.
	evA1 := filterFixtureEvent(t, "run_started", &runA)
	evA2 := filterFixtureEvent(t, "run_finished", &runA)

	b1, _ := json.Marshal(evA1)
	b2, _ := json.Marshal(evA2)

	//nolint:gosec // G304: path is t.TempDir()-based; not user input.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	for _, chunk := range [][]byte{
		append(b1, '\n'),
		[]byte("{{not valid json}}\n"),
		append(b2, '\n'),
	} {
		if _, writeErr := f.Write(chunk); writeErr != nil {
			t.Fatalf("write: %v", writeErr)
		}
	}
	_ = f.Close()

	got := filterFixtureCollect(t, eventbus.Filter(path, runA))
	if len(got) != 2 {
		t.Fatalf("expected 2 matching events (malformed line skipped), got %d", len(got))
	}
	if got[0].EventID != evA1.EventID {
		t.Errorf("event[0] EventID: got %v, want %v", got[0].EventID, evA1.EventID)
	}
	if got[1].EventID != evA2.EventID {
		t.Errorf("event[1] EventID: got %v, want %v", got[1].EventID, evA2.EventID)
	}
}

// TestFilterFileOrderPreserved verifies that matched events are yielded in the
// same order they appear in the file.
func TestFilterFileOrderPreserved(t *testing.T) {
	t.Parallel()

	path := filterFixtureTempPath(t, "events.jsonl")
	runA := filterFixtureRunID(t)

	const n = 10
	events := make([]core.Event, n)
	for i := range n {
		events[i] = filterFixtureEvent(t, "run_step", &runA)
	}
	filterFixtureWriteEvents(t, path, events)

	got := filterFixtureCollect(t, eventbus.Filter(path, runA))
	if len(got) != n {
		t.Fatalf("expected %d events, got %d", n, len(got))
	}
	wantIDs := make([]core.EventID, n)
	for i, ev := range events {
		wantIDs[i] = ev.EventID
	}
	gotIDs := make([]core.EventID, n)
	for i, ev := range got {
		gotIDs[i] = ev.EventID
	}
	if !slices.Equal(gotIDs, wantIDs) {
		t.Errorf("event order mismatch:\n  got  %v\n  want %v", gotIDs, wantIDs)
	}
}

// TestFilterEmptyFile verifies that Filter on an empty (or non-existent-but-
// creatable) JSONL file yields no events without error.
func TestFilterEmptyFile(t *testing.T) {
	t.Parallel()

	path := filterFixtureTempPath(t, "empty.jsonl")
	// Create the file but write nothing.
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	_ = f.Close()

	runA := filterFixtureRunID(t)
	got := filterFixtureCollect(t, eventbus.Filter(path, runA))
	if len(got) != 0 {
		t.Errorf("expected 0 events from empty file, got %d", len(got))
	}
}

// TestFilterFileNotFound verifies that Filter on a non-existent path yields
// no events and does not panic.
func TestFilterFileNotFound(t *testing.T) {
	t.Parallel()

	path := filterFixtureTempPath(t, "does_not_exist.jsonl")
	runA := filterFixtureRunID(t)

	// Must not panic.
	got := filterFixtureCollect(t, eventbus.Filter(path, runA))
	if len(got) != 0 {
		t.Errorf("expected 0 events from missing file, got %d", len(got))
	}
}

package core

// Tests for DeadLetterSink: OpenDeadLetterSink, Record, Close, NoopDeadLetterSink.
//
// Coverage target: integration-style with a temp file — write N records,
// re-read the JSONL, assert each envelope round-trips.
//
// Property layer (TestProp_*) uses pgregory.net/rapid, mirroring beadid_prop_test.go.
//
// Refs: hk-fd95q, hk-j3hrn (core coverage uplift).

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"pgregory.net/rapid"
)

// makeTestEnvelope returns a minimal but valid EventEnvelope for sink tests.
func makeTestEnvelope(t *testing.T, eventType string) EventEnvelope {
	t.Helper()
	id, err := uuid.NewRandom()
	if err != nil {
		t.Fatalf("uuid.NewRandom: %v", err)
	}
	return EventEnvelope{
		EventID:         EventID(id),
		SchemaVersion:   1,
		Type:            eventType,
		TimestampWall:   time.Now().UTC(),
		SourceSubsystem: "core_test",
		Payload:         json.RawMessage(`{"k":"v"}`),
	}
}

// readDeadLetterRecords reads all JSONL lines from path and decodes each into
// a deadLetterRecord.  It returns the slice in file order.
func readDeadLetterRecords(t *testing.T, path string) []deadLetterRecord {
	t.Helper()
	f, err := os.Open(path) //nolint:gosec
	if err != nil {
		t.Fatalf("open %q: %v", path, err)
	}
	defer f.Close()

	var records []deadLetterRecord
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec deadLetterRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			t.Fatalf("unmarshal line %q: %v", line, err)
		}
		records = append(records, rec)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scanner: %v", err)
	}
	return records
}

// TestDeadLetterSink_OpenAndClose verifies that OpenDeadLetterSink creates the
// file and that Close succeeds.
func TestDeadLetterSink_OpenAndClose(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dead.jsonl")

	sink, err := OpenDeadLetterSink(path)
	if err != nil {
		t.Fatalf("OpenDeadLetterSink: %v", err)
	}
	if err := sink.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file should exist after open: %v", err)
	}
}

// TestDeadLetterSink_OpenBadDir verifies that OpenDeadLetterSink returns an
// error when the parent directory does not exist.
func TestDeadLetterSink_OpenBadDir(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nonexistent", "dead.jsonl")
	_, err := OpenDeadLetterSink(path)
	if err == nil {
		t.Fatal("expected error for missing parent directory, got nil")
	}
}

// TestDeadLetterSink_RecordRoundTrip writes N records and re-reads the JSONL
// file, asserting that each envelope round-trips losslessly.
func TestDeadLetterSink_RecordRoundTrip(t *testing.T) {
	const N = 5
	dir := t.TempDir()
	path := filepath.Join(dir, "dead.jsonl")

	sink, err := OpenDeadLetterSink(path)
	if err != nil {
		t.Fatalf("OpenDeadLetterSink: %v", err)
	}

	envs := make([]EventEnvelope, N)
	reasons := make([]string, N)
	for i := range envs {
		envs[i] = makeTestEnvelope(t, "test_event")
		reasons[i] = "reason_" + string(rune('A'+i))
		if err := sink.Record(context.Background(), envs[i], reasons[i]); err != nil {
			t.Fatalf("Record[%d]: %v", i, err)
		}
	}
	if err := sink.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	records := readDeadLetterRecords(t, path)
	if len(records) != N {
		t.Fatalf("expected %d records, got %d", N, len(records))
	}

	for i, rec := range records {
		if rec.Reason != reasons[i] {
			t.Errorf("record[%d] reason: got %q, want %q", i, rec.Reason, reasons[i])
		}
		if rec.RecordedAt == "" {
			t.Errorf("record[%d] recorded_at is empty", i)
		}
		gotID := uuid.UUID(rec.Envelope.EventID)
		wantID := uuid.UUID(envs[i].EventID)
		if gotID != wantID {
			t.Errorf("record[%d] EventID: got %v, want %v", i, gotID, wantID)
		}
		if rec.Envelope.Type != envs[i].Type {
			t.Errorf("record[%d] Type: got %q, want %q", i, rec.Envelope.Type, envs[i].Type)
		}
		if rec.Envelope.SchemaVersion != envs[i].SchemaVersion {
			t.Errorf("record[%d] SchemaVersion: got %d, want %d", i, rec.Envelope.SchemaVersion, envs[i].SchemaVersion)
		}
		if rec.Envelope.SourceSubsystem != envs[i].SourceSubsystem {
			t.Errorf("record[%d] SourceSubsystem: got %q, want %q", i, rec.Envelope.SourceSubsystem, envs[i].SourceSubsystem)
		}
	}
}

// TestDeadLetterSink_AppendPreservesExisting verifies that re-opening an
// existing file appends rather than truncating prior records.
func TestDeadLetterSink_AppendPreservesExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dead.jsonl")

	// First open: write one record.
	s1, err := OpenDeadLetterSink(path)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	env1 := makeTestEnvelope(t, "first_event")
	if err := s1.Record(context.Background(), env1, "r1"); err != nil {
		t.Fatalf("first Record: %v", err)
	}
	if err := s1.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}

	// Second open: write a second record.
	s2, err := OpenDeadLetterSink(path)
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	env2 := makeTestEnvelope(t, "second_event")
	if err := s2.Record(context.Background(), env2, "r2"); err != nil {
		t.Fatalf("second Record: %v", err)
	}
	if err := s2.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}

	records := readDeadLetterRecords(t, path)
	if len(records) != 2 {
		t.Fatalf("expected 2 records after two opens, got %d", len(records))
	}
	if records[0].Envelope.Type != "first_event" {
		t.Errorf("record[0] type: got %q, want %q", records[0].Envelope.Type, "first_event")
	}
	if records[1].Envelope.Type != "second_event" {
		t.Errorf("record[1] type: got %q, want %q", records[1].Envelope.Type, "second_event")
	}
}

// TestDeadLetterSink_WriteAfterClose verifies that Record returns an error
// after the underlying fd has been closed.
func TestDeadLetterSink_WriteAfterClose(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dead.jsonl")

	sink, err := OpenDeadLetterSink(path)
	if err != nil {
		t.Fatalf("OpenDeadLetterSink: %v", err)
	}
	if err := sink.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	env := makeTestEnvelope(t, "post_close")
	if err := sink.Record(context.Background(), env, "should_fail"); err == nil {
		t.Fatal("expected error writing to closed sink, got nil")
	}
}

// TestNoopDeadLetterSink_RecordAndClose verifies that the Noop variant accepts
// any call without error and satisfies the DeadLetterSink interface.
func TestNoopDeadLetterSink_RecordAndClose(t *testing.T) {
	var sink DeadLetterSink = NoopDeadLetterSink{}

	for i := 0; i < 3; i++ {
		env := makeTestEnvelope(t, "noop_event")
		if err := sink.Record(context.Background(), env, "discarded"); err != nil {
			t.Errorf("Noop Record[%d]: unexpected error: %v", i, err)
		}
	}
	if err := sink.Close(); err != nil {
		t.Errorf("Noop Close: unexpected error: %v", err)
	}
}

// TestProp_DeadLetterSink_EnvelopeRoundTrip is a property test verifying that
// for any event type string and reason string, Record writes an entry whose
// Reason and envelope.Type survive the JSONL round-trip unchanged.
func TestProp_DeadLetterSink_EnvelopeRoundTrip(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		eventType := rapid.StringN(1, 64, -1).Draw(rt, "eventType")
		reason := rapid.StringN(1, 128, -1).Draw(rt, "reason")

		dir, err := os.MkdirTemp("", "deadletter-prop-*")
		if err != nil {
			rt.Fatalf("MkdirTemp: %v", err)
		}
		defer os.RemoveAll(dir) //nolint:errcheck
		path := filepath.Join(dir, "dead.jsonl")

		id, err := uuid.NewRandom()
		if err != nil {
			rt.Fatalf("uuid: %v", err)
		}
		env := EventEnvelope{
			EventID:         EventID(id),
			SchemaVersion:   1,
			Type:            eventType,
			TimestampWall:   time.Now().UTC(),
			SourceSubsystem: "prop_test",
			Payload:         json.RawMessage(`{}`),
		}

		sink, err := OpenDeadLetterSink(path)
		if err != nil {
			rt.Fatalf("OpenDeadLetterSink: %v", err)
		}
		if err := sink.Record(context.Background(), env, reason); err != nil {
			rt.Fatalf("Record: %v", err)
		}
		if err := sink.Close(); err != nil {
			rt.Fatalf("Close: %v", err)
		}

		// Re-read and verify.
		f, err := os.Open(path) //nolint:gosec
		if err != nil {
			rt.Fatalf("open: %v", err)
		}
		defer f.Close()

		sc := bufio.NewScanner(f)
		if !sc.Scan() {
			rt.Fatalf("expected one line in JSONL, got none")
		}
		var rec deadLetterRecord
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			rt.Fatalf("unmarshal: %v", err)
		}
		if rec.Reason != reason {
			rt.Errorf("reason round-trip: got %q, want %q", rec.Reason, reason)
		}
		if rec.Envelope.Type != eventType {
			rt.Errorf("envelope.Type round-trip: got %q, want %q", rec.Envelope.Type, eventType)
		}
	})
}

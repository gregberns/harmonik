package handlercontract_test

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// deadLetterFixtureMakeEnvelope builds a minimal valid EventEnvelope for tests.
func deadLetterFixtureMakeEnvelope(t *testing.T) core.EventEnvelope {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("deadLetterFixtureMakeEnvelope: uuid.NewV7: %v", err)
	}
	return core.EventEnvelope{
		EventID:         core.EventID(id),
		SchemaVersion:   1,
		Type:            "test.dead_letter",
		TimestampWall:   time.Now().UTC(),
		SourceSubsystem: "handlercontract_test",
		Payload:         json.RawMessage(`{"msg":"test payload"}`),
	}
}

// TestDeadLetterSink_ThreeRecords writes three dead-letter records, reopens the
// file, and asserts: 3 lines, each valid JSON, each with recorded_at, reason,
// and envelope keys.
func TestDeadLetterSink_ThreeRecords(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dead-letters.jsonl")

	sink, err := handlercontract.OpenDeadLetterSink(path)
	if err != nil {
		t.Fatalf("OpenDeadLetterSink: %v", err)
	}
	defer func() { _ = sink.Close() }()

	ctx := context.Background()
	reasons := []string{"consumer_error", "panic_in_observer", "redaction_config_missing"}

	for _, reason := range reasons {
		env := deadLetterFixtureMakeEnvelope(t)
		if err := sink.Record(ctx, env, reason); err != nil {
			t.Fatalf("Record(%q): %v", reason, err)
		}
	}

	if err := sink.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Reopen and read back.
	//nolint:gosec // G304: path is t.TempDir()-relative; test-only.
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = f.Close() }()

	var lines []map[string]json.RawMessage
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(sc.Bytes(), &obj); err != nil {
			t.Fatalf("line %d: invalid JSON: %v", len(lines)+1, err)
		}
		lines = append(lines, obj)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scanner: %v", err)
	}

	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3", len(lines))
	}

	for i, obj := range lines {
		for _, key := range []string{"recorded_at", "reason", "envelope"} {
			if _, ok := obj[key]; !ok {
				t.Errorf("line %d: missing key %q", i+1, key)
			}
		}

		// recorded_at must parse as RFC3339Nano.
		var recordedAt string
		if err := json.Unmarshal(obj["recorded_at"], &recordedAt); err != nil {
			t.Errorf("line %d: recorded_at not a string: %v", i+1, err)
		} else if _, err := time.Parse(time.RFC3339Nano, recordedAt); err != nil {
			t.Errorf("line %d: recorded_at not RFC3339Nano: %v", i+1, err)
		}

		// reason must match expected.
		var gotReason string
		if err := json.Unmarshal(obj["reason"], &gotReason); err != nil {
			t.Errorf("line %d: reason not a string: %v", i+1, err)
		} else if gotReason != reasons[i] {
			t.Errorf("line %d: reason = %q, want %q", i+1, gotReason, reasons[i])
		}

		// envelope must be a non-empty object.
		var env map[string]json.RawMessage
		if err := json.Unmarshal(obj["envelope"], &env); err != nil {
			t.Errorf("line %d: envelope not a JSON object: %v", i+1, err)
		} else if len(env) == 0 {
			t.Errorf("line %d: envelope is empty object", i+1)
		}
	}
}

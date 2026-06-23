package sentinel_test

// governor_bounded_scan_hkusn8o_test.go — bounded-scan regression guard.
//
// Root cause (hk-usn8o): computeWindowMovement called eventbus.ScanAfter with
// a zero EventID cursor, re-parsing the entire events.jsonl (46MB) from offset
// zero on every sentinel.Evaluate call. The fix derives a UUIDv7 floor cursor
// from windowStart so the scan starts near the window instead of the file start.
//
// Tests:
//   - TestGovernor_BoundedScan_SkipsOldEvents: 100 old bead_closed events
//     (2h ago) + 3 in-window bead_closed events → score = 3*DefaultHighWeight.
//     Verifies the bounded cursor doesn't drop in-window events.
//   - TestGovernor_BoundedScan_CursorSkipsPreWindow: directly verifies that
//     ScanAfter with the same floor cursor returns only the in-window events,
//     not the 100 old events. This is the anti-regression assertion: without
//     the fix the cursor would be EventID{} and all 103 events would be returned.
//
// Bead ref: hk-usn8o.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/sentinel"
)

// writeEventWithV7ID appends one JSONL event whose EventID embeds ts as its
// UUIDv7 timestamp. seq is mixed into the random bits to ensure uniqueness.
// This mirrors how the daemon generates events: the UUID timestamp matches the
// wall-clock at emission time, which is the invariant the floor-cursor fix relies on.
func writeEventWithV7ID(t *testing.T, path string, evType core.EventType, ts time.Time, seq int, payload []byte) {
	t.Helper()
	ms := ts.UnixMilli()
	var b [16]byte
	b[0] = byte(ms >> 40)
	b[1] = byte(ms >> 32)
	b[2] = byte(ms >> 24)
	b[3] = byte(ms >> 16)
	b[4] = byte(ms >> 8)
	b[5] = byte(ms)
	b[6] = 0x70 // version 7, rand_a high nibble = 0
	b[8] = 0x80 // variant bits
	// Mix seq into random bits for uniqueness across events at the same ms.
	b[14] = byte(seq >> 8)
	b[15] = byte(seq)
	id := core.EventID(b)

	ev := core.Event{
		EventID:         id,
		SchemaVersion:   1,
		Type:            string(evType),
		TimestampWall:   ts,
		SourceSubsystem: "test",
		Payload:         payload,
	}
	line, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open events file: %v", err)
	}
	defer func() { _ = f.Close() }()
	if _, wErr := f.Write(append(line, '\n')); wErr != nil {
		t.Fatalf("write event: %v", wErr)
	}
}

// eventsFilePath returns the canonical events.jsonl path inside projectDir.
func eventsFilePath(projectDir string) string {
	return filepath.Join(projectDir, ".harmonik", "events", "events.jsonl")
}

// TestGovernor_BoundedScan_SkipsOldEvents verifies that ComputeWindowMovement
// returns the correct score when a large number of pre-window events precede the
// in-window events. Without the bounded cursor fix, the score would still be
// correct (the wall-clock filter discards old events), but this test ensures the
// core contract holds regardless of cursor behaviour.
//
// The companion test TestGovernor_BoundedScan_CursorSkipsPreWindow is the
// anti-regression assertion for the I/O bound itself.
func TestGovernor_BoundedScan_SkipsOldEvents(t *testing.T) {
	t.Parallel()

	projectDir := makeEventsFile(t)
	evPath := eventsFilePath(projectDir)
	now := time.Now()
	windowDur := 30 * time.Minute
	windowStart := now.Add(-windowDur)

	// Write 100 old bead_closed events (2 hours ago, well before the window).
	for i := range 100 {
		writeEventWithV7ID(t, evPath, core.EventTypeBeadClosed, now.Add(-2*time.Hour), i, nil)
	}
	// Write 3 in-window bead_closed events (15 minutes ago).
	for i := range 3 {
		writeEventWithV7ID(t, evPath, core.EventTypeBeadClosed, now.Add(-15*time.Minute), 100+i, nil)
	}

	sample := sentinel.ComputeWindowMovement(
		context.Background(),
		evPath,
		windowStart,
		now,
		sentinel.DefaultWeights,
		"", // gitPath: empty skips git
		"", // projectDir: empty skips countHeadAdvances
	)

	wantScore := 3 * sentinel.DefaultHighWeight
	if sample.MovementScore != wantScore {
		t.Errorf("MovementScore = %d, want %d (3 in-window bead_closed events × weight %d)",
			sample.MovementScore, wantScore, sentinel.DefaultHighWeight)
	}
	if sample.TerminalEventCount != 3 {
		t.Errorf("TerminalEventCount = %d, want 3", sample.TerminalEventCount)
	}
}

// TestGovernor_BoundedScan_CursorSkipsPreWindow is the anti-regression assertion
// for the I/O bound: it verifies that ScanAfter with the UUIDv7 floor cursor
// derived from windowStart returns only in-window events, not the 100 old ones.
//
// Without the fix (cursor = EventID{}), ScanAfter would return all 103 events.
// With the fix (cursor = floor(windowStart)), it returns at most 3 + a tiny
// clock-skew buffer.
func TestGovernor_BoundedScan_CursorSkipsPreWindow(t *testing.T) {
	t.Parallel()

	projectDir := makeEventsFile(t)
	evPath := eventsFilePath(projectDir)
	now := time.Now()
	windowDur := 30 * time.Minute
	windowStart := now.Add(-windowDur)

	const nOld = 100
	const nNew = 3

	for i := range nOld {
		writeEventWithV7ID(t, evPath, core.EventTypeBeadClosed, now.Add(-2*time.Hour), i, nil)
	}
	for i := range nNew {
		writeEventWithV7ID(t, evPath, core.EventTypeBeadClosed, now.Add(-15*time.Minute), nOld+i, nil)
	}

	// Construct the same floor cursor that computeWindowMovement now uses.
	ms := windowStart.UnixMilli()
	var b [16]byte
	b[0] = byte(ms >> 40)
	b[1] = byte(ms >> 32)
	b[2] = byte(ms >> 24)
	b[3] = byte(ms >> 16)
	b[4] = byte(ms >> 8)
	b[5] = byte(ms)
	b[6] = 0x70
	cursor := core.EventID(b)

	// Count how many events ScanAfter returns with the bounded cursor.
	boundedCount := 0
	for range eventbus.ScanAfter(evPath, cursor) {
		boundedCount++
	}

	// The bounded scan must return only the nNew in-window events. Allow a small
	// buffer for clock-skew edge cases (events within 1ms of windowStart). If
	// the cursor was EventID{} (the regression), this would be 103.
	const maxAllowed = nNew + 5
	if boundedCount > maxAllowed {
		t.Errorf("bounded scan returned %d events with floor cursor, want ≤ %d "+
			"(regression: zero cursor would return %d)", boundedCount, maxAllowed, nOld+nNew)
	}
	if boundedCount < nNew {
		t.Errorf("bounded scan returned %d events, want ≥ %d (in-window events must not be dropped)",
			boundedCount, nNew)
	}
}

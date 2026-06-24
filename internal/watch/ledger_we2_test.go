package watch_test

// ledger_we2_test.go — RED→GREEN tests for WE2 (watch event ledger).
//
// Three required assertions (task spec):
//   (a) A Scan advances the cursor to the last event_id WITHOUT touching any
//       comms-recv cursor.
//   (b) A duplicate event_id is not double-counted.
//   (c) A simulated subscription_gap triggers a re-scan from the cursor and
//       returns events that were dropped from the live stream.
//
// Done-check: these tests must be GREEN; no comms-recv cursor write must
// appear on the watch read path (verified structurally: watch.Ledger only
// writes .harmonik/watch/cursor and .harmonik/watch/latest.json).

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/watch"
)

// ledgerFixtureDir builds a temp project tree with .harmonik/events/ and
// .harmonik/watch/ sub-dirs.  Returns (projectDir, harmonikDir, eventsPath).
func ledgerFixtureDir(t *testing.T) (string, string, string) {
	t.Helper()
	root := t.TempDir()
	harmonikDir := filepath.Join(root, ".harmonik")
	eventsDir := filepath.Join(harmonikDir, "events")
	for _, d := range []string{eventsDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("ledgerFixtureDir: mkdir %s: %v", d, err)
		}
	}
	return root, harmonikDir, filepath.Join(eventsDir, "events.jsonl")
}

// ledgerFixtureEvent builds a minimal valid core.Event with a fresh UUIDv7.
func ledgerFixtureEvent(t *testing.T, evType string) core.Event {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("ledgerFixtureEvent: uuid.NewV7: %v", err)
	}
	return core.Event{
		EventID:         core.EventID(id),
		SchemaVersion:   1,
		Type:            evType,
		TimestampWall:   time.Now(),
		SourceSubsystem: "watch_test",
		Payload:         json.RawMessage(`{}`),
	}
}

// ledgerFixtureAppend marshals events and appends them to eventsPath.
func ledgerFixtureAppend(t *testing.T, eventsPath string, events []core.Event) {
	t.Helper()
	w, err := eventbus.OpenJSONLWriter(eventsPath)
	if err != nil {
		t.Fatalf("ledgerFixtureAppend: OpenJSONLWriter: %v", err)
	}
	defer func() { _ = w.Close() }()
	for i, ev := range events {
		b, marshalErr := json.Marshal(ev)
		if marshalErr != nil {
			t.Fatalf("ledgerFixtureAppend: marshal event %d: %v", i, marshalErr)
		}
		if appendErr := w.Append(b, false); appendErr != nil {
			t.Fatalf("ledgerFixtureAppend: Append event %d: %v", i, appendErr)
		}
	}
}

// readCursorFile reads the cursor file and returns its trimmed content.
// Returns "" if the file does not exist.
func readCursorFile(t *testing.T, harmonikDir string) string {
	t.Helper()
	path := filepath.Join(harmonikDir, "watch", "cursor")
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil {
		t.Fatalf("readCursorFile: %v", err)
	}
	return strings.TrimSpace(string(raw))
}

// Test (a): Scan advances .harmonik/watch/cursor to the last event_id and does
// NOT write to the comms-recv cursor directory (.harmonik/comms/cursors/).
func TestWatchLedger_ScanAdvancesCursorWithoutTouchingRecvCursor(t *testing.T) {
	t.Parallel()

	_, harmonikDir, eventsPath := ledgerFixtureDir(t)

	evA := ledgerFixtureEvent(t, "run_started")
	evB := ledgerFixtureEvent(t, "run_completed")
	evC := ledgerFixtureEvent(t, "agent_heartbeat")
	ledgerFixtureAppend(t, eventsPath, []core.Event{evA, evB, evC})

	ledger, err := watch.NewLedger(harmonikDir)
	if err != nil {
		t.Fatalf("NewLedger: %v", err)
	}

	got, err := ledger.Scan(eventsPath)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	// Returned exactly the 3 events in order.
	if len(got) != 3 {
		t.Fatalf("Scan: want 3 events, got %d", len(got))
	}
	if got[0].EventID != evA.EventID {
		t.Errorf("event[0] EventID mismatch: got %v, want %v", got[0].EventID, evA.EventID)
	}
	if got[2].EventID != evC.EventID {
		t.Errorf("event[2] EventID mismatch: got %v, want %v", got[2].EventID, evC.EventID)
	}

	// Watch cursor advanced to the last event.
	cursorStr := readCursorFile(t, harmonikDir)
	if cursorStr == "" {
		t.Fatal("cursor file not written after Scan")
	}
	if cursorStr != evC.EventID.String() {
		t.Errorf("cursor: got %q, want %q", cursorStr, evC.EventID.String())
	}

	// comms-recv cursor directory MUST NOT exist.
	recvCursorDir := filepath.Join(harmonikDir, "comms", "cursors")
	if _, statErr := os.Stat(recvCursorDir); !os.IsNotExist(statErr) {
		t.Errorf("comms-recv cursor dir %q must not be created by Scan (recv-cursor contamination)", recvCursorDir)
	}
}

// Test (b): An event_id already in the in-memory seen set is not double-counted.
//
// Scenario: the live subscribe stream delivers event A before the re-scan.
// MarkSeen registers A.  Scan then re-reads from cursor (zero → A,B,C visible)
// and skips A.  Only B and C are returned.
func TestWatchLedger_DedupeEventID(t *testing.T) {
	t.Parallel()

	_, harmonikDir, eventsPath := ledgerFixtureDir(t)

	evA := ledgerFixtureEvent(t, "run_started")
	evB := ledgerFixtureEvent(t, "run_completed")
	evC := ledgerFixtureEvent(t, "agent_heartbeat")
	ledgerFixtureAppend(t, eventsPath, []core.Event{evA, evB, evC})

	ledger, err := watch.NewLedger(harmonikDir)
	if err != nil {
		t.Fatalf("NewLedger: %v", err)
	}

	// Simulate the live stream delivering evA before the scan.
	ledger.MarkSeen(evA.EventID)

	got, err := ledger.Scan(eventsPath)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	// evA is already seen → only evB and evC are returned.
	if len(got) != 2 {
		t.Fatalf("Scan after MarkSeen: want 2 events, got %d", len(got))
	}
	if got[0].EventID != evB.EventID {
		t.Errorf("event[0] want evB (%v), got %v", evB.EventID, got[0].EventID)
	}
	if got[1].EventID != evC.EventID {
		t.Errorf("event[1] want evC (%v), got %v", evC.EventID, got[1].EventID)
	}

	// A second Scan with no new events returns nothing (cursor at evC, no events after it).
	got2, err := ledger.Scan(eventsPath)
	if err != nil {
		t.Fatalf("second Scan: %v", err)
	}
	if len(got2) != 0 {
		t.Errorf("second Scan: want 0 events, got %d", len(got2))
	}
}

// Test (c): A simulated subscription_gap triggers a re-scan from the cursor
// and returns events that were dropped from the live stream.
//
// Scenario:
//  1. Events A, B, C are scanned normally (cursor advances to C).
//  2. Events D, E are appended to events.jsonl — these represent events that
//     the 256-slot subscribe buffer dropped (the live stream missed them).
//  3. ScanOnSubscriptionGap re-scans from cursor C → returns D, E.
//  4. If D was also delivered by the live stream (MarkSeen), it is skipped.
func TestWatchLedger_SubscriptionGapRescans(t *testing.T) {
	t.Parallel()

	_, harmonikDir, eventsPath := ledgerFixtureDir(t)

	evA := ledgerFixtureEvent(t, "run_started")
	evB := ledgerFixtureEvent(t, "run_completed")
	evC := ledgerFixtureEvent(t, "agent_heartbeat")
	ledgerFixtureAppend(t, eventsPath, []core.Event{evA, evB, evC})

	ledger, err := watch.NewLedger(harmonikDir)
	if err != nil {
		t.Fatalf("NewLedger: %v", err)
	}

	// Normal scan: A, B, C processed; cursor advances to C.
	got, err := ledger.Scan(eventsPath)
	if err != nil {
		t.Fatalf("initial Scan: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("initial Scan: want 3, got %d", len(got))
	}
	if ledger.Cursor() != evC.EventID {
		t.Fatalf("cursor after initial Scan: got %v, want %v", ledger.Cursor(), evC.EventID)
	}

	// Append D, E — the "dropped" events (subscription_gap).
	evD := ledgerFixtureEvent(t, "run_started")
	evE := ledgerFixtureEvent(t, "run_completed")
	ledgerFixtureAppend(t, eventsPath, []core.Event{evD, evE})

	// Additionally simulate the live stream having delivered D already.
	ledger.MarkSeen(evD.EventID)

	// subscription_gap re-scan: should return only E (D is in the seen set).
	got2, err := ledger.ScanOnSubscriptionGap(eventsPath)
	if err != nil {
		t.Fatalf("ScanOnSubscriptionGap: %v", err)
	}
	if len(got2) != 1 {
		t.Fatalf("ScanOnSubscriptionGap: want 1 new event (E), got %d: %v", len(got2), got2)
	}
	if got2[0].EventID != evE.EventID {
		t.Errorf("ScanOnSubscriptionGap: want evE (%v), got %v", evE.EventID, got2[0].EventID)
	}

	// Cursor advanced to E.
	if ledger.Cursor() != evE.EventID {
		t.Errorf("cursor after gap re-scan: got %v, want %v", ledger.Cursor(), evE.EventID)
	}
	cursorStr := readCursorFile(t, harmonikDir)
	if cursorStr != evE.EventID.String() {
		t.Errorf("cursor file after gap re-scan: got %q, want %q", cursorStr, evE.EventID.String())
	}
}

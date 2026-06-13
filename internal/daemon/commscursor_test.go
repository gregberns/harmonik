package daemon

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
)

// freshV7 returns a valid UUIDv7 string for use as a cursor event_id. Cursors in
// production are always real event_ids (UUIDv7); Advance parses them to enforce
// the monotonic-advance invariant (hk-fvo9e), so test cursors must be valid too.
func freshV7(t *testing.T) string {
	t.Helper()
	u, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("uuid.NewV7: %v", err)
	}
	return u.String()
}

// orderedV7Pair returns two valid UUIDv7 strings in strictly ascending order
// (first < second by byte/chronological order, EV-002).
func orderedV7Pair(t *testing.T) (string, string) {
	t.Helper()
	for {
		a := freshV7(t)
		b := freshV7(t)
		if a < b {
			return a, b
		}
		if b < a {
			return b, a
		}
		// equal (same intra-ms slot collision) — retry
	}
}

// TestCursorStoreGet_NoFile verifies Get returns "" (no error) when no cursor
// exists yet — the caller should treat this as "start of log".
func TestCursorStoreGet_NoFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cs := NewCursorStore(filepath.Join(dir, "cursors"))

	got, err := cs.Get("agent-a")
	if err != nil {
		t.Fatalf("Get on absent cursor: unexpected error: %v", err)
	}
	if got != "" {
		t.Fatalf("Get on absent cursor: want \"\", got %q", got)
	}
}

// TestCursorStoreAdvanceThenGet verifies that Advance persists the eventID and
// Get returns it.
func TestCursorStoreAdvanceThenGet(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cs := NewCursorStore(filepath.Join(dir, "cursors"))

	wantID := freshV7(t)
	if err := cs.Advance("agent-a", wantID); err != nil {
		t.Fatalf("Advance: %v", err)
	}

	got, err := cs.Get("agent-a")
	if err != nil {
		t.Fatalf("Get after Advance: %v", err)
	}
	if got != wantID {
		t.Fatalf("Get: want %q, got %q", wantID, got)
	}
}

// TestCursorStoreAdvanceOverwrite verifies that a second, strictly-forward
// Advance replaces the previous cursor value.
func TestCursorStoreAdvanceOverwrite(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cs := NewCursorStore(filepath.Join(dir, "cursors"))

	first, second := orderedV7Pair(t) // first < second
	if err := cs.Advance("agent-b", first); err != nil {
		t.Fatalf("first Advance: %v", err)
	}
	if err := cs.Advance("agent-b", second); err != nil {
		t.Fatalf("second Advance: %v", err)
	}

	got, err := cs.Get("agent-b")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != second {
		t.Fatalf("Get: want %q, got %q", second, got)
	}
}

// TestCursorStoreSurvivesRestart verifies that a new CursorStore pointed at
// the same directory reads the cursor written by a prior instance — daemon
// restart safety.
func TestCursorStoreSurvivesRestart(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cursorDir := filepath.Join(dir, "cursors")

	wantID := freshV7(t)
	cs1 := NewCursorStore(cursorDir)
	if err := cs1.Advance("flywheel", wantID); err != nil {
		t.Fatalf("Advance: %v", err)
	}

	// Simulate daemon restart: new CursorStore instance, same directory.
	cs2 := NewCursorStore(cursorDir)
	got, err := cs2.Get("flywheel")
	if err != nil {
		t.Fatalf("Get (after restart): %v", err)
	}
	if got != wantID {
		t.Fatalf("Get (after restart): want %q, got %q", wantID, got)
	}
}

// TestCursorStoreMultipleAgents verifies that cursors for different agent names
// are stored independently.
func TestCursorStoreMultipleAgents(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cs := NewCursorStore(filepath.Join(dir, "cursors"))

	aliceID := freshV7(t)
	bobID := freshV7(t)
	if err := cs.Advance("alice", aliceID); err != nil {
		t.Fatalf("Advance alice: %v", err)
	}
	if err := cs.Advance("bob", bobID); err != nil {
		t.Fatalf("Advance bob: %v", err)
	}

	alice, err := cs.Get("alice")
	if err != nil {
		t.Fatalf("Get alice: %v", err)
	}
	if alice != aliceID {
		t.Fatalf("alice cursor: want %q, got %q", aliceID, alice)
	}

	bob, err := cs.Get("bob")
	if err != nil {
		t.Fatalf("Get bob: %v", err)
	}
	if bob != bobID {
		t.Fatalf("bob cursor: want %q, got %q", bobID, bob)
	}
}

// TestCursorStoreAdvanceEmptyEventID verifies that Advance rejects an empty
// eventID.
func TestCursorStoreAdvanceEmptyEventID(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cs := NewCursorStore(filepath.Join(dir, "cursors"))

	if err := cs.Advance("agent-c", ""); err == nil {
		t.Fatal("Advance with empty eventID: want error, got nil")
	}
}

// TestCursorStoreInvalidNames verifies that Get and Advance reject names with
// path-unsafe characters or reserved values.
func TestCursorStoreInvalidNames(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cs := NewCursorStore(filepath.Join(dir, "cursors"))

	cases := []string{
		"",
		"../escape",
		"foo/bar",
		"foo\x00bar",
		".",
		"..",
	}
	for _, name := range cases {
		if _, err := cs.Get(name); err == nil {
			t.Errorf("Get(%q): want error for invalid name, got nil", name)
		}
		if err := cs.Advance(name, "01JXXX"); err == nil {
			t.Errorf("Advance(%q): want error for invalid name, got nil", name)
		}
	}
}

// TestCursorStoreAtomicWrite verifies that no temp file leaks on a normal
// Advance — the only file left in the cursor directory is the cursor itself.
func TestCursorStoreAtomicWrite(t *testing.T) {
	t.Parallel()
	cursorDir := filepath.Join(t.TempDir(), "cursors")
	cs := NewCursorStore(cursorDir)

	if err := cs.Advance("agent-d", freshV7(t)); err != nil {
		t.Fatalf("Advance: %v", err)
	}

	entries, err := os.ReadDir(cursorDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Fatalf("cursor dir has %d entries (want 1): %v", len(entries), names)
	}
	if entries[0].Name() != "agent-d" {
		t.Fatalf("cursor file name: want %q, got %q", "agent-d", entries[0].Name())
	}
}

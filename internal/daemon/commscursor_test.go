package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

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

	const wantID = "01JXXXXXXXXXXXXXXXXXXX"
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

// TestCursorStoreAdvanceOverwrite verifies that a second Advance replaces the
// previous cursor value.
func TestCursorStoreAdvanceOverwrite(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cs := NewCursorStore(filepath.Join(dir, "cursors"))

	first := "01JFIRST00000000000000"
	second := "01JSECOND0000000000000"
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

	const wantID = "01JRESTART0000000000000"
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

	if err := cs.Advance("alice", "01JALICE000000000000000"); err != nil {
		t.Fatalf("Advance alice: %v", err)
	}
	if err := cs.Advance("bob", "01JBOB0000000000000000"); err != nil {
		t.Fatalf("Advance bob: %v", err)
	}

	alice, err := cs.Get("alice")
	if err != nil {
		t.Fatalf("Get alice: %v", err)
	}
	if alice != "01JALICE000000000000000" {
		t.Fatalf("alice cursor: want %q, got %q", "01JALICE000000000000000", alice)
	}

	bob, err := cs.Get("bob")
	if err != nil {
		t.Fatalf("Get bob: %v", err)
	}
	if bob != "01JBOB0000000000000000" {
		t.Fatalf("bob cursor: want %q, got %q", "01JBOB0000000000000000", bob)
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

	if err := cs.Advance("agent-d", "01JATOMIC0000000000000"); err != nil {
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

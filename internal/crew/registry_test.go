package crew_test

import (
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/crew"
)

func makeRecord(name string) crew.Record {
	return crew.Record{
		Name:      name,
		SessionID: "sess-" + name,
		Queue:     "q-" + name,
		Epic:      "",
		Handle:    "handle-" + name,
		StartedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

// TestRoundTrip verifies Write → Load round-trips all fields correctly.
func TestRoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	r := makeRecord("alpha")
	if err := crew.Write(dir, r); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got, err := crew.Load(dir, "alpha")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got.SchemaVersion != 1 {
		t.Errorf("SchemaVersion = %d, want 1", got.SchemaVersion)
	}
	if got.Name != r.Name {
		t.Errorf("Name = %q, want %q", got.Name, r.Name)
	}
	if got.SessionID != r.SessionID {
		t.Errorf("SessionID = %q, want %q", got.SessionID, r.SessionID)
	}
	if got.Queue != r.Queue {
		t.Errorf("Queue = %q, want %q", got.Queue, r.Queue)
	}
	if got.Handle != r.Handle {
		t.Errorf("Handle = %q, want %q", got.Handle, r.Handle)
	}
	if !got.StartedAt.Equal(r.StartedAt) {
		t.Errorf("StartedAt = %v, want %v", got.StartedAt, r.StartedAt)
	}
}

// TestUpdateSessionID verifies that UpdateSessionID mutates only session_id.
func TestUpdateSessionID(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	r := makeRecord("beta")
	if err := crew.Write(dir, r); err != nil {
		t.Fatalf("Write: %v", err)
	}

	const newSess = "new-session-uuid"
	if err := crew.UpdateSessionID(dir, "beta", newSess); err != nil {
		t.Fatalf("UpdateSessionID: %v", err)
	}

	got, err := crew.Load(dir, "beta")
	if err != nil {
		t.Fatalf("Load after update: %v", err)
	}
	if got.SessionID != newSess {
		t.Errorf("SessionID = %q, want %q", got.SessionID, newSess)
	}
	// All other fields must be unchanged.
	if got.Queue != r.Queue {
		t.Errorf("Queue mutated: got %q, want %q", got.Queue, r.Queue)
	}
	if got.Handle != r.Handle {
		t.Errorf("Handle mutated: got %q, want %q", got.Handle, r.Handle)
	}
}

// TestListSortedByName verifies List returns all records sorted by name.
func TestListSortedByName(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	names := []string{"charlie", "alpha", "beta"}
	for _, n := range names {
		if err := crew.Write(dir, makeRecord(n)); err != nil {
			t.Fatalf("Write %q: %v", n, err)
		}
	}

	records, err := crew.List(dir)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(records) != 3 {
		t.Fatalf("List len = %d, want 3", len(records))
	}
	want := []string{"alpha", "beta", "charlie"}
	for i, r := range records {
		if r.Name != want[i] {
			t.Errorf("records[%d].Name = %q, want %q", i, r.Name, want[i])
		}
	}
}

// TestListEmptyDir verifies List on an absent directory returns empty slice, no error.
func TestListEmptyDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	records, err := crew.List(dir)
	if err != nil {
		t.Fatalf("List on empty dir: %v", err)
	}
	if len(records) != 0 {
		t.Errorf("expected empty slice, got %d records", len(records))
	}
}

// TestRemove verifies Remove deletes the record and returns ErrNotFound afterwards.
func TestRemove(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	r := makeRecord("delta")
	if err := crew.Write(dir, r); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := crew.Remove(dir, "delta"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	_, err := crew.Load(dir, "delta")
	if err == nil {
		t.Fatal("Load after Remove: expected error, got nil")
	}
}

// TestInvalidNames verifies Write rejects invalid crew names.
func TestInvalidNames(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	cases := []struct {
		name  string
		label string
	}{
		{"", "empty"},
		{"/etc/passwd", "slash"},
		{"..", "dotdot"},
		{"Alpha", "uppercase"},
		{"has space", "space"},
		{"has_underscore", "underscore"},
		{"a-very-long-name-that-exceeds-sixty-four-characters-in-total-length", "toolong"},
	}

	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			t.Parallel()
			r := crew.Record{Name: tc.name}
			if err := crew.Write(dir, r); err == nil {
				t.Errorf("Write(%q) expected error, got nil", tc.name)
			}
		})
	}
}

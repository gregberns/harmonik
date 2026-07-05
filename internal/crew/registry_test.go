package crew_test

import (
	"errors"
	"os"
	"path/filepath"
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

// TestEffectiveType verifies the legacy-default and explicit-type cases.
func TestEffectiveType(t *testing.T) {
	t.Parallel()

	t.Run("empty_defaults_to_crew", func(t *testing.T) {
		t.Parallel()
		r := crew.Record{Name: "leto"}
		if got := r.EffectiveType(); got != "crew" {
			t.Errorf("EffectiveType() = %q, want %q", got, "crew")
		}
	})

	t.Run("explicit_type_returned", func(t *testing.T) {
		t.Parallel()
		r := crew.Record{Name: "remus", Type: "captain"}
		if got := r.EffectiveType(); got != "captain" {
			t.Errorf("EffectiveType() = %q, want %q", got, "captain")
		}
	})
}

// TestTypeFieldRoundTrip verifies the Type field survives Write → Load and that
// legacy records (no type field in JSON) load without migration errors with default "crew".
func TestTypeFieldRoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	r := makeRecord("remus")
	r.Type = "captain"
	if err := crew.Write(dir, r); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := crew.Load(dir, "remus")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Type != "captain" {
		t.Errorf("Type = %q, want %q", got.Type, "captain")
	}
	if got.EffectiveType() != "captain" {
		t.Errorf("EffectiveType() = %q, want %q", got.EffectiveType(), "captain")
	}
}

// TestLegacyRecordNoTypeField simulates loading a JSON record that was written
// before the type field existed (no "type" key in the JSON).
func TestLegacyRecordNoTypeField(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Write the record without the type field (legacy format).
	legacyJSON := `{"schema_version":1,"name":"oldcrew","session_id":"s1","queue":"main","epic":"","handle":"h1","started_at":"2026-01-01T00:00:00Z"}`
	crewDir := filepath.Join(dir, ".harmonik", "crew")
	//nolint:gosec // G301: test fixture directory
	if err := os.MkdirAll(crewDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(crewDir, "oldcrew.json"), []byte(legacyJSON), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := crew.Load(dir, "oldcrew")
	if err != nil {
		t.Fatalf("Load legacy record: %v", err)
	}
	if got.Type != "" {
		t.Errorf("Type = %q, want empty string for legacy record", got.Type)
	}
	if got.EffectiveType() != "crew" {
		t.Errorf("EffectiveType() = %q, want %q for legacy record", got.EffectiveType(), "crew")
	}
}

// TestResolveType verifies all four resolution branches.
func TestResolveType(t *testing.T) {
	t.Parallel()

	// Shared fixture: a project dir with crew records + an agents dir with type folders.
	setup := func(t *testing.T) (projectDir, agentsDir string) {
		t.Helper()
		base := t.TempDir()
		projectDir = base
		agentsDir = filepath.Join(base, ".harmonik", "agents")

		// Create a crew type folder (bare type).
		//nolint:gosec // G301: test fixture directory
		if err := os.MkdirAll(filepath.Join(agentsDir, "crew"), 0o755); err != nil {
			t.Fatalf("mkdir agents/crew: %v", err)
		}
		// Create an admiral type folder.
		//nolint:gosec // G301: test fixture directory
		if err := os.MkdirAll(filepath.Join(agentsDir, "admiral"), 0o755); err != nil {
			t.Fatalf("mkdir agents/admiral: %v", err)
		}
		// Write a crew instance record for "leto" (no explicit type → defaults to crew).
		letoRec := makeRecord("leto")
		if err := crew.Write(projectDir, letoRec); err != nil {
			t.Fatalf("Write leto: %v", err)
		}
		// Write a crew instance record for "remus" with explicit type "admiral".
		remusRec := makeRecord("remus")
		remusRec.Type = "admiral"
		if err := crew.Write(projectDir, remusRec); err != nil {
			t.Fatalf("Write remus: %v", err)
		}
		return projectDir, agentsDir
	}

	t.Run("bare_type_name_resolves_to_itself", func(t *testing.T) {
		t.Parallel()
		projectDir, agentsDir := setup(t)
		got, err := crew.ResolveType(projectDir, agentsDir, "crew")
		if err != nil {
			t.Fatalf("ResolveType(crew): %v", err)
		}
		if got != "crew" {
			t.Errorf("got %q, want %q", got, "crew")
		}
	})

	t.Run("instance_name_default_type_is_crew", func(t *testing.T) {
		t.Parallel()
		projectDir, agentsDir := setup(t)
		got, err := crew.ResolveType(projectDir, agentsDir, "leto")
		if err != nil {
			t.Fatalf("ResolveType(leto): %v", err)
		}
		if got != "crew" {
			t.Errorf("got %q, want %q", got, "crew")
		}
	})

	t.Run("instance_with_explicit_type", func(t *testing.T) {
		t.Parallel()
		projectDir, agentsDir := setup(t)
		got, err := crew.ResolveType(projectDir, agentsDir, "remus")
		if err != nil {
			t.Fatalf("ResolveType(remus): %v", err)
		}
		if got != "admiral" {
			t.Errorf("got %q, want %q", got, "admiral")
		}
	})

	t.Run("unknown_name_returns_not_found", func(t *testing.T) {
		t.Parallel()
		projectDir, agentsDir := setup(t)
		_, err := crew.ResolveType(projectDir, agentsDir, "ghost")
		if err == nil {
			t.Fatal("expected error for unknown name, got nil")
		}
		if !errors.Is(err, crew.ErrNotFound) {
			t.Errorf("expected ErrNotFound, got %v", err)
		}
	})

	t.Run("corrupted_record_propagates_non_notfound_error", func(t *testing.T) {
		t.Parallel()
		projectDir, agentsDir := setup(t)

		// Write a record file with invalid JSON to simulate corruption.
		crewDir := filepath.Join(projectDir, ".harmonik", "crew")
		//nolint:gosec // G301: test fixture directory
		if err := os.MkdirAll(crewDir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(crewDir, "corrupt.json"), []byte("{not valid json"), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		_, err := crew.ResolveType(projectDir, agentsDir, "corrupt")
		if err == nil {
			t.Fatal("expected error for corrupted record, got nil")
		}
		if errors.Is(err, crew.ErrNotFound) {
			t.Errorf("corrupted record must NOT return ErrNotFound, got %v", err)
		}
	})
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

// TestHKAOAPQ_List_EmptyFile verifies that List returns a stub Record{Name: name}
// when a crew JSON file is 0-byte (truncated write / partial fsync at restart).
// The stub allows callers to probe the corresponding tmux session by name rather
// than silently dropping all crew exemptions (hk-aoapq).
func TestHKAOAPQ_List_EmptyFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Write a valid crew record first, then simulate a 0-byte crash of a second.
	if err := crew.Write(dir, makeRecord("valid")); err != nil {
		t.Fatalf("Write valid: %v", err)
	}
	crewDir := filepath.Join(dir, ".harmonik", "crew")
	if err := os.WriteFile(filepath.Join(crewDir, "empty.json"), []byte{}, 0o600); err != nil {
		t.Fatalf("WriteFile empty: %v", err)
	}

	records, err := crew.List(dir)
	if err != nil {
		t.Fatalf("List: unexpected error: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("List len = %d, want 2 (valid record + stub for empty file)", len(records))
	}
	// Find the stub.
	var stub *crew.Record
	for i := range records {
		if records[i].Name == "empty" {
			r := records[i]
			stub = &r
		}
	}
	if stub == nil {
		t.Fatal("stub record for empty.json not found in List result")
	}
	if stub.SessionID != "" || stub.Queue != "" {
		t.Errorf("stub record has non-empty fields: %+v; want Name-only stub", stub)
	}
}

// TestHKAOAPQ_List_CorruptFile verifies that List returns a stub Record{Name: name}
// when a crew JSON file contains invalid JSON (hk-aoapq).
func TestHKAOAPQ_List_CorruptFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	crewDir := filepath.Join(dir, ".harmonik", "crew")
	//nolint:gosec // G301: test fixture directory
	if err := os.MkdirAll(crewDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(crewDir, "corrupt.json"), []byte("{not valid json"), 0o600); err != nil {
		t.Fatalf("WriteFile corrupt: %v", err)
	}

	records, err := crew.List(dir)
	if err != nil {
		t.Fatalf("List: unexpected error: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("List len = %d, want 1 (stub for corrupt file)", len(records))
	}
	if records[0].Name != "corrupt" {
		t.Errorf("stub Name = %q, want %q", records[0].Name, "corrupt")
	}
	if records[0].SessionID != "" {
		t.Errorf("stub SessionID = %q, want empty", records[0].SessionID)
	}
}

// TestHKAOAPQ_List_OneGoodOneCorrupt verifies that List returns all valid records
// PLUS a stub for the corrupt file — it does not abort on the first bad file (hk-aoapq).
func TestHKAOAPQ_List_OneGoodOneCorrupt(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	if err := crew.Write(dir, makeRecord("alpha")); err != nil {
		t.Fatalf("Write alpha: %v", err)
	}
	crewDir := filepath.Join(dir, ".harmonik", "crew")
	if err := os.WriteFile(filepath.Join(crewDir, "corrupt.json"), []byte("{not valid json"), 0o600); err != nil {
		t.Fatalf("WriteFile corrupt: %v", err)
	}

	records, err := crew.List(dir)
	if err != nil {
		t.Fatalf("List: unexpected error: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("List len = %d, want 2", len(records))
	}
	// Both should be present (sort order: alpha < corrupt).
	if records[0].Name != "alpha" {
		t.Errorf("records[0].Name = %q, want %q", records[0].Name, "alpha")
	}
	if records[1].Name != "corrupt" {
		t.Errorf("records[1].Name = %q, want %q", records[1].Name, "corrupt")
	}
	// The valid record must have its fields.
	if records[0].SessionID != "sess-alpha" {
		t.Errorf("alpha SessionID = %q, want %q", records[0].SessionID, "sess-alpha")
	}
	// The stub must have only the name.
	if records[1].SessionID != "" {
		t.Errorf("corrupt stub SessionID = %q, want empty", records[1].SessionID)
	}
}

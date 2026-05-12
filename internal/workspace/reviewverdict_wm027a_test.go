package workspace

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// Tests for ReadReviewVerdict per workspace-model.md §4.7.WM-027a and
// event-model.md §8.1a.3 (bead hk-7om2q.15).
//
// Helper prefix: reviewVerdictFixture (distinct from sibling helpers).

// reviewVerdictFixtureValidJSON returns a JSON byte slice for a valid
// agent-reviewer schema v1 verdict payload.
func reviewVerdictFixtureValidJSON(t *testing.T) []byte {
	t.Helper()
	payload := map[string]interface{}{
		"schema_version": 1,
		"verdict":        "APPROVE",
		"flags":          []string{},
		"notes":          "All changes look correct and tests pass.",
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("reviewVerdictFixtureValidJSON: json.Marshal: %v", err)
	}
	return data
}

// reviewVerdictFixtureWrite writes JSON data to a review.json file inside
// a fresh temp workspace and returns the workspace path.
func reviewVerdictFixtureWrite(t *testing.T, data []byte) string {
	t.Helper()
	workspacePath := t.TempDir()
	harmonikDir := filepath.Join(workspacePath, ".harmonik")
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(harmonikDir, 0o755); err != nil {
		t.Fatalf("reviewVerdictFixtureWrite: MkdirAll: %v", err)
	}
	target := ReviewVerdictPath(workspacePath)
	//nolint:gosec // G306: test fixture; 0644 is appropriate
	if err := os.WriteFile(target, data, 0o644); err != nil {
		t.Fatalf("reviewVerdictFixtureWrite: WriteFile: %v", err)
	}
	return workspacePath
}

// ─────────────────────────────────────────────────────────────────────────────
// ReviewVerdictPath — path helper
// ─────────────────────────────────────────────────────────────────────────────

// TestWM027a_ReviewVerdictPathShape verifies that ReviewVerdictPath returns
// the canonical path per WM-027a: ${workspace_path}/.harmonik/review.json
func TestWM027a_ReviewVerdictPathShape(t *testing.T) {
	t.Parallel()

	workspacePath := "/abs/path/to/worktree"
	got := ReviewVerdictPath(workspacePath)
	want := filepath.Join(workspacePath, ".harmonik", "review.json")

	if got != want {
		t.Errorf("WM-027a: ReviewVerdictPath = %q, want %q", got, want)
	}
}

// TestWM027a_ReviewVerdictPathFilename verifies the filename component is
// exactly "review.json" per WM-027a.
func TestWM027a_ReviewVerdictPathFilename(t *testing.T) {
	t.Parallel()

	got := ReviewVerdictPath("/any/path")
	if filepath.Base(got) != "review.json" {
		t.Errorf("WM-027a: review verdict filename = %q, want review.json", filepath.Base(got))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ReadReviewVerdict — happy path
// ─────────────────────────────────────────────────────────────────────────────

// TestWM027a_ReadReviewVerdictHappyPath verifies that a valid schema v1 file
// is parsed and returns a non-nil ReviewVerdict with all fields intact.
func TestWM027a_ReadReviewVerdictHappyPath(t *testing.T) {
	t.Parallel()

	data := reviewVerdictFixtureValidJSON(t)
	workspacePath := reviewVerdictFixtureWrite(t, data)

	v, err := ReadReviewVerdict(workspacePath)
	if err != nil {
		t.Fatalf("WM-027a: ReadReviewVerdict: %v", err)
	}
	if v == nil {
		t.Fatal("WM-027a: ReadReviewVerdict returned nil verdict; want non-nil")
	}
	if v.SchemaVersion != ReviewVerdictSchemaVersion {
		t.Errorf("WM-027a: SchemaVersion = %d; want %d", v.SchemaVersion, ReviewVerdictSchemaVersion)
	}
	if v.Verdict != ReviewVerdictApprove {
		t.Errorf("WM-027a: Verdict = %q; want %q", v.Verdict, ReviewVerdictApprove)
	}
	if v.Notes == "" {
		t.Error("WM-027a: Notes is empty; want non-empty")
	}
}

// TestWM027a_ReadReviewVerdictRequestChanges verifies REQUEST_CHANGES is accepted.
func TestWM027a_ReadReviewVerdictRequestChanges(t *testing.T) {
	t.Parallel()

	payload := map[string]interface{}{
		"schema_version": 1,
		"verdict":        "REQUEST_CHANGES",
		"flags":          []string{"missing-tests"},
		"notes":          "Tests are missing for the new feature.",
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	workspacePath := reviewVerdictFixtureWrite(t, data)

	v, err := ReadReviewVerdict(workspacePath)
	if err != nil {
		t.Fatalf("WM-027a: ReadReviewVerdict: %v", err)
	}
	if v == nil {
		t.Fatal("WM-027a: ReadReviewVerdict returned nil; want non-nil")
	}
	if v.Verdict != ReviewVerdictRequestChanges {
		t.Errorf("WM-027a: Verdict = %q; want %q", v.Verdict, ReviewVerdictRequestChanges)
	}
	if len(v.Flags) != 1 || v.Flags[0] != "missing-tests" {
		t.Errorf("WM-027a: Flags = %v; want [missing-tests]", v.Flags)
	}
}

// TestWM027a_ReadReviewVerdictBlock verifies BLOCK is accepted.
func TestWM027a_ReadReviewVerdictBlock(t *testing.T) {
	t.Parallel()

	payload := map[string]interface{}{
		"schema_version": 1,
		"verdict":        "BLOCK",
		"flags":          []string{"spec-violation"},
		"notes":          "Changes violate spec requirements.",
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	workspacePath := reviewVerdictFixtureWrite(t, data)

	v, err := ReadReviewVerdict(workspacePath)
	if err != nil {
		t.Fatalf("WM-027a: ReadReviewVerdict: %v", err)
	}
	if v == nil {
		t.Fatal("WM-027a: ReadReviewVerdict returned nil; want non-nil")
	}
	if v.Verdict != ReviewVerdictBlock {
		t.Errorf("WM-027a: Verdict = %q; want %q", v.Verdict, ReviewVerdictBlock)
	}
}

// TestWM027a_ReadReviewVerdictEmptyFlagsAccepted verifies that an empty
// flags array (and a null flags JSON value) are both accepted.
func TestWM027a_ReadReviewVerdictEmptyFlagsAccepted(t *testing.T) {
	t.Parallel()

	for _, flagsJSON := range []string{"[]", "null"} {
		flagsJSON := flagsJSON
		t.Run("flags="+flagsJSON, func(t *testing.T) {
			t.Parallel()

			raw := []byte(`{"schema_version":1,"verdict":"APPROVE","flags":` + flagsJSON + `,"notes":"Looks good."}`)
			workspacePath := reviewVerdictFixtureWrite(t, raw)

			v, err := ReadReviewVerdict(workspacePath)
			if err != nil {
				t.Fatalf("WM-027a: ReadReviewVerdict with flags=%s: %v", flagsJSON, err)
			}
			if v == nil {
				t.Fatalf("WM-027a: ReadReviewVerdict returned nil; want non-nil")
			}
			if v.Flags == nil {
				t.Errorf("WM-027a: Flags is nil after read; want non-nil empty slice")
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ReadReviewVerdict — absent file
// ─────────────────────────────────────────────────────────────────────────────

// TestWM027a_ReadReviewVerdictAbsentReturnsNil verifies that (nil, nil) is
// returned when review.json does not exist (WM-027a §(e) inconclusive condition).
func TestWM027a_ReadReviewVerdictAbsentReturnsNil(t *testing.T) {
	t.Parallel()

	workspacePath := t.TempDir()
	v, err := ReadReviewVerdict(workspacePath)
	if err != nil {
		t.Errorf("WM-027a: ReadReviewVerdict(absent) error = %v; want nil", err)
	}
	if v != nil {
		t.Errorf("WM-027a: ReadReviewVerdict(absent) returned non-nil verdict; want nil")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ReadReviewVerdict — ErrMalformed: schema_version mismatch
// ─────────────────────────────────────────────────────────────────────────────

// TestWM027a_ReadReviewVerdictSchemaVersionMismatch verifies that a file with
// schema_version != 1 returns ErrMalformed.
func TestWM027a_ReadReviewVerdictSchemaVersionMismatch(t *testing.T) {
	t.Parallel()

	payload := map[string]interface{}{
		"schema_version": 2,
		"verdict":        "APPROVE",
		"flags":          []string{},
		"notes":          "Some notes.",
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	workspacePath := reviewVerdictFixtureWrite(t, data)

	_, err = ReadReviewVerdict(workspacePath)
	if err == nil {
		t.Fatal("WM-027a: ReadReviewVerdict with schema_version=2 = nil error; want ErrMalformed")
	}
	if !errors.Is(err, ErrMalformed) {
		t.Errorf("WM-027a: error = %v; want errors.Is(err, ErrMalformed)", err)
	}
}

// TestWM027a_ReadReviewVerdictSchemaVersionMissing verifies that a file with
// no schema_version key returns ErrMalformed.
func TestWM027a_ReadReviewVerdictSchemaVersionMissing(t *testing.T) {
	t.Parallel()

	raw := []byte(`{"verdict":"APPROVE","flags":[],"notes":"Some notes."}`)
	workspacePath := reviewVerdictFixtureWrite(t, raw)

	_, err := ReadReviewVerdict(workspacePath)
	if err == nil {
		t.Fatal("WM-027a: ReadReviewVerdict with missing schema_version = nil error; want ErrMalformed")
	}
	if !errors.Is(err, ErrMalformed) {
		t.Errorf("WM-027a: error = %v; want errors.Is(err, ErrMalformed)", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ReadReviewVerdict — ErrMalformed: unknown verdict
// ─────────────────────────────────────────────────────────────────────────────

// TestWM027a_ReadReviewVerdictUnknownVerdict verifies that an unrecognised
// verdict string returns ErrMalformed.
func TestWM027a_ReadReviewVerdictUnknownVerdict(t *testing.T) {
	t.Parallel()

	for _, bad := range []string{"UNKNOWN", "approve", "request_changes", "", "REJECT"} {
		bad := bad
		t.Run("verdict="+bad, func(t *testing.T) {
			t.Parallel()

			payload := map[string]interface{}{
				"schema_version": 1,
				"verdict":        bad,
				"flags":          []string{},
				"notes":          "Notes.",
			}
			data, err := json.Marshal(payload)
			if err != nil {
				t.Fatalf("json.Marshal: %v", err)
			}
			workspacePath := reviewVerdictFixtureWrite(t, data)

			_, err = ReadReviewVerdict(workspacePath)
			if err == nil {
				t.Fatalf("WM-027a: ReadReviewVerdict with verdict=%q = nil error; want ErrMalformed", bad)
			}
			if !errors.Is(err, ErrMalformed) {
				t.Errorf("WM-027a: error = %v; want errors.Is(err, ErrMalformed)", err)
			}
		})
	}
}

// TestWM027a_ReadReviewVerdictMissingVerdict verifies that a file with no
// verdict key returns ErrMalformed.
func TestWM027a_ReadReviewVerdictMissingVerdict(t *testing.T) {
	t.Parallel()

	raw := []byte(`{"schema_version":1,"flags":[],"notes":"Notes."}`)
	workspacePath := reviewVerdictFixtureWrite(t, raw)

	_, err := ReadReviewVerdict(workspacePath)
	if err == nil {
		t.Fatal("WM-027a: ReadReviewVerdict with missing verdict = nil error; want ErrMalformed")
	}
	if !errors.Is(err, ErrMalformed) {
		t.Errorf("WM-027a: error = %v; want errors.Is(err, ErrMalformed)", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ReadReviewVerdict — ErrMalformed: missing fields
// ─────────────────────────────────────────────────────────────────────────────

// TestWM027a_ReadReviewVerdictMissingFlags verifies that a file with no
// flags key returns ErrMalformed.
func TestWM027a_ReadReviewVerdictMissingFlags(t *testing.T) {
	t.Parallel()

	raw := []byte(`{"schema_version":1,"verdict":"APPROVE","notes":"Notes."}`)
	workspacePath := reviewVerdictFixtureWrite(t, raw)

	_, err := ReadReviewVerdict(workspacePath)
	if err == nil {
		t.Fatal("WM-027a: ReadReviewVerdict with missing flags = nil error; want ErrMalformed")
	}
	if !errors.Is(err, ErrMalformed) {
		t.Errorf("WM-027a: error = %v; want errors.Is(err, ErrMalformed)", err)
	}
}

// TestWM027a_ReadReviewVerdictMissingNotes verifies that a file with no
// notes key returns ErrMalformed.
func TestWM027a_ReadReviewVerdictMissingNotes(t *testing.T) {
	t.Parallel()

	raw := []byte(`{"schema_version":1,"verdict":"APPROVE","flags":[]}`)
	workspacePath := reviewVerdictFixtureWrite(t, raw)

	_, err := ReadReviewVerdict(workspacePath)
	if err == nil {
		t.Fatal("WM-027a: ReadReviewVerdict with missing notes = nil error; want ErrMalformed")
	}
	if !errors.Is(err, ErrMalformed) {
		t.Errorf("WM-027a: error = %v; want errors.Is(err, ErrMalformed)", err)
	}
}

// TestWM027a_ReadReviewVerdictEmptyNotes verifies that a file with an empty
// notes string returns ErrMalformed.
func TestWM027a_ReadReviewVerdictEmptyNotes(t *testing.T) {
	t.Parallel()

	payload := map[string]interface{}{
		"schema_version": 1,
		"verdict":        "APPROVE",
		"flags":          []string{},
		"notes":          "",
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	workspacePath := reviewVerdictFixtureWrite(t, data)

	_, err = ReadReviewVerdict(workspacePath)
	if err == nil {
		t.Fatal("WM-027a: ReadReviewVerdict with empty notes = nil error; want ErrMalformed")
	}
	if !errors.Is(err, ErrMalformed) {
		t.Errorf("WM-027a: error = %v; want errors.Is(err, ErrMalformed)", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ReadReviewVerdict — ErrMalformed: unparseable JSON
// ─────────────────────────────────────────────────────────────────────────────

// TestWM027a_ReadReviewVerdictInvalidJSON verifies that a non-JSON file returns
// ErrMalformed.
func TestWM027a_ReadReviewVerdictInvalidJSON(t *testing.T) {
	t.Parallel()

	workspacePath := reviewVerdictFixtureWrite(t, []byte("not valid json"))

	_, err := ReadReviewVerdict(workspacePath)
	if err == nil {
		t.Fatal("WM-027a: ReadReviewVerdict with invalid JSON = nil error; want ErrMalformed")
	}
	if !errors.Is(err, ErrMalformed) {
		t.Errorf("WM-027a: error = %v; want errors.Is(err, ErrMalformed)", err)
	}
}

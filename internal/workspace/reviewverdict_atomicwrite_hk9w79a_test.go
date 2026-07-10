package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

// Tests for WriteReviewVerdictAtomic (bead hk-9w79a).
//
// Repro: reviewer intermittently wrote invalid JSON to review.json — a
// backtick-containing code snippet in the free-text notes field got a stray
// "\`" backslash-escape (not a legal JSON escape) when the reviewer agent
// hand-typed the JSON via its Write tool. WriteReviewVerdictAtomic replaces
// hand-typed JSON with encoding/json.Marshal, so a backtick in Notes can never
// produce invalid JSON — this test proves the round trip for exactly that
// input shape and would have failed against the old hand-rolled-write path.
//
// Helper prefix: reviewVerdictAtomicFixture (distinct from sibling helpers).

func TestHK9w79a_WriteReviewVerdictAtomic_BacktickInNotesRoundTrips(t *testing.T) {
	t.Parallel()

	workspacePath := t.TempDir()
	notes := "Diff adds `func Foo() error` per the spec; verified against `errors.Is(err, io.EOF)` idiom."
	verdict := &ReviewVerdict{
		SchemaVersion: ReviewVerdictSchemaVersion,
		Verdict:       ReviewVerdictApprove,
		Flags:         []string{},
		Notes:         notes,
	}

	if err := WriteReviewVerdictAtomic(workspacePath, verdict); err != nil {
		t.Fatalf("WriteReviewVerdictAtomic: %v", err)
	}

	got, err := ReadReviewVerdict(workspacePath)
	if err != nil {
		t.Fatalf("ReadReviewVerdict after WriteReviewVerdictAtomic: %v (this is the hk-9w79a failure mode — malformed JSON from a backtick in notes)", err)
	}
	if got == nil {
		t.Fatal("ReadReviewVerdict returned nil verdict after a successful write")
	}
	if got.Notes != notes {
		t.Fatalf("Notes round-trip mismatch: got %q, want %q", got.Notes, notes)
	}
	if got.Verdict != ReviewVerdictApprove {
		t.Fatalf("Verdict round-trip mismatch: got %q, want %q", got.Verdict, ReviewVerdictApprove)
	}
}

func TestHK9w79a_WriteReviewVerdictAtomic_NilFlagsBecomeEmptySlice(t *testing.T) {
	t.Parallel()

	workspacePath := t.TempDir()
	verdict := &ReviewVerdict{
		SchemaVersion: ReviewVerdictSchemaVersion,
		Verdict:       ReviewVerdictRequestChanges,
		Notes:         "No unit tests for the new sentinel set.",
	}

	if err := WriteReviewVerdictAtomic(workspacePath, verdict); err != nil {
		t.Fatalf("WriteReviewVerdictAtomic: %v", err)
	}

	got, err := ReadReviewVerdict(workspacePath)
	if err != nil {
		t.Fatalf("ReadReviewVerdict: %v", err)
	}
	if got == nil {
		t.Fatal("ReadReviewVerdict returned nil verdict after a successful write")
	}
	if got.Flags == nil || len(got.Flags) != 0 {
		t.Fatalf("Flags = %v, want empty non-nil slice", got.Flags)
	}
}

func TestHK9w79a_WriteReviewVerdictAtomic_NoTempFileLeftBehind(t *testing.T) {
	t.Parallel()

	workspacePath := t.TempDir()
	verdict := &ReviewVerdict{
		SchemaVersion: ReviewVerdictSchemaVersion,
		Verdict:       ReviewVerdictBlock,
		Flags:         []string{"spec-divergence"},
		Notes:         "HC-020 Class() must return a typed alias.",
	}

	if err := WriteReviewVerdictAtomic(workspacePath, verdict); err != nil {
		t.Fatalf("WriteReviewVerdictAtomic: %v", err)
	}

	entries, err := os.ReadDir(filepath.Join(workspacePath, ".harmonik"))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, entry := range entries {
		if entry.Name() != "review.json" {
			t.Fatalf("unexpected leftover file %q in .harmonik/ after WriteReviewVerdictAtomic", entry.Name())
		}
	}
}

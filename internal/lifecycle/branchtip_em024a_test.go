package lifecycle

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
)

// --- Fixtures (prefix: monotonicityFixture) ---

// monotonicityFixtureRunID returns a deterministic, valid UUIDv7-shaped run ID
// string for use in EM-024a monotonicity tests. Uses the same format as
// durableFixtureRunID so fixture helpers (durableFixtureCreateTaskBranch, etc.)
// accept the result directly.
func monotonicityFixtureRunID(n int) string {
	return durableFixtureRunID(100 + n)
}

// monotonicityFixtureParseRunID parses a raw UUID string into a core.RunID,
// failing the test on parse error.
func monotonicityFixtureParseRunID(t *testing.T, raw string) core.RunID {
	t.Helper()
	var id core.RunID
	if err := id.UnmarshalText([]byte(raw)); err != nil {
		t.Fatalf("monotonicityFixtureParseRunID(%q): %v", raw, err)
	}
	return id
}

// monotonicityFixtureRepoAndProject creates:
//   - a git repository in a temp dir (repoDir) with a root commit on main
//   - a project dir (projectDir) where .harmonik/run-tips/ tip files land
//
// Returns (repoDir, projectDir). Both are sub-directories of t.TempDir().
func monotonicityFixtureRepoAndProject(t *testing.T) (repoDir, projectDir string) {
	t.Helper()
	base := t.TempDir()
	repoDir = filepath.Join(base, "repo")
	projectDir = filepath.Join(base, "project")
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("monotonicityFixtureRepoAndProject: mkdir repo: %v", err)
	}
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("monotonicityFixtureRepoAndProject: mkdir project: %v", err)
	}
	durableFixtureInitRepo(t, repoDir)
	return repoDir, projectDir
}

// --- Tests for EM-024a: WritePersistedTip / ReadPersistedTip ---

// TestEM024a_WriteReadPersistedTip verifies that WritePersistedTip persists a
// tip SHA and ReadPersistedTip retrieves it correctly.
//
// Spec ref: execution-model.md §4.5 EM-024a — daemon MUST persist the
// last-observed task-branch-tip SHA per in-flight run.
func TestEM024a_WriteReadPersistedTip(t *testing.T) {
	t.Parallel()

	_, projectDir := monotonicityFixtureRepoAndProject(t)
	runID := monotonicityFixtureParseRunID(t, monotonicityFixtureRunID(1))

	const tipSHA = "aabbccddeeff00112233445566778899aabbccdd"

	if err := WritePersistedTip(projectDir, runID, tipSHA); err != nil {
		t.Fatalf("WritePersistedTip: %v", err)
	}

	got, err := ReadPersistedTip(projectDir, runID)
	if err != nil {
		t.Fatalf("ReadPersistedTip: %v", err)
	}
	if got != tipSHA {
		t.Errorf("ReadPersistedTip: got %q, want %q", got, tipSHA)
	}
}

// TestEM024a_ReadPersistedTip_MissingFile verifies that ReadPersistedTip
// returns an empty string (not an error) when no tip file exists for the run.
//
// Spec ref: execution-model.md §4.5 EM-024a — "A missing prior-tip file for
// a run observed for the first time is NOT a violation."
func TestEM024a_ReadPersistedTip_MissingFile(t *testing.T) {
	t.Parallel()

	_, projectDir := monotonicityFixtureRepoAndProject(t)
	runID := monotonicityFixtureParseRunID(t, monotonicityFixtureRunID(2))

	got, err := ReadPersistedTip(projectDir, runID)
	if err != nil {
		t.Fatalf("ReadPersistedTip on missing file: unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("ReadPersistedTip on missing file: got %q, want empty string", got)
	}
}

// TestEM024a_WritePersistedTip_CreatesDir verifies that WritePersistedTip
// creates the .harmonik/run-tips/ directory if it does not yet exist.
func TestEM024a_WritePersistedTip_CreatesDir(t *testing.T) {
	t.Parallel()

	_, projectDir := monotonicityFixtureRepoAndProject(t)
	runID := monotonicityFixtureParseRunID(t, monotonicityFixtureRunID(3))

	// Verify the run-tips dir does not exist yet.
	tipsDir := filepath.Join(projectDir, ".harmonik", "run-tips")
	if _, statErr := os.Stat(tipsDir); !os.IsNotExist(statErr) {
		t.Fatalf("precondition: run-tips dir should not exist yet")
	}

	const tipSHA = "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	if err := WritePersistedTip(projectDir, runID, tipSHA); err != nil {
		t.Fatalf("WritePersistedTip: %v", err)
	}

	// run-tips dir must now exist.
	if _, statErr := os.Stat(tipsDir); statErr != nil {
		t.Errorf("run-tips dir was not created: %v", statErr)
	}

	// Tip file must exist at <run-tips>/<run_id>.
	tipPath := filepath.Join(tipsDir, runID.String())
	if _, statErr := os.Stat(tipPath); statErr != nil {
		t.Errorf("tip file not created at %s: %v", tipPath, statErr)
	}
}

// TestEM024a_WritePersistedTip_EmptySHA verifies that WritePersistedTip
// rejects an empty tipSHA with an error.
func TestEM024a_WritePersistedTip_EmptySHA(t *testing.T) {
	t.Parallel()

	_, projectDir := monotonicityFixtureRepoAndProject(t)
	runID := monotonicityFixtureParseRunID(t, monotonicityFixtureRunID(4))

	err := WritePersistedTip(projectDir, runID, "")
	if err == nil {
		t.Fatal("WritePersistedTip with empty SHA: expected error, got nil")
	}
}

// TestEM024a_WritePersistedTip_OverwritesPreviousTip verifies that a second
// call to WritePersistedTip overwrites the prior value (idempotent update).
func TestEM024a_WritePersistedTip_OverwritesPreviousTip(t *testing.T) {
	t.Parallel()

	_, projectDir := monotonicityFixtureRepoAndProject(t)
	runID := monotonicityFixtureParseRunID(t, monotonicityFixtureRunID(5))

	const sha1 = "1111111111111111111111111111111111111111"
	const sha2 = "2222222222222222222222222222222222222222"

	if err := WritePersistedTip(projectDir, runID, sha1); err != nil {
		t.Fatalf("WritePersistedTip(sha1): %v", err)
	}
	if err := WritePersistedTip(projectDir, runID, sha2); err != nil {
		t.Fatalf("WritePersistedTip(sha2): %v", err)
	}

	got, err := ReadPersistedTip(projectDir, runID)
	if err != nil {
		t.Fatalf("ReadPersistedTip: %v", err)
	}
	if got != sha2 {
		t.Errorf("ReadPersistedTip after overwrite: got %q, want %q", got, sha2)
	}
}

// --- Tests for IsFastForwardDescendant ---

// TestEM024a_IsFastForwardDescendant_LinearChain verifies that for a linear
// commit chain A → B → C, A is an ancestor of B and C, B is an ancestor of C,
// but C is NOT an ancestor of A.
//
// Spec ref: execution-model.md §4.5 EM-024a — "the prior tip is in the
// ancestor chain of the new tip."
func TestEM024a_IsFastForwardDescendant_LinearChain(t *testing.T) {
	t.Parallel()

	repoDir, _ := monotonicityFixtureRepoAndProject(t)
	raw := monotonicityFixtureRunID(10)
	durableFixtureCreateTaskBranch(t, repoDir, raw)

	shaA := durableFixtureCommitCheckpoint(t, repoDir, raw, "node-a")
	shaB := durableFixtureCommitCheckpoint(t, repoDir, raw, "node-b")
	shaC := durableFixtureCommitCheckpoint(t, repoDir, raw, "node-c")

	ctx := t.Context()

	// A is an ancestor of B.
	ok, err := IsFastForwardDescendant(ctx, repoDir, shaA, shaB)
	if err != nil {
		t.Fatalf("IsFastForwardDescendant(A, B): %v", err)
	}
	if !ok {
		t.Errorf("IsFastForwardDescendant(A, B): got false, want true")
	}

	// A is an ancestor of C (transitivity).
	ok, err = IsFastForwardDescendant(ctx, repoDir, shaA, shaC)
	if err != nil {
		t.Fatalf("IsFastForwardDescendant(A, C): %v", err)
	}
	if !ok {
		t.Errorf("IsFastForwardDescendant(A, C): got false, want true")
	}

	// B is an ancestor of C.
	ok, err = IsFastForwardDescendant(ctx, repoDir, shaB, shaC)
	if err != nil {
		t.Fatalf("IsFastForwardDescendant(B, C): %v", err)
	}
	if !ok {
		t.Errorf("IsFastForwardDescendant(B, C): got false, want true")
	}

	// C is NOT an ancestor of A (reverse direction — rewind).
	ok, err = IsFastForwardDescendant(ctx, repoDir, shaC, shaA)
	if err != nil {
		t.Fatalf("IsFastForwardDescendant(C, A): %v", err)
	}
	if ok {
		t.Errorf("IsFastForwardDescendant(C, A): got true, want false (C is not an ancestor of A)")
	}
}

// --- Tests for CheckBranchTipMonotonicity ---

// TestEM024a_CheckMonotonicity_FirstObservation verifies that the first call
// to CheckBranchTipMonotonicity with no prior tip file initializes the file
// and returns nil (not a violation).
//
// Spec ref: execution-model.md §4.5 EM-024a — "A missing prior-tip file for
// a run observed for the first time is NOT a violation."
func TestEM024a_CheckMonotonicity_FirstObservation(t *testing.T) {
	t.Parallel()

	repoDir, projectDir := monotonicityFixtureRepoAndProject(t)
	raw := monotonicityFixtureRunID(20)
	runID := monotonicityFixtureParseRunID(t, raw)

	durableFixtureCreateTaskBranch(t, repoDir, raw)
	tipSHA := durableFixtureCommitCheckpoint(t, repoDir, raw, "node-a")

	if err := CheckBranchTipMonotonicity(t.Context(), repoDir, projectDir, runID, tipSHA); err != nil {
		t.Fatalf("CheckBranchTipMonotonicity (first observation): unexpected error: %v", err)
	}

	// Tip file must now exist with the correct SHA.
	got, err := ReadPersistedTip(projectDir, runID)
	if err != nil {
		t.Fatalf("ReadPersistedTip after first observation: %v", err)
	}
	if got != tipSHA {
		t.Errorf("persisted tip after first observation: got %q, want %q", got, tipSHA)
	}
}

// TestEM024a_CheckMonotonicity_NormalAdvance verifies that a subsequent call
// with a fast-forward tip succeeds and updates the persisted tip.
//
// Spec ref: execution-model.md §4.5 EM-024a — normal fast-forward advance.
func TestEM024a_CheckMonotonicity_NormalAdvance(t *testing.T) {
	t.Parallel()

	repoDir, projectDir := monotonicityFixtureRepoAndProject(t)
	raw := monotonicityFixtureRunID(21)
	runID := monotonicityFixtureParseRunID(t, raw)

	durableFixtureCreateTaskBranch(t, repoDir, raw)
	sha1 := durableFixtureCommitCheckpoint(t, repoDir, raw, "node-a")
	sha2 := durableFixtureCommitCheckpoint(t, repoDir, raw, "node-b")

	// Initialize with sha1.
	if err := CheckBranchTipMonotonicity(t.Context(), repoDir, projectDir, runID, sha1); err != nil {
		t.Fatalf("CheckBranchTipMonotonicity (init): %v", err)
	}

	// Advance to sha2 (fast-forward).
	if err := CheckBranchTipMonotonicity(t.Context(), repoDir, projectDir, runID, sha2); err != nil {
		t.Fatalf("CheckBranchTipMonotonicity (advance): unexpected error: %v", err)
	}

	// Persisted tip must be sha2.
	got, err := ReadPersistedTip(projectDir, runID)
	if err != nil {
		t.Fatalf("ReadPersistedTip: %v", err)
	}
	if got != sha2 {
		t.Errorf("persisted tip after advance: got %q, want %q", got, sha2)
	}
}

// TestEM024a_CheckMonotonicity_SameTip verifies that calling
// CheckBranchTipMonotonicity with the same tip as the persisted prior tip is
// idempotent and returns nil.
func TestEM024a_CheckMonotonicity_SameTip(t *testing.T) {
	t.Parallel()

	repoDir, projectDir := monotonicityFixtureRepoAndProject(t)
	raw := monotonicityFixtureRunID(22)
	runID := monotonicityFixtureParseRunID(t, raw)

	durableFixtureCreateTaskBranch(t, repoDir, raw)
	sha1 := durableFixtureCommitCheckpoint(t, repoDir, raw, "node-a")

	// Initialize.
	if err := CheckBranchTipMonotonicity(t.Context(), repoDir, projectDir, runID, sha1); err != nil {
		t.Fatalf("CheckBranchTipMonotonicity (init): %v", err)
	}

	// Re-check with the same tip — must be idempotent (no error, tip unchanged).
	if err := CheckBranchTipMonotonicity(t.Context(), repoDir, projectDir, runID, sha1); err != nil {
		t.Fatalf("CheckBranchTipMonotonicity (same tip): unexpected error: %v", err)
	}

	got, err := ReadPersistedTip(projectDir, runID)
	if err != nil {
		t.Fatalf("ReadPersistedTip: %v", err)
	}
	if got != sha1 {
		t.Errorf("persisted tip after same-tip re-check: got %q, want %q", got, sha1)
	}
}

// TestEM024a_CheckMonotonicity_RewindDetected verifies that
// CheckBranchTipMonotonicity returns ErrBranchTipRewound when the new tip is
// not a fast-forward descendant of the persisted prior tip, simulating an
// external force-push or git reset --hard.
//
// The test also asserts that the persisted tip is NOT updated on a rewind.
//
// Spec ref: execution-model.md §4.5 EM-024a — "Non-fast-forward → route to
// RC §8.4 Cat 3; MUST NOT advance run against new tip."
func TestEM024a_CheckMonotonicity_RewindDetected(t *testing.T) {
	t.Parallel()

	repoDir, projectDir := monotonicityFixtureRepoAndProject(t)

	// Create two independent branches from the same root commit so their tips
	// are not ancestry-related — simulating a force-push to an unrelated SHA.
	rawA := monotonicityFixtureRunID(30)
	rawB := monotonicityFixtureRunID(31)

	durableFixtureCreateTaskBranch(t, repoDir, rawA)
	shaA := durableFixtureCommitCheckpoint(t, repoDir, rawA, "node-a")

	runGitRepo(t, repoDir, "checkout", "main")
	durableFixtureCreateTaskBranch(t, repoDir, rawB)
	shaB := durableFixtureCommitCheckpoint(t, repoDir, rawB, "node-x")

	// Initialize run A's persisted tip to shaA.
	runID := monotonicityFixtureParseRunID(t, rawA)
	if err := CheckBranchTipMonotonicity(t.Context(), repoDir, projectDir, runID, shaA); err != nil {
		t.Fatalf("CheckBranchTipMonotonicity (init): %v", err)
	}

	// Present shaB (from an unrelated branch) as the "new tip" — rewind simulation.
	err := CheckBranchTipMonotonicity(t.Context(), repoDir, projectDir, runID, shaB)
	if !errors.Is(err, ErrBranchTipRewound) {
		t.Fatalf("CheckBranchTipMonotonicity (rewind): got %v, want ErrBranchTipRewound", err)
	}

	// Persisted tip must still be shaA — NOT updated to shaB.
	got, readErr := ReadPersistedTip(projectDir, runID)
	if readErr != nil {
		t.Fatalf("ReadPersistedTip: %v", readErr)
	}
	if got != shaA {
		t.Errorf("persisted tip after rewind: got %q, want %q (tip MUST NOT be updated on rewind)", got, shaA)
	}
}

// TestEM024a_CheckMonotonicity_MultiRunIsolation verifies that persisted tips
// for distinct runs are stored independently and do not interfere.
//
// Spec ref: execution-model.md §4.5 EM-024a — "daemon MUST persist, per
// in-flight run" (per-run file isolation).
func TestEM024a_CheckMonotonicity_MultiRunIsolation(t *testing.T) {
	t.Parallel()

	repoDir, projectDir := monotonicityFixtureRepoAndProject(t)

	rawA := monotonicityFixtureRunID(40)
	rawB := monotonicityFixtureRunID(41)
	runIDA := monotonicityFixtureParseRunID(t, rawA)
	runIDB := monotonicityFixtureParseRunID(t, rawB)

	durableFixtureCreateTaskBranch(t, repoDir, rawA)
	shaA1 := durableFixtureCommitCheckpoint(t, repoDir, rawA, "node-a1")
	shaA2 := durableFixtureCommitCheckpoint(t, repoDir, rawA, "node-a2")

	runGitRepo(t, repoDir, "checkout", "main")
	durableFixtureCreateTaskBranch(t, repoDir, rawB)
	shaB1 := durableFixtureCommitCheckpoint(t, repoDir, rawB, "node-b1")

	// Initialize both runs.
	if err := CheckBranchTipMonotonicity(t.Context(), repoDir, projectDir, runIDA, shaA1); err != nil {
		t.Fatalf("init run A: %v", err)
	}
	if err := CheckBranchTipMonotonicity(t.Context(), repoDir, projectDir, runIDB, shaB1); err != nil {
		t.Fatalf("init run B: %v", err)
	}

	// Advance run A to shaA2.
	if err := CheckBranchTipMonotonicity(t.Context(), repoDir, projectDir, runIDA, shaA2); err != nil {
		t.Fatalf("advance run A: %v", err)
	}

	// Run B's persisted tip must still be shaB1.
	gotB, err := ReadPersistedTip(projectDir, runIDB)
	if err != nil {
		t.Fatalf("ReadPersistedTip run B: %v", err)
	}
	if gotB != shaB1 {
		t.Errorf("run B persisted tip after advancing run A: got %q, want %q", gotB, shaB1)
	}

	// Run A's persisted tip must be shaA2.
	gotA, err := ReadPersistedTip(projectDir, runIDA)
	if err != nil {
		t.Fatalf("ReadPersistedTip run A: %v", err)
	}
	if gotA != shaA2 {
		t.Errorf("run A persisted tip: got %q, want %q", gotA, shaA2)
	}
}

// TestEM024a_CheckMonotonicity_ErrorMsgContainsDiagInfo verifies that the
// ErrBranchTipRewound error message contains the run ID, prior SHA, and new
// SHA for operator diagnosability.
func TestEM024a_CheckMonotonicity_ErrorMsgContainsDiagInfo(t *testing.T) {
	t.Parallel()

	repoDir, projectDir := monotonicityFixtureRepoAndProject(t)

	rawA := monotonicityFixtureRunID(50)
	rawB := monotonicityFixtureRunID(51)

	durableFixtureCreateTaskBranch(t, repoDir, rawA)
	shaA := durableFixtureCommitCheckpoint(t, repoDir, rawA, "node-a")

	runGitRepo(t, repoDir, "checkout", "main")
	durableFixtureCreateTaskBranch(t, repoDir, rawB)
	shaB := durableFixtureCommitCheckpoint(t, repoDir, rawB, "node-x")

	runID := monotonicityFixtureParseRunID(t, rawA)
	if err := CheckBranchTipMonotonicity(t.Context(), repoDir, projectDir, runID, shaA); err != nil {
		t.Fatalf("init: %v", err)
	}

	err := CheckBranchTipMonotonicity(t.Context(), repoDir, projectDir, runID, shaB)
	if err == nil {
		t.Fatal("expected ErrBranchTipRewound, got nil")
	}

	msg := err.Error()
	if !strings.Contains(msg, rawA) {
		t.Errorf("error message missing run ID %q: %s", rawA, msg)
	}
	if !strings.Contains(msg, shaA) {
		t.Errorf("error message missing prior SHA %q: %s", shaA, msg)
	}
	if !strings.Contains(msg, shaB) {
		t.Errorf("error message missing new SHA %q: %s", shaB, msg)
	}
}

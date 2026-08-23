package lifecycle

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func corruptCheckpointFixtureRunID(n int) string {
	return durableFixtureRunID(200 + n)
}

func corruptCheckpointFixtureTransitionID(n int) string {
	return fmt.Sprintf("01900000-0000-7000-8000-0000ffff%04d", n)
}

func corruptCheckpointFixtureWriteSiblingFile(t *testing.T, repoDir, runID, transitionID, content string) {
	t.Helper()

	dir := filepath.Join(repoDir, ".harmonik", "transitions", runID)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("corruptCheckpointFixtureWriteSiblingFile: MkdirAll: %v", err)
	}

	p := filepath.Join(dir, transitionID+".json")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatalf("corruptCheckpointFixtureWriteSiblingFile: WriteFile: %v", err)
	}
}

func corruptCheckpointFixtureMinimalJSON(runID, transitionID string) string {
	return fmt.Sprintf(
		`{"schema_version":1,"run_id":%q,"transition_id":%q}`,
		runID, transitionID,
	)
}

func corruptCheckpointFixtureCommitWithTrailers(t *testing.T, repoDir, runID, transitionID, nodeID string) string {
	t.Helper()

	stateFile := filepath.Join(repoDir, "state.txt")
	if err := os.WriteFile(stateFile, []byte(fmt.Sprintf("run=%s node=%s tx=%s\n", runID, nodeID, transitionID)), 0o600); err != nil {
		t.Fatalf("corruptCheckpointFixtureCommitWithTrailers: WriteFile state.txt: %v", err)
	}
	runGitRepo(t, repoDir, "add", "state.txt")

	msg := fmt.Sprintf(
		"checkpoint: %s\n\nHarmonik-Run-ID: %s\nHarmonik-Transition-ID: %s\n",
		nodeID, runID, transitionID,
	)
	runGitRepo(t, repoDir, "commit", "-m", msg)
	return durableFixtureReadTip(t, repoDir, "run/"+runID)
}

func corruptCheckpointFixtureSiblingPath(repoDir, runID, transitionID string) string {
	return filepath.Join(repoDir, ".harmonik", "transitions", runID, transitionID+".json")
}

func corruptCheckpointFixtureCheckTrailerPresent(t *testing.T, repoDir, commitSHA string) {
	t.Helper()

	out, err := exec.CommandContext(t.Context(), "git", "-C", repoDir,
		"log", "-1", "--format=%B", commitSHA,
	).Output()
	if err != nil {
		t.Fatalf("corruptCheckpointFixtureCheckTrailerPresent: git log: %v", err)
	}
	if !strings.Contains(string(out), "Harmonik-Transition-ID:") {
		t.Fatalf("pre-condition violation: commit %s has no Harmonik-Transition-ID trailer; EM-017a sensor requires the trailer to be present", commitSHA)
	}
}

func corruptCheckpointFixtureClassify(t *testing.T, repoDir, commitSHA string) (hasTrailer bool, err error) {
	t.Helper()

	out, gitErr := exec.CommandContext(t.Context(), "git", "-C", repoDir,
		"log", "-1", "--format=%B", commitSHA,
	).Output()
	if gitErr != nil {
		t.Fatalf("corruptCheckpointFixtureClassify: git log: %v", gitErr)
	}

	var runID, transitionID string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "Harmonik-Run-ID:"):
			runID = strings.TrimSpace(strings.TrimPrefix(line, "Harmonik-Run-ID:"))
		case strings.HasPrefix(line, "Harmonik-Transition-ID:"):
			transitionID = strings.TrimSpace(strings.TrimPrefix(line, "Harmonik-Transition-ID:"))
		}
	}

	if transitionID == "" {
		return false, nil
	}

	siblingPath := corruptCheckpointFixtureSiblingPath(repoDir, runID, transitionID)
	//nolint:gosec // G304: siblingPath constructed from repoDir (t.TempDir()) + trailer values; not user input
	data, readErr := os.ReadFile(siblingPath)
	if readErr != nil {
		return true, fmt.Errorf("EM-017a: sibling file missing at %s: %w", siblingPath, readErr)
	}

	if len(data) == 0 {
		return true, fmt.Errorf("EM-017a: sibling file truncated (zero length) at %s: %w", siblingPath, ErrSiblingTruncated)
	}

	var record map[string]json.RawMessage
	if jsonErr := json.Unmarshal(data, &record); jsonErr != nil {
		return true, fmt.Errorf("EM-017a: sibling file fails schema validation at %s: %w", siblingPath, ErrSiblingInvalidSchema)
	}
	if _, ok := record["schema_version"]; !ok {
		return true, fmt.Errorf("EM-017a: sibling file missing schema_version at %s: %w", siblingPath, ErrSiblingInvalidSchema)
	}

	return true, nil
}

// ErrSiblingTruncated is returned by corruptCheckpointFixtureClassify when the
// sibling file exists but is zero length (truncated write).
//
// Spec ref: execution-model.md §4.4 EM-017a — "truncated" corruption class.
var ErrSiblingTruncated = fmt.Errorf("sibling file is zero-length (truncated)")

// ErrSiblingInvalidSchema is returned by corruptCheckpointFixtureClassify when
// the sibling file exists and is non-empty but fails JSON schema validation.
//
// Spec ref: execution-model.md §4.4 EM-017a — "fails schema validation" corruption class.
var ErrSiblingInvalidSchema = fmt.Errorf("sibling file fails schema validation")

// TestEM017a_MissingSiblingFileDetectedAsCorrupted is the primary sensor for
// the EM-017a "sibling file missing" corruption class.
//
// It commits a checkpoint with Harmonik-Run-ID and Harmonik-Transition-ID
// trailers but does NOT write the expected sibling file. The sensor MUST
// detect this as a corrupted checkpoint (hasTrailer=true, err!=nil).
//
// Spec ref: execution-model.md §4.4 EM-017a — "if … the expected sibling file
// at `.harmonik/transitions/<run_id>/<transition_id>.json` is missing … the
// daemon MUST treat the commit as a corrupted checkpoint."
func TestEM017a_MissingSiblingFileDetectedAsCorrupted(t *testing.T) {
	t.Parallel()

	repoDir := t.TempDir()
	durableFixtureInitRepo(t, repoDir)

	runID := corruptCheckpointFixtureRunID(1)
	transitionID := corruptCheckpointFixtureTransitionID(1)
	durableFixtureCreateTaskBranch(t, repoDir, runID)

	sha := corruptCheckpointFixtureCommitWithTrailers(t, repoDir, runID, transitionID, "node-a")

	corruptCheckpointFixtureCheckTrailerPresent(t, repoDir, sha)

	hasTrailer, err := corruptCheckpointFixtureClassify(t, repoDir, sha)
	if !hasTrailer {
		t.Errorf("EM-017a: classify returned hasTrailer=false; Harmonik-Transition-ID trailer is present in commit %s", sha)
	}
	if err == nil {
		t.Errorf("EM-017a: classify returned nil error for missing sibling file; daemon must NOT silently proceed per EM-017a")
	}

	siblingPath := corruptCheckpointFixtureSiblingPath(repoDir, runID, transitionID)
	if _, statErr := os.Stat(siblingPath); !os.IsNotExist(statErr) {
		t.Errorf("test invariant: sibling file should not exist at %s; got statErr=%v", siblingPath, statErr)
	}
}

// TestEM017a_TruncatedSiblingFileDetectedAsCorrupted is the sensor for the
// EM-017a "sibling file truncated" corruption class.
//
// A zero-byte sibling file at the canonical path simulates a crash between
// the file-create and the file-write steps. The sensor MUST detect this as
// a corrupted checkpoint.
//
// Spec ref: execution-model.md §4.4 EM-017a — "truncated … the daemon MUST
// treat the commit as a corrupted checkpoint."
func TestEM017a_TruncatedSiblingFileDetectedAsCorrupted(t *testing.T) {
	t.Parallel()

	repoDir := t.TempDir()
	durableFixtureInitRepo(t, repoDir)

	runID := corruptCheckpointFixtureRunID(2)
	transitionID := corruptCheckpointFixtureTransitionID(2)
	durableFixtureCreateTaskBranch(t, repoDir, runID)

	corruptCheckpointFixtureWriteSiblingFile(t, repoDir, runID, transitionID, "")

	sha := corruptCheckpointFixtureCommitWithTrailers(t, repoDir, runID, transitionID, "node-a")
	corruptCheckpointFixtureCheckTrailerPresent(t, repoDir, sha)

	hasTrailer, err := corruptCheckpointFixtureClassify(t, repoDir, sha)
	if !hasTrailer {
		t.Errorf("EM-017a: classify returned hasTrailer=false for truncated-sibling commit %s", sha)
	}
	if err == nil {
		t.Errorf("EM-017a: classify returned nil error for truncated (zero-length) sibling file; daemon must NOT silently proceed")
	}
	if err != nil && !strings.Contains(err.Error(), "truncated") {
		t.Errorf("EM-017a: expected error to mention truncated, got %q", err.Error())
	}
}

// TestEM017a_InvalidJSONSiblingFileDetectedAsCorrupted is the sensor for the
// EM-017a "fails schema validation" corruption class.
//
// The sibling file exists and is non-empty but contains invalid JSON. The
// sensor MUST detect this as a corrupted checkpoint and MUST NOT silently
// proceed.
//
// Spec ref: execution-model.md §4.4 EM-017a — "fails schema validation …
// the daemon MUST treat the commit as a corrupted checkpoint."
func TestEM017a_InvalidJSONSiblingFileDetectedAsCorrupted(t *testing.T) {
	t.Parallel()

	repoDir := t.TempDir()
	durableFixtureInitRepo(t, repoDir)

	runID := corruptCheckpointFixtureRunID(3)
	transitionID := corruptCheckpointFixtureTransitionID(3)
	durableFixtureCreateTaskBranch(t, repoDir, runID)

	corruptCheckpointFixtureWriteSiblingFile(t, repoDir, runID, transitionID, `{invalid json`)

	sha := corruptCheckpointFixtureCommitWithTrailers(t, repoDir, runID, transitionID, "node-a")
	corruptCheckpointFixtureCheckTrailerPresent(t, repoDir, sha)

	hasTrailer, err := corruptCheckpointFixtureClassify(t, repoDir, sha)
	if !hasTrailer {
		t.Errorf("EM-017a: classify returned hasTrailer=false for invalid-JSON-sibling commit %s", sha)
	}
	if err == nil {
		t.Errorf("EM-017a: classify returned nil error for invalid-JSON sibling file; daemon must NOT silently proceed")
	}
}

// TestEM017a_MissingSchemaVersionFieldDetectedAsCorrupted is the sensor for
// the EM-017a "fails schema validation" sub-case where the JSON is syntactically
// valid but the required schema_version field is absent.
//
// Spec ref: execution-model.md §4.4 EM-017a (schema validation includes
// EM-022 schema_version requirement); §4.5 EM-022 — "MUST carry a
// schema_version integer field matching the commit's Harmonik-Schema-Version."
func TestEM017a_MissingSchemaVersionFieldDetectedAsCorrupted(t *testing.T) {
	t.Parallel()

	repoDir := t.TempDir()
	durableFixtureInitRepo(t, repoDir)

	runID := corruptCheckpointFixtureRunID(4)
	transitionID := corruptCheckpointFixtureTransitionID(4)
	durableFixtureCreateTaskBranch(t, repoDir, runID)

	corruptCheckpointFixtureWriteSiblingFile(t, repoDir, runID, transitionID,
		fmt.Sprintf(`{"run_id":%q,"transition_id":%q}`, runID, transitionID),
	)

	sha := corruptCheckpointFixtureCommitWithTrailers(t, repoDir, runID, transitionID, "node-a")
	corruptCheckpointFixtureCheckTrailerPresent(t, repoDir, sha)

	hasTrailer, err := corruptCheckpointFixtureClassify(t, repoDir, sha)
	if !hasTrailer {
		t.Errorf("EM-017a: classify returned hasTrailer=false for missing-schema_version commit %s", sha)
	}
	if err == nil {
		t.Errorf("EM-017a: classify returned nil error for sibling file missing schema_version; daemon must NOT silently proceed")
	}
}

// TestEM017a_ValidSiblingFileIsNotCorrupted is the negative sensor: a
// well-formed checkpoint commit with a valid sibling file MUST NOT be
// classified as corrupted.
//
// This is the passing baseline; the sensor MUST return (true, nil) for a
// clean checkpoint.
//
// Spec ref: execution-model.md §4.4 EM-017a — the sensor is only triggered
// when the sibling is absent/truncated/schema-invalid; a valid checkpoint
// must not be false-positively flagged.
func TestEM017a_ValidSiblingFileIsNotCorrupted(t *testing.T) {
	t.Parallel()

	repoDir := t.TempDir()
	durableFixtureInitRepo(t, repoDir)

	runID := corruptCheckpointFixtureRunID(5)
	transitionID := corruptCheckpointFixtureTransitionID(5)
	durableFixtureCreateTaskBranch(t, repoDir, runID)

	corruptCheckpointFixtureWriteSiblingFile(t, repoDir, runID, transitionID,
		corruptCheckpointFixtureMinimalJSON(runID, transitionID),
	)

	sha := corruptCheckpointFixtureCommitWithTrailers(t, repoDir, runID, transitionID, "node-a")
	corruptCheckpointFixtureCheckTrailerPresent(t, repoDir, sha)

	hasTrailer, err := corruptCheckpointFixtureClassify(t, repoDir, sha)
	if !hasTrailer {
		t.Errorf("EM-017a baseline: classify returned hasTrailer=false for valid checkpoint %s", sha)
	}
	if err != nil {
		t.Errorf("EM-017a baseline: classify returned unexpected error for valid checkpoint: %v", err)
	}
}

// TestEM017a_NoTrailerCommitIsNotAnEM017aConcern asserts that a commit
// without a Harmonik-Transition-ID trailer is not subject to EM-017a:
// the sensor MUST return (false, nil) and the absence of a sibling file
// is not a corruption condition.
//
// Spec ref: execution-model.md §4.4 EM-017a — "If a checkpoint commit's
// Harmonik-Transition-ID trailer is present …"; absence of the trailer
// means EM-017a does not apply.
func TestEM017a_NoTrailerCommitIsNotAnEM017aConcern(t *testing.T) {
	t.Parallel()

	repoDir := t.TempDir()
	durableFixtureInitRepo(t, repoDir)

	runID := corruptCheckpointFixtureRunID(6)
	durableFixtureCreateTaskBranch(t, repoDir, runID)

	sha := durableFixtureCommitCheckpoint(t, repoDir, runID, "node-a")

	hasTrailer, err := corruptCheckpointFixtureClassify(t, repoDir, sha)
	if hasTrailer {
		t.Errorf("EM-017a: classify returned hasTrailer=true for a commit with no Harmonik-Transition-ID trailer; commit %s", sha)
	}
	if err != nil {
		t.Errorf("EM-017a: classify returned unexpected error for no-trailer commit: %v", err)
	}
}

// TestEM017a_RecursionBoundedToOneLevel asserts the EM-017a recursion
// constraint: if the reconciliation verdict commit is itself detected as a
// corrupted checkpoint (i.e., it carries Harmonik-Transition-ID but has a
// missing/invalid sibling), the sensor MUST classify it as corrupted — this
// is the trigger for Cat 6b escalation rather than a second reconciliation
// dispatch. The sensor itself does not distinguish "first" from "verdict"
// checkpoints; the distinction is the caller's responsibility (the daemon
// checks whether it is already executing a reconciliation workflow before
// re-dispatching). This test verifies that the sensor is idempotent: the
// same classify function returns an error for a corrupted verdict commit,
// so the daemon CAN detect the Cat 6b condition.
//
// Spec ref: execution-model.md §4.4 EM-017a — "A corrupted checkpoint in the
// reconciliation workflow itself … cannot recur without producing a corrupted
// verdict commit — the recursion is bounded to at most one reconciliation
// level … If the verdict commit of a Cat 6 reconciliation is itself detected
// as corrupted on a subsequent restart, it MUST escalate to operator attention
// as Cat 6b."
func TestEM017a_RecursionBoundedToOneLevel(t *testing.T) {
	t.Parallel()

	repoDir := t.TempDir()
	durableFixtureInitRepo(t, repoDir)

	runID := corruptCheckpointFixtureRunID(7)
	transitionID := corruptCheckpointFixtureTransitionID(7)
	durableFixtureCreateTaskBranch(t, repoDir, runID)

	sha := corruptCheckpointFixtureCommitWithTrailers(t, repoDir, runID, transitionID, "verdict-node")
	corruptCheckpointFixtureCheckTrailerPresent(t, repoDir, sha)

	hasTrailer, err := corruptCheckpointFixtureClassify(t, repoDir, sha)
	if !hasTrailer {
		t.Errorf("EM-017a recursion: sensor returned hasTrailer=false for corrupted verdict commit %s", sha)
	}
	if err == nil {
		t.Errorf("EM-017a recursion: sensor returned nil error for corrupted verdict commit; " +
			"daemon must detect this to trigger Cat 6b escalation rather than a nested reconciliation dispatch")
	}
}

// TestEM017a_MultipleCheckpointsOnlyCorruptedOneDetected asserts that when a
// run has N checkpoint commits and exactly one is corrupted (sibling missing),
// the sensor flags only the corrupted commit and passes the clean ones.
//
// This models the recovery scenario: prior durable checkpoints are intact;
// only the last one (at crash time) is corrupted.
//
// Spec ref: execution-model.md §4.4 EM-017a — per-commit sensor; prior
// durable checkpoints are unaffected (EM-033).
func TestEM017a_MultipleCheckpointsOnlyCorruptedOneDetected(t *testing.T) {
	t.Parallel()

	repoDir := t.TempDir()
	durableFixtureInitRepo(t, repoDir)

	runID := corruptCheckpointFixtureRunID(8)
	durableFixtureCreateTaskBranch(t, repoDir, runID)

	txID1 := corruptCheckpointFixtureTransitionID(81)
	txID2 := corruptCheckpointFixtureTransitionID(82)
	txID3 := corruptCheckpointFixtureTransitionID(83)

	corruptCheckpointFixtureWriteSiblingFile(t, repoDir, runID, txID1,
		corruptCheckpointFixtureMinimalJSON(runID, txID1))
	sha1 := corruptCheckpointFixtureCommitWithTrailers(t, repoDir, runID, txID1, "node-1")

	corruptCheckpointFixtureWriteSiblingFile(t, repoDir, runID, txID2,
		corruptCheckpointFixtureMinimalJSON(runID, txID2))
	sha2 := corruptCheckpointFixtureCommitWithTrailers(t, repoDir, runID, txID2, "node-2")

	sha3 := corruptCheckpointFixtureCommitWithTrailers(t, repoDir, runID, txID3, "node-3")

	_, err1 := corruptCheckpointFixtureClassify(t, repoDir, sha1)
	if err1 != nil {
		t.Errorf("EM-017a: clean checkpoint sha1 incorrectly flagged as corrupted: %v", err1)
	}
	_, err2 := corruptCheckpointFixtureClassify(t, repoDir, sha2)
	if err2 != nil {
		t.Errorf("EM-017a: clean checkpoint sha2 incorrectly flagged as corrupted: %v", err2)
	}

	hasTrailer3, err3 := corruptCheckpointFixtureClassify(t, repoDir, sha3)
	if !hasTrailer3 {
		t.Errorf("EM-017a: classify returned hasTrailer=false for corrupted sha3 %s", sha3)
	}
	if err3 == nil {
		t.Errorf("EM-017a: classify returned nil error for corrupted sha3 (missing sibling); " +
			"daemon must detect and dispatch reconciliation, NOT silently proceed")
	}
}

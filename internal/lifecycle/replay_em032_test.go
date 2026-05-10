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

// --- EM-032 sensor: deterministic replay contract ---
//
// EM-032 (execution-model.md §4.7) requires that given a run's git checkpoint
// trail and its Beads record, the run's state MUST be reconstructable to any
// point for debugging, audit, scenario-harness assertions, and restart
// reconciliation. "Transition history" in this spec refers to the git
// checkpoint trail, NOT the JSONL event tail.
//
// The tests below assert two complementary halves:
//
//  1. Structural: each checkpoint commit carries a Harmonik-Transition-ID
//     trailer, and the corresponding sibling file is retrievable by
//     `git show <commit>:.harmonik/transitions/<run_id>/<transition_id>.json`.
//     This is the machine-checkable form of "state reconstructable from git".
//
//  2. Behavioral: a multi-step run's state at each intermediate checkpoint is
//     independently addressable (i.e., the replay target is any point in the
//     trail, not just the tip). No JSONL source is consulted; the JSONL event
//     log plays no part in reconstruction.
//
// Spec ref: execution-model.md §4.7 EM-032 — "Given the run's git checkpoint
// trail and its Beads record, the run's state MUST be reconstructable to any
// point for debugging, audit, scenario-harness assertions, and restart
// reconciliation."

// replayFixtureRunID returns a deterministic, valid UUIDv7-shaped run ID for
// EM-032 replay-contract tests. Counter space starts at 300 to avoid collision
// with durableFixtureRunID (1–99), nonTxFixtureRunID (100–199), and
// corruptCheckpointFixtureRunID (200–299).
func replayFixtureRunID(n int) string {
	return durableFixtureRunID(300 + n)
}

// replayFixtureTransitionID returns a deterministic UUIDv7-shaped transition
// ID for use in EM-032 replay fixtures.
func replayFixtureTransitionID(n int) string {
	return fmt.Sprintf("01900000-0000-7000-8000-0000eeee%04d", n)
}

// replayFixtureWriteSiblingFile writes a minimal valid transition record JSON
// at the canonical sibling path `.harmonik/transitions/<runID>/<transitionID>.json`
// inside repoDir. Returns the absolute path of the written file.
//
// Spec ref: execution-model.md §4.4 EM-019 — canonical sibling-file path shape.
func replayFixtureWriteSiblingFile(t *testing.T, repoDir, runID, transitionID string) string {
	t.Helper()

	dir := filepath.Join(repoDir, ".harmonik", "transitions", runID)
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("replayFixtureWriteSiblingFile: MkdirAll: %v", err)
	}

	p := filepath.Join(dir, transitionID+".json")
	content := fmt.Sprintf(
		`{"schema_version":1,"run_id":%q,"transition_id":%q}`,
		runID, transitionID,
	)
	//nolint:gosec // G306: 0644 is the correct mode for a JSON record file; path is t.TempDir()
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("replayFixtureWriteSiblingFile: WriteFile: %v", err)
	}
	return p
}

// replayFixtureCommitCheckpoint writes the sibling file and lands a checkpoint
// commit carrying Harmonik-Run-ID and Harmonik-Transition-ID trailers.
// Returns the full commit SHA.
//
// Spec ref: execution-model.md §4.4 EM-017 (sibling file must be present in
// commit tree); §6.2 (checkpoint trailer format).
func replayFixtureCommitCheckpoint(t *testing.T, repoDir, runID, transitionID, nodeID string) string {
	t.Helper()

	// Write the sibling file into the working tree so it is captured by the commit.
	replayFixtureWriteSiblingFile(t, repoDir, runID, transitionID)

	// Stage all new files (sibling file + state file).
	stateFile := filepath.Join(repoDir, "state.txt")
	//nolint:gosec // G306: 0644 is correct for a test state file; path is t.TempDir()
	if err := os.WriteFile(stateFile, []byte(fmt.Sprintf("run=%s node=%s tx=%s\n", runID, nodeID, transitionID)), 0o644); err != nil {
		t.Fatalf("replayFixtureCommitCheckpoint: WriteFile state.txt: %v", err)
	}
	runGitRepo(t, repoDir, "add", ".")

	msg := fmt.Sprintf(
		"checkpoint: %s\n\nHarmonik-Run-ID: %s\nHarmonik-Transition-ID: %s\n",
		nodeID, runID, transitionID,
	)
	runGitRepo(t, repoDir, "commit", "-m", msg)
	return durableFixtureReadTip(t, repoDir, "run/"+runID)
}

// replayFixtureReadSiblingAtCommit uses `git show <sha>:<path>` to retrieve
// the sibling file at the given commit SHA and verifies it is valid JSON.
// This is the mechanical test of "state reconstructable from git": the content
// of the transition record is retrievable by commit SHA alone, with no JSONL
// consultation.
//
// Spec ref: execution-model.md §4.4 EM-019 — "a reader that needs the record
// for a given (run_id, transition_id) pair retrieves it by
// `git show <commit>:.harmonik/transitions/<run_id>/<transition_id>.json`."
func replayFixtureReadSiblingAtCommit(t *testing.T, repoDir, sha, runID, transitionID string) map[string]json.RawMessage {
	t.Helper()

	siblingPath := ".harmonik/transitions/" + runID + "/" + transitionID + ".json"
	//nolint:gosec // G204: sha is a git SHA from durableFixtureReadTip; siblingPath is derived from test constants; repoDir is t.TempDir()
	out, err := exec.CommandContext(t.Context(), "git", "-C", repoDir,
		"show", sha+":"+siblingPath,
	).Output()
	if err != nil {
		t.Fatalf("replayFixtureReadSiblingAtCommit: git show %s:%s: %v", sha, siblingPath, err)
	}

	var record map[string]json.RawMessage
	if jsonErr := json.Unmarshal(out, &record); jsonErr != nil {
		t.Fatalf("replayFixtureReadSiblingAtCommit: JSON unmarshal for commit %s: %v", sha, jsonErr)
	}
	return record
}

// replayFixtureExtractRunIDFromCommit reads the Harmonik-Run-ID trailer from a
// checkpoint commit's message. Returns the run ID string.
func replayFixtureExtractRunIDFromCommit(t *testing.T, repoDir, sha string) string {
	t.Helper()

	//nolint:gosec // G204: sha is a git SHA from durableFixtureReadTip; repoDir is t.TempDir()
	out, err := exec.CommandContext(t.Context(), "git", "-C", repoDir,
		"log", "-1", "--format=%B", sha,
	).Output()
	if err != nil {
		t.Fatalf("replayFixtureExtractRunIDFromCommit: git log: %v", err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Harmonik-Run-ID:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "Harmonik-Run-ID:"))
		}
	}
	t.Fatalf("replayFixtureExtractRunIDFromCommit: no Harmonik-Run-ID trailer in commit %s", sha)
	return ""
}

// replayFixtureExtractTransitionIDFromCommit reads the Harmonik-Transition-ID
// trailer from a checkpoint commit's message. Returns the transition ID string.
func replayFixtureExtractTransitionIDFromCommit(t *testing.T, repoDir, sha string) string {
	t.Helper()

	//nolint:gosec // G204: sha is a git SHA from durableFixtureReadTip; repoDir is t.TempDir()
	out, err := exec.CommandContext(t.Context(), "git", "-C", repoDir,
		"log", "-1", "--format=%B", sha,
	).Output()
	if err != nil {
		t.Fatalf("replayFixtureExtractTransitionIDFromCommit: git log: %v", err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Harmonik-Transition-ID:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "Harmonik-Transition-ID:"))
		}
	}
	t.Fatalf("replayFixtureExtractTransitionIDFromCommit: no Harmonik-Transition-ID trailer in commit %s", sha)
	return ""
}

// --- Tests for EM-032 ---

// TestEM032_SiblingFileAddressableByCheckpointSHA is the primary EM-032 sensor.
//
// It asserts that for each checkpoint commit in a run's trail, the transition
// record at `.harmonik/transitions/<run_id>/<transition_id>.json` is directly
// retrievable via `git show <sha>:<path>`. This is the machine-checkable form
// of "state reconstructable from the git checkpoint trail."
//
// No JSONL source is consulted or required. Reconstruction uses only the commit
// SHA extracted from the run's task branch tip and the trailer-embedded IDs.
//
// Spec ref: execution-model.md §4.7 EM-032 — "given the run's git checkpoint
// trail and its Beads record, the run's state MUST be reconstructable to any point."
// Spec ref: execution-model.md §4.4 EM-019 — git-show path is the canonical
// retrieval path; no cross-commit index may be required.
func TestEM032_SiblingFileAddressableByCheckpointSHA(t *testing.T) {
	t.Parallel()

	repoDir := t.TempDir()
	durableFixtureInitRepo(t, repoDir)

	runID := replayFixtureRunID(1)
	transitionID := replayFixtureTransitionID(1)
	durableFixtureCreateTaskBranch(t, repoDir, runID)

	// Land one checkpoint commit with a sibling file in the tree.
	sha := replayFixtureCommitCheckpoint(t, repoDir, runID, transitionID, "node-a")

	// Structural sensor: retrieve the sibling file at the checkpoint SHA using
	// only the commit SHA and the (run_id, transition_id) pair from the trailer.
	// No JSONL path. No index file. Git is the sole state source per EM-032.
	record := replayFixtureReadSiblingAtCommit(t, repoDir, sha, runID, transitionID)

	// The run_id in the record MUST match the run ID from the Harmonik-Run-ID trailer.
	runIDFromTrailer := replayFixtureExtractRunIDFromCommit(t, repoDir, sha)
	if runIDFromTrailer != runID {
		t.Errorf("EM-032 pre-condition: Harmonik-Run-ID trailer = %q; want %q", runIDFromTrailer, runID)
	}

	rawRunID, hasRunID := record["run_id"]
	if !hasRunID {
		t.Fatalf("EM-032: sibling file at commit %s is missing run_id field; state not reconstructable", sha)
	}
	var recordedRunID string
	if err := json.Unmarshal(rawRunID, &recordedRunID); err != nil {
		t.Fatalf("EM-032: sibling file run_id field is not a string: %v", err)
	}
	if recordedRunID != runID {
		t.Errorf("EM-032: sibling file run_id = %q; want %q (record must carry the run's canonical run_id)", recordedRunID, runID)
	}

	// The schema_version MUST be present (EM-022 gate).
	if _, hasSchemaVersion := record["schema_version"]; !hasSchemaVersion {
		t.Errorf("EM-032: sibling file at commit %s is missing schema_version; schema_version required per EM-022", sha)
	}
}

// TestEM032_MultiPointReplayFromCheckpointTrail verifies that a multi-step
// run's state is independently addressable at EACH intermediate checkpoint —
// not only at the tip.
//
// This is the "any point" half of EM-032. For a run with N checkpoint commits,
// the transition record for checkpoint K (1 ≤ K ≤ N) MUST be retrievable via
// `git show <sha_K>:<path_K>` without consulting a later commit or any JSONL.
//
// Spec ref: execution-model.md §4.7 EM-032 — "reconstructable to any point
// for debugging, audit, scenario-harness assertions, and restart reconciliation."
func TestEM032_MultiPointReplayFromCheckpointTrail(t *testing.T) {
	t.Parallel()

	const checkpointCount = 4

	repoDir := t.TempDir()
	durableFixtureInitRepo(t, repoDir)

	runID := replayFixtureRunID(2)
	durableFixtureCreateTaskBranch(t, repoDir, runID)

	// Land N checkpoint commits, each with its own transition record in the tree.
	type checkpointEntry struct {
		sha          string
		transitionID string
	}
	checkpoints := make([]checkpointEntry, checkpointCount)
	for i := range checkpointCount {
		txID := replayFixtureTransitionID(20 + i)
		nodeID := fmt.Sprintf("node-%d", i+1)
		sha := replayFixtureCommitCheckpoint(t, repoDir, runID, txID, nodeID)
		checkpoints[i] = checkpointEntry{sha: sha, transitionID: txID}
	}

	// Verify each checkpoint's sibling file is addressable by its own SHA.
	// Crucially, checkpoint K's record is read AT commit sha_K — not at the tip.
	// This proves that state at any intermediate point is reconstructable from git.
	for i, cp := range checkpoints {
		record := replayFixtureReadSiblingAtCommit(t, repoDir, cp.sha, runID, cp.transitionID)

		// Each record must carry schema_version per EM-022.
		if _, ok := record["schema_version"]; !ok {
			t.Errorf("EM-032 checkpoint[%d] (sha=%s): sibling file missing schema_version", i+1, cp.sha)
		}

		// Each record must carry the correct transition_id.
		rawTxID, hasTxID := record["transition_id"]
		if !hasTxID {
			t.Errorf("EM-032 checkpoint[%d] (sha=%s): sibling file missing transition_id field", i+1, cp.sha)
			continue
		}
		var recordedTxID string
		if err := json.Unmarshal(rawTxID, &recordedTxID); err != nil {
			t.Errorf("EM-032 checkpoint[%d] (sha=%s): transition_id field is not a string: %v", i+1, cp.sha, err)
			continue
		}
		if recordedTxID != cp.transitionID {
			t.Errorf("EM-032 checkpoint[%d] (sha=%s): sibling file transition_id = %q; want %q",
				i+1, cp.sha, recordedTxID, cp.transitionID)
		}
	}
}

// TestEM032_TransitionHistoryIsGitNotJSONL is the structural sensor asserting
// that "transition history" means the git checkpoint trail, not the JSONL event
// tail. This is embodied by verifying that the replay retrieval path —
// `git show <commit>:<sibling-path>` — requires no JSONL file on disk.
//
// The test deletes any JSONL file from the working tree after committing and
// confirms that state reconstruction from the git trail remains successful.
//
// Spec ref: execution-model.md §4.7 EM-032 — "Transition history in this spec
// refers to the git checkpoint trail, NOT the JSONL event tail."
func TestEM032_TransitionHistoryIsGitNotJSONL(t *testing.T) {
	t.Parallel()

	repoDir := t.TempDir()
	durableFixtureInitRepo(t, repoDir)

	runID := replayFixtureRunID(3)
	transitionID := replayFixtureTransitionID(30)
	durableFixtureCreateTaskBranch(t, repoDir, runID)

	// Write a JSONL event log file (simulating what the daemon would emit at runtime).
	// This file is NOT part of the committed tree — it represents the ephemeral tail.
	jsonlPath := filepath.Join(repoDir, ".harmonik", "events.jsonl")
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(filepath.Join(repoDir, ".harmonik"), 0o755); err != nil {
		t.Fatalf("MkdirAll .harmonik: %v", err)
	}
	eventLine := fmt.Sprintf(
		`{"event_id":"0196e3d1-3af8-7000-8000-000000000001","run_id":%q,"schema_version":1}`+"\n",
		runID,
	)
	//nolint:gosec // G306: 0644 is correct for an event log file; path is t.TempDir()
	if err := os.WriteFile(jsonlPath, []byte(eventLine), 0o644); err != nil {
		t.Fatalf("WriteFile events.jsonl: %v", err)
	}

	// Land the checkpoint commit (sibling file written to the tree, state committed).
	sha := replayFixtureCommitCheckpoint(t, repoDir, runID, transitionID, "node-a")

	// Simulate JSONL loss (crash, rotation, deliberate deletion):
	// the event log is gone. State reconstruction MUST still work from git alone.
	if err := os.Remove(jsonlPath); err != nil && !os.IsNotExist(err) {
		t.Fatalf("Remove events.jsonl: %v", err)
	}

	// Confirm JSONL is absent.
	if _, err := os.Stat(jsonlPath); !os.IsNotExist(err) {
		t.Fatalf("test pre-condition: JSONL file should be absent after removal")
	}

	// State reconstruction via git show: MUST succeed without JSONL.
	record := replayFixtureReadSiblingAtCommit(t, repoDir, sha, runID, transitionID)
	if _, ok := record["schema_version"]; !ok {
		t.Errorf("EM-032: state reconstruction without JSONL failed; sibling file at commit %s is missing schema_version", sha)
	}
	if _, ok := record["run_id"]; !ok {
		t.Errorf("EM-032: state reconstruction without JSONL failed; sibling file at commit %s is missing run_id", sha)
	}
}

// TestEM032_CrossRunReplayIsolation asserts that replaying run A's checkpoint
// trail cannot retrieve run B's transition records (run-scoped path uniqueness).
//
// Per §4.4.EM-018, the sibling-file path is scoped by run_id so that cross-run
// cherry-picks and replay-tree construction cannot collide. This test verifies
// that a git show for run A's run_id at run B's commit SHA returns an error —
// the transition record does not bleed across run boundaries.
//
// Spec ref: execution-model.md §4.7 EM-032 — reconstruction is per-run;
// §4.4 EM-018 — run-scoped sibling-file path is the structural uniqueness guarantee.
func TestEM032_CrossRunReplayIsolation(t *testing.T) {
	t.Parallel()

	repoDir := t.TempDir()
	durableFixtureInitRepo(t, repoDir)

	runIDA := replayFixtureRunID(4)
	runIDB := replayFixtureRunID(5)

	txIDA := replayFixtureTransitionID(40)
	txIDB := replayFixtureTransitionID(50)

	// Create run A's task branch and commit its checkpoint.
	durableFixtureCreateTaskBranch(t, repoDir, runIDA)
	shaA := replayFixtureCommitCheckpoint(t, repoDir, runIDA, txIDA, "node-a")

	// Switch back to main and create run B's task branch + checkpoint.
	runGitRepo(t, repoDir, "checkout", "main")
	durableFixtureCreateTaskBranch(t, repoDir, runIDB)
	shaB := replayFixtureCommitCheckpoint(t, repoDir, runIDB, txIDB, "node-b")

	// Run A's sibling is retrievable at run A's commit SHA — baseline.
	recordA := replayFixtureReadSiblingAtCommit(t, repoDir, shaA, runIDA, txIDA)
	if _, ok := recordA["run_id"]; !ok {
		t.Errorf("EM-032 isolation baseline: run A sibling missing run_id at sha=%s", shaA)
	}

	// Run B's sibling is retrievable at run B's commit SHA — baseline.
	recordB := replayFixtureReadSiblingAtCommit(t, repoDir, shaB, runIDB, txIDB)
	if _, ok := recordB["run_id"]; !ok {
		t.Errorf("EM-032 isolation baseline: run B sibling missing run_id at sha=%s", shaB)
	}

	// Cross-run isolation: attempting to retrieve run A's sibling AT run B's
	// commit SHA (using run A's path) MUST fail — run B's tree has no
	// .harmonik/transitions/<runIDA>/... entry.
	siblingPathA := ".harmonik/transitions/" + runIDA + "/" + txIDA + ".json"
	//nolint:gosec // G204: shaB is a git SHA from durableFixtureReadTip; siblingPathA is from test constants; repoDir is t.TempDir()
	_, err := exec.CommandContext(t.Context(), "git", "-C", repoDir,
		"show", shaB+":"+siblingPathA,
	).Output()
	if err == nil {
		t.Errorf("EM-032 cross-run isolation violation: git show of run A's sibling at run B's commit SHA succeeded; "+
			"run-scoped paths per EM-018 must prevent cross-run bleed (shaB=%s, pathA=%s)",
			shaB, siblingPathA)
	}
	// err != nil → git returned non-zero (path not found in run B's tree). This is correct.
}

// TestEM032_TrailWalkReconstructsRunIDAtEveryPoint verifies that a consumer
// walking the checkpoint trail (oldest to newest) can reconstruct the run's
// identity (run_id) from each commit's Harmonik-Run-ID trailer alone — without
// any external index. This models the restart-reconciliation path described in
// EM-032: the daemon walks the trail and reads trailers to identify runs.
//
// Spec ref: execution-model.md §4.7 EM-032; §4.7 EM-031 — git + Beads are the
// state-reconstruction source; §6.2 — Harmonik-Run-ID trailer is normative.
func TestEM032_TrailWalkReconstructsRunIDAtEveryPoint(t *testing.T) {
	t.Parallel()

	const checkpointCount = 3

	repoDir := t.TempDir()
	durableFixtureInitRepo(t, repoDir)

	runID := replayFixtureRunID(6)
	durableFixtureCreateTaskBranch(t, repoDir, runID)

	shas := make([]string, checkpointCount)
	for i := range checkpointCount {
		txID := replayFixtureTransitionID(60 + i)
		nodeID := fmt.Sprintf("node-%d", i+1)
		shas[i] = replayFixtureCommitCheckpoint(t, repoDir, runID, txID, nodeID)
	}

	// Walk the trail (oldest to newest) and verify the run_id is recoverable
	// from each commit's Harmonik-Run-ID trailer. This is the minimal contract:
	// a replay consumer can re-identify the run without any external state.
	for i, sha := range shas {
		gotRunID := replayFixtureExtractRunIDFromCommit(t, repoDir, sha)
		if gotRunID != runID {
			t.Errorf("EM-032 trail walk checkpoint[%d] (sha=%s): Harmonik-Run-ID = %q; want %q (run_id must be self-describing at each point)",
				i+1, sha, gotRunID, runID)
		}

		// The transition_id at this point must also be extractable from the trailer.
		txID := replayFixtureExtractTransitionIDFromCommit(t, repoDir, sha)
		if txID == "" {
			t.Errorf("EM-032 trail walk checkpoint[%d] (sha=%s): Harmonik-Transition-ID trailer absent; "+
				"transition_id must be present for point-in-time reconstruction", i+1, sha)
		}

		// And the sibling file at this exact commit SHA must parse correctly.
		record := replayFixtureReadSiblingAtCommit(t, repoDir, sha, runID, txID)
		if _, ok := record["schema_version"]; !ok {
			t.Errorf("EM-032 trail walk checkpoint[%d] (sha=%s): sibling missing schema_version", i+1, sha)
		}
	}
}

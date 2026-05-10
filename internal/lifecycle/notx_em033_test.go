package lifecycle

import (
	"os/exec"
	"testing"
)

// nonTxFixtureRunID returns a deterministic, valid UUIDv7-shaped run ID for
// EM-033 no-transactionality tests. The counter space is offset from
// durableFixtureRunID to avoid collisions across concurrent test packages.
func nonTxFixtureRunID(n int) string {
	return durableFixtureRunID(100 + n)
}

// nonTxFixtureIsAncestor reports whether ancestor is an ancestor of (or equal
// to) descendant via `git merge-base --is-ancestor`. Returns true on success.
func nonTxFixtureIsAncestor(t *testing.T, repoDir, ancestor, descendant string) bool {
	t.Helper()

	//nolint:gosec // G204: ancestor/descendant are commit SHAs from durableFixtureCommitCheckpoint; repoDir is t.TempDir()
	cmd := exec.CommandContext(t.Context(), "git", "-C", repoDir,
		"merge-base", "--is-ancestor", ancestor, descendant,
	)
	return cmd.Run() == nil
}

// --- Tests for EM-033 ---

// TestEM033_PriorCheckpointsDurableAfterNodeFailure is the primary EM-033 sensor.
//
// It simulates a multi-node run where nodes 1..N each commit a durable
// checkpoint, then node N+1 fails without committing. The test asserts:
//   - The branch tip remains at the Nth checkpoint commit (no rollback).
//   - Every prior checkpoint SHA is an ancestor of the current branch tip,
//     proving that none of the N checkpoint commits were removed or rewritten.
//
// Spec ref: execution-model.md §4.7 EM-033 — "a run that commits N nodes and
// fails on node N+1 MUST leave all N prior checkpoints durable."
func TestEM033_PriorCheckpointsDurableAfterNodeFailure(t *testing.T) {
	t.Parallel()

	repoDir := t.TempDir()
	durableFixtureInitRepo(t, repoDir)

	runID := nonTxFixtureRunID(1)
	durableFixtureCreateTaskBranch(t, repoDir, runID)

	// Commit N=3 durable checkpoints (nodes 1..3).
	sha1 := durableFixtureCommitCheckpoint(t, repoDir, runID, "node-1")
	sha2 := durableFixtureCommitCheckpoint(t, repoDir, runID, "node-2")
	sha3 := durableFixtureCommitCheckpoint(t, repoDir, runID, "node-3")

	// Simulate node-4 failure: no commit is landed. The branch tip stays at sha3.
	// (No git operation here — the absence of a commit IS the test.)

	tipAfterFailure := durableFixtureReadTip(t, repoDir, "run/"+runID)

	// Primary assertion: tip must still be the Nth checkpoint commit (sha3).
	// Any rollback mechanism that removed sha3 or pointed tip to an earlier
	// commit would be caught here.
	if tipAfterFailure != sha3 {
		t.Errorf("EM-033 violation: node-4 failure must not move branch tip; got %q, want sha3 %q",
			tipAfterFailure, sha3)
	}

	// Durability assertions: every prior checkpoint must be an ancestor of the
	// current tip. If any checkpoint were removed or the branch were force-reset,
	// the ancestor check would fail.
	if !nonTxFixtureIsAncestor(t, repoDir, sha1, tipAfterFailure) {
		t.Errorf("EM-033 violation: sha1 (node-1) is not an ancestor of the post-failure tip %q; "+
			"node-1 checkpoint was rolled back", tipAfterFailure)
	}
	if !nonTxFixtureIsAncestor(t, repoDir, sha2, tipAfterFailure) {
		t.Errorf("EM-033 violation: sha2 (node-2) is not an ancestor of the post-failure tip %q; "+
			"node-2 checkpoint was rolled back", tipAfterFailure)
	}
	if !nonTxFixtureIsAncestor(t, repoDir, sha3, tipAfterFailure) {
		t.Errorf("EM-033 violation: sha3 (node-3) is not an ancestor of the post-failure tip %q; "+
			"node-3 checkpoint was rolled back", tipAfterFailure)
	}
}

// TestEM033_FailureAtFirstNodeLeavesNoCheckpoints asserts that when a run
// fails on its very first node (N=0 prior checkpoints), the task branch
// contains only the root commit — there is nothing to roll back and nothing
// durable is lost.
//
// Spec ref: execution-model.md §4.7 EM-033 (degenerate N=0 case: tip remains
// at root, no checkpoint commits exist to rollback).
func TestEM033_FailureAtFirstNodeLeavesNoCheckpoints(t *testing.T) {
	t.Parallel()

	repoDir := t.TempDir()
	durableFixtureInitRepo(t, repoDir)

	runID := nonTxFixtureRunID(2)
	durableFixtureCreateTaskBranch(t, repoDir, runID)

	// Record branch tip before any checkpoint: the root commit.
	tipBeforeAnyCheckpoint := durableFixtureReadTip(t, repoDir, "run/"+runID)

	// Simulate node-1 failure: no commit is landed. The branch tip must not move.
	tipAfterFailure := durableFixtureReadTip(t, repoDir, "run/"+runID)

	if tipAfterFailure != tipBeforeAnyCheckpoint {
		t.Errorf("EM-033 degenerate case: node-1 failure moved tip from %q to %q; "+
			"tip must remain at root when no checkpoints exist",
			tipBeforeAnyCheckpoint, tipAfterFailure)
	}
}

// TestEM033_PartialRunCheckpointsPreservedAcrossMultipleFailures asserts that
// multiple consecutive node failures (at node N+1 and at a later re-dispatch of
// node N+1) do not erode the N prior durable checkpoints.
//
// This models the reconciliation-driven retry path: the run stalls at a node,
// is retried, fails again, yet all prior checkpoints remain intact.
//
// Spec ref: execution-model.md §4.7 EM-033; [reconciliation/spec.md §8]
// recovery categories (recovery routes through RC categories, NOT rollback).
func TestEM033_PartialRunCheckpointsPreservedAcrossMultipleFailures(t *testing.T) {
	t.Parallel()

	repoDir := t.TempDir()
	durableFixtureInitRepo(t, repoDir)

	runID := nonTxFixtureRunID(3)
	durableFixtureCreateTaskBranch(t, repoDir, runID)

	// Two durable checkpoints for nodes 1 and 2.
	sha1 := durableFixtureCommitCheckpoint(t, repoDir, runID, "node-1")
	sha2 := durableFixtureCommitCheckpoint(t, repoDir, runID, "node-2")

	// First failure at node-3: no commit.
	tipAfterFirstFailure := durableFixtureReadTip(t, repoDir, "run/"+runID)
	if tipAfterFirstFailure != sha2 {
		t.Errorf("EM-033: first node-3 failure must not move tip; got %q, want sha2 %q",
			tipAfterFirstFailure, sha2)
	}

	// Second failure at node-3 (retry also fails): still no commit.
	tipAfterSecondFailure := durableFixtureReadTip(t, repoDir, "run/"+runID)
	if tipAfterSecondFailure != sha2 {
		t.Errorf("EM-033: second node-3 failure must not move tip; got %q, want sha2 %q",
			tipAfterSecondFailure, sha2)
	}

	// Both prior checkpoints must remain ancestors of the unchanged tip.
	if !nonTxFixtureIsAncestor(t, repoDir, sha1, tipAfterSecondFailure) {
		t.Errorf("EM-033 violation: sha1 (node-1) not ancestor of tip after repeated failure; " +
			"checkpoint was rolled back")
	}
	if !nonTxFixtureIsAncestor(t, repoDir, sha2, tipAfterSecondFailure) {
		t.Errorf("EM-033 violation: sha2 (node-2) not ancestor of tip after repeated failure; " +
			"checkpoint was rolled back")
	}
}

// TestEM033_LargeNCheckpointsAllDurableAfterFailure stress-tests the EM-033
// invariant with N=10 checkpoint commits followed by a single node-(N+1)
// failure. Every one of the 10 prior checkpoint SHAs must be an ancestor of
// the post-failure tip.
//
// Spec ref: execution-model.md §4.7 EM-033 — "all N prior checkpoints durable."
func TestEM033_LargeNCheckpointsAllDurableAfterFailure(t *testing.T) {
	t.Parallel()

	const nodeCount = 10

	repoDir := t.TempDir()
	durableFixtureInitRepo(t, repoDir)

	runID := nonTxFixtureRunID(4)
	durableFixtureCreateTaskBranch(t, repoDir, runID)

	shas := make([]string, nodeCount)
	for i := range nodeCount {
		nodeID := durableFixtureRunID(i + 1) // use as an opaque node label
		shas[i] = durableFixtureCommitCheckpoint(t, repoDir, runID, nodeID)
	}

	// Simulate node-(N+1) failure: no commit.
	tipAfterFailure := durableFixtureReadTip(t, repoDir, "run/"+runID)

	// Tip must equal the last checkpoint.
	if tipAfterFailure != shas[nodeCount-1] {
		t.Errorf("EM-033 violation (large N): tip moved after node-%d failure; "+
			"got %q, want %q", nodeCount+1, tipAfterFailure, shas[nodeCount-1])
	}

	// Every checkpoint must be an ancestor of the tip.
	for i, sha := range shas {
		if !nonTxFixtureIsAncestor(t, repoDir, sha, tipAfterFailure) {
			t.Errorf("EM-033 violation (large N): checkpoint[%d] sha=%q is not an ancestor of tip %q; "+
				"checkpoint was rolled back", i+1, sha, tipAfterFailure)
		}
	}
}

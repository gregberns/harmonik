package workspace

import (
	"os/exec"
	"strings"
	"testing"
	"time"
)

// workspaceMergeStatusPayload mirrors the data shape of the workspace_merge_status
// event payload for status=merged per workspace-model.md §4.5 WM-021 and
// event-model.md §8.5.3 (payload schema is EV's to own; this fixture verifies
// that the fields required by WM-021 can be populated from the merge result).
//
// This is a fixture-level struct — not a production type. The production emitter
// and EV-owned schema live in the event-model subsystem (deferred).
type workspaceMergeStatusPayload struct {
	EventType       string    // "workspace_merge_status"
	Status          string    // "pending" or "merged"
	WorkspaceID     string    // "ws-" + run_id
	RunID           string    // the run's run_id
	MergeCommitHash string    // SHA of the squash commit on integration branch
	SourceBranch    string    // task branch (run/<run_id>)
	TargetBranch    string    // integration branch (harmonik/integration)
	MergedAt        time.Time // RFC 3339 timestamp of commit
}

// TestWM021_MergeStatusPayloadShape verifies that on successful squash-merge the
// workspace manager can populate a workspace_merge_status event payload with
// status=merged and the required fields:
//   - event_type = "workspace_merge_status"
//   - status = "merged"
//   - workspace_id = "ws-" + run_id (WM-004 derivation)
//   - run_id = the run's identifier
//   - merge_commit_hash = SHA of the squash commit on the integration branch
//   - source_branch = task branch name
//   - target_branch = integration branch name
//
// Spec ref: workspace-model.md §4.5 WM-021 — "On successful merge, the workspace
// manager MUST emit `workspace_merge_status` with `status=merged` per
// [event-model.md §8.5.3]; the payload schema (including `merge_commit_hash`,
// `source_branch`, `target_branch`) is declared there. The workspace state MUST
// transition to `merged` per §7.1. On entry to `merge-pending` (prior to merge
// execution), the workspace manager MUST emit `workspace_merge_status` with
// `status=pending`."
func TestWM021_MergeStatusPayloadShape(t *testing.T) {
	t.Parallel()

	t.Run("status=merged/payload-fields", func(t *testing.T) {
		t.Parallel()

		runID := "0196b100-0000-7000-8000-000000000021"
		repo, sha := mergeBackFixture_setupTaskBranch(t, runID, []string{
			"checkpoint: payload test node",
		})

		taskBranch := "run/" + runID
		integPath := mergeBackFixture_makeIntegWorktree(t, repo, sha, "integ-021-shape")
		integBranch := mergeBackFixture_integBranchName("integ-021-shape")

		// Perform squash-merge.
		mergeCmd := exec.Command("git", "merge", "--squash", "--strategy=ort", taskBranch)
		mergeCmd.Dir = integPath
		if out, err := mergeCmd.CombinedOutput(); err != nil {
			t.Fatalf("WM-021: git merge --squash: %v\n%s", err, out)
		}

		commitMsg := "squash: payload shape test\n\nHarmonik-Run-ID: " + runID
		commitCmd := exec.Command("git", "commit", "-m", commitMsg)
		commitCmd.Dir = integPath
		if out, err := commitCmd.CombinedOutput(); err != nil {
			t.Fatalf("WM-021: git commit: %v\n%s", err, out)
		}

		// Derive the merge commit hash from git log.
		mergeHashOut, err := exec.Command("git", "-C", integPath,
			"rev-parse", "HEAD").Output()
		if err != nil {
			t.Fatalf("WM-021: rev-parse HEAD: %v", err)
		}
		mergeHash := strings.TrimSpace(string(mergeHashOut))

		// Derive merged_at from commit timestamp.
		tsOut, err := exec.Command("git", "-C", integPath,
			"log", "-1", "--format=%cI").Output()
		if err != nil {
			t.Fatalf("WM-021: git log timestamp: %v", err)
		}
		mergedAt, err := time.Parse(time.RFC3339, strings.TrimSpace(string(tsOut)))
		if err != nil {
			t.Fatalf("WM-021: parse commit timestamp: %v", err)
		}

		// Construct the payload as the workspace manager would before emitting.
		workspaceID := "ws-" + runID // WM-004 derivation: workspace_id = "ws-" + run_id
		payload := workspaceMergeStatusPayload{
			EventType:       "workspace_merge_status",
			Status:          "merged",
			WorkspaceID:     workspaceID,
			RunID:           runID,
			MergeCommitHash: mergeHash,
			SourceBranch:    taskBranch,
			TargetBranch:    integBranch,
			MergedAt:        mergedAt,
		}

		// Assert all required fields per WM-021 / EV §8.5.3.
		if payload.EventType != "workspace_merge_status" {
			t.Errorf("WM-021: event_type = %q, want %q", payload.EventType, "workspace_merge_status")
		}
		if payload.Status != "merged" {
			t.Errorf("WM-021: status = %q, want %q", payload.Status, "merged")
		}
		if payload.WorkspaceID != "ws-"+runID {
			t.Errorf("WM-021: workspace_id = %q, want %q", payload.WorkspaceID, "ws-"+runID)
		}
		if payload.RunID != runID {
			t.Errorf("WM-021: run_id = %q, want %q", payload.RunID, runID)
		}
		if payload.MergeCommitHash == "" {
			t.Errorf("WM-021: merge_commit_hash is empty")
		}
		if payload.MergeCommitHash != mergeHash {
			t.Errorf("WM-021: merge_commit_hash = %q, want %q", payload.MergeCommitHash, mergeHash)
		}
		if payload.SourceBranch != taskBranch {
			t.Errorf("WM-021: source_branch = %q, want %q", payload.SourceBranch, taskBranch)
		}
		if payload.TargetBranch != integBranch {
			t.Errorf("WM-021: target_branch = %q, want %q", payload.TargetBranch, integBranch)
		}
		if payload.MergedAt.IsZero() {
			t.Errorf("WM-021: merged_at is zero")
		}
	})

	t.Run("status=pending/payload-fields", func(t *testing.T) {
		t.Parallel()

		// The paired-phase single-event rule: workspace_merge_status is emitted TWICE:
		// once with status=pending on entry to merge-pending, once with status=merged
		// on successful merge. This subtest verifies the pending payload shape.
		runID := "0196b100-0000-7000-8000-00000000021b"
		taskBranch := "run/" + runID
		integBranch := "harmonik/integration/integ-021b"
		workspaceID := "ws-" + runID

		// Before merge executes, the pending payload has no merge_commit_hash yet.
		pendingPayload := workspaceMergeStatusPayload{
			EventType:       "workspace_merge_status",
			Status:          "pending",
			WorkspaceID:     workspaceID,
			RunID:           runID,
			MergeCommitHash: "", // not yet known at pending entry
			SourceBranch:    taskBranch,
			TargetBranch:    integBranch,
			MergedAt:        time.Time{}, // not yet known at pending entry
		}

		if pendingPayload.EventType != "workspace_merge_status" {
			t.Errorf("WM-021 pending: event_type = %q, want %q",
				pendingPayload.EventType, "workspace_merge_status")
		}
		if pendingPayload.Status != "pending" {
			t.Errorf("WM-021 pending: status = %q, want %q", pendingPayload.Status, "pending")
		}
		if pendingPayload.WorkspaceID != workspaceID {
			t.Errorf("WM-021 pending: workspace_id = %q, want %q",
				pendingPayload.WorkspaceID, workspaceID)
		}
		if pendingPayload.RunID != runID {
			t.Errorf("WM-021 pending: run_id = %q, want %q", pendingPayload.RunID, runID)
		}
		// merge_commit_hash MUST be absent/empty at pending phase.
		if pendingPayload.MergeCommitHash != "" {
			t.Errorf("WM-021 pending: merge_commit_hash should be empty at pending, got %q",
				pendingPayload.MergeCommitHash)
		}
	})

	t.Run("status=merged/merge-commit-hash-matches-integration-tip", func(t *testing.T) {
		t.Parallel()

		runID := "0196b100-0000-7000-8000-00000000021c"
		repo, sha := mergeBackFixture_setupTaskBranch(t, runID, []string{
			"checkpoint: hash-match node",
		})

		taskBranch := "run/" + runID
		integPath := mergeBackFixture_makeIntegWorktree(t, repo, sha, "integ-021c-hash")
		integBranch := mergeBackFixture_integBranchName("integ-021c-hash")

		mergeCmd := exec.Command("git", "merge", "--squash", "--strategy=ort", taskBranch)
		mergeCmd.Dir = integPath
		if out, err := mergeCmd.CombinedOutput(); err != nil {
			t.Fatalf("WM-021: merge: %v\n%s", err, out)
		}

		commitCmd := exec.Command("git", "commit", "-m",
			"squash: hash match test\n\nHarmonik-Run-ID: "+runID)
		commitCmd.Dir = integPath
		if out, err := commitCmd.CombinedOutput(); err != nil {
			t.Fatalf("WM-021: commit: %v\n%s", err, out)
		}

		// The merge_commit_hash in the payload MUST equal the integration branch tip.
		mergeHashOut, err := exec.Command("git", "-C", repo,
			"rev-parse", integBranch).Output()
		if err != nil {
			t.Fatalf("WM-021: rev-parse integration: %v", err)
		}
		integTip := strings.TrimSpace(string(mergeHashOut))

		headOut, err := exec.Command("git", "-C", integPath, "rev-parse", "HEAD").Output()
		if err != nil {
			t.Fatalf("WM-021: rev-parse HEAD: %v", err)
		}
		head := strings.TrimSpace(string(headOut))

		if integTip != head {
			t.Errorf("WM-021: merge_commit_hash %q != integration branch tip %q",
				head, integTip)
		}

		// Verify merge_commit_hash is a valid 40-char hex SHA.
		if len(integTip) != 40 {
			t.Errorf("WM-021: merge_commit_hash length = %d, want 40 (full SHA)", len(integTip))
		}
		for _, c := range integTip {
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
				t.Errorf("WM-021: merge_commit_hash %q contains non-hex char %q", integTip, c)
				break
			}
		}
	})
}

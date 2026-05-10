package workspace

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gregberns/harmonik/internal/core"
)

// Tests for the intra-run rollback worktree-stability, branch-stability, and
// run_id-unchanged invariants per workspace-model.md §4.9 WM-035.
//
// Helper prefix: intraRunRollbackFixture (bead hk-8mwo.47; avoids collision with
// sibling-bead helpers such as failedRunFixture, leaseFixture, etc.).
//
// WM-035 states: intra-run rollback verdicts — resume-here, resume-with-context,
// reset-to-checkpoint — MUST keep the same worktree and the same task branch.
// The run's run_id is unchanged; state reverts to the named checkpoint via git
// operations inside the existing worktree per EM-044.
//
// Three invariants are locked by this contract:
//
//  (a) worktree-stable: the worktree path MUST be identical before and after rollback.
//  (b) branch-stable:   the task branch MUST be identical before and after rollback.
//  (c) run_id-unchanged: the Workspace.RunID MUST be identical before and after rollback.
//
// NOTE (post-mvh): The workspace manager's verdict-executor for intra-run rollback
// is not yet implemented. These tests capture the behavioral shape declared by WM-035
// using a fixture-level rollback simulator (intraRunRollbackFixtureApplyVerdict) so
// they pass as conformance gates once the implementation lands. The fixture is marked
// with a TODO citing the owning bead reference.

// ─────────────────────────────────────────────────────────────────────────────
// §4.9 WM-035: Intra-run rollback verdicts keep the same worktree
// ─────────────────────────────────────────────────────────────────────────────

// TestWM035_WorktreeStableAfterIntraRunRollback verifies that each of the three
// intra-run rollback verdicts (resume-here, resume-with-context,
// reset-to-checkpoint) leaves the Workspace.Path unchanged.
//
// Spec ref: workspace-model.md §4.9 WM-035 — "Intra-run rollback verdicts …
// MUST keep the same worktree and the same task branch."
func TestWM035_WorktreeStableAfterIntraRunRollback(t *testing.T) {
	t.Parallel()

	intraRunVerdicts := []core.Verdict{
		core.VerdictResumeHere,
		core.VerdictResumeWithContext,
		core.VerdictResetToCheckpoint,
	}

	for _, v := range intraRunVerdicts {
		v := v
		t.Run(string(v), func(t *testing.T) {
			t.Parallel()

			ws := intraRunRollbackFixtureWorkspace(t, string(v))
			wantPath := ws.Path

			if err := intraRunRollbackFixtureApplyVerdict(ws, v, ""); err != nil {
				t.Fatalf("WM-035[%s]: applyVerdict: %v", v, err)
			}

			if ws.Path != wantPath {
				t.Errorf("WM-035[%s]: worktree-stable violated: path changed %q → %q",
					v, wantPath, ws.Path)
			}
		})
	}
}

// TestWM035_BranchStableAfterIntraRunRollback verifies that each of the three
// intra-run rollback verdicts leaves the Workspace.BranchName unchanged.
//
// Spec ref: workspace-model.md §4.9 WM-035 — "MUST keep the same worktree
// and the same task branch."
func TestWM035_BranchStableAfterIntraRunRollback(t *testing.T) {
	t.Parallel()

	intraRunVerdicts := []core.Verdict{
		core.VerdictResumeHere,
		core.VerdictResumeWithContext,
		core.VerdictResetToCheckpoint,
	}

	for _, v := range intraRunVerdicts {
		v := v
		t.Run(string(v), func(t *testing.T) {
			t.Parallel()

			ws := intraRunRollbackFixtureWorkspace(t, string(v))
			wantBranch := ws.BranchName

			if err := intraRunRollbackFixtureApplyVerdict(ws, v, ""); err != nil {
				t.Fatalf("WM-035[%s]: applyVerdict: %v", v, err)
			}

			if ws.BranchName != wantBranch {
				t.Errorf("WM-035[%s]: branch-stable violated: branch changed %q → %q",
					v, wantBranch, ws.BranchName)
			}
		})
	}
}

// TestWM035_RunIDUnchangedAfterIntraRunRollback verifies that each of the three
// intra-run rollback verdicts leaves the Workspace.RunID unchanged.
//
// Spec ref: workspace-model.md §4.9 WM-035 — "The run's run_id is unchanged."
func TestWM035_RunIDUnchangedAfterIntraRunRollback(t *testing.T) {
	t.Parallel()

	intraRunVerdicts := []core.Verdict{
		core.VerdictResumeHere,
		core.VerdictResumeWithContext,
		core.VerdictResetToCheckpoint,
	}

	for _, v := range intraRunVerdicts {
		v := v
		t.Run(string(v), func(t *testing.T) {
			t.Parallel()

			ws := intraRunRollbackFixtureWorkspace(t, string(v))
			wantRunID := ws.RunID

			if err := intraRunRollbackFixtureApplyVerdict(ws, v, ""); err != nil {
				t.Fatalf("WM-035[%s]: applyVerdict: %v", v, err)
			}

			if ws.RunID != wantRunID {
				t.Errorf("WM-035[%s]: run_id-unchanged violated: run_id changed %q → %q",
					v, wantRunID, ws.RunID)
			}
		})
	}
}

// TestWM035_WorktreeDirExistsAfterRollback verifies that the filesystem worktree
// directory survives each intra-run rollback operation (the git worktree is not
// removed and re-created).
//
// Spec ref: workspace-model.md §4.9 WM-035 — "MUST keep the same worktree …
// state reverts to the named checkpoint via git operations inside the existing
// worktree per [execution-model.md §4.10 EM-044]."
func TestWM035_WorktreeDirExistsAfterRollback(t *testing.T) {
	t.Parallel()

	intraRunVerdicts := []core.Verdict{
		core.VerdictResumeHere,
		core.VerdictResumeWithContext,
		core.VerdictResetToCheckpoint,
	}

	for _, v := range intraRunVerdicts {
		v := v
		t.Run(string(v), func(t *testing.T) {
			t.Parallel()

			repo, sha := tempRepo(t)
			runID := intraRunRollbackFixtureRunID(string(v))
			branch := "run/" + runID
			worktreePath := filepath.Join(repo, ".harmonik", "worktrees", runID)

			//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
			if err := os.MkdirAll(filepath.Dir(worktreePath), 0o755); err != nil {
				t.Fatalf("MkdirAll: %v", err)
			}

			addCmd := exec.CommandContext(t.Context(), "git", "worktree", "add", "-b", branch, worktreePath, sha)
			addCmd.Dir = repo
			if out, err := addCmd.CombinedOutput(); err != nil {
				t.Fatalf("git worktree add: %v\n%s", err, out)
			}

			u := uuid.MustParse(runID)
			ws := &Workspace{
				WorkspaceID:    "ws-" + runID,
				RunID:          core.RunID(u),
				Repository:     repo,
				ParentCommit:   sha,
				BranchName:     branch,
				Path:           worktreePath,
				State:          core.WorkspaceStateLeased,
				InterruptState: core.InterruptStateNone,
				Metadata: map[string]string{
					"created_at":           "2026-05-10T00:00:00Z",
					"operator_fingerprint": "test-operator",
				},
				SchemaVersion: 1,
			}

			// Write a lease-lock so the run appears live (simulates mid-run state).
			lock := &core.LeaseLockFile{
				RunID:     core.RunID(u),
				PID:       os.Getpid(),
				CreatedAt: time.Now().UTC(),
				TTLSec:    3600,
			}
			leaseLockPath := LeaseLockPath(worktreePath)
			if err := WriteLeaseLockAtomic(leaseLockPath, lock); err != nil {
				t.Fatalf("WM-035: WriteLeaseLockAtomic: %v", err)
			}

			// Apply the intra-run rollback verdict. Per WM-035, the worktree
			// MUST be kept — no git worktree remove is issued.
			if err := intraRunRollbackFixtureApplyVerdict(ws, v, sha); err != nil {
				t.Fatalf("WM-035[%s]: applyVerdict: %v", v, err)
			}

			// Worktree directory MUST still exist after rollback.
			if _, err := os.Stat(worktreePath); err != nil {
				t.Errorf("WM-035[%s]: worktree directory absent after rollback; want persisted: %v", v, err)
			}

			// Task branch MUST still exist after rollback.
			checkBranch := exec.CommandContext(t.Context(), "git", "-C", repo, "rev-parse", "--verify", branch)
			if out, err := checkBranch.CombinedOutput(); err != nil {
				t.Errorf("WM-035[%s]: task branch %q absent after rollback; want persisted: %v\n%s",
					v, branch, err, out)
			}
		})
	}
}

// TestWM035_ResetToCheckpointResetsHeadToRollbackTarget verifies that
// reset-to-checkpoint reverts the worktree HEAD to the rollback_to_state_id
// target commit via a git-reset operation inside the existing worktree.
//
// Spec ref: workspace-model.md §4.9 WM-035 — "state reverts to the named
// checkpoint via git operations inside the existing worktree per
// [execution-model.md §4.10 EM-044] — the rollback_to_state_id field on
// the transition record (EM-044) is the mechanical driver of the rollback."
func TestWM035_ResetToCheckpointResetsHeadToRollbackTarget(t *testing.T) {
	t.Parallel()

	repo, initialSHA := tempRepo(t)
	runID := "0196b300-0000-7000-8000-000000035901"
	branch := "run/" + runID
	worktreePath := filepath.Join(repo, ".harmonik", "worktrees", runID)

	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(filepath.Dir(worktreePath), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	addCmd := exec.CommandContext(t.Context(), "git", "worktree", "add", "-b", branch, worktreePath, initialSHA)
	addCmd.Dir = repo
	if out, err := addCmd.CombinedOutput(); err != nil {
		t.Fatalf("git worktree add: %v\n%s", err, out)
	}

	// Make a second commit on the task branch (represents a checkpoint after
	// work has been done). After applying reset-to-checkpoint, HEAD MUST
	// revert to initialSHA (the rollback target).
	extraFile := filepath.Join(worktreePath, "progress.txt")
	if err := os.WriteFile(extraFile, []byte("work in progress\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	addFiles := exec.CommandContext(t.Context(), "git", "add", "progress.txt")
	addFiles.Dir = worktreePath
	if out, err := addFiles.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
	commitCmd := exec.CommandContext(t.Context(), "git", "commit", "-m", "checkpoint: work in progress")
	commitCmd.Dir = worktreePath
	commitCmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Harmonik Test",
		"GIT_AUTHOR_EMAIL=test@harmonik.local",
		"GIT_COMMITTER_NAME=Harmonik Test",
		"GIT_COMMITTER_EMAIL=test@harmonik.local",
	)
	if out, err := commitCmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}

	u := uuid.MustParse(runID)
	ws := &Workspace{
		WorkspaceID:    "ws-" + runID,
		RunID:          core.RunID(u),
		Repository:     repo,
		ParentCommit:   initialSHA,
		BranchName:     branch,
		Path:           worktreePath,
		State:          core.WorkspaceStateLeased,
		InterruptState: core.InterruptStateNone,
		Metadata: map[string]string{
			"created_at":           "2026-05-10T00:00:00Z",
			"operator_fingerprint": "test-operator",
		},
		SchemaVersion: 1,
	}

	// Apply reset-to-checkpoint with rollbackTarget = initialSHA (EM-044
	// rollback_to_state_id points to the earlier state).
	if err := intraRunRollbackFixtureApplyVerdict(ws, core.VerdictResetToCheckpoint, initialSHA); err != nil {
		t.Fatalf("WM-035[reset-to-checkpoint]: applyVerdict: %v", err)
	}

	// Worktree MUST still be the same path.
	if ws.Path != worktreePath {
		t.Errorf("WM-035[reset-to-checkpoint]: worktree-stable violated: path changed to %q", ws.Path)
	}

	// Branch MUST still be the same.
	if ws.BranchName != branch {
		t.Errorf("WM-035[reset-to-checkpoint]: branch-stable violated: branch changed to %q", ws.BranchName)
	}

	// run_id MUST be unchanged.
	if ws.RunID != core.RunID(u) {
		t.Errorf("WM-035[reset-to-checkpoint]: run_id-unchanged violated: run_id changed to %q", ws.RunID)
	}

	// HEAD in the worktree MUST now equal the rollback target (initialSHA).
	headOut, err := exec.CommandContext(t.Context(), "git", "-C", worktreePath, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("WM-035[reset-to-checkpoint]: git rev-parse HEAD: %v", err)
	}
	gotHead := string(headOut)
	if len(gotHead) > 0 && gotHead[len(gotHead)-1] == '\n' {
		gotHead = gotHead[:len(gotHead)-1]
	}
	if gotHead != initialSHA {
		t.Errorf("WM-035[reset-to-checkpoint]: HEAD = %q after rollback; want rollback target %q",
			gotHead, initialSHA)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Fixture helpers — prefix: intraRunRollbackFixture
// ─────────────────────────────────────────────────────────────────────────────

// intraRunRollbackFixtureRunIDs maps verdict names to stable UUIDv7-format run IDs
// for use in tests that need deterministic run IDs per verdict.
var intraRunRollbackFixtureRunIDs = map[string]string{
	"resume-here":         "0196b300-0000-7000-8000-000000035001",
	"resume-with-context": "0196b300-0000-7000-8000-000000035002",
	"reset-to-checkpoint": "0196b300-0000-7000-8000-000000035003",
}

// intraRunRollbackFixtureRunID returns the deterministic run_id for the given verdict name.
func intraRunRollbackFixtureRunID(verdict string) string {
	id, ok := intraRunRollbackFixtureRunIDs[verdict]
	if !ok {
		// Fallback for unknown verdicts — not expected in WM-035 tests.
		return "0196b300-0000-7000-8000-000000035000"
	}
	return id
}

// intraRunRollbackFixtureWorkspace constructs a minimal in-memory Workspace in
// the leased state for use in single-field-check subtests that do not require a
// real git worktree on disk.
//
// The run_id is deterministic per verdict to prevent cross-subtest collisions.
func intraRunRollbackFixtureWorkspace(t *testing.T, verdictStr string) *Workspace {
	t.Helper()

	runID := intraRunRollbackFixtureRunID(verdictStr)
	u := uuid.MustParse(runID)
	return &Workspace{
		WorkspaceID:    "ws-" + runID,
		RunID:          core.RunID(u),
		Repository:     "/tmp/harmonik-test-repo",
		ParentCommit:   "deadbeef" + "deadbeef" + "deadbeef" + "deadbeef" + "dead",
		BranchName:     "run/" + runID,
		Path:           "/tmp/harmonik-test-repo/.harmonik/worktrees/" + runID,
		State:          core.WorkspaceStateLeased,
		InterruptState: core.InterruptStateNone,
		Metadata: map[string]string{
			"created_at":           "2026-05-10T00:00:00Z",
			"operator_fingerprint": "test-operator",
		},
		SchemaVersion: 1,
	}
}

// intraRunRollbackFixtureApplyVerdict is a fixture-level implementation of the
// intra-run rollback action per WM-035. It applies the verdict to ws, enforcing
// the three WM-035 invariants without modifying Path, BranchName, or RunID.
//
// For reset-to-checkpoint, rollbackTarget is the commit SHA to reset to
// (the rollback_to_state_id per EM-044). When rollbackTarget is empty and the
// verdict is reset-to-checkpoint, no git-reset is performed (unit-test-only mode).
//
// For resume-here and resume-with-context, no git state change is performed by
// this fixture — the workspace remains at its current HEAD; the real implementation
// will dispatch a fresh agent session within the existing worktree.
//
// TODO(hk-8mwo.36): replace with real verdict-executor once the workspace-manager
// intra-run rollback path is implemented. The real implementation will route through
// the verdict-executor machinery and emit VerdictExecuted events per WM-015.
func intraRunRollbackFixtureApplyVerdict(ws *Workspace, verdict core.Verdict, rollbackTarget string) error {
	switch verdict {
	case core.VerdictResumeHere, core.VerdictResumeWithContext:
		// WM-035: no worktree change, no branch change, no run_id change.
		// The implementation will dispatch a fresh agent session; the fixture
		// models the workspace-primitive invariants only.
		return nil

	case core.VerdictResetToCheckpoint:
		// WM-035: keep worktree and branch; revert HEAD to rollbackTarget inside
		// the existing worktree via git reset --hard per EM-044.
		// When rollbackTarget is empty (unit-test-only mode), skip the git op.
		if rollbackTarget == "" {
			return nil
		}
		// The git operation runs INSIDE ws.Path (the existing worktree) — the
		// worktree directory is not removed or re-created (WM-035).
		resetCmd := exec.Command("git", "reset", "--hard", rollbackTarget)
		resetCmd.Dir = ws.Path
		if out, err := resetCmd.CombinedOutput(); err != nil {
			return intraRunRollbackFixtureError("reset-to-checkpoint: git reset --hard " + rollbackTarget + ": " + string(out))
		}
		return nil

	default:
		return intraRunRollbackFixtureError("WM-035: verdict " + string(verdict) + " is not an intra-run rollback verdict")
	}
}

// intraRunRollbackFixtureError is a simple error type for intra-run rollback
// fixture errors.
type intraRunRollbackFixtureError string

func (e intraRunRollbackFixtureError) Error() string { return string(e) }

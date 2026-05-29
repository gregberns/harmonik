package daemon_test

// chb023_review_loop_resume_test.go — 3-iteration review-loop test verifying
// claude_session_id stability across implementer-resume launches (hk-cw56j).
//
// Exercises CHB-023 durable-checkpoint commit across daemon restart:
//
//  1. Iteration 1 (implementer-initial): persistClaudeSessionID is called with a
//     known session ID, writing a checkpoint commit to the task branch.
//  2. Daemon-restart simulation: the session ID is recovered from the git
//     checkpoint commit (reading context.json from git HEAD), mirroring what a
//     restarted daemon does via state-reconstruction (EM-031).
//  3. Iterations 2 and 3 (implementer-resume): buildClaudeLaunchSpec is called
//     with the recovered session ID as PriorClaudeSessID; the resulting LaunchSpec
//     MUST carry --resume (CHB-008), not --session-id, and the embedded
//     ClaudeSessionID MUST equal the iter-1 value (stability invariant).
//
// # Invariants tested
//
//  - CHB-023 persist: after iter 1, a git commit exists at the task branch tip
//    with a context.json file containing the correct claude_session_id.
//  - CHB-023 recovery: the session ID read from git equals the one persisted.
//  - CHB-008 --resume: buildClaudeLaunchSpec uses --resume for implementer-resume,
//    not --session-id; and the resulting ClaudeSessionID is the same value.
//  - Stability: all three implementer phases (iter 1 initial + iter 2 resume +
//    iter 3 resume) reference the same claude_session_id value, satisfying the
//    "stable across implementer-resume launches" invariant from CHB-021 §10.
//
// # Daemon-restart simulation
//
// A real daemon restart would stop the running process and start a new one,
// which would then perform state-reconstruction (EM-031) by scanning the task
// branch git log for the CHB-023 checkpoint commit and reading context.json.
// This test simulates only the observable contract: after persistClaudeSessionID
// returns, context.json is readable from git HEAD and contains the correct
// session ID — sufficient to guarantee recovery correctness without starting a
// full daemon process.
//
// Helper prefix: rl3Fixture (bead hk-cw56j;
// per implementer-protocol.md §Helper-prefix discipline).
//
// Spec refs:
//   - specs/claude-hook-bridge.md §4.3.CHB-008 (--resume for resume phase)
//   - specs/claude-hook-bridge.md §4.6.CHB-023 (durable-checkpoint commit)
//   - specs/execution-model.md §4.7.EM-031 (state-reconstruction on restart)
//   - specs/claude-hook-bridge.md §4.8.CHB-021 §10 (3-iter scenario)
//
// Bead: hk-cw56j.

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// ─────────────────────────────────────────────────────────────────────────────
// Fixtures
// ─────────────────────────────────────────────────────────────────────────────

// rl3FixtureGitRepo initialises a bare git repository in dir with a single
// initial commit on main, then checks out a task branch named "run/<runID>".
func rl3FixtureGitRepo(t *testing.T, dir, runID string) {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
		//nolint:gosec // G204: git args are test-internal literals; not user input
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("rl3FixtureGitRepo: git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "--initial-branch=main")
	run("config", "user.email", "test@harmonik.local")
	run("config", "user.name", "Harmonik Test")
	readmePath := filepath.Join(dir, "README")
	if err := os.WriteFile(readmePath, []byte("harmonik hk-cw56j chb023 resume test\n"), 0o644); err != nil {
		t.Fatalf("rl3FixtureGitRepo: WriteFile README: %v", err)
	}
	run("add", "README")
	run("commit", "-m", "Initial commit")
	run("checkout", "-b", "run/"+runID)
}

// rl3FixtureRunID returns a test RunID derived from a fresh UUIDv7.
func rl3FixtureRunID(t *testing.T) core.RunID {
	t.Helper()
	u, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("rl3FixtureRunID: uuid.NewV7: %v", err)
	}
	return core.RunID(u)
}

// rl3FixtureWorkspaceDir creates a minimal workspace directory with a .claude/
// subdirectory, suitable for buildClaudeLaunchSpec.
func rl3FixtureWorkspaceDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".claude"), 0o755); err != nil {
		t.Fatalf("rl3FixtureWorkspaceDir: MkdirAll .claude: %v", err)
	}
	return dir
}

// rl3FixtureReadContextFileFromGit reads the CHB-023 context.json file from
// the git HEAD of dir and returns the parsed claude_session_id.
// This mirrors what a restarted daemon does during state-reconstruction (EM-031).
func rl3FixtureReadContextFileFromGit(t *testing.T, dir string, runID core.RunID) string {
	t.Helper()
	relPath := ".harmonik/run-context/" + runID.String() + "/context.json"
	//nolint:gosec // G204: git args are test-internal literals; not user input
	cmd := exec.CommandContext(t.Context(), "git", "show", "HEAD:"+relPath)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("rl3FixtureReadContextFileFromGit: git show HEAD:%s: %v", relPath, err)
	}
	var ctxFile struct {
		ClaudeSessionID string `json:"claude_session_id"`
	}
	if jsonErr := json.Unmarshal(out, &ctxFile); jsonErr != nil {
		t.Fatalf("rl3FixtureReadContextFileFromGit: unmarshal: %v", jsonErr)
	}
	return ctxFile.ClaudeSessionID
}

// rl3FixtureAssertResumeFlag verifies that args contains "--resume" followed
// by a non-empty session ID, and that "--session-id" is absent.
// Satisfies CHB-008: implementer-resume MUST use --resume.
func rl3FixtureAssertResumeFlag(t *testing.T, args []string, wantSessionID string) {
	t.Helper()
	for i, a := range args {
		if a == "--resume" {
			if i+1 >= len(args) || args[i+1] == "" {
				t.Error("CHB-008: --resume present but session ID value is missing")
				return
			}
			if args[i+1] != wantSessionID {
				t.Errorf("CHB-008: --resume session ID = %q; want %q", args[i+1], wantSessionID)
			}
			for _, b := range args {
				if b == "--session-id" {
					t.Error("CHB-008: --session-id must not coexist with --resume for implementer-resume phase")
				}
			}
			return
		}
	}
	t.Errorf("CHB-008: --resume not found in args %v; required for implementer-resume phase", args)
}

// rl3FixtureAssertCHB023CommitExists verifies that the git log in dir contains
// at least one commit whose message references CHB-023, confirming the checkpoint
// commit was made.
func rl3FixtureAssertCHB023CommitExists(t *testing.T, dir string, runID core.RunID) {
	t.Helper()
	//nolint:gosec // G204: git args are test-internal literals; not user input
	cmd := exec.CommandContext(t.Context(), "git", "log", "--oneline", "--grep=CHB-023")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("rl3FixtureAssertCHB023CommitExists: git log: %v", err)
	}
	hits := 0
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line != "" {
			hits++
		}
	}
	if hits == 0 {
		t.Errorf("CHB-023: expected at least one commit mentioning CHB-023 in git log for run %s; got 0", runID)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests
// ─────────────────────────────────────────────────────────────────────────────

// TestReviewLoopResume_CHB023_DurableCheckpointAndRecovery verifies that after
// iter 1 implementer-initial, the claude_session_id checkpoint commit written by
// persistClaudeSessionID (CHB-023) is readable from git HEAD and contains the
// correct session ID — the "daemon restart" recovery invariant (EM-031).
func TestReviewLoopResume_CHB023_DurableCheckpointAndRecovery(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	runID := rl3FixtureRunID(t)
	rl3FixtureGitRepo(t, dir, runID.String())

	const implSessionID = "claude-impl-rl3-cw56j-001"

	// Iter 1 implementer-initial: persist session ID to git (CHB-023).
	sha, skipped, err := daemon.ExportedPersistClaudeSessionID(t.Context(), dir, runID, implSessionID)
	if err != nil {
		t.Fatalf("PersistClaudeSessionID iter 1: %v", err)
	}
	if skipped {
		t.Error("PersistClaudeSessionID iter 1: got Skipped=true for non-empty session ID")
	}
	if sha == "" {
		t.Error("PersistClaudeSessionID iter 1: CommitSHA is empty")
	}

	// Verify the CHB-023 checkpoint commit exists in git log.
	rl3FixtureAssertCHB023CommitExists(t, dir, runID)

	// Daemon-restart simulation (EM-031): read context.json from git HEAD.
	// A restarted daemon would scan the task branch log for the Harmonik-Context-Event
	// trailer and read the context file from that commit.
	recoveredSessionID := rl3FixtureReadContextFileFromGit(t, dir, runID)
	if recoveredSessionID != implSessionID {
		t.Errorf("EM-031 recovery: recovered session ID = %q; want %q", recoveredSessionID, implSessionID)
	}
}

// TestReviewLoopResume_CHB023_SessionIDStabilityAcross3Iterations verifies the
// end-to-end stability invariant for the 3-iteration review-loop scenario
// (CHB-021 §10 class 2):
//
//   - Iter 1: implementer-initial — session ID persisted to git (CHB-023).
//   - Daemon restart simulation: session ID recovered from git checkpoint.
//   - Iter 2: implementer-resume — buildClaudeLaunchSpec uses recovered ID with
//     --resume flag (CHB-008); ClaudeSessionID == iter-1 ID.
//   - Iter 3: implementer-resume — same as iter 2; ClaudeSessionID still stable.
//
// Stability invariant: all three implementer phases reference the same
// claude_session_id value (CHB-021 §10 "claude_session_id stable across
// implementer-resume launches").
func TestReviewLoopResume_CHB023_SessionIDStabilityAcross3Iterations(t *testing.T) {
	t.Parallel()
	gitDir := t.TempDir()
	ws := rl3FixtureWorkspaceDir(t)
	runID := rl3FixtureRunID(t)
	rl3FixtureGitRepo(t, gitDir, runID.String())

	const implSessionID = "claude-impl-rl3-cw56j-stab-001"

	// ── Iteration 1: implementer-initial ─────────────────────────────────────
	//
	// Persist the session ID to git as the CHB-023 durable checkpoint.

	_, _, err := daemon.ExportedPersistClaudeSessionID(t.Context(), gitDir, runID, implSessionID)
	if err != nil {
		t.Fatalf("PersistClaudeSessionID iter 1: %v", err)
	}

	// ── Daemon-restart simulation (EM-031) ────────────────────────────────────
	//
	// Read the session ID back from the committed context.json, mirroring what
	// a restarted daemon does via state-reconstruction before launching iter 2.

	recoveredID := rl3FixtureReadContextFileFromGit(t, gitDir, runID)
	if recoveredID != implSessionID {
		t.Fatalf("EM-031: recovered session ID = %q; want %q (CHB-023 persist failed to round-trip)",
			recoveredID, implSessionID)
	}

	// ── Iteration 2: implementer-resume ──────────────────────────────────────
	//
	// Build launch spec using the recovered session ID. Must use --resume (CHB-008).

	rcIter2 := daemon.ExportedClaudeRunCtx{
		RunID:             runID,
		BeadID:            "hk-cw56j-test-bead",
		WorkspacePath:     ws,
		DaemonSocket:      "/tmp/harmonik-test-cw56j.sock",
		WorkflowMode:      core.WorkflowModeReviewLoop,
		Phase:             handlercontract.ReviewLoopPhaseImplementerResume,
		IterationCount:    2,
		PriorClaudeSessID: &recoveredID,
		HandlerBinary:     "claude",
		BaseEnv:           []string{"HARMONIK_PROJECT_HASH=cw56j-test-deadbeef"},
	}
	spec2, arts2, err2 := daemon.ExportedBuildClaudeLaunchSpec(context.Background(), rcIter2)
	if err2 != nil {
		t.Fatalf("BuildClaudeLaunchSpec iter 2: %v", err2)
	}

	// CHB-008: --resume must be present for implementer-resume phase.
	rl3FixtureAssertResumeFlag(t, spec2.Args, implSessionID)

	// Stability: ClaudeSessionID in artifacts == iter-1 ID.
	if arts2.ClaudeSessionID != implSessionID {
		t.Errorf("iter 2: ClaudeSessionID = %q; want stable iter-1 ID %q (CHB-021 §10)",
			arts2.ClaudeSessionID, implSessionID)
	}

	// ── Iteration 3: implementer-resume ──────────────────────────────────────
	//
	// Same as iter 2 — state.claudeSessionID is carried through from iter 1.

	priorForIter3 := arts2.ClaudeSessionID // same as recoveredID
	rcIter3 := daemon.ExportedClaudeRunCtx{
		RunID:             runID,
		BeadID:            "hk-cw56j-test-bead",
		WorkspacePath:     ws,
		DaemonSocket:      "/tmp/harmonik-test-cw56j.sock",
		WorkflowMode:      core.WorkflowModeReviewLoop,
		Phase:             handlercontract.ReviewLoopPhaseImplementerResume,
		IterationCount:    3,
		PriorClaudeSessID: &priorForIter3,
		HandlerBinary:     "claude",
		BaseEnv:           []string{"HARMONIK_PROJECT_HASH=cw56j-test-deadbeef"},
	}
	spec3, arts3, err3 := daemon.ExportedBuildClaudeLaunchSpec(context.Background(), rcIter3)
	if err3 != nil {
		t.Fatalf("BuildClaudeLaunchSpec iter 3: %v", err3)
	}

	// CHB-008: --resume must be present for implementer-resume phase.
	rl3FixtureAssertResumeFlag(t, spec3.Args, implSessionID)

	// Stability: ClaudeSessionID in artifacts == iter-1 ID (unchanged across 3 iters).
	if arts3.ClaudeSessionID != implSessionID {
		t.Errorf("iter 3: ClaudeSessionID = %q; want stable iter-1 ID %q (CHB-021 §10)",
			arts3.ClaudeSessionID, implSessionID)
	}

	// ── Cross-iteration stability assertion ───────────────────────────────────
	//
	// All three implementer phases must reference the SAME session ID.
	// iter1 = implSessionID (persisted)
	// iter2 = arts2.ClaudeSessionID (recovered + --resume)
	// iter3 = arts3.ClaudeSessionID (carried through + --resume)

	if arts2.ClaudeSessionID != arts3.ClaudeSessionID {
		t.Errorf("cross-iteration stability: iter2 ClaudeSessionID = %q, iter3 = %q; must be equal (CHB-021 §10)",
			arts2.ClaudeSessionID, arts3.ClaudeSessionID)
	}
	_ = spec2 // already asserted via rl3FixtureAssertResumeFlag
	_ = spec3 // already asserted via rl3FixtureAssertResumeFlag
}

// TestReviewLoopResume_CHB023_ReviewerAlwaysFresh verifies the complementary
// invariant (CHB-009): reviewer phases in the 3-iteration loop always mint a
// fresh session ID, distinct from the stable implementer ID.
//
// This confirms that the stability invariant applies only to implementer phases.
func TestReviewLoopResume_CHB023_ReviewerAlwaysFresh(t *testing.T) {
	t.Parallel()
	ws := rl3FixtureWorkspaceDir(t)
	runID := rl3FixtureRunID(t)

	const implSessionID = "claude-impl-rl3-cw56j-fresh-001"

	// Build reviewer launch specs for all 3 reviewer iterations.
	// CHB-009: reviewer MUST NOT receive a priorClaudeSessID (passing one is a
	// daemon defect per MintClaudeSessionID; we call with nil as the daemon does).
	reviewerIDs := make([]string, 3)
	for i := 0; i < 3; i++ {
		rcRev := daemon.ExportedClaudeRunCtx{
			RunID:             runID,
			BeadID:            "hk-cw56j-test-bead",
			WorkspacePath:     ws,
			DaemonSocket:      "/tmp/harmonik-test-cw56j-rev.sock",
			WorkflowMode:      core.WorkflowModeReviewLoop,
			Phase:             handlercontract.ReviewLoopPhaseReviewer,
			IterationCount:    i + 1,
			PriorClaudeSessID: nil, // CHB-009: reviewer never inherits prior session
			HandlerBinary:     "claude",
			BaseEnv:           []string{"HARMONIK_PROJECT_HASH=cw56j-test-deadbeef"},
		}
		_, arts, err := daemon.ExportedBuildClaudeLaunchSpec(context.Background(), rcRev)
		if err != nil {
			t.Fatalf("BuildClaudeLaunchSpec reviewer iter %d: %v", i+1, err)
		}
		reviewerIDs[i] = arts.ClaudeSessionID

		// CHB-009: reviewer session ID must differ from the implementer session ID.
		if arts.ClaudeSessionID == implSessionID {
			t.Errorf("CHB-009: reviewer iter %d reused implementer session ID %q; must be fresh",
				i+1, implSessionID)
		}
		if arts.ClaudeSessionID == "" {
			t.Errorf("CHB-009: reviewer iter %d ClaudeSessionID is empty", i+1)
		}
	}

	// CHB-009: each reviewer iteration must mint a distinct session ID.
	if reviewerIDs[0] == reviewerIDs[1] {
		t.Errorf("CHB-009: reviewer iter 1 and iter 2 share session ID %q; must be distinct", reviewerIDs[0])
	}
	if reviewerIDs[1] == reviewerIDs[2] {
		t.Errorf("CHB-009: reviewer iter 2 and iter 3 share session ID %q; must be distinct", reviewerIDs[1])
	}
}

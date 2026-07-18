package workspace

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
)

// workspace_w4_regressions_test.go — Wave-4 mega-review regression tests:
//
//  1. DetectSquashMergeConflict is a side-effect-free probe: the worktree is
//     byte-clean afterward in BOTH the no-conflict and conflict cases
//     (previously the trial `git merge --squash` left a staged squash on
//     success and conflict markers + a half-staged index on conflict).
//  2. WriteLeaseLockAtomic is test-and-set: a second claim on the same path
//     fails with ErrLeaseAlreadyHeld instead of silently overwriting the
//     holder's lease; release re-opens the path for claiming.

// gitOutput runs git in dir and returns trimmed stdout, failing t on error.
func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// assertWorktreeByteClean asserts dir has an empty `git status --porcelain`
// and that HEAD equals wantHEAD.
func assertWorktreeByteClean(t *testing.T, dir, wantHEAD, label string) {
	t.Helper()
	if status := gitOutput(t, dir, "status", "--porcelain"); status != "" {
		t.Errorf("%s: worktree not clean after probe; git status --porcelain:\n%s", label, status)
	}
	if head := gitOutput(t, dir, "rev-parse", "HEAD"); head != wantHEAD {
		t.Errorf("%s: HEAD moved after probe: got %s, want %s", label, head, wantHEAD)
	}
}

// TestW4_DetectSquashMergeConflict_NoConflict_LeavesWorktreeClean verifies the
// success (detection-probe) path resets the staged trial squash: without the
// reset, a later real merge would double-apply the staged changes.
func TestW4_DetectSquashMergeConflict_NoConflict_LeavesWorktreeClean(t *testing.T) {
	t.Parallel()

	repo, sha := mergeBackFixtureSetupTaskBranch(t,
		"0196b100-0000-7000-8000-0000004a0001",
		[]string{"clean change"},
	)
	integPath := mergeBackFixtureMakeIntegWorktree(t, repo, sha, "integ-w4-clean")
	taskBranch := "run/0196b100-0000-7000-8000-0000004a0001"

	headBefore := gitOutput(t, integPath, "rev-parse", "HEAD")

	result, err := DetectSquashMergeConflict(integPath, taskBranch)
	if err != nil {
		t.Fatalf("DetectSquashMergeConflict: %v", err)
	}
	if result.HasConflict {
		t.Fatalf("HasConflict = true, want false (reason %q)", result.Reason)
	}

	assertWorktreeByteClean(t, integPath, headBefore, "no-conflict probe")

	// The probe must be repeatable with the same answer (no leftover state).
	again, err := DetectSquashMergeConflict(integPath, taskBranch)
	if err != nil {
		t.Fatalf("DetectSquashMergeConflict (repeat): %v", err)
	}
	if again.HasConflict {
		t.Errorf("repeat probe: HasConflict = true, want false (reason %q)", again.Reason)
	}
	assertWorktreeByteClean(t, integPath, headBefore, "no-conflict probe repeat")
}

// TestW4_DetectSquashMergeConflict_Conflict_LeavesWorktreeClean verifies the
// conflict path resets: `--squash` sets no MERGE_HEAD, so without an explicit
// reset the worktree was left with conflict markers and a half-staged index.
func TestW4_DetectSquashMergeConflict_Conflict_LeavesWorktreeClean(t *testing.T) {
	t.Parallel()

	repo, sha := tempRepo(t)
	runID := "0196b100-0000-7000-8000-0000004a0002"
	taskBranch := "run/" + runID
	taskPath := filepath.Join(repo, ".harmonik", "worktrees", runID)
	if err := os.MkdirAll(filepath.Dir(taskPath), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	gitOutput(t, repo, "worktree", "add", "-b", taskBranch, taskPath, sha)
	if err := os.WriteFile(filepath.Join(taskPath, "shared.txt"), []byte("task version\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	gitOutput(t, taskPath, "add", ".")
	gitOutput(t, taskPath, "commit", "-m", "task: change shared.txt")

	integPath := filepath.Join(repo, ".harmonik", "worktrees", "integ-w4-clash")
	gitOutput(t, repo, "worktree", "add", "-b", "harmonik/integration/integ-w4-clash", integPath, sha)
	if err := os.WriteFile(filepath.Join(integPath, "shared.txt"), []byte("integration version\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	gitOutput(t, integPath, "add", ".")
	gitOutput(t, integPath, "commit", "-m", "integ: change shared.txt")

	headBefore := gitOutput(t, integPath, "rev-parse", "HEAD")
	sharedBefore, err := os.ReadFile(filepath.Join(integPath, "shared.txt"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	result, err := DetectSquashMergeConflict(integPath, taskBranch)
	if err != nil {
		t.Fatalf("DetectSquashMergeConflict: %v", err)
	}
	if !result.HasConflict {
		t.Fatal("HasConflict = false, want true")
	}

	assertWorktreeByteClean(t, integPath, headBefore, "conflict probe")

	// The conflicted file must be restored byte-for-byte (no conflict markers).
	sharedAfter, err := os.ReadFile(filepath.Join(integPath, "shared.txt"))
	if err != nil {
		t.Fatalf("ReadFile after: %v", err)
	}
	if string(sharedAfter) != string(sharedBefore) {
		t.Errorf("shared.txt mutated by probe:\nbefore: %q\nafter:  %q", sharedBefore, sharedAfter)
	}
}

// TestW4_WriteLeaseLockAtomic_SecondClaimFails verifies test-and-set lease
// acquisition: a second claimant on the same path must fail (previously the
// rename-based write silently overwrote the holder's lease), and release
// re-opens the path.
func TestW4_WriteLeaseLockAtomic_SecondClaimFails(t *testing.T) {
	t.Parallel()

	wsPath := t.TempDir()
	target := LeaseLockPath(wsPath)

	newLock := func(runID string) *core.LeaseLockFile {
		return &core.LeaseLockFile{
			RunID:     core.RunID(uuid.MustParse(runID)),
			PID:       os.Getpid(),
			CreatedAt: time.Now().UTC(),
			TTLSec:    3600,
		}
	}

	first := newLock("0196b100-0000-7000-8000-0000004a0011")
	if err := WriteLeaseLockAtomic(target, first); err != nil {
		t.Fatalf("first claim: %v", err)
	}

	second := newLock("0196b100-0000-7000-8000-0000004a0012")
	err := WriteLeaseLockAtomic(target, second)
	if err == nil {
		t.Fatal("second claim on held lease succeeded, want failure")
	}
	if !errors.Is(err, ErrLeaseAlreadyHeld) {
		t.Fatalf("second claim error = %v, want errors.Is(_, ErrLeaseAlreadyHeld)", err)
	}

	// The holder's lease content must be untouched by the failed claim.
	got, err := ReadLeaseLock(target)
	if err != nil {
		t.Fatalf("ReadLeaseLock: %v", err)
	}
	if got == nil || got.RunID.String() != first.RunID.String() {
		t.Fatalf("lease content clobbered: got %+v, want run_id %s", got, first.RunID)
	}

	// No temp-file litter from the failed claim.
	entries, err := os.ReadDir(filepath.Dir(target))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Errorf("temp file left behind by failed claim: %s", e.Name())
		}
	}

	// After release, the path can be claimed again.
	if err := ReleaseLeaseLock(target); err != nil {
		t.Fatalf("ReleaseLeaseLock: %v", err)
	}
	if err := WriteLeaseLockAtomic(target, second); err != nil {
		t.Fatalf("re-claim after release: %v", err)
	}
}

package daemon

// stripruncontext_hk4je.go — strip .harmonik/run-context/** from the run-branch
// at the merge-to-main step.
//
// # Problem (hk-4je)
//
// CHB-023 (sessioncontext_chb023.go) force-commits context.json to the run's
// task branch via `git add -f` so that the crash-recovery path (EM-031) can
// find the session ID via `git log --grep Harmonik-Run-ID`.  The idempotency
// guard only prevents a duplicate commit for the *same* session ID — it does
// not prevent the per-run commit itself.  As a result, every run's context.json
// was fast-forwarded onto main (37 % of trunk commits, 117 files tracked).
//
// # Fix
//
// Strip .harmonik/run-context/** from the run-branch INDEX just before the
// fast-forward update-ref.  We create a strip commit in the run-branch worktree
// so the run-branch tip (used as the FF target) no longer includes those paths.
//
// This is safe because:
//   - The strip happens at the merge step — after the implementer/reviewer
//     sessions have exited and the crash-recovery window has closed.
//   - The strip commit only removes the index entry; it does NOT delete the
//     object from git's object store.  The CHB-023 commit is still reachable
//     via the run-branch's reflog and the git object DB for the duration of git
//     gc's window.
//   - Safety verified during design: zero runtime readers of context.json from
//     main.  session_id is in-memory state.claudeSessionID in reviewloop.go;
//     recovery reads the run worktree via git log --grep Harmonik-Run-ID, never
//     main (EM-031).
//
// Spec refs:
//   - specs/claude-hook-bridge.md §4.6.CHB-023
//   - specs/execution-model.md §4.7.EM-031, §4.12.EM-052
//
// Bead: hk-4je.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// stripRunContextFromMerge removes any .harmonik/run-context/** paths from the
// run-branch index and commits the removal, so they cannot land on the merge
// target via the subsequent fast-forward update-ref.
//
// The function is a no-op when:
//   - wtPath does not exist (worktree already removed, very rare edge case), or
//   - no .harmonik/run-context/** files are tracked in the worktree index.
//
// When a strip commit IS made, the run-branch HEAD advances by one commit and
// the caller MUST re-resolve runTip to pick up the new SHA.
//
// Returns stripped=true when the strip commit was created, false otherwise.
//
// Bead: hk-4je.
func stripRunContextFromMerge(ctx context.Context, wtPath string) (stripped bool, err error) {
	if _, statErr := os.Stat(wtPath); statErr != nil {
		// Worktree was already removed — nothing to strip.
		return false, nil
	}

	// Check whether any .harmonik/run-context/** paths are tracked in the index.
	lsCmd := exec.CommandContext(ctx, "git", "ls-files", "--cached", "--", runContextDirPrefix)
	lsCmd.Dir = wtPath
	lsOut, lsErr := lsCmd.Output()
	if lsErr != nil {
		return false, fmt.Errorf("daemon: stripRunContextFromMerge: git ls-files --cached: %w", lsErr)
	}
	if strings.TrimSpace(string(lsOut)) == "" {
		// Nothing tracked — nothing to strip.
		return false, nil
	}

	// Remove from the index only (--cached keeps the working-tree files).
	// --ignore-unmatch is defensive: if a concurrent operation already removed
	// some entries the command should not fail.
	rmCmd := exec.CommandContext(ctx, "git", "rm", "--cached", "-r", "--ignore-unmatch", "--", runContextDirPrefix)
	rmCmd.Dir = wtPath
	if out, rmErr := rmCmd.CombinedOutput(); rmErr != nil {
		return false, fmt.Errorf("daemon: stripRunContextFromMerge: git rm --cached -r: %w\ngit output: %s", rmErr, out)
	}

	// Commit the removal onto the run-branch so the fast-forward update-ref
	// carries a clean tree to main.
	commitMsg := "harmonik: strip run-context from merge (hk-4je)\n\n" +
		"Remove .harmonik/run-context/** that was force-committed by CHB-023 for\n" +
		"crash-recovery (EM-031). The files remain valid on the run-branch reflog;\n" +
		"they must not land on the merge target."
	commitCmd := exec.CommandContext(ctx, "git", "commit", "-m", commitMsg)
	commitCmd.Dir = wtPath
	if out, commitErr := commitCmd.CombinedOutput(); commitErr != nil {
		return false, fmt.Errorf("daemon: stripRunContextFromMerge: git commit: %w\ngit output: %s", commitErr, out)
	}

	return true, nil
}

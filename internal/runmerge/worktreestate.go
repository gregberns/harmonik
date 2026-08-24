package runmerge

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/gitprobe"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

func mergedCommitPaths(ctx context.Context, projectDir, mainTip, runTip string) ([]string, error) {
	out, err := gitprobe.Output(ctx, projectDir, "diff", "--name-only", mainTip, runTip)
	if err != nil {
		return nil, fmt.Errorf("git diff --name-only %s %s: %w", mainTip, runTip, err)
	}
	var paths []string
	for _, p := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if p != "" {
			paths = append(paths, p)
		}
	}
	return paths, nil
}

func refreshMergedPaths(ctx context.Context, projectDir string, runID core.RunID, bus handlercontract.EventEmitter, beadID core.BeadID, mainTip string, paths []string) {
	overwritten := locallyEditedPaths(ctx, projectDir, mainTip, paths)
	if len(overwritten) > 0 {
		patchPath := writeRecoveryPatch(ctx, projectDir, runID, mainTip, overwritten)
		fmt.Fprintf(os.Stderr, "daemon: RunBranchToTarget: WARNING: post-merge refresh overwrote %d uncommitted local edit(s) in main (bead %s run %s): %s; recovery patch: %s\n",
			len(overwritten), beadID, runID.String(), strings.Join(overwritten, ", "), patchPath)
		emitWorkingTreeLocalEditsOverwritten(ctx, bus, runID, beadID, projectDir, overwritten, patchPath)
	}

	// The path list goes to git on standard input. gitprobe holds the list as a
	// string and makes a new reader for each attempt, so a second attempt gets
	// the same paths as the first one.
	out, restoreErr := gitprobe.CombinedOutputStdin(ctx, projectDir, strings.Join(paths, "\n")+"\n",
		"restore", "--source=HEAD", "--staged", "--worktree", "--pathspec-from-file=-")
	if restoreErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: RunBranchToTarget: WARNING: scoped working-tree refresh failed (bead %s run %s): %v\n%s",
			beadID, runID.String(), restoreErr, out)
		emitWorkingTreeRefreshFailed(ctx, bus, runID, beadID, restoreErr)
	}
}

func locallyEditedPaths(ctx context.Context, projectDir, mainTip string, paths []string) []string {
	args := append([]string{"diff", "--name-only", mainTip, "--"}, paths...)
	out, err := gitprobe.Output(ctx, projectDir, args...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "daemon: RunBranchToTarget: WARNING: local-edits diagnostic failed against %s: %v\n", mainTip, err)
		return nil
	}
	var edited []string
	for _, p := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if p != "" {
			edited = append(edited, p)
		}
	}
	return edited
}

func writeRecoveryPatch(ctx context.Context, projectDir string, runID core.RunID, mainTip string, paths []string) string {
	dir := filepath.Join(projectDir, ".harmonik", "recovery")
	if err := os.MkdirAll(dir, 0o700); err != nil { //dirmode:allow tighter on purpose: .harmonik/recovery/ holds rescued uncommitted work, 0o700 (never widen to core.HarmonikDirMode)
		return ""
	}
	args := append([]string{"diff", mainTip, "--"}, paths...)
	patch, err := gitprobe.Output(ctx, projectDir, args...)
	if err != nil || len(patch) == 0 {
		return ""
	}
	dest := filepath.Join(dir, "worktree-refresh-"+runID.String()+".patch")
	if err := os.WriteFile(dest, patch, 0o644); err != nil { //nolint:gosec // G306: a recovery patch is operator-readable by design
		return ""
	}
	return dest
}

// DiscardDirtyChurn discards UNCOMMITTED changes to daemon/agent-owned churn
// files in the run worktree, restoring each to the run-branch's committed
// version, so `git rebase main` can proceed.
//
// `git rebase` refuses to start when the worktree has unstaged changes
// (it aborts with "error: cannot rebase: You have unstaged changes" before any
// conflict detection). Two distinct tracked files get dirtied during every run
// without the implementer ever touching them as part of its task work:
//
//   - .beads/issues.jsonl — the bead ledger. Becomes dirty whenever a `br`
//     operation flushes its shared SQLite DB to the per-worktree JSONL during the
//     run. Its canonical source of truth is main (the daemon owns all terminal
//     bead transitions).
//   - .claude/settings.json — the Claude hook-bridge settings. The daemon's
//     MaterializeClaudeSettings (CHB-001..005) merges the bridge hooks +
//     permissions.allow into the worktree copy on every launch, and the running
//     claude agent may further mutate it. Because this repo TRACKS the file (the
//     root .gitignore only covers /.claude/worktrees/, not .claude/settings.json),
//     the per-launch materialization leaves it modified-but-unstaged. This is the
//     hk-aiw63 blocker: it persisted after hk-i1n7j (which only discarded the
//     ledger) and aborted every real merge-to-main where claude mutates settings.
//
// Discarding either is safe: both are reconstructed deterministically (the
// ledger from main, the settings from the next MaterializeClaudeSettings call)
// and neither carries implementer task work.
//
// The set of discardable paths is exactly IsHarmonikChurn. This preserves the
// hk-i1n7j safety property: a dirty file that is NOT recognized churn is left
// untouched, so an implementer that escaped its worktree (left genuine
// uncommitted work) still fails the rebase loudly rather than being silently
// reset. The rebase is now the ONLY thing that catches that case — the
// post-merge escape check that used to share this allowlist is deleted.
//
// Errors are non-fatal and best-effort: if `git status` or a `git checkout`
// fails, the function writes the failure to stderr, continues / returns, and
// the subsequent rebase reports the real failure. It is a no-op when no churn
// paths are dirty.
//
// Beads: hk-3yz2d (ledger), hk-aiw63 (generalized to .claude/settings.json and
// the full IsHarmonikChurn allowlist).
func DiscardDirtyChurn(ctx context.Context, wtPath string) {
	statusOut, statusErr := gitprobe.Output(ctx, wtPath, "status", "--porcelain")
	if statusErr != nil {
		// A failed status is not a clean worktree. Say so, or the churn is left
		// dirty and the rebase failure that follows names the wrong cause.
		fmt.Fprintf(os.Stderr, "daemon: DiscardDirtyChurn: git status --porcelain: %v\n", statusErr)
		return
	}
	if strings.TrimSpace(string(statusOut)) == "" {
		return
	}

	var churnPaths []string
	for _, line := range strings.Split(strings.TrimRight(string(statusOut), "\n"), "\n") {
		if len(line) < 4 {
			continue
		}
		xy := line[:2]
		if xy == "??" {
			continue
		}
		path := line[3:]
		if idx := strings.Index(path, " -> "); idx >= 0 {
			path = path[idx+4:]
		}
		path = strings.Trim(path, "\"")
		if IsHarmonikChurn(path) {
			churnPaths = append(churnPaths, path)
		}
	}
	if len(churnPaths) == 0 {
		return
	}

	for _, path := range churnPaths {
		// A checkout of a pathspec puts the committed content back. It means the
		// same thing every time, so a stopped child can run again.
		if out, err := gitprobe.CombinedOutput(ctx, wtPath, "checkout", "--", path); err != nil {
			fmt.Fprintf(os.Stderr, "daemon: DiscardDirtyChurn: git checkout -- %s: %v\n%s",
				path, err, out)
		}
	}
}

// residualDeltaCommitMessage is the message CommitResidualDelta commits. Named
// so the commit-message gate test reads what production writes — see
// stripRunContextCommitMessage for why that matters.
func residualDeltaCommitMessage(runID core.RunID) string {
	return fmt.Sprintf("chore: residual iteration delta [%s]\n\nTrivial: true", runID.String())
}

// CommitResidualDelta commits any UNCOMMITTED change that survives
// DiscardDirtyChurn onto the run-branch, immediately before the pre-merge
// `git rebase main`.
//
// Bug (hk-rljho class): a review-loop iteration can leave a TRACKED but
// UNCOMMITTED change in the run worktree (e.g. a staged deletion of a test file
// an iteration removed, whose deletion never got its own commit because the
// daemon's commit-detection had already fired). DiscardDirtyChurn deliberately
// restores only the IsHarmonikChurn allowlist and leaves genuine work untouched
// (hk-i1n7j safety property), so this real iteration delta survives to
// `git rebase main`, which aborts with "cannot rebase: You have unstaged
// changes" — failing the merge even though the bead's work is complete.
//
// Bug (hk-cmry defect #3): the implementer / a review-loop iteration can author
// genuinely NEW files (untracked, status "??") that were never committed — e.g.
// hk-8prq's GREEN added internal/keeper/sessionid.go and a new hook script as
// brand-new files. The original `git add -u` staged only TRACKED modifications
// and SILENTLY DROPPED those new files: the residual commit then carried only
// the tracked changes (often the RED test) and the new-file GREEN was lost when
// the worktree was later cleaned. That is how the daemon silently dropped a
// reviewed GREEN and broke main fleet-wide. The fix stages with `git add -A` so
// authored new files can NEVER be silently dropped.
//
// The fix PRESERVES hk-i1n7j: it does NOT discard the residual work. It COMMITS
// the delta onto the run-branch (it IS the bead's own work — a review-loop edit
// or new source file that never got committed) so the rebase proceeds with the
// work intact.
//
// On the original `git add -u` → `git add -A` concern: -A also stages untracked
// files, which the prior code feared would sweep "stray junk" to main+origin.
// That concern is now mitigated on two grounds: (1) `git add -A` HONORS
// .gitignore, and this repo's .gitignore excludes every class of daemon/runtime
// junk (.harmonik/, .beads/*, .env, build outputs, worktrees, *.test, etc.), so
// none of it is stageable; (2) DiscardDirtyChurn has already restored the
// IsHarmonikChurn allowlist, so any non-gitignored untracked file that SURVIVES
// to this point is the bead's authored work, not stray junk. Dropping it (the
// old behavior) is the actual bug; capturing it is correct.
//
// It is a no-op when no non-churn change remains after churn cleanup (so it
// never manufactures an empty commit).
//
// It returns an error, and the caller must stop the merge on it. This step is
// NOT best-effort like its two neighbours, because of what the first of its two
// callers does next. In prepareInitialMerge the step after this one is
// CleanUntrackedFiles, which runs `git clean -fd`: when this step fails to save
// an authored untracked file, that next step deletes exactly the file this one
// failed to save. The rebase then succeeds, the merge succeeds, and the work is
// gone with nothing red anywhere. A step whose neighbour destroys what it
// failed to save has to be able to fail the merge.
//
// The other caller, prepareRebase, goes straight to the rebase and deletes
// nothing. It stops on the error too, but for a smaller reason: a failed save
// leaves the worktree dirty, so the rebase fails anyway and names a conflict
// that is not the cause. Stopping here reports the cause instead.
//
// A failed `git status` counts the same way and is reported the same way. It
// reads here exactly like a clean worktree, so a worktree that cannot say what
// it holds must not be treated as one that holds nothing worth saving.
//
// Bead: review-loop residual-delta merge fix (hk-rljho class); untracked-capture
// fix (hk-cmry defect #3); the error return (hk-33u5r).
func CommitResidualDelta(ctx context.Context, wtPath string, runID core.RunID) error {
	statusOut, statusErr := gitprobe.Output(ctx, wtPath, "status", "--porcelain")
	if statusErr != nil {
		// A failed status reads exactly like a clean worktree here, and the step
		// after this one deletes untracked files. An unreadable worktree has to
		// stop the merge.
		return fmt.Errorf("git status --porcelain in %s: %w", wtPath, statusErr)
	}

	var residual bool
	for _, line := range strings.Split(strings.TrimRight(string(statusOut), "\n"), "\n") {
		if len(line) < 4 {
			continue
		}
		path := line[3:]
		if idx := strings.Index(path, " -> "); idx >= 0 {
			path = path[idx+4:]
		}
		path = strings.Trim(path, "\"")
		if IsHarmonikChurn(path) {
			continue // should already be restored by DiscardDirtyChurn; skip defensively
		}
		residual = true
		break
	}
	if !residual {
		return nil // no genuine residual delta — do not create an empty commit
	}

	// Staging sets the index to an exact state, so a stopped child can run again.
	out, err := gitprobe.CombinedOutput(ctx, wtPath, "add", "-A", "--",
		".",
		":(exclude).claude",
		":(exclude).harmonik",
	)
	if err != nil {
		return fmt.Errorf("git add -A in %s: %w\n%s", wtPath, err, strings.TrimRight(string(out), "\n"))
	}

	// A commit does not mean the same thing twice, so it does not go through the
	// shared retry. runCommitOnce reads HEAD around the one attempt and runs the
	// commit again only when HEAD did not move.
	commitOut, commitErr := runCommitOnce(ctx, wtPath, func() ([]byte, error) {
		//nolint:gosec // G204: fixed git binary; commit message is daemon-generated from a typed RunID and a constant template.
		commitCmd := exec.CommandContext(ctx, "git", "commit", "-m", residualDeltaCommitMessage(runID))
		commitCmd.Dir = wtPath
		return commitCmd.CombinedOutput()
	})
	if commitErr != nil {
		return fmt.Errorf("git commit in %s: %w\n%s", wtPath, commitErr, strings.TrimRight(string(commitOut), "\n"))
	}
	return nil
}

// CleanUntrackedFiles removes untracked non-gitignored files and directories
// from the run worktree using `git clean -fd`. Called after DiscardDirtyChurn
// and CommitResidualDelta as the final pre-rebase cleanup step so that
// integration-test artifacts (binaries built without an output path, temp
// objects, etc.) cannot abort the rebase.
//
// `git rebase` aborts when an untracked file in the working tree would be
// overwritten by a commit being replayed ("error: The following untracked
// working tree files would be overwritten by checkout"). `git clean -fd`
// removes those files — it honours .gitignore, so platform/build artifacts
// already covered by .gitignore are left untouched.
//
// Safety: at this point CommitResidualDelta has already committed any genuine
// authored untracked files (new source files the implementer added), so the
// only files that survive to this step are integration-test artifacts, not
// bead work. Gitignored files (*.test, /harmonik-twin-claude, /.harmonik/)
// are unaffected and do not interfere with the rebase.
//
// Non-fatal and best-effort: errors are logged to stderr; the subsequent
// rebase will surface the real dirty-state failure if cleaning did not fully
// succeed.
//
// Bead: hk-g9zz.
func CleanUntrackedFiles(ctx context.Context, wtPath string) {
	// A second clean removes whatever the first one did not, so a stopped child
	// can run again.
	if out, err := gitprobe.CombinedOutput(ctx, wtPath, "clean", "-fd"); err != nil {
		fmt.Fprintf(os.Stderr, "daemon: CleanUntrackedFiles: git clean -fd: %v\n%s", err, out)
	}
}

// IsHarmonikChurn reports whether a path is part of the expected harmonik
// churn surface that should be excluded from the escape check.
func IsHarmonikChurn(path string) bool {
	switch {
	case strings.HasPrefix(path, ".harmonik/"), path == ".harmonik":
		return true
	case strings.HasPrefix(path, ".claude/"), path == ".claude":
		return true
	case path == ".beads/issues.jsonl":
		return true
	case path == "AGENT_COMMS.md":
		return true
	}
	return false
}

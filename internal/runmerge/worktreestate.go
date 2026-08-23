package runmerge

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

func mergedCommitPaths(ctx context.Context, projectDir, mainTip, runTip string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "git", "diff", "--name-only", mainTip, runTip)
	cmd.Dir = projectDir
	out, err := cmd.Output()
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

	restoreCmd := exec.CommandContext(ctx, "git", "restore", "--source=HEAD", "--staged", "--worktree", "--pathspec-from-file=-")
	restoreCmd.Dir = projectDir
	restoreCmd.Stdin = strings.NewReader(strings.Join(paths, "\n") + "\n")
	if out, restoreErr := restoreCmd.CombinedOutput(); restoreErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: RunBranchToTarget: WARNING: scoped working-tree refresh failed (bead %s run %s): %v\n%s",
			beadID, runID.String(), restoreErr, out)
		emitWorkingTreeRefreshFailed(ctx, bus, runID, beadID, restoreErr)
	}
}

func locallyEditedPaths(ctx context.Context, projectDir, mainTip string, paths []string) []string {
	args := append([]string{"diff", "--name-only", mainTip, "--"}, paths...)
	cmd := exec.CommandContext(ctx, "git", args...) //nolint:gosec // G204: fixed git binary; args are a git SHA and repo-relative paths from git itself
	cmd.Dir = projectDir
	out, err := cmd.Output()
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
	cmd := exec.CommandContext(ctx, "git", args...) //nolint:gosec // G204: fixed git binary; args are a git SHA and repo-relative paths from git itself
	cmd.Dir = projectDir
	patch, err := cmd.Output()
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
// fails, the function continues / returns silently and the subsequent rebase
// reports the real failure. It is a no-op when no churn paths are dirty.
//
// Beads: hk-3yz2d (ledger), hk-aiw63 (generalized to .claude/settings.json and
// the full IsHarmonikChurn allowlist).
func DiscardDirtyChurn(ctx context.Context, wtPath string) {
	statusCmd := exec.CommandContext(ctx, "git", "status", "--porcelain")
	statusCmd.Dir = wtPath
	statusOut, statusErr := statusCmd.Output()
	if statusErr != nil || strings.TrimSpace(string(statusOut)) == "" {
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
		//nolint:gosec // G204: fixed git binary; path is parsed from git status, accepted only by IsHarmonikChurn, and follows `--`.
		checkoutCmd := exec.CommandContext(ctx, "git", "checkout", "--", path)
		checkoutCmd.Dir = wtPath
		if out, err := checkoutCmd.CombinedOutput(); err != nil {
			fmt.Fprintf(os.Stderr, "daemon: DiscardDirtyChurn: git checkout -- %s: %v\n%s",
				path, err, out)
		}
	}
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
// never manufactures an empty commit). Errors are best-effort/non-fatal in the
// same style as DiscardDirtyChurn: a failure leaves the residual delta in place
// and the subsequent rebase surfaces the real "unstaged changes" failure rather
// than masking it.
//
// Bead: review-loop residual-delta merge fix (hk-rljho class); untracked-capture
// fix (hk-cmry defect #3).
func CommitResidualDelta(ctx context.Context, wtPath string, runID core.RunID) {
	statusCmd := exec.CommandContext(ctx, "git", "status", "--porcelain")
	statusCmd.Dir = wtPath
	statusOut, statusErr := statusCmd.Output()
	if statusErr != nil {
		return
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
		return // no genuine residual delta — do not create an empty commit
	}

	addCmd := exec.CommandContext(ctx, "git", "add", "-A", "--",
		".",
		":(exclude).claude",
		":(exclude).harmonik",
	)
	addCmd.Dir = wtPath
	if out, err := addCmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "daemon: CommitResidualDelta: git add -A: %v\n%s", err, out)
		return
	}

	commitMsg := fmt.Sprintf(
		"chore: residual iteration delta [%s]\n\nTrivial: true",
		runID.String(),
	)
	//nolint:gosec // G204: fixed git binary; commit message is daemon-generated from a typed RunID and a constant template.
	commitCmd := exec.CommandContext(ctx, "git", "commit", "-m", commitMsg)
	commitCmd.Dir = wtPath
	if out, err := commitCmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "daemon: CommitResidualDelta: git commit: %v\n%s", err, out)
	}
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
	cleanCmd := exec.CommandContext(ctx, "git", "clean", "-fd")
	cleanCmd.Dir = wtPath
	if out, err := cleanCmd.CombinedOutput(); err != nil {
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

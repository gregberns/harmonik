package runmerge

import (
	"bytes"
	"context"
	"errors"
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

// recoveryDir is where a rescued copy of destroyed work goes. It is under
// projectDir and NEVER under the run worktree: the worktree is removed with the
// run for most harnesses, so a rescue written into it dies with the work it
// rescued.
// maxRecoveryArtifacts bounds the sequence writeRecoveryArtifact will try. A
// run reaches the pre-rebase cleanup once per rebase retry, so the real count is
// small; the cap is here so a caller in a retry loop cannot fill the disk, which
// on this daemon is its own outage — under the dispatch floor it stops
// dispatching silently.
const maxRecoveryArtifacts = 100

func recoveryDir(projectDir string) string {
	return filepath.Join(projectDir, ".harmonik", "recovery")
}

// writeRecoveryArtifact writes data to a NEW recovery file under the stem and
// returns its path.
//
// ONE FILE PER CALL, and that is the whole point. The churn revert reaches this
// more than once in a single run — prepareInitialMerge calls it, and so does
// prepareRebase on each push or non-fast-forward retry. (The untracked clean
// runs once, from prepareInitialMerge only.) Truncating would delete the rescue
// the previous call made. Appending would be worse, and it is what this
// function used to do: two `git diff` patches for the SAME path against the
// same index base do not apply concatenated. Plain `git apply` refuses the
// second hunk with "patch does not apply" and restores nothing; `git apply -3`,
// which the stderr warning recommends, is worse still — it leaves the file
// conflicted with markers in it. That is not a rare case. The churn paths
// re-dirty between the two calls by design — constant churn is the reason the
// allowlist exists — so any retried merge produced an artifact that recovered
// nothing while the event still advertised it as the rescue.
//
// The sequence suffix comes from O_EXCL rather than from a directory listing or
// a clock: the first free number wins, and a caller that loses the race simply
// takes the next one.
func writeRecoveryArtifact(projectDir, stem string, data []byte) (string, error) {
	dir := recoveryDir(projectDir)
	if err := os.MkdirAll(dir, 0o700); err != nil { //dirmode:allow tighter on purpose: .harmonik/recovery/ holds rescued uncommitted work, 0o700 (never widen to core.HarmonikDirMode)
		return "", fmt.Errorf("mkdir %s: %w", dir, err)
	}
	// 0o600, not 0o644. rescueUntrackedFiles does not filter by path, so
	// whatever the merge pathspec excluded is copied in here as-is. An agent
	// settings file holding a plaintext auth token is the case to design for:
	// whether any one such file reaches this depends on the machine's global
	// gitignore, so the mode has to be safe without knowing. Operator-readable
	// means readable by the owner.
	for seq := 1; seq <= maxRecoveryArtifacts; seq++ {
		dest := filepath.Join(dir, fmt.Sprintf("%s-%d.patch", stem, seq))
		//nolint:gosec // G304: the name is daemon-generated from a typed RunID and a constant stem.
		f, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("open %s: %w", dest, err)
		}
		// Close runs whether the write worked or not, and the write failure is
		// the one worth reporting when both fail.
		_, writeErr := f.Write(data)
		closeErr := f.Close()
		if writeErr != nil || closeErr != nil {
			// Remove the file. A failed write leaves it truncated under the
			// ordinary name, so an operator reading the directory would find a
			// patch that looks like a rescue and restores part of one, while the
			// event correctly reports no patch at all. A failed close may well
			// have got every byte down, and it goes too: nothing points at it,
			// and a patch that MIGHT be whole is not something to hand somebody
			// as though it were.
			if rmErr := os.Remove(dest); rmErr != nil {
				fmt.Fprintf(os.Stderr, "daemon: WARNING: could not remove the unusable recovery file %s: %v;"+
					" it is not a rescue and no event points at it\n", dest, rmErr)
			}
			if writeErr != nil {
				return "", fmt.Errorf("write %s: %w", dest, writeErr)
			}
			return "", fmt.Errorf("close %s: %w", dest, closeErr)
		}
		return dest, nil
	}
	return "", fmt.Errorf("recovery artifact %s: %d files already exist in %s", stem, maxRecoveryArtifacts, dir)
}

// unsavedNote names the files the rescue could not read, and says nothing at
// all when it read them all. A warning that always ends in "unsaved: none"
// trains a reader to stop at the patch path.
func unsavedNote(unsaved []string) string {
	if len(unsaved) == 0 {
		return ""
	}
	return "; NOT in that patch (could not be read): " + strings.Join(unsaved, ", ")
}

// recoveryPatchNote is what a stderr warning says about the patch. An empty
// path means the save failed, and a warning that ends in "recovery patch: "
// with nothing after it reads like a truncated line rather than a second
// failure.
func recoveryPatchNote(patchPath string) string {
	if patchPath == "" {
		return "none (it could not be written)"
	}
	return patchPath
}

// unstagedPaths reports which of paths carry an unstaged edit in dir.
//
// `git checkout -- <path>` restores from the INDEX, so the content a revert
// destroys is the UNSTAGED delta and nothing else. `git diff` without a commit
// argument is exactly that comparison. `git diff HEAD` would name paths whose
// content the revert keeps.
func unstagedPaths(ctx context.Context, dir string, paths []string) []string {
	args := append([]string{"diff", "--name-only", "--"}, paths...)
	out, err := gitprobe.Output(ctx, dir, args...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "daemon: DiscardDirtyChurn: WARNING: git diff --name-only: %v;"+
			" the revert that follows cannot say what it destroys\n", err)
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

// saveChurnEdits parks the uncommitted edits on the churn paths before the
// revert removes them, warns on stderr, and names the loss in an event.
//
// It is BEST-EFFORT throughout. Every failure here is written to stderr and
// then ignored: the revert has to run for the rebase to start, and a merge that
// failed because a rescue failed would be a new outage on a path that worked.
//
// It is a no-op when no churn path carries an unstaged edit, which is the
// ordinary case — a churn file that was rewritten to the same content, or
// changed only in the index, loses nothing to the revert.
//
// Bead: hk-nqvqr.
func saveChurnEdits(ctx context.Context, wtPath, projectDir string, runID core.RunID, bus handlercontract.EventEmitter, beadID core.BeadID, churnPaths []string) {
	edited := unstagedPaths(ctx, wtPath, churnPaths)
	if len(edited) == 0 {
		return
	}

	patchPath := ""
	// --binary, exactly as rescueUntrackedFiles does: without it a binary churn
	// file yields a "Binary files differ" stub that git apply cannot restore, and
	// the stub would still be saved and reported as a successful rescue.
	args := append([]string{"diff", "--binary", "--"}, edited...)
	patch, diffErr := gitprobe.Output(ctx, wtPath, args...)
	switch {
	case diffErr != nil:
		fmt.Fprintf(os.Stderr, "daemon: DiscardDirtyChurn: WARNING: git diff of %s: %v\n",
			strings.Join(edited, ", "), diffErr)
	case len(patch) == 0:
		// git named these paths as edited a moment ago and now diffs them as
		// empty. Reporting it as an error with a nil cause reads as a defect in
		// the rescue; say what actually happened instead.
		fmt.Fprintf(os.Stderr, "daemon: DiscardDirtyChurn: WARNING: git diff of %s produced an empty patch,"+
			" so the edits it reported cannot be saved\n", strings.Join(edited, ", "))
	default:
		saved, writeErr := writeRecoveryArtifact(projectDir, "churn-discard-"+runID.String(), patch)
		if writeErr != nil {
			fmt.Fprintf(os.Stderr, "daemon: DiscardDirtyChurn: WARNING: recovery patch: %v\n", writeErr)
		}
		patchPath = saved
	}

	// The event fires even when the patch could not be written. Naming the loss
	// matters more than the recovery file, and a discard that saved nothing is
	// the case an operator most needs to hear about.
	fmt.Fprintf(os.Stderr, "daemon: DiscardDirtyChurn: WARNING: discarding uncommitted edits to %d churn path(s)"+
		" in the run worktree (bead %s run %s): %s; recovery patch (base is the run worktree INDEX,"+
		" so apply it with `git apply -3`): %s\n",
		len(edited), beadID, runID.String(), strings.Join(edited, ", "), recoveryPatchNote(patchPath))
	emitRunWorktreeChurnEditsDiscarded(ctx, bus, runID, beadID, wtPath, edited, patchPath)
}

// rescueUntrackedFiles copies the untracked non-ignored files out of the run
// worktree before `git clean -fd` deletes them, warns on stderr, and names the
// loss in an event.
//
// It does NOT filter by path. By the time the clean runs, CommitResidualDelta
// has already committed every untracked non-ignored file its pathspec allowed
// it to stage, so the files that survive to here ARE the excluded ones by
// construction. Deriving that list from the exclusion pathspec would be a
// second copy of a rule that already lives in one place.
//
// One artifact, not a mirrored directory tree: `git diff --no-index` against
// /dev/null produces an add-this-file patch, so the whole rescue is one file a
// person applies with `git apply`, in the same idiom as every other recovery
// patch the merge writes.
//
// BEST-EFFORT throughout, for the same reason saveChurnEdits is: the clean has
// to run for the rebase to start.
//
// Bead: hk-4q6ah.
func rescueUntrackedFiles(ctx context.Context, wtPath, projectDir string, runID core.RunID, bus handlercontract.EventEmitter, beadID core.BeadID) {
	// ls-files --others --exclude-standard is the set `git clean -fd` deletes:
	// untracked, not ignored, listed one file at a time rather than collapsed
	// into directory entries, and NUL-separated so a path with a space or a
	// quote in it survives the read.
	out, err := gitprobe.Output(ctx, wtPath, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		fmt.Fprintf(os.Stderr, "daemon: CleanUntrackedFiles: WARNING: git ls-files --others: %v;"+
			" the clean that follows deletes untracked files with no record\n", err)
		return
	}
	var paths []string
	for _, p := range strings.Split(string(out), "\x00") {
		if p != "" {
			paths = append(paths, p)
		}
	}
	if len(paths) == 0 {
		return
	}

	var patch bytes.Buffer
	var unsaved []string
	for _, p := range paths {
		// `git diff --no-index` exits 1 when the two inputs DIFFER, which is
		// every file here — a real file never matches /dev/null. Read the
		// output, not the exit status.
		fileDiff, diffErr := gitprobe.Output(ctx, wtPath, "diff", "--no-index", "--binary", "--", os.DevNull, p)
		if len(fileDiff) == 0 {
			fmt.Fprintf(os.Stderr, "daemon: CleanUntrackedFiles: WARNING: could not read %s before deleting it: %v\n", p, diffErr)
			// The clean deletes it either way, so it stays in paths. It goes in
			// unsaved as well, or the event names it beside a non-empty patch
			// that does not hold it and reads as a save that happened.
			unsaved = append(unsaved, p)
			continue
		}
		patch.Write(fileDiff)
	}

	patchPath := ""
	if patch.Len() > 0 {
		saved, writeErr := writeRecoveryArtifact(projectDir, "untracked-rescue-"+runID.String(), patch.Bytes())
		if writeErr != nil {
			fmt.Fprintf(os.Stderr, "daemon: CleanUntrackedFiles: WARNING: recovery patch: %v\n", writeErr)
		}
		patchPath = saved
	}

	fmt.Fprintf(os.Stderr, "daemon: CleanUntrackedFiles: WARNING: deleting %d untracked file(s)"+
		" from the run worktree (bead %s run %s): %s; recovery patch (apply it with `git apply`): %s%s\n",
		len(paths), beadID, runID.String(), strings.Join(paths, ", "), recoveryPatchNote(patchPath),
		unsavedNote(unsaved))
	emitRunWorktreeUntrackedFilesRemoved(ctx, bus, runID, beadID, wtPath, paths, unsaved, patchPath)
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
//
//   - .claude/settings.json — the Claude hook-bridge settings. The daemon's
//     MaterializeClaudeSettings (CHB-001..005) merges the bridge hooks +
//     permissions.allow into the worktree copy on every launch, and the running
//     claude agent may further mutate it. When a deployment TRACKS the file, the
//     per-launch materialization leaves it modified-but-unstaged and the rebase
//     aborts. That is the hk-aiw63 blocker: it persisted after hk-i1n7j (which
//     only discarded the ledger) and aborted every real merge-to-main where
//     claude mutates settings.
//
//     This repo no longer tracks it — 8392c1feb untracked the file and the root
//     .gitignore now covers it — so on this repo the clause is inert. It stays
//     because it is load-bearing on any deployment that still tracks the file,
//     and NOTHING enforces which kind of deployment this is (hk-op1fd). Read the
//     clause as conditional, not as a claim about the current tree.
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
// The revert is no longer silent. Before any checkout runs, saveChurnEdits
// writes the unstaged delta of exactly these paths to a recovery patch under
// projectDir, warns on stderr, and emits an event naming them. The discard
// still happens — the rebase needs it — but the work it destroys is recoverable
// and announced, which is the policy EM-054 already sets for the main-root
// refresh. It does not make the edit reach the target branch: the merge still
// ships without it.
//
// Errors are non-fatal and best-effort: if `git status` or a `git checkout`
// fails, the function writes the failure to stderr, continues / returns, and
// the subsequent rebase reports the real failure. The save is best-effort in
// the same way and can never fail the merge. It is a no-op when no churn
// paths are dirty.
//
// Beads: hk-3yz2d (ledger), hk-aiw63 (generalized to .claude/settings.json and
// the full IsHarmonikChurn allowlist), hk-nqvqr (save before discard).
func DiscardDirtyChurn(ctx context.Context, wtPath, projectDir string, runID core.RunID, bus handlercontract.EventEmitter, beadID core.BeadID) {
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

	// Park the work before destroying it. Best-effort: it never stops the
	// revert, and the revert is what lets the rebase start.
	saveChurnEdits(ctx, wtPath, projectDir, runID, bus, beadID, churnPaths)

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
// Safety: at this point CommitResidualDelta has already committed the genuine
// authored untracked files its pathspec allowed it to stage. What it excludes
// — .claude/ and .harmonik/, excluded because a .claude/ file can hold a
// credential (hk-igq3) — survives to this step and used to be deleted with no
// record at all. rescueUntrackedFiles now copies every untracked non-ignored
// file into one recovery patch under projectDir first, warns on stderr, and
// emits an event naming the files. The clean still happens; the delete stops
// being silent. Gitignored files (*.test, /harmonik-twin-claude, /.harmonik/)
// are neither rescued nor deleted — `git clean` and `git ls-files --others
// --exclude-standard` read the same exclude rules.
//
// Non-fatal and best-effort: errors are logged to stderr; the subsequent
// rebase will surface the real dirty-state failure if cleaning did not fully
// succeed. The rescue is best-effort in the same way and can never fail the
// merge.
//
// Beads: hk-g9zz, hk-4q6ah (rescue before clean).
func CleanUntrackedFiles(ctx context.Context, wtPath, projectDir string, runID core.RunID, bus handlercontract.EventEmitter, beadID core.BeadID) {
	// Copy the files out before deleting them. Best-effort: it never stops the
	// clean, and the clean is what lets the rebase start.
	rescueUntrackedFiles(ctx, wtPath, projectDir, runID, bus, beadID)

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

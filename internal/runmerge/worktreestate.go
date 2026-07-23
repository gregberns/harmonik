package runmerge

// worktreestate.go — worktree hygiene around the merge: pre-rebase churn
// discard / residual-delta capture / untracked cleanup, the post-merge scoped
// working-tree refresh, and the implementer-escape detector.
//
// Carved out of internal/daemon/workloop.go by P2 unit E5 RT13 (pure move).

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// mergedCommitPaths returns the repo-relative paths the merged commit changed
// between mainTip and runTip — the exact set EM-054's refresh is responsible
// for, and the same set the BL-MRG-004/005 bead-ledger reconciliation checks.
//
// Bead: hk-7qmpp.
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

// refreshMergedPaths re-syncs index + working tree for exactly the merged
// commit's own paths, and names any uncommitted local edits it overwrites.
//
// A merged path may itself carry an uncommitted local edit. The merged commit
// is authoritative for its own paths, so the refresh proceeds — but it MUST NOT
// be silent: the edits are written to a recovery patch first ("park it before
// you delete it") and the paths are named in a
// working_tree_local_edits_overwritten event.
//
// Local edits are detected against mainTip, NOT against HEAD: the ref has
// already advanced (Phase A), so every merged path reads as modified relative
// to HEAD whether or not anyone touched it. Diffing against the pre-merge tip
// separates a real local edit from that phantom staleness.
//
// All steps are best-effort / non-fatal — the merge is already durable.
//
// Spec ref: specs/execution-model.md §4.12 EM-054. Bead: hk-7qmpp, hk-4goy3.
func refreshMergedPaths(ctx context.Context, projectDir string, runID core.RunID, bus handlercontract.EventEmitter, beadID core.BeadID, mainTip string, paths []string) {
	overwritten := locallyEditedPaths(ctx, projectDir, mainTip, paths)
	if len(overwritten) > 0 {
		patchPath := writeRecoveryPatch(ctx, projectDir, runID, mainTip, overwritten)
		fmt.Fprintf(os.Stderr, "daemon: RunBranchToTarget: WARNING: post-merge refresh overwrote %d uncommitted local edit(s) in main (bead %s run %s): %s; recovery patch: %s\n",
			len(overwritten), beadID, runID.String(), strings.Join(overwritten, ", "), patchPath)
		emitWorkingTreeLocalEditsOverwritten(ctx, bus, runID, beadID, projectDir, overwritten, patchPath)
	}

	// `git restore --source=HEAD --staged --worktree` re-syncs index AND working
	// tree for the given pathspec, and correctly REMOVES paths the merged commit
	// deleted. Pathspecs arrive on stdin so a large merge cannot overflow ARG_MAX.
	restoreCmd := exec.CommandContext(ctx, "git", "restore", "--source=HEAD", "--staged", "--worktree", "--pathspec-from-file=-")
	restoreCmd.Dir = projectDir
	restoreCmd.Stdin = strings.NewReader(strings.Join(paths, "\n") + "\n")
	if out, restoreErr := restoreCmd.CombinedOutput(); restoreErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: RunBranchToTarget: WARNING: scoped working-tree refresh failed (bead %s run %s): %v\n%s",
			beadID, runID.String(), restoreErr, out)
		emitWorkingTreeRefreshFailed(ctx, bus, runID, beadID, restoreErr)
	}
}

// locallyEditedPaths returns the subset of paths whose working-tree content
// deviates from mainTip — i.e. genuine uncommitted local edits, as opposed to
// the phantom staleness every merged path shows against the already-advanced
// HEAD. Returns nil on error: the refresh must not be blocked by a failed
// diagnostic.
//
// Bead: hk-7qmpp.
func locallyEditedPaths(ctx context.Context, projectDir, mainTip string, paths []string) []string {
	args := append([]string{"diff", "--name-only", mainTip, "--"}, paths...)
	cmd := exec.CommandContext(ctx, "git", args...) //nolint:gosec // G204: fixed git binary; args are a git SHA and repo-relative paths from git itself
	cmd.Dir = projectDir
	out, err := cmd.Output()
	if err != nil {
		// Fail open by contract — but say so. This is the only signal that the
		// local-edits diagnostic ran and produced nothing because it FAILED,
		// rather than because there were genuinely no local edits to report.
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

// writeRecoveryPatch saves the about-to-be-overwritten local edits as a patch
// under .harmonik/recovery/ and returns its path, or "" if it could not be
// written. Untracked and outside the merged paths, so neither the refresh nor a
// future one can eat it.
//
// Bead: hk-7qmpp.
func writeRecoveryPatch(ctx context.Context, projectDir string, runID core.RunID, mainTip string, paths []string) string {
	dir := filepath.Join(projectDir, ".harmonik", "recovery")
	// 0o700, deliberately TIGHTER than core.HarmonikDirMode: this directory holds
	// rescued uncommitted work, so it is owner-only by intent. The constant's own
	// doc sanctions this — it is the default for the .harmonik tree, not a ceiling.
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
// The set of discardable paths is exactly IsHarmonikChurn — the same allowlist
// the post-merge escape check (CheckMainWorkingTreeDirty) uses to classify
// expected churn. This preserves the hk-i1n7j safety property: a dirty file that
// is NOT recognized churn is left untouched, so an implementer that escaped its
// worktree (left genuine uncommitted work) still fails the rebase loudly rather
// than being silently reset.
//
// Errors are non-fatal and best-effort: if `git status` or a `git checkout`
// fails, the function continues / returns silently and the subsequent rebase
// reports the real failure. It is a no-op when no churn paths are dirty.
//
// Beads: hk-3yz2d (ledger), hk-aiw63 (generalized to .claude/settings.json and
// the full IsHarmonikChurn allowlist).
func DiscardDirtyChurn(ctx context.Context, wtPath string) {
	// Enumerate ALL dirty paths in the worktree once, then discard only those
	// the churn allowlist recognizes. Untracked files (status "??") are not
	// git-checkout-restorable and are excluded by tracked-status filtering below.
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
		// Porcelain v1: "XY <path>". Untracked is "?? <path>" — skip (cannot be
		// `git checkout`-restored).
		xy := line[:2]
		if xy == "??" {
			continue
		}
		path := line[3:]
		// Handle rename "old -> new": restore the destination path.
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

	// Restore each churn path to its committed version. Use one checkout per
	// path so a failure on one (e.g. a path that is staged-only) does not block
	// the others; mirrors hk-i1n7j's best-effort/non-fatal style.
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
	// Enumerate dirty paths once. We commit only if a non-churn change survives
	// churn cleanup. Untracked files ("??") ARE counted now: an untracked file
	// surviving DiscardDirtyChurn is the bead's authored work (a new source
	// file), and `git add -A` will stage it. Gitignored paths never appear in
	// `git status --porcelain`, so they are excluded here and by `git add -A`.
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
		// Porcelain v1: "XY <path>". Untracked is "?? <path>" — a NEW authored
		// file that must be captured, NOT skipped (hk-cmry defect #3).
		path := line[3:]
		// Handle rename "old -> new": classify on the destination path.
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

	// Stage all residual work — tracked modifications/deletions AND untracked
	// NEW files. `git add -A` HONORS .gitignore (so daemon/runtime/build junk
	// stays excluded), and DiscardDirtyChurn already restored the churn
	// allowlist, so -A captures exactly the bead's genuine residual iteration
	// delta, including any newly authored source files that would otherwise be
	// silently dropped (hk-cmry defect #3).
	//
	// Hardening (hk-igq3): use explicit pathspec EXCLUSIONS for .claude/ and
	// .harmonik/ so that neither is ever swept into a commit even when a
	// legitimate non-churn change is present in the same worktree.
	// .claude/ is only partially gitignored (worktrees/ + scheduled_tasks.lock),
	// so a blanket -A could stage and push credential-adjacent files to origin.
	// .harmonik/ contains daemon runtime state (review.json, run-context, etc.)
	// that MUST NOT land on the merge target; gitignore covers it but the
	// explicit exclusion is belt-and-suspenders (GH #7, hk-znou: review.json
	// committed via -A caused add/add rebase conflicts on concurrent runs).
	// The :(exclude) pathspec magic is honored by git ≥ 2.0 and matches the
	// directory and all descendants.
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

	// Subject ≤72 chars with CC type + runID for traceability.
	// Trivial: true bypasses the Reviewed-By/Review-Verdict trailer requirement
	// for this machine-generated commit (commit-msg gate; build-practices.md).
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

// SnapshotUntrackedFiles (hk-ooexj) captures the set of paths the main repo's
// working tree reports as dirty/untracked at run-start, BEFORE the implementer
// launches. The returned set is fed to CheckMainWorkingTreeDirty after the run
// so that files which already existed (and which the implementer never touched)
// are NOT mistaken for an escape.
//
// It uses the same `git status --porcelain` surface as the escape check (which
// already excludes gitignored paths by default), so a pre-existing
// untracked-but-not-ignored file (e.g. a scratch note in the project root) is
// baselined and excluded, while a NET-NEW file the implementer writes outside
// its worktree still surfaces as an escape.
//
// Errors (e.g. git not in PATH) return (nil, err); the caller treats a failed
// snapshot as "no baseline" — the escape check then degrades to its prior
// behaviour rather than silently suppressing genuine escapes.
func SnapshotUntrackedFiles(ctx context.Context, mainPath string) (map[string]struct{}, error) {
	if mainPath == "" {
		return nil, fmt.Errorf("SnapshotUntrackedFiles: empty mainPath")
	}
	cmd := exec.CommandContext(ctx, "git", "-C", mainPath, "status", "--porcelain")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("SnapshotUntrackedFiles: git status: %w", err)
	}
	baseline := make(map[string]struct{})
	for _, path := range ParsePorcelainPaths(string(out)) {
		baseline[path] = struct{}{}
	}
	return baseline, nil
}

// ParsePorcelainPaths extracts the destination path from each line of
// `git status --porcelain` output, stripping the XY status prefix, resolving
// rename "old -> new" to the destination, and unquoting special-char paths.
func ParsePorcelainPaths(out string) []string {
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	paths := make([]string, 0, len(lines))
	for _, line := range lines {
		if line == "" {
			continue
		}
		// Porcelain v1 format: "XY <path>" (rename: "XY <oldpath> -> <newpath>").
		// The first three runes are the XY status and the separating space.
		if len(line) < 4 {
			continue
		}
		path := line[3:]
		// Handle rename "old -> new": consider the destination path.
		if idx := strings.Index(path, " -> "); idx >= 0 {
			path = path[idx+4:]
		}
		// Strip surrounding quotes (git quotes paths with special chars).
		path = strings.Trim(path, "\"")
		paths = append(paths, path)
	}
	return paths
}

// CheckMainWorkingTreeDirty (hk-6zylj) reports whether the main repo's working
// tree contains dirty files outside the harmonik churn allowlist that did NOT
// exist before the run started.
//
// It runs `git -C <mainPath> status --porcelain` and filters the output:
//   - `.harmonik/...`        — daemon state (expected churn)
//   - `.claude/...`          — orchestrator/Claude state (expected churn)
//   - `.beads/issues.jsonl`  — bead ledger (expected churn from br sync)
//   - `AGENT_COMMS.md`       — orchestrator scratch (expected churn, hk-77q8e)
//   - paths in `baseline`    — pre-existing untracked files (hk-ooexj)
//   - gitignored paths       — never the implementer's escape (hk-ooexj)
//
// `git status --porcelain` already omits gitignored paths by default; the
// explicit check-ignore pass is defense-in-depth against a parent-repo
// `.gitignore` or core.excludesFile that surfaces an ignored path here.
//
// The caller (runAgentImplementer) holds mergeMu across this call (hk-zguy6),
// so no sibling merge can be mid-flight (between update-ref and reset-hard)
// when we inspect the working tree. No path-exclusion heuristic is needed for
// sibling-merge races — the lock provides the full guarantee (hk-xux36).
//
// Anything else dirty is treated as an escape. The returned list contains the
// destination path of each surviving porcelain status line.
//
// Errors (e.g. git not in PATH) return (false, nil, err) so the caller can
// treat the check as informational and skip without failing the run.
func CheckMainWorkingTreeDirty(ctx context.Context, mainPath string, baseline map[string]struct{}) (dirty bool, dirtyPaths []string, err error) {
	if mainPath == "" {
		return false, nil, fmt.Errorf("CheckMainWorkingTreeDirty: empty mainPath")
	}
	cmd := exec.CommandContext(ctx, "git", "-C", mainPath, "status", "--porcelain")
	out, statusErr := cmd.Output()
	if statusErr != nil {
		return false, nil, fmt.Errorf("CheckMainWorkingTreeDirty: git status: %w", statusErr)
	}

	reported := ParsePorcelainPaths(string(out))
	candidates := make([]string, 0, len(reported))
	for _, path := range reported {
		if IsHarmonikChurn(path) {
			continue
		}
		// hk-ooexj: pre-existing untracked file — present at run-start, so the
		// implementer did not create it. Not an escape.
		if _, preexisting := baseline[path]; preexisting {
			continue
		}
		candidates = append(candidates, path)
	}
	// hk-ooexj: drop any gitignored paths (defense-in-depth — git status already
	// omits these by default, but a parent gitignore could surface them).
	kept := filterIgnoredPaths(ctx, mainPath, candidates)
	return len(kept) > 0, kept, nil
}

// filterIgnoredPaths returns paths minus those git considers ignored under
// mainPath. It batches the paths through a single `git check-ignore` call
// (NUL-delimited via --stdin -z). On any real error it returns paths unchanged
// — failing open keeps genuine escapes visible rather than swallowing them.
func filterIgnoredPaths(ctx context.Context, mainPath string, paths []string) []string {
	if len(paths) == 0 {
		return paths
	}
	cmd := exec.CommandContext(ctx, "git", "-C", mainPath, "check-ignore", "--stdin", "-z")
	cmd.Stdin = strings.NewReader(strings.Join(paths, "\x00"))
	out, err := cmd.Output()
	// check-ignore exits 0 when ≥1 path is ignored, 1 when none are ignored
	// (not an error for us), and ≥128 on a real failure. Treat exit 1 (no
	// matches) as "nothing ignored"; treat other non-zero codes as fail-open.
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return paths // none ignored
		}
		return paths // fail open: keep all candidates visible
	}
	ignored := make(map[string]struct{})
	for _, p := range strings.Split(strings.TrimRight(string(out), "\x00"), "\x00") {
		if p != "" {
			ignored[p] = struct{}{}
		}
	}
	kept := make([]string, 0, len(paths))
	for _, p := range paths {
		if _, isIgnored := ignored[p]; isIgnored {
			continue
		}
		kept = append(kept, p)
	}
	return kept
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
	// hk-77q8e: AGENT_COMMS.md was the v0 file-outbox comms channel (retired by
	// hk-8sm4f — use `harmonik comms send/recv` instead). The exemption is kept
	// for the live-transition period: any session still tailing the old file must
	// not cause a false implementer_escape on in-flight beads.
	case path == "AGENT_COMMS.md":
		return true
	}
	return false
}

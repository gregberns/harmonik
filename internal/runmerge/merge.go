package runmerge

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/gitprobe"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/workspace"
)

// IsRetryableReason returns true when a RunBranchToTarget failure is a
// race-condition artifact worth retrying at the workloop level WITHOUT
// re-running the full implementer+reviewer cycle. The APPROVE verdict is
// preserved across retries.
//
// Retryable (transient race): rebase_conflict, non_ff_merge, merge_fmt_failed,
// and a merge_build_failed whose compiler output is the vanished-build-cache
// signature (hk-pgtbr — see below).
// Non-retryable (structural): every other merge_build_failed, push_failed,
// strip_run_context_failed, etc.
//
// hk-pgtbr: a merge_build_failed whose errors are `could not import <stdlib
// package> (open <gocache path>: no such file or directory)` is not a statement
// about the bead's code at all — it is another process having deleted the shared
// Go build cache mid-compile. Charging it to the bead records an infrastructure
// fault as a code regression, which is indistinguishable from a real one in the
// event log. Classifying it retryable re-prepares and rebuilds instead.
// Bounded: the caller caps attempts (RunConfig.MaxMergeAttempts), so a
// misclassification costs extra builds, never an unbounded loop.
//
// Beads: hk-f9xzs, hk-pgtbr.
func IsRetryableReason(reason string) bool {
	for _, prefix := range []string{"rebase_conflict", "non_ff_merge", "merge_fmt_failed"} {
		if strings.HasPrefix(reason, prefix) {
			return true
		}
	}
	if strings.HasPrefix(reason, "merge_build_failed") && isMergeBuildColdCacheError([]byte(reason)) {
		return true
	}
	return false
}

// Outcome carries the result of RunBranchToTarget so the caller can
// decide which terminal event sequence to emit.
type Outcome struct {
	// Success is true when the merge-and-push completed without error.
	Success bool
	// Reason is the failure reason for emit/logging when Success is false.
	Reason string
	// NoChange is true when the run-branch has no commits beyond its merge-base
	// with main, i.e. the agent made no commits. The caller proceeds to CloseBead
	// normally (no merge required).
	NoChange bool
}

// Submit runs a critical section inside the merge exclusion domain
// (mergeq.Queue.Submit) or, when no queue is wired (unit tests that drive a
// single beadRunOne directly), inline under the caller's context. It mirrors
// mergeq.Queue.Submit's signature so the two are interchangeable.
type Submit func(ctx context.Context, label string, critical func(context.Context) error) error

// InlineSubmit runs critical directly under ctx — the nil-queue fallback.
func InlineSubmit(ctx context.Context, _ string, critical func(context.Context) error) error {
	return critical(ctx)
}

type mergePrepareKind int

const (
	mergePrepareFirst mergePrepareKind = iota
	mergePrepareNonFFRetry
	mergePreparePushRetry
)

type commitOutcome struct {
	done       *Outcome
	retryKind  mergePrepareKind
	newMainTip string
}

type commitAdvanceResult struct {
	done         *Outcome
	retry        bool
	newMainTip   string
	advanced     bool
	priorMainTip string
}

// RunBranchToTarget implements the §4.12.EM-052 ordered merge sequence,
// split (RSM-012..016, merge-queue-design §2) into a speculative prepare phase
// that runs OUTSIDE the merge exclusion domain (rebase, strip, go build/vet, the
// gofumpt/gci auto-fix) and a commit phase whose ref-mutations run INSIDE the
// domain via `submit` (the fresh FF re-validation + git update-ref in Phase A;
// the working-tree reset + conditional br sync in Phase C; the CAS-rollback +
// fetch + re-base-to-origin in Phase D). The network `git push origin <target>`
// runs OUTSIDE the domain (Phase B) per the F4 relocation (RSM-019 / M4-C5): the
// exclusive section serializes local ref + working-tree mutation, not the
// publication to origin. No build-class command (go build/vet, gofumpt, gci, git
// rebase) runs inside the domain (RSM-017 / RSM-INV-005); the retry loop's
// re-rebase re-prepares OUTSIDE it.
//
// The retry budget (maxPushAttempts = 3) and every Outcome.reason string
// are preserved from the pre-split single-function form.
//
// Steps:
//  1. Resolve run-branch tip; no-change short-circuits.
//  2. Rebase run-branch onto the target (prepare; rebase_conflict → EM-053).
//  3. Fast-forward re-validation against a freshly read target tip (commit).
//  4. git update-ref refs/heads/<target> <tip> (Phase A, inside the domain).
//  5. git push origin <target>, OUTSIDE the domain (Phase B), with a CAS-rollback
//     + re-prepare on a non-FF rejection (Phase D, inside the domain).
//  6. git restore --staged . + git reset --hard HEAD (Phase C, inside; EM-054).
//  7. br sync --import-only when .beads/issues.jsonl is in the diff (Phase C).
//
// Spec ref: specs/run-state-machine.md RSM-012..019; specs/execution-model.md
// §4.12 EM-052/EM-053/EM-054. Bead: hk-ftyvo, hk-4goy3, hk-6r6xv, hk-zgt4u,
// hk-yyso7 (mergeMu → mergeq).
//
//nolint:gocognit,cyclop // slated for giant-retirement refactor (TRACK 3); do not split here
func RunBranchToTarget(ctx context.Context, submit Submit, projectDir string, runID core.RunID, bus handlercontract.EventEmitter, beadID core.BeadID, headSHA, targetBranch string, protectBranches []string, brPath string) Outcome {
	if submit == nil {
		submit = InlineSubmit
	}

	runBranch := workspace.TaskBranchName(runID.String())

	runTip, mainTip, done := resolveMergeTips(ctx, projectDir, runBranch, targetBranch, headSHA, protectBranches)
	if done != nil {
		return *done
	}

	wtPath := workspace.WorktreePath(projectDir, runID.String(), workspace.NoWorktreeRootOverride())

	if _, statErr := os.Stat(wtPath); statErr != nil {
		addWtCmd := exec.CommandContext(ctx, "git", "worktree", "add", wtPath, runBranch)
		addWtCmd.Dir = projectDir
		if _, addErr := addWtCmd.CombinedOutput(); addErr == nil {
			cleanupCtx := context.WithoutCancel(ctx)
			defer func() {
				if cleanupErr := RemoveWorktree(cleanupCtx, projectDir, wtPath); cleanupErr != nil {
					fmt.Fprintf(os.Stderr, "daemon: runmerge: temporary worktree reclaim failed: %v\n", cleanupErr)
				}
			}()
		}
	}

	if prepOut := prepareInitialMerge(ctx, wtPath, projectDir, runID, runBranch, targetBranch, &runTip, &mainTip); prepOut != nil {
		return *prepOut
	}

	const maxPushAttempts = 3
	for pushAttempt := 1; pushAttempt <= maxPushAttempts; pushAttempt++ {
		if buildOut := runMergeBuildGate(ctx, wtPath, projectDir, runID, beadID, bus); buildOut != nil {
			return *buildOut
		}
		if fmtOut, fmtRunTip := runMergeFmtGate(ctx, wtPath, projectDir, runID, beadID, bus); fmtOut != nil {
			return *fmtOut
		} else if fmtRunTip != "" {
			runTip = fmtRunTip
		}

		var adv commitAdvanceResult
		if submitErr := submit(ctx, "commit-merge", func(qctx context.Context) error {
			adv = commitAdvanceRef(qctx, projectDir, runTip, targetBranch, pushAttempt, maxPushAttempts)
			return nil
		}); submitErr != nil {
			return Outcome{
				Success: false,
				Reason:  fmt.Sprintf("merge_queue_submit_failed: %v", submitErr),
			}
		}
		if adv.done != nil {
			return *adv.done
		}
		if adv.retry {
			mainTip = adv.newMainTip
			if prepOut := prepareRebase(ctx, wtPath, projectDir, runID, runBranch, targetBranch, &runTip, mainTip, mergePrepareNonFFRetry, pushAttempt); prepOut != nil {
				return *prepOut
			}
			continue
		}

		pushOut, pushErr := gitPushOrigin(ctx, projectDir, targetBranch)
		if pushErr == nil {
			emitWorkspaceMergeStatusMerged(ctx, bus, runID, runBranch, targetBranch, runTip)

			if submitErr := submit(ctx, "commit-merge", func(qctx context.Context) error {
				commitFinalizeWorkingTree(qctx, projectDir, runID, bus, beadID, adv.priorMainTip, runTip, brPath)
				return nil
			}); submitErr != nil {
				fmt.Fprintf(os.Stderr, "daemon: RunBranchToTarget: WARNING: post-push finalize submit failed (bead %s run %s): %v\n",
					beadID, runID.String(), submitErr)
			}
			return Outcome{Success: true}
		}

		var co commitOutcome
		if submitErr := submit(ctx, "commit-merge", func(qctx context.Context) error {
			co = commitHandlePushFailure(qctx, projectDir, targetBranch, adv.priorMainTip, runTip, pushOut, pushErr, pushAttempt, maxPushAttempts)
			return nil
		}); submitErr != nil {
			return Outcome{
				Success: false,
				Reason:  fmt.Sprintf("merge_queue_submit_failed: %v", submitErr),
			}
		}
		if co.done != nil {
			return *co.done
		}

		mainTip = co.newMainTip
		if prepOut := prepareRebase(ctx, wtPath, projectDir, runID, runBranch, targetBranch, &runTip, mainTip, co.retryKind, pushAttempt); prepOut != nil {
			return *prepOut
		}
	}

	return Outcome{Success: false, Reason: "non_ff_merge: retry budget exhausted"}
}

func resolveMergeTips(ctx context.Context, projectDir, runBranch, targetBranch, headSHA string, protectBranches []string) (runTip, mainTip string, done *Outcome) {
	if targetBranch == "" {
		return "", "", &Outcome{Success: false, Reason: "merge_target_empty: targetBranch must not be empty"}
	}
	for _, protected := range protectBranches {
		if targetBranch == protected {
			return "", "", &Outcome{
				Success: false,
				Reason:  fmt.Sprintf("merge_target_protected: %q is in ProtectBranches", targetBranch),
			}
		}
	}

	rt, rtErr := gitprobe.RevParse(ctx, projectDir, "refs/heads/"+runBranch)
	if rtErr != nil {
		return "", "", &Outcome{
			Success: false,
			Reason: fmt.Sprintf(
				"merge_run_branch_missing: %s does not resolve in %s; the run's work cannot be merged"+
					" and may be stranded — salvage it from the run worktree: %v",
				runBranch, projectDir, rtErr),
		}
	}

	mt, mtErr := gitprobe.RevParse(ctx, projectDir, "refs/heads/"+targetBranch)
	if mtErr != nil {
		return "", "", &Outcome{Success: false, Reason: fmt.Sprintf("git rev-parse %s: %v", targetBranch, mtErr)}
	}
	if mt == rt {
		return "", "", &Outcome{NoChange: true}
	}

	if headSHA != "" && rt == headSHA {
		return "", "", &Outcome{NoChange: true}
	}
	return rt, mt, nil
}

func prepareInitialMerge(ctx context.Context, wtPath, projectDir string, runID core.RunID, runBranch, targetBranch string, runTip, mainTip *string) *Outcome {
	if _, statErr := os.Stat(wtPath); statErr == nil {
		DiscardDirtyChurn(ctx, wtPath)
		CommitResidualDelta(ctx, wtPath, runID)
		CleanUntrackedFiles(ctx, wtPath)

		rebaseCmd := exec.CommandContext(ctx, "git", "rebase", targetBranch)
		rebaseCmd.Dir = wtPath
		if out, rebaseErr := rebaseCmd.CombinedOutput(); rebaseErr != nil {
			gitRebaseAbort(ctx, wtPath)
			return &Outcome{
				Success: false,
				Reason:  fmt.Sprintf("rebase_conflict: %v\n%s", rebaseErr, strings.TrimRight(string(out), "\n")),
			}
		}
		if t, rerr := gitprobe.RevParse(ctx, projectDir, "refs/heads/"+runBranch); rerr == nil {
			*runTip = t
		}
		if t, rerr := gitprobe.RevParse(ctx, projectDir, "refs/heads/"+targetBranch); rerr == nil {
			*mainTip = t
		}

		if *runTip == *mainTip {
			return &Outcome{
				Success: false,
				Reason: fmt.Sprintf(
					"rebase_dropped_commits: rebase of %s onto %s produced no commits"+
						" ahead of target; reviewed work silently dropped — salvage from run-branch",
					runBranch, targetBranch),
			}
		}
	}

	stripped, stripErr := StripRunContextFromMerge(ctx, wtPath)
	if stripErr != nil {
		return &Outcome{
			Success: false,
			Reason:  fmt.Sprintf("strip_run_context_failed: %v", stripErr),
		}
	}
	if stripped {
		if newTip, resolveErr := gitprobe.ResolveWorktreeHEAD(ctx, wtPath); resolveErr == nil {
			*runTip = newTip
		}
	}
	return nil
}

func gitRebaseAbort(ctx context.Context, wtPath string) {
	abortCmd := exec.CommandContext(ctx, "git", "rebase", "--abort")
	abortCmd.Dir = wtPath
	if out, err := abortCmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "daemon: RunBranchToTarget: git rebase --abort failed in %s: %v\n%s", wtPath, err, out)
	}
}

func runMergeBuildGate(ctx context.Context, wtPath, projectDir string, runID core.RunID, beadID core.BeadID, bus handlercontract.EventEmitter) *Outcome {
	buildDir := projectDir
	if _, statErr := os.Stat(wtPath); statErr == nil {
		buildDir = wtPath
	}
	if _, goModErr := os.Stat(filepath.Join(buildDir, "go.mod")); goModErr != nil {
		return nil
	}
	for _, buildArgs := range [][]string{
		{"build", "./..."},
		{"vet", "./..."},
	} {
		out, buildErr := runMergeBuildStep(ctx, execGoInDir, buildDir, buildArgs, mergeBuildColdCacheBackoff)
		if buildErr == nil {
			continue
		}
		emitMergeBuildFailed(ctx, bus, runID, beadID, buildErr, out)
		return &Outcome{
			Success: false,
			Reason:  fmt.Sprintf("merge_build_failed (go %s): %v\n%s", buildArgs[0], buildErr, strings.TrimRight(string(out), "\n")),
		}
	}
	return nil
}

type mergeBuildRunner func(ctx context.Context, dir string, args []string) ([]byte, error)

func execGoInDir(ctx context.Context, dir string, args []string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = dir
	return cmd.CombinedOutput()
}

var mergeBuildColdCacheBackoff = []time.Duration{3 * time.Second, 9 * time.Second}

func runMergeBuildStep(ctx context.Context, run mergeBuildRunner, dir string, args []string, backoff []time.Duration) ([]byte, error) {
	out, err := run(ctx, dir, args)
	for _, wait := range backoff {
		if err == nil || !isMergeBuildColdCacheError(out) {
			return out, err
		}
		if wait > 0 {
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				timer.Stop()
				return out, err
			case <-timer.C:
			}
		}
		out, err = run(ctx, dir, args)
	}
	return out, err
}

func commitAdvanceRef(ctx context.Context, projectDir, runTip, targetBranch string, pushAttempt, maxPushAttempts int) commitAdvanceResult {
	freshMainCmd := exec.CommandContext(ctx, "git", "rev-parse", "refs/heads/"+targetBranch) //nolint:gosec // G204: fixed git/go binary with controlled args (config target branch, git SHAs, module path) — not user input
	freshMainCmd.Dir = projectDir
	freshMainOut, freshMainErr := freshMainCmd.Output()
	if freshMainErr != nil {
		return commitAdvanceResult{done: &Outcome{
			Success: false,
			Reason:  fmt.Sprintf("non_ff_merge_retry_rev_parse (attempt %d): %v", pushAttempt, freshMainErr),
		}}
	}
	mainTip := strings.TrimRight(string(freshMainOut), "\n")

	isAncestor, ancestryErr := gitprobe.IsAncestor(ctx, projectDir, mainTip, runTip)
	if ancestryErr != nil {
		return commitAdvanceResult{done: &Outcome{
			Success: false,
			Reason:  fmt.Sprintf("non_ff_merge_ancestry_check: %v", ancestryErr),
		}}
	}
	if !isAncestor {
		if pushAttempt >= maxPushAttempts {
			return commitAdvanceResult{done: &Outcome{
				Success: false,
				Reason:  fmt.Sprintf("non_ff_merge: %s advanced concurrently", targetBranch),
			}}
		}
		return commitAdvanceResult{retry: true, newMainTip: mainTip}
	}

	updateRefCmd := exec.CommandContext(ctx, "git", "update-ref", "refs/heads/"+targetBranch, runTip) //nolint:gosec // G204: fixed git/go binary with controlled args (config target branch, git SHAs, module path) — not user input
	updateRefCmd.Dir = projectDir
	if out, err := updateRefCmd.CombinedOutput(); err != nil {
		return commitAdvanceResult{done: &Outcome{
			Success: false,
			Reason:  fmt.Sprintf("git update-ref %s: %v\n%s", targetBranch, err, out),
		}}
	}

	return commitAdvanceResult{advanced: true, priorMainTip: mainTip}
}

func gitPushOrigin(ctx context.Context, projectDir, targetBranch string) ([]byte, error) {
	pushCmd := exec.CommandContext(ctx, "git", "push", "origin", targetBranch)
	pushCmd.Dir = projectDir
	return pushCmd.CombinedOutput()
}

func commitHandlePushFailure(ctx context.Context, projectDir, targetBranch, priorMainTip, advancedTip string, pushOut []byte, pushErr error, pushAttempt, maxPushAttempts int) commitOutcome {
	if cur, rerr := gitprobe.RevParse(ctx, projectDir, "refs/heads/"+targetBranch); rerr == nil && cur == advancedTip {
		gitUpdateRefBestEffort(ctx, projectDir, targetBranch, priorMainTip)
	}

	if !IsRetryablePushRejection(string(pushOut)) || pushAttempt >= maxPushAttempts {
		return commitOutcome{done: &Outcome{
			Success: false,
			Reason:  fmt.Sprintf("push_failed: %v\n%s", pushErr, pushOut),
		}}
	}

	fetchCmd := exec.CommandContext(ctx, "git", "fetch", "origin", targetBranch)
	fetchCmd.Dir = projectDir
	if fetchOut, fetchErr := fetchCmd.CombinedOutput(); fetchErr != nil {
		return commitOutcome{done: &Outcome{
			Success: false,
			Reason:  fmt.Sprintf("push_failed_fetch (attempt %d): %v\n%s", pushAttempt, fetchErr, fetchOut),
		}}
	}
	newMainTip, rerr := gitprobe.RevParse(ctx, projectDir, "refs/remotes/origin/"+targetBranch)
	if rerr != nil {
		return commitOutcome{done: &Outcome{
			Success: false,
			Reason:  fmt.Sprintf("push_failed_rev_parse_remote (attempt %d): rev-parse refs/remotes/origin/%s", pushAttempt, targetBranch),
		}}
	}
	updateToRemoteCmd := exec.CommandContext(ctx, "git", "update-ref", "refs/heads/"+targetBranch, newMainTip) //nolint:gosec // G204: fixed git/go binary with controlled args (config target branch, git SHAs, module path) — not user input
	updateToRemoteCmd.Dir = projectDir
	if updateOut, updateErr := updateToRemoteCmd.CombinedOutput(); updateErr != nil {
		return commitOutcome{done: &Outcome{
			Success: false,
			Reason:  fmt.Sprintf("push_failed_update_to_remote (attempt %d): %v\n%s", pushAttempt, updateErr, updateOut),
		}}
	}
	return commitOutcome{retryKind: mergePreparePushRetry, newMainTip: newMainTip}
}

func commitFinalizeWorkingTree(ctx context.Context, projectDir string, runID core.RunID, bus handlercontract.EventEmitter, beadID core.BeadID, mainTip, runTip, brPath string) {
	paths, pathsErr := mergedCommitPaths(ctx, projectDir, mainTip, runTip)
	if pathsErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: RunBranchToTarget: WARNING: cannot scope working-tree refresh, skipping it (bead %s run %s): %v\n",
			beadID, runID.String(), pathsErr)
		emitWorkingTreeRefreshFailed(ctx, bus, runID, beadID, pathsErr)
	} else if len(paths) > 0 {
		refreshMergedPaths(ctx, projectDir, runID, bus, beadID, mainTip, paths)
	}

	if brPath == "" {
		return
	}
	for _, p := range paths {
		if p == ".beads/issues.jsonl" {
			syncCmd := exec.CommandContext(ctx, brPath, "sync", "--import-only")
			syncCmd.Dir = projectDir
			if syncOut, syncErr := syncCmd.CombinedOutput(); syncErr != nil {
				emitBeadSyncFailed(ctx, bus, runID, syncErr, syncOut)
			}
			return
		}
	}
}

func gitUpdateRefBestEffort(ctx context.Context, dir, branch, sha string) {
	cmd := exec.CommandContext(ctx, "git", "update-ref", "refs/heads/"+branch, sha) //nolint:gosec // G204: fixed git/go binary with controlled args (config target branch, git SHAs, module path) — not user input
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "daemon: RunBranchToTarget: rollback update-ref %s failed: %v\n%s", branch, err, out)
	}
}

func prepareRebase(ctx context.Context, wtPath, projectDir string, runID core.RunID, runBranch, targetBranch string, runTip *string, mainTip string, kind mergePrepareKind, pushAttempt int) *Outcome {
	conflictReason := "rebase_conflict_on_non_ff_merge_retry"
	droppedReason := "rebase_dropped_commits_on_non_ff_merge_retry"
	if kind == mergePreparePushRetry {
		conflictReason = "rebase_conflict_on_push_retry"
		droppedReason = "rebase_dropped_commits_on_push_retry"
	}

	if _, statErr := os.Stat(wtPath); statErr == nil {
		DiscardDirtyChurn(ctx, wtPath)
		CommitResidualDelta(ctx, wtPath, runID)
		retryRebaseCmd := exec.CommandContext(ctx, "git", "rebase", targetBranch)
		retryRebaseCmd.Dir = wtPath
		if out, rebaseErr := retryRebaseCmd.CombinedOutput(); rebaseErr != nil {
			gitRebaseAbort(ctx, wtPath)
			return &Outcome{
				Success: false,
				Reason:  fmt.Sprintf("%s (attempt %d): %v\n%s", conflictReason, pushAttempt, rebaseErr, strings.TrimRight(string(out), "\n")),
			}
		}
	}

	if t, rerr := gitprobe.RevParse(ctx, projectDir, "refs/heads/"+runBranch); rerr == nil {
		*runTip = t
	}

	if *runTip == mainTip {
		return &Outcome{
			Success: false,
			Reason: fmt.Sprintf(
				"%s (attempt %d): rebase of %s onto %s produced no commits ahead of target",
				droppedReason, pushAttempt, runBranch, targetBranch),
		}
	}
	return nil
}

func isMergeBuildColdCacheError(out []byte) bool {
	s := string(out)
	return strings.Contains(s, "go-build cache") ||
		(strings.Contains(s, "could not import") && strings.Contains(s, "no such file"))
}

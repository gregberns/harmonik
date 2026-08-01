package runmerge

// merge.go — the §4.12 EM-052/EM-053 ordered merge-to-main sequence: the
// prepare/commit split (RSM-012..019), the rebase prepare passes, the
// exclusion-domain ref advance, the push, and the push-failure classification.
//
// Carved out of internal/daemon/workloop.go by P2 unit E5 RT13 (pure move).

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

// mergePrepareKind selects which reason-string vocabulary a re-prepare rebase
// emits, matching the pre-split call site: the first prepare, a re-prepare after
// a lost FF-check (non_ff_merge retry), or a re-prepare after a non-fast-forward
// push rejection (push retry).
type mergePrepareKind int

const (
	mergePrepareFirst mergePrepareKind = iota
	mergePrepareNonFFRetry
	mergePreparePushRetry
)

// commitOutcome is commitHandlePushFailure's classified result.
//
//   - done != nil  → a terminal merge outcome (a fatal push failure or an
//     exhausted budget); the driver returns it directly.
//   - otherwise    → the driver re-prepares (rebase) and re-attempts. retryKind
//     picks the reason-string variant; newMainTip is the fresh origin tip to
//     rebase onto.
type commitOutcome struct {
	done       *Outcome
	retryKind  mergePrepareKind
	newMainTip string
}

// commitAdvanceResult is commitAdvanceRef's classified result (Phase A, inside
// the exclusion domain).
//
//   - done != nil     → a terminal outcome (a fresh rev-parse failure, an
//     exhausted non-FF budget, or an update-ref failure); returned directly.
//   - retry == true   → a lost FF race: the target advanced concurrently below
//     the cap. The driver re-prepares (rebase onto newMainTip) and re-attempts.
//   - advanced == true → the local target ref now points at runTip. priorMainTip
//     is the target tip BEFORE the advance, carried out for the Phase-D
//     CAS-rollback and the Phase-C ledger diff base.
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

	// Guards + tip resolution + no-change short-circuits (all cheap rev-parse
	// reads, OUTSIDE the exclusion domain).
	runTip, mainTip, done := resolveMergeTips(ctx, projectDir, runBranch, targetBranch, headSHA, protectBranches)
	if done != nil {
		return *done
	}

	wtPath := workspace.WorktreePath(projectDir, runID.String(), workspace.NoWorktreeRootOverride())

	// hk-sfy7f: no-worktree fallback for remote runs. When wtPath does not exist,
	// attempt to create a temporary local worktree linked to refs/heads/run/<id>
	// so the prepare-phase rebase/build/fmt have a tree to operate on; the
	// deferred cleanup removes it after the merge (success or failure).
	if _, statErr := os.Stat(wtPath); statErr != nil {
		addWtCmd := exec.CommandContext(ctx, "git", "worktree", "add", wtPath, runBranch)
		addWtCmd.Dir = projectDir
		if _, addErr := addWtCmd.CombinedOutput(); addErr == nil {
			// WithoutCancel, not Background: the cleanup must still run when the
			// merge ctx is already cancelled (that is precisely when a temporary
			// worktree would otherwise be left behind), but it should keep the
			// ctx values the rest of the merge path carries.
			cleanupCtx := context.WithoutCancel(ctx)
			defer func() {
				RemoveWorktree(cleanupCtx, projectDir, wtPath)
			}()
		}
	}

	// Initial prepare (OUTSIDE the exclusion domain): churn discard, rebase onto
	// the target, rebase-drop guard, and run-context strip.
	if prepOut := prepareInitialMerge(ctx, wtPath, projectDir, runID, runBranch, targetBranch, &runTip, &mainTip); prepOut != nil {
		return *prepOut
	}

	// ── prepare→commit attempt loop ──────────────────────────────────────────
	// Each attempt has three phases, per RSM-016/019 (F4 push relocation, M4-C5):
	//
	//   Prepare (OUTSIDE the domain): the build + fmt gates and the rebase.
	//   Phase A (INSIDE the domain):  fresh FF re-validation + local update-ref.
	//   Phase B (OUTSIDE the domain): git push origin <target> — the network I/O
	//                                 is NO LONGER held under the exclusive section.
	//   Phase C (INSIDE the domain):  on push success, the working-tree reset +
	//                                 br sync reconciliation.
	//   Phase D (INSIDE the domain):  on push failure, CAS-rollback + fetch +
	//                                 re-base to origin (classify retry/terminal).
	//
	// A lost FF race (Phase A) or a non-FF push rejection (Phase D) re-prepares
	// (rebase OUTSIDE the domain) and re-attempts, up to maxPushAttempts. The
	// RSM-019 taxonomy (reason strings, retry cap, terminals) is byte-identical to
	// the pre-relocation form.
	//
	// Bead ref: hk-svieq (retry taxonomy); M4-C5 / T7 (push relocation).
	const maxPushAttempts = 3
	for pushAttempt := 1; pushAttempt <= maxPushAttempts; pushAttempt++ {
		// Post-merge build gate (hk-o68j3 / hk-ycp62) — prepare phase.
		if buildOut := runMergeBuildGate(ctx, wtPath, projectDir, runID, beadID, bus); buildOut != nil {
			return *buildOut
		}
		// Post-merge fmt gate (hk-k1hn) — prepare phase; may advance runTip via
		// the gofumpt/gci auto-fix commit landed in the worktree.
		if fmtOut, fmtRunTip := runMergeFmtGate(ctx, wtPath, projectDir, runID, beadID, bus); fmtOut != nil {
			return *fmtOut
		} else if fmtRunTip != "" {
			runTip = fmtRunTip
		}

		// Phase A (INSIDE the domain): re-validate the fast-forward against a
		// freshly read target tip and advance the LOCAL target ref. No network
		// push runs here (RSM-019 relocation) — only the cheap ref-mutation the
		// exclusion domain must serialize.
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
			// Lost FF race: re-prepare (rebase onto the fresh target) OUTSIDE the
			// domain and re-attempt (non_ff_merge retry vocabulary, RSM-019).
			mainTip = adv.newMainTip
			if prepOut := prepareRebase(ctx, wtPath, projectDir, runID, runBranch, targetBranch, &runTip, mainTip, mergePrepareNonFFRetry, pushAttempt); prepOut != nil {
				return *prepOut
			}
			continue
		}

		// Phase B (OUTSIDE the domain, RSM-019): publish the already-committed
		// local target ref to origin. The exclusive section is NOT held across
		// this network I/O — correctness comes from Phase D re-validating inside
		// the domain on conflict, not from holding the lock across the push.
		pushOut, pushErr := gitPushOrigin(ctx, projectDir, targetBranch)
		if pushErr == nil {
			// Phase C (INSIDE the domain): refresh the project working tree
			// (scoped to the merged commit's own paths, RSM-016/¶1, EM-054 as
			// amended by hk-7qmpp) and reconcile the ledger.
			if submitErr := submit(ctx, "commit-merge", func(qctx context.Context) error {
				commitFinalizeWorkingTree(qctx, projectDir, runID, bus, beadID, adv.priorMainTip, runTip, brPath)
				return nil
			}); submitErr != nil {
				// The push already published durably; the working-tree refresh is
				// best-effort (EM-054 is non-fatal), so still report success.
				fmt.Fprintf(os.Stderr, "daemon: RunBranchToTarget: WARNING: post-push finalize submit failed (bead %s run %s): %v\n",
					beadID, runID.String(), submitErr)
			}
			return Outcome{Success: true}
		}

		// Phase D (INSIDE the domain): classify the push failure. A non-FF
		// rejection below the cap CAS-rolls-back the local ref, fetches, advances
		// to the fresh origin tip, and signals a push-retry re-prepare; any other
		// failure (or an exhausted budget) is terminal.
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

		// Retry: re-prepare (rebase onto the fresh origin tip) OUTSIDE the domain.
		mainTip = co.newMainTip
		if prepOut := prepareRebase(ctx, wtPath, projectDir, runID, runBranch, targetBranch, &runTip, mainTip, co.retryKind, pushAttempt); prepOut != nil {
			return *prepOut
		}
	}

	// Defensive: Phase A / Phase D return a terminal outcome once attempts are
	// exhausted, so the loop always returns above. Fail closed if it does not.
	return Outcome{Success: false, Reason: "non_ff_merge: retry budget exhausted"}
}

// resolveMergeTips runs the merge preflight: the fail-closed target guards
// (hk-6r6xv), run-branch/target tip resolution, and the no-change short-circuits
// (hk-cwxow). It returns (runTip, mainTip, nil) to proceed, or (_, _, outcome)
// when the merge is already resolved (no-change or a guard failure). All reads
// are cheap rev-parse, OUTSIDE the exclusion domain.
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

	// Step 1: resolve run-branch tip. A missing branch means no commits → no-change.
	rt, rtErr := gitprobe.RevParse(ctx, projectDir, "refs/heads/"+runBranch)
	if rtErr != nil {
		return "", "", &Outcome{NoChange: true}
	}

	// Step 1b: resolve the target tip; equal tips → the agent made no commits.
	mt, mtErr := gitprobe.RevParse(ctx, projectDir, "refs/heads/"+targetBranch)
	if mtErr != nil {
		return "", "", &Outcome{Success: false, Reason: fmt.Sprintf("git rev-parse %s: %v", targetBranch, mtErr)}
	}
	if mt == rt {
		return "", "", &Outcome{NoChange: true}
	}

	// hk-cwxow: false-positive guard — runTip == fork-point SHA ⇒ no commits,
	// regardless of where the target now points.
	if headSHA != "" && rt == headSHA {
		return "", "", &Outcome{NoChange: true}
	}
	return rt, mt, nil
}

// prepareInitialMerge runs the first prepare pass OUTSIDE the exclusion domain:
// discard churn, rebase the run-branch onto the target, guard against a
// silently-dropped rebase, and strip run-context. It updates *runTip / *mainTip
// in place and returns nil on success, a terminal outcome on failure. All
// commands run in the per-run worktree (build-class → OUTSIDE the domain,
// RSM-017).
func prepareInitialMerge(ctx context.Context, wtPath, projectDir string, runID core.RunID, runBranch, targetBranch string, runTip, mainTip *string) *Outcome {
	if _, statErr := os.Stat(wtPath); statErr == nil {
		// Pre-rebase cleanup (hk-3yz2d, hk-aiw63): discard UNCOMMITTED churn.
		DiscardDirtyChurn(ctx, wtPath)
		// hk-rljho class: commit any residual TRACKED-but-uncommitted delta.
		CommitResidualDelta(ctx, wtPath, runID)
		// hk-g9zz: remove untracked //go:build integration-test artifacts.
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
		// Rebase succeeded — re-resolve runTip and targetTip (both may have changed).
		if t, rerr := gitprobe.RevParse(ctx, projectDir, "refs/heads/"+runBranch); rerr == nil {
			*runTip = t
		}
		if t, rerr := gitprobe.RevParse(ctx, projectDir, "refs/heads/"+targetBranch); rerr == nil {
			*mainTip = t
		}

		// hk-zmpd: rebase-drop guard — a rebase that silently drops every commit
		// as "already applied" must fail-closed so reviewed work is salvageable.
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

	// hk-4je: strip .harmonik/run-context/** from the run-branch before the
	// fast-forward update-ref.
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

// gitRebaseAbort runs `git rebase --abort` best-effort in wtPath, logging on
// failure (the caller has already captured the originating rebase error).
func gitRebaseAbort(ctx context.Context, wtPath string) {
	abortCmd := exec.CommandContext(ctx, "git", "rebase", "--abort")
	abortCmd.Dir = wtPath
	if out, err := abortCmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "daemon: RunBranchToTarget: git rebase --abort failed in %s: %v\n%s", wtPath, err, out)
	}
}

// runMergeBuildGate runs go build+vet on the merged tree in the run-branch
// worktree (or projectDir when the worktree is gone) — the prepare-phase build
// gate. It runs OUTSIDE the merge exclusion domain (RSM-017): no update-ref has
// advanced the target, so a failure needs no rollback (a deliberate delta from
// the pre-split form, allowlisted M3-D12: build failures no longer transiently
// advance the target ref). Returns nil on pass, a terminal outcome on failure.
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

// mergeBuildRunner runs one `go <args>` in dir and returns its combined output.
// A seam so runMergeBuildStep's retry schedule is testable without racing a real
// `go clean -cache` against a real compile.
type mergeBuildRunner func(ctx context.Context, dir string, args []string) ([]byte, error)

// execGoInDir is the production mergeBuildRunner.
func execGoInDir(ctx context.Context, dir string, args []string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = dir
	return cmd.CombinedOutput()
}

// mergeBuildColdCacheBackoff is the wait before each cold-cache retry of a
// merge-gate build step; its length is the number of RETRIES (total attempts =
// len+1).
//
// hk-44ab2 retried once, IMMEDIATELY. That is too eager to survive the failure
// it targets: `go build` aborts within seconds of the first vanished cache
// entry, while the `rm -rf` of a multi-GiB GOCACHE that caused it runs for
// considerably longer, so the immediate retry re-enters the same deletion window
// and fails again. hk-pgtbr recorded a merge-gate rejection on 2026-07-21 with
// the single-retry code already in the tree since 2026-07-04 (9b8288ad).
// Backing off puts the later attempts after the deletion rather than inside it.
var mergeBuildColdCacheBackoff = []time.Duration{3 * time.Second, 9 * time.Second}

// runMergeBuildStep runs one merge-gate build step, retrying only on the
// cold-cache signature (isMergeBuildColdCacheError) per the backoff schedule.
// Any other failure — a genuine compile error — returns on the FIRST attempt so
// a real regression is never masked or delayed. Returns the last attempt's
// output and error.
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

// commitAdvanceRef is the Phase-A merge exclusion-domain critical section
// (RSM-016): it runs entirely INSIDE mergeq.Queue.Submit and performs no
// build-class command and NO network push (RSM-017 / RSM-019 relocation). It
// re-reads the target tip freshly (re-validate under lock), re-runs the FF-check,
// and advances the LOCAL target ref to runTip. The network `git push` runs
// OUTSIDE this section (Phase B); a lost FF race is classified as a retry so the
// driver re-prepares (rebase) OUTSIDE the domain.
func commitAdvanceRef(ctx context.Context, projectDir, runTip, targetBranch string, pushAttempt, maxPushAttempts int) commitAdvanceResult {
	// Re-validate under lock (RSM-016): re-read the target tip freshly. Between
	// the prepare-phase rebase (OUTSIDE the domain) and this critical section, a
	// sibling merge may have advanced the target; the fresh read + FF-check below
	// is the re-validation the pre-split form got implicitly from holding mergeMu
	// across the whole sequence.
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

	// Step 3: fast-forward check. target MUST be an ancestor of runTip.
	isAncestor, ancestryErr := gitprobe.IsAncestor(ctx, projectDir, mainTip, runTip)
	if ancestryErr != nil {
		return commitAdvanceResult{done: &Outcome{
			Success: false,
			Reason:  fmt.Sprintf("non_ff_merge_ancestry_check: %v", ancestryErr),
		}}
	}
	if !isAncestor {
		// Non-FF: the target advanced concurrently (hk-1u4wp). Re-prepare (rebase
		// onto the fresh target) and retry — up to maxPushAttempts total.
		if pushAttempt >= maxPushAttempts {
			return commitAdvanceResult{done: &Outcome{
				Success: false,
				Reason:  fmt.Sprintf("non_ff_merge: %s advanced concurrently", targetBranch),
			}}
		}
		return commitAdvanceResult{retry: true, newMainTip: mainTip}
	}

	// Step 3a: fast-forward the target branch to runTip.
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

// gitPushOrigin publishes refs/heads/<targetBranch> to origin. It runs OUTSIDE
// the merge exclusion domain (RSM-019 / M4-C5 F4 relocation): the exclusive
// section serializes local ref + working-tree mutation, not network publication.
func gitPushOrigin(ctx context.Context, projectDir, targetBranch string) ([]byte, error) {
	pushCmd := exec.CommandContext(ctx, "git", "push", "origin", targetBranch)
	pushCmd.Dir = projectDir
	return pushCmd.CombinedOutput()
}

// commitHandlePushFailure rolls back the local ref-advance and classifies a push
// failure (Phase D, inside the exclusion domain): a non-fast-forward rejection
// below the retry cap fetches the new remote tip, advances the local target to
// it, and signals a push-retry re-prepare; any other failure (or an exhausted
// budget) is terminal. All commands (update-ref, fetch, rev-parse) are
// commit-allowlisted (RSM-017).
//
// The rollback is COMPARE-AND-SWAP on advancedTip: because the push now runs
// OUTSIDE the domain (Phase B), a sibling merge may have advanced+published the
// local target in the window between our Phase-A update-ref and this handler.
// Rolling the ref back unconditionally would clobber the sibling's advance, so we
// only regress to priorMainTip when the target still points at the tip WE set.
func commitHandlePushFailure(ctx context.Context, projectDir, targetBranch, priorMainTip, advancedTip string, pushOut []byte, pushErr error, pushAttempt, maxPushAttempts int) commitOutcome {
	// CAS rollback: only regress the local target if it is STILL the tip we
	// advanced it to (a sibling may have moved it under the relocated push).
	if cur, rerr := gitprobe.RevParse(ctx, projectDir, "refs/heads/"+targetBranch); rerr == nil && cur == advancedTip {
		gitUpdateRefBestEffort(ctx, projectDir, targetBranch, priorMainTip)
	}

	pushOutStr := string(pushOut)
	isNonFF := strings.Contains(pushOutStr, "non-fast-forward") || strings.Contains(pushOutStr, "[rejected]")
	if !isNonFF || pushAttempt >= maxPushAttempts {
		return commitOutcome{done: &Outcome{
			Success: false,
			Reason:  fmt.Sprintf("push_failed: %v\n%s", pushErr, pushOut),
		}}
	}

	// Non-FF push rejection: fetch the new remote tip, advance the local target to
	// it, and re-prepare (rebase) OUTSIDE the domain on retry.
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

// commitFinalizeWorkingTree refreshes the project working tree after a successful
// push (EM-054) and reconciles the bead ledger (BL-MRG-004/005). All steps are
// best-effort / non-fatal — the merge is already durable.
func commitFinalizeWorkingTree(ctx context.Context, projectDir string, runID core.RunID, bus handlercontract.EventEmitter, beadID core.BeadID, mainTip, runTip, brPath string) {
	// Step 5: refresh the working tree, SCOPED to the paths the merged commit
	// itself changed (hk-7qmpp).
	//
	// This step used to be a tree-wide `git restore --staged .` + `git reset
	// --hard HEAD`. That is strictly stronger than EM-054 needs, and on
	// 2026-07-22 it silently destroyed uncommitted fleet state in the main root
	// on every merge. The interaction that made it invisible: the pre-merge
	// escape check (CheckMainWorkingTreeDirty) FAILS a run when main is dirty,
	// but its churn allowlist deliberately exempts `.harmonik/` and `.claude/` —
	// exactly where agent and fleet state live. So the one region waved through
	// as expected churn was the one region the refresh then deleted.
	//
	// Scoping the refresh to the merge's own paths satisfies EM-054's obligation
	// ("the merged commit's files match HEAD") and cannot touch anything the
	// merge did not write.
	paths, pathsErr := mergedCommitPaths(ctx, projectDir, mainTip, runTip)
	if pathsErr != nil {
		// Without the path list there is no safe refresh: a tree-wide reset is
		// the destructive behaviour this change exists to remove. Skip it.
		//
		// Be precise about what is loud: the EVENT below is, the resulting STATE
		// is not. CheckMainWorkingTreeDirty drops .harmonik/, .claude/,
		// .beads/issues.jsonl and AGENT_COMMS.md as expected churn, so stale
		// paths in exactly the region this bead exists to protect are never
		// surfaced by the escape check. Worse, the skip leaves the INDEX stale
		// too (index at mainTip, HEAD at runTip), so a later commit of those
		// paths from the main root would silently commit PRE-MERGE content.
		// Still the right trade — the trigger needs a git diff between two
		// known-good SHAs to fail, while the behaviour being removed fired on
		// every merge — but it is a real hole, not a clean fallback.
		fmt.Fprintf(os.Stderr, "daemon: RunBranchToTarget: WARNING: cannot scope working-tree refresh, skipping it (bead %s run %s): %v\n",
			beadID, runID.String(), pathsErr)
		emitWorkingTreeRefreshFailed(ctx, bus, runID, beadID, pathsErr)
	} else if len(paths) > 0 {
		refreshMergedPaths(ctx, projectDir, runID, bus, beadID, mainTip, paths)
	}

	// BL-MRG-004/005: reconcile the bead ledger when the merge touched
	// .beads/issues.jsonl (non-fatal). brPath == "" disables the step.
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

// gitUpdateRefBestEffort advances refs/heads/<branch> to sha in dir, logging on
// failure (used for the push-failure rollback, where a failed rollback is
// surfaced to reconciliation, EM-INV-005, rather than aborting).
func gitUpdateRefBestEffort(ctx context.Context, dir, branch, sha string) {
	cmd := exec.CommandContext(ctx, "git", "update-ref", "refs/heads/"+branch, sha) //nolint:gosec // G204: fixed git/go binary with controlled args (config target branch, git SHAs, module path) — not user input
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "daemon: RunBranchToTarget: rollback update-ref %s failed: %v\n%s", branch, err, out)
	}
}

// prepareRebase re-prepares the run-branch for a re-attempt after a lost FF race
// or a non-FF push rejection: it rebases the run-branch onto the (already
// updated) target OUTSIDE the exclusion domain (RSM-017). It updates *runTip in
// place and returns nil on success, a terminal outcome on a rebase conflict or a
// silently-dropped rebase. The kind selects the reason-string variant so the
// pre-split "(attempt N)" strings are preserved.
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

	// hk-zmpd: rebase-drop guard — reviewed commits must survive onto the new base.
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

// isMergeBuildColdCacheError reports whether the go build/vet output matches
// the cold-build-cache failure signature observed after the proactive go-cache
// reaper runs in the TOCTOU window before the merge-build starts (hk-44ab2):
//
//   - "go-build cache" in output: Go toolchain references the deleted cache path
//   - "could not import" + "no such file": stdlib lookup fails against cold cache
//
// These are transient: a single retry almost always succeeds because the first
// attempt repopulates cache entries for subsequent compilations.
func isMergeBuildColdCacheError(out []byte) bool {
	s := string(out)
	return strings.Contains(s, "go-build cache") ||
		(strings.Contains(s, "could not import") && strings.Contains(s, "no such file"))
}

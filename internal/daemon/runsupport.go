package daemon

// runsupport.go — run-path helpers shared by the DOT cascade and the substrate.
//
// These declarations lived in reviewloop.go until the review-loop driver was
// retired. They were never review-loop-specific: every one of them has a live
// caller on the DOT path or in the shared substrate, which is why they were
// relocated here rather than deleted with the driver. The former `rl` prefix is
// dropped for the same reason — it named a mode that no longer exists.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"unicode/utf8"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/gitprobe"
	"github.com/gregberns/harmonik/internal/handlercontract"
	tmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	"github.com/gregberns/harmonik/internal/workspace"
)

// substrateRunnerObserver is a TEST SEAM (hk-fxy9). When non-nil it is invoked
// with the CommandRunner passed into newPerRunSubstrate at the DOT agentic launch
// sites, letting a regression test assert the SUBSTRATE-spawn runner is the real
// (non-nil) worker runner for a REMOTE run — distinct from the SPEC runner the
// hk-3sus test already covers. nil in production (zero overhead).
var substrateRunnerObserver func(tmux.CommandRunner)

// notifySubstrateRunner invokes substrateRunnerObserver if set. No-op in prod.
// Called from tmuxsubstrate.go, which is mode-blind.
func notifySubstrateRunner(r tmux.CommandRunner) {
	if substrateRunnerObserver != nil {
		substrateRunnerObserver(r)
	}
}

// priorVerdictSummaryMaxBytes is the maximum byte length of the
// prior_verdict_summary field in implementer_resumed events, per
// event-model.md §8.1a.1 (front-truncation to 256 UTF-8 bytes).
const priorVerdictSummaryMaxBytes = 256

// truncateUTF8 returns the prefix of s that is at most maxBytes UTF-8 bytes,
// trimming any incomplete trailing code unit per event-model.md §6.3.
func truncateUTF8(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	b := []byte(s[:maxBytes])
	for len(b) > 0 && !utf8.Valid(b) {
		b = b[:len(b)-1]
	}
	return string(b)
}

// computeDiffHash resolves the current HEAD of the worktree and computes the
// diff hash against parentSHA. Used by the DOT cascade's no-progress detector.
func computeDiffHash(ctx context.Context, wtPath, parentSHA string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "HEAD")
	cmd.Dir = wtPath
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("daemon: computeDiffHash: git rev-parse HEAD in %q: %w", wtPath, err)
	}
	headSHA := string(out)
	for len(headSHA) > 0 && headSHA[len(headSHA)-1] == '\n' {
		headSHA = headSHA[:len(headSHA)-1]
	}
	if headSHA == "" {
		return "", fmt.Errorf("daemon: computeDiffHash: git rev-parse HEAD returned empty in %q", wtPath)
	}
	return workspace.ComputeDiffHash(ctx, wtPath, parentSHA, headSHA)
}

// computeDiffHashVia is like computeDiffHash but routes both the HEAD probe and
// the diff through runner. When runner is nil (every LOCAL run) it delegates to
// computeDiffHash byte-identically (NFR7); only REMOTE DOT runs (runner is an
// SSHRunner) take the routed path, REQUIRED when the worktree is on a worker
// whose filesystem box A cannot read directly.
func computeDiffHashVia(ctx context.Context, runner tmux.CommandRunner, wtPath, parentSHA string) (string, error) {
	if runner == nil {
		return computeDiffHash(ctx, wtPath, parentSHA)
	}
	headSHA, err := gitprobe.ResolveWorktreeHEADVia(ctx, runner, wtPath)
	if err != nil {
		return "", fmt.Errorf("daemon: computeDiffHashVia: %w", err)
	}
	return workspace.ComputeDiffHashVia(ctx, runner, wtPath, parentSHA, headSHA)
}

// emitReviewFixupStalled emits review_fixup_stalled (§8.1a.7) when a
// REQUEST_CHANGES fix-up run advances HEAD by zero commits. The DOT cascade
// terminates directly on this signal.
func emitReviewFixupStalled(
	ctx context.Context,
	bus handlercontract.EventEmitter,
	runID core.RunID,
	workflowMode core.WorkflowMode,
	iterationCount int,
	reviewerFlags []string,
	diffHashCurrent string,
	diffHashPrior string,
) {
	flags := reviewerFlags
	if flags == nil {
		flags = []string{}
	}
	pl := core.ReviewFixupStalledPayload{
		RunID:           runID,
		WorkflowMode:    workflowMode,
		IterationCount:  iterationCount,
		ReviewerFlags:   flags,
		DiffHashCurrent: diffHashCurrent,
		DiffHashPrior:   diffHashPrior,
	}
	b, err := json.Marshal(pl)
	if err != nil {
		return
	}
	_ = bus.EmitWithRunID(ctx, runID, core.EventTypeReviewFixupStalled, b)
}

// emitReviewerBudgetExceeded emits a reviewer_budget_exceeded event (hk-da3rr)
// when a reviewer session is force-killed for exhausting its diff-scaled verdict
// budget. Non-fatal: a nil bus or marshal error is silently discarded.
func emitReviewerBudgetExceeded(ctx context.Context, bus handlercontract.EventEmitter, runID core.RunID, budgetMS, elapsedMS int64, changedLines int, reason string) {
	if bus == nil {
		return
	}
	if reason == "" {
		reason = "reviewer-budget-exceeded"
	}
	pl := core.ReviewerBudgetExceededPayload{
		RunID:        runID.String(),
		BudgetMS:     budgetMS,
		ElapsedMS:    elapsedMS,
		ChangedLines: changedLines,
		Reason:       reason,
	}
	b, err := json.Marshal(pl)
	if err != nil {
		fmt.Fprintf(os.Stderr, "daemon: emitReviewerBudgetExceeded: marshal: %v\n", err)
		return
	}
	_ = bus.EmitWithRunID(ctx, runID, core.EventTypeReviewerBudgetExceeded, b)
}

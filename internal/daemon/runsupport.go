package daemon

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

var substrateRunnerObserver func(tmux.CommandRunner)

func notifySubstrateRunner(r tmux.CommandRunner) {
	if substrateRunnerObserver != nil {
		substrateRunnerObserver(r)
	}
}

const priorVerdictSummaryMaxBytes = 256

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
	if emitErr := bus.EmitWithRunID(ctx, runID, core.EventTypeReviewFixupStalled, b); emitErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: emit review_fixup_stalled: %v\n", emitErr)
	}
}

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
	if emitErr := bus.EmitWithRunID(ctx, runID, core.EventTypeReviewerBudgetExceeded, b); emitErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: emit reviewer_budget_exceeded: %v\n", emitErr)
	}
}

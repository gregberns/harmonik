package daemon

// reviewtrailers_hkdyim.go — review verdict commit-trailer injection (hk-dyim).
//
// The daemon review loop fires and APPROVEs a bead, but the merge commit that
// lands on main carries no Reviewed-By / Review-Verdict trailers — the review
// audit trail never reaches git history. This file implements the fix:
// appendReviewTrailersToHEAD amends the HEAD commit in the implementer's
// worktree (before the FF merge) to embed the verdict as git trailers, matching
// the format documented in the agent-reviewer skill contract (SKILL.md §"How the
// verdict lands in git").
//
// Bead: hk-dyim.

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/gregberns/harmonik/internal/workspace"
)

// reviewedByTrailerValue is the fixed Reviewed-By: trailer value per the
// agent-reviewer skill contract (SKILL.md §"How the verdict lands in git").
const reviewedByTrailerValue = "agent-reviewer"

// appendReviewTrailersToHEAD amends the HEAD commit in wtPath to add
//
//	Reviewed-By: agent-reviewer
//	Review-Verdict: <verdict-json>
//
// trailers from the given APPROVE verdict, matching the agent-reviewer skill
// contract. The commit tree (working files) is unchanged; only the commit
// message is amended. Idempotent: if both trailers are already present the
// amend is skipped.
//
// Called from the review-loop APPROVE path in workloop.go immediately before
// lockedMergeRunBranchToMain so that the trailer-bearing commit is the one
// that the FF merge fast-forwards main to.
//
// Returns an error when the amend fails; the caller treats this as non-fatal
// and logs it, proceeding with the merge without trailers.
//
// Bead: hk-dyim.
func appendReviewTrailersToHEAD(ctx context.Context, wtPath string, verdict *workspace.ReviewVerdict) error {
	if verdict == nil {
		return nil
	}

	// Marshal the full verdict struct as JSON for the Review-Verdict trailer.
	// Use the ReviewVerdict struct directly so the JSON fields match the
	// agent-reviewer schema v1 exactly (same tags as workspace.ReviewVerdict).
	verdictJSON, err := json.Marshal(verdict)
	if err != nil {
		return fmt.Errorf("appendReviewTrailersToHEAD: marshal verdict: %w", err)
	}

	reviewedByLine := "Reviewed-By: " + reviewedByTrailerValue
	reviewVerdictLine := "Review-Verdict: " + string(verdictJSON)

	// Read the current HEAD commit message.
	logCmd := exec.CommandContext(ctx, "git", "log", "-1", "--format=%B", "HEAD")
	logCmd.Dir = wtPath
	out, err := logCmd.Output()
	if err != nil {
		return fmt.Errorf("appendReviewTrailersToHEAD: git log HEAD: %w", err)
	}
	existing := strings.TrimRight(string(out), "\n")

	// Idempotency: skip if both trailers are already present.
	hasReviewedBy := containsExactLine(existing, reviewedByLine)
	hasReviewVerdict := containsExactLine(existing, reviewVerdictLine)
	if hasReviewedBy && hasReviewVerdict {
		return nil
	}

	// Append missing trailers. Trailers must be separated from the body by a
	// blank line (git trailer convention). The two trailer lines are adjacent.
	newMsg := existing
	if !hasReviewedBy && !hasReviewVerdict {
		newMsg = existing + "\n\n" + reviewedByLine + "\n" + reviewVerdictLine
	} else if !hasReviewedBy {
		newMsg = existing + "\n" + reviewedByLine
	} else {
		newMsg = existing + "\n" + reviewVerdictLine
	}

	amendCmd := exec.CommandContext(ctx, "git", "commit", "--amend", "-m", newMsg)
	amendCmd.Dir = wtPath
	if out, err := amendCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("appendReviewTrailersToHEAD: git commit --amend: %w\ngit output: %s", err, out)
	}
	return nil
}

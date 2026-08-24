package runmerge

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/gregberns/harmonik/internal/gitprobe"
	"github.com/gregberns/harmonik/internal/harness/shared"
	"github.com/gregberns/harmonik/internal/workspace"
)

const reviewedByTrailerValue = "agent-reviewer"

// AppendReviewTrailersToHEAD amends the HEAD commit in wtPath to add
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
func AppendReviewTrailersToHEAD(ctx context.Context, wtPath string, verdict *workspace.ReviewVerdict) error {
	if verdict == nil {
		return nil
	}

	verdictJSON, err := json.Marshal(verdict)
	if err != nil {
		return fmt.Errorf("AppendReviewTrailersToHEAD: marshal verdict: %w", err)
	}

	reviewedByLine := "Reviewed-By: " + reviewedByTrailerValue
	reviewVerdictLine := "Review-Verdict: " + string(verdictJSON)

	// Reading the HEAD message changes nothing, so gitprobe may run it again
	// when the git process does not complete.
	out, err := gitprobe.Output(ctx, wtPath, "log", "-1", "--format=%B", "HEAD")
	if err != nil {
		return fmt.Errorf("AppendReviewTrailersToHEAD: git log HEAD: %w", err)
	}
	existing := strings.TrimRight(string(out), "\n")

	hasReviewedBy := shared.ContainsExactLine(existing, reviewedByLine)
	hasReviewVerdict := shared.ContainsExactLine(existing, reviewVerdictLine)
	if hasReviewedBy && hasReviewVerdict {
		return nil
	}

	var newMsg string
	switch {
	case !hasReviewedBy && !hasReviewVerdict:
		newMsg = existing + "\n\n" + reviewedByLine + "\n" + reviewVerdictLine
	case !hasReviewedBy:
		newMsg = existing + "\n" + reviewedByLine
	default:
		newMsg = existing + "\n" + reviewVerdictLine
	}

	if out, err := amendHEADMessage(ctx, wtPath, newMsg); err != nil {
		return fmt.Errorf("AppendReviewTrailersToHEAD: git commit --amend: %w\ngit output: %s", err, out)
	}
	return nil
}

// amendHEADMessage rewrites the HEAD commit message. The amend is commit-class:
// it makes a new commit object, so a second run would rewrite a commit that was
// already rewritten. It does not go through gitprobe's blind retry — a stopped
// amend is decided from HEAD, which moves when the amend lands.
//
// Bead: hk-7neu1.
func amendHEADMessage(ctx context.Context, wtPath, message string) ([]byte, error) {
	return runCommitOnce(ctx, wtPath, func() ([]byte, error) {
		cmd := exec.CommandContext(ctx, "git", "commit", "--amend", "-m", message)
		cmd.Dir = wtPath
		return cmd.CombinedOutput()
	})
}

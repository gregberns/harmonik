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

// ReplaceReviewTrailersOnHEAD amends the HEAD commit in wtPath to replace its
// review trailers with
//
//	Reviewed-By: agent-reviewer
//	Review-Verdict: <verdict-json>
//
// trailers from the given APPROVE verdict, matching the agent-reviewer skill
// contract. The commit tree (working files) is unchanged; only the commit
// message is amended. The subject, body, and unrelated trailers retain their
// order. Idempotent: if both review trailers are already correct and unique,
// the amend is skipped.
//
// Called from the review-loop APPROVE path in workloop.go immediately before
// lockedMergeRunBranchToMain so that the trailer-bearing commit is the one
// that the FF merge fast-forwards main to.
//
// Returns an error when the amend fails; the caller treats this as non-fatal
// and logs it, proceeding with the merge without trailers.
//
// Bead: hk-dyim.
func ReplaceReviewTrailersOnHEAD(ctx context.Context, wtPath string, verdict *workspace.ReviewVerdict) error {
	if verdict == nil {
		return nil
	}

	verdictJSON, err := json.Marshal(verdict)
	if err != nil {
		return fmt.Errorf("ReplaceReviewTrailersOnHEAD: marshal verdict: %w", err)
	}

	reviewedByLine := "Reviewed-By: " + reviewedByTrailerValue
	reviewVerdictLine := "Review-Verdict: " + string(verdictJSON)

	// Reading the HEAD message changes nothing, so gitprobe may run it again
	// when the git process does not complete.
	out, err := gitprobe.Output(ctx, wtPath, "log", "-1", "--format=%B", "HEAD")
	if err != nil {
		return fmt.Errorf("ReplaceReviewTrailersOnHEAD: git log HEAD: %w", err)
	}
	existing := strings.TrimRight(string(out), "\n")

	hasReviewedBy := shared.ContainsExactLine(existing, reviewedByLine)
	hasReviewVerdict := shared.ContainsExactLine(existing, reviewVerdictLine)
	if hasReviewedBy && hasReviewVerdict &&
		strings.Count(existing, "Reviewed-By:") == 1 && strings.Count(existing, "Review-Verdict:") == 1 {
		return nil
	}

	// Commit trailers occupy the final paragraph. Remove only the review keys
	// from that paragraph so examples or prose in the body remain byte-for-byte
	// unchanged and unrelated trailers keep their position.
	body, trailers := "", existing
	if split := strings.LastIndex(existing, "\n\n"); split >= 0 {
		body, trailers = existing[:split], existing[split+2:]
	}
	kept := make([]string, 0, len(strings.Split(trailers, "\n"))+2)
	for _, line := range strings.Split(trailers, "\n") {
		if strings.HasPrefix(line, "Reviewed-By:") || strings.HasPrefix(line, "Review-Verdict:") {
			continue
		}
		kept = append(kept, line)
	}
	kept = append(kept, reviewedByLine, reviewVerdictLine)
	newMsg := strings.Join(kept, "\n")
	if body != "" {
		newMsg = body + "\n\n" + newMsg
	}

	if out, err := amendHEADMessage(ctx, wtPath, newMsg); err != nil {
		return fmt.Errorf("ReplaceReviewTrailersOnHEAD: git commit --amend: %w\ngit output: %s", err, out)
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

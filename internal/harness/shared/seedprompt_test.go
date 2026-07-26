package shared_test

// seedprompt_test.go — direct unit tests for the resume seed prompt.
//
// Before P2 unit E1a this builder was only tested THROUGH the pi and codex
// launch specs, which meant the clamp — the rule that keeps the prompt from
// pointing at a reviewer-feedback.iter-0.md that never exists — had no test of
// its own. Moving the function is the moment to fix that.

import (
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/harness/shared"
)

func TestImplementerResumeSeedPrompt_PointsAtPriorIteration(t *testing.T) {
	t.Parallel()

	got := shared.ImplementerResumeSeedPrompt("hk-abc12", 3)

	if !strings.Contains(got, ".harmonik/reviewer-feedback.iter-3.md") {
		t.Errorf("prompt must name the prior iteration's feedback file; got:\n%s", got)
	}
	if !strings.Contains(got, "Refs: hk-abc12") {
		t.Errorf("prompt must demand the Refs trailer for the bead; got:\n%s", got)
	}
}

// TestImplementerResumeSeedPrompt_ClampsBelowOne pins the clamp: iteration 0 and
// negatives must still name iter-1. Without it the prompt sends the implementer
// to reviewer-feedback.iter-0.md, a file the daemon never writes, and the
// "address every point" instruction silently has no content behind it.
func TestImplementerResumeSeedPrompt_ClampsBelowOne(t *testing.T) {
	t.Parallel()

	for _, prior := range []int{0, -1, -7} {
		got := shared.ImplementerResumeSeedPrompt("hk-abc12", prior)
		if !strings.Contains(got, "reviewer-feedback.iter-1.md") {
			t.Errorf("priorIteration=%d must clamp to iter-1; got:\n%s", prior, got)
		}
		if strings.Contains(got, "iter-0.md") || strings.Contains(got, "iter--") {
			t.Errorf("priorIteration=%d produced a feedback path the daemon never writes; got:\n%s", prior, got)
		}
	}
}

// TestImplementerResumeSeedPrompt_DemandsANewCommit pins the other half of the
// c073 fix: the resumed implementer must be told a new commit is required, or it
// re-satisfies the prompt it already met and the run no-progress-loops.
func TestImplementerResumeSeedPrompt_DemandsANewCommit(t *testing.T) {
	t.Parallel()

	got := shared.ImplementerResumeSeedPrompt("hk-abc12", 1)
	if !strings.Contains(got, "NEW git commit") {
		t.Errorf("prompt must demand a new commit; got:\n%s", got)
	}
}

package daemon

// pasteinject_hkzn3vs_internal_test.go — regression floor for hk-zn3vs.
//
// The reviewer kick-off seed MUST stay short. A long single-line paste is
// collapsed by the Claude Code TUI into a "[Pasted text #N]" placeholder chip,
// so the literal "review-target.md" marker never renders into the pane and
// injectAndVerifySeed fails deterministically ("seed marker absent from pane"),
// wedging every review taskless. These tests lock the seed short and confirm the
// marker the verifier checks for is actually present in it.

import (
	"strings"
	"testing"
)

func TestReviewerKickoffSeedStaysShort(t *testing.T) {
	if got := len(reviewerKickoffSeed); got >= reviewerSeedMaxLen {
		t.Fatalf("reviewerKickoffSeed length %d must stay under reviewerSeedMaxLen %d — "+
			"a long single-line paste is collapsed into a placeholder chip so the marker "+
			"never renders (hk-zn3vs). Move new reviewer constraints into "+
			"buildReviewTargetContent (review-target.md), not this seed.", got, reviewerSeedMaxLen)
	}
}

func TestReviewerKickoffSeedContainsVerifyMarker(t *testing.T) {
	// injectAndVerifySeed is called with marker "review-target.md" in
	// pasteInjectReviewer; the seed must contain it or verification can never pass.
	const marker = "review-target.md"
	if !strings.Contains(reviewerKickoffSeed, marker) {
		t.Fatalf("reviewerKickoffSeed must contain the verify marker %q (hk-zn3vs)", marker)
	}
}

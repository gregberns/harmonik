package workspace

// feedbackorigin_hk2f3v4_test.go — the feedback file must not claim a review
// that did not happen (hk-2f3v4).
//
// Observed live on the codex:local cell, 2026-08-10: a red commit gate routed
// the run back to the implementer, and the daemon delivered the gate's output
// through .harmonik/reviewer-feedback.iter-N.md under the heading "Reviewer
// feedback" with `verdict: GATE_FAIL`. No reviewer node had run at any point.
// The implementer read it as a reviewer asking for a change and spent three
// passes acting on that reading.
//
// The file name cannot move — the resume instruction, both harness launch specs
// and the gitignore hygiene set all know it. So the DOCUMENT declares its own
// origin, and the origin is derived from the verdict rather than from a flag a
// future call site could forget to set.

import (
	"strings"
	"testing"
)

func TestVerdictCameFromAReviewer_ClosedToSchemaV1(t *testing.T) {
	t.Parallel()

	for _, v := range []string{ReviewVerdictApprove, ReviewVerdictRequestChanges, ReviewVerdictBlock} {
		if !VerdictCameFromAReviewer(v) {
			t.Errorf("%q is a schema-v1 reviewer verdict and must read as reviewer-produced", v)
		}
	}
	// GATE_FAIL and NO_COMMIT are the two the daemon writes today. "" and an
	// unrecognized string must fall the same way: no reviewer is claimed unless
	// one is proven.
	for _, v := range []string{"GATE_FAIL", "NO_COMMIT", "", "approve", "LGTM"} {
		if VerdictCameFromAReviewer(v) {
			t.Errorf("%q is not a schema-v1 reviewer verdict and must NOT read as reviewer-produced", v)
		}
	}
}

func TestFeedbackContent_GateFailDoesNotClaimAReviewer(t *testing.T) {
	t.Parallel()

	got := buildReviewerFeedbackContent(ReviewerFeedbackPayload{
		WorkspacePath:  "/tmp/wt",
		PriorIteration: 3,
		Verdict:        "GATE_FAIL",
		Notes:          "The commit gate failed — your commit was recorded but the build/test gate did not pass.",
	})

	if strings.Contains(got, "# Reviewer feedback") {
		t.Fatalf("a daemon-produced GATE_FAIL is headed as reviewer feedback:\n%s", got)
	}
	if !strings.Contains(got, "NO REVIEWER READ YOUR CHANGE") {
		t.Fatalf("a daemon-produced GATE_FAIL does not say no reviewer read the change:\n%s", got)
	}
	// The notes and verdict are still delivered — the point is to name the
	// author, not to withhold the diagnostic.
	if !strings.Contains(got, "verdict: GATE_FAIL") || !strings.Contains(got, "build/test gate did not pass") {
		t.Fatalf("the gate diagnostic did not survive the heading change:\n%s", got)
	}
}

func TestFeedbackContent_NoCommitDoesNotClaimAReviewer(t *testing.T) {
	t.Parallel()

	got := buildReviewerFeedbackContent(ReviewerFeedbackPayload{
		WorkspacePath:  "/tmp/wt",
		PriorIteration: 1,
		Verdict:        "NO_COMMIT",
		Notes:          "Your previous pass produced NO commit.",
	})

	if strings.Contains(got, "# Reviewer feedback") {
		t.Fatalf("a daemon-produced NO_COMMIT is headed as reviewer feedback:\n%s", got)
	}
	if !strings.Contains(got, "NO REVIEWER READ YOUR CHANGE") {
		t.Fatalf("a daemon-produced NO_COMMIT does not say no reviewer read the change:\n%s", got)
	}
}

func TestFeedbackContent_RealReviewerVerdictIsUnchanged(t *testing.T) {
	t.Parallel()

	got := buildReviewerFeedbackContent(ReviewerFeedbackPayload{
		WorkspacePath:  "/tmp/wt",
		PriorIteration: 2,
		Verdict:        ReviewVerdictRequestChanges,
		Flags:          []string{"test-gap"},
		Notes:          "Add a test for the empty case.",
		DiffHash:       "abc123",
		DiffLines:      42,
	})

	// EM-015d-RFD's shape for a genuine reviewer verdict is unchanged.
	if !strings.HasPrefix(got, "# Reviewer feedback — iteration 2\n\n") {
		t.Fatalf("a real reviewer verdict lost its EM-015d-RFD heading:\n%s", got)
	}
	if strings.Contains(got, "NO REVIEWER READ YOUR CHANGE") {
		t.Fatalf("a real reviewer verdict carries the no-reviewer disclaimer:\n%s", got)
	}
	for _, want := range []string{"verdict: REQUEST_CHANGES", "- test-gap", "Add a test for the empty case.", "diff_hash: abc123", "diff_lines: 42"} {
		if !strings.Contains(got, want) {
			t.Errorf("reviewer feedback is missing %q:\n%s", want, got)
		}
	}
}

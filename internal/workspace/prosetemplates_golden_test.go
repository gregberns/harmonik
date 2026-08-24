package workspace

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/gregberns/harmonik/internal/handlercontract"
)

// updateProseGolden rewrites the checked-in golden files instead of comparing
// against them: `go test ./internal/workspace/ -run Prose -update-prose-golden`.
// Review the resulting diff — it is the diff of what every agent will read.
var updateProseGolden = flag.Bool("update-prose-golden", false,
	"rewrite internal/workspace/testdata/prose/* from the current templates")

// TestProseGolden pins the three markdown artifacts the daemon writes into a
// worktree — agent-task.md, reviewer-feedback.iter-N.md and review-target.md.
//
// What it buys: that prose is the product. It moved out of Go string literals
// and into templates/, where it is easy to edit and just as easy to change by
// accident — a stray blank line or a lost {{-}} shifts the text every agent
// reads and no other test in this package would notice. Six renders cover the
// conditional sections: worktree discipline, extra context, implementer
// guidance, all three phase blocks, both completion modes, both feedback
// headings, and both arms of the flags list.
//
// THE PAYLOAD SHAPE IS THE COVERAGE, because the failure mode is a trim marker.
// A {{-}} inside a branch is only rendered when that branch is taken, so a
// golden that never takes it pins nothing there. A whitespace-mutation sweep
// over every trim marker in templates/ found the holes this table now closes:
// four surviving mutants, all of them inside the non-empty arm of the flags
// list, which no payload here used to reach. Add a case whenever a template
// grows a branch, not whenever it grows a line.
func TestProseGolden(t *testing.T) {
	cases := []struct {
		golden  string
		content string
	}{
		{
			golden: "agent-task.implementer-resume.md",
			content: buildAgentTaskContent(AgentTaskPayload{
				BeadID:              "hk-abc12",
				Title:               "Shrink the file",
				Phase:               "implementer-resume",
				Iteration:           2,
				RunID:               "0198f000-0000-7000-8000-000000000001",
				WorkspacePath:       "/wt/alpha",
				Body:                "Do the thing.",
				PriorVerdictSummary: "REQUEST_CHANGES — address flagged issues",
				ExtraContext:        "Predecessor commit abc1234 has landed.",
				BaseBranch:          "work/integration",
				Completion:          handlercontract.CompletionEventStreamThenQuit,
			}),
		},
		{
			// The plain implementer phase, which is the most common artifact
			// the daemon writes: the Tests/commit guidance renders, and NEITHER
			// prior-iteration block does. Nothing else in this table takes the
			// no-branch arm of the phase chain, so nothing else would notice a
			// condition that starts matching an initial dispatch.
			golden: "agent-task.implementer-initial.md",
			content: buildAgentTaskContent(AgentTaskPayload{
				BeadID:        "hk-abc12",
				Title:         "Shrink the file",
				Phase:         "implementer-initial",
				Iteration:     1,
				RunID:         "0198f000-0000-7000-8000-000000000001",
				WorkspacePath: "/wt/alpha",
				Body:          "Do the thing.",
				Completion:    handlercontract.CompletionProcessExit,
			}),
		},
		{
			golden: "agent-task.reviewer.md",
			content: buildAgentTaskContent(AgentTaskPayload{
				BeadID:        "hk-abc12",
				Title:         "Shrink the file",
				Phase:         "reviewer",
				Iteration:     2,
				RunID:         "0198f000-0000-7000-8000-000000000001",
				WorkspacePath: "/wt/alpha",
				Body:          "Do the thing.",
				ReviewBaseSHA: "aaaa1111",
				ReviewHeadSHA: "bbbb2222",
				Completion:    handlercontract.CompletionProcessExit,
			}),
		},
		{
			golden: "reviewer-feedback.no-commit.md",
			content: buildReviewerFeedbackContent(ReviewerFeedbackPayload{
				WorkspacePath:  "/wt/alpha",
				PriorIteration: 1,
				Verdict:        "NO_COMMIT",
				Notes:          "HEAD did not advance.",
			}),
		},
		{
			// The reviewer arm of the feedback file. TWO flags, because the
			// trim markers that join the list live BETWEEN iterations of the
			// range and a one-element list renders the same with or without
			// them. Notes already ends in a newline, which is the ensureNL arm
			// no other case here takes.
			golden: "reviewer-feedback.request-changes.md",
			content: buildReviewerFeedbackContent(ReviewerFeedbackPayload{
				WorkspacePath:  "/wt/alpha",
				PriorIteration: 2,
				Verdict:        ReviewVerdictRequestChanges,
				Flags:          []string{"missing-tests", "spec-divergence"},
				Notes:          "The empty case is untested.\nName the spec clause you changed.\n",
			}),
		},
		{
			golden: "review-target.md",
			content: buildReviewTargetContent(ReviewTargetPayload{
				WorkspacePath: "/wt/alpha",
				BeadID:        "hk-abc12",
				Iteration:     2,
				BeadTitle:     "Shrink the file",
				BeadBody:      "Do the thing.",
				BaseSHA:       "aaaa1111",
				HeadSHA:       "bbbb2222",
			}),
		},
	}

	for _, tc := range cases {
		t.Run(tc.golden, func(t *testing.T) {
			path := filepath.Join("testdata", "prose", tc.golden)
			if *updateProseGolden {
				if err := os.WriteFile(path, []byte(tc.content), 0o600); err != nil {
					t.Fatalf("write golden %s: %v", path, err)
				}
				return
			}
			//nolint:gosec // G304: path is testdata/prose + a constant from the table above, not user input
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read golden %s: %v", path, err)
			}
			if tc.content != string(want) {
				t.Errorf("%s does not match the golden file.\n"+
					"The prose every agent reads has changed. If that is what you meant, run:\n"+
					"  go test ./internal/workspace/ -run Prose -update-prose-golden\n"+
					"and review the resulting diff.\n\n--- got ---\n%s", tc.golden, tc.content)
			}
		})
	}
}

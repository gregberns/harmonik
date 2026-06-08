package daemon

// standardgraph.go — embedded standard-bead.dot graph for the DOT-mode fallback.
//
// When a bead has no explicit workflow_ref AND no <projectDir>/workflow.dot exists,
// the daemon loads this embedded graph instead of failing (hk-30vlb).
//
// The embedded graph is identical to specs/examples/standard-bead.dot (the canonical
// source of truth). The copy here is the embed target; keep in sync with the spec file.
//
// Review-floor guarantee (hk-30vlb §REVIEW FLOOR):
//   (a) The embedded graph contains a reviewer node, so the DOT default is reviewed
//       by construction.
//   (b) If loading the embedded graph fails (parse or validation error), the pre-switch
//       block in workloop.go changes workflowMode to WorkflowModeReviewLoop before
//       entering the dispatch switch, so execution falls through to the review-loop
//       driver — NEVER to single.

import (
	_ "embed"

	"github.com/gregberns/harmonik/internal/workflow"
	"github.com/gregberns/harmonik/internal/workflow/dot"
)

//go:embed standard-bead.dot
var standardBeadDotSrc []byte

// loadStandardGraph parses and validates the embedded standard-bead.dot graph.
// params is forwarded to template substitution (no-op when nil or empty).
// Returns nil + error if the embedded graph fails to parse or validate.
func loadStandardGraph(params map[string]string) (*dot.Graph, error) {
	return workflow.LoadDotWorkflowFromBytes(standardBeadDotSrc, "embedded:standard-bead.dot", params)
}

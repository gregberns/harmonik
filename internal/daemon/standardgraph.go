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
	"fmt"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/workflow"
	"github.com/gregberns/harmonik/internal/workflow/dot"
)

//go:embed standard-bead.dot
var standardBeadDotSrc []byte

//go:embed no-review-bead.dot
var noReviewBeadDotSrc []byte

type embeddedGraphSource struct {
	sourceName string
	src        func() []byte
}

func mustEmbeddedWorkflowDescriptor(id, version string) core.WorkflowDescriptor {
	workflowID, err := core.NewWorkflowID(id)
	if err != nil {
		panic(err)
	}
	return core.WorkflowDescriptor{
		WorkflowID:      workflowID,
		WorkflowVersion: core.WorkflowVersion(version),
	}
}

var (
	standardBeadDescriptor = mustEmbeddedWorkflowDescriptor("standard-bead", "1.0")
	noReviewBeadDescriptor = mustEmbeddedWorkflowDescriptor("no-review-bead", "1.0")
	embeddedGraphRegistry  = map[core.WorkflowDescriptor]embeddedGraphSource{
		standardBeadDescriptor: {
			sourceName: "embedded:standard-bead.dot",
			src:        func() []byte { return standardBeadDotSrc },
		},
		noReviewBeadDescriptor: {
			sourceName: "embedded:no-review-bead.dot",
			src:        func() []byte { return noReviewBeadDotSrc },
		},
	}
)

func loadRegisteredEmbeddedGraph(descriptor core.WorkflowDescriptor, params map[string]string) (*dot.Graph, error) {
	entry, ok := embeddedGraphRegistry[descriptor]
	if !ok {
		return nil, fmt.Errorf("embedded workflow descriptor %s@%s is not registered", descriptor.WorkflowID, descriptor.WorkflowVersion)
	}
	return workflow.LoadDotWorkflowFromBytes(entry.src(), entry.sourceName, params)
}

// loadStandardGraph parses and validates the embedded standard-bead.dot graph.
// params is forwarded to template substitution (no-op when nil or empty).
// Returns nil + error if the embedded graph fails to parse or validate.
func loadStandardGraph(params map[string]string) (*dot.Graph, error) {
	return loadRegisteredEmbeddedGraph(standardBeadDescriptor, params)
}

// loadNoReviewGraph parses and validates the registered no-review graph.
func loadNoReviewGraph(params map[string]string) (*dot.Graph, error) {
	return loadRegisteredEmbeddedGraph(noReviewBeadDescriptor, params)
}

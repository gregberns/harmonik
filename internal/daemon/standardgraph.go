package daemon

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

func loadStandardGraph(params map[string]string) (*dot.Graph, error) {
	return loadRegisteredEmbeddedGraph(standardBeadDescriptor, params)
}

func loadNoReviewGraph(params map[string]string) (*dot.Graph, error) {
	return loadRegisteredEmbeddedGraph(noReviewBeadDescriptor, params)
}

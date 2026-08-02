package workflow

import (
	"errors"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/workflow/dot"
)

func TestSubstituteGraphParamsWorkflowID(t *testing.T) {
	g := &dot.Graph{WorkflowID: core.WorkflowID("__WORKFLOW_ID__")}
	if err := substituteGraphParams(g, map[string]string{"WORKFLOW_ID": "named-graph"}); err != nil {
		t.Fatalf("substituteGraphParams: %v", err)
	}
	if g.WorkflowID != core.WorkflowID("named-graph") {
		t.Errorf("WorkflowID = %q, want %q", g.WorkflowID, "named-graph")
	}
}

func TestSubstituteGraphParamsWorkflowIDResidualFailsWithoutParams(t *testing.T) {
	g := &dot.Graph{WorkflowID: core.WorkflowID("__WORKFLOW_ID__")}
	err := substituteGraphParams(g, nil)
	var residual *ErrResidualToken
	if !errors.As(err, &residual) {
		t.Fatalf("substituteGraphParams error = %v, want ErrResidualToken", err)
	}
}

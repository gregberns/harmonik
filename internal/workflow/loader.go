package workflow

// loader.go — DOT workflow loader for workflow_mode=dot.
//
// Provides LoadDotWorkflow: reads a .dot file, parses via dot.Parse, validates
// via dot.Validate, and returns the validated graph or a typed error the daemon
// can map to failure_class=workflow_load.
//
// Spec refs:
//   - specs/workflow-graph.md §10 WG-031/032 — parse policy.
//   - specs/workflow-graph.md §9 WG-024..028 — validation obligations.
//   - specs/execution-model.md §4.3 EM-012   — WorkflowModeDot dispatch.
//
// Bead ref: hk-waj4b (T-IMPL-004).
// Tags: mechanism

import (
	"fmt"
	"os"
	"strings"

	"github.com/gregberns/harmonik/internal/workflow/dot"
)

// ErrWorkflowLoad is the typed error returned by LoadDotWorkflow when the .dot
// artifact cannot be read, parsed, or validated. The daemon maps this to
// failure_class=workflow_load when emitting run_failed events.
type ErrWorkflowLoad struct {
	// Path is the filesystem path that was attempted.
	Path string
	// Reason describes the failure (read error, parse error, validation error).
	Reason string
}

func (e *ErrWorkflowLoad) Error() string {
	return fmt.Sprintf("workflow_load: %s: %s", e.Path, e.Reason)
}

// LoadDotWorkflow reads a .dot file at dotPath, parses it via dot.Parse,
// validates via dot.Validate, and returns the validated graph.
//
// On any failure (file read, parse, validation with SeverityError diagnostics),
// returns nil and an *ErrWorkflowLoad that the daemon can map to
// failure_class=workflow_load.
func LoadDotWorkflow(dotPath string) (*dot.Graph, error) {
	src, err := os.ReadFile(dotPath)
	if err != nil {
		return nil, &ErrWorkflowLoad{
			Path:   dotPath,
			Reason: fmt.Sprintf("read failed: %v", err),
		}
	}

	graph, parseErr := dot.Parse(string(src), dotPath)
	if parseErr != nil {
		return nil, &ErrWorkflowLoad{
			Path:   dotPath,
			Reason: fmt.Sprintf("parse failed: %v", parseErr),
		}
	}

	diags := dot.Validate(graph)
	var errs []string
	for _, d := range diags {
		if d.Severity == dot.SeverityError {
			errs = append(errs, d.String())
		}
	}
	if len(errs) > 0 {
		return nil, &ErrWorkflowLoad{
			Path:   dotPath,
			Reason: fmt.Sprintf("validation failed: %s", strings.Join(errs, "; ")),
		}
	}

	return graph, nil
}

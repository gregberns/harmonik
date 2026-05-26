package workflow_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gregberns/harmonik/internal/workflow"
)

func TestLoadDotWorkflow_Success(t *testing.T) {
	// Minimal valid .dot file per WG-033/035/027.
	src := `digraph test {
		schema_version="1";
		version="1.0";
		start_node="impl";
		terminal_node_ids="done";

		impl [type="agentic"; agent_type="claude-code"; handler_ref="builtin:claude-code"; idempotency_class="non-idempotent"];
		done [type="non-agentic"; handler_ref="builtin:noop"; idempotency_class="idempotent"];

		impl -> done;
	}`
	dir := t.TempDir()
	dotPath := filepath.Join(dir, "workflow.dot")
	if err := os.WriteFile(dotPath, []byte(src), 0o644); err != nil {
		t.Fatalf("write temp dot file: %v", err)
	}

	g, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if g == nil {
		t.Fatal("expected non-nil graph")
	}
	if g.StartNodeID != "impl" {
		t.Errorf("expected start_node=impl, got %q", g.StartNodeID)
	}
}

func TestLoadDotWorkflow_FileNotFound(t *testing.T) {
	_, err := workflow.LoadDotWorkflow("/nonexistent/path/workflow.dot")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	var wlErr *workflow.ErrWorkflowLoad
	if !isWorkflowLoadErr(err, &wlErr) {
		t.Fatalf("expected *ErrWorkflowLoad, got %T: %v", err, err)
	}
}

func TestLoadDotWorkflow_ParseError(t *testing.T) {
	dir := t.TempDir()
	dotPath := filepath.Join(dir, "bad.dot")
	if err := os.WriteFile(dotPath, []byte("not valid dot at all {{{"), 0o644); err != nil {
		t.Fatalf("write temp dot file: %v", err)
	}

	_, err := workflow.LoadDotWorkflow(dotPath)
	if err == nil {
		t.Fatal("expected parse error")
	}
	var wlErr *workflow.ErrWorkflowLoad
	if !isWorkflowLoadErr(err, &wlErr) {
		t.Fatalf("expected *ErrWorkflowLoad, got %T: %v", err, err)
	}
}

func TestLoadDotWorkflow_ValidationError(t *testing.T) {
	// Parseable but invalid: missing start_node, terminal_node_ids, version.
	src := `digraph bad {
		schema_version="1";
		impl [type="agentic"; agent_type="claude-code"; handler_ref="builtin:claude-code"; idempotency_class="non-idempotent"];
	}`
	dir := t.TempDir()
	dotPath := filepath.Join(dir, "invalid.dot")
	if err := os.WriteFile(dotPath, []byte(src), 0o644); err != nil {
		t.Fatalf("write temp dot file: %v", err)
	}

	_, err := workflow.LoadDotWorkflow(dotPath)
	if err == nil {
		t.Fatal("expected validation error")
	}
	var wlErr *workflow.ErrWorkflowLoad
	if !isWorkflowLoadErr(err, &wlErr) {
		t.Fatalf("expected *ErrWorkflowLoad, got %T: %v", err, err)
	}
}

// isWorkflowLoadErr is a helper that uses errors.As semantics via type assertion.
func isWorkflowLoadErr(err error, target **workflow.ErrWorkflowLoad) bool {
	if e, ok := err.(*workflow.ErrWorkflowLoad); ok {
		*target = e
		return true
	}
	return false
}

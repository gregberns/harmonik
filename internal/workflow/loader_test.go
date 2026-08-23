package workflow_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/workflow"
)

func TestLoadDotWorkflow_Success(t *testing.T) {
	src := `digraph test {
		schema_version="1";
		version="1.0";
		workflow_id="loader-success";
		start_node="impl";
		terminal_node_ids="done";

		impl [type="agentic"; agent_type="claude-code"; handler_ref="builtin:claude-code"; idempotency_class="non-idempotent"];
		done [type="non-agentic"; handler_ref="builtin:noop"; idempotency_class="idempotent"];

		impl -> done;
	}`
	dir := t.TempDir()
	dotPath := filepath.Join(dir, "workflow.dot")
	if err := os.WriteFile(dotPath, []byte(src), 0o600); err != nil {
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
	if !errors.As(err, &wlErr) {
		t.Fatalf("expected *ErrWorkflowLoad, got %T: %v", err, err)
	}
}

func TestLoadDotWorkflow_ParseError(t *testing.T) {
	dir := t.TempDir()
	dotPath := filepath.Join(dir, "bad.dot")
	if err := os.WriteFile(dotPath, []byte("not valid dot at all {{{"), 0o600); err != nil {
		t.Fatalf("write temp dot file: %v", err)
	}

	_, err := workflow.LoadDotWorkflow(dotPath)
	if err == nil {
		t.Fatal("expected parse error")
	}
	var wlErr *workflow.ErrWorkflowLoad
	if !errors.As(err, &wlErr) {
		t.Fatalf("expected *ErrWorkflowLoad, got %T: %v", err, err)
	}
}

func TestLoadDotWorkflow_ValidationError(t *testing.T) {
	src := `digraph bad {
		schema_version="1";
		workflow_id="loader-invalid";
		impl [type="agentic"; agent_type="claude-code"; handler_ref="builtin:claude-code"; idempotency_class="non-idempotent"];
	}`
	dir := t.TempDir()
	dotPath := filepath.Join(dir, "invalid.dot")
	if err := os.WriteFile(dotPath, []byte(src), 0o600); err != nil {
		t.Fatalf("write temp dot file: %v", err)
	}

	_, err := workflow.LoadDotWorkflow(dotPath)
	if err == nil {
		t.Fatal("expected validation error")
	}
	var wlErr *workflow.ErrWorkflowLoad
	if !errors.As(err, &wlErr) {
		t.Fatalf("expected *ErrWorkflowLoad, got %T: %v", err, err)
	}
}

func TestLoadDotWorkflowWithParamsSubstitutesWorkflowID(t *testing.T) {
	src := `digraph test {
		schema_version="1";
		version="1.0";
		workflow_id="__WORKFLOW_ID__";
		start_node="done";
		terminal_node_ids="done";
		done [type="non-agentic"; handler_ref="builtin:noop"; idempotency_class="idempotent"];
	}`
	dotPath := filepath.Join(t.TempDir(), "workflow.dot")
	if err := os.WriteFile(dotPath, []byte(src), 0o600); err != nil {
		t.Fatalf("write temp dot file: %v", err)
	}

	g, err := workflow.LoadDotWorkflowWithParams(dotPath, map[string]string{"WORKFLOW_ID": "substituted-graph"})
	if err != nil {
		t.Fatalf("LoadDotWorkflowWithParams: %v", err)
	}
	if g.WorkflowID != core.WorkflowID("substituted-graph") {
		t.Errorf("WorkflowID = %q, want %q", g.WorkflowID, "substituted-graph")
	}
}

func TestLoadDotWorkflowWithParamsRejectsInvalidWorkflowID(t *testing.T) {
	src := `digraph test {
		schema_version="1";
		version="1.0";
		workflow_id="__WORKFLOW_ID__";
		start_node="done";
		terminal_node_ids="done";
		done [type="non-agentic"; handler_ref="builtin:noop"; idempotency_class="idempotent"];
	}`
	dotPath := filepath.Join(t.TempDir(), "workflow.dot")
	if err := os.WriteFile(dotPath, []byte(src), 0o600); err != nil {
		t.Fatalf("write temp dot file: %v", err)
	}

	_, err := workflow.LoadDotWorkflowWithParams(dotPath, map[string]string{"WORKFLOW_ID": "bad_id"})
	if err == nil {
		t.Fatal("LoadDotWorkflowWithParams accepted an invalid substituted workflow ID")
	}
	var loadErr *workflow.ErrWorkflowLoad
	if !errors.As(err, &loadErr) {
		t.Fatalf("error = %T %v, want *ErrWorkflowLoad", err, err)
	}
}

// TestLoadDotWorkflow_PolicyRefDeprecationWarning verifies (a): when a workflow
// declares a policy_ref attribute (deprecated per CP-056), LoadDotWorkflow prints
// a deprecation warning to stderr that cites CP-056 and names the typed
// replacement attributes per CP-055.
func TestLoadDotWorkflow_PolicyRefDeprecationWarning(t *testing.T) {
	src := `digraph test {
		schema_version="1";
		version="1.0";
		workflow_id="policy-warning";
		start_node="impl";
		terminal_node_ids="done";

		impl [type="agentic"; agent_type="claude-code"; handler_ref="builtin:claude-code";
		      idempotency_class="non-idempotent"; policy_ref="some-policy-set"];
		done [type="non-agentic"; handler_ref="builtin:noop"; idempotency_class="idempotent"];

		impl -> done;
	}`
	dir := t.TempDir()
	dotPath := filepath.Join(dir, "workflow.dot")
	if err := os.WriteFile(dotPath, []byte(src), 0o600); err != nil {
		t.Fatalf("write temp dot file: %v", err)
	}

	var loadErr error
	stderrOutput := loaderCaptureStderr(t, func() {
		_, loadErr = workflow.LoadDotWorkflow(dotPath)
	})

	if loadErr == nil {
		t.Fatal("expected load error for policy_ref, got nil")
	}

	if !strings.Contains(stderrOutput, "CP-056") {
		t.Errorf("expected CP-056 in stderr deprecation warning, got: %q", stderrOutput)
	}

	for _, replacement := range []string{"gate_ref", "skills_ref", "freedom_profile_ref"} {
		if strings.Contains(stderrOutput, replacement) {
			return // at least one named — passes
		}
	}
	t.Errorf("expected stderr warning to name a typed replacement attribute, got: %q", stderrOutput)
}

// TestLoadDotWorkflow_PolicyRefReturnsErrDeterministic verifies the CP-056 error-class
// mandate: the error returned for a policy_ref workflow MUST wrap
// handlercontract.ErrDeterministic (not ErrWorkflowLoad). The spec text:
// "Workflow-ingest MUST reject any DOT attribute named `policy_ref` with ErrDeterministic".
func TestLoadDotWorkflow_PolicyRefReturnsErrDeterministic(t *testing.T) {
	src := `digraph test {
		schema_version="1";
		version="1.0";
		workflow_id="policy-error";
		start_node="impl";
		terminal_node_ids="done";

		impl [type="agentic"; agent_type="claude-code"; handler_ref="builtin:claude-code";
		      idempotency_class="non-idempotent"; policy_ref="some-policy-set"];
		done [type="non-agentic"; handler_ref="builtin:noop"; idempotency_class="idempotent"];

		impl -> done;
	}`
	dir := t.TempDir()
	dotPath := filepath.Join(dir, "workflow.dot")
	if err := os.WriteFile(dotPath, []byte(src), 0o600); err != nil {
		t.Fatalf("write temp dot file: %v", err)
	}

	var loadErr error
	loaderCaptureStderr(t, func() {
		_, loadErr = workflow.LoadDotWorkflow(dotPath)
	})

	if loadErr == nil {
		t.Fatal("expected error for policy_ref workflow, got nil")
	}

	if !errors.Is(loadErr, handlercontract.ErrDeterministic) {
		t.Errorf("expected errors.Is(err, ErrDeterministic)=true for policy_ref rejection (CP-056), got %T: %v", loadErr, loadErr)
	}

	var wlErr *workflow.ErrWorkflowLoad
	if errors.As(loadErr, &wlErr) {
		t.Errorf("expected *ErrPolicyRefRejected (not *ErrWorkflowLoad) for policy_ref rejection, got *ErrWorkflowLoad")
	}
}

func minimalPolicy() *core.PolicyDocument {
	return &core.PolicyDocument{
		SkillSets: []core.PolicySkillSet{
			{Name: "base-tools", Skills: []string{"bash", "read"}},
		},
	}
}

// TestLoadDotWorkflowWithPolicy_SkillsRefResolved verifies (b): when a node
// declares skills_ref that matches a skill_sets[] entry in the policy, the loader
// returns a SkillsResolvedPayload for that node (CP-057).
func TestLoadDotWorkflowWithPolicy_SkillsRefResolved(t *testing.T) {
	src := `digraph test {
		schema_version="1";
		version="1.0";
		workflow_id="skills-resolved";
		start_node="impl";
		terminal_node_ids="done";

		impl [type="agentic"; agent_type="claude-code"; handler_ref="builtin:claude-code";
		      idempotency_class="non-idempotent"; skills_ref="base-tools"];
		done [type="non-agentic"; handler_ref="builtin:noop"; idempotency_class="idempotent"];

		impl -> done;
	}`
	dir := t.TempDir()
	dotPath := filepath.Join(dir, "workflow.dot")
	if err := os.WriteFile(dotPath, []byte(src), 0o600); err != nil {
		t.Fatalf("write temp dot file: %v", err)
	}

	policy := minimalPolicy()
	g, resolved, err := workflow.LoadDotWorkflowWithPolicy(dotPath, policy)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if g == nil {
		t.Fatal("expected non-nil graph")
	}
	if len(resolved) != 1 {
		t.Fatalf("expected 1 SkillsResolvedPayload, got %d", len(resolved))
	}
	p := resolved[0]
	if p.NodeID != "impl" {
		t.Errorf("expected NodeID=impl, got %q", p.NodeID)
	}
	if p.SkillsRef != "base-tools" {
		t.Errorf("expected SkillsRef=base-tools, got %q", p.SkillsRef)
	}
	if len(p.Skills) != 2 || p.Skills[0] != "bash" || p.Skills[1] != "read" {
		t.Errorf("expected Skills=[bash read], got %v", p.Skills)
	}
}

// TestLoadDotWorkflowWithPolicy_SkillsRefUnresolved verifies that an unresolvable
// skills_ref produces an *ErrWorkflowLoad (structural failure per CP-057 / WG-026).
func TestLoadDotWorkflowWithPolicy_SkillsRefUnresolved(t *testing.T) {
	src := `digraph test {
		schema_version="1";
		version="1.0";
		workflow_id="skills-unresolved";
		start_node="impl";
		terminal_node_ids="done";

		impl [type="agentic"; agent_type="claude-code"; handler_ref="builtin:claude-code";
		      idempotency_class="non-idempotent"; skills_ref="nonexistent-set"];
		done [type="non-agentic"; handler_ref="builtin:noop"; idempotency_class="idempotent"];

		impl -> done;
	}`
	dir := t.TempDir()
	dotPath := filepath.Join(dir, "workflow.dot")
	if err := os.WriteFile(dotPath, []byte(src), 0o600); err != nil {
		t.Fatalf("write temp dot file: %v", err)
	}

	policy := minimalPolicy()
	_, _, err := workflow.LoadDotWorkflowWithPolicy(dotPath, policy)
	if err == nil {
		t.Fatal("expected error for unresolved skills_ref, got nil")
	}
	var wlErr *workflow.ErrWorkflowLoad
	if !errors.As(err, &wlErr) {
		t.Fatalf("expected *ErrWorkflowLoad, got %T: %v", err, err)
	}
	if !strings.Contains(wlErr.Reason, "nonexistent-set") {
		t.Errorf("expected error to name the unresolved ref %q, got: %q", "nonexistent-set", wlErr.Reason)
	}
}

// TestLoadDotWorkflowWithPolicy_SkillsRefOptional verifies (c): skills_ref is
// OPTIONAL on every node type per CP-057. A workflow with nodes of all four types
// that declare no skills_ref must load successfully with zero resolved payloads.
func TestLoadDotWorkflowWithPolicy_SkillsRefOptional(t *testing.T) {
	src := `digraph test {
		schema_version="1";
		version="1.0";
		workflow_id="skills-optional";
		start_node="agent";
		terminal_node_ids="close";

		agent  [type="agentic"; agent_type="claude-code"; handler_ref="builtin:claude-code"; idempotency_class="non-idempotent"];
		worker [type="non-agentic"; handler_ref="builtin:lint"; idempotency_class="idempotent"];
		guard  [type="gate"; gate_ref="review-gate"; handler_ref="builtin:gate"];
		close  [type="non-agentic"; handler_ref="builtin:noop"; idempotency_class="idempotent"];

		agent -> worker;
		worker -> guard;
		guard -> close;
	}`
	dir := t.TempDir()
	dotPath := filepath.Join(dir, "workflow.dot")
	if err := os.WriteFile(dotPath, []byte(src), 0o600); err != nil {
		t.Fatalf("write temp dot file: %v", err)
	}

	policy := &core.PolicyDocument{
		Gates: []core.PolicyGate{{Name: "review-gate"}},
	}
	g, resolved, err := workflow.LoadDotWorkflowWithPolicy(dotPath, policy)
	if err != nil {
		t.Fatalf("expected success (skills_ref optional), got error: %v", err)
	}
	if g == nil {
		t.Fatal("expected non-nil graph")
	}
	if len(resolved) != 0 {
		t.Errorf("expected 0 resolved payloads (no skills_ref declared), got %d", len(resolved))
	}
}

// TestLoadDotWorkflowWithPolicy_SkillsRefOnGateNode verifies that a gate node
// MAY declare skills_ref per CP-057 ("optional on every node type including gate").
func TestLoadDotWorkflowWithPolicy_SkillsRefOnGateNode(t *testing.T) {
	src := `digraph test {
		schema_version="1";
		version="1.0";
		workflow_id="skills-gate";
		start_node="agent";
		terminal_node_ids="close";

		agent [type="agentic"; agent_type="claude-code"; handler_ref="builtin:claude-code"; idempotency_class="non-idempotent"];
		guard [type="gate"; gate_ref="review-gate"; handler_ref="builtin:gate"; skills_ref="base-tools"];
		close [type="non-agentic"; handler_ref="builtin:noop"; idempotency_class="idempotent"];

		agent -> guard;
		guard -> close;
	}`
	dir := t.TempDir()
	dotPath := filepath.Join(dir, "workflow.dot")
	if err := os.WriteFile(dotPath, []byte(src), 0o600); err != nil {
		t.Fatalf("write temp dot file: %v", err)
	}

	policy := &core.PolicyDocument{
		Gates:     []core.PolicyGate{{Name: "review-gate"}},
		SkillSets: []core.PolicySkillSet{{Name: "base-tools", Skills: []string{"bash"}}},
	}
	g, resolved, err := workflow.LoadDotWorkflowWithPolicy(dotPath, policy)
	if err != nil {
		t.Fatalf("gate node with skills_ref must be accepted (CP-057), got: %v", err)
	}
	if g == nil {
		t.Fatal("expected non-nil graph")
	}
	if len(resolved) != 1 || resolved[0].NodeID != "guard" {
		t.Errorf("expected skills_resolved for guard node, got: %v", resolved)
	}
}

func loaderCaptureStderr(t *testing.T, fn func()) string {
	t.Helper()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	orig := os.Stderr
	os.Stderr = w

	var buf bytes.Buffer
	drained := make(chan error, 1)
	go func() {
		_, copyErr := buf.ReadFrom(r)
		drained <- copyErr
	}()

	fn()

	os.Stderr = orig
	if closeErr := w.Close(); closeErr != nil {
		t.Errorf("loaderCaptureStderr: close pipe writer: %v", closeErr)
	}
	if copyErr := <-drained; copyErr != nil {
		t.Errorf("loaderCaptureStderr: read captured stderr: %v", copyErr)
	}
	if closeErr := r.Close(); closeErr != nil {
		t.Errorf("loaderCaptureStderr: close pipe reader: %v", closeErr)
	}
	return buf.String()
}

package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	tmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	"github.com/gregberns/harmonik/internal/workflow"
	"github.com/gregberns/harmonik/internal/workflow/dot"
)

func loadInjectionToolNode(t *testing.T, sid string) *dot.Node {
	t.Helper()
	src := `digraph inj {
  schema_version="1";
  version="1.0";
  workflow_id="tool-injection-fixture";
  start_node="run";
  terminal_node_ids="run";
  run [type="non-agentic", handler_ref="shell", idempotency_class="idempotent", tool_command="echo __SID__"];
}`
	dotPath := filepath.Join(t.TempDir(), "inj.dot")
	if err := os.WriteFile(dotPath, []byte(src), 0o644); err != nil {
		t.Fatalf("write dot: %v", err)
	}
	g, err := workflow.LoadDotWorkflowWithParams(dotPath, map[string]string{"SID": sid})
	if err != nil {
		t.Fatalf("LoadDotWorkflowWithParams: %v", err)
	}
	for _, n := range g.Nodes {
		if n.ToolCommand != "" {
			return n
		}
	}
	t.Fatal("no tool node in loaded graph")
	return nil
}

// TestDispatchDotToolNode_LocalInjection_NoExec drives the LOCAL /bin/sh -c sink.
func TestDispatchDotToolNode_LocalInjection_NoExec(t *testing.T) {
	sentinel := filepath.Join(t.TempDir(), "pwned")
	node := loadInjectionToolNode(t, "x; touch "+sentinel+" #")

	if !strings.Contains(node.ToolCommand, "'x; touch "+sentinel+" #'") {
		t.Fatalf("tool_command not shell-quoted: %q", node.ToolCommand)
	}

	outcome, err := dispatchDotToolNode(context.Background(), nil, core.RunID{}, nil, t.TempDir(), t.TempDir(), node, nil)
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if outcome.Status != core.OutcomeStatusSuccess {
		t.Fatalf("expected SUCCESS (literal echo), got %q (notes=%q)", outcome.Status, outcome.Notes)
	}
	if _, statErr := os.Stat(sentinel); statErr == nil {
		t.Fatalf("INJECTION: sentinel %s was created — local command injection NOT neutralized", sentinel)
	}
}

// TestDispatchDotToolNode_RemoteInjection_NoExec drives the REMOTE /bin/sh -lc sink
// through a RecordingRunner that execs locally (simulating the worker login shell).
func TestDispatchDotToolNode_RemoteInjection_NoExec(t *testing.T) {
	sentinel := filepath.Join(t.TempDir(), "pwned-remote")
	node := loadInjectionToolNode(t, "x; touch "+sentinel+" #")

	rr := &tmux.RecordingRunner{} // nil CmdFunc → exec.CommandContext directly
	outcome, err := dispatchDotToolNode(context.Background(), nil, core.RunID{}, rr, t.TempDir(), t.TempDir(), node, nil)
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if outcome.Status != core.OutcomeStatusSuccess {
		t.Fatalf("expected SUCCESS (literal echo), got %q (notes=%q)", outcome.Status, outcome.Notes)
	}
	if _, statErr := os.Stat(sentinel); statErr == nil {
		t.Fatalf("INJECTION: sentinel %s was created — remote command injection NOT neutralized", sentinel)
	}
	if len(rr.Calls) == 0 {
		t.Fatal("no Command call recorded")
	}
	script := rr.Calls[0].Args[len(rr.Calls[0].Args)-1]
	if !strings.Contains(script, "'x; touch "+sentinel+" #'") {
		t.Fatalf("remote script does not contain the shell-quoted value: %q", script)
	}
}

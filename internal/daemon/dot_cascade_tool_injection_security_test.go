package daemon

// dot_cascade_tool_injection_security_test.go — end-to-end command-injection
// repro for the tool-node shell sink (WG-045). These tests drive the REAL load
// path (workflow.LoadDotWorkflowWithParams) so the shell-quoting that closes the
// hole is exercised, then dispatch the loaded node through dispatchDotToolNode and
// assert that a sentinel side-effect file is NOT created.
//
// Pre-fix: the param value `x; touch <sentinel> #` was spliced raw into
// tool_command and the `; touch` fired. Post-fix: the value is one shell-quoted
// word and only the literal echo runs.
//
// Covers both sinks: LOCAL (/bin/sh -c, runner == nil) and REMOTE
// (/bin/sh -lc via a RecordingRunner that execs locally to simulate the worker).

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

// loadInjectionToolNode writes a single-node shell-tool .dot whose tool_command is
// `echo __SID__`, loads it with the supplied SID value through the production
// loader, and returns the (now shell-quoted) tool node.
func loadInjectionToolNode(t *testing.T, sid string) *dot.Node {
	t.Helper()
	src := `digraph inj {
  schema_version="1";
  version="1.0";
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

	outcome, err := dispatchDotToolNode(context.Background(), nil, core.RunID{}, nil, t.TempDir(), node, nil)
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
	outcome, err := dispatchDotToolNode(context.Background(), nil, core.RunID{}, rr, t.TempDir(), node, nil)
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if outcome.Status != core.OutcomeStatusSuccess {
		t.Fatalf("expected SUCCESS (literal echo), got %q (notes=%q)", outcome.Status, outcome.Notes)
	}
	if _, statErr := os.Stat(sentinel); statErr == nil {
		t.Fatalf("INJECTION: sentinel %s was created — remote command injection NOT neutralized", sentinel)
	}
	// The recorded worker script must carry the value only inside the single-quoted
	// span, never as an unquoted command separator.
	if len(rr.Calls) == 0 {
		t.Fatal("no Command call recorded")
	}
	script := rr.Calls[0].Args[len(rr.Calls[0].Args)-1]
	if !strings.Contains(script, "'x; touch "+sentinel+" #'") {
		t.Fatalf("remote script does not contain the shell-quoted value: %q", script)
	}
}

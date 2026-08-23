package daemon_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/workflow/dot"
)

// TestDotParser_AutoStatusTrue_Parsed verifies that auto_status="true" on an
// agentic node parses to node.AutoStatus=true with no errors (hk-oo4 WG-053).
func TestDotParser_AutoStatusTrue_Parsed(t *testing.T) {
	t.Parallel()

	src := `digraph {
  workflow_id = "auto-status-true"
  start_node = "n"
  terminal_node_ids = "n"
  n [type="agentic", agent_type="implementer", handler_ref="h",
     idempotency_class="non-idempotent", auto_status="true"]
}`

	g, err := dot.Parse(src, "")
	if err != nil {
		t.Fatalf("Parse: unexpected error: %v", err)
	}
	if len(g.Nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(g.Nodes))
	}
	if !g.Nodes[0].AutoStatus {
		t.Errorf("AutoStatus = false; want true when auto_status=\"true\"")
	}
	if len(g.Warnings) != 0 {
		t.Errorf("unexpected warnings: %v", g.Warnings)
	}
}

// TestDotParser_AutoStatusFalse_Parsed verifies that auto_status="false" on an
// agentic node parses to node.AutoStatus=false (explicit default, no errors).
func TestDotParser_AutoStatusFalse_Parsed(t *testing.T) {
	t.Parallel()

	src := `digraph {
  workflow_id = "auto-status-false"
  start_node = "n"
  terminal_node_ids = "n"
  n [type="agentic", agent_type="implementer", handler_ref="h",
     idempotency_class="non-idempotent", auto_status="false"]
}`

	g, err := dot.Parse(src, "")
	if err != nil {
		t.Fatalf("Parse: unexpected error: %v", err)
	}
	if len(g.Nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(g.Nodes))
	}
	if g.Nodes[0].AutoStatus {
		t.Errorf("AutoStatus = true; want false when auto_status=\"false\"")
	}
}

// TestDotParser_AutoStatus_WarnOnNonAgentic verifies that auto_status="true"
// on a non-agentic node is retained with a v1 WARNING and does NOT raise a
// strict error (WG-031 permissive-retain / WG-053).
func TestDotParser_AutoStatus_WarnOnNonAgentic(t *testing.T) {
	t.Parallel()

	src := `digraph {
  workflow_id = "auto-status-non-agentic"
  start_node = "n"
  terminal_node_ids = "n"
  n [type="non-agentic", handler_ref="noop", idempotency_class="idempotent",
     auto_status="true"]
}`

	g, err := dot.Parse(src, "")
	if err != nil {
		t.Fatalf("Parse: unexpected strict error: %v", err)
	}
	if len(g.Warnings) == 0 {
		t.Fatal("Warnings is empty; want at least one warning for auto_status on non-agentic node")
	}
	found := false
	for _, w := range g.Warnings {
		if strings.Contains(w.Message, "auto_status") && strings.Contains(w.Message, "agentic-only") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("no warning mentions auto_status + agentic-only; warnings: %v", g.Warnings)
	}
	if len(g.Nodes) != 1 || !g.Nodes[0].AutoStatus {
		t.Errorf("AutoStatus not retained on non-agentic node; node=%v", g.Nodes)
	}
}

// TestAutoStatusInspection_NoGoMod_Pass verifies that a directory without a
// go.mod passes inspection unconditionally (non-Go project skip, AR-006-clean).
func TestAutoStatusInspection_NoGoMod_Pass(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	_, pass := daemon.ExportedRunAutoStatusInspection(context.Background(), dir)
	if !pass {
		t.Error("runAutoStatusInspection: want pass=true when no go.mod present")
	}
}

// TestAutoStatusInspection_ValidGo_Pass verifies that a directory with a
// valid go.mod and valid Go code passes inspection (success path → SUCCESS).
func TestAutoStatusInspection_ValidGo_Pass(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	autoStatusWriteFile(t, dir, "go.mod", "module example.com/autostatustest\n\ngo 1.21\n")
	autoStatusWriteFile(t, dir, "main.go", "package main\n\nfunc main() {}\n")

	_, pass := daemon.ExportedRunAutoStatusInspection(context.Background(), dir)
	if !pass {
		t.Error("runAutoStatusInspection: want pass=true for valid Go code")
	}
}

// TestAutoStatusInspection_BrokenGo_Fail verifies that a directory with a
// go.mod and broken Go code fails inspection with FAIL+deterministic
// (FAIL-axis slice, hk-oo4).
//
// AR-006 assertion: this test exercises the deterministic-only code path.
// The only subprocess spawned is `go build ./...` (exit-code check).
// No LLM is invoked at any point — the test would fail in CI if it were.
func TestAutoStatusInspection_BrokenGo_Fail(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	autoStatusWriteFile(t, dir, "go.mod", "module example.com/autostatustest\n\ngo 1.21\n")
	autoStatusWriteFile(t, dir, "broken.go", "package main\n\nfunc main() { this is not valid go syntax }\n")

	outcome, pass := daemon.ExportedRunAutoStatusInspection(context.Background(), dir)
	if pass {
		t.Error("runAutoStatusInspection: want pass=false for broken Go code")
	}
	if outcome.Status != core.OutcomeStatusFail {
		t.Errorf("outcome.Status = %q; want %q", outcome.Status, core.OutcomeStatusFail)
	}
	if outcome.FailureClass == nil {
		t.Fatal("outcome.FailureClass = nil; want non-nil on FAIL outcome")
	}
	if *outcome.FailureClass != core.FailureClassDeterministic {
		t.Errorf("outcome.FailureClass = %q; want %q", *outcome.FailureClass, core.FailureClassDeterministic)
	}
	if !outcome.Valid() {
		t.Errorf("outcome.Valid() = false; FAIL+deterministic outcome must be structurally valid")
	}
}

func autoStatusWriteFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("autoStatusWriteFile %s: %v", path, err)
	}
}

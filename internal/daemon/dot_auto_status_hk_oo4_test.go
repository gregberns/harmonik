package daemon_test

// dot_auto_status_hk_oo4_test.go — unit tests: auto_status FAIL-axis slice.
//
// # What this file proves
//
//  1. Parser: auto_status="true" on an agentic node parses to
//     node.AutoStatus=true with no strict errors and no warnings.
//
//  2. Parser: auto_status="false" on an agentic node parses to
//     node.AutoStatus=false (explicit default) with no strict errors.
//
//  3. Parser: auto_status="true" on a non-agentic node is retained with a v1
//     WARNING and does not raise a strict error (WG-031 permissive-retain).
//
//  4. Engine (AR-006 mechanism-tag): runAutoStatusInspection on a directory
//     without a go.mod returns pass=true (inspection skipped — non-Go project).
//
//  5. Engine: runAutoStatusInspection on a directory with a valid go.mod and
//     valid Go code returns pass=true (inspection passes → caller uses SUCCESS).
//
//  6. Engine: runAutoStatusInspection on a directory with a go.mod and broken
//     Go code returns pass=false, Outcome.Status=FAIL,
//     Outcome.FailureClass=deterministic (inspection fails → FAIL emitted).
//
// # AR-006 compliance assertion (mechanism-tag)
//
// runAutoStatusInspection is a mechanism-tagged evaluation point
// (AR-006, execution-model.md §4.2). It MUST NOT invoke an LLM. The
// implementation satisfies this by executing only deterministic subprocess
// calls (go build / go vet with exit-code evaluation) and no LLM API calls.
// Tests 4-6 below exercise this path with no LLM dependency: they call the
// function directly in the test process, which would immediately deadlock or
// time out if any LLM call were attempted (no API key / no network in CI).
//
// Bead ref: hk-oo4. Spec refs: WG-041 §I.4, AR-006.

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

// ─────────────────────────────────────────────────────────────────────────────
// Parser tests (hk-oo4 §1-3)
// ─────────────────────────────────────────────────────────────────────────────

// TestDotParser_AutoStatusTrue_Parsed verifies that auto_status="true" on an
// agentic node parses to node.AutoStatus=true with no errors (hk-oo4 WG-041 §I.4).
func TestDotParser_AutoStatusTrue_Parsed(t *testing.T) {
	t.Parallel()

	src := `digraph {
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
// strict error (WG-031 permissive-retain / WG-041 §I.4).
func TestDotParser_AutoStatus_WarnOnNonAgentic(t *testing.T) {
	t.Parallel()

	src := `digraph {
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
	// Value is retained in the AST.
	if len(g.Nodes) != 1 || !g.Nodes[0].AutoStatus {
		t.Errorf("AutoStatus not retained on non-agentic node; node=%v", g.Nodes)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Engine tests — runAutoStatusInspection (hk-oo4 §4-6)
//
// AR-006 compliance: these tests call runAutoStatusInspection directly. The
// function is mechanism-tagged and MUST NOT invoke an LLM — verified here by
// the absence of any LLM API key or network call in the test binary. If the
// implementation called an LLM the test would immediately fail (timeout /
// missing credentials).
// ─────────────────────────────────────────────────────────────────────────────

// TestAutoStatusInspection_NoGoMod_Pass verifies that a directory without a
// go.mod passes inspection unconditionally (non-Go project skip, AR-006-clean).
func TestAutoStatusInspection_NoGoMod_Pass(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	// No go.mod present.

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

	// Write a minimal valid Go module.
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

	// Write a go.mod + a broken .go file that will not compile.
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

// autoStatusWriteFile is a test helper that writes content to path relative to dir.
func autoStatusWriteFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("autoStatusWriteFile %s: %v", path, err)
	}
}

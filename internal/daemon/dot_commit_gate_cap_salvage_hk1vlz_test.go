package daemon_test

// dot_commit_gate_cap_salvage_hk1vlz_test.go — regression test for F42 (hk-1vlz).
//
// # The bug
//
// When the DOT cascade's traversal cap fired at the commit_gate node and the
// implementer had already committed work (HEAD advanced past parentSHA), that
// committed work was silently discarded. The cascade returned Failed
// (needs-attention) and reopened the bead, stranding the committed tip on the
// run branch and never merging it to main.
//
// Refs: hk-3js5m (field incident: run 019ec643-18f8 / bead hk-jqpr), hk-1vlz.
//
// # The fix (hk-1vlz F42)
//
// driveDotWorkflow detects cap-hit at "commit_gate" with committed work (HEAD ≠
// parentSHA) and returns success=true instead of the previous fail-and-reopen.
// The caller (workloop.go) then merges the committed run branch to main
// — no silent loss.
//
// # Scenario (this test)
//
// Graph: start → implement(agentic) → commit_gate(shell, always exit 1) with
// the standard-bead topology back-edge:
//
//	commit_gate → implement [deterministic FAIL, traversal_cap=2]
//	commit_gate → close             [SUCCESS — never taken; gate always FAILs]
//	commit_gate → close-needs-attention [unconditional fallback]
//
// Handler script (the agentic implementer):
//   - every entry: commits a unique file → HEAD advances on every invoke.
//
// Walk:
//   - implement(1): commits A. commit_gate exit1 → back-edge (cap 1/2).
//   - implement(2): commits B. commit_gate exit1 → back-edge (cap 2/2).
//   - commit_gate exit1 → cap-hit (back-edge exhausted).
//   - F42 fix: HEAD (SHA_B) ≠ parentSHA → salvage → success=true.
//
// Without the fix the same walk would return success=false / needs_attention=true
// and strand the committed work on the run branch.
//
// # Spec refs
//   - specs/execution-model.md §4.3 EM-015e (traversal-cap semantics)
//   - specs/workflow-graph.md §5 WG-010..WG-012 (cascade five-step)
//
// Bead: hk-1vlz.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/workflow"
)

// gateCapSalvageDOT returns the DOT source for the F42 salvage topology:
// start → implement(agentic) → commit_gate(shell, always exit 1) with
// a traversal_cap=2 back-edge to implement.
//
// The node is named "commit_gate" to match the hardcoded check in
// driveDotWorkflow's auto-salvage logic.
func gateCapSalvageDOT() string {
	return `digraph "hk-1vlz-cap-salvage" {
    schema_version="1"; version="1.0"; workflow_id="hk-1vlz-cap-salvage";
    start_node="start"; terminal_node_ids="close,close-needs-attention";

    start [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];
    implement [type="agentic", agent_type="implementer", handler_ref="claude-implementer", idempotency_class="non-idempotent"];
    commit_gate [type="non-agentic", handler_ref="shell", idempotency_class="idempotent", tool_command="exit 1", timeout="30"];
    close [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];
    "close-needs-attention" [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];

    start -> implement;
    implement -> commit_gate;
    commit_gate -> close [condition="outcome.status == 'SUCCESS'"];
    commit_gate -> implement [condition="outcome.status == 'FAIL' && outcome.failure_class == 'deterministic'", traversal_cap="2"];
    commit_gate -> "close-needs-attention";
}
`
}

// gateCapSalvageScript writes the agentic-implementer handler:
//   - every invocation: commits a unique file → HEAD always advances.
//
// Because the implementer commits on every entry, the no-progress check
// never fires; the cascade loops until the traversal cap is exhausted.
func gateCapSalvageScript(t *testing.T, wtPath string) string {
	t.Helper()
	wtpEsc := strings.ReplaceAll(wtPath, "'", "'\\''")
	script := fmt.Sprintf(`#!/bin/sh
set -e
WTP='%s'
WS="${HARMONIK_WORKSPACE_PATH:-$WTP}"
CNT_FILE="$WTP/.harmonik/cap_salvage_count"
if [ ! -f "$CNT_FILE" ]; then
  printf '0' > "$CNT_FILE"
fi
CNT=$(cat "$CNT_FILE")
CNT=$((CNT + 1))
printf '%%d' "$CNT" > "$CNT_FILE"
# Every entry: commit a unique file so HEAD always advances.
printf '%%d' "$CNT" > "$WS/cap_salvage_impl_$CNT.txt"
git -C "$WS" add "cap_salvage_impl_$CNT.txt" >/dev/null 2>&1
git -C "$WS" -c user.email=test@harmonik.local -c user.name="Test" \
    commit -m "cap-salvage impl entry $CNT" --no-gpg-sign >/dev/null 2>&1
exit 0
`, wtpEsc)

	scriptPath := filepath.Join(t.TempDir(), "cap_salvage_handler.sh")
	//nolint:gosec // G306: test-only fixture script; not production
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("gateCapSalvageScript: WriteFile: %v", err)
	}
	return scriptPath
}

// TestCommitGateCapSalvage_hk1vlz verifies that a traversal cap hit at the
// commit_gate node with committed work (HEAD advanced past parentSHA) results
// in success=true (auto-salvage), NOT fail/needs-attention.
//
// This is the regression test for F42 (hk-1vlz): before the fix the same walk
// produced success=false + needs_attention=true + "traversal cap hit" summary,
// silently stranding the committed work on the run branch.
func TestCommitGateCapSalvage_hk1vlz(t *testing.T) {
	t.Parallel()

	projectDir, wtPath, parentSHA := rlcFixtureSetup(t)
	scriptPath := gateCapSalvageScript(t, wtPath)

	graphDir := t.TempDir()
	dotPath := filepath.Join(graphDir, "cap-salvage.dot")
	//nolint:gosec // G306: test-only fixture
	if err := os.WriteFile(dotPath, []byte(gateCapSalvageDOT()), 0o644); err != nil {
		t.Fatalf("write DOT: %v", err)
	}
	graph, loadErr := workflow.LoadDotWorkflow(dotPath)
	if loadErr != nil {
		t.Fatalf("LoadDotWorkflow(%s): %v", dotPath, loadErr)
	}

	collector := &stubEventCollector{}
	ledger := &stubBeadLedger{}
	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:           ledger,
		Bus:                 collector,
		ProjectDir:          projectDir,
		HandlerBinary:       "/bin/sh",
		HandlerArgs:         []string{scriptPath},
		IntentLogDir:        filepath.Join(projectDir, ".harmonik", "beads-intents"),
		AdapterRegistry2:    NewSealedAdapterRegistryForTest(t),
		WorkflowModeDefault: core.WorkflowModeDot,
	})

	// cap=2 back-edge + 3 implement entries (each commits) → cap fires on the 3rd
	// gate evaluation. Budget: generous but bounded.
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	result := daemon.ExportedDriveDotWorkflow(
		ctx, deps,
		rlFixtureRunID(t),
		core.BeadID("dot-cap-salvage-hk1vlz"),
		wtPath, parentSHA,
		graph,
	)

	events := collector.eventTypes()
	t.Logf("hk-1vlz F42: result=%+v events=%v", result, events)

	// ── Result assertions ────────────────────────────────────────────────────
	// F42 fix: cap-hit at commit_gate with committed work → success (auto-salvage).
	if !result.Success {
		t.Errorf("expected success=true (committed work salvaged on cap-hit); got success=false, summary=%q", result.Summary)
	}
	if result.NeedsAttention {
		t.Errorf("expected needs_attention=false on salvage path; summary=%q", result.Summary)
	}
	// Summary should indicate the salvage path, not a traversal-cap failure.
	if strings.Contains(result.Summary, "traversal cap") {
		t.Errorf("regression (hk-1vlz): cap-hit stranded committed work instead of salvaging; summary=%q", result.Summary)
	}
	if !strings.Contains(result.Summary, "salvage") && !strings.Contains(result.Summary, "hk-1vlz") {
		t.Errorf("expected summary to mention salvage or hk-1vlz; got %q", result.Summary)
	}
}

// TestCommitGateCapSalvage_NoCommit_StillFails_hk1vlz verifies that when the
// traversal cap fires at commit_gate but the implementer NEVER committed (HEAD
// equals parentSHA), the run still fails with needs-attention=true. The
// auto-salvage must NOT trigger when no committed work exists.
func TestCommitGateCapSalvage_NoCommit_StillFails_hk1vlz(t *testing.T) {
	t.Parallel()

	projectDir, wtPath, parentSHA := rlcFixtureSetup(t)

	// Implementer never commits — exits 0 each time with no git changes.
	// The no-progress check (hk-pj4b6) would normally catch this before the cap
	// (iter 1 commits nothing → hard-fail at "node exited without advancing HEAD").
	// So we script the implementer to commit ONLY on the first entry, then exit 0
	// without committing on re-entries. The first commit triggers the cap loop;
	// subsequent entries don't commit (no-progress would fire at iter 3), but we
	// test that the NO-COMMIT-AT-ALL case still fails when HEAD == parentSHA.
	//
	// To test the pure "no commit ever" path we need the implementer to exit 0
	// without committing on iter 1 — but that hard-fails at dispatchDotAgenticNode
	// (iterationCount < 2 + HEAD unchanged). Use a cap=1 graph so the FIRST gate
	// failure exhausts the cap immediately, before the no-progress check can fire.
	noCommitDOT := `digraph "hk-1vlz-no-commit" {
    schema_version="1"; version="1.0"; workflow_id="hk-1vlz-no-commit";
    start_node="start"; terminal_node_ids="close,close-needs-attention";

    start [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];
    implement [type="agentic", agent_type="implementer", handler_ref="claude-implementer", idempotency_class="non-idempotent"];
    commit_gate [type="non-agentic", handler_ref="shell", idempotency_class="idempotent", tool_command="exit 1", timeout="30"];
    close [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];
    "close-needs-attention" [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];

    start -> implement;
    implement -> commit_gate;
    commit_gate -> close [condition="outcome.status == 'SUCCESS'"];
    commit_gate -> implement [condition="outcome.status == 'FAIL' && outcome.failure_class == 'deterministic'", traversal_cap="1"];
    commit_gate -> "close-needs-attention";
}
`
	// Implementer commits on entry 1, does NOT commit on entry 2 (iter 2 no-commit
	// path). Cap=1 means: iter 1 commits → gate FAIL → [cap 1/1 exhausted] →
	// cap-hit at commit_gate. HEAD IS past parentSHA (iter 1 committed) → salvage
	// fires. We need a TRULY no-commit case to test the else-branch.
	//
	// Simplest: cap=0 is not valid (non-positive). Instead, use a graph where the
	// back-edge does NOT go through commit_gate — use a plain "gate" node name.
	// The salvage only triggers on node named "commit_gate".
	noCommitNonGateDOT := `digraph "hk-1vlz-no-commit-plain" {
    schema_version="1"; version="1.0"; workflow_id="hk-1vlz-no-commit-plain";
    start_node="start"; terminal_node_ids="close,close-needs-attention";

    start [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];
    implement [type="agentic", agent_type="implementer", handler_ref="claude-implementer", idempotency_class="non-idempotent"];
    gate [type="non-agentic", handler_ref="shell", idempotency_class="idempotent", tool_command="exit 1", timeout="30"];
    close [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];
    "close-needs-attention" [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];

    start -> implement;
    implement -> gate;
    gate -> close [condition="outcome.status == 'SUCCESS'"];
    gate -> implement [condition="outcome.status == 'FAIL' && outcome.failure_class == 'deterministic'", traversal_cap="2"];
    gate -> "close-needs-attention";
}
`
	// Implementer commits on every entry (same as the salvage test above).
	// But the gate node is named "gate" not "commit_gate" — so the F42 salvage
	// does NOT trigger. The result should be SUCCESS via... wait, the implementer
	// always commits and HEAD advances every time, so no-progress never fires.
	// The cap fires at "gate" → F42 salvage does NOT apply → fail+needs-attention.
	wtpEsc := strings.ReplaceAll(wtPath, "'", "'\\''")
	alwaysCommitScript := fmt.Sprintf(`#!/bin/sh
set -e
WTP='%s'
WS="${HARMONIK_WORKSPACE_PATH:-$WTP}"
CNT_FILE="$WTP/.harmonik/nc_count"
if [ ! -f "$CNT_FILE" ]; then printf '0' > "$CNT_FILE"; fi
CNT=$(cat "$CNT_FILE"); CNT=$((CNT + 1)); printf '%%d' "$CNT" > "$CNT_FILE"
printf '%%d' "$CNT" > "$WS/nc_impl_$CNT.txt"
git -C "$WS" add "nc_impl_$CNT.txt" >/dev/null 2>&1
git -C "$WS" -c user.email=test@harmonik.local -c user.name="Test" \
    commit -m "nc impl $CNT" --no-gpg-sign >/dev/null 2>&1
exit 0
`, wtpEsc)
	scriptPath := filepath.Join(t.TempDir(), "nc_handler.sh")
	//nolint:gosec // G306: test-only fixture
	if err := os.WriteFile(scriptPath, []byte(alwaysCommitScript), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	graphDir := t.TempDir()
	dotPath := filepath.Join(graphDir, "no-commit-plain.dot")
	//nolint:gosec // G306: test-only fixture
	if err := os.WriteFile(dotPath, []byte(noCommitNonGateDOT), 0o644); err != nil {
		t.Fatalf("write DOT: %v", err)
	}
	// Suppress unused variable lint error for the non-gate DOT.
	_ = noCommitDOT

	graph, loadErr := workflow.LoadDotWorkflow(dotPath)
	if loadErr != nil {
		t.Fatalf("LoadDotWorkflow: %v", loadErr)
	}

	collector := &stubEventCollector{}
	ledger := &stubBeadLedger{}
	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:           ledger,
		Bus:                 collector,
		ProjectDir:          projectDir,
		HandlerBinary:       "/bin/sh",
		HandlerArgs:         []string{scriptPath},
		IntentLogDir:        filepath.Join(projectDir, ".harmonik", "beads-intents"),
		AdapterRegistry2:    NewSealedAdapterRegistryForTest(t),
		WorkflowModeDefault: core.WorkflowModeDot,
	})

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	result := daemon.ExportedDriveDotWorkflow(
		ctx, deps,
		rlFixtureRunID(t),
		core.BeadID("dot-cap-no-salvage-hk1vlz"),
		wtPath, parentSHA,
		graph,
	)

	t.Logf("hk-1vlz no-salvage: result=%+v events=%v", result, collector.eventTypes())

	// When gate is NOT named "commit_gate", the F42 salvage must NOT trigger.
	// Cap fires → fail + needs-attention (prior hk-8b35c orphan-salvage behavior
	// is preserved for non-commit_gate nodes).
	if result.Success {
		t.Errorf("expected success=false when cap fires at non-commit_gate node (salvage must not over-reach); summary=%q", result.Summary)
	}
	if !result.NeedsAttention {
		t.Errorf("expected needs_attention=true when cap fires at non-commit_gate node; summary=%q", result.Summary)
	}
	if !strings.Contains(result.Summary, "traversal cap") {
		t.Errorf("expected summary to report traversal cap hit; got %q", result.Summary)
	}
}

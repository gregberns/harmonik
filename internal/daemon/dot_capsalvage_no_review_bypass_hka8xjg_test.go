package daemon_test

// dot_capsalvage_no_review_bypass_hka8xjg_test.go — regression test for hk-a8xjg.
//
// # The bug
//
// The F42 cap-hit auto-salvage path (hk-1vlz) returned success=true whenever
// the traversal cap fired at commit_gate AND the implementer had committed work
// (HEAD != parentSHA), with NO check that a reviewer node existed and was
// visited. Graphs that contain a downstream reviewer node would get their last
// implementer commit merged to main UNREVIEWED.
//
// Three production runs confirmed this: hk-aev8t (run 019ef080), hk-t48rg
// (run 019ef108), hk-3pbox (run 019ef117) — all reached run_completed with
// zero reviewer node visits, merged by the salvage path.
//
// # The fix (hk-a8xjg)
//
// driveDotWorkflow's F42 gate now additionally checks !graphHasReviewerNode:
//
//	if decision.CompletionReason=="cap_hit" && currentNodeID=="commit_gate" &&
//	   !graphHasReviewerNode(nodesByID) { …salvage… }
//
// When the graph contains a reviewer node the cap-hit falls through to the
// existing needs-attention reopen path — triage, not auto-merge.
//
// # Scenario (this test)
//
// Graph: start → implement → commit_gate → review (SUCCESS)
//
//	commit_gate → implement [FAIL, traversal_cap=2]
//	commit_gate → close-needs-attention [unconditional fallback]
//	review → close [unconditional]
//
// Handler script: every entry commits a unique file → HEAD always advances.
// commit_gate shell: always exits 1.
//
// Walk:
//   - implement(1): commits A. commit_gate exit1 → back-edge (cap 1/2).
//   - implement(2): commits B. commit_gate exit1 → back-edge (cap 2/2).
//   - commit_gate exit1 → cap-hit. Review node PRESENT but NEVER visited.
//
// Assert (FAILS before fix; GREEN after): success=false, needsAttention=true.
// The run must NOT auto-merge when a reviewer exists but was never reached.
//
// Bead: hk-a8xjg.

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

// reviewBypassDOT returns a DOT graph that contains a downstream reviewer node
// reachable on the commit_gate SUCCESS path. The implementer loops back from
// commit_gate on FAIL (cap=2); the review node is never reached because the
// cap fires at commit_gate first.
func reviewBypassDOT() string {
	return `digraph "hk-a8xjg-review-bypass" {
    schema_version="1"; version="1.0"; workflow_id="hk-a8xjg-review-bypass";
    start_node="start"; terminal_node_ids="close,close-needs-attention";

    start [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];
    implement [type="agentic", agent_type="implementer", handler_ref="claude-implementer", idempotency_class="non-idempotent"];
    commit_gate [type="non-agentic", handler_ref="shell", idempotency_class="idempotent", tool_command="exit 1", timeout="30"];
    review [type="agentic", agent_type="reviewer", handler_ref="claude-reviewer", idempotency_class="non-idempotent"];
    close [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];
    "close-needs-attention" [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];

    start -> implement;
    implement -> commit_gate;
    commit_gate -> review [condition="outcome.status == 'SUCCESS'"];
    commit_gate -> implement [condition="outcome.status == 'FAIL' && outcome.failure_class == 'deterministic'", traversal_cap="2"];
    commit_gate -> "close-needs-attention";
    review -> close;
}
`
}

// reviewBypassScript writes the agentic-implementer handler: every invocation
// commits a unique file so HEAD always advances.
func reviewBypassScript(t *testing.T, wtPath string) string {
	t.Helper()
	wtpEsc := strings.ReplaceAll(wtPath, "'", "'\\''")
	script := fmt.Sprintf(`#!/bin/sh
set -e
WTP='%s'
WS="${HARMONIK_WORKSPACE_PATH:-$WTP}"
CNT_FILE="$WTP/.harmonik/rb_count"
if [ ! -f "$CNT_FILE" ]; then
  printf '0' > "$CNT_FILE"
fi
CNT=$(cat "$CNT_FILE")
CNT=$((CNT + 1))
printf '%%d' "$CNT" > "$CNT_FILE"
printf '%%d' "$CNT" > "$WS/rb_impl_$CNT.txt"
git -C "$WS" add "rb_impl_$CNT.txt" >/dev/null 2>&1
git -C "$WS" -c user.email=test@harmonik.local -c user.name="Test" \
    commit -m "review-bypass impl entry $CNT" --no-gpg-sign >/dev/null 2>&1
exit 0
`, wtpEsc)

	scriptPath := filepath.Join(t.TempDir(), "review_bypass_handler.sh")
	//nolint:gosec // G306: test-only fixture script; not production
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("reviewBypassScript: WriteFile: %v", err)
	}
	return scriptPath
}

// TestCommitGateCapSalvage_DoesNotBypassReview_hka8xjg asserts that the F42
// auto-salvage does NOT fire when the graph contains a reviewer node that was
// never visited. The cap-hit must route to needs-attention (reopen), not merge.
func TestCommitGateCapSalvage_DoesNotBypassReview_hka8xjg(t *testing.T) {
	t.Parallel()

	projectDir, wtPath, parentSHA := rlcFixtureSetup(t)
	scriptPath := reviewBypassScript(t, wtPath)

	graphDir := t.TempDir()
	dotPath := filepath.Join(graphDir, "review-bypass.dot")
	//nolint:gosec // G306: test-only fixture
	if err := os.WriteFile(dotPath, []byte(reviewBypassDOT()), 0o644); err != nil {
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

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	result := daemon.ExportedDriveDotWorkflow(
		ctx, deps,
		rlFixtureRunID(t),
		core.BeadID("dot-review-bypass-hka8xjg"),
		wtPath, parentSHA,
		graph,
	)

	events := collector.eventTypes()
	t.Logf("hk-a8xjg: result=%+v events=%v", result, events)

	// The graph has a reviewer node; cap-hit at commit_gate must NOT auto-merge.
	if result.Success {
		t.Errorf("F42 review-bypass (hk-a8xjg): expected success=false when reviewer node exists but was never visited; got success=true, summary=%q", result.Summary)
	}
	if !result.NeedsAttention {
		t.Errorf("expected needs_attention=true when reviewer exists but unvisited; summary=%q", result.Summary)
	}
	// Must NOT contain the salvage summary (that would indicate the bypass fired).
	if strings.Contains(result.Summary, "salvaged") || strings.Contains(result.Summary, "hk-1vlz F42") {
		t.Errorf("F42 salvage fired despite reviewer node in graph — review bypass NOT fixed; summary=%q", result.Summary)
	}
}

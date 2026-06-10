package daemon_test

// dot_approved_done_completes_hk8ps7q_test.go — regression test for hk-8ps7q.
//
// # The bug
//
// The DOT-cascade no-progress detector (EM-015e) fires at iteration ≥ 2 whenever
// HEAD did not advance since the prior agentic-node entry. That check was
// VERDICT-BLIND: it could not tell a genuinely-stuck re-entry (prior reviewer
// said REQUEST_CHANGES but the implementer produced no new commit) from an
// approved-and-done re-entry (prior reviewer APPROVED, so there is legitimately
// nothing left for the next iteration to do and HEAD does not advance).
//
// In production (T12 / hk-xhawy) a bead committed valid iter-1 work, the reviewer
// APPROVED it, but a post-APPROVE re-entry into an agentic node (commit_gate
// fix-loop shape) hit the no-progress check at iter ≥ 2 with HEAD unchanged →
// run_failed "no-progress … HEAD did not advance" → the valid, APPROVED commit
// was STRANDED on the run branch (never merged). Every one-and-done bead whose
// graph loops back through an agentic node after APPROVE was at risk.
//
// # The fix (hk-8ps7q)
//
// The no-progress block now consults the most-recent reviewer verdict
// (priorVerdict). When the no-progress condition is met (iter ≥ 2 + HEAD
// unchanged) AND there is a committed result (HEAD is past the run baseline
// parentSHA) AND the prior verdict was APPROVE, the run COMPLETES (success) so
// the caller merges the approved work — instead of firing no_progress and
// stranding it. The genuinely-stuck case (prior verdict REQUEST_CHANGES, or no
// commit ever) still no_progress-fails — see the hk-togxq negative-guard and
// hk-5e9yj tests, which remain green.
//
// # Scenario
//
// Graph: start → implement → review, with review→implement on APPROVE (cap 3) so
// an APPROVE re-enters the agentic implementer node (mimicking a post-APPROVE
// agentic loop-back such as a commit_gate fix-loop). Walk:
//   1. implement (iter 1): commits a file → HEAD advances past parentSHA.
//   2. review    (iter 1): APPROVE → back-edge to implement.
//   3. implement (iter 2): exits 0 WITHOUT committing → HEAD unchanged.
//   (no invocation 4: the no-progress check fires before dispatch — but because
//    the prior verdict was APPROVE and committed work exists, it COMPLETES.)
//
// # Spec refs
//   - specs/execution-model.md §4.3 EM-015e (no-progress early-exit)
//   - specs/event-model.md §8.1a.5 (no_progress_detected; workflow_mode="dot")
//
// Bead: hk-8ps7q.

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
	"github.com/gregberns/harmonik/internal/workflow/dot"
)

// ps7qApproveLoopDOT returns a graph whose APPROVE verdict loops back through the
// agentic implementer node (rather than going straight to a terminal). This
// reproduces the post-APPROVE agentic re-entry that the production commit_gate
// fix-loop produces, so the no-progress check is exercised after an APPROVE.
func ps7qApproveLoopDOT() string {
	// Note: the APPROVE edge intentionally loops back through the agentic
	// implementer node (not to a terminal) so the no-progress check is exercised
	// AFTER an APPROVE — the production post-APPROVE agentic re-entry shape. The
	// success outcome comes from the hk-8ps7q short-circuit return, not from
	// reaching a success terminal, so the graph declares only the reachable
	// "close-needs-attention" terminal (WG-027 requires all declared terminals to
	// be reachable from start).
	return `digraph "hk-8ps7q-approve-loop" {
    schema_version="1"; version="1.0"; workflow_id="hk-8ps7q-approve-loop";
    start_node="start"; terminal_node_ids="close-needs-attention";

    start [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];
    implement [type="agentic", agent_type="implementer", handler_ref="claude-implementer", idempotency_class="non-idempotent"];
    review [type="agentic", agent_type="reviewer", handler_ref="claude-reviewer", idempotency_class="idempotent"];
    "close-needs-attention" [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];

    start -> implement;
    implement -> review;
    review -> implement [condition="outcome.preferred_label == 'APPROVE'", traversal_cap="3"];
    review -> "close-needs-attention" [condition="outcome.preferred_label == 'BLOCK'"];
    review -> "close-needs-attention";
}
`
}

// ps7qApprovedDoneScript: iter-1 implementer COMMITS (HEAD advances past parent),
// reviewer APPROVES, iter-2 implementer makes NO commit (HEAD unchanged). With
// the hk-8ps7q fix, the no-progress check sees prior verdict=APPROVE + committed
// work and COMPLETES (success) rather than no_progress-failing.
func ps7qApprovedDoneScript(t *testing.T, wtPath string) string {
	t.Helper()
	wtpEsc := strings.ReplaceAll(wtPath, "'", "'\\''")
	approve := strings.ReplaceAll(rlFixtureVerdictJSON("APPROVE"), "'", "'\\''")
	script := fmt.Sprintf(`#!/bin/sh
set -e
WTP='%s'
WS="${HARMONIK_WORKSPACE_PATH:-$WTP}"
CNT_FILE="$WTP/.harmonik/ps7q_count"
if [ ! -f "$CNT_FILE" ]; then printf '0' > "$CNT_FILE"; fi
CNT=$(cat "$CNT_FILE"); CNT=$((CNT + 1)); printf '%%d' "$CNT" > "$CNT_FILE"
case "$CNT" in
  1)
    # Implementer iter 1: commit real work -> HEAD advances past parent.
    printf 'ps7q work' > "$WS/ps7q.txt"
    git -C "$WS" add "ps7q.txt" >/dev/null 2>&1
    git -C "$WS" -c user.email=test@harmonik.local -c user.name="Test" commit -m "ps7q impl iter1" --no-gpg-sign >/dev/null 2>&1
    ;;
  2)
    # Reviewer iter 1: APPROVE -> back-edge re-enters the implementer node.
    mkdir -p "$WS/.harmonik"
    printf '%%s' '%s' > "$WS/.harmonik/review.json"
    ;;
  3)
    # Implementer iter 2: NO commit -> HEAD unchanged. Pre-fix this no_progress-
    # failed and stranded the APPROVED iter-1 commit; post-fix it COMPLETES.
    ;;
  *)
    exit 1 ;;
esac
exit 0
`, wtpEsc, approve)
	scriptPath := filepath.Join(t.TempDir(), "ps7q_handler.sh")
	//nolint:gosec // G306: test-only fixture script; not production
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("ps7qApprovedDoneScript: WriteFile: %v", err)
	}
	return scriptPath
}

// TestNoProgress_ApprovedAndDone_Completes_hk8ps7q is the hk-8ps7q regression: an
// APPROVED bead that committed in iter-1 and re-enters an agentic node at iter-2
// with HEAD unchanged (nothing left to do) must COMPLETE (success) — NOT
// no_progress-fail and strand the valid, reviewer-approved commit.
func TestNoProgress_ApprovedAndDone_Completes_hk8ps7q(t *testing.T) {
	t.Parallel()
	projectDir, wtPath, parentSHA := rlcFixtureSetup(t)
	scriptPath := ps7qApprovedDoneScript(t, wtPath)
	graph := ps7qWriteDOT(t, ps7qApproveLoopDOT())

	collector := &stubEventCollector{}
	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:           &stubBeadLedger{},
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
	result := daemon.ExportedDriveDotWorkflow(ctx, deps, rlFixtureRunID(t),
		core.BeadID("dot-ps7q-approved-done"), wtPath, parentSHA, graph)

	events := collector.eventTypes()
	t.Logf("approved-done: result=%+v events=%v", result, events)

	if !result.Success {
		t.Errorf("hk-8ps7q: expected success=true (APPROVED + committed work completes); summary=%q", result.Summary)
	}
	if result.NeedsAttention {
		t.Errorf("hk-8ps7q: expected needs_attention=false; summary=%q", result.Summary)
	}
	// The no-progress signal MUST NOT have fired — the approved commit is final.
	for _, et := range events {
		if et == string(core.EventTypeNoProgressDetected) {
			t.Errorf("hk-8ps7q: no_progress_detected false-fired on an APPROVED, committed bead; events=%v summary=%q", events, result.Summary)
		}
	}
	// The reviewer DID run (and APPROVED).
	rlAssertEventPresent(t, events, string(core.EventTypeReviewerVerdict))
}

// ps7qWriteDOT materialises a DOT graph to a temp file and loads it.
func ps7qWriteDOT(t *testing.T, src string) *dot.Graph {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "g.dot")
	//nolint:gosec // G306: test-only fixture
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatalf("write DOT: %v", err)
	}
	g, err := workflow.LoadDotWorkflow(p)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}
	return g
}

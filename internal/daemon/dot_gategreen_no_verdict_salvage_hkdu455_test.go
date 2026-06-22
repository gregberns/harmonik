package daemon_test

// dot_gategreen_no_verdict_salvage_hkdu455_test.go — regression test for
// FIX3 (hk-du455, defense-in-depth for hk-7xgu4).
//
// # The bug
//
// When a transient gate failure bounced the bead back to the implementer
// (iter-2), the implementer correctly made no new commit (the committed tree
// from iter-1 was already gate-green). Gate-2 then passed on the same tree
// and routed to the reviewer — but the no-progress guard fired at the reviewer
// entry because iterationCount ≥ 2 and HEAD had not advanced. The existing
// completion exemptions (:568/:599) only covered prior-verdict APPROVE /
// advisory-RC, NOT "committed + gate-green + no reviewer verdict yet."
// The run failed with no_progress_detected, discarding the valid committed work.
//
// Live incident: run 019eedd9-1de4-75b4-8be7-f47942252e3d, bead hk-y3o51.
// 14 prior occurrences on the same commit_gate→implement back-edge.
//
// # The fix (hk-du455 / FIX3)
//
// A new completion exemption in the no-progress block: when there IS a
// committed result (HEAD past parentSHA) AND no reviewer has run yet
// (priorVerdict == "") AND the most-recent gate passed (lastGatePassed),
// the run COMPLETES (success=true) instead of failing. The committed work is
// preserved even though the reviewer did not run (defense-in-depth).
//
// # Scenario (positive test)
//
// Graph: start → implement(agentic) → commit_gate(shell, fail-then-pass) →
//   review(agentic,reviewer) → close
//
// Handler script:
//   - CNT=1 (implement iter-1): commits real work → HEAD advances past parentSHA.
//   - CNT=2 (implement iter-2): NO commit (nothing to fix; gate was transient).
//   - CNT=3 (reviewer): MUST NOT be reached — exemption fires first.
//
// Gate command:
//   - call 1 → exit 1 (deterministic failure, bounces to implement).
//   - call 2 → exit 0 (passes on the same committed tree).
//
// Walk:
//   - implement(1): commits → iterationCount→1, priorIterHeadSHA=commit_A.
//   - commit_gate(1): FAILS → back-edge → implement(2).
//   - implement(2): no commit → iterationCount→2, priorIterHeadSHA=commit_A.
//   - commit_gate(2): PASSES (lastGatePassed=true) → reviewer entry.
//   - reviewer entry: iterationCount=2, headAdvanced=false, priorVerdict="",
//     committedResult=true, lastGatePassed=true → hk-du455 exemption fires →
//     success=true.
//
// # Negative test
//
// Same topology, but the implementer NEVER commits (HEAD stays at parentSHA).
// committedResult=false → exemption does NOT fire → no_progress_detected fires →
// success=false.
//
// Bead: hk-du455.

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

// du455GatedReviewDOT returns a standard-bead topology (implement → commit_gate
// → review) where the gate command is parameterised. Mirrors the production
// layout that caused the hk-7xgu4 incident.
func du455GatedReviewDOT(gateCommand string) string {
	return fmt.Sprintf(`digraph "hk-du455-test" {
    schema_version="1"; version="1.0"; workflow_id="hk-du455-test";
    start_node="start"; terminal_node_ids="close,close-needs-attention";

    start [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];
    implement [type="agentic", agent_type="implementer", handler_ref="claude-implementer", idempotency_class="non-idempotent"];
    commit_gate [type="non-agentic", handler_ref="shell", idempotency_class="idempotent", tool_command="%s", timeout="60"];
    review [type="agentic", agent_type="reviewer", handler_ref="claude-reviewer", idempotency_class="idempotent"];
    close [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];
    "close-needs-attention" [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];

    start -> implement;
    implement -> commit_gate;
    commit_gate -> review [condition="outcome.status == 'SUCCESS'"];
    commit_gate -> implement [condition="outcome.status == 'FAIL' && outcome.failure_class == 'deterministic'", traversal_cap="3"];
    commit_gate -> "close-needs-attention";
    review -> close [condition="outcome.preferred_label == 'APPROVE'"];
    review -> "close-needs-attention";
}
`, gateCommand)
}

// du455WriteDOT materialises a DOT graph string to a temp file and loads it.
func du455WriteDOT(t *testing.T, src string) *dot.Graph {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "g.dot")
	//nolint:gosec // G306: test-only fixture
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatalf("du455WriteDOT: write: %v", err)
	}
	g, err := workflow.LoadDotWorkflow(p)
	if err != nil {
		t.Fatalf("du455WriteDOT: LoadDotWorkflow: %v", err)
	}
	return g
}

// du455WriteScript writes a /bin/sh fixture script to a temp dir.
func du455WriteScript(t *testing.T, name, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	//nolint:gosec // G306: test-only fixture script; not production
	if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
		t.Fatalf("du455WriteScript(%s): %v", name, err)
	}
	return p
}

// du455CommittingScript drives the agentic nodes for the positive scenario:
//
//	CNT=1 (implement iter-1): commits real work → HEAD advances past parentSHA.
//	CNT=2 (implement iter-2): NO commit (gate was transient; nothing to fix).
//	CNT=3 (reviewer): MUST NOT be reached if the hk-du455 exemption fires first.
func du455CommittingScript(t *testing.T, wtPath string) string {
	t.Helper()
	wtpEsc := strings.ReplaceAll(wtPath, "'", "'\\''")
	script := fmt.Sprintf(`#!/bin/sh
set -e
WTP='%s'
WS="${HARMONIK_WORKSPACE_PATH:-$WTP}"
CNT_FILE="$WTP/.harmonik/du455_commit_count"
if [ ! -f "$CNT_FILE" ]; then printf '0' > "$CNT_FILE"; fi
CNT=$(cat "$CNT_FILE"); CNT=$((CNT + 1)); printf '%%d' "$CNT" > "$CNT_FILE"
case "$CNT" in
  1)
    # Implement iter-1: commit real work.
    printf 'du455 work' > "$WS/du455_impl.txt"
    git -C "$WS" add "du455_impl.txt" >/dev/null 2>&1
    git -C "$WS" -c user.email=test@harmonik.local -c user.name="Test" \
        commit -m "du455 impl iter1" --no-gpg-sign >/dev/null 2>&1
    ;;
  2)
    # Implement iter-2: transient gate failed; committed tree is already correct.
    # Make NO new commit — HEAD stays put.
    ;;
  *)
    # CNT=3 would be the reviewer — must never be reached.
    printf 'UNEXPECTED_INVOCATION_%%d\n' "$CNT" >&2
    exit 1 ;;
esac
exit 0
`, wtpEsc)
	return du455WriteScript(t, "du455_commit.sh", script)
}

// du455RedGateScript drives the agentic nodes for the negative scenario:
//
//	CNT=1 (implement iter-1): commits real work → HEAD advances past parentSHA.
//	CNT=2 (implement iter-2): NO commit (gate kept failing; nothing to fix).
//	CNT=3 (implement iter-3): MUST NOT be reached (no_progress fires instead).
//
// The gate ALWAYS fails (never passes), so lastGatePassed stays false.
// At the iter-3 entry: committedResult=true, priorVerdict="", lastGatePassed=false
// → the hk-du455 exemption does NOT fire (requires lastGatePassed=true).
func du455RedGateScript(t *testing.T, wtPath string) string {
	t.Helper()
	wtpEsc := strings.ReplaceAll(wtPath, "'", "'\\''")
	script := fmt.Sprintf(`#!/bin/sh
set -e
WTP='%s'
WS="${HARMONIK_WORKSPACE_PATH:-$WTP}"
CNT_FILE="$WTP/.harmonik/du455_redgate_count"
if [ ! -f "$CNT_FILE" ]; then printf '0' > "$CNT_FILE"; fi
CNT=$(cat "$CNT_FILE"); CNT=$((CNT + 1)); printf '%%d' "$CNT" > "$CNT_FILE"
case "$CNT" in
  1)
    # Implement iter-1: commit real work.
    printf 'du455 rg work' > "$WS/du455_rg_impl.txt"
    git -C "$WS" add "du455_rg_impl.txt" >/dev/null 2>&1
    git -C "$WS" -c user.email=test@harmonik.local -c user.name="Test" \
        commit -m "du455 rg impl iter1" --no-gpg-sign >/dev/null 2>&1
    ;;
  2)
    # Implement iter-2: gate kept failing; nothing new to commit.
    ;;
  *)
    printf 'UNEXPECTED_INVOCATION_%%d\n' "$CNT" >&2
    exit 1 ;;
esac
exit 0
`, wtpEsc)
	return du455WriteScript(t, "du455_redgate.sh", script)
}

// TestGateGreenNoVerdict_CommittedWork_Completes_hkdu455 is the positive
// regression: a committed + gate-green tree with no prior reviewer verdict must
// COMPLETE (success=true) instead of firing no_progress_detected.
//
// This is the exact failure shape from hk-7xgu4 / run 019eedd9: transient
// Gate-1 failure → iter-2 no-op → Gate-2 passes → reviewer entry →
// no_progress fires (before fix), discarding the valid committed tree.
func TestGateGreenNoVerdict_CommittedWork_Completes_hkdu455(t *testing.T) {
	t.Parallel()

	projectDir, wtPath, parentSHA := rlcFixtureSetup(t)
	scriptPath := du455CommittingScript(t, wtPath)

	// Gate fails on the first call, passes on the second (simulating transient
	// Gate-1 failure followed by Gate-2 success on the same committed tree).
	gateCmd := "mkdir -p .harmonik; C=$(cat .harmonik/du455_gate 2>/dev/null || echo 0); C=$((C+1)); printf '%d' $C > .harmonik/du455_gate; [ $C -gt 1 ]"
	graph := du455WriteDOT(t, du455GatedReviewDOT(gateCmd))

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
		core.BeadID("du455-committed-completes"), wtPath, parentSHA, graph)

	events := collector.eventTypes()
	t.Logf("hk-du455 positive: result=%+v events=%v", result, events)

	// hk-du455 fix: committed + gate-green + no prior verdict → complete, not fail.
	if !result.Success {
		t.Errorf("hk-du455: expected success=true (committed work + green gate + no prior verdict must complete); summary=%q", result.Summary)
	}
	if result.NeedsAttention {
		t.Errorf("hk-du455: expected needs_attention=false; summary=%q", result.Summary)
	}
	// The no-progress and stall signals must NOT have fired.
	for _, et := range events {
		if et == string(core.EventTypeNoProgressDetected) {
			t.Errorf("hk-du455: no_progress_detected false-fired on committed+gate-green tree with no prior verdict; events=%v summary=%q", events, result.Summary)
		}
		if et == string(core.EventTypeReviewFixupStalled) {
			t.Errorf("hk-du455: review_fixup_stalled false-fired on committed+gate-green tree with no prior verdict; events=%v summary=%q", events, result.Summary)
		}
	}
	// Summary should mention the hk-du455 exemption path.
	if !strings.Contains(result.Summary, "hk-du455") && !strings.Contains(result.Summary, "gate-green") {
		t.Errorf("hk-du455: expected summary to reference hk-du455 or gate-green; got %q", result.Summary)
	}
}

// TestGateGreenNoVerdict_RedGate_StillFails_hkdu455 is the negative guard:
// when the gate is RED (lastGatePassed=false), the hk-du455 exemption must NOT
// fire even though committed work exists and no reviewer verdict has been
// produced. The no-progress guard must still catch this genuinely-stuck run.
//
// Walk: implement(1) commits → gate FAILS → implement(2) no-commit → gate FAILS
// → implement(3) entry: committedResult=true, priorVerdict="", lastGatePassed=false
// → exemption skipped → no_progress_detected fires → success=false.
func TestGateGreenNoVerdict_RedGate_StillFails_hkdu455(t *testing.T) {
	t.Parallel()

	projectDir, wtPath, parentSHA := rlcFixtureSetup(t)
	scriptPath := du455RedGateScript(t, wtPath)

	// Gate always fails — lastGatePassed stays false throughout. The
	// hk-du455 exemption requires lastGatePassed=true; when RED it must not fire.
	gateCmd := "exit 1"
	graph := du455WriteDOT(t, du455GatedReviewDOT(gateCmd))

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
		core.BeadID("du455-redgate-fails"), wtPath, parentSHA, graph)

	events := collector.eventTypes()
	t.Logf("hk-du455 negative (red gate): result=%+v events=%v", result, events)

	// hk-du455 exemption must NOT fire when the gate is RED (lastGatePassed=false).
	if result.Success {
		t.Errorf("hk-du455: expected success=false (gate RED — exemption requires lastGatePassed=true); summary=%q", result.Summary)
	}
	if !result.NeedsAttention {
		t.Errorf("hk-du455: expected needs_attention=true on genuinely-stuck run (red gate); summary=%q", result.Summary)
	}
	// The no-progress guard must have fired.
	found := false
	for _, et := range events {
		if et == string(core.EventTypeNoProgressDetected) || et == string(core.EventTypeReviewFixupStalled) {
			found = true
		}
	}
	if !found {
		t.Errorf("hk-du455: expected no_progress_detected or review_fixup_stalled; got events=%v", events)
	}
}

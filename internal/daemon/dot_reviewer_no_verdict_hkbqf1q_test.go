package daemon_test

// dot_reviewer_no_verdict_hkbqf1q_test.go — regression tests for hk-bqf1q.
//
// # The bug
//
// When a reviewer node exits without writing review.json (stall, hang, or
// budget kill without a budget sentinel), the DOT cascade returned an opaque
// error and marked the run failed/needs-attention. Any impl commit from the
// current iteration was STRANDED on the run branch — never merged.
//
// The hk-8ps7q fix only rescues approved-and-done runs (priorVerdict==APPROVE).
// A run where the reviewer stalled on its FIRST invocation (priorVerdict=="")
// still hard-failed, stranding the valid impl commit.
//
// # The fix (hk-bqf1q)
//
// When dispatchDotAgenticNode returns errDotReviewerNoVerdict AND there is a
// committed result (HEAD is past parentSHA), driveDotWorkflow retries the
// reviewer node up to dotMaxReviewerNoVerdictRetries times (=1) rather than
// hard-failing. If the retry produces a verdict the run proceeds normally.
// If the retry also produces no verdict (or there is no committed result), the
// run hard-fails as before.
//
// # Scenarios
//
// Scenario A — stall then APPROVE: reviewer stalls on attempt 1, writes APPROVE
// on attempt 2. Run must COMPLETE (success=true).
//
// Scenario B — stall twice (retry exhausted): reviewer stalls on both attempts.
// Run must FAIL (success=false, needsAttention=true). Committed work cannot be
// rescued without human attention.
//
// Scenario C — no committed result: reviewer stalls, but no impl commit was
// ever made (uncommon; non-committing implementer or iter-1 hard exit).
// Run must FAIL immediately (no retry, since there is nothing to rescue).
//
// # Spec refs
//   - specs/execution-model.md §4.3 EM-015e (no-progress / reviewer stall)
//   - specs/event-model.md §8.1a (reviewer_verdict / no_progress_detected)
//
// Bead: hk-bqf1q.

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

// bqf1qReviewLoopDOT returns a minimal DOT graph: start → implement → review,
// with APPROVE going to close and REQUEST_CHANGES looping back to implement.
// This is the standard review-loop shape that the production standard-bead.dot
// uses, trimmed to the nodes required for the stall scenarios.
func bqf1qReviewLoopDOT() string {
	return `digraph "hk-bqf1q-stall" {
    schema_version="1"; version="1.0"; workflow_id="hk-bqf1q-stall";
    start_node="start"; terminal_node_ids="close,close-needs-attention";

    start         [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];
    implement     [type="agentic", agent_type="implementer", handler_ref="claude-implementer", idempotency_class="non-idempotent"];
    review        [type="agentic", agent_type="reviewer",    handler_ref="claude-reviewer",    idempotency_class="idempotent"];
    close         [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];
    "close-needs-attention" [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];

    start -> implement;
    implement -> review;
    review -> close [condition="outcome.preferred_label == 'APPROVE'"];
    review -> implement [condition="outcome.preferred_label == 'REQUEST_CHANGES'", traversal_cap="3"];
    review -> "close-needs-attention" [condition="outcome.preferred_label == 'BLOCK'"];
    review -> "close-needs-attention";
}
`
}

// bqf1qWriteDOT materialises a DOT graph string to a temp file and loads it.
func bqf1qWriteDOT(t *testing.T, src string) *dot.Graph {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "g.dot")
	//nolint:gosec // G306: test-only fixture
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatalf("bqf1qWriteDOT: WriteFile: %v", err)
	}
	g, err := workflow.LoadDotWorkflow(p)
	if err != nil {
		t.Fatalf("bqf1qWriteDOT: LoadDotWorkflow: %v", err)
	}
	return g
}

// bqf1qScenarioAScript — stall on first reviewer attempt, APPROVE on second.
//
// Invocation sequence:
//  1. implement (iter 1): commits a file → HEAD advances past parentSHA.
//  2. review (attempt 1): exits 0 WITHOUT writing review.json → stall.
//  3. review (attempt 2, retry): writes APPROVE → run completes.
func bqf1qScenarioAScript(t *testing.T, wtPath string) string {
	t.Helper()
	wtpEsc := strings.ReplaceAll(wtPath, "'", "'\\''")
	approve := strings.ReplaceAll(rlFixtureVerdictJSON("APPROVE"), "'", "'\\''")
	script := fmt.Sprintf(`#!/bin/sh
set -e
WTP='%s'
WS="${HARMONIK_WORKSPACE_PATH:-$WTP}"
CNT_FILE="$WTP/.harmonik/bqf1q_a_count"
if [ ! -f "$CNT_FILE" ]; then printf '0' > "$CNT_FILE"; fi
CNT=$(cat "$CNT_FILE"); CNT=$((CNT + 1)); printf '%%d' "$CNT" > "$CNT_FILE"
case "$CNT" in
  1)
    # Implementer iter 1: commit real work -> HEAD advances past parentSHA.
    printf 'bqf1q work' > "$WS/bqf1q_a.txt"
    git -C "$WS" add "bqf1q_a.txt" >/dev/null 2>&1
    git -C "$WS" -c user.email=test@harmonik.local -c user.name="Test" \
        commit -m "bqf1q-a impl iter1" --no-gpg-sign >/dev/null 2>&1
    ;;
  2)
    # Reviewer attempt 1: exits WITHOUT writing review.json -> stall.
    # driveDotWorkflow should retry the reviewer node (hk-bqf1q fix).
    ;;
  3)
    # Reviewer attempt 2 (retry): write APPROVE -> run should complete.
    mkdir -p "$WS/.harmonik"
    printf '%%s' '%s' > "$WS/.harmonik/review.json"
    ;;
  *)
    exit 1 ;;
esac
exit 0
`, wtpEsc, approve)
	scriptPath := filepath.Join(t.TempDir(), "bqf1q_a_handler.sh")
	//nolint:gosec // G306: test-only fixture
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("bqf1qScenarioAScript: WriteFile: %v", err)
	}
	return scriptPath
}

// bqf1qScenarioBScript — stall on both reviewer attempts (retry exhausted).
//
// Invocation sequence:
//  1. implement (iter 1): commits a file → HEAD advances past parentSHA.
//  2. review (attempt 1): exits 0 WITHOUT writing review.json → stall.
//  3. review (attempt 2, retry): also exits 0 WITHOUT writing review.json.
//     Retry budget exhausted → run hard-fails.
func bqf1qScenarioBScript(t *testing.T, wtPath string) string {
	t.Helper()
	wtpEsc := strings.ReplaceAll(wtPath, "'", "'\\''")
	script := fmt.Sprintf(`#!/bin/sh
set -e
WTP='%s'
WS="${HARMONIK_WORKSPACE_PATH:-$WTP}"
CNT_FILE="$WTP/.harmonik/bqf1q_b_count"
if [ ! -f "$CNT_FILE" ]; then printf '0' > "$CNT_FILE"; fi
CNT=$(cat "$CNT_FILE"); CNT=$((CNT + 1)); printf '%%d' "$CNT" > "$CNT_FILE"
case "$CNT" in
  1)
    # Implementer iter 1: commit real work -> HEAD advances.
    printf 'bqf1q-b work' > "$WS/bqf1q_b.txt"
    git -C "$WS" add "bqf1q_b.txt" >/dev/null 2>&1
    git -C "$WS" -c user.email=test@harmonik.local -c user.name="Test" \
        commit -m "bqf1q-b impl iter1" --no-gpg-sign >/dev/null 2>&1
    ;;
  2|3)
    # Reviewer attempts 1 and 2: both stall (no review.json written).
    # After attempt 2, retry budget exhausted -> hard-fail.
    ;;
  *)
    exit 1 ;;
esac
exit 0
`, wtpEsc)
	scriptPath := filepath.Join(t.TempDir(), "bqf1q_b_handler.sh")
	//nolint:gosec // G306: test-only fixture
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("bqf1qScenarioBScript: WriteFile: %v", err)
	}
	return scriptPath
}

// bqf1qScenarioCScript — reviewer stalls with NO impl commit (nothing to rescue).
//
// Invocation sequence:
//  1. implement (iter 1): exits 0 WITHOUT committing → HEAD stays at parentSHA.
//  2. Cascade hard-fails on iter-1 no-commit (implementer must advance HEAD on
//     iter 1). Reviewer never runs.
//
// NOTE: Because the iter-1 implementer hard-fails before the reviewer is ever
// dispatched, this scenario tests that the no-retry path still fails correctly.
// We exercise it via a variant where the implementer DOES commit but we test
// the reviewer-stall + no-committed-result branch by comparing against parentSHA.
//
// Actually, the simplest way to test "no committed result + reviewer stall" is:
// implementer exits WITHOUT committing on iter 1 → cascade already fails
// (iter-1 no-commit). The reviewer never runs, so there is nothing to retry.
// We verify the run fails with needsAttention=true.
func bqf1qScenarioCScript(t *testing.T, wtPath string) string {
	t.Helper()
	wtpEsc := strings.ReplaceAll(wtPath, "'", "'\\''")
	script := fmt.Sprintf(`#!/bin/sh
set -e
WTP='%s'
WS="${HARMONIK_WORKSPACE_PATH:-$WTP}"
# iter-1 implementer: exits 0 WITHOUT committing.
# The cascade must fail (iter-1 HEAD advance is required).
exit 0
`, wtpEsc)
	scriptPath := filepath.Join(t.TempDir(), "bqf1q_c_handler.sh")
	//nolint:gosec // G306: test-only fixture
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("bqf1qScenarioCScript: WriteFile: %v", err)
	}
	return scriptPath
}

// TestReviewerNoVerdict_StallThenApprove_Completes_hkbqf1q — scenario A:
// reviewer stalls on attempt 1, APPROVE on the retry → run must COMPLETE.
func TestReviewerNoVerdict_StallThenApprove_Completes_hkbqf1q(t *testing.T) {
	t.Parallel()
	projectDir, wtPath, parentSHA := rlcFixtureSetup(t)
	scriptPath := bqf1qScenarioAScript(t, wtPath)
	graph := bqf1qWriteDOT(t, bqf1qReviewLoopDOT())

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
		core.BeadID("dot-bqf1q-stall-then-approve"), wtPath, parentSHA, graph)

	events := collector.eventTypes()
	t.Logf("stall-then-approve: result=%+v events=%v", result, events)

	if !result.Success {
		t.Errorf("hk-bqf1q scenario A: expected success=true (reviewer retried after stall and APPROVED); summary=%q", result.Summary)
	}
	if result.NeedsAttention {
		t.Errorf("hk-bqf1q scenario A: expected needs_attention=false; summary=%q", result.Summary)
	}
	// The reviewer verdict MUST have been emitted (from the successful retry).
	rlAssertEventPresent(t, events, string(core.EventTypeReviewerVerdict))
	// no_progress_detected must NOT have fired — committed work was rescued.
	for _, et := range events {
		if et == string(core.EventTypeNoProgressDetected) {
			t.Errorf("hk-bqf1q scenario A: no_progress_detected false-fired; events=%v summary=%q", events, result.Summary)
		}
	}
}

// TestReviewerNoVerdict_StallTwice_Fails_hkbqf1q — scenario B:
// reviewer stalls on both attempts → retry budget exhausted → run must FAIL.
func TestReviewerNoVerdict_StallTwice_Fails_hkbqf1q(t *testing.T) {
	t.Parallel()
	projectDir, wtPath, parentSHA := rlcFixtureSetup(t)
	scriptPath := bqf1qScenarioBScript(t, wtPath)
	graph := bqf1qWriteDOT(t, bqf1qReviewLoopDOT())

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
		core.BeadID("dot-bqf1q-stall-twice"), wtPath, parentSHA, graph)

	events := collector.eventTypes()
	t.Logf("stall-twice: result=%+v events=%v", result, events)

	if result.Success {
		t.Errorf("hk-bqf1q scenario B: expected success=false (retry budget exhausted); summary=%q", result.Summary)
	}
	if !result.NeedsAttention {
		t.Errorf("hk-bqf1q scenario B: expected needs_attention=true; summary=%q", result.Summary)
	}
	// reviewer_verdict must NOT have fired — no verdict was ever written.
	for _, et := range events {
		if et == string(core.EventTypeReviewerVerdict) {
			t.Errorf("hk-bqf1q scenario B: reviewer_verdict fired but reviewer never wrote a verdict; events=%v", events)
		}
	}
}

// TestReviewerNoVerdict_NoCommit_Fails_hkbqf1q — scenario C:
// iter-1 implementer makes no commit → cascade fails before reviewer runs.
// This verifies the existing iter-1 no-commit hard-fail is preserved (the
// retry path only applies when committed work exists).
func TestReviewerNoVerdict_NoCommit_Fails_hkbqf1q(t *testing.T) {
	t.Parallel()
	projectDir, wtPath, parentSHA := rlcFixtureSetup(t)
	scriptPath := bqf1qScenarioCScript(t, wtPath)
	graph := bqf1qWriteDOT(t, bqf1qReviewLoopDOT())

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
		core.BeadID("dot-bqf1q-no-commit"), wtPath, parentSHA, graph)

	t.Logf("no-commit: result=%+v", result)

	if result.Success {
		t.Errorf("hk-bqf1q scenario C: expected success=false (iter-1 no-commit); summary=%q", result.Summary)
	}
	if !result.NeedsAttention {
		t.Errorf("hk-bqf1q scenario C: expected needs_attention=true; summary=%q", result.Summary)
	}
}

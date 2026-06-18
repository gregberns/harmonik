package daemon_test

// dot_review_gate_fanout_hk2vpj_test.go — regression test for hk-2vpj.
//
// # The bug
//
// In a multi-reviewer fan-out DOT graph (review_correctness → review_design →
// review_tests → consolidate), when the FIRST reviewer APPROVEs at
// iterationCount >= 2, the hk-8ps7q "approved-and-done" exemption in
// dot_cascade.go fires on ENTRY to the SECOND reviewer and short-circuits the
// run as success — reviewers 2..N and consolidate never run.
//
// iterationCount reaches 2 trivially via one commit_gate deterministic-fail
// loopback BEFORE the first reviewer: the exact shape that hit hk-7xr9 in
// production (start → implement → commit_gate(FAIL) → implement → commit_gate(PASS)
// → review_correctness(APPROVE@iter2) → guard fires entering review_design →
// run_completed with a single reviewer's APPROVE, design+tests review bypassed).
//
// Wave-1 of hk-7xr9 masked the bug because the first reviewer returned
// REQUEST_CHANGES (APPROVE exemption did not apply), so all 3 reviewers and
// consolidate ran once.
//
// # The fix (hk-2vpj)
//
// Gate the hk-8ps7q APPROVE-completion exemption on !isReviewer: it must only
// complete the run when the next/re-entered node is an IMPLEMENTER (nothing left
// to do after APPROVE), never when advancing to the next REVIEWER in the fan-out.
//
// # Scenarios (this test)
//
// TestDotReviewFanout_FirstApproveDoesNotShortCircuit_hk2vpj (RED→GREEN):
//
//	Graph: triple-review spine with a commit_gate that fails once then passes.
//	Walk:  implement(commit)→commit_gate(fail→loopback)→implement(commit)→
//	       commit_gate(pass)→review_correctness(APPROVE@iter=2)→
//	       review_design→review_tests→consolidate→close.
//	Assert: run succeeds AND review_design, review_tests, consolidate all dispatched.
//	Without the fix: guard fires on entry to review_design → run_completed after
//	only review_correctness (review_design node count = 0).
//
// TestDotReviewFanout_GenuineNoProgressStillFires_hk2vpj (regression guard):
//
//	Graph: same spine.
//	Walk:  implement(commit)→commit_gate(fail→loopback)→implement(NO commit)→
//	       no-progress guard fires before reaching any reviewer.
//	Assert: run fails (success=false) — the hk-8ps7q + hk-togxq genuine-stuck
//	protection is intact.
//
// Bead: hk-2vpj.

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

// ── DOT fixture ───────────────────────────────────────────────────────────────

// fanoutReviewGateDOT returns a triple-review DOT graph where the commit_gate
// node uses the given shell command as its tool_command — allowing the caller
// to inject a fail-once-then-pass counter script.
func fanoutReviewGateDOT(gateToolCommand string) string {
	return fmt.Sprintf(`digraph "hk-2vpj-fanout-review-gate" {
    schema_version="1"; version="1.0"; workflow_id="hk-2vpj-fanout-review-gate";
    start_node="start"; terminal_node_ids="close,close-needs-attention";

    start [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];
    implement [type="agentic", agent_type="implementer", handler_ref="claude-implementer", idempotency_class="non-idempotent"];
    commit_gate [type="non-agentic", handler_ref="shell", idempotency_class="idempotent", tool_command=%q, timeout="30"];
    review_correctness [type="agentic", agent_type="reviewer", handler_ref="claude-reviewer", idempotency_class="idempotent"];
    review_design [type="agentic", agent_type="reviewer", handler_ref="claude-reviewer", idempotency_class="idempotent"];
    review_tests [type="agentic", agent_type="reviewer", handler_ref="claude-reviewer", idempotency_class="idempotent"];
    consolidate [type="agentic", agent_type="reviewer", handler_ref="claude-reviewer", idempotency_class="idempotent"];
    close [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];
    "close-needs-attention" [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];

    start -> implement;
    implement -> commit_gate;
    commit_gate -> review_correctness [condition="outcome.status == 'SUCCESS'"];
    commit_gate -> implement [condition="outcome.status == 'FAIL' && outcome.failure_class == 'deterministic'", traversal_cap="3"];
    commit_gate -> "close-needs-attention";
    review_correctness -> review_design;
    review_design -> review_tests;
    review_tests -> consolidate;
    consolidate -> close [condition="outcome.preferred_label == 'APPROVE'"];
    consolidate -> implement [condition="outcome.preferred_label == 'REQUEST_CHANGES'", traversal_cap="3"];
    consolidate -> "close-needs-attention" [condition="outcome.preferred_label == 'BLOCK'"];
    consolidate -> "close-needs-attention";
}
`, gateToolCommand)
}

// fanoutLoadDOT writes src to a temp file and loads it as a *dot.Graph.
func fanoutLoadDOT(t *testing.T, src string) *dot.Graph {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "hk-2vpj.dot")
	//nolint:gosec // G306: test-only fixture
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatalf("fanoutLoadDOT: write: %v", err)
	}
	g, err := workflow.LoadDotWorkflow(p)
	if err != nil {
		t.Fatalf("fanoutLoadDOT: LoadDotWorkflow: %v", err)
	}
	return g
}

// ── handler scripts ───────────────────────────────────────────────────────────

// fanoutAllApproveScript writes a handler script for the commit-loopback +
// all-APPROVE scenario:
//
//	Call 1 (implement):            commit a file → HEAD advances.
//	Call 2 (implement after fail): commit another file → HEAD advances.
//	Calls 3..6 (reviewers):        write an APPROVE verdict to .harmonik/review.json.
//
// The commit_gate is non-agentic (shell) and does NOT go through this handler.
func fanoutAllApproveScript(t *testing.T, wtPath string) string {
	t.Helper()
	wtpEsc := strings.ReplaceAll(wtPath, "'", "'\\''")
	approve := strings.ReplaceAll(rlFixtureVerdictJSON("APPROVE"), "'", "'\\''")
	script := fmt.Sprintf(`#!/bin/sh
set -e
WTP='%s'
WS="${HARMONIK_WORKSPACE_PATH:-$WTP}"
CNT_FILE="$WTP/.harmonik/hk2vpj_all_approve_count"
if [ ! -f "$CNT_FILE" ]; then printf '0' > "$CNT_FILE"; fi
CNT=$(cat "$CNT_FILE"); CNT=$((CNT + 1)); printf '%%d' "$CNT" > "$CNT_FILE"
case "$CNT" in
  1|2)
    # Implementer pass $CNT: commit a unique file so HEAD advances.
    printf '%%d' "$CNT" > "$WS/hk2vpj_impl_$CNT.txt"
    git -C "$WS" add "hk2vpj_impl_$CNT.txt" >/dev/null 2>&1
    git -C "$WS" -c user.email=test@harmonik.local -c user.name="Test" \
        commit -m "hk2vpj impl pass $CNT" --no-gpg-sign >/dev/null 2>&1
    ;;
  3|4|5|6)
    # Reviewer pass $CNT: write APPROVE verdict.
    mkdir -p "$WS/.harmonik"
    printf '%%s' '%s' > "$WS/.harmonik/review.json"
    ;;
  *)
    exit 1 ;;
esac
exit 0
`, wtpEsc, approve)
	p := filepath.Join(t.TempDir(), "hk2vpj_all_approve.sh")
	//nolint:gosec // G306: test-only fixture script; not production
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatalf("fanoutAllApproveScript: %v", err)
	}
	return p
}

// fanoutGenuineStuckScript writes a handler script for the genuine-no-progress
// scenario:
//
//	Call 1 (implement): commit a file → HEAD advances.
//	Call 2 (implement): make NO commit → HEAD does not advance.
//	(Guard fires before any reviewer runs.)
func fanoutGenuineStuckScript(t *testing.T, wtPath string) string {
	t.Helper()
	wtpEsc := strings.ReplaceAll(wtPath, "'", "'\\''")
	script := fmt.Sprintf(`#!/bin/sh
set -e
WTP='%s'
WS="${HARMONIK_WORKSPACE_PATH:-$WTP}"
CNT_FILE="$WTP/.harmonik/hk2vpj_stuck_count"
if [ ! -f "$CNT_FILE" ]; then printf '0' > "$CNT_FILE"; fi
CNT=$(cat "$CNT_FILE"); CNT=$((CNT + 1)); printf '%%d' "$CNT" > "$CNT_FILE"
case "$CNT" in
  1)
    # Implementer pass 1: commit.
    printf 'stuck-work' > "$WS/hk2vpj_stuck.txt"
    git -C "$WS" add "hk2vpj_stuck.txt" >/dev/null 2>&1
    git -C "$WS" -c user.email=test@harmonik.local -c user.name="Test" \
        commit -m "hk2vpj stuck impl pass 1" --no-gpg-sign >/dev/null 2>&1
    ;;
  2)
    # Implementer pass 2: NO commit — genuinely stuck.
    ;;
  *)
    exit 1 ;;
esac
exit 0
`, wtpEsc)
	p := filepath.Join(t.TempDir(), "hk2vpj_stuck.sh")
	//nolint:gosec // G306: test-only fixture script; not production
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatalf("fanoutGenuineStuckScript: %v", err)
	}
	return p
}

// fanoutGateScript returns a shell tool_command string for the commit_gate that
// fails on its first invocation (deterministic exit 1) and succeeds on the second
// (exit 0), using counterFile as the persistent counter.
//
// The command is single-line /bin/sh -c compatible; single-quotes in the path are
// escaped.
func fanoutGateScript(counterFile string) string {
	cf := strings.ReplaceAll(counterFile, "'", "'\\''")
	// Increment counter; fail if count <= 1, succeed if count >= 2.
	return fmt.Sprintf(
		"CNT=$(cat '%s' 2>/dev/null || echo 0); CNT=$((CNT+1)); printf '%%d' \"$CNT\" > '%s'; [ \"$CNT\" -ge 2 ]",
		cf, cf,
	)
}

// ── tests ─────────────────────────────────────────────────────────────────────

// TestDotReviewFanout_FirstApproveDoesNotShortCircuit_hk2vpj is the RED→GREEN
// regression test for hk-2vpj.
//
// Without the fix (hk-2vpj), the hk-8ps7q APPROVE exemption fires on ENTRY to
// review_design (the second reviewer) because iterationCount >= 2 and HEAD did
// not advance after review_correctness's APPROVE. The run completes after only
// review_correctness — review_design, review_tests, and consolidate never run.
//
// With the fix, the exemption is gated on !isReviewer: it no longer fires while
// advancing through the reviewer fan-out, so all three axis reviewers and
// consolidate dispatch as intended.
func TestDotReviewFanout_FirstApproveDoesNotShortCircuit_hk2vpj(t *testing.T) {
	t.Parallel()

	projectDir, wtPath, parentSHA := rlcFixtureSetup(t)
	scriptPath := fanoutAllApproveScript(t, wtPath)
	counterFile := filepath.Join(t.TempDir(), "hk2vpj_gate_counter")
	gateCmd := fanoutGateScript(counterFile)
	graph := fanoutLoadDOT(t, fanoutReviewGateDOT(gateCmd))

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

	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	result := daemon.ExportedDriveDotWorkflow(ctx, deps, rlFixtureRunID(t),
		core.BeadID("hk-2vpj-fanout-approve"), wtPath, parentSHA, graph)

	events := collector.eventTypes()
	t.Logf("fanout-approve: result=%+v events=%v", result, events)

	// The run MUST succeed: all four reviewer nodes (review_correctness,
	// review_design, review_tests, consolidate) ran and the final APPROVE
	// routed to the close terminal.
	if !result.Success {
		t.Errorf("hk-2vpj: expected success=true — all reviewers must run even when the first APPROVEs at iter>=2; summary=%q", result.Summary)
	}

	// Guard events must NOT have fired — that would indicate the exemption
	// short-circuited mid-fan-out.
	for _, et := range events {
		if et == string(core.EventTypeNoProgressDetected) || et == string(core.EventTypeReviewFixupStalled) {
			t.Errorf("hk-2vpj: no-progress guard fired (%s) but run should complete via the close terminal after all reviewers dispatch; events=%v", et, events)
		}
	}

	// The summary must NOT mention "completed at iteration" with the first
	// reviewer's APPROVE — that is the pre-fix short-circuit signature.
	// A legitimate completion routes through the consolidate→close terminal.
	if strings.Contains(result.Summary, "reviewer APPROVED and committed work is final") {
		t.Errorf("hk-2vpj: run summary indicates early APPROVE-exemption short-circuit (review_design/review_tests/consolidate bypassed); summary=%q", result.Summary)
	}
}

// TestDotReviewFanout_GenuineNoProgressStillFires_hk2vpj verifies that the
// hk-2vpj fix does NOT weaken the genuine no-progress protection for implementer
// loopbacks.
//
// Scenario: implement commits on pass 1; commit_gate fails → loopback; implement
// makes NO new commit on pass 2 (genuinely stuck). The guard must still fire
// (success=false) — the hk-8ps7q + hk-togxq negative-guard invariant is preserved.
func TestDotReviewFanout_GenuineNoProgressStillFires_hk2vpj(t *testing.T) {
	t.Parallel()

	projectDir, wtPath, parentSHA := rlcFixtureSetup(t)
	scriptPath := fanoutGenuineStuckScript(t, wtPath)
	// Gate always fails (exit 1) — we only need one loopback before the guard fires.
	graph := fanoutLoadDOT(t, fanoutReviewGateDOT("exit 1"))

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
		core.BeadID("hk-2vpj-genuine-stuck"), wtPath, parentSHA, graph)

	events := collector.eventTypes()
	t.Logf("genuine-stuck: result=%+v events=%v", result, events)

	// The genuine-stuck case MUST fail: the implementer looped without committing.
	if result.Success {
		t.Errorf("hk-2vpj regression: expected success=false for genuine implementer no-progress; hk-togxq negative-guard must still fire; summary=%q", result.Summary)
	}

	// no_progress_detected or review_fixup_stalled must have been emitted.
	guardFired := false
	for _, et := range events {
		if et == string(core.EventTypeNoProgressDetected) || et == string(core.EventTypeReviewFixupStalled) {
			guardFired = true
		}
	}
	if !guardFired {
		t.Errorf("hk-2vpj regression: expected no_progress_detected or review_fixup_stalled event; got events=%v", events)
	}
}

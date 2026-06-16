package daemon_test

// dot_advisory_rc_completes_hkw2ow_test.go — regression tests for hk-w2ow.
//
// # The bug
//
// The DOT-cascade no-progress / fix-up-stalled guard (the hk-togxq HEAD-advance
// check, broadened for APPROVE by hk-8ps7q) fires at review iteration ≥ 2
// whenever HEAD has not advanced. When the reviewer returns REQUEST_CHANGES
// carrying ONLY advisory / nitpick feedback (nothing committable), the
// implementer correctly adds nothing on the next iteration, HEAD stays put, the
// build/test gate is STILL GREEN — and the run nonetheless FAILS
// (review_fixup_stalled), discarding finished, tested work. The pre-existing
// completion exemption (hk-8ps7q) only covered reviewer=APPROVE + unchanged
// HEAD; it did NOT cover advisory-only REQUEST_CHANGES.
//
// # The fix (hk-w2ow)
//
// The no-progress block now broadens the completion exemption: at iter ≥ 2 with
// unchanged HEAD, when there is a committed result AND the prior reviewer verdict
// is REQUEST_CHANGES (advisory severity — NOT a BLOCK, per the hk-cmry
// BLOCK>RC>APPROVE severity-join) AND the most-recent build/test gate passed,
// the run COMPLETES instead of failing. Genuinely-stalled rework still fails: a
// REQUEST_CHANGES whose gate is RED (build/test failure still un-addressed), or a
// BLOCK verdict, with unchanged HEAD STILL fails as before.
//
// # The two labelled scenarios (acceptance gate)
//
//   - advisory-complete: REQUEST_CHANGES iter-1 (advisory) + GREEN gate + NO new
//     commit at iter-2 → the run COMPLETES (success); review_fixup_stalled /
//     no_progress_detected must NOT fire.
//   - blocking-fail:     REQUEST_CHANGES iter-1 + a RED gate at iter-2 (build/test
//     failure) + NO new commit → the run STILL FAILS (review_fixup_stalled).
//
// # Spec refs
//   - specs/execution-model.md §4.3 EM-015e (no-progress early-exit)
//   - docs/issues/no-progress-guard-fails-one-shot-beads.md (PR #3, fix #3)
//
// Bead: hk-w2ow. Builds on hk-togxq (the guard) and hk-8ps7q (the APPROVE
// exemption this generalizes).

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

// w2owGatedLoopDOT returns the standard-bead topology (implement → commit_gate →
// review) with a parameterised commit_gate tool_command, so a test can make the
// gate pass or fail on a chosen cycle. Mirrors specs/examples/standard-bead.dot:
// gate SUCCESS → review; gate deterministic FAIL → implement (cap 3); review
// APPROVE → close; review REQUEST_CHANGES → implement (cap 3); BLOCK / fallback
// → close-needs-attention.
func w2owGatedLoopDOT(gateCommand string) string {
	return fmt.Sprintf(`digraph "hk-w2ow-gated-loop" {
    schema_version="1"; version="1.0"; workflow_id="hk-w2ow-gated-loop";
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
    commit_gate -> commit_gate [condition="outcome.status == 'FAIL' && outcome.failure_class == 'transient'", traversal_cap="2"];
    commit_gate -> "close-needs-attention";
    review -> close [condition="outcome.preferred_label == 'APPROVE'"];
    review -> implement [condition="outcome.preferred_label == 'REQUEST_CHANGES'", traversal_cap="3"];
    review -> "close-needs-attention" [condition="outcome.preferred_label == 'BLOCK'"];
    review -> "close-needs-attention";
}
`, gateCommand)
}

// w2owWriteDOT materialises a DOT graph to a temp file and loads it.
func w2owWriteDOT(t *testing.T, src string) *dot.Graph {
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

// w2owAdvisoryScript drives the agentic nodes (implement + review):
//
//	invocation 1 (implement iter 1): commits real work → HEAD advances past parent.
//	invocation 2 (review    iter 1): REQUEST_CHANGES → back-edge to implement.
//	invocation 3 (implement iter 2): NO commit (advisory feedback) → HEAD unchanged.
//	(no invocation 4: the no-progress check fires at the review re-entry — but
//	 because the prior verdict was REQUEST_CHANGES, committed work exists, and the
//	 gate is GREEN, the run COMPLETES via the hk-w2ow exemption.)
func w2owAdvisoryScript(t *testing.T, wtPath string) string {
	t.Helper()
	wtpEsc := strings.ReplaceAll(wtPath, "'", "'\\''")
	rc := strings.ReplaceAll(rlFixtureVerdictJSON("REQUEST_CHANGES"), "'", "'\\''")
	script := fmt.Sprintf(`#!/bin/sh
set -e
WTP='%s'
WS="${HARMONIK_WORKSPACE_PATH:-$WTP}"
CNT_FILE="$WTP/.harmonik/w2ow_adv_count"
if [ ! -f "$CNT_FILE" ]; then printf '0' > "$CNT_FILE"; fi
CNT=$(cat "$CNT_FILE"); CNT=$((CNT + 1)); printf '%%d' "$CNT" > "$CNT_FILE"
case "$CNT" in
  1)
    printf 'w2ow work' > "$WS/w2ow_adv.txt"
    git -C "$WS" add "w2ow_adv.txt" >/dev/null 2>&1
    git -C "$WS" -c user.email=test@harmonik.local -c user.name="Test" commit -m "w2ow impl iter1" --no-gpg-sign >/dev/null 2>&1
    ;;
  2)
    # Reviewer iter 1: advisory-only REQUEST_CHANGES -> back-edge to implement.
    mkdir -p "$WS/.harmonik"; printf '%%s' '%s' > "$WS/.harmonik/review.json"
    ;;
  3)
    # Implementer iter 2: NO commit (feedback was advisory) -> HEAD unchanged.
    ;;
  *)
    exit 1 ;;
esac
exit 0
`, wtpEsc, rc)
	return w2owWriteScript(t, "w2ow_advisory.sh", script)
}

// TestAdvisoryRequestChanges_GreenGate_Completes_hkw2ow is the advisory-complete
// regression: an advisory-only REQUEST_CHANGES at iter ≥ 2 with unchanged HEAD
// and a GREEN gate must COMPLETE (success) — NOT review_fixup_stalled and strand
// the finished, tested work.
func TestAdvisoryRequestChanges_GreenGate_Completes_hkw2ow(t *testing.T) {
	t.Parallel()
	projectDir, wtPath, parentSHA := rlcFixtureSetup(t)
	scriptPath := w2owAdvisoryScript(t, wtPath)
	// Gate always passes (build/tests green).
	graph := w2owWriteDOT(t, w2owGatedLoopDOT("true"))

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
		core.BeadID("dot-w2ow-advisory"), wtPath, parentSHA, graph)

	events := collector.eventTypes()
	t.Logf("advisory-complete: result=%+v events=%v", result, events)

	if !result.Success {
		t.Errorf("hk-w2ow: expected success=true (advisory RC + green gate completes); summary=%q", result.Summary)
	}
	if result.NeedsAttention {
		t.Errorf("hk-w2ow: expected needs_attention=false; summary=%q", result.Summary)
	}
	// Neither stall signal may have fired — the advisory commit is final.
	for _, et := range events {
		if et == string(core.EventTypeReviewFixupStalled) {
			t.Errorf("hk-w2ow: review_fixup_stalled false-fired on advisory RC + green gate; events=%v summary=%q", events, result.Summary)
		}
		if et == string(core.EventTypeNoProgressDetected) {
			t.Errorf("hk-w2ow: no_progress_detected false-fired on advisory RC + green gate; events=%v summary=%q", events, result.Summary)
		}
	}
	// The reviewer DID run (and returned REQUEST_CHANGES).
	rlAssertEventPresent(t, events, string(core.EventTypeReviewerVerdict))
}

// w2owBlockingScript drives the agentic nodes for the blocking-fail path:
//
//	invocation 1 (implement iter 1): commits real work → HEAD advances past parent.
//	invocation 2 (review    iter 1): REQUEST_CHANGES → back-edge to implement.
//	invocation 3 (implement iter 2): NO commit → HEAD unchanged.
//	(no invocation 4: with the gate RED on cycle 2, commit_gate routes back to
//	 implement, and the no-progress check fires at the implement re-entry and
//	 FAILS as stalled rework.)
func w2owBlockingScript(t *testing.T, wtPath string) string {
	t.Helper()
	wtpEsc := strings.ReplaceAll(wtPath, "'", "'\\''")
	rc := strings.ReplaceAll(rlFixtureVerdictJSON("REQUEST_CHANGES"), "'", "'\\''")
	script := fmt.Sprintf(`#!/bin/sh
set -e
WTP='%s'
WS="${HARMONIK_WORKSPACE_PATH:-$WTP}"
CNT_FILE="$WTP/.harmonik/w2ow_blk_count"
if [ ! -f "$CNT_FILE" ]; then printf '0' > "$CNT_FILE"; fi
CNT=$(cat "$CNT_FILE"); CNT=$((CNT + 1)); printf '%%d' "$CNT" > "$CNT_FILE"
case "$CNT" in
  1)
    printf 'w2ow blk work' > "$WS/w2ow_blk.txt"
    git -C "$WS" add "w2ow_blk.txt" >/dev/null 2>&1
    git -C "$WS" -c user.email=test@harmonik.local -c user.name="Test" commit -m "w2ow blk impl iter1" --no-gpg-sign >/dev/null 2>&1
    ;;
  2)
    mkdir -p "$WS/.harmonik"; printf '%%s' '%s' > "$WS/.harmonik/review.json"
    ;;
  3)
    # Implementer iter 2: NO commit -> HEAD unchanged; the gate then goes RED.
    ;;
  *)
    exit 1 ;;
esac
exit 0
`, wtpEsc, rc)
	return w2owWriteScript(t, "w2ow_blocking.sh", script)
}

// TestRequestChanges_RedGate_StillFails_hkw2ow is the blocking-fail guard: a
// REQUEST_CHANGES whose build/test gate is RED at iter ≥ 2 (real, un-addressed
// work) with unchanged HEAD MUST STILL FAIL (review_fixup_stalled). The
// broadened hk-w2ow exemption must NOT rescue genuinely-stalled rework.
func TestRequestChanges_RedGate_StillFails_hkw2ow(t *testing.T) {
	t.Parallel()
	projectDir, wtPath, parentSHA := rlcFixtureSetup(t)
	scriptPath := w2owBlockingScript(t, wtPath)
	// Gate passes the FIRST time (cycle 1, reaches review→RC) then FAILS
	// deterministically on every subsequent call (cycle 2 → RED).
	gateCmd := "C=$(cat .harmonik/w2ow_gate 2>/dev/null || echo 0); C=$((C+1)); echo $C > .harmonik/w2ow_gate; [ $C -le 1 ]"
	graph := w2owWriteDOT(t, w2owGatedLoopDOT(gateCmd))

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
		core.BeadID("dot-w2ow-blocking"), wtPath, parentSHA, graph)

	events := collector.eventTypes()
	t.Logf("blocking-fail: result=%+v events=%v", result, events)

	if result.Success {
		t.Errorf("hk-w2ow: expected success=false (RC + RED gate is stalled rework); summary=%q", result.Summary)
	}
	if !result.NeedsAttention {
		t.Errorf("hk-w2ow: expected needs_attention=true; summary=%q", result.Summary)
	}
	if !strings.Contains(result.Summary, "fix-up stalled") && !strings.Contains(result.Summary, "no-progress") {
		t.Errorf("hk-w2ow: expected summary to report stalled rework; got %q", result.Summary)
	}
	// The stalled-rework signal MUST have fired (prior verdict REQUEST_CHANGES).
	rlAssertEventPresent(t, events, string(core.EventTypeReviewFixupStalled))
}

// w2owWriteScript writes a /bin/sh fixture script to a temp dir and returns its path.
func w2owWriteScript(t *testing.T, name, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	//nolint:gosec // G306: test-only fixture script; not production
	if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
		t.Fatalf("w2owWriteScript(%s): %v", name, err)
	}
	return p
}

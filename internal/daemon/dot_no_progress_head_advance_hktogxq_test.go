package daemon_test

// dot_no_progress_head_advance_hktogxq_test.go — regression tests for hk-togxq.
//
// # The bug
//
// The DOT-cascade (and the review-loop) no-progress detector (EM-015e) compared
// `git diff parentSHA..HEAD` hashes across agentic-node entries and hard-failed
// at iteration ≥ 2 whenever the *cumulative* diff from parentSHA was unchanged.
// That test was VERDICT-BLIND and HEAD-BLIND:
//
//   - It false-flagged a run whose iter-N commit advanced HEAD but produced the
//     SAME NET parent..HEAD diff as a prior commit (e.g. delete-then-readd, or a
//     commit that nets to the same tree). Real new work was discarded as
//     "no-progress", stranded on the run branch (MODE B, run controlpoints /
//     019eadd1).
//   - Downstream, workloop only merges when dotResult.success==true, so the
//     false failure discarded the committed work entirely.
//
// # The fix (hk-togxq)
//
// Progress is now measured by COMMIT/HEAD ADVANCEMENT across iterations: the
// detector records the worktree HEAD at each agentic entry (priorIterHeadSHA in
// driveDotWorkflow; state.lastIterHeadSHA in runReviewLoop) and fires no_progress
// at iteration ≥ 2 ONLY when HEAD did NOT advance since the prior entry — i.e.
// the intervening implementer produced no new commit. lastDiffHash is retained
// only for the no_progress_detected event payload.
//
// # The three labelled scenarios (acceptance gate)
//
//   - mode-A:        committed iter-1 work + HEAD advanced past parent + reaches
//     a reviewer at iter-1 → the no-progress check must NOT pre-empt the reviewer
//     / discard the committed work; the run flows to review-then-(close/merge).
//   - mode-B:        REQUEST_CHANGES iter-1 + a REAL new iter-2 commit that
//     ADVANCES HEAD but whose net parent..HEAD diff COLLIDES with iter-1's →
//     no_progress must NOT false-fire; the run continues / re-reviews.
//   - negative-guard: REQUEST_CHANGES iter-1 + NO new commit at iter-2 (HEAD does
//     NOT advance) → no_progress MUST STILL fire; rejected/un-addressed work is
//     never merged. (Mirrors the pre-existing TestDotCascade_NoProgressDetected;
//     re-asserted here so the three modes live together.)
//
// # Spec refs
//   - specs/execution-model.md §4.3 EM-015e (no-progress early-exit)
//   - specs/event-model.md §8.1a.5 (no_progress_detected; workflow_mode="dot")
//
// Bead: hk-togxq.

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

// togxqReviewLoopDOT returns the standard implement→review review-loop topology
// (no commit_gate): start → implement → review, with the verdict cascade
// review→close (APPROVE), review→implement (REQUEST_CHANGES, cap 3),
// review→close-needs-attention (BLOCK / fallback).
func togxqReviewLoopDOT() string {
	return `digraph "hk-togxq-review-loop" {
    schema_version="1"; version="1.0"; workflow_id="hk-togxq-review-loop";
    start_node="start"; terminal_node_ids="close,close-needs-attention";

    start [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];
    implement [type="agentic", agent_type="implementer", handler_ref="claude-implementer", idempotency_class="non-idempotent"];
    review [type="agentic", agent_type="reviewer", handler_ref="claude-reviewer", idempotency_class="idempotent"];
    close [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];
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

// togxqWriteDOT materialises a DOT graph to a temp file and loads it.
func togxqWriteDOT(t *testing.T, src string) *dot.Graph {
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

// ── mode-A ───────────────────────────────────────────────────────────────────

// togxqModeAScript: iter-1 implementer COMMITS (HEAD advances past parent), then
// the reviewer APPROVES. Proves the no-progress check does NOT pre-empt the
// reviewer at the entry where HEAD has already advanced (committed work is not
// discarded; the run flows to the success terminal so workloop would merge it).
func togxqModeAScript(t *testing.T, wtPath string) string {
	t.Helper()
	wtpEsc := strings.ReplaceAll(wtPath, "'", "'\\''")
	approve := strings.ReplaceAll(rlFixtureVerdictJSON("APPROVE"), "'", "'\\''")
	script := fmt.Sprintf(`#!/bin/sh
set -e
WTP='%s'
WS="${HARMONIK_WORKSPACE_PATH:-$WTP}"
CNT_FILE="$WTP/.harmonik/togxq_a_count"
if [ ! -f "$CNT_FILE" ]; then printf '0' > "$CNT_FILE"; fi
CNT=$(cat "$CNT_FILE"); CNT=$((CNT + 1)); printf '%%d' "$CNT" > "$CNT_FILE"
case "$CNT" in
  1)
    # Implementer iter 1: commit real work -> HEAD advances past parent.
    printf 'modeA work' > "$WS/togxq_a.txt"
    git -C "$WS" add "togxq_a.txt" >/dev/null 2>&1
    git -C "$WS" -c user.email=test@harmonik.local -c user.name="Test" commit -m "modeA impl iter1" --no-gpg-sign >/dev/null 2>&1
    ;;
  2)
    # Reviewer iter 1: APPROVE -> run reaches close terminal (merge path).
    mkdir -p "$WS/.harmonik"
    printf '%%s' '%s' > "$WS/.harmonik/review.json"
    ;;
  *)
    exit 1 ;;
esac
exit 0
`, wtpEsc, approve)
	return togxqWriteScript(t, "togxq_modeA.sh", script)
}

// TestNoProgress_ModeA_CommittedWorkReachesReview_hktogxq is the mode-A
// regression: a run that committed iter-1 work (HEAD advanced) must NOT be
// pre-empted by the no-progress check; it flows to review and, on APPROVE,
// reaches the success terminal so the committed work can merge.
func TestNoProgress_ModeA_CommittedWorkReachesReview_hktogxq(t *testing.T) {
	t.Parallel()
	projectDir, wtPath, parentSHA := rlcFixtureSetup(t)
	scriptPath := togxqModeAScript(t, wtPath)
	graph := togxqWriteDOT(t, togxqReviewLoopDOT())

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
		core.BeadID("dot-togxq-modeA"), wtPath, parentSHA, graph)

	events := collector.eventTypes()
	t.Logf("mode-A: result=%+v events=%v", result, events)

	if !result.Success {
		t.Errorf("mode-A: expected success=true (committed work reaches review→close); summary=%q", result.Summary)
	}
	if result.TerminalNodeID != "close" {
		t.Errorf("mode-A: expected terminal node \"close\"; got %q (summary=%q)", result.TerminalNodeID, result.Summary)
	}
	// The no-progress check must NOT have pre-empted the reviewer.
	for _, et := range events {
		if et == string(core.EventTypeNoProgressDetected) {
			t.Errorf("mode-A: no_progress_detected fired but iter-1 work was committed (HEAD advanced); events=%v", events)
		}
	}
	// The reviewer DID run.
	rlAssertEventPresent(t, events, string(core.EventTypeReviewerVerdict))
}

// ── mode-B ─────────────────────────────────────────────────────────────────--

// togxqModeBScript reproduces the controlpoints (019eadd1) failure shape:
//   - iter-1 implementer commits file with content "v1" (commit C1).
//   - reviewer iter-1: REQUEST_CHANGES -> back-edge to implementer.
//   - iter-2 implementer makes a NEW commit (C2, HEAD advances C1->C2) whose NET
//     `git diff parent..C2` is IDENTICAL to `git diff parent..C1` — it deletes
//     the file and re-adds it with the same "v1" content in a fresh commit. This
//     is the diff-hash COLLISION that the old detector false-flagged.
//   - reviewer iter-2: APPROVE -> reaches close.
//
// The fix must see HEAD advanced (C1 != C2) and NOT fire no_progress, so the
// run re-reviews and (here) APPROVEs.
func togxqModeBScript(t *testing.T, wtPath string) string {
	t.Helper()
	wtpEsc := strings.ReplaceAll(wtPath, "'", "'\\''")
	rc := strings.ReplaceAll(rlFixtureVerdictJSON("REQUEST_CHANGES"), "'", "'\\''")
	approve := strings.ReplaceAll(rlFixtureVerdictJSON("APPROVE"), "'", "'\\''")
	script := fmt.Sprintf(`#!/bin/sh
set -e
WTP='%s'
WS="${HARMONIK_WORKSPACE_PATH:-$WTP}"
CNT_FILE="$WTP/.harmonik/togxq_b_count"
if [ ! -f "$CNT_FILE" ]; then printf '0' > "$CNT_FILE"; fi
CNT=$(cat "$CNT_FILE"); CNT=$((CNT + 1)); printf '%%d' "$CNT" > "$CNT_FILE"
case "$CNT" in
  1)
    # Implementer iter 1: commit file "togxq_b.txt"=v1 (commit C1).
    printf 'v1' > "$WS/togxq_b.txt"
    git -C "$WS" add "togxq_b.txt" >/dev/null 2>&1
    git -C "$WS" -c user.email=test@harmonik.local -c user.name="Test" commit -m "modeB impl iter1 (C1)" --no-gpg-sign >/dev/null 2>&1
    ;;
  2)
    # Reviewer iter 1: REQUEST_CHANGES -> loop back to implementer.
    mkdir -p "$WS/.harmonik"; printf '%%s' '%s' > "$WS/.harmonik/review.json"
    ;;
  3)
    # Implementer iter 2: NEW commit C2 that ADVANCES HEAD but whose net
    # parent..HEAD diff COLLIDES with C1 (delete + re-add same content).
    git -C "$WS" rm "togxq_b.txt" >/dev/null 2>&1
    git -C "$WS" -c user.email=test@harmonik.local -c user.name="Test" commit -m "modeB impl iter2 part1 (remove)" --no-gpg-sign >/dev/null 2>&1
    printf 'v1' > "$WS/togxq_b.txt"
    git -C "$WS" add "togxq_b.txt" >/dev/null 2>&1
    git -C "$WS" -c user.email=test@harmonik.local -c user.name="Test" commit -m "modeB impl iter2 part2 (readd v1) (C2)" --no-gpg-sign >/dev/null 2>&1
    ;;
  4)
    # Reviewer iter 2: APPROVE -> reaches close.
    mkdir -p "$WS/.harmonik"; printf '%%s' '%s' > "$WS/.harmonik/review.json"
    ;;
  *)
    exit 1 ;;
esac
exit 0
`, wtpEsc, rc, approve)
	return togxqWriteScript(t, "togxq_modeB.sh", script)
}

// TestNoProgress_ModeB_RealCommitDiffCollision_hktogxq is the mode-B regression:
// an iter-2 commit that advances HEAD but whose net parent..HEAD diff collides
// with iter-1's must NOT be false-flagged as no-progress. The run re-reviews.
func TestNoProgress_ModeB_RealCommitDiffCollision_hktogxq(t *testing.T) {
	t.Parallel()
	projectDir, wtPath, parentSHA := rlcFixtureSetup(t)
	scriptPath := togxqModeBScript(t, wtPath)
	graph := togxqWriteDOT(t, togxqReviewLoopDOT())

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
		core.BeadID("dot-togxq-modeB"), wtPath, parentSHA, graph)

	events := collector.eventTypes()
	t.Logf("mode-B: result=%+v events=%v", result, events)

	// The whole point: no_progress must NOT fire even though the iter-2 commit's
	// net diff collides with iter-1's. The run advances and (here) APPROVEs.
	for _, et := range events {
		if et == string(core.EventTypeNoProgressDetected) {
			t.Errorf("mode-B: no_progress_detected false-fired — HEAD advanced (C1->C2) but the diff hash collided; events=%v summary=%q", events, result.Summary)
		}
	}
	if !result.Success {
		t.Errorf("mode-B: expected success=true (re-reviewed → APPROVE → close); got summary=%q", result.Summary)
	}
	if result.TerminalNodeID != "close" {
		t.Errorf("mode-B: expected terminal node \"close\"; got %q", result.TerminalNodeID)
	}
	// Both reviewer iterations ran (REQUEST_CHANGES then APPROVE).
	rlAssertEventPresent(t, events, string(core.EventTypeReviewerVerdict))
}

// ── negative-guard ─────────────────────────────────────────────────────────--

// togxqNegGuardScript: iter-1 implementer commits (HEAD advances), reviewer
// REQUEST_CHANGES, iter-2 implementer makes NO commit (HEAD does NOT advance).
// no_progress MUST fire.
func togxqNegGuardScript(t *testing.T, wtPath string) string {
	t.Helper()
	wtpEsc := strings.ReplaceAll(wtPath, "'", "'\\''")
	rc := strings.ReplaceAll(rlFixtureVerdictJSON("REQUEST_CHANGES"), "'", "'\\''")
	script := fmt.Sprintf(`#!/bin/sh
set -e
WTP='%s'
WS="${HARMONIK_WORKSPACE_PATH:-$WTP}"
CNT_FILE="$WTP/.harmonik/togxq_ng_count"
if [ ! -f "$CNT_FILE" ]; then printf '0' > "$CNT_FILE"; fi
CNT=$(cat "$CNT_FILE"); CNT=$((CNT + 1)); printf '%%d' "$CNT" > "$CNT_FILE"
case "$CNT" in
  1)
    printf 'ng work' > "$WS/togxq_ng.txt"
    git -C "$WS" add "togxq_ng.txt" >/dev/null 2>&1
    git -C "$WS" -c user.email=test@harmonik.local -c user.name="Test" commit -m "ng impl iter1" --no-gpg-sign >/dev/null 2>&1
    ;;
  2)
    mkdir -p "$WS/.harmonik"; printf '%%s' '%s' > "$WS/.harmonik/review.json"
    ;;
  3)
    # Implementer iter 2: NO commit -> HEAD unchanged -> genuinely stuck.
    ;;
  *)
    exit 1 ;;
esac
exit 0
`, wtpEsc, rc)
	return togxqWriteScript(t, "togxq_negguard.sh", script)
}

// TestNoProgress_NegativeGuard_NoCommitAfterRequestChanges_hktogxq is the
// negative guard: REQUEST_CHANGES iter-1 + NO new commit at iter-2 (HEAD does
// not advance) MUST still fire no_progress and fail. Rejected/un-addressed work
// is never merged.
func TestNoProgress_NegativeGuard_NoCommitAfterRequestChanges_hktogxq(t *testing.T) {
	t.Parallel()
	projectDir, wtPath, parentSHA := rlcFixtureSetup(t)
	scriptPath := togxqNegGuardScript(t, wtPath)
	graph := togxqWriteDOT(t, togxqReviewLoopDOT())

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
		core.BeadID("dot-togxq-negguard"), wtPath, parentSHA, graph)

	events := collector.eventTypes()
	t.Logf("negative-guard: result=%+v events=%v", result, events)

	if result.Success {
		t.Errorf("negative-guard: expected success=false (no commit after REQUEST_CHANGES); summary=%q", result.Summary)
	}
	if !result.NeedsAttention {
		t.Errorf("negative-guard: expected needs_attention=true; summary=%q", result.Summary)
	}
	if !strings.Contains(result.Summary, "no-progress") {
		t.Errorf("negative-guard: expected summary to report no-progress; got %q", result.Summary)
	}
	rlAssertEventPresent(t, events, string(core.EventTypeNoProgressDetected))
}

// togxqWriteScript writes a /bin/sh fixture script to a temp dir and returns its path.
func togxqWriteScript(t *testing.T, name, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	//nolint:gosec // G306: test-only fixture script; not production
	if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
		t.Fatalf("togxqWriteScript(%s): %v", name, err)
	}
	return p
}

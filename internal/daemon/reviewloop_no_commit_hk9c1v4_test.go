package daemon_test

// reviewloop_no_commit_hk9c1v4_test.go — scenario test for hk-9c1v4.
//
// # Bug context
//
// On 2026-05-21 the dispatch `harmonik run --beads hk-xegej,...` exhibited
// the following failure mode: the implementer claude never committed
// (run_id 019e4d47-1626-755f-bfcb-9d30d96101d4 on bead hk-xegej). The
// pasteinject quit-on-commit noChange-timeout eventually fired (~10 min).
// Despite no commit being made, the daemon emitted `reviewer_launched`
// with `claude_session_id="synthetic-claude-session-..."` and dispatched
// the reviewer — which then crashed with "structural invariant violated:
// task file absent: review-target.md (skipping inject)".
//
// Root cause: runReviewLoop discards the implementer wait outcome and
// proceeds straight to diff-hash computation and reviewer dispatch
// regardless of whether the implementer advanced HEAD. The no-progress
// guard (EM-015e) only fires on iteration ≥ 2, so iteration 1 with an
// empty diff still launches the reviewer.
//
// # Expected behaviour
//
// On iteration 1, if the implementer phase exits without advancing the
// worktree HEAD past parentSHA, the review loop MUST short-circuit:
//
//   - return a non-success reviewLoopResult with completionReason=error
//     and needsAttention=true.
//   - emit review_loop_cycle_complete (terminal) before returning.
//   - NEVER emit reviewer_launched.
//   - NEVER launch the reviewer subprocess.
//
// # What this test asserts (BEFORE fix it FAILS)
//
//  1. result.Success == false
//  2. result.CompletionReason == "error"
//  3. result.NeedsAttention == true
//  4. reviewer_launched MUST NOT be emitted.
//  5. review_loop_cycle_complete IS emitted.
//
// Helper prefix: `ncv4` (per implementer-protocol §Helper-prefix discipline).
//
// Bead: hk-9c1v4.

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
)

// ncv4HandlerScriptNoCommit writes a handler that:
//   - Exits 0 immediately on the implementer invocation (CNT=1) WITHOUT
//     committing anything to the worktree. HEAD stays equal to parentSHA.
//   - If somehow invoked a second time (reviewer), writes an APPROVE verdict
//     — this branch MUST NOT execute, because the fix is to never launch the
//     reviewer when the implementer made no commit. The test asserts the
//     review.json file does NOT exist after the run.
func ncv4HandlerScriptNoCommit(t *testing.T, wtPath string) string {
	t.Helper()
	wtpEsc := strings.ReplaceAll(wtPath, "'", "'\\''")
	script := fmt.Sprintf(`#!/bin/sh
set -e
WTP='%s'
CNT_FILE="$WTP/.harmonik/ncv4_count"
if [ ! -f "$CNT_FILE" ]; then printf '0' > "$CNT_FILE"; fi
CNT=$(cat "$CNT_FILE")
CNT=$((CNT + 1))
printf '%%d' "$CNT" > "$CNT_FILE"
if [ "$CNT" -eq 1 ]; then
  # Implementer: exit without committing — HEAD stays at parentSHA.
  exit 0
fi
# Any subsequent invocation = reviewer; emit APPROVE so we can detect the
# regression (test will fail because review.json should never appear).
printf '{"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"ncv4 reviewer must NOT have run"}' > "$WTP/.harmonik/review.json"
exit 0
`, wtpEsc)
	scriptPath := filepath.Join(t.TempDir(), "ncv4_handler.sh")
	//nolint:gosec // G306: test-only fixture script
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("ncv4HandlerScriptNoCommit: WriteFile: %v", err)
	}
	return scriptPath
}

// TestReviewLoop_NoCommit_FailsRun_DoesNotLaunchReviewer_Hk9c1v4 is the
// load-bearing scenario test for hk-9c1v4. It MUST fail on parent commit
// 4ea1deb (reviewer_launched fires despite no commit) and pass after the
// fix (run terminates with error completionReason; reviewer never launches).
//
// Spec ref: specs/execution-model.md §4.3 EM-015d (implementer phase MUST
// advance HEAD before the daemon launches the reviewer).
// Bead: hk-9c1v4.
func TestReviewLoop_NoCommit_FailsRun_DoesNotLaunchReviewer_Hk9c1v4(t *testing.T) {
	t.Parallel()

	projectDir := rlFixtureProjectDir(t)
	rlFixtureGitRepo(t, projectDir)
	wtPath, parentSHA := rlFixtureWorktree(t, projectDir)

	scriptPath := ncv4HandlerScriptNoCommit(t, wtPath)

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
		WorkflowModeDefault: core.WorkflowModeReviewLoop,
	})

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	result := daemon.ExportedRunReviewLoop(
		ctx, deps,
		rlFixtureRunID(t),
		core.BeadID("ncv4-no-commit-001"),
		wtPath, parentSHA,
	)

	eventTypes := collector.eventTypes()

	// Assertion 1: result MUST be a failure.
	if result.Success {
		t.Errorf("hk-9c1v4 FAIL: result.Success = true; want false (no commit means no review-loop progress). summary=%q events=%v",
			result.Summary, eventTypes)
	}

	// Assertion 2: completion_reason must be "error".
	if result.CompletionReason != string(core.ReviewLoopCompletionReasonError) {
		t.Errorf("hk-9c1v4 FAIL: completion_reason = %q; want %q. events=%v",
			result.CompletionReason, core.ReviewLoopCompletionReasonError, eventTypes)
	}

	// Assertion 3: needs-attention must be set.
	if !result.NeedsAttention {
		t.Errorf("hk-9c1v4 FAIL: NeedsAttention = false; want true (no-commit failure is operator-visible)")
	}

	// Assertion 4 (the load-bearing one): reviewer_launched MUST NOT fire.
	// This is the exact regression: pre-fix, the review loop advances past
	// the no-commit implementer and emits reviewer_launched anyway.
	for _, et := range eventTypes {
		if et == string(core.EventTypeReviewerLaunched) {
			t.Errorf("hk-9c1v4 FAIL: reviewer_launched emitted despite implementer making no commit; "+
				"all events: %v", eventTypes)
			break
		}
	}

	// Assertion 5: review_loop_cycle_complete must be the terminal review-loop
	// event (matches the existing cycle_complete-is-terminal contract).
	foundCycleComplete := false
	for _, et := range eventTypes {
		if et == string(core.EventTypeReviewLoopCycleComplete) {
			foundCycleComplete = true
			break
		}
	}
	if !foundCycleComplete {
		t.Errorf("hk-9c1v4 FAIL: review_loop_cycle_complete not emitted; events=%v", eventTypes)
	}

	// Assertion 6: review.json must NOT exist — the reviewer must never have run.
	reviewPath := filepath.Join(wtPath, ".harmonik", "review.json")
	if _, err := os.Stat(reviewPath); err == nil {
		t.Errorf("hk-9c1v4 FAIL: review.json exists at %q — reviewer ran despite no implementer commit", reviewPath)
	} else if !os.IsNotExist(err) {
		t.Fatalf("ncv4: stat review.json: %v", err)
	}
}

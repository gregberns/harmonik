package daemon_test

// reviewloop_cycle_complete_hk7om2q24_test.go — T-WM-024 acceptance tests.
//
// Verifies that review_loop_cycle_complete is emitted exactly once per cycle
// on all five termination paths and that it is the last review-loop event
// emitted before runReviewLoop returns (ensuring it precedes any terminal
// run_completed / run_failed that callers emit after the return).
//
// The five paths under test:
//   1. approved       — APPROVE verdict on first iteration.
//   2. cap_hit        — REQUEST_CHANGES × 3 (cap exhausted).
//   3. blocked        — BLOCK verdict.
//   4. no_progress    — identical diff hash on iteration 2 (T-WM-022 path).
//   5. error          — reviewer produces malformed verdict file.
//
// Helper prefix: rlcFixture (per implementer-protocol.md §Helper-prefix
// discipline; bead hk-7om2q.24).
//
// Bead: hk-7om2q.24.

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

// ─────────────────────────────────────────────────────────────────────────────
// Fixtures
// ─────────────────────────────────────────────────────────────────────────────

// rlcFixtureSetup creates a fresh project dir, git repo, and worktree for one
// test case. Returns wtPath and parentSHA. Cleanup is registered on t.
func rlcFixtureSetup(t *testing.T) (projectDir, wtPath, parentSHA string) {
	t.Helper()
	projectDir = rlFixtureProjectDir(t)
	rlFixtureGitRepo(t, projectDir)
	wtPath, parentSHA = rlFixtureWorktree(t, projectDir)
	return projectDir, wtPath, parentSHA
}

// rlcFixtureMalformedVerdictScript writes a handler script that:
//   - On odd invocations (implementer): commits a unique file to advance HEAD.
//   - On even invocations (reviewer): writes a syntactically invalid JSON file
//     to .harmonik/review.json, causing workspace.ReadReviewVerdict to fail.
func rlcFixtureMalformedVerdictScript(t *testing.T, wtPath string) string {
	t.Helper()
	wtpEsc := strings.ReplaceAll(wtPath, "'", "'\\''")
	script := fmt.Sprintf(`#!/bin/sh
set -e
WTP='%s'
WS="${HARMONIK_WORKSPACE_PATH:-$WTP}"
CNT_FILE="$WTP/.harmonik/rl_count"
if [ ! -f "$CNT_FILE" ]; then
  printf '0' > "$CNT_FILE"
fi
CNT=$(cat "$CNT_FILE")
CNT=$((CNT + 1))
printf '%%d' "$CNT" > "$CNT_FILE"
if [ $((CNT %% 2)) -eq 0 ]; then
  # Reviewer invocation: write malformed JSON to reviewer's workspace (hk-dut6b).
  mkdir -p "$WS/.harmonik"
  printf 'NOT-VALID-JSON' > "$WS/.harmonik/review.json"
else
  # Implementer invocation: commit a file to advance HEAD.
  printf '%%d' "$CNT" > "$WS/impl_err_$CNT.txt"
  git -C "$WS" add "impl_err_$CNT.txt" >/dev/null 2>&1
  git -C "$WS" -c user.email=test@harmonik.local -c user.name="Test" commit -m "impl err $CNT" --no-gpg-sign >/dev/null 2>&1
fi
exit 0
`, wtpEsc)

	scriptPath := filepath.Join(t.TempDir(), "rlc_error_handler.sh")
	//nolint:gosec // G306: test-only fixture script; not production
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("rlcFixtureMalformedVerdictScript: WriteFile: %v", err)
	}
	return scriptPath
}

// rlcFixtureNoProgressScript writes a handler script where the implementer does
// NOT commit any new files on iteration 2, leaving HEAD unchanged so the diff
// hash equals the prior iteration's hash, triggering no-progress detection.
//
// Iteration sequence:
//
//	invocation 1 (implementer iter 1): commits a file.
//	invocation 2 (reviewer iter 1):    writes REQUEST_CHANGES verdict.
//	invocation 3 (implementer iter 2): exits 0 without committing → same HEAD.
//	(no invocation 4: daemon detects no-progress before launching reviewer)
func rlcFixtureNoProgressScript(t *testing.T, wtPath string) string {
	t.Helper()
	wtpEsc := strings.ReplaceAll(wtPath, "'", "'\\''")
	rcVerdict := strings.ReplaceAll(rlFixtureVerdictJSON("REQUEST_CHANGES"), "'", "'\\''")
	script := fmt.Sprintf(`#!/bin/sh
set -e
WTP='%s'
WS="${HARMONIK_WORKSPACE_PATH:-$WTP}"
CNT_FILE="$WTP/.harmonik/rl_count"
if [ ! -f "$CNT_FILE" ]; then
  printf '0' > "$CNT_FILE"
fi
CNT=$(cat "$CNT_FILE")
CNT=$((CNT + 1))
printf '%%d' "$CNT" > "$CNT_FILE"
case "$CNT" in
  1)
    # Implementer iter 1: commit a file so HEAD advances (non-empty diff).
    printf '1' > "$WS/impl_np_1.txt"
    git -C "$WS" add "impl_np_1.txt" >/dev/null 2>&1
    git -C "$WS" -c user.email=test@harmonik.local -c user.name="Test" commit -m "impl iter 1" --no-gpg-sign >/dev/null 2>&1
    ;;
  2)
    # Reviewer iter 1: REQUEST_CHANGES so we loop. Write to reviewer's workspace (hk-dut6b).
    mkdir -p "$WS/.harmonik"
    printf '%%s' '%s' > "$WS/.harmonik/review.json"
    ;;
  3)
    # Implementer iter 2: exit without committing — HEAD stays the same.
    # Daemon detects same diff hash and emits no_progress_detected.
    ;;
  *)
    # Should not be reached.
    exit 1
    ;;
esac
exit 0
`, wtpEsc, rcVerdict)

	scriptPath := filepath.Join(t.TempDir(), "rlc_noprogress_handler.sh")
	//nolint:gosec // G306: test-only fixture script; not production
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("rlcFixtureNoProgressScript: WriteFile: %v", err)
	}
	return scriptPath
}

// ─────────────────────────────────────────────────────────────────────────────
// Assertion helpers (T-WM-024 specific)
// ─────────────────────────────────────────────────────────────────────────────

// rlcAssertCycleCompleteExactlyOnce asserts that review_loop_cycle_complete
// appears exactly once in the emitted event list.
func rlcAssertCycleCompleteExactlyOnce(t *testing.T, types []string) {
	t.Helper()
	count := 0
	for _, et := range types {
		if et == string(core.EventTypeReviewLoopCycleComplete) {
			count++
		}
	}
	if count != 1 {
		t.Errorf("review_loop_cycle_complete emitted %d times; want exactly 1; events: %v", count, types)
	}
}

// rlcAssertCycleCompleteIsLastReviewEvent asserts that after the last
// review_loop_cycle_complete, no other review-loop event types are emitted.
// This validates the "precedes terminal run event" ordering rule: since
// runReviewLoop emits cycle_complete immediately before returning, any
// run_completed / run_failed that callers emit must come after.
//
// The review-loop event types checked are the six §8.1a events.
func rlcAssertCycleCompleteIsLastReviewEvent(t *testing.T, types []string) {
	t.Helper()
	reviewLoopEvents := map[string]bool{
		string(core.EventTypeImplementerResumed):      true,
		string(core.EventTypeReviewerLaunched):        true,
		string(core.EventTypeReviewerVerdict):         true,
		string(core.EventTypeIterationCapHit):         true,
		string(core.EventTypeNoProgressDetected):      true,
		string(core.EventTypeReviewFixupStalled):      true, // hk-m1wqp
		string(core.EventTypeReviewLoopCycleComplete): true,
	}

	// Find the index of the last review_loop_cycle_complete.
	lastCycleCompleteIdx := -1
	for i, et := range types {
		if et == string(core.EventTypeReviewLoopCycleComplete) {
			lastCycleCompleteIdx = i
		}
	}
	if lastCycleCompleteIdx == -1 {
		t.Error("review_loop_cycle_complete not found in emitted events")
		return
	}

	// No review-loop event should follow the last cycle_complete.
	for _, et := range types[lastCycleCompleteIdx+1:] {
		if reviewLoopEvents[et] {
			t.Errorf("review-loop event %q emitted after review_loop_cycle_complete; ordering violated; events: %v", et, types)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests — five termination paths
// ─────────────────────────────────────────────────────────────────────────────

// TestReviewLoopCycleComplete_Approved verifies that the approved termination
// path emits review_loop_cycle_complete exactly once and as the last §8.1a event.
//
// Spec: event-model.md §8.1a.6; execution-model.md §4.3 EM-015e.
// Bead: hk-7om2q.24.
func TestReviewLoopCycleComplete_Approved(t *testing.T) {
	t.Parallel()

	projectDir, wtPath, parentSHA := rlcFixtureSetup(t)
	scriptPath := rlFixtureHandlerScript(t, wtPath, []string{"APPROVE"})

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
		core.BeadID("rlc-approved-024"),
		wtPath, parentSHA,
	)

	if result.CompletionReason != string(core.ReviewLoopCompletionReasonApproved) {
		t.Errorf("completion_reason = %q; want %q", result.CompletionReason, core.ReviewLoopCompletionReasonApproved)
	}

	eventTypes := collector.eventTypes()
	rlcAssertCycleCompleteExactlyOnce(t, eventTypes)
	rlcAssertCycleCompleteIsLastReviewEvent(t, eventTypes)
}

// TestReviewLoopCycleComplete_CapHit verifies that the cap_hit termination
// path emits review_loop_cycle_complete exactly once and as the last §8.1a event.
//
// Spec: event-model.md §8.1a.6; execution-model.md §4.3 EM-015e.
// Bead: hk-7om2q.24.
func TestReviewLoopCycleComplete_CapHit(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	t.Parallel()

	projectDir, wtPath, parentSHA := rlcFixtureSetup(t)
	scriptPath := rlFixtureHandlerScript(t, wtPath, []string{
		"REQUEST_CHANGES", "REQUEST_CHANGES", "REQUEST_CHANGES",
	})

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

	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()

	result := daemon.ExportedRunReviewLoop(
		ctx, deps,
		rlFixtureRunID(t),
		core.BeadID("rlc-cap-hit-024"),
		wtPath, parentSHA,
	)

	if result.CompletionReason != string(core.ReviewLoopCompletionReasonCapHit) {
		t.Errorf("completion_reason = %q; want %q", result.CompletionReason, core.ReviewLoopCompletionReasonCapHit)
	}

	eventTypes := collector.eventTypes()
	rlcAssertCycleCompleteExactlyOnce(t, eventTypes)
	rlcAssertCycleCompleteIsLastReviewEvent(t, eventTypes)
}

// TestReviewLoopCycleComplete_Blocked verifies that the blocked termination
// path emits review_loop_cycle_complete exactly once with completion_reason=blocked,
// and that it is the last §8.1a event.
//
// Spec: event-model.md §8.1a.6 completion_reason ∈ {blocked};
//
//	execution-model.md §4.3 EM-015e BLOCK → needs-attention.
//
// Bead: hk-7om2q.24.
func TestReviewLoopCycleComplete_Blocked(t *testing.T) {
	t.Parallel()

	projectDir, wtPath, parentSHA := rlcFixtureSetup(t)
	scriptPath := rlFixtureHandlerScript(t, wtPath, []string{"BLOCK"})

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
		core.BeadID("rlc-blocked-024"),
		wtPath, parentSHA,
	)

	if result.Success {
		t.Error("expected success=false on BLOCK path")
	}
	if result.CompletionReason != string(core.ReviewLoopCompletionReasonBlocked) {
		t.Errorf("completion_reason = %q; want %q", result.CompletionReason, core.ReviewLoopCompletionReasonBlocked)
	}
	if !result.NeedsAttention {
		t.Error("expected needs_attention=true on BLOCK path")
	}

	eventTypes := collector.eventTypes()
	rlAssertEventPresent(t, eventTypes, string(core.EventTypeReviewerVerdict))
	rlcAssertCycleCompleteExactlyOnce(t, eventTypes)
	rlAssertEventSubsequence(t, eventTypes, []string{
		string(core.EventTypeReviewerVerdict),
		string(core.EventTypeReviewLoopCycleComplete),
	})
	rlcAssertCycleCompleteIsLastReviewEvent(t, eventTypes)
}

// TestReviewLoopCycleComplete_NoProgress verifies that the no_progress
// termination path emits review_loop_cycle_complete exactly once with
// completion_reason=no_progress, preceded by no_progress_detected, and that
// it is the last §8.1a event.
//
// Spec: event-model.md §8.1a.5–6 ordering (no_progress_detected → cycle_complete);
//
//	execution-model.md §4.3 EM-015e.
//
// Bead: hk-7om2q.24.
func TestReviewLoopCycleComplete_NoProgress(t *testing.T) {
	t.Parallel()

	projectDir, wtPath, parentSHA := rlcFixtureSetup(t)
	scriptPath := rlcFixtureNoProgressScript(t, wtPath)

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
		core.BeadID("rlc-noprogress-024"),
		wtPath, parentSHA,
	)

	if result.Success {
		t.Error("expected success=false on no-progress path")
	}
	// hk-m1wqp: review-loop no-progress after REQUEST_CHANGES now uses fixup_stalled.
	if result.CompletionReason != string(core.ReviewLoopCompletionReasonFixupStalled) {
		t.Errorf("completion_reason = %q; want %q", result.CompletionReason, core.ReviewLoopCompletionReasonFixupStalled)
	}
	if !result.NeedsAttention {
		t.Error("expected needs_attention=true on no-progress path")
	}

	eventTypes := collector.eventTypes()
	// hk-m1wqp: review_fixup_stalled replaces no_progress_detected when the
	// prior verdict was REQUEST_CHANGES (structural guarantee in review-loop mode).
	rlAssertEventPresent(t, eventTypes, string(core.EventTypeReviewFixupStalled))
	rlcAssertCycleCompleteExactlyOnce(t, eventTypes)
	rlAssertEventSubsequence(t, eventTypes, []string{
		string(core.EventTypeReviewFixupStalled),
		string(core.EventTypeReviewLoopCycleComplete),
	})
	rlcAssertCycleCompleteIsLastReviewEvent(t, eventTypes)
}

// TestReviewLoopCycleComplete_Error verifies that the error termination path
// (malformed reviewer verdict file) emits review_loop_cycle_complete exactly
// once with completion_reason=error, and that it is the last §8.1a event.
//
// Spec: event-model.md §8.1a.6 completion_reason ∈ {error};
//
//	execution-model.md §4.3 EM-015e — malformed verdict → error path.
//
// Bead: hk-7om2q.24.
func TestReviewLoopCycleComplete_Error(t *testing.T) {
	t.Parallel()

	projectDir, wtPath, parentSHA := rlcFixtureSetup(t)
	scriptPath := rlcFixtureMalformedVerdictScript(t, wtPath)

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
		core.BeadID("rlc-error-024"),
		wtPath, parentSHA,
	)

	if result.Success {
		t.Error("expected success=false on error path")
	}
	if result.CompletionReason != string(core.ReviewLoopCompletionReasonError) {
		t.Errorf("completion_reason = %q; want %q", result.CompletionReason, core.ReviewLoopCompletionReasonError)
	}
	if !result.NeedsAttention {
		t.Error("expected needs_attention=true on error path")
	}

	eventTypes := collector.eventTypes()
	rlcAssertCycleCompleteExactlyOnce(t, eventTypes)
	rlcAssertCycleCompleteIsLastReviewEvent(t, eventTypes)
}

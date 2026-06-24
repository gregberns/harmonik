package daemon_test

// reviewloop_settings_only_hk30jd_test.go — scenario test for hk-30jd.
//
// # Bug context
//
// On bead hk-rai2 (M0b) the implementer's iter-1 committed ONLY a harness-
// injected .claude/settings.json edit and no real implementation code.  The
// reviewer correctly flagged it a no-op (REQUEST_CHANGES, flags: no-op,
// missing-implementation, missing-tests), burning a full review cycle before
// iter-2 delivered the actual work.
//
// Root cause: MaterializeClaudeSettings writes .claude/settings.json into
// the worktree before the implementer runs.  Because the file is tracked in
// the main repo the write leaves it dirty.  An implementer that stages+commits
// before writing real code produces a commit that only touches that
// harness-injected file.  HEAD advances past the no-commit baseline, so the
// existing no-commit guard (hk-9c1v4) does not fire, and the reviewer is
// launched with a churn-only diff.
//
// # Expected behaviour after fix (hk-30jd)
//
// When the iter-1 implementer commits ONLY isHarmonikChurn paths (e.g.
// .claude/settings.json), runReviewLoop MUST:
//
//   - return a non-success reviewLoopResult with completionReason=error
//     and needsAttention=true.
//   - emit review_loop_cycle_complete (terminal) before returning.
//   - NEVER emit reviewer_launched.
//   - NEVER launch the reviewer subprocess.
//
// Helper prefix: `h30jd` (per implementer-protocol §Helper-prefix discipline).
//
// Bead: hk-30jd.

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

// h30jdHandlerScriptSettingsOnlyCommit writes a handler script that:
//   - On its first invocation (implementer, CNT=1): stages and commits ONLY
//     .claude/settings.json (simulating the harness-injection pathology).
//   - On any subsequent invocation (reviewer): writes an APPROVE verdict so
//     the test can detect the regression (reviewer MUST NOT run).
func h30jdHandlerScriptSettingsOnlyCommit(t *testing.T, wtPath string) string {
	t.Helper()
	wtpEsc := strings.ReplaceAll(wtPath, "'", "'\\''")
	script := fmt.Sprintf(`#!/bin/sh
set -e
WTP='%s'
CNT_FILE="$WTP/.harmonik/h30jd_count"
if [ ! -f "$CNT_FILE" ]; then printf '0' > "$CNT_FILE"; fi
CNT=$(cat "$CNT_FILE")
CNT=$((CNT + 1))
printf '%%d' "$CNT" > "$CNT_FILE"

if [ "$CNT" -eq 1 ]; then
  # Implementer: create and commit ONLY .claude/settings.json.
  # This simulates MaterializeClaudeSettings writing the hook-bridge settings
  # into a tracked file, followed by the agent committing it before any real work.
  mkdir -p "$WTP/.claude"
  printf '{"hooks":{}}' > "$WTP/.claude/settings.json"
  git -C "$WTP" add .claude/settings.json
  git -C "$WTP" commit -m "harness: materialize .claude/settings.json"
  exit 0
fi

# Any invocation after CNT=1 is the reviewer; emit APPROVE so we can detect
# the regression (test will fail because reviewer_launched must never fire).
printf '{"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"h30jd reviewer must NOT have run"}' > "$WTP/.harmonik/review.json"
exit 0
`, wtpEsc)
	scriptPath := filepath.Join(t.TempDir(), "h30jd_handler.sh")
	//nolint:gosec // G306: test-only fixture script
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("h30jdHandlerScriptSettingsOnlyCommit: WriteFile: %v", err)
	}
	return scriptPath
}

// TestReviewLoop_SettingsOnlyCommit_FailsRun_DoesNotLaunchReviewer_Hk30jd is
// the load-bearing scenario test for hk-30jd.  It MUST fail before the fix
// (reviewer_launched fires on a churn-only iter-1 diff) and pass after (run
// terminates with error completionReason; reviewer never launches).
//
// Spec ref: specs/execution-model.md §4.3 EM-015d (implementer MUST advance
// HEAD with real work before the daemon launches the reviewer).
// Bead: hk-30jd.
func TestReviewLoop_SettingsOnlyCommit_FailsRun_DoesNotLaunchReviewer_Hk30jd(t *testing.T) {
	t.Parallel()

	projectDir := rlFixtureProjectDir(t)
	rlFixtureGitRepo(t, projectDir)
	wtPath, parentSHA := rlFixtureWorktree(t, projectDir)

	scriptPath := h30jdHandlerScriptSettingsOnlyCommit(t, wtPath)

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
		core.BeadID("h30jd-settings-only-001"),
		wtPath, parentSHA,
	)

	eventTypes := collector.eventTypes()

	// Assertion 1: result MUST be a failure.
	if result.Success {
		t.Errorf("hk-30jd FAIL: result.Success = true; want false (settings-only commit is a no-op). summary=%q events=%v",
			result.Summary, eventTypes)
	}

	// Assertion 2: completion_reason must be "error".
	if result.CompletionReason != string(core.ReviewLoopCompletionReasonError) {
		t.Errorf("hk-30jd FAIL: completion_reason = %q; want %q. events=%v",
			result.CompletionReason, core.ReviewLoopCompletionReasonError, eventTypes)
	}

	// Assertion 3: needs-attention must be set.
	if !result.NeedsAttention {
		t.Errorf("hk-30jd FAIL: NeedsAttention = false; want true (churn-only commit failure is operator-visible)")
	}

	// Assertion 4 (load-bearing): reviewer_launched MUST NOT fire.
	// Pre-fix: the churn-only iter-1 commit passes the headSHA==baseline guard,
	// falls through to diff-hash computation, and launches the reviewer.
	for _, et := range eventTypes {
		if et == string(core.EventTypeReviewerLaunched) {
			t.Errorf("hk-30jd FAIL: reviewer_launched emitted despite implementer committing only harness churn files; "+
				"all events: %v", eventTypes)
			break
		}
	}

	// Assertion 5: review_loop_cycle_complete must be emitted.
	foundCycleComplete := false
	for _, et := range eventTypes {
		if et == string(core.EventTypeReviewLoopCycleComplete) {
			foundCycleComplete = true
			break
		}
	}
	if !foundCycleComplete {
		t.Errorf("hk-30jd FAIL: review_loop_cycle_complete not emitted; events=%v", eventTypes)
	}

	// Assertion 6: summary must mention churn (diagnostic check).
	if !strings.Contains(result.Summary, "churn") {
		t.Errorf("hk-30jd FAIL: summary does not mention 'churn': %q", result.Summary)
	}

	// Assertion 7: review.json must NOT exist — the reviewer must never have run.
	reviewPath := filepath.Join(wtPath, ".harmonik", "review.json")
	if _, err := os.Stat(reviewPath); err == nil {
		t.Errorf("hk-30jd FAIL: review.json exists at %q — reviewer ran despite settings-only implementer commit", reviewPath)
	} else if !os.IsNotExist(err) {
		t.Fatalf("h30jd: stat review.json: %v", err)
	}
}

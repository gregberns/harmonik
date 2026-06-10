package daemon_test

// dot_no_progress_hk5e9yj_test.go — T-WM-022 acceptance test for DOT mode.
//
// Verifies that the DOT cascade driver detects no-progress (EM-015e) when an
// implementer node exits without advancing HEAD on iteration ≥ 2, and emits
// no_progress_detected{workflow_mode="dot"} without a subsequent
// review_loop_cycle_complete (which DOT mode does not emit).
//
// # What this test proves
//
// driveDotWorkflow now tracks the diff-hash (git diff parentSHA..HEAD) across
// iterations and checks it before each reviewer dispatch. When iterationCount ≥ 2
// and the current hash equals the prior, it emits no_progress_detected with
// workflow_mode="dot" and terminates (needsAttention=true). The cascade terminates
// directly — review_loop_cycle_complete is NOT emitted (that event is review-loop
// mode only per §8.1a ordering-rule DOT exemption).
//
// # Scenario
//
// Invocation sequence for the canonical review-loop.dot graph:
//   1. implementer (iter 1): commits a file, advancing HEAD.
//   2. reviewer (iter 1):    writes REQUEST_CHANGES verdict → back-edge fires.
//   3. implementer (iter 2): exits 0 without committing → HEAD stays at iter1HEAD.
//   (no invocation 4: cascade detects same diff hash before reviewer dispatch)
//
// # Spec refs
//
// - specs/event-model.md §8.1a.5 (no_progress_detected; workflow_mode="dot" permitted)
// - specs/event-model.md §8.1a ordering-rule DOT exemption (no review_loop_cycle_complete)
// - specs/execution-model.md §4.3 EM-015e (no-progress early-exit)
//
// Bead: hk-5e9yj (DOT cascade missing no_progress detector).

import (
	"context"
	"encoding/json"
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

// dotNPFixtureScript writes a handler script that:
//   - Invocation 1 (implementer iter 1): commits a file to advance HEAD.
//   - Invocation 2 (reviewer iter 1): writes REQUEST_CHANGES to .harmonik/review.json.
//   - Invocation 3 (implementer iter 2): exits 0 without committing → HEAD unchanged.
//   - Invocation 4+: should never be reached (test fails if script exits nonzero).
//
// In DOT mode the reviewer writes review.json to $HARMONIK_WORKSPACE_PATH/.harmonik/
// (same worktree as the implementer — no reviewer worktree isolation in DOT mode).
func dotNPFixtureScript(t *testing.T, wtPath string) string {
	t.Helper()
	wtpEsc := strings.ReplaceAll(wtPath, "'", "'\\''")
	rcVerdict := strings.ReplaceAll(rlFixtureVerdictJSON("REQUEST_CHANGES"), "'", "'\\''")
	script := fmt.Sprintf(`#!/bin/sh
set -e
WTP='%s'
WS="${HARMONIK_WORKSPACE_PATH:-$WTP}"
CNT_FILE="$WTP/.harmonik/dot_np_count"
if [ ! -f "$CNT_FILE" ]; then
  printf '0' > "$CNT_FILE"
fi
CNT=$(cat "$CNT_FILE")
CNT=$((CNT + 1))
printf '%%d' "$CNT" > "$CNT_FILE"
case "$CNT" in
  1)
    # Implementer iter 1: commit a file so HEAD advances (non-empty diff).
    printf '1' > "$WS/dot_np_impl1.txt"
    git -C "$WS" add "dot_np_impl1.txt" >/dev/null 2>&1
    git -C "$WS" -c user.email=test@harmonik.local -c user.name="Test" commit -m "impl dot np iter 1" --no-gpg-sign >/dev/null 2>&1
    ;;
  2)
    # Reviewer iter 1: REQUEST_CHANGES so the back-edge fires and we loop.
    mkdir -p "$WS/.harmonik"
    printf '%%s' '%s' > "$WS/.harmonik/review.json"
    ;;
  3)
    # Implementer iter 2: exit without committing — HEAD stays at iter1HEAD.
    # driveDotWorkflow detects same diff hash before dispatching reviewer and
    # emits no_progress_detected.
    ;;
  *)
    # Should not be reached: no-progress check fires before reviewer dispatch.
    exit 1
    ;;
esac
exit 0
`, wtpEsc, rcVerdict)

	scriptPath := filepath.Join(t.TempDir(), "dot_np_handler.sh")
	//nolint:gosec // G306: test-only fixture script; not production
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("dotNPFixtureScript: WriteFile: %v", err)
	}
	return scriptPath
}

// TestDotCascade_NoProgressDetected verifies that driveDotWorkflow emits
// no_progress_detected{workflow_mode="dot"} when the implementer makes zero new
// changes on iteration 2, and does NOT emit review_loop_cycle_complete (DOT
// exemption per §8.1a ordering rule).
func TestDotCascade_NoProgressDetected(t *testing.T) {
	t.Parallel()

	// Reuse the review-loop fixture setup (same git-repo + worktree structure).
	projectDir, wtPath, parentSHA := rlcFixtureSetup(t)
	scriptPath := dotNPFixtureScript(t, wtPath)

	// Load the canonical review-loop DOT graph (start→implementer→reviewer→close).
	dotPath := filepath.Join(dotE2EModuleRoot(), "specs", "examples", "review-loop.dot")
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
		core.BeadID("dot-np-hk5e9yj"),
		wtPath, parentSHA,
		graph,
	)

	// ── Result assertions ────────────────────────────────────────────────────
	if result.Success {
		t.Error("expected success=false on DOT no-progress path")
	}
	if !result.NeedsAttention {
		t.Error("expected needs_attention=true on DOT no-progress path")
	}
	// hk-m1wqp: summary now says "fix-up stalled" when prior verdict was REQUEST_CHANGES.
	if !strings.Contains(result.Summary, "no-progress") && !strings.Contains(result.Summary, "fix-up stalled") {
		t.Errorf("expected summary to mention no-progress or fix-up stalled; got %q", result.Summary)
	}

	// ── Event assertions ─────────────────────────────────────────────────────
	eventTypes := collector.eventTypes()

	// hk-m1wqp: review_fixup_stalled replaces no_progress_detected when the
	// prior verdict was REQUEST_CHANGES (the DOT fixture writes REQUEST_CHANGES
	// on invocation 2 before the no-progress fires on invocation 3).
	rlAssertEventPresent(t, eventTypes, string(core.EventTypeReviewFixupStalled))

	// review_loop_cycle_complete MUST NOT be emitted in DOT mode (§8.1a ordering
	// rule DOT exemption).
	for _, et := range eventTypes {
		if et == string(core.EventTypeReviewLoopCycleComplete) {
			t.Errorf("review_loop_cycle_complete must NOT be emitted in DOT mode; events: %v", eventTypes)
			break
		}
	}

	// ── Payload assertion: workflow_mode MUST be "dot" ───────────────────────
	var foundEvent bool
	for _, ev := range collector.allEvents() {
		if ev.EventType != string(core.EventTypeReviewFixupStalled) {
			continue
		}
		foundEvent = true
		var pl core.ReviewFixupStalledPayload
		if err := json.Unmarshal(ev.Payload, &pl); err != nil {
			t.Fatalf("unmarshal review_fixup_stalled payload: %v", err)
		}
		if pl.WorkflowMode != core.WorkflowModeDot {
			t.Errorf("review_fixup_stalled.workflow_mode = %q; want %q", pl.WorkflowMode, core.WorkflowModeDot)
		}
		if !pl.Valid() {
			t.Errorf("review_fixup_stalled payload is not Valid(): %+v", pl)
		}
		if pl.IterationCount < 2 {
			t.Errorf("review_fixup_stalled.iteration_count = %d; want ≥ 2", pl.IterationCount)
		}
		if pl.ReviewerFlags == nil {
			t.Errorf("review_fixup_stalled.reviewer_flags is nil; want non-nil (MAY be empty)")
		}
		break
	}
	if !foundEvent {
		t.Error("review_fixup_stalled event not found in emitted events")
	}
}

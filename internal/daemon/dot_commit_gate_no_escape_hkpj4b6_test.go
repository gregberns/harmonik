package daemon_test

// dot_commit_gate_no_escape_hkpj4b6_test.go — regression test for hk-pj4b6.
//
// # The bug
//
// The DOT cascade's no-progress diff-hash check (EM-015e) USED to fire only
// inside the `if isReviewer` branch of driveDotWorkflow. So an implementer that
// RE-ENTERED from a deterministic commit_gate FAIL back-edge and made NO new diff
// (iteration ≥ 2) was never detected as no-progress. Instead the
// implement→commit_gate→implement back-edge looped until the traversal cap fired,
// surfacing as "dot: traversal cap hit at node …" ~30min later — a noChange bead
// burning a full slot to the cap instead of failing cleanly.
//
// # The fix (hk-pj4b6)
//
// The diff-hash no-progress check is hoisted OUT of the reviewer-only branch so
// it runs for ANY agentic node dispatch at iteration ≥ 2 — both reviewer
// re-entries (preserved) AND implementer re-entries from a gate FAIL. A no-diff
// implementer re-entry is now caught as no-progress (single clean failure) BEFORE
// the traversal cap is reached.
//
// # Scenario (this test)
//
// Graph: start → implement(agentic) → gate(shell, always exit 3) with the
// standard-bead topology back-edge:
//
//	gate -> implement [deterministic FAIL, traversal_cap=3]
//	gate -> close             [SUCCESS — never taken; gate always FAILs]
//	gate -> close-needs-attention  [unconditional fallback]
//
// Handler script (the agentic implementer):
//   - entry 1: commits a file → HEAD advances (non-empty diff H(A)).
//   - entry ≥ 2: exits 0 WITHOUT committing → HEAD stays at H(A).
//
// Walk with the fix:
//   - implement(1): commits A.  gate exit3 → back-edge (cap 1/3).
//   - implement(2): no commit (iter≥2 SUCCESS).  gate exit3 → back-edge (cap 2/3).
//   - implement(3): no-progress check fires (H(A) == lastDiffHash, iter≥2) →
//     no_progress_detected, terminate. Cap is at 2/3 — NOT hit.
//
// Without the fix the same walk would loop to cap 3/3 and surface
// "traversal cap hit"; no no_progress_detected would be emitted.
//
// # Spec refs
//   - specs/execution-model.md §4.3 EM-015e (no-progress early-exit)
//   - specs/event-model.md §8.1a.5 (no_progress_detected; workflow_mode="dot")
//
// Bead: hk-pj4b6.

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

// gateNoEscapeDOT returns the DOT source for the commit_gate no-escape topology:
// start → implement(agentic) → gate(shell, exit 3) with the standard-bead
// deterministic-FAIL back-edge to implement (capped at 3).
func gateNoEscapeDOT() string {
	return `digraph "hk-pj4b6-gate-no-escape" {
    schema_version="1"; version="1.0"; workflow_id="hk-pj4b6-gate-no-escape";
    start_node="start"; terminal_node_ids="close,close-needs-attention";

    start [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];
    implement [type="agentic", agent_type="implementer", handler_ref="claude-implementer", idempotency_class="non-idempotent"];
    gate [type="non-agentic", handler_ref="shell", idempotency_class="idempotent", tool_command="exit 3", timeout="30"];
    close [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];
    "close-needs-attention" [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];

    start -> implement;
    implement -> gate;
    gate -> close [condition="outcome.status == 'SUCCESS'"];
    gate -> implement [condition="outcome.status == 'FAIL' && outcome.failure_class == 'deterministic'", traversal_cap="3"];
    gate -> "close-needs-attention";
}
`
}

// gateNoEscapeScript writes the agentic-implementer handler:
//   - entry 1: commits a file (HEAD advances).
//   - entry ≥ 2: exits 0 WITHOUT committing (HEAD unchanged → same diff hash).
//
// The counter lives in the shared implementer worktree so it persists across the
// agentic re-entries. The gate node is a non-agentic in-process shell node and
// does NOT invoke this handler, so the counter strictly tracks implement entries.
func gateNoEscapeScript(t *testing.T, wtPath string) string {
	t.Helper()
	wtpEsc := strings.ReplaceAll(wtPath, "'", "'\\''")
	script := fmt.Sprintf(`#!/bin/sh
set -e
WTP='%s'
WS="${HARMONIK_WORKSPACE_PATH:-$WTP}"
CNT_FILE="$WTP/.harmonik/gate_no_escape_count"
if [ ! -f "$CNT_FILE" ]; then
  printf '0' > "$CNT_FILE"
fi
CNT=$(cat "$CNT_FILE")
CNT=$((CNT + 1))
printf '%%d' "$CNT" > "$CNT_FILE"
case "$CNT" in
  1)
    # Implementer entry 1: commit a file so HEAD advances (non-empty diff).
    printf '1' > "$WS/gate_no_escape_impl1.txt"
    git -C "$WS" add "gate_no_escape_impl1.txt" >/dev/null 2>&1
    git -C "$WS" -c user.email=test@harmonik.local -c user.name="Test" commit -m "impl gate-no-escape entry 1" --no-gpg-sign >/dev/null 2>&1
    ;;
  *)
    # Implementer re-entry (≥2): exit without committing. HEAD stays at entry-1
    # HEAD, so the diff hash is unchanged — the hoisted no-progress check must
    # catch this on the next agentic dispatch BEFORE the traversal cap is hit.
    ;;
esac
exit 0
`, wtpEsc)

	scriptPath := filepath.Join(t.TempDir(), "gate_no_escape_handler.sh")
	//nolint:gosec // G306: test-only fixture script; not production
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("gateNoEscapeScript: WriteFile: %v", err)
	}
	return scriptPath
}

// TestCommitGateNoEscapeLoop_hkpj4b6 verifies that an implementer re-entering
// from a deterministic commit_gate FAIL with NO new diff is caught as
// no_progress_detected (single clean failure) rather than looping the
// implement↔commit_gate back-edge until the traversal cap fires.
func TestCommitGateNoEscapeLoop_hkpj4b6(t *testing.T) {
	t.Parallel()

	projectDir, wtPath, parentSHA := rlcFixtureSetup(t)
	scriptPath := gateNoEscapeScript(t, wtPath)

	graphDir := t.TempDir()
	dotPath := filepath.Join(graphDir, "gate-no-escape.dot")
	//nolint:gosec // G306: test-only fixture
	if err := os.WriteFile(dotPath, []byte(gateNoEscapeDOT()), 0o644); err != nil {
		t.Fatalf("write DOT: %v", err)
	}
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

	// Generous-but-bounded budget. With the fix the walk terminates after a
	// handful of fast in-process gate runs; if the bug regressed (loop to cap)
	// it would still terminate at cap=3, but the SUMMARY assertion below pins
	// the no-progress outcome rather than cap-hit.
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	result := daemon.ExportedDriveDotWorkflow(
		ctx, deps,
		rlFixtureRunID(t),
		core.BeadID("dot-gate-no-escape-hkpj4b6"),
		wtPath, parentSHA,
		graph,
	)

	events := collector.eventTypes()
	t.Logf("hk-pj4b6: result=%+v events=%v", result, events)

	// ── Result assertions ────────────────────────────────────────────────────
	if result.Success {
		t.Errorf("expected success=false on no-progress path; summary=%q", result.Summary)
	}
	if !result.NeedsAttention {
		t.Errorf("expected needs_attention=true on no-progress path; summary=%q", result.Summary)
	}
	if !strings.Contains(result.Summary, "no-progress") {
		t.Errorf("expected summary to report no-progress; got %q", result.Summary)
	}
	// The bug surfaced as a traversal-cap-hit. If the fix regressed, the loop
	// would run to the cap and the summary would mention the cap instead.
	if strings.Contains(result.Summary, "traversal cap") {
		t.Errorf("regression (hk-pj4b6): commit_gate loop ran to the traversal cap "+
			"instead of detecting no-progress; summary=%q", result.Summary)
	}

	// ── Event assertions ─────────────────────────────────────────────────────
	// no_progress_detected MUST be emitted; that is the clean-failure signal the
	// hoisted check produces.
	rlAssertEventPresent(t, events, string(core.EventTypeNoProgressDetected))

	// review_loop_cycle_complete MUST NOT be emitted in DOT mode (§8.1a DOT
	// exemption) — guards against accidentally re-using the review-loop teardown.
	for _, et := range events {
		if et == string(core.EventTypeReviewLoopCycleComplete) {
			t.Errorf("review_loop_cycle_complete must NOT be emitted in DOT mode; events: %v", events)
			break
		}
	}

	// ── Payload assertions ───────────────────────────────────────────────────
	var foundNoProgress bool
	for _, ev := range collector.allEvents() {
		if ev.EventType != string(core.EventTypeNoProgressDetected) {
			continue
		}
		foundNoProgress = true
		var pl core.NoProgressDetectedPayload
		if err := json.Unmarshal(ev.Payload, &pl); err != nil {
			t.Fatalf("unmarshal no_progress_detected payload: %v", err)
		}
		if pl.WorkflowMode != core.WorkflowModeDot {
			t.Errorf("no_progress_detected.workflow_mode = %q; want %q", pl.WorkflowMode, core.WorkflowModeDot)
		}
		if !pl.Valid() {
			t.Errorf("no_progress_detected payload is not Valid(): %+v", pl)
		}
		if pl.IterationCount < 2 {
			t.Errorf("no_progress_detected.iteration_count = %d; want ≥ 2", pl.IterationCount)
		}
		// The current and prior diff hashes must match (that IS no-progress).
		if pl.DiffHashCurrent != pl.DiffHashPrior {
			t.Errorf("no_progress_detected: diff_hash_current %q != diff_hash_prior %q; expected equal",
				pl.DiffHashCurrent, pl.DiffHashPrior)
		}
		break
	}
	if !foundNoProgress {
		t.Error("no_progress_detected event not found in emitted events")
	}
}

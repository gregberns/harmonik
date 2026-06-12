//go:build scenario

package daemon_test

// scenario_commit_gate_cap_hki8g59_test.go — scenario regression guard for
// hk-i8g59 (pairs with the hk-pj4b6 no-escape fix).
//
// # What this guards
//
// hk-pj4b6 hoisted the no-progress diff-hash check so an implementer that
// RE-ENTERS from a deterministic commit_gate FAIL and makes NO new diff is
// caught as no_progress_detected BEFORE the traversal cap fires. The unit test
// dot_commit_gate_no_escape_hkpj4b6_test.go pins THAT mechanism (no-diff
// re-entry → no-progress, cap NOT reached).
//
// This scenario pins the COMPLEMENTARY guarantee: when the no-progress
// short-circuit canNOT fire — the implementer makes a NEW diff on EVERY entry —
// the implement↔commit_gate back-edge is STILL bounded, by the per-edge
// traversal_cap (dotEdgeTraversalCap, dot_cascade.go; core.SelectNextEdge /
// CycleCounter, execution-model.md EM-043). The cascade terminates as a
// run-failure (success=false, needs_attention=true, summary "traversal cap
// hit") within the cap, NOT an infinite implement↔gate loop that would surface
// ~30 min later as run_stale.
//
// Together the two tests guard BOTH termination mechanisms of the no-escape
// loop: hk-pj4b6 (no-progress, fires first when the diff is unchanged) and
// hk-i8g59 (traversal cap, fires when every re-entry produces a fresh diff).
//
// # Topology (commit_gate no-escape, cap=3)
//
//	start → implement(agentic) → gate(shell, always exit 3)
//	gate -> implement  [outcome.status=='FAIL' && failure_class=='deterministic', traversal_cap="3"]
//	gate -> close              [outcome.status=='SUCCESS' — never taken; gate always FAILs]
//	gate -> "close-needs-attention"  [unconditional fallback]
//
// # Handler (the agentic implementer)
//
// EVERY entry commits a UNIQUE file (impl_cap_<N>.txt) → HEAD advances and the
// diff hash CHANGES on every iteration. The hk-pj4b6 no-progress check compares
// the current diff hash against the prior; because the hash differs each time it
// NEVER fires here. So the ONLY thing that can terminate the loop is the cap.
//
// # Cap arithmetic (cap=3, gate→implement edge)
//
// SelectNextEdge rejects a traversal when Get() >= cap (pre-check); the driver
// Increments AFTER each Advance (incrementCapIfBounded):
//
//	implement(1) → gate FAIL → select: count 0<3 Advance → increment → 1
//	implement(2) → gate FAIL → select: count 1<3 Advance → increment → 2
//	implement(3) → gate FAIL → select: count 2<3 Advance → increment → 3
//	implement(4) → gate FAIL → select: count 3>=3 → Failed (cap_hit). STOP.
//
// So the loop is bounded at exactly 3 back-edge traversals (gate→implement)
// before the cap fires — provably finite, no infinite loop.
//
// # Spec refs
//   - specs/execution-model.md §4.10 EM-043 / EM-043a (per-edge traversal cap)
//   - specs/execution-model.md §4.3 EM-015e (cap_hit vocabulary)
//   - specs/event-model.md §8.x node_dispatch_decided (cap-hit signal)
//   - dot_cascade.go: dotEdgeTraversalCap, incrementCapIfBounded, cap-hit branch
//
// Bead: hk-i8g59 (Refs hk-pj4b6).
//
// Run: go test -tags=scenario -run TestScenario_CommitGateCapTerminates_hki8g59 ./internal/daemon/...

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

// capTraversalCap is the traversal_cap on the gate→implement back-edge. It is
// referenced both by the DOT source and the bounded-advance assertion so the
// two cannot drift apart.
const capTraversalCap = 3

// gateCapDOT returns the DOT source for the commit_gate no-escape topology with
// a gate→implement back-edge bounded by traversal_cap=capTraversalCap. Identical
// in shape to the hk-pj4b6 topology; the cap value is interpolated so the
// assertion below stays in lockstep with the graph.
func gateCapDOT() string {
	return fmt.Sprintf(`digraph "hk-i8g59-gate-cap" {
    schema_version="1"; version="1.0"; workflow_id="hk-i8g59-gate-cap";
    start_node="start"; terminal_node_ids="close,close-needs-attention";

    start [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];
    implement [type="agentic", agent_type="implementer", handler_ref="claude-implementer", idempotency_class="non-idempotent"];
    gate [type="non-agentic", handler_ref="shell", idempotency_class="idempotent", tool_command="exit 3", timeout="30"];
    close [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];
    "close-needs-attention" [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];

    start -> implement;
    implement -> gate;
    gate -> close [condition="outcome.status == 'SUCCESS'"];
    gate -> implement [condition="outcome.status == 'FAIL' && outcome.failure_class == 'deterministic'", traversal_cap="%d"];
    gate -> "close-needs-attention";
}
`, capTraversalCap)
}

// gateCapScript writes the agentic-implementer handler: EVERY entry commits a
// UNIQUE file so HEAD advances and the diff hash differs on every iteration —
// the hk-pj4b6 no-progress check never fires, so termination MUST come from the
// traversal cap. The counter lives in the shared implementer worktree so it
// persists across re-entries (the gate node is a non-agentic in-process shell
// node and does NOT invoke this handler, so the counter strictly tracks
// implement entries).
func gateCapScript(t *testing.T, wtPath string) string {
	t.Helper()
	wtpEsc := strings.ReplaceAll(wtPath, "'", "'\\''")
	script := fmt.Sprintf(`#!/bin/sh
set -e
WTP='%s'
WS="${HARMONIK_WORKSPACE_PATH:-$WTP}"
CNT_FILE="$WTP/.harmonik/gate_cap_count"
if [ ! -f "$CNT_FILE" ]; then
  printf '0' > "$CNT_FILE"
fi
CNT=$(cat "$CNT_FILE")
CNT=$((CNT + 1))
printf '%%d' "$CNT" > "$CNT_FILE"
# Every entry commits a UNIQUE file → HEAD advances, diff hash changes each time.
# This is the OPPOSITE of the hk-pj4b6 no-diff re-entry: no-progress can never
# fire, so only the traversal cap can terminate the loop.
printf '%%d' "$CNT" > "$WS/impl_cap_$CNT.txt"
git -C "$WS" add "impl_cap_$CNT.txt" >/dev/null 2>&1
git -C "$WS" -c user.email=test@harmonik.local -c user.name="Test" commit -m "impl gate-cap entry $CNT" --no-gpg-sign >/dev/null 2>&1
exit 0
`, wtpEsc)

	scriptPath := filepath.Join(t.TempDir(), "gate_cap_handler.sh")
	//nolint:gosec // G306: test-only fixture script; not production
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("gateCapScript: WriteFile: %v", err)
	}
	return scriptPath
}

// TestScenario_CommitGateCapTerminates_hki8g59 boots the real DOT cascade driver
// (driveDotWorkflow — the same code path the daemon runs in workflow-mode dot)
// over an isolated worktree + real shell handler, and asserts that an
// always-failing commit_gate whose implementer makes fresh progress on every
// re-entry is bounded by the traversal cap and terminates as a run-failure —
// NEVER an infinite loop.
func TestScenario_CommitGateCapTerminates_hki8g59(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	t.Parallel()

	projectDir, wtPath, parentSHA := rlcFixtureSetup(t)
	scriptPath := gateCapScript(t, wtPath)

	graphDir := t.TempDir()
	dotPath := filepath.Join(graphDir, "gate-cap.dot")
	//nolint:gosec // G306: test-only fixture
	if err := os.WriteFile(dotPath, []byte(gateCapDOT()), 0o644); err != nil {
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

	// Bounded budget. With a bound of capTraversalCap=3 fast in-process gate runs
	// + 4 implementer entries (each a quick shell commit), the cascade terminates
	// in well under this budget. If the cap regressed (unbounded loop), this
	// deadline is the safety net that would surface as a FAILURE — the whole
	// point being that the loop must NOT run to a stale/30-min hang.
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()

	done := make(chan daemon.DotWorkflowResultExported, 1)
	go func() {
		done <- daemon.ExportedDriveDotWorkflow(
			ctx, deps,
			rlFixtureRunID(t),
			core.BeadID("dot-gate-cap-hki8g59"),
			wtPath, parentSHA,
			graph,
		)
	}()

	var result daemon.DotWorkflowResultExported
	select {
	case result = <-done:
	case <-ctx.Done():
		t.Fatalf("hk-i8g59: cascade did not terminate within budget — the traversal "+
			"cap did NOT bound the implement↔commit_gate loop (infinite-loop regression); "+
			"events=%v", collector.eventTypes())
	}

	events := collector.eventTypes()
	t.Logf("hk-i8g59: result=%+v events=%v", result, events)

	// ── Result assertions: terminates as a needs-attention run-failure ────────
	if result.Success {
		t.Errorf("expected success=false on the cap-hit path; summary=%q", result.Summary)
	}
	if !result.NeedsAttention {
		t.Errorf("expected needs_attention=true on the cap-hit path; summary=%q", result.Summary)
	}
	// The defining signal: the loop terminated by HITTING the cap, not by some
	// other mechanism. (Contrast hk-pj4b6, where the summary reports no-progress
	// and explicitly must NOT mention the cap.)
	if !strings.Contains(result.Summary, "traversal cap") {
		t.Errorf("expected summary to report the traversal-cap hit; got %q", result.Summary)
	}

	// ── Distinguishes this from the hk-pj4b6 path ─────────────────────────────
	// Because every implement entry produces a fresh diff, the no-progress check
	// must NEVER fire here. If it did, the test would not actually be exercising
	// the cap as the terminating mechanism.
	for _, et := range events {
		if et == string(core.EventTypeNoProgressDetected) {
			t.Errorf("no_progress_detected must NOT fire when every re-entry makes a "+
				"fresh diff; this scenario must terminate via the traversal cap, not "+
				"no-progress (events=%v)", events)
			break
		}
	}

	// ── Cap-hit event payload: the cascade-engine cap-hit signal ──────────────
	// node_dispatch_decided is emitted on every cascade decision. Find the one
	// that reports the cap hit at the gate node and assert its classification.
	var foundCapHit bool
	var implementAdvances int
	for _, ev := range collector.allEvents() {
		if ev.EventType != string(core.EventTypeNodeDispatchDecided) {
			continue
		}
		var pl core.NodeDispatchDecidedPayload
		if err := json.Unmarshal(ev.Payload, &pl); err != nil {
			t.Fatalf("unmarshal node_dispatch_decided payload: %v", err)
		}
		if !pl.Valid() {
			t.Errorf("node_dispatch_decided payload not Valid(): %+v", pl)
		}
		// Count gate→implement back-edge advances (the bounded loop body).
		if pl.NextNodeID == "implement" && pl.FromNodeID == "gate" {
			implementAdvances++
		}
		// The cap-hit decision: Failed at the gate node, classified as the
		// compilation-loop cap hit (EM-043).
		if pl.Failed && pl.FromNodeID == "gate" && pl.CompletionReason == "cap_hit" {
			foundCapHit = true
			if pl.FailureClass != string(core.FailureClassCompilationLoop) {
				t.Errorf("cap-hit node_dispatch_decided.failure_class = %q; want %q",
					pl.FailureClass, core.FailureClassCompilationLoop)
			}
		}
	}
	if !foundCapHit {
		t.Errorf("no cap-hit node_dispatch_decided event found (Failed at gate with "+
			"completion_reason=cap_hit); events=%v", events)
	}

	// ── Bounded-loop assertion: the back-edge was traversed EXACTLY cap times ─
	// SelectNextEdge admits the gate→implement edge while count<cap and rejects
	// it at count>=cap, with the driver incrementing after each Advance. So the
	// edge is traversed exactly capTraversalCap times before the cap fires. This
	// is the provably-finite bound — the heart of the no-infinite-loop guarantee.
	if implementAdvances != capTraversalCap {
		t.Errorf("gate→implement back-edge traversed %d times; want exactly %d "+
			"(traversal_cap bound). A different count means the cap did not bound "+
			"the loop as specified (EM-043).", implementAdvances, capTraversalCap)
	}

	// ── run_stale must NOT have fired ─────────────────────────────────────────
	// The bug class this guards against surfaces as a run that loops until a
	// ~30-min stale timeout. A clean cap-bounded termination emits no run_stale.
	for _, et := range events {
		if et == string(core.EventTypeRunStale) {
			t.Errorf("run_stale must NOT fire — a cap-bounded loop terminates cleanly, "+
				"not via a stale timeout (events=%v)", events)
			break
		}
	}

	_ = parentSHA
}

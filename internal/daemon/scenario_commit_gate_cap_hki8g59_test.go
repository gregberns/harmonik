//go:build scenario

package daemon_test

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

const capTraversalCap = 3

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
	deps := daemon.ExportedTestRuntime(daemon.TestRuntimeParams{
		BrAdapter:           ledger,
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

	if result.Success {
		t.Errorf("expected success=false on the cap-hit path; summary=%q", result.Summary)
	}
	if !result.NeedsAttention {
		t.Errorf("expected needs_attention=true on the cap-hit path; summary=%q", result.Summary)
	}
	if !strings.Contains(result.Summary, "traversal cap") {
		t.Errorf("expected summary to report the traversal-cap hit; got %q", result.Summary)
	}

	for _, et := range events {
		if et == string(core.EventTypeNoProgressDetected) {
			t.Errorf("no_progress_detected must NOT fire when every re-entry makes a "+
				"fresh diff; this scenario must terminate via the traversal cap, not "+
				"no-progress (events=%v)", events)
			break
		}
	}

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
		if pl.NextNodeID == "implement" && pl.FromNodeID == "gate" {
			implementAdvances++
		}
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

	if implementAdvances != capTraversalCap {
		t.Errorf("gate→implement back-edge traversed %d times; want exactly %d "+
			"(traversal_cap bound). A different count means the cap did not bound "+
			"the loop as specified (EM-043).", implementAdvances, capTraversalCap)
	}

	for _, et := range events {
		if et == string(core.EventTypeRunStale) {
			t.Errorf("run_stale must NOT fire — a cap-bounded loop terminates cleanly, "+
				"not via a stale timeout (events=%v)", events)
			break
		}
	}

	_ = parentSHA
}

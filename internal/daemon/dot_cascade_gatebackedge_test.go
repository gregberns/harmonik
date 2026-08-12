package daemon_test

// dot_cascade_gatebackedge_test.go — the commit_gate→implement back-edge must
// still tell an implementer to fix a gate that RAN and found a fault
// (hk-killed-gate-read-as-red-0hi5z).
//
// This is the INVERSE risk of the killed-gate defect. gateBackEdgeMessage picks
// its wording from the failure class of the last gate FAIL, and driveDotWorkflow
// is the only thing that captures that class (lastGateClass). If the capture
// breaks, every gate FAIL reads as classless, and an implementer whose tests
// really did fail is told that NOTHING is known to be wrong with the change and
// not to invent a fix. It would then commit nothing, and the run would loop until
// the cap.
//
// The unit tests in dot_cascade_gatekilled_test.go call gateBackEdgeMessage
// directly, so they pass with the capture deleted. This one drives the real
// cascade over a real worktree and reads the file the implementer would actually
// receive.
//
// Excluded from -short (real subprocess dispatch + git worktree); runs in the
// plain package run, in `make core` and in the scenario tier.

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
	"github.com/gregberns/harmonik/internal/workspace"
)

// gateBackEdgeDOT is the smallest graph that reaches the back-edge message:
// implement commits, the gate RUNS and reports a genuine test failure, and the
// deterministic back-edge returns to the implementer. The cap is 1 so the run
// terminates after exactly one re-entry.
//
// The gate node MUST be named commit_gate. driveDotWorkflow keys the gate-failure
// message on that literal node ID; any other name takes the no-commit nudge
// instead, whatever the gate did.
//
// The gate output is the shape a real red `make full` produces. It must match
// neither the could-not-run signature (`] Error 127`) nor the killed signature
// (a `*** [` line naming a signal), or a different branch would classify it and
// the test would assert nothing about the deterministic path.
const gateBackEdgeDOT = `digraph "gate-backedge" {
    schema_version="1"; version="1.0"; workflow_id="gate-backedge";
    start_node="start"; terminal_node_ids="close,close-needs-attention";

    start [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];
    implement [type="agentic", agent_type="implementer", handler_ref="claude-implementer", idempotency_class="non-idempotent"];
    commit_gate [type="non-agentic", handler_ref="shell", idempotency_class="idempotent", tool_command="echo '--- FAIL: TestBackEdge (0.02s)'; echo '    backedge_test.go:41: want 3, got 4'; echo 'FAIL'; echo 'make[1]: *** [test] Error 1'; exit 2", timeout="30"];
    close [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];
    "close-needs-attention" [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];

    start -> implement;
    implement -> commit_gate;
    commit_gate -> close [condition="outcome.status == 'SUCCESS'"];
    commit_gate -> implement [condition="outcome.status == 'FAIL' && outcome.failure_class == 'deterministic'", traversal_cap="1"];
    commit_gate -> "close-needs-attention";
}
`

// gateBackEdgeScript writes the implementer handler. Every entry commits a
// unique file, so HEAD advances on each entry and the no-progress guard never
// fires — the run reaches the second implement entry, which is the only entry
// that writes reviewer feedback.
func gateBackEdgeScript(t *testing.T, wtPath string) string {
	t.Helper()
	wtpEsc := strings.ReplaceAll(wtPath, "'", "'\\''")
	script := fmt.Sprintf(`#!/bin/sh
set -e
WTP='%s'
WS="${HARMONIK_WORKSPACE_PATH:-$WTP}"
CNT_FILE="$WTP/.harmonik/backedge_count"
if [ ! -f "$CNT_FILE" ]; then
  printf '0' > "$CNT_FILE"
fi
CNT=$(cat "$CNT_FILE")
CNT=$((CNT + 1))
printf '%%d' "$CNT" > "$CNT_FILE"
printf '%%d' "$CNT" > "$WS/impl_backedge_$CNT.txt"
git -C "$WS" add "impl_backedge_$CNT.txt" >/dev/null 2>&1
git -C "$WS" -c user.email=test@harmonik.local -c user.name="Test" commit -m "impl backedge entry $CNT" --no-gpg-sign >/dev/null 2>&1
exit 0
`, wtpEsc)

	scriptPath := filepath.Join(t.TempDir(), "backedge_handler.sh")
	//nolint:gosec // G306: test-only fixture script; not production
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("gateBackEdgeScript: WriteFile: %v", err)
	}
	return scriptPath
}

// TestDeterministicGateFail_TellsTheImplementerToFixTheFailure drives the real
// cascade and asserts on the file the re-entering implementer reads. A gate that
// RAN and found a fault must produce the fix-the-failure wording, which is only
// possible if driveDotWorkflow captured the gate's failure class and handed it to
// gateBackEdgeMessage.
func TestDeterministicGateFail_TellsTheImplementerToFixTheFailure(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	t.Parallel()

	projectDir, wtPath, parentSHA := rlcFixtureSetup(t)
	scriptPath := gateBackEdgeScript(t, wtPath)

	dotPath := filepath.Join(t.TempDir(), "gate-backedge.dot")
	//nolint:gosec // G306: test-only fixture
	if err := os.WriteFile(dotPath, []byte(gateBackEdgeDOT), 0o644); err != nil {
		t.Fatalf("write DOT: %v", err)
	}
	graph, loadErr := workflow.LoadDotWorkflow(dotPath)
	if loadErr != nil {
		t.Fatalf("LoadDotWorkflow(%s): %v", dotPath, loadErr)
	}

	deps := daemon.ExportedTestRuntime(daemon.TestRuntimeParams{
		BrAdapter:           &stubBeadLedger{},
		Bus:                 &stubEventCollector{},
		ProjectDir:          projectDir,
		HandlerBinary:       "/bin/sh",
		HandlerArgs:         []string{scriptPath},
		IntentLogDir:        filepath.Join(projectDir, ".harmonik", "beads-intents"),
		AdapterRegistry2:    NewSealedAdapterRegistryForTest(t),
		WorkflowModeDefault: core.WorkflowModeDot,
	})

	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()

	done := make(chan daemon.DotWorkflowResultExported, 1)
	go func() {
		done <- daemon.ExportedDriveDotWorkflow(
			ctx, deps, rlFixtureRunID(t),
			core.BeadID("dot-gate-backedge"),
			wtPath, parentSHA, graph,
		)
	}()

	select {
	case result := <-done:
		t.Logf("cascade result: %+v", result)
	case <-ctx.Done():
		t.Fatal("cascade did not terminate within budget")
	}

	// The second implement entry writes feedback for the prior iteration (1).
	fbPath := workspace.ReviewerFeedbackPath(wtPath, 1)
	raw, err := os.ReadFile(fbPath) //nolint:gosec // G304: test-controlled path
	if err != nil {
		t.Fatalf("the gate FAIL never reached the implementer: read %s: %v", fbPath, err)
	}
	fb := string(raw)

	if !strings.Contains(fb, "build/test gate did not pass") ||
		!strings.Contains(fb, "Fix the failure and re-commit") {
		t.Errorf("a gate that RAN and found a fault must tell the implementer to fix it. "+
			"This wording comes from the gate's failure class, which driveDotWorkflow captures; "+
			"without the capture the class is empty and the implementer is told nothing is wrong. Got:\n%s", fb)
	}
	if strings.Contains(fb, "NOTHING is known to be wrong") {
		t.Errorf("the implementer was told nothing is wrong with its change after a gate that RAN and FAILED:\n%s", fb)
	}
	// The gate's own diagnostic must ride along, or the implementer has nothing to fix.
	if !strings.Contains(fb, "TestBackEdge") {
		t.Errorf("the gate output did not reach the implementer:\n%s", fb)
	}
}

package daemon_test

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
		if result.Success {
			t.Errorf("a deterministic gate failure must not report success: %+v", result)
		}
	case <-ctx.Done():
		t.Fatal("cascade did not terminate within budget")
	}

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
	if !strings.Contains(fb, "TestBackEdge") {
		t.Errorf("the gate output did not reach the implementer:\n%s", fb)
	}
}

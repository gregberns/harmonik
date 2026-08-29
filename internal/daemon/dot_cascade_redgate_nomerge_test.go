package daemon_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/workflow"
)

func TestRedCommitGateWithNoReviewerNodeDoesNotMerge(t *testing.T) {
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
	result := daemon.ExportedDriveDotWorkflow(
		ctx, deps, rlFixtureRunID(t), core.BeadID("dot-red-gate-no-reviewer"),
		wtPath, parentSHA, graph,
	)
	if ctx.Err() != nil {
		t.Fatalf("cascade did not terminate within budget: %v", ctx.Err())
	}
	if result.Success {
		t.Errorf("red commit gate reported success: %+v", result)
	}
	if !result.NeedsAttention {
		t.Errorf("red commit gate did not request attention: %+v", result)
	}

	headCmd := exec.CommandContext(t.Context(), "git", "rev-parse", "HEAD")
	headCmd.Dir = wtPath
	headOut, headErr := headCmd.Output()
	if headErr != nil {
		t.Fatalf("resolve worktree HEAD: %v", headErr)
	}
	headSHA := strings.TrimSpace(string(headOut))
	if headSHA == "" || headSHA == parentSHA {
		t.Fatalf("implementer did not advance HEAD: parent=%q head=%q", parentSHA, headSHA)
	}
	if !strings.Contains(result.Summary, headSHA) {
		t.Errorf("failure summary %q does not name committed worktree HEAD %q", result.Summary, headSHA)
	}
}

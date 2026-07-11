package daemon

// harnessregistry_remote_hkr36v_test.go — gate-runnable parity test asserting
// buildCodexRoutedLaunchSpec threads rc.runner into its agent-task.md write for
// a REMOTE run, and uses the unchanged box-A-local path for a LOCAL run (hk-r36v).
//
// Prior to this fix, buildCodexRoutedLaunchSpec called the non-Via
// workspace.WriteAgentTask(rc.workspacePath, ...) unconditionally, discarding
// rc.runner (populated for every remote run): a remote codex bead wrote its task
// brief to box A's local filesystem instead of the worker's. The fix converts the
// call to workspace.WriteAgentTaskVia(ctx, rc.runner, ...), matching the
// established pattern in buildClaudeLaunchSpec (claudelaunchspec.go, hk-z8ek).

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
)

// TestBuildCodexRoutedLaunchSpec_Remote_RoutesAgentTaskThroughRunner asserts
// that when rc.runner is set (remote run), buildCodexRoutedLaunchSpec writes
// agent-task.md THROUGH the runner, targeting the worker-side worktree path —
// never box A's local filesystem.
func TestBuildCodexRoutedLaunchSpec_Remote_RoutesAgentTaskThroughRunner(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	const workerWt = "/Users/gb/harmonik-worker/repo/.harmonik/worktrees/run-r36v"
	rr := newNoOpRecorderZ8ek()

	rc := claudeRunCtx{
		runID:           z8ekRunID(t),
		beadID:          "hk-r36v",
		workspacePath:   workerWt,
		phase:           "implementer-initial",
		iterationCount:  1,
		beadTitle:       "remote codex parity",
		beadDescription: "Verify agent-task.md lands on the worker, not box A.",
		model:           "o4-mini",
		runner:          rr,
	}

	h := NewCodexHarness("", "")
	if _, _, err := buildCodexRoutedLaunchSpec(ctx, rc, h, core.AgentTypeCodex); err != nil {
		t.Fatalf("buildCodexRoutedLaunchSpec (remote): %v", err)
	}

	// No box-A-local agent-task.md must exist for this workspacePath (it's a
	// worker path that doesn't exist on box A, but guard against a stray local
	// write regardless).
	if _, err := os.Stat(filepath.Join(workerWt, ".harmonik", "agent-task.md")); err == nil {
		t.Fatalf("agent-task.md written to box-A local FS at a worker path: %s", workerWt)
	}

	var taskScript string
	for _, c := range rr.Calls {
		if c.Name == "sh" && len(c.Args) == 2 && c.Args[0] == "-lc" && strings.Contains(c.Args[1], "agent-task.md") {
			taskScript = c.Args[1]
		}
	}
	if taskScript == "" {
		t.Fatalf("no remote agent-task.md write recorded; calls=%v", rr.Calls)
	}

	wantTaskDest := filepath.Join(workerWt, ".harmonik", "agent-task.md")
	if !strings.Contains(taskScript, wantTaskDest) {
		t.Errorf("agent-task write does not target worker path %q:\n%s", wantTaskDest, taskScript)
	}
	taskContent := decodeBase64FromScript(t, taskScript)
	if !strings.Contains(taskContent, "Verify agent-task.md lands on the worker, not box A.") {
		t.Errorf("agent-task.md missing bead body:\n%s", taskContent)
	}
}

// TestBuildCodexRoutedLaunchSpec_Local_UsesLocalFS asserts that with a nil
// runner (local run) buildCodexRoutedLaunchSpec writes agent-task.md to box A's
// local filesystem and makes NO runner-routed remote write (NFR7).
func TestBuildCodexRoutedLaunchSpec_Local_UsesLocalFS(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	wt := t.TempDir()
	if err := os.MkdirAll(filepath.Join(wt, ".harmonik"), 0o755); err != nil {
		t.Fatalf("mkdir .harmonik: %v", err)
	}

	rc := claudeRunCtx{
		runID:           z8ekRunID(t),
		beadID:          "hk-r36v-local",
		workspacePath:   wt,
		phase:           "implementer-initial",
		iterationCount:  1,
		beadTitle:       "local codex parity",
		beadDescription: "local body",
		model:           "o4-mini",
		runner:          nil, // LOCAL run
	}

	h := NewCodexHarness("", "")
	if _, _, err := buildCodexRoutedLaunchSpec(ctx, rc, h, core.AgentTypeCodex); err != nil {
		t.Fatalf("buildCodexRoutedLaunchSpec (local): %v", err)
	}

	data, err := os.ReadFile(filepath.Join(wt, ".harmonik", "agent-task.md"))
	if err != nil {
		t.Fatalf("local agent-task.md not written: %v", err)
	}
	if !strings.Contains(string(data), "local body") {
		t.Errorf("local agent-task.md missing bead body:\n%s", data)
	}
}

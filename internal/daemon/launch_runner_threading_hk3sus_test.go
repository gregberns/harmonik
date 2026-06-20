package daemon_test

// launch_runner_threading_hk3sus_test.go — regression tests that the REMOTE
// launch paths thread the run's CommandRunner into the claudeRunCtx they build,
// so buildClaudeLaunchSpec routes the worktree-trust / settings / agent-task
// writes ONTO THE WORKER (runner != nil) rather than box-A-local.
//
// # The defect (hk-3sus)
//
// For a REMOTE review-loop run the trust upsert was landing in BOX A's
// ~/.claude.json instead of the worker's: the per-run worktree on the worker
// stayed untrusted → claude showed the trust/bypass modal → exited in ~9s →
// no_commit_during_implementer. Root cause: runReviewLoop's implRC/revRC and
// dispatchDotAgenticNode's rc were constructed WITHOUT runner:, so it defaulted
// to nil; buildClaudeLaunchSpec then ran EnsureWorktreeTrustVia(nil, …) =
// box-A-local. The fix sets runner: runner on all three claudeRunCtx values,
// symmetric with how MaterializeClaudeSettingsVia / WriteAgentTaskVia already
// reach the worker through rc.runner.
//
// These tests capture rc.runner via a stub launchSpecBuilder and assert it is
// the SAME non-nil runner passed into the dispatch path. They pin against a
// regression to nil: if the runner: field is dropped from either claudeRunCtx,
// the captured runner is nil and the test fails.

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	tmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	"github.com/gregberns/harmonik/internal/workflow/dot"
)

// hk3susFakeRunner is a sentinel CommandRunner that passes commands through to a
// real local exec.Command. The pass-through is needed because the DOT / review-
// loop dispatch paths run git probes (rev-parse HEAD, diff-hash) THROUGH the
// runner against the test's real local worktree BEFORE reaching the
// launchSpecBuilder; a no-op runner would make HEAD resolution fail and the
// cascade would bail before dispatch. The distinct type is the sentinel the test
// asserts on (it must be the SAME runner the dispatch threaded into the rc).
type hk3susFakeRunner struct{}

func (hk3susFakeRunner) Command(ctx context.Context, name string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, name, args...)
}

func hk3susInitProject(t *testing.T) string {
	t.Helper()
	projectDir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = projectDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "--initial-branch=main")
	run("config", "user.email", "test@harmonik.local")
	run("config", "user.name", "Harmonik Test")
	run("commit", "--allow-empty", "-m", "Initial commit")
	return projectDir
}

// TestReviewLoopThreadsRunnerIntoRunCtx_hk3sus proves runReviewLoop passes the
// non-nil CommandRunner it receives (the REMOTE sshRunner) into the implementer
// claudeRunCtx, so the worktree-trust write lands on the worker (hk-3sus).
func TestReviewLoopThreadsRunnerIntoRunCtx_hk3sus(t *testing.T) {
	t.Parallel()

	projectDir := hk3susInitProject(t)
	wtPath, parentSHA := implReadyFixtureWorktree(t, projectDir)
	runID := implReadyFixtureRunID(t)
	adapterReg := NewSealedAdapterRegistryForTest(t)

	captured := make(chan tmux.CommandRunner, 1)
	lsb := daemon.ExportedCaptureRunnerBuilder(captured)

	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:           &stubBeadLedger{},
		Bus:                 &stubEventCollector{},
		ProjectDir:          projectDir,
		HandlerBinary:       "/bin/sh",
		HandlerArgs:         []string{"-c", "exit 0"},
		IntentLogDir:        filepath.Join(projectDir, ".harmonik", "beads-intents"),
		WorkflowModeDefault: core.WorkflowModeReviewLoop,
		AdapterRegistry2:    adapterReg,
		HookStore:           daemon.ExportedNewHookSessionStore(),
		LaunchSpecBuilder:   lsb,
	})

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	want := hk3susFakeRunner{}
	_ = daemon.ExportedRunReviewLoopWithRunner(ctx, deps, runID, core.BeadID("hk-3sus-rl-test-001"), wtPath, parentSHA, want)

	select {
	case got := <-captured:
		if got == nil {
			t.Fatal("review-loop implementer claudeRunCtx.runner is nil; remote trust/settings/agent-task writes would land box-A-local (hk-3sus regression)")
		}
		if _, ok := got.(hk3susFakeRunner); !ok {
			t.Fatalf("review-loop runner = %T; want the sentinel hk3susFakeRunner passed into runReviewLoop", got)
		}
	default:
		t.Fatal("launchSpecBuilder was never called — runner not captured")
	}
}

// TestDotThreadsRunnerIntoRunCtx_hk3sus proves dispatchDotAgenticNode (via
// driveDotWorkflow) passes the non-nil CommandRunner it receives into the
// agentic-node claudeRunCtx, so the worktree-trust write lands on the worker for
// a REMOTE DOT run (hk-3sus).
func TestDotThreadsRunnerIntoRunCtx_hk3sus(t *testing.T) {
	t.Parallel()

	projectDir := hk3susInitProject(t)
	wtPath, parentSHA := implReadyFixtureWorktree(t, projectDir)
	runID := implReadyFixtureRunID(t)
	adapterReg := NewSealedAdapterRegistryForTest(t)

	captured := make(chan tmux.CommandRunner, 1)
	lsb := daemon.ExportedCaptureRunnerBuilder(captured)

	graph := &dot.Graph{
		StartNodeID:     "implement",
		TerminalNodeIDs: []string{"close"},
		Nodes: []*dot.Node{
			{
				ID:               "implement",
				Type:             core.NodeTypeAgentic,
				AgentType:        "implementer",
				HandlerRef:       "claude-implementer",
				IdempotencyClass: "non-idempotent",
			},
		},
		UnknownAttrs: map[string]string{},
	}

	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:           &stubBeadLedger{},
		Bus:                 &stubEventCollector{},
		ProjectDir:          projectDir,
		HandlerBinary:       "/bin/sh",
		HandlerArgs:         []string{"-c", "exit 0"},
		IntentLogDir:        filepath.Join(projectDir, ".harmonik", "beads-intents"),
		WorkflowModeDefault: core.WorkflowModeDot,
		AdapterRegistry2:    adapterReg,
		HookStore:           daemon.ExportedNewHookSessionStore(),
		LaunchSpecBuilder:   lsb,
	})

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	want := hk3susFakeRunner{}
	_ = daemon.ExportedDriveDotWorkflowWithRunner(
		ctx, deps, runID, core.BeadID("hk-3sus-dot-test-001"),
		"implement task", "bead body",
		wtPath, parentSHA, graph, want,
	)

	select {
	case got := <-captured:
		if got == nil {
			t.Fatal("DOT agentic-node claudeRunCtx.runner is nil; remote trust/settings/agent-task writes would land box-A-local (hk-3sus regression)")
		}
		if _, ok := got.(hk3susFakeRunner); !ok {
			t.Fatalf("DOT runner = %T; want the sentinel hk3susFakeRunner passed into driveDotWorkflow", got)
		}
	default:
		t.Fatal("launchSpecBuilder was never called — runner not captured")
	}
}

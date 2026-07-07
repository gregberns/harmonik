package daemon_test

// launch_substrate_runner_threading_hkfxy9_test.go — regression tests that the
// REMOTE launch paths thread the run's CommandRunner into the SUBSTRATE-SPAWN
// constructor (newPerRunSubstrate), so the claude PROCESS spawns ON THE WORKER's
// tmux server and the pasteinject/stat probes run there too.
//
// # The defect (hk-fxy9 review-loop / hk-538l DOT)
//
// hk-3sus fixed the SPEC runner (claudeRunCtx.runner — controls WHERE trust /
// settings / agent-task files are written). But there is a SECOND, independent
// runner threading: the SUBSTRATE-SPAWN runner passed to
// newPerRunSubstrate(sub, bin, runner) — it controls WHERE the claude PROCESS
// spawns. For the review-loop implementer this was hardcoded nil, so even with
// the SPEC runner correctly pointed at the worker, the process spawned against
// box A's tmux/-default session (which does not exist on the worker) → wedge at
// launch_initiated → agent_ready_timeout → no_commit.
//
// The existing launch_runner_threading_hk3sus_test.go only covers the SPEC
// runner (captured via the launchSpecBuilder stub, which short-circuits BEFORE
// reaching newPerRunSubstrate). These tests capture the SUBSTRATE runner via the
// package test seam (ExportedSetSubstrateRunnerObserver), which fires at the
// newPerRunSubstrate call site AFTER spec-build. They pin against a regression to
// nil: if `runner` is dropped from either newPerRunSubstrate call, the captured
// substrate runner is nil and the test fails.

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	tmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	"github.com/gregberns/harmonik/internal/workflow/dot"
)

// hkfxy9HangHandlerScript writes a shell handler that hangs indefinitely (never
// emits agent_ready), so the launch path reaches newPerRunSubstrate, fires the
// substrate-runner seam, then returns via the short agent_ready timeout rather
// than blocking the test.
func hkfxy9HangHandlerScript(t *testing.T) string {
	t.Helper()
	script := "#!/bin/sh\nsleep 3600\n"
	scriptPath := filepath.Join(t.TempDir(), "hkfxy9_hang_handler.sh")
	//nolint:gosec // G306: test-only fixture script; not production
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("hkfxy9HangHandlerScript: WriteFile: %v", err)
	}
	return scriptPath
}

// TestReviewLoopThreadsRunnerIntoSubstrate_hkfxy9 proves the review-loop
// implementer launch passes the non-nil CommandRunner it receives (the REMOTE
// sshRunner) into newPerRunSubstrate, so the claude PROCESS spawns on the worker
// (hk-fxy9). Pins against a regression to the previously-hardcoded nil.
func TestReviewLoopThreadsRunnerIntoSubstrate_hkfxy9(t *testing.T) {
	skipRealDaemonE2EInShort(t) // spawns real tmux pane (sleep 3600 hang-handler) — reap can time out, leaving zombie pane that wedges sibling tests
	// NOT parallel: installs a process-global test seam + isolates ~/.claude.json.
	rlIsolateClaudeConfig(t)

	projectDir := implReadyFixtureProjectDir(t)
	wtPath, parentSHA := implReadyFixtureWorktree(t, projectDir)
	scriptPath := hkfxy9HangHandlerScript(t)
	adapterReg := implReadyFixtureAdapterRegistry(t)

	captured := make(chan tmux.CommandRunner, 4)
	daemon.ExportedSetSubstrateRunnerObserver(func(r tmux.CommandRunner) {
		select {
		case captured <- r:
		default:
		}
	})
	t.Cleanup(func() { daemon.ExportedSetSubstrateRunnerObserver(nil) })

	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:           &stubBeadLedger{},
		Bus:                 &stubEventCollector{},
		ProjectDir:          projectDir,
		HandlerBinary:       "/bin/sh",
		HandlerArgs:         []string{scriptPath},
		IntentLogDir:        filepath.Join(projectDir, ".harmonik", "beads-intents"),
		WorkflowModeDefault: core.WorkflowModeReviewLoop,
		AdapterRegistry2:    adapterReg,
		AgentReadyTimeout:   100 * time.Millisecond,
		HookStore:           daemon.ExportedNewHookSessionStore(),
	})

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	want := hk3susFakeRunner{}
	_ = daemon.ExportedRunReviewLoopWithRunner(ctx, deps, implReadyFixtureRunID(t),
		core.BeadID("hk-fxy9-rl-substrate-001"), wtPath, parentSHA, want)

	select {
	case got := <-captured:
		if got == nil {
			t.Fatal("review-loop implementer newPerRunSubstrate runner is nil; the claude PROCESS would spawn against box A's tmux/-default session, not the worker → launch_initiated wedge → agent_ready_timeout (hk-fxy9 regression)")
		}
		if _, ok := got.(hk3susFakeRunner); !ok {
			t.Fatalf("review-loop substrate runner = %T; want the sentinel hk3susFakeRunner passed into runReviewLoop", got)
		}
	default:
		t.Fatal("substrate-runner seam was never invoked — launch path did not reach newPerRunSubstrate")
	}
}

// TestDotThreadsRunnerIntoSubstrate_hkfxy9 proves the DOT agentic-node launch
// passes the non-nil CommandRunner it receives into newPerRunSubstrate, so the
// claude PROCESS spawns on the worker for a REMOTE DOT run (hk-538l). DOT already
// threaded the SPEC runner (hk-3sus); this covers the SUBSTRATE runner.
func TestDotThreadsRunnerIntoSubstrate_hkfxy9(t *testing.T) {
	skipRealDaemonE2EInShort(t) // spawns real tmux pane (sleep 3600 hang-handler) — reap can time out, leaving zombie pane that wedges sibling tests
	// NOT parallel: installs a process-global test seam + isolates ~/.claude.json.
	rlIsolateClaudeConfig(t)

	projectDir := implReadyFixtureProjectDir(t)
	wtPath, parentSHA := implReadyFixtureWorktree(t, projectDir)
	scriptPath := hkfxy9HangHandlerScript(t)
	adapterReg := implReadyFixtureAdapterRegistry(t)

	captured := make(chan tmux.CommandRunner, 4)
	daemon.ExportedSetSubstrateRunnerObserver(func(r tmux.CommandRunner) {
		select {
		case captured <- r:
		default:
		}
	})
	t.Cleanup(func() { daemon.ExportedSetSubstrateRunnerObserver(nil) })

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
		HandlerArgs:         []string{scriptPath},
		IntentLogDir:        filepath.Join(projectDir, ".harmonik", "beads-intents"),
		WorkflowModeDefault: core.WorkflowModeDot,
		AdapterRegistry2:    adapterReg,
		AgentReadyTimeout:   100 * time.Millisecond,
		HookStore:           daemon.ExportedNewHookSessionStore(),
	})

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	want := hk3susFakeRunner{}
	_ = daemon.ExportedDriveDotWorkflowWithRunner(
		ctx, deps, implReadyFixtureRunID(t), core.BeadID("hk-fxy9-dot-substrate-001"),
		"implement task", "bead body",
		wtPath, parentSHA, graph, want,
	)

	select {
	case got := <-captured:
		if got == nil {
			t.Fatal("DOT agentic-node newPerRunSubstrate runner is nil; the claude PROCESS would spawn against box A's tmux/-default session, not the worker → agent_ready_timeout (hk-538l regression)")
		}
		if _, ok := got.(hk3susFakeRunner); !ok {
			t.Fatalf("DOT substrate runner = %T; want the sentinel hk3susFakeRunner passed into driveDotWorkflow", got)
		}
	default:
		t.Fatal("substrate-runner seam was never invoked — DOT launch path did not reach newPerRunSubstrate")
	}
}

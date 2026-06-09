package daemon_test

// dot_launch_initiated_hkgoczd_test.go — regression test: the DOT cascade
// agentic-node dispatch must emit launch_initiated once the window is live, so
// the stale watcher does not flag a false-positive launch_stall_detected on
// every DOT-mode run (hk-goczd).
//
// # The bug
//
// The DOT cascade (dot_cascade.go) and DOT gate (dot_gate.go) dispatch paths
// emitted ZERO CHB-018 pre-exec progress messages — including launch_initiated.
// The single-mode (workloop.go:2098/2137) and review-loop (reviewloop.go:336)
// paths both emit them. The stale watcher's launch_stall_detected keys solely on
// launch_initiated absence within 30 s of run_started (stalewatch.go:296), so it
// fired a cosmetic false-positive launch_stall_detected on EVERY DOT dispatch —
// even when the implementer spawned fine and the run completed. Live evidence:
// spawn_cap_blocked=0 and tmux_new_window_timeout=0 across all stalls, stable
// goroutine count, every "stalled" run reaching a terminal state — i.e. no slot
// leak, a pure phantom-stall signal.
//
// # The fix
//
// dispatchDotAgenticNode now emits the non-launch_initiated pre-exec messages
// before Launch and the held-back launch_initiated after a successful Launch
// (mirroring workloop / reviewloop). This test drives the real DOT cascade
// dispatch path with a /bin/sh handler whose Launch succeeds (window spawns) and
// asserts launch_initiated appears in the emitted-event stream.
//
// # Bead
//
//   - hk-goczd (DOT-path launch_initiated / false launch_stall_detected).

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/workflow"
)

// TestScenario_DotMode_EmitsLaunchInitiated drives the DOT cascade implementer
// dispatch and asserts launch_initiated is emitted once the window is live —
// the hk-goczd fix that eliminates the phantom launch_stall_detected.
//
// It reuses the same fixtures as TestScenario_DotMode_ImplementerAgentReadyTimeout
// (a real git project + worktree, a hanging /bin/sh handler, and an AdapterRegistry
// with the ClaudeCodeAdapter). The handler hangs without emitting agent_ready, so
// the dispatch ultimately fails via the agent_ready_timeout path — but Launch
// itself SUCCEEDS (the /bin/sh window spawns), so the held-back launch_initiated
// is emitted before the timeout fires. Pre-fix, no pre-exec message was ever
// emitted on the DOT path and this assertion fails.
func TestScenario_DotMode_EmitsLaunchInitiated(t *testing.T) {
	t.Parallel()

	projectDir := implReadyFixtureProjectDir(t)
	wtPath, parentSHA := implReadyFixtureWorktree(t, projectDir)
	scriptPath := implReadyFixtureHandlerScript(t)
	adapterReg := implReadyFixtureAdapterRegistry(t)

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
		WorkflowModeDefault: core.WorkflowModeDot,
		AdapterRegistry2:    adapterReg,
		// Short agent_ready timeout: the handler hangs, so the dispatch ends via
		// the agent_ready_timeout path. Launch (the /bin/sh window spawn) succeeds
		// first, so launch_initiated is emitted before the timeout.
		AgentReadyTimeout: 100 * time.Millisecond,
		HookStore:         daemon.ExportedNewHookSessionStore(),
	})

	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()

	_ = daemon.ExportedDriveDotWorkflow(
		ctx, deps,
		implReadyFixtureRunID(t),
		core.BeadID("dot-launch-initiated-001"),
		wtPath, parentSHA,
		graph,
	)

	eventTypes := collector.eventTypes()
	t.Logf("TestScenario_DotMode_EmitsLaunchInitiated: events=%v", eventTypes)

	launchInitiatedFound := false
	for _, et := range eventTypes {
		if et == string(core.EventTypeLaunchInitiated) {
			launchInitiatedFound = true
			break
		}
	}
	if !launchInitiatedFound {
		t.Errorf("DotMode launch_initiated FAIL (hk-goczd): launch_initiated event not emitted on the DOT cascade path — the stale watcher will flag a phantom launch_stall_detected; got events: %v", eventTypes)
	}
}

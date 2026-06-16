package daemon_test

// dot_heartbeat_hknvjk_test.go — regression test: the DOT cascade and DOT gate
// dispatch paths must emit agent_heartbeat events after launch_initiated so the
// stale watcher's last_event_type never stays frozen at "launch_initiated" for
// the duration of a healthy in-flight run (hk-nvjk).
//
// # The bug
//
// dispatchDotAgenticNode (dot_cascade.go) and dispatchDotGateNode (dot_gate.go)
// both emitted launch_initiated after a successful Launch (fixed by hk-goczd),
// but neither started the CHB-019 heartbeat goroutine (handler.RunHeartbeatLoop).
// The single-mode beadRunOne (workloop.go Step 5) and the review-loop implementer
// phase (reviewloop.go) DID start it. Without the goroutine, no agent_heartbeat
// events carried run_id were emitted to the bus for DOT-mode runs, so the stale
// watcher's observe() never advanced lastEventType past "launch_initiated".
// After staleAfter (10 min default), checkRun() fired and emitted run_stale with
// last_event_type="launch_initiated" — a false positive for every DOT dispatch.
//
// # The fix
//
// dispatchDotAgenticNode and dispatchDotGateNode now start handler.RunHeartbeatLoop
// immediately after the held-back launch_initiated is emitted, using the same
// pattern as workloop.go Step 5. RunHeartbeatLoop emits the first agent_heartbeat
// immediately on goroutine start (before the 300 s ticker), so the stale watcher
// observes a heartbeat within milliseconds of launch.
//
// # This test
//
// Drives the real DOT cascade dispatch (ExportedDriveDotWorkflow) with a
// handler that hangs — same fixture as TestScenario_DotMode_EmitsLaunchInitiated.
// Launch succeeds (window spawns), the heartbeat goroutine starts, and the first
// heartbeat is emitted immediately. The agent_ready timeout (100 ms) then fires,
// ending the dispatch. The test asserts that the event stream contains at least
// one agent_heartbeat event — verifying the goroutine was started.
//
// Bead: hk-nvjk.

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/workflow"
)

// TestScenario_DotMode_EmitsHeartbeatAfterLaunch verifies that the DOT cascade
// dispatch path emits agent_heartbeat events (with run_id) after launch_initiated
// — the hk-nvjk fix that prevents last_event_type from staying frozen at
// "launch_initiated" in the stale watcher.
func TestScenario_DotMode_EmitsHeartbeatAfterLaunch(t *testing.T) {
	skipRealDaemonE2EInShort(t)
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
		// the agent_ready_timeout path. The heartbeat goroutine fires immediately
		// on start, so agent_heartbeat appears before the timeout.
		AgentReadyTimeout: 500 * time.Millisecond,
		HookStore:         daemon.ExportedNewHookSessionStore(),
	})

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	_ = daemon.ExportedDriveDotWorkflow(
		ctx, deps,
		implReadyFixtureRunID(t),
		core.BeadID("dot-heartbeat-hknvjk-001"),
		wtPath, parentSHA,
		graph,
	)

	eventTypes := collector.eventTypes()
	t.Logf("TestScenario_DotMode_EmitsHeartbeatAfterLaunch: events=%v", eventTypes)

	heartbeatFound := false
	for _, et := range eventTypes {
		if et == string(core.EventTypeAgentHeartbeat) {
			heartbeatFound = true
			break
		}
	}
	if !heartbeatFound {
		t.Errorf("DotMode heartbeat FAIL (hk-nvjk): agent_heartbeat not emitted on the DOT cascade path — "+
			"the stale watcher's last_event_type will remain frozen at \"launch_initiated\" for the full run, "+
			"causing false-positive run_stale; got events: %v", eventTypes)
	}
}

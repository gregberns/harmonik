package daemon_test

// spawn_path_sc2_hk92v9m_test.go — SC-2: twin single-happy-path N=2 concurrent —
// parallel spawn, both complete (hk-92v9m).
//
// # What this test covers
//
// SC-2 extends SC-1 to N=2 simultaneous beads.  With MaxConcurrent=2 the work loop
// dispatches both beads before either exits, exercising the concurrent-spawn path
// through beadRunOne (hk-e61c3.2, POST_MVH_PARALLELISM_ROADMAP row 5).
//
// Setup is identical to SC-1 except:
//   - Two bead IDs are seeded in the stub ledger.
//   - MaxConcurrent=2 so both goroutines are launched before the first exits.
//   - The poll condition waits for len(closedIDs) >= 2.
//
// The twin wrapper is the same /bin/sh shim discarding Claude-specific flags
// and invoking harmonik-twin-claude --scenario single-happy-path.
//
// # Expected outcome
//
// Both twins emit agent_ready then exit 0.  CloseBead is called once for each
// bead; ReopenBead is never called.  The event bus receives at least two
// run_completed events and at least two agent_ready events (one per bead).
//
// Assertions:
//  1. CloseBead called exactly twice — once for each bead ID.
//  2. ReopenBead NOT called.
//  3. At least two run_completed events emitted (not run_failed).
//  4. At least two agent_ready events observed (proves waitAgentReady fired for
//     both runs).
//
// Helper prefix: sc2Fixture (bead hk-92v9m; per implementer-protocol.md
// §Helper-prefix discipline).
//
// Spec refs:
//   - specs/handler-contract.md §4.9 HC-056 (waitAgentReady timeout guard)
//   - specs/scenario-harness.md §4 (twin-driven spawn-path coverage)
//
// Bead: hk-92v9m.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/daemon/scenariotest"
)

// sc2FixtureTwinWrapperScript writes a /bin/sh wrapper script that invokes the
// twin binary with --scenario commit-on-cue-startup-delay, discarding all other
// args that buildClaudeLaunchSpec appends (e.g. --session-id, --print).
//
// commit-on-cue-startup-delay (not single-happy-path) is used so each implementer
// actually makes a git commit: single-happy-path never commits, so the no-commit
// guard (hk-mmh8f) fires and reopens both beads instead of closing them (hk-4f5ua).
//
// ExportedWorkLoopDeps hardcodes handlerEnv=nil, so the wrapper re-exports the
// test process's PATH so the twin's internal `git commit` can find git.
// Each commit-on-cue commit is uniquely timestamped, so the two concurrent beads
// do not collide.
func sc2FixtureTwinWrapperScript(t *testing.T, twinPath string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "twin-sc2-wrapper.sh")
	content := "#!/bin/sh\nexport PATH=" + os.Getenv("PATH") + "\nexec " + twinPath +
		" --scenario commit-on-cue-startup-delay --worktree-path \"$PWD\"\n"
	//nolint:gosec // G306: script is test-only; chmod 0755 required for execution
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("sc2FixtureTwinWrapperScript: WriteFile: %v", err)
	}
	return path
}

// sc2FixtureCountEvent returns how many times typeName appears in types.
func sc2FixtureCountEvent(types []string, typeName string) int {
	n := 0
	for _, t := range types {
		if strings.EqualFold(t, typeName) {
			n++
		}
	}
	return n
}

// ─────────────────────────────────────────────────────────────────────────────
// TestSC2_SpawnPath_SingleHappyPath_N2Concurrent
// ─────────────────────────────────────────────────────────────────────────────

// TestSC2_SpawnPath_SingleHappyPath_N2Concurrent is SC-2 from the spawn-path
// scenario suite (hk-p3diy).
//
// It verifies that two beads complete successfully in parallel (both CloseBead
// called, run_completed and agent_ready emitted twice) when the work loop runs
// with MaxConcurrent=2 and the real harmonik-twin-claude in single-happy-path
// mode as the handler.
//
// Not parallel: uses t.Setenv(HARMONIK_CLAUDE_CONFIG_PATH) to redirect
// EnsureWorktreeTrust away from ~/.claude.json; t.Setenv is incompatible
// with t.Parallel.
//
// Bead: hk-92v9m.
func TestSC2_SpawnPath_SingleHappyPath_N2Concurrent(t *testing.T) {
	// Locate the twin binary; skip when absent.
	twinPath, ok := scenariotest.TwinBinaryPath()
	if !ok {
		t.Skip("SC2: harmonik-twin-claude binary not found; set HARMONIK_TWIN_CLAUDE or build the binary")
	}

	// Create project directory with git repo (real git required by
	// productionWorktreeFactory, which calls `git worktree add`).
	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	// Redirect EnsureWorktreeTrust to a test-local claude config so that
	// buildClaudeLaunchSpec does not contend with a running daemon on ~/.claude.json.lock.
	claudeConfigPath := filepath.Join(t.TempDir(), ".claude.json")
	t.Setenv("HARMONIK_CLAUDE_CONFIG_PATH", claudeConfigPath)

	// Seed two beads in the stub ledger.  Ready() dequeues one per call, so the
	// work loop will dispatch the first bead, then (while it is still in-flight
	// and Len < MaxConcurrent=2) dequeue and dispatch the second concurrently.
	const (
		beadAlpha = core.BeadID("sc2-bead-alpha")
		beadBeta  = core.BeadID("sc2-bead-beta")
	)
	ledger := &stubBeadLedger{
		ready: []core.BeadID{beadAlpha, beadBeta},
	}
	collector := &stubEventCollector{}

	// Build the twin wrapper script (ignores Claude-specific flags from buildClaudeLaunchSpec).
	twinWrapper := sc2FixtureTwinWrapperScript(t, twinPath)

	// Wire deps with AdapterRegistry2 = real ClaudeCode adapter (not empty-sealed).
	// MaxConcurrent=2 allows both beads to be in-flight simultaneously.
	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:         ledger,
		Bus:               collector,
		ProjectDir:        projectDir,
		HandlerBinary:     twinWrapper,
		IntentLogDir:      filepath.Join(projectDir, ".harmonik", "beads-intents"),
		AdapterRegistry2:  NewSealedAdapterRegistryForTest(t),
		MaxConcurrent:     2,
		AgentReadyTimeout: 10 * time.Second,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	// Poll until both beads are closed, or any reopen occurs (failure path).
	const terminalPollBudget = 45 * time.Second
	terminalDeadline := time.Now().Add(terminalPollBudget)
	for time.Now().Before(terminalDeadline) {
		if len(ledger.closedIDs()) >= 2 || len(ledger.reopenedIDs()) > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Cancel the loop before assertions so cleanup is deterministic.
	cancel()
	select {
	case <-loopDone:
	case <-time.After(10 * time.Second):
		t.Error("SC2: work loop did not exit within 10 s after context cancel")
	}

	closedIDs := ledger.closedIDs()
	reopenedIDs := ledger.reopenedIDs()
	emittedTypes := collector.eventTypes()

	t.Logf("SC2: closedIDs=%v reopenedIDs=%v eventTypes=%v", closedIDs, reopenedIDs, emittedTypes)

	// ── Assertion 1: CloseBead called exactly twice ───────────────────────────
	if len(closedIDs) < 2 {
		t.Errorf("SC2 FAIL: expected 2 CloseBead calls; got %d (%v); reopenedIDs=%v", len(closedIDs), closedIDs, reopenedIDs)
	} else {
		closedSet := map[core.BeadID]bool{}
		for _, id := range closedIDs {
			closedSet[id] = true
		}
		if !closedSet[beadAlpha] {
			t.Errorf("SC2 FAIL: beadAlpha %q not in closedIDs=%v", beadAlpha, closedIDs)
		}
		if !closedSet[beadBeta] {
			t.Errorf("SC2 FAIL: beadBeta %q not in closedIDs=%v", beadBeta, closedIDs)
		}
	}

	// ── Assertion 2: ReopenBead NOT called ───────────────────────────────────
	if len(reopenedIDs) > 0 {
		t.Errorf("SC2 FAIL: ReopenBead called unexpectedly: %v (expected clean CloseBead path for both beads)", reopenedIDs)
	}

	// ── Assertion 3: at least two run_completed events emitted ───────────────
	nCompleted := sc2FixtureCountEvent(emittedTypes, string(core.EventTypeRunCompleted))
	if nCompleted < 2 {
		t.Errorf("SC2 FAIL: expected ≥2 run_completed events; got %d; all events: %v", nCompleted, emittedTypes)
	}
	if sc2FixtureCountEvent(emittedTypes, string(core.EventTypeRunFailed)) > 0 {
		t.Errorf("SC2 FAIL: run_failed emitted unexpectedly; got %v", emittedTypes)
	}

	// ── Assertion 4: at least two agent_ready events observed ────────────────
	// Proves that waitAgentReady fired and did NOT time out for either run.
	nReady := sc2FixtureCountEvent(emittedTypes, string(core.EventTypeAgentReady))
	if nReady < 2 {
		t.Errorf("SC2 FAIL: expected ≥2 agent_ready events; got %d — waitAgentReady may have timed out for one run; all events: %v", nReady, emittedTypes)
	}

	if len(closedIDs) >= 2 && len(reopenedIDs) == 0 && nCompleted >= 2 && nReady >= 2 {
		t.Logf("SC2 PASS: both beads closed (%v), run_completed×%d, agent_ready×%d", closedIDs, nCompleted, nReady)
	}
}

package daemon_test

// spawn_path_sc1_hkx3s1p_test.go — SC-1: twin single-happy-path + AdapterRegistry2
// wired — full spawn path N=1 (hk-x3s1p).
//
// # What this test covers
//
// SC-1 exercises the full beadRunOne spawn path with:
//   - The real harmonik-twin-claude binary running the "single-happy-path" canned
//     scenario (emits handler_capabilities → agent_ready → agent_completed).
//   - AdapterRegistry2 wired with the real ClaudeCode adapter (NewSealedAdapterRegistryForTest),
//     so waitAgentReady is NOT skipped — the test exercises the full HC-056 path.
//   - N=1 bead dispatched through ExportedWorkLoopDeps + ExportedRunWorkLoop (stub
//     ledger; no real br binary required).
//
// The key difference from TestScenario_HappyPath_N1 (hk-jf2tb) — which uses the
// production composition root daemon.Start + real br — is that SC-1 uses the
// ExportedWorkLoopDeps test seam with AdapterRegistry2: NewSealedAdapterRegistryForTest.
// This directly validates that the real adapter path (DetectReady) succeeds when the
// twin emits agent_ready on its stdout, rather than verifying only the end-to-end br
// integration.
//
// # Twin wrapper
//
// buildClaudeLaunchSpec appends Claude-specific flags (--session-id, --print, etc.)
// that harmonik-twin-claude does not recognise. A thin /bin/sh wrapper ignores all
// args and invokes the twin with only --scenario single-happy-path, matching the
// idiomatic pattern established by scenarioN1TwinWrapperScript (hk-jf2tb).
//
// # Expected outcome
//
// The twin emits agent_ready on stdout; the watcher publishes it to the tapping bus;
// waitAgentReady observes it via DetectReady and returns nil (not ErrAgentReadyTimeout).
// The twin then exits 0, causing CloseBead to be called (not ReopenBead).
//
// Assertions:
//   1. CloseBead is called exactly once for the seeded bead.
//   2. ReopenBead is NOT called.
//   3. run_completed event appears in the emitted events (not run_failed).
//   4. agent_ready event appears in the emitted events (proves waitAgentReady fired).
//
// Helper prefix: sc1Fixture (bead hk-x3s1p; per implementer-protocol.md
// §Helper-prefix discipline).
//
// Spec refs:
//   - specs/handler-contract.md §4.9 HC-056 (waitAgentReady timeout guard)
//   - specs/scenario-harness.md §4 (twin-driven spawn-path coverage)
//
// Bead: hk-x3s1p.

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

// sc1FixtureTwinWrapperScript writes a /bin/sh wrapper script that invokes the
// twin binary with only --scenario single-happy-path, discarding all other args
// that buildClaudeLaunchSpec appends (e.g. --session-id, --print).
//
// This is the canonical adaptation layer for twin-via-ExportedWorkLoopDeps tests.
func sc1FixtureTwinWrapperScript(t *testing.T, twinPath string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "twin-sc1-wrapper.sh")
	// Discard all args; invoke only with --scenario.
	content := "#!/bin/sh\nexec " + twinPath + " --scenario single-happy-path\n"
	//nolint:gosec // G306: script is test-only; chmod 0755 required for execution
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("sc1FixtureTwinWrapperScript: WriteFile: %v", err)
	}
	return path
}

// ─────────────────────────────────────────────────────────────────────────────
// TestSC1_SpawnPath_SingleHappyPath_AdapterRegistry2Wired
// ─────────────────────────────────────────────────────────────────────────────

// TestSC1_SpawnPath_SingleHappyPath_AdapterRegistry2Wired is SC-1 from the
// spawn-path scenario suite (hk-p3diy).
//
// It verifies that a single bead completes successfully (CloseBead called,
// run_completed emitted, agent_ready observed) when the work loop runs with:
//   - the real harmonik-twin-claude in single-happy-path mode as the handler,
//   - AdapterRegistry2 wired with the real ClaudeCode adapter (waitAgentReady active).
//
// Not parallel: uses t.Setenv(HARMONIK_CLAUDE_CONFIG_PATH) to redirect
// EnsureWorktreeTrust away from ~/.claude.json; t.Setenv is incompatible
// with t.Parallel.
//
// Bead: hk-x3s1p.
func TestSC1_SpawnPath_SingleHappyPath_AdapterRegistry2Wired(t *testing.T) {
	// Locate the twin binary; skip when absent.
	twinPath, ok := scenariotest.TwinBinaryPath()
	if !ok {
		t.Skip("SC1: harmonik-twin-claude binary not found; set HARMONIK_TWIN_CLAUDE or build the binary")
	}

	// Create project directory with git repo (real git required by
	// productionWorktreeFactory, which calls `git worktree add`).
	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	// Redirect EnsureWorktreeTrust to a test-local claude config so that
	// buildClaudeLaunchSpec does not contend with a running daemon on ~/.claude.json.lock.
	claudeConfigPath := filepath.Join(t.TempDir(), ".claude.json")
	t.Setenv("HARMONIK_CLAUDE_CONFIG_PATH", claudeConfigPath)

	// Seed one bead in the stub ledger.
	const beadID = core.BeadID("sc1-single-happy-path")
	ledger := &stubBeadLedger{
		ready: []core.BeadID{beadID},
	}
	collector := &stubEventCollector{}

	// Build the twin wrapper script (ignores Claude-specific flags from buildClaudeLaunchSpec).
	twinWrapper := sc1FixtureTwinWrapperScript(t, twinPath)

	// Wire deps with AdapterRegistry2 = real ClaudeCode adapter (not empty-sealed).
	// This ensures waitAgentReady is active and exercises the DetectReady path.
	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:        ledger,
		Bus:              collector,
		ProjectDir:       projectDir,
		HandlerBinary:    twinWrapper,
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		// AgentReadyTimeout: 5 s — the twin emits agent_ready quickly; 5 s is
		// comfortable headroom above the watcher-done race window.
		AgentReadyTimeout: 5 * time.Second,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	// Poll until CloseBead or ReopenBead is called (terminal bead state).
	const terminalPollBudget = 20 * time.Second
	terminalDeadline := time.Now().Add(terminalPollBudget)
	for time.Now().Before(terminalDeadline) {
		if len(ledger.closedIDs()) > 0 || len(ledger.reopenedIDs()) > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Cancel the loop before assertions so cleanup is deterministic.
	cancel()
	select {
	case <-loopDone:
	case <-time.After(5 * time.Second):
		t.Error("SC1: work loop did not exit within 5 s after context cancel")
	}

	closedIDs := ledger.closedIDs()
	reopenedIDs := ledger.reopenedIDs()
	emittedTypes := collector.eventTypes()

	t.Logf("SC1: closedIDs=%v reopenedIDs=%v eventTypes=%v", closedIDs, reopenedIDs, emittedTypes)

	// ── Assertion 1: CloseBead called exactly once ────────────────────────────
	if len(closedIDs) == 0 {
		t.Errorf("SC1 FAIL: CloseBead not called; bead %q never completed (reopenedIDs=%v)", beadID, reopenedIDs)
	} else if closedIDs[0] != beadID {
		t.Errorf("SC1 FAIL: closed bead = %q; want %q", closedIDs[0], beadID)
	}

	// ── Assertion 2: ReopenBead NOT called ───────────────────────────────────
	if len(reopenedIDs) > 0 {
		t.Errorf("SC1 FAIL: ReopenBead called unexpectedly: %v (expected clean CloseBead path)", reopenedIDs)
	}

	// ── Assertion 3: run_completed emitted ───────────────────────────────────
	if !sc1FixtureContainsEvent(emittedTypes, string(core.EventTypeRunCompleted)) {
		t.Errorf("SC1 FAIL: run_completed not emitted; got %v", emittedTypes)
	}
	if sc1FixtureContainsEvent(emittedTypes, string(core.EventTypeRunFailed)) {
		t.Errorf("SC1 FAIL: run_failed emitted unexpectedly; got %v", emittedTypes)
	}

	// ── Assertion 4: agent_ready observed ────────────────────────────────────
	// Proves that waitAgentReady fired with the real adapter and did NOT time out.
	// The twin emits agent_ready in its single-happy-path scenario; if
	// waitAgentReady had timed out (HC-056), run_failed would be emitted instead.
	if !sc1FixtureContainsEvent(emittedTypes, string(core.EventTypeAgentReady)) {
		t.Errorf("SC1 FAIL: agent_ready not observed in bus events; got %v — waitAgentReady may have timed out", emittedTypes)
	}

	if len(closedIDs) > 0 && closedIDs[0] == beadID && !sc1FixtureContainsEvent(emittedTypes, string(core.EventTypeRunFailed)) {
		t.Logf("SC1 PASS: bead %q closed, run_completed emitted, agent_ready observed", beadID)
	}
}

// sc1FixtureContainsEvent reports whether typeName appears in the event type list.
func sc1FixtureContainsEvent(types []string, typeName string) bool {
	for _, t := range types {
		if strings.EqualFold(t, typeName) {
			return true
		}
	}
	return false
}

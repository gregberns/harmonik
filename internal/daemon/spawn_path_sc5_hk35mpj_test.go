package daemon_test

// spawn_path_sc5_hk35mpj_test.go — SC-5: twin emits handler_capabilities but
// NEVER agent_ready — watcher exits, bead reopened (hk-35mpj).
//
// # What this test covers
//
// SC-5 exercises the beadRunOne spawn path in the failure case where the twin
// emits handler_capabilities + agent_started but never emits agent_ready.  The
// daemon's waitAgentReady (HC-056) times out; the bead is reopened rather than
// closed, and run_failed is emitted.
//
// Setup mirrors SC-1 except:
//   - The twin wrapper invokes harmonik-twin-claude --scenario partial-pre-exec,
//     which emits handler_capabilities → agent_started only (no agent_ready).
//   - AgentReadyTimeout is set to 1 s so the test completes in a few seconds
//     rather than waiting the 30 s production default.
//   - The poll condition waits for ReopenBead to be called.
//
// The test uses AdapterRegistry2: NewSealedAdapterRegistryForTest(t) (real
// ClaudeCode adapter) so that waitAgentReady is NOT skipped — we're testing
// the full HC-056 timeout path, not a nil-guarded fast exit.
//
// # Expected outcome
//
// The twin exits cleanly after emitting its two messages; waitAgentReady times
// out because agent_ready never arrives within 1 s; the daemon calls ReopenBead
// (not CloseBead) and emits run_failed (not run_completed).
//
// Assertions:
//  1. ReopenBead is called exactly once for the seeded bead.
//  2. CloseBead is NOT called.
//  3. run_failed event is emitted.
//  4. run_completed is NOT emitted.
//  5. agent_ready is NOT in the bus events (confirms the timeout was the
//     cause, not a missed event).
//
// Helper prefix: sc5Fixture (bead hk-35mpj; per implementer-protocol.md
// §Helper-prefix discipline).
//
// Spec refs:
//   - specs/handler-contract.md §4.9 HC-056 (waitAgentReady timeout guard)
//   - specs/handler-contract.md §7.2 (handshake window)
//   - specs/scenario-harness.md §4 (twin-driven spawn-path coverage)
//
// Bead: hk-35mpj.

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

// sc5FixtureTwinWrapperScript writes a /bin/sh wrapper script that invokes the
// twin binary with --scenario partial-pre-exec, discarding all other args that
// buildClaudeLaunchSpec appends (e.g. --session-id, --print).
//
// partial-pre-exec emits handler_capabilities + agent_started only — no
// agent_ready — which is the precondition for the HC-056 timeout path.
func sc5FixtureTwinWrapperScript(t *testing.T, twinPath string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "twin-sc5-wrapper.sh")
	content := "#!/bin/sh\nexec " + twinPath + " --scenario partial-pre-exec\n"
	//nolint:gosec // G306: script is test-only; chmod 0755 required for execution
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("sc5FixtureTwinWrapperScript: WriteFile: %v", err)
	}
	return path
}

// sc5FixtureContainsEvent reports whether typeName appears in the event type list.
func sc5FixtureContainsEvent(types []string, typeName string) bool {
	for _, t := range types {
		if strings.EqualFold(t, typeName) {
			return true
		}
	}
	return false
}

// ─────────────────────────────────────────────────────────────────────────────
// TestSC5_SpawnPath_PartialPreExec_AgentReadyTimeout
// ─────────────────────────────────────────────────────────────────────────────

// TestSC5_SpawnPath_PartialPreExec_AgentReadyTimeout is SC-5 from the
// spawn-path scenario suite (hk-p3diy).
//
// It verifies that when the twin emits only the pre-exec preamble
// (handler_capabilities + agent_started, no agent_ready), the work loop:
//   - times out on waitAgentReady (HC-056),
//   - calls ReopenBead (not CloseBead), and
//   - emits run_failed (not run_completed).
//
// Not parallel: uses t.Setenv(HARMONIK_CLAUDE_CONFIG_PATH) to redirect
// EnsureWorktreeTrust away from ~/.claude.json; t.Setenv is incompatible
// with t.Parallel.
//
// Bead: hk-35mpj.
func TestSC5_SpawnPath_PartialPreExec_AgentReadyTimeout(t *testing.T) {
	// Locate the twin binary; skip when absent.
	twinPath, ok := scenariotest.TwinBinaryPath()
	if !ok {
		t.Skip("SC5: harmonik-twin-claude binary not found; set HARMONIK_TWIN_CLAUDE or build the binary")
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
	const beadID = core.BeadID("sc5-partial-pre-exec")
	ledger := &stubBeadLedger{
		ready: []core.BeadID{beadID},
	}
	collector := &stubEventCollector{}

	// Build the twin wrapper script (invokes partial-pre-exec scenario).
	twinWrapper := sc5FixtureTwinWrapperScript(t, twinPath)

	// Wire deps with AdapterRegistry2 = real ClaudeCode adapter (not empty-sealed).
	// This ensures waitAgentReady is active and exercises the HC-056 timeout path.
	// AgentReadyTimeout = 1 s: the twin exits without agent_ready so the timeout
	// fires after 1 s — short enough for a fast test, long enough to be stable.
	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:         ledger,
		Bus:               collector,
		ProjectDir:        projectDir,
		HandlerBinary:     twinWrapper,
		IntentLogDir:      filepath.Join(projectDir, ".harmonik", "beads-intents"),
		AdapterRegistry2:  NewSealedAdapterRegistryForTest(t),
		AgentReadyTimeout: 1 * time.Second,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	// Poll until ReopenBead or CloseBead is called (terminal bead state).
	const terminalPollBudget = 20 * time.Second
	terminalDeadline := time.Now().Add(terminalPollBudget)
	for time.Now().Before(terminalDeadline) {
		if len(ledger.reopenedIDs()) > 0 || len(ledger.closedIDs()) > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Cancel the loop before assertions so cleanup is deterministic.
	cancel()
	select {
	case <-loopDone:
	case <-time.After(5 * time.Second):
		t.Error("SC5: work loop did not exit within 5 s after context cancel")
	}

	closedIDs := ledger.closedIDs()
	reopenedIDs := ledger.reopenedIDs()
	emittedTypes := collector.eventTypes()

	t.Logf("SC5: closedIDs=%v reopenedIDs=%v eventTypes=%v", closedIDs, reopenedIDs, emittedTypes)

	// ── Assertion 1: ReopenBead called exactly once ───────────────────────────
	// The HC-056 timeout path calls ReopenBead (not CloseBead): the bead did not
	// complete successfully and must be retried.
	if len(reopenedIDs) == 0 {
		t.Errorf("SC5 FAIL: ReopenBead not called; bead %q was not reopened after agent_ready timeout (HC-056)", beadID)
	} else if reopenedIDs[0] != beadID {
		t.Errorf("SC5 FAIL: reopened bead = %q; want %q", reopenedIDs[0], beadID)
	}

	// ── Assertion 2: CloseBead NOT called ────────────────────────────────────
	if len(closedIDs) > 0 {
		t.Errorf("SC5 FAIL: CloseBead called unexpectedly: %v (HC-056 timeout must reopen, not close)", closedIDs)
	}

	// ── Assertion 3: run_failed emitted ──────────────────────────────────────
	if !sc5FixtureContainsEvent(emittedTypes, string(core.EventTypeRunFailed)) {
		t.Errorf("SC5 FAIL: run_failed not emitted; got %v (HC-056 timeout must produce run_failed)", emittedTypes)
	}

	// ── Assertion 4: run_completed NOT emitted ───────────────────────────────
	if sc5FixtureContainsEvent(emittedTypes, string(core.EventTypeRunCompleted)) {
		t.Errorf("SC5 FAIL: run_completed emitted unexpectedly; got %v (partial-pre-exec must not complete successfully)", emittedTypes)
	}

	// ── Assertion 5: agent_ready NOT in bus events ────────────────────────────
	// Confirms the timeout was the cause: the twin never emitted agent_ready.
	if sc5FixtureContainsEvent(emittedTypes, string(core.EventTypeAgentReady)) {
		t.Errorf("SC5 FAIL: agent_ready observed in bus events; got %v — partial-pre-exec scenario must NOT emit agent_ready (§7.2)", emittedTypes)
	}

	if len(reopenedIDs) > 0 && reopenedIDs[0] == beadID && !sc5FixtureContainsEvent(emittedTypes, string(core.EventTypeRunCompleted)) {
		t.Logf("SC5 PASS: bead %q reopened, run_failed emitted, agent_ready absent (HC-056 timeout path confirmed)", beadID)
	}
}

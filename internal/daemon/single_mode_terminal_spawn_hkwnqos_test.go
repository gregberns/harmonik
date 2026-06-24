package daemon_test

// single_mode_terminal_spawn_hkwnqos_test.go — regression test: single-mode
// implementer spawn must pass Terminal=true to Substrate.SpawnWindow (hk-wnqos).
//
// # The bug
//
// The spawn-cap terminal-slot reservation fix (hk-x882o) wired Terminal=true only
// for DOT consolidate-join nodes (dot_cascade.go). The single-mode dispatch path
// in workloop.go never set spec.Terminal, so single-mode runs (including all codex
// runs) used a non-terminal spawn and could be starved at launch when the
// non-terminal semaphore was fully saturated.
//
// # Fix (hk-wnqos)
//
// Set spec.Terminal = true in the single-mode dispatch block in workloop.go
// immediately after spec.Substrate = runSubstrate. This draws from the reserved
// +1 slot so a saturated non-terminal pool cannot block a single-mode launch.
//
// # What this test covers
//
// TestSingleModeImplementerIsTerminalSpawn_hkwnqos injects a spy substrate into
// the workloop deps and dispatches a single-mode bead. The spy captures
// SubstrateSpawn.Terminal from the handler.Launch → Substrate.SpawnWindow call.
//
//   - RED: before the fix, spec.Terminal is never set in the single-mode path;
//     SpawnWindow receives Terminal=false → test fails.
//   - GREEN: after the fix, spec.Terminal = true; SpawnWindow receives Terminal=true
//     → test passes.
//
// # Helper prefix
//
// Helpers use the prefix "wnqosFixture" (bead hk-wnqos; per implementer-protocol.md
// §Helper-prefix discipline).

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/handler"
)

// wnqosFixtureTerminalSpySubstrate captures SubstrateSpawn.Terminal from the
// first SpawnWindow call and always fails the spawn so beadRunOne returns
// quickly without requiring a real tmux session.
type wnqosFixtureTerminalSpySubstrate struct {
	captured chan bool
}

func (s *wnqosFixtureTerminalSpySubstrate) SpawnWindow(_ context.Context, in handler.SubstrateSpawn) (handler.SubstrateSession, error) {
	select {
	case s.captured <- in.Terminal:
	default:
	}
	return nil, fmt.Errorf("wnqos spy: intentional spawn failure to short-circuit beadRunOne")
}

var _ handler.Substrate = (*wnqosFixtureTerminalSpySubstrate)(nil)

// TestSingleModeImplementerIsTerminalSpawn_hkwnqos verifies that the single-mode
// implementer spawn passes Terminal=true to Substrate.SpawnWindow so it draws
// from the reserved +1 slot in the spawn semaphore (hk-wnqos).
func TestSingleModeImplementerIsTerminalSpawn_hkwnqos(t *testing.T) {
	t.Parallel()

	// implReadyFixtureProjectDir creates a minimal git repo used by
	// productionWorktreeFactory (real git worktree add). Defined in
	// reviewloop_impl_agent_ready_hkkunm4_test.go.
	projectDir := implReadyFixtureProjectDir(t)

	captured := make(chan bool, 1)
	spy := &wnqosFixtureTerminalSpySubstrate{captured: captured}

	// Cancel once the spy fires so ExportedRunWorkLoop exits promptly.
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:     &stubBeadLedger{ready: []core.BeadID{"hk-wnqos-test-001"}},
		Bus:           &stubEventCollector{},
		ProjectDir:    projectDir,
		HandlerBinary: "/bin/true",
		IntentLogDir:  filepath.Join(projectDir, ".harmonik", "beads-intents"),
		// WorkflowModeDefault zero value is normalised to WorkflowModeSingle by
		// ExportedWorkLoopDeps, so single-mode dispatch fires without a per-bead label.
		WorkflowModeDefault: core.WorkflowModeSingle,
		// AdapterRegistry2 must be non-nil (hk-d8u1y deleted the nil-guard).
		// Use the empty-sealed variant: spy.SpawnWindow always returns an error so
		// we never reach waitAgentReady and the registry is never consulted.
		AdapterRegistry2: NewEmptySealedAdapterRegistryForTest(t),
		Substrate:        spy,
		// ExportedMinimalLaunchSpecBuilder returns a valid LaunchSpec with Binary=/bin/true
		// so handler.Launch reaches Substrate.SpawnWindow without Claude infrastructure.
		LaunchSpecBuilder: daemon.ExportedMinimalLaunchSpecBuilder(),
	})

	done := make(chan error, 1)
	go func() {
		done <- daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	select {
	case terminal := <-captured:
		cancel() // stop the workloop now that we have what we need
		if !terminal {
			t.Error("single-mode implementer spawn: SpawnWindow received Terminal=false; " +
				"want Terminal=true — single-mode merge path must draw from the reserved +1 " +
				"slot so it cannot be starved by a saturated non-terminal pool (hk-wnqos)")
		}
	case <-ctx.Done():
		t.Fatal("spy substrate SpawnWindow was never called within timeout — " +
			"single-mode dispatch path did not reach Substrate.SpawnWindow")
	}

	<-done
}

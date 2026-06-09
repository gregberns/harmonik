//go:build scenario

package daemon_test

// scenario_concurrent_dispatch_vn4_hkukhzu_test.go — THE flagship concurrent-
// dispatch regression guard (validation-net VN4, bead hk-ukhzu).
//
// # What incident this guards
//
// hk-37giq was a concurrency-ONLY bug: a single per-run event-tap channel (tapCh)
// was consumed by two competitors — waitAgentReady's drain goroutine AND
// pasteInjectQuitOnCommit's launch/heartbeat watchdog. A Go channel receive is
// exclusive, so under 2+ concurrent runs the drainer stole every agent_heartbeat,
// the watchdog never observed firstHeartbeatSeen, and (while the pane reported an
// active child) the launch-suppression branch reset launchDeadline FOREVER — the
// run wedged at launch (launch_stall_detected → run_stale) and never advanced to
// merge. The fix (53ead2aa) made the tap a true fan-out so each consumer gets its
// own copy of every event. The bug hid ~2 weeks because NO scenario test
// exercised concurrent real-bead dispatch through the real heartbeat/launch/
// watchdog path — the fix shipped with only a narrow channel-level unit test.
//
// # What this test asserts (the DETERMINISTIC TERMINAL OUTCOME)
//
// TestScenario_ConcurrentDispatch_VN4_AllReachMerge boots the full daemon
// composition root at MaxConcurrent=N (N>=3), dispatches N distinct beads from a
// single wave queue, and asserts — via the reusable RunConcurrentMerge fixture
// (hk-944c2) — that on current main ALL N runs reach run_completed + merge +
// close, with NO terminal run_stale / launch_stall_detected wedge, and the
// concurrent-runs counter never exceeds the cap. The assertion is the terminal
// LIFECYCLE (event-ordered via AssertEventCausality), NOT a suppression-line
// count — the postmortem's environment-dependent "217×" figure is deliberately
// NOT the assertion (it is flaky under -race).
//
// # Altitude caveat (read before changing the substrate wiring)
//
// The hk-37giq tapCh competing-consumer race requires TWO consumers of the per-
// run tap. The SECOND consumer (the pasteInjectQuitOnCommit watchdog) only
// launches when runPasteTarget is a quitSender, i.e. when daemon.Config.Substrate
// is a *tmuxSubstrate (workloop.go:2079/2367). The standard exec / stdout-watcher
// path used here (nil Substrate) has only ONE tap consumer (waitAgentReady), so
// it CANNOT reproduce the wedge regardless of the fix — reverting 53ead2aa does
// NOT make THIS exec-path test fail. The exec-path test is therefore the broad
// concurrent-dispatch+merge regression guard (it would catch a regression that
// wedges or violates the cap on the exec path), while the dedicated keystone
// reproduction lives in the worktree experiment documented in the VN4 handoff:
// engaging a fake-adapter *tmuxSubstrate makes the watchdog the second tap
// consumer, but driving that substrate path to run_completed deterministically is
// blocked by (a) the nil watcher on the substrate path (completion flows via the
// hook-bridge socket, not stdout) and (b) HeartbeatInterval being a 300s const
// (the only tap heartbeat producer on the substrate path). See the worktree
// branch report for the empirical revert-demonstration result.
//
// Run by hand (the daemon commit-gate SKIPS //go:build scenario tests):
//
//	go test -tags=scenario -run TestScenario_ConcurrentDispatch_VN4 ./internal/daemon/ -race
//
// Bead: hk-ukhzu. Refs: hk-37giq, hk-944c2, hk-he18w, hk-3j50y.

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/daemon/scenariotest"
	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// vn4BootForTesting binds daemon.StartForTesting with the determinism options
// the RunConcurrentMerge fixture requires but cannot reference itself (the
// options live in package daemon's *_test.go files — see the fixture's
// import-boundary note). Each bead goroutine shares the one merge mutex so
// concurrent merges to the shared bare-repo origin serialise.
func vn4BootForTesting() func(ctx context.Context, cfg daemon.Config) <-chan error {
	var mergeMu sync.Mutex
	return func(ctx context.Context, cfg daemon.Config) <-chan error {
		done := make(chan error, 1)
		go func() {
			done <- daemon.StartForTesting(ctx, cfg,
				daemon.WithWorktreeFactory(emptyCommitWorktreeFactory),
				daemon.WithMergeMutex(&mergeMu),
			)
		}()
		return done
	}
}

// TestScenario_ConcurrentDispatch_VN4_AllReachMerge is the flagship regression
// guard: N>=3 distinct beads dispatched concurrently through the real spawn/
// heartbeat/merge path all reach run_completed + merge + close, the cap is
// honored, and there is no terminal run_stale / launch_stall wedge.
//
// It uses single-happy-path (which emits the full agent lifecycle but makes no
// commit; the empty-commit worktree factory provides the HEAD advance that
// satisfies the no-commit guard — the hkumemp determinism recipe). N>=3 of these
// running concurrently exercises real concurrent spawn timing through the work
// loop, merge mutex, and review loop.
//
// Bead: hk-ukhzu.
func TestScenario_ConcurrentDispatch_VN4_AllReachMerge(t *testing.T) {
	res := scenariotest.RunConcurrentMerge(t, scenariotest.ConcurrentMergeConfig{
		N:                 3,
		TwinScenario:      "single-happy-path",
		Boot:              vn4BootForTesting(),
		ExpectAllComplete: true,
		AgentReadyTimeout: 5 * time.Second,
		BeadPrefix:        "vn4",
	})

	// Belt-and-braces beyond the fixture's internal assertions: the cap and the
	// all-complete invariant are the load-bearing regression signal.
	if res.Completed < len(res.BeadIDs) {
		t.Errorf("VN4: only %d/%d runs completed (concurrent-dispatch wedge signature)",
			res.Completed, len(res.BeadIDs))
	}
	t.Logf("VN4 AllReachMerge PASS: N=%d completed=%d closed=%d maxConcurrent=%d stale=%d launchStall=%d",
		len(res.BeadIDs), res.Completed, res.ClosedBeads, res.MaxConcurrent, res.Stale, res.LaunchStall)
}

// ─────────────────────────────────────────────────────────────────────────────
// Keystone: watchdog-engaging substrate variant (the hk-37giq reproduction)
// ─────────────────────────────────────────────────────────────────────────────

// vn4PaneFixtureAdapter is a recording tmux.Adapter whose pane is "active" for a
// bounded window after each NewWindowIn, then reports the window gone. While the
// pane is active, WindowPanePID returns the test process PID — which has child
// processes (go test spawns subprocesses), so perRunSubstrate.PaneHasActiveProcess
// returns true. This keeps the pasteInjectQuitOnCommit launch-suppression branch
// (internal/daemon/pasteinject.go:679) active during the launch window, which is
// the exact condition the hk-37giq tapCh competing-consumer starve needed: with
// the pane "active" and no heartbeat reaching the watchdog (stolen by
// waitAgentReady's drainer on the pre-53ead2aa single-shared-channel tap), the
// launch deadline resets until launchSuppressionCeiling, then the watchdog kills
// the run → run_failed/run_stale. With the fan-out tap the watchdog observes the
// immediate startup heartbeat (RunHeartbeatLoop emits the first beat synchronously
// at launch), clears firstHeartbeatSeen, and the run advances.
//
// After paneAliveWindow elapses, WindowPanePID returns ErrNoSession so the
// substrate session's runWait poll loop unblocks and the run completes.
//
// All methods are safe for concurrent use.
type vn4PaneFixtureAdapter struct {
	mu sync.Mutex
	// paneCounter assigns sequential pane IDs.
	paneCounter int
	// spawnedAt records the wall time each window's pane was spawned, keyed by
	// pane ID, so WindowPanePID can report "gone" once paneAliveWindow elapses.
	spawnedAt map[string]time.Time
	// paneAliveWindow is how long a pane reports an active PID before reporting
	// gone. Tuned to comfortably exceed the (shrunk) launchSuppressionCeiling so
	// the reverted path has time to wedge-and-kill, while the fixed path advances.
	paneAliveWindow time.Duration
}

func newVN4PaneFixtureAdapter(aliveWindow time.Duration) *vn4PaneFixtureAdapter {
	return &vn4PaneFixtureAdapter{
		spawnedAt:       make(map[string]time.Time),
		paneAliveWindow: aliveWindow,
	}
}

func (a *vn4PaneFixtureAdapter) ProbeTmux(context.Context) error { return nil }
func (a *vn4PaneFixtureAdapter) ListSessions(context.Context) ([]string, error) {
	return nil, nil
}
func (a *vn4PaneFixtureAdapter) ListWindows(context.Context, string) ([]string, error) {
	return nil, nil
}

func (a *vn4PaneFixtureAdapter) NewWindowIn(_ context.Context, params tmux.NewWindowIn) tmux.Outcome {
	a.mu.Lock()
	a.paneCounter++
	paneID := fmt.Sprintf("%%%d", a.paneCounter)
	a.spawnedAt[paneID] = time.Now()
	a.mu.Unlock()
	return tmux.Outcome{
		Handle: tmux.WindowHandle(params.Session + ":" + params.WindowName),
		PaneID: paneID,
	}
}

func (a *vn4PaneFixtureAdapter) KillWindow(context.Context, tmux.WindowHandle) error { return nil }

// WindowPanePID returns the test-process PID while the pane is within its alive
// window, then ErrNoSession (pane gone). The handle may be the "%N" pane ID
// (from PaneHasActiveProcess / pidTarget) or the "session:window" handle (from
// runWait's slow path before pid is set); we treat any non-empty, in-window pane
// as alive by checking the most-recent spawn when the handle is not a pane ID.
func (a *vn4PaneFixtureAdapter) WindowPanePID(_ context.Context, handle tmux.WindowHandle) (int, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	h := string(handle)
	var spawned time.Time
	if t, ok := a.spawnedAt[h]; ok {
		spawned = t
	} else {
		// Not a recorded pane ID (e.g. a "session:window" handle). Use the most
		// recent spawn as a conservative liveness proxy.
		for _, t := range a.spawnedAt {
			if t.After(spawned) {
				spawned = t
			}
		}
	}
	if spawned.IsZero() || time.Since(spawned) > a.paneAliveWindow {
		return 0, tmux.ErrNoSession
	}
	// Pane alive: return the test process PID. It has child processes (go test
	// subprocesses), so hasAnyDirectChild → PaneHasActiveProcess returns true.
	return os.Getpid(), nil
}

func (a *vn4PaneFixtureAdapter) WindowPaneID(context.Context, tmux.WindowHandle) (string, error) {
	return "", nil // PaneID is set via NewWindowIn outcome
}
func (a *vn4PaneFixtureAdapter) KillSession(context.Context, string) error { return nil }
func (a *vn4PaneFixtureAdapter) LoadBuffer(context.Context, string, []byte) error {
	return nil
}
func (a *vn4PaneFixtureAdapter) PasteBuffer(context.Context, string, string) error { return nil }
func (a *vn4PaneFixtureAdapter) SendKeysLiteral(context.Context, string, string) error {
	return nil
}
func (a *vn4PaneFixtureAdapter) SendKeysEnter(context.Context, string) error { return nil }
func (a *vn4PaneFixtureAdapter) SendKeysQuit(context.Context, string) error  { return nil }
func (a *vn4PaneFixtureAdapter) WriteToPane(context.Context, string, string, []byte) error {
	return nil
}

var _ tmux.Adapter = (*vn4PaneFixtureAdapter)(nil)

// TestScenario_ConcurrentDispatch_VN4_WatchdogContention is the KEYSTONE
// reproduction: it engages a fake-adapter *tmuxSubstrate so the
// pasteInjectQuitOnCommit watchdog launches as the SECOND per-run-tap consumer,
// then dispatches N>=3 heartbeat-then-hold beads concurrently. This is the only
// path that exercises the hk-37giq competing-consumer race (the exec path has a
// single tap consumer and cannot reproduce the wedge — see the AllReachMerge
// docstring).
//
// On current main (fan-out tap, 53ead2aa) the watchdog gets its own copy of the
// immediate startup heartbeat, advances, and all N runs reach a terminal event
// without a launch wedge → this asserts all N reach run_completed and there is no
// terminal run_stale / launch_stall_detected.
//
// Reverting 53ead2aa (restore the single-shared-channel tap) is expected to make
// THIS test fail: waitAgentReady's drainer steals the heartbeat, the watchdog
// wedges in the launch-suppression branch until launchSuppressionCeiling, then
// kills the run (run_failed / launch_stall_detected). Run the keystone manually
// fixed-vs-reverted; the worktree branch report records both results and any
// timing caveats (the race window is environment-dependent — the assertion is the
// terminal lifecycle, never a suppression-line count).
//
// The launch/suppression timeouts are shrunk via the export seams so the test
// runs in seconds rather than the production 180s/12min.
//
// Bead: hk-ukhzu. Refs: hk-37giq.
//
// ── ALTITUDE BLOCKER (empirically established; do not re-enable without solving)
//
// This test is SKIPPED. Engaging a fake-adapter *tmuxSubstrate does make the
// pasteInjectQuitOnCommit watchdog the second per-run-tap consumer (so the race
// CAN occur), but the substrate path cannot be driven to a terminal run state by
// a fake adapter alone, because on the substrate path:
//
//   - The substrate session's Stdout() is nil (handler.launchViaSubstrate), so
//     there is NO stdout watcher. agent_ready and the run outcome arrive over the
//     hook-bridge UNIX socket (HookSessionStore.SetAgentReadyCallback /
//     WaitForOutcome), NOT the twin's stdout. A fake adapter does not run the
//     socket relay, so waitAgentReady ALWAYS times out (agent_ready_timeout,
//     HC-056) and the run is killed BEFORE the watchdog/launch phase — i.e. the
//     wedge condition is never reached. (Verified: the run fails with
//     "agent_ready timeout: no agent_ready event within deadline" in ~4s.)
//   - Returning a real live PID (e.g. os.Getpid()) from the fake adapter's
//     WindowPanePID to satisfy PaneHasActiveProcess is unsafe — the daemon's kill
//     paths may signal that PID.
//
// Driving this to completion requires the full hook-bridge socket wiring (a real
// relay or a socket stub feeding agent_ready + outcome) AND a tmux-or-equivalent
// substrate that reports a pane-active child it is safe to kill. That is real
// tmux + real socket altitude, which (a) the validation-net brief forbids
// touching on the shared box and (b) is far heavier than a regression-guard
// test. The shipped channel-level fan-out unit test
// (workloopeventsource_hk37giq_test.go) IS the deterministic keystone for the
// tap mechanism (it FAILS on the reverted single-shared-channel design, PASSES on
// the fan-out); the exec-path TestScenario_ConcurrentDispatch_VN4_AllReachMerge
// above is the end-to-end concurrent-dispatch+merge guard. This skipped test
// documents the path and is the scaffold to finish once a socket stub lands
// (follow-up: a hook-bridge socket fake for substrate-path scenario tests).
func TestScenario_ConcurrentDispatch_VN4_WatchdogContention(t *testing.T) {
	t.Skip("VN4 keystone: substrate-path run cannot reach a terminal state without " +
		"hook-bridge socket wiring (agent_ready/outcome arrive over the socket, not " +
		"stdout); see the test docstring's ALTITUDE BLOCKER. The channel-level fan-out " +
		"unit test (workloopeventsource_hk37giq_test.go) is the deterministic tap-mechanism " +
		"keystone; AllReachMerge is the end-to-end concurrent-merge guard.")

	// Shrink the watchdog timing so a wedge (reverted) resolves in seconds and a
	// healthy run (fixed) is not falsely guillotined. Restore on cleanup.
	restore := vn4ShrinkWatchdogTimers(t,
		2*time.Second,  // launchHeartbeatTimeout
		6*time.Second,  // launchSuppressionCeiling (the reverted wedge kills here)
		1*time.Second,  // noChangeKillDelay
		2*time.Second,  // postQuitKillGrace
		20*time.Second, // commitPollTimeout
	)
	defer restore()

	// Pane stays "active" comfortably past launchSuppressionCeiling so the
	// reverted path takes the suppress-then-kill branch, then dies so sess.Wait
	// returns.
	fakeAdapter := newVN4PaneFixtureAdapter(10 * time.Second)
	substrate := daemon.NewTmuxSubstrate(fakeAdapter, "vn4-keystone-session")

	res := scenariotest.RunConcurrentMerge(t, scenariotest.ConcurrentMergeConfig{
		N:                 3,
		TwinScenario:      "heartbeat-then-hold",
		Boot:              vn4BootForTesting(),
		Substrate:         substrate,
		ExpectAllComplete: true,
		AgentReadyTimeout: 3 * time.Second,
		TerminalBudget:    90 * time.Second,
		BeadPrefix:        "vn4k",
	})

	t.Logf("VN4 WatchdogContention: N=%d completed=%d failed=%d closed=%d maxConcurrent=%d stale=%d launchStall=%d",
		len(res.BeadIDs), res.Completed, res.Failed, res.ClosedBeads, res.MaxConcurrent, res.Stale, res.LaunchStall)
}

// vn4ShrinkWatchdogTimers sets the package-level watchdog timing vars (via the
// export seams) to the supplied short durations and returns a restore func.
func vn4ShrinkWatchdogTimers(t *testing.T, launchHB, launchSuppress, noChangeKill, postQuit, commitPoll time.Duration) func() {
	t.Helper()
	origLaunchHB := *daemon.ExportedLaunchHeartbeatTimeout
	origLaunchSuppress := *daemon.ExportedLaunchSuppressionCeiling
	origNoChangeKill := *daemon.ExportedNoChangeKillDelay
	origPostQuit := *daemon.ExportedPostQuitKillGrace
	origCommitPoll := *daemon.ExportedCommitPollTimeout

	*daemon.ExportedLaunchHeartbeatTimeout = launchHB
	*daemon.ExportedLaunchSuppressionCeiling = launchSuppress
	*daemon.ExportedNoChangeKillDelay = noChangeKill
	*daemon.ExportedPostQuitKillGrace = postQuit
	*daemon.ExportedCommitPollTimeout = commitPoll

	return func() {
		*daemon.ExportedLaunchHeartbeatTimeout = origLaunchHB
		*daemon.ExportedLaunchSuppressionCeiling = origLaunchSuppress
		*daemon.ExportedNoChangeKillDelay = origNoChangeKill
		*daemon.ExportedPostQuitKillGrace = origPostQuit
		*daemon.ExportedCommitPollTimeout = origCommitPoll
	}
}

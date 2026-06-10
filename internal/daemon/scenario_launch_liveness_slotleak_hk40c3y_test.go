//go:build scenario

package daemon_test

// scenario_launch_liveness_slotleak_hk40c3y_test.go — concurrent launch-liveness
// + spawn-semaphore no-leak scenario (validation-net, bead hk-40c3y).
//
// # What incident this guards
//
// The launch-stall / no-spawn wedge family (hk-4l7zs, hk-tcenh, hk-goczd): under
// concurrent N-wide dispatch, a SpawnWindow semaphore slot was acquired but never
// released (a leaked slot — release happens only inside the session's Kill via
// killOnce.Do). Once the pool saturated with leaked slots, the next launch's
// SpawnWindow blocked at launch_initiated with no tmux window until the 30-min
// implementer budget expired and the run failed no_commit. The fix (hk-4l7zs)
// bounds the acquire (defaultSpawnAcquireTimeout) so a leaked-slot wedge surfaces
// as a prompt ErrSpawnCapTimeout launch failure, and tightened the release path so
// EVERY terminal run returns its slot. The newWindowMu serialise (hk-oihnf /
// hk-goczd) is held only for the bounded new-window call so it cannot wedge.
//
// Before this test the slot-leak / bounded-acquire mechanism had ONLY unit
// coverage (tmuxsubstrate_slotleak_hk4l7zs_test.go drives SpawnWindow/Kill
// directly). The bead asks for the LIVE mechanism — the spawn semaphore engaged
// through a BOOTED daemon under real concurrent wave dispatch.
//
// # What this test asserts (the DETERMINISTIC properties)
//
// TestScenario_LaunchLiveness_SlotNoLeak_HK40C3Y boots the full daemon
// composition root at MaxConcurrent=N (N>=3) with a fake-adapter-backed
// *tmuxSubstrate carrying a spawn cap of N, then dispatches N distinct beads from
// a single wave queue and asserts:
//
//   (a) LAUNCH-LIVENESS — every dispatched bead actually launches: N run_started
//       events appear (the run advanced past launch_initiated into a live run),
//       AND no terminal launch_stall_detected / run_stale wedge fires. A leaked
//       slot saturating the pool would block a later SpawnWindow at
//       launch_initiated and the run would never reach run_started within budget
//       (or would surface as launch_stall_detected).
//
//   (b) SPAWN-SEMAPHORE NO-LEAK — after the wave drains (all N runs reach a
//       terminal event) and the daemon is cancelled, the spawn semaphore returns
//       to baseline: SpawnSlotsInUse()==0 against a cap of N. Every run's
//       teardown (forceTeardownSession → tmuxSubstrateSession.Kill →
//       releaseSpawnSlot, inside killOnce.Do) must have returned its slot, so a
//       subsequent dispatch is NOT starved. A single leaked slot leaves
//       SpawnSlotsInUse() > 0 here — the regression signal.
//
//   (c) CAP HONORED — the in-flight run counter never exceeds MaxConcurrent=N
//       (asserted by the RunConcurrentMerge fixture).
//
// # Altitude note — why the runs reach run_FAILED, not run_completed
//
// This test engages a fake-adapter *tmuxSubstrate to drive the REAL spawn
// semaphore (the exec/nil-substrate path never touches spawnSem, so it cannot
// exercise the slot-leak mechanism at all). On the substrate path the spawned
// session's Stdout() is nil (handler.launchViaSubstrate), so agent_ready and the
// run outcome would arrive over the hook-bridge UNIX socket, which a fake adapter
// does not run. Each run therefore times out at agent_ready (HC-056) in
// AgentReadyTimeout and the daemon kills + reopens it (run_failed). That is FINE
// for this bead: the no-leak property is about whether the slot returns on
// TEARDOWN, and the agent_ready_timeout path runs `sess.Kill(ctx)` +
// forceTeardownSession exactly like the happy path — so it exercises the release
// just as well, deterministically and in seconds. The fake adapter returns pid=0
// from WindowPanePID (same as the hk-4l7zs unit fixture) so the daemon's kill path
// NEVER signals a real PID — the test process is never at risk.
//
// This is the deliberate division of labour with the VN4 keystone
// (scenario_concurrent_dispatch_vn4_hkukhzu_test.go): VN4 guards the exec-path
// concurrent-dispatch+merge lifecycle (the tapCh race terminal outcome); THIS test
// guards the substrate-resident spawn-semaphore + launch-liveness invariants that
// the exec path structurally cannot reach.
//
// Run by hand (the daemon commit-gate SKIPS //go:build scenario tests):
//
//	go test -tags=scenario -run TestScenario_LaunchLiveness_SlotNoLeak_HK40C3Y ./internal/daemon/ -race
//
// Bead: hk-40c3y. Refs: hk-4l7zs, hk-tcenh, hk-goczd, hk-ukhzu, hk-944c2.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/daemon/scenariotest"
	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// ─────────────────────────────────────────────────────────────────────────────
// llslFixtureAdapter — concurrency-safe fake tmux adapter (helper prefix "llsl")
// ─────────────────────────────────────────────────────────────────────────────

// llslFixtureAdapter is a concurrency-safe fake tmux.Adapter for the
// launch-liveness / slot-leak scenario. NewWindowIn always succeeds with a
// unique slash-free pane ID so SpawnWindow proceeds (and acquires a spawn-
// semaphore slot).
//
// WindowPanePID returns pid=0 (no live process) for a bounded window after each
// NewWindowIn, then ErrNoSession (pane gone). Returning pid=0 — never a real PID
// — keeps the daemon's kill path from ever signalling a live process (the test
// process is never at risk; same safety posture as the hk-4l7zs
// slotLeakFixtureAdapter). The "pane gone after paneAliveWindow" behaviour is
// load-bearing for determinism: on the substrate path with s.pid==0 the
// session's runWait slow-path polls WindowPanePID and only returns once it sees
// an error (ErrNoSession). Reporting the pane gone after a short window lets
// runWait — and therefore the agent_ready_timeout teardown's blocking
// sess.Wait(ctx) — unblock promptly so the run reaches run_failed (and releases
// its slot) in seconds, instead of polling until the daemon ctx is cancelled.
//
// KillWindow is a no-op success, which (combined with the substrate session's
// killOnce) drives releaseSpawnSlot on teardown.
//
// All methods are safe for concurrent use under MaxConcurrent>1 wave dispatch.
type llslFixtureAdapter struct {
	mu          sync.Mutex
	paneCounter int
	// spawnedAt records when each pane's window was created, keyed by pane ID,
	// so WindowPanePID can report "gone" once paneAliveWindow elapses.
	spawnedAt map[string]time.Time
	// paneAliveWindow is how long a pane reports alive (pid=0, no error) before
	// reporting gone (ErrNoSession). Tuned to comfortably cover run_started +
	// launch_initiated + the short agent_ready wait, then let runWait unblock.
	paneAliveWindow time.Duration
}

func newLLSLFixtureAdapter(aliveWindow time.Duration) *llslFixtureAdapter {
	return &llslFixtureAdapter{
		spawnedAt:       make(map[string]time.Time),
		paneAliveWindow: aliveWindow,
	}
}

func (a *llslFixtureAdapter) ProbeTmux(_ context.Context) error { return nil }
func (a *llslFixtureAdapter) ListSessions(_ context.Context) ([]string, error) {
	return nil, nil
}
func (a *llslFixtureAdapter) ListWindows(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}

func (a *llslFixtureAdapter) NewWindowIn(_ context.Context, params tmux.NewWindowIn) tmux.Outcome {
	a.mu.Lock()
	a.paneCounter++
	paneID := fmt.Sprintf("%%%d", a.paneCounter) // slash-free "%N" pane ID
	a.spawnedAt[paneID] = time.Now()
	a.mu.Unlock()
	return tmux.Outcome{
		Handle: tmux.WindowHandle(params.Session + ":" + params.WindowName),
		PaneID: paneID,
	}
}

func (a *llslFixtureAdapter) KillWindow(_ context.Context, _ tmux.WindowHandle) error { return nil }

// WindowPanePID returns (pid=0, nil) while the pane is within its alive window,
// then (0, ErrNoSession) once paneAliveWindow elapses (pane gone). pid is ALWAYS
// 0 — never a real PID — so the kill path's killProcessWithGrace (which only
// signals when pid>0) can never SIGTERM a live process; the test process is never
// at risk. Returning os.Getpid() (as the VN4 pane fixture does for a different
// purpose) would be UNSAFE here. The "gone after window" behaviour lets the
// substrate session's runWait slow-path (s.pid==0) observe the error and unblock
// sess.Wait so the run reaches a terminal event promptly. The handle may be the
// "%N" pane ID (from runWait's panePIDTarget) or the "session:window" handle; we
// treat any in-window pane as alive, using the most-recent spawn when the handle
// is not a recorded pane ID.
func (a *llslFixtureAdapter) WindowPanePID(_ context.Context, handle tmux.WindowHandle) (int, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	h := string(handle)
	var spawned time.Time
	if ts, ok := a.spawnedAt[h]; ok {
		spawned = ts
	} else {
		for _, ts := range a.spawnedAt {
			if ts.After(spawned) {
				spawned = ts
			}
		}
	}
	if spawned.IsZero() || time.Since(spawned) > a.paneAliveWindow {
		return 0, tmux.ErrNoSession
	}
	return 0, nil // pane alive, pid unavailable (never a real PID)
}

func (a *llslFixtureAdapter) WindowPaneID(_ context.Context, _ tmux.WindowHandle) (string, error) {
	return "", nil // PaneID is set via NewWindowIn outcome
}
func (a *llslFixtureAdapter) KillSession(_ context.Context, _ string) error { return nil }
func (a *llslFixtureAdapter) LoadBuffer(_ context.Context, _ string, _ []byte) error {
	return nil
}
func (a *llslFixtureAdapter) PasteBuffer(_ context.Context, _, _ string) error     { return nil }
func (a *llslFixtureAdapter) SendKeysLiteral(_ context.Context, _, _ string) error { return nil }
func (a *llslFixtureAdapter) SendKeysEnter(_ context.Context, _ string) error      { return nil }
func (a *llslFixtureAdapter) SendKeysQuit(_ context.Context, _ string) error       { return nil }
func (a *llslFixtureAdapter) WriteToPane(_ context.Context, _, _ string, _ []byte) error {
	return nil
}

var _ tmux.Adapter = (*llslFixtureAdapter)(nil)

// llslEventCount returns the number of JSONL events matching eventType. It is a
// local copy (the scenariotest fixture's rcmEventCount is unexported) so the
// caller-side launch-liveness assertions can read run_started / launch_stall /
// run_stale counts directly.
func llslEventCount(t *testing.T, jsonlPath, eventType string) int {
	t.Helper()
	//nolint:gosec // G304: path is t.TempDir()-based; not user input
	f, err := os.Open(jsonlPath)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatalf("llslEventCount: open %s: %v", jsonlPath, err)
	}
	defer func() {
		if cErr := f.Close(); cErr != nil {
			t.Logf("llslEventCount: close: %v", cErr)
		}
	}()
	var count int
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var env struct {
			Type string `json:"type"`
		}
		if json.Unmarshal([]byte(line), &env) != nil {
			continue
		}
		if env.Type == eventType {
			count++
		}
	}
	return count
}

// TestScenario_LaunchLiveness_SlotNoLeak_HK40C3Y is the live-mechanism regression
// guard for concurrent launch-liveness + spawn-semaphore no-leak.
//
// It boots the full daemon at MaxConcurrent=N with a fake-adapter *tmuxSubstrate
// carrying a spawn cap of N, dispatches N distinct beads from one wave queue, and
// after the wave drains asserts: (a) all N launched (N run_started, no
// launch_stall_detected / run_stale), (b) the spawn semaphore returned to
// baseline (SpawnSlotsInUse()==0 against cap N — no leaked slot), (c) the
// concurrent-run cap was honored. See the file docstring for why the runs reach
// run_failed (agent_ready_timeout on the fake-substrate path) and why that still
// exercises the slot-release / launch-liveness mechanism deterministically.
//
// Not parallel: RunConcurrentMerge sets/unsets HARMONIK_CLAUDE_CONFIG_PATH.
//
// Bead: hk-40c3y.
func TestScenario_LaunchLiveness_SlotNoLeak_HK40C3Y(t *testing.T) {
	const n = 3

	// Engage the REAL spawn semaphore via a fake-adapter *tmuxSubstrate. The cap
	// is set to N so the whole wave can launch concurrently; the no-leak assertion
	// then checks the pool returns to 0/N after teardown.
	//
	// A short spawn-acquire timeout keeps the test prompt: if a slot WERE leaked
	// and a later SpawnWindow saturated the pool, the acquire would fail fast
	// (ErrSpawnCapTimeout) rather than block toward the 2-minute default — the
	// failing-fast behaviour is itself the hk-4l7zs fix. With N slots for N runs no
	// acquire should ever block on a healthy build.
	// paneAliveWindow comfortably covers run_started + launch_initiated + the 2s
	// agent_ready wait, then reports the pane gone so the substrate session's
	// runWait unblocks (s.pid==0 slow path) and the agent_ready_timeout teardown's
	// sess.Wait returns promptly → run reaches run_failed in seconds.
	adapter := newLLSLFixtureAdapter(4 * time.Second)
	const sessionName = "hk40c3y-llsl-isolated-session" // unique; never a live session
	substrate := daemon.NewTmuxSubstrate(adapter, sessionName,
		daemon.WithSpawnCap(n),
		daemon.WithSpawnAcquireTimeout(10*time.Second),
	)

	// Pre-condition: the pool starts empty against a cap of N.
	if got := daemon.ExportedSpawnSlotsInUse(substrate); got != 0 {
		t.Fatalf("precondition: SpawnSlotsInUse()=%d, want 0 before any dispatch", got)
	}

	res := scenariotest.RunConcurrentMerge(t, scenariotest.ConcurrentMergeConfig{
		N:            n,
		TwinScenario: "single-happy-path",
		Boot:         vn4BootForTesting(), // binds StartForTesting + determinism options
		Substrate:    substrate,
		// ExpectAllComplete=false: the fake-substrate path cannot reach
		// run_completed (no hook-bridge socket relay → agent_ready_timeout). The
		// fixture then asserts only that all N reach SOME terminal event + the cap
		// is honored — exactly the drain condition the no-leak assertion needs.
		ExpectAllComplete: false,
		AgentReadyTimeout: 2 * time.Second, // short → each run fails fast on the fake path
		TerminalBudget:    90 * time.Second,
		BeadPrefix:        "llsl",
	})

	// ── Assertion (a): LAUNCH-LIVENESS ────────────────────────────────────────
	//
	// Every dispatched bead must have launched (advanced past launch_initiated
	// into a live run → run_started), and no run may wedge at launch.
	nStarted := llslEventCount(t, res.JSONLPath, string(core.EventTypeRunStarted))
	if nStarted < n {
		t.Errorf("(a) launch-liveness: %d/%d run_started events; want all N "+
			"(a shortfall is the launch-stall/no-spawn wedge signature — a run stuck "+
			"at launch_initiated never reaches run_started)", nStarted, n)
	}
	if res.LaunchStall > 0 {
		t.Errorf("(a) launch-liveness: %d launch_stall_detected event(s); want 0 "+
			"(launch_stall_detected is the spawn-wedge terminal signature, hk-4l7zs)", res.LaunchStall)
	}
	if res.Stale > 0 {
		t.Errorf("(a) launch-liveness: %d run_stale event(s); want 0 "+
			"(run_stale is the launch-wedge terminal signature)", res.Stale)
	}

	// ── Assertion (b): SPAWN-SEMAPHORE NO-LEAK ─────────────────────────────────
	//
	// After the wave drains and the daemon is cancelled (both done inside
	// RunConcurrentMerge before it returns), every run's teardown must have
	// released its slot. The pool must be back to baseline: 0 in use against a
	// cap of N. A leaked slot (acquired-but-never-released, the hk-4l7zs bug)
	// leaves SpawnSlotsInUse() > 0 here.
	if got := daemon.ExportedSpawnSlotsInUse(substrate); got != 0 {
		t.Errorf("(b) spawn-semaphore LEAK: SpawnSlotsInUse()=%d after the wave drained; "+
			"want 0 — a slot was acquired by SpawnWindow but never returned by Kill "+
			"(hk-4l7zs slot leak). Subsequent dispatch would be starved.", got)
	}

	// ── Assertion (c): cap honored ────────────────────────────────────────────
	//
	// RunConcurrentMerge already asserts MaxConcurrent <= N internally; restate it
	// here as the load-bearing concurrency invariant.
	if res.MaxConcurrent > n {
		t.Errorf("(c) cap violated: max concurrent runs = %d, want <= %d", res.MaxConcurrent, n)
	}

	t.Logf("hk-40c3y PASS: N=%d run_started=%d (launched all), launchStall=%d stale=%d "+
		"maxConcurrent=%d slotsInUseAfterDrain=%d (cap=%d) — launch-liveness OK, no slot leak",
		n, nStarted, res.LaunchStall, res.Stale, res.MaxConcurrent,
		daemon.ExportedSpawnSlotsInUse(substrate), n)
}

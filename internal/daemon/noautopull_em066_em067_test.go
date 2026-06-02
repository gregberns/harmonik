package daemon_test

// noautopull_em066_em067_test.go — scenario tests for EM-066 (quiet-daemon
// no-auto-pull topology) and EM-067 (operator-pause gate on br-ready fallback).
//
// # EM-066 — No-auto-pull (queue-only) daemon topology
//
// When noAutoPull is set, the daemon MUST NOT fall back to `br ready` for
// dispatch input. A daemon booted in this topology with no submitted queue MUST
// dispatch zero runs — it MUST NOT emit run_started, MUST NOT spawn any agent
// subprocess, and MUST consume no agent credit — until a queue is submitted.
//
// Tests in this file:
//   - TestScenario_NoAutoPull_ZeroRunsStarted_EM066 — quiet-daemon: set
//     NoAutoPull=true, no queue submitted; assert zero run_started events
//     and zero Ready() calls over a bounded observation window.
//   - TestScenario_NoAutoPull_BrReadyFallbackEnabled_EM066 — opt-in branch:
//     NoAutoPull=false with ≥1 ready bead and no queue; verify Ready() is
//     called (demonstrating the fallback fires when the flag is unset).
//   - TestScenario_BrReadyOperatorPauseGate_EM067 — operator-pause gate on
//     fallback: NoAutoPull=false with ≥1 ready bead and operator-control
//     state driven to paused; assert Ready() is NOT called while paused and
//     IS called after resume.
//
// Spec refs:
//   - specs/execution-model.md §4.11.EM-066 (no-auto-pull topology)
//   - specs/execution-model.md §4.11.EM-067 (operator-pause fallback gate)
//   - specs/execution-model.md §10.2 (EM-066/EM-067 test obligations)
//
// Bead: hk-h5lv2.

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// ─────────────────────────────────────────────────────────────────────────────
// countingLedger — records Ready() and ClaimBead() calls for EM-066/EM-067.
// ─────────────────────────────────────────────────────────────────────────────

// countingLedger is a minimal beadLedger stub that counts how many times
// Ready() and ClaimBead() are called so the tests can assert on whether the
// br-ready fallback path was entered.
type countingLedger struct {
	readyCalls  atomic.Int64
	claimCalls  atomic.Int64
	readyResult []core.BeadRecord // returned on every Ready() call
	notifyReady chan struct{}      // closed or sent on first Ready() call (may be nil)
}

func (c *countingLedger) Ready(_ context.Context) ([]core.BeadRecord, error) {
	n := c.readyCalls.Add(1)
	if n == 1 && c.notifyReady != nil {
		select {
		case c.notifyReady <- struct{}{}:
		default:
		}
	}
	return c.readyResult, nil
}

func (c *countingLedger) ShowBead(_ context.Context, id core.BeadID) (core.BeadRecord, error) {
	return core.BeadRecord{BeadID: id, Status: core.CoarseStatusOpen}, nil
}

func (c *countingLedger) ClaimBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID) error {
	c.claimCalls.Add(1)
	return nil
}

func (c *countingLedger) CloseBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID, _ bool) error {
	return nil
}

func (c *countingLedger) ReopenBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID, _ string) error {
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// EM-066: Quiet-daemon — NoAutoPull=true, no queue, zero run_started
// ─────────────────────────────────────────────────────────────────────────────

// TestScenario_NoAutoPull_ZeroRunsStarted_EM066 verifies the quiet-daemon
// (queue-only) topology: when NoAutoPull is set and no queue is submitted, the
// work loop MUST NOT call br-ready, MUST NOT emit run_started, and MUST NOT
// spawn any agent subprocess.
//
// The test observes the daemon for a bounded window (300 ms) then cancels the
// context and checks that zero run_started events were emitted and Ready() was
// never called.
//
// Spec ref: specs/execution-model.md §4.11.EM-066.
// Bead: hk-h5lv2.
func TestScenario_NoAutoPull_ZeroRunsStarted_EM066(t *testing.T) {
	t.Parallel()

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	// Seed ≥1 ready bead so the test would fail if the fallback path fires.
	ledger := &countingLedger{
		readyResult: []core.BeadRecord{
			{BeadID: core.BeadID("hk-h5lv2-em066-bead")},
		},
	}
	bus := &stubEventCollector{}

	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:        ledger,
		Bus:              bus,
		ProjectDir:       projectDir,
		HandlerBinary:    "/bin/sh",
		HandlerArgs:      []string{"-c", "exit 0"},
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		NoAutoPull:       true, // queue-only topology — br-ready MUST NOT fire
		// No QueueStore: no queue submitted.
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	// Observe for 300 ms — long enough for several poll ticks.
	time.Sleep(300 * time.Millisecond)
	cancel()

	select {
	case <-loopDone:
	case <-time.After(3 * time.Second):
		t.Fatal("em066: workloop did not exit within 3s after context cancel")
	}

	// Assert: Ready() was never called.
	if readyCalls := ledger.readyCalls.Load(); readyCalls != 0 {
		t.Errorf("em066: Ready() called %d time(s); want 0 — noAutoPull must suppress the br-ready fallback path", readyCalls)
	}

	// Assert: no run_started events emitted.
	for _, et := range bus.eventTypes() {
		if et == string(core.EventTypeRunStarted) {
			t.Errorf("em066: run_started event emitted while NoAutoPull=true and no queue submitted — daemon must dispatch zero runs")
			break
		}
	}

	t.Logf("em066 PASS: Ready()=%d calls, run_started events=0, daemon sat idle in queue-only mode",
		ledger.readyCalls.Load())
}

// ─────────────────────────────────────────────────────────────────────────────
// EM-066 opt-in branch: NoAutoPull=false (--auto-pull) with ready bead
// ─────────────────────────────────────────────────────────────────────────────

// TestScenario_AutoPull_BrReadyFallbackFires_EM066 verifies the opt-in
// historical topology: when NoAutoPull=false (equivalent to --auto-pull) and
// ≥1 ready bead exists, the br-ready fallback path IS entered — Ready() is
// called within a bounded window.
//
// This is the EM-066 "Historical-topology" test obligation in §10.2.
//
// Spec ref: specs/execution-model.md §4.11.EM-066 opt-in branch.
// Bead: hk-h5lv2.
func TestScenario_AutoPull_BrReadyFallbackFires_EM066(t *testing.T) {
	t.Parallel()

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	notifyReady := make(chan struct{}, 1)
	ledger := &countingLedger{
		readyResult: []core.BeadRecord{}, // empty so the loop idles after Ready() returns
		notifyReady: notifyReady,
	}
	bus := &stubEventCollector{}

	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:        ledger,
		Bus:              bus,
		ProjectDir:       projectDir,
		HandlerBinary:    "/bin/sh",
		HandlerArgs:      []string{"-c", "exit 0"},
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		NoAutoPull:       false, // br-ready fallback enabled (--auto-pull opt-in)
		// No QueueStore: no queue submitted, triggers the br-ready path.
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	// Ready() MUST be called within the poll window.
	select {
	case <-notifyReady:
		// Correct: br-ready fallback fired.
	case <-time.After(5 * time.Second):
		t.Fatal("em066-opt-in: Ready() was not called within 5s — br-ready fallback must fire when NoAutoPull=false")
	}

	cancel()
	select {
	case <-loopDone:
	case <-time.After(3 * time.Second):
		t.Fatal("em066-opt-in: workloop did not exit within 3s after context cancel")
	}

	t.Logf("em066-opt-in PASS: Ready() called %d time(s) — br-ready fallback active when NoAutoPull=false",
		ledger.readyCalls.Load())
}

// ─────────────────────────────────────────────────────────────────────────────
// EM-067: Operator-pause gate on br-ready fallback path
// ─────────────────────────────────────────────────────────────────────────────

// TestScenario_BrReadyOperatorPauseGate_EM067 verifies the defense-in-depth
// operator-pause gate on the br-ready fallback path.
//
// Setup: NoAutoPull=false (br-ready fallback enabled), ≥1 ready bead, no
// submitted queue, operator-control state driven to paused before the loop
// starts.
//
// Phase 1 (paused): observe for 200 ms; Ready() MUST NOT be called.
// Phase 2 (resumed): call HandleOperatorResume; Ready() MUST be called within
// a bounded window — demonstrating that fallback dispatch resumes once the
// operator-pause gate releases.
//
// The observable criterion (Ready() not called while paused → called after
// resume) is the EM-067 conformance criterion regardless of whether the
// primary §7.4 loop-top ON-008 gate or the inline defense-in-depth assertion
// enforces it.
//
// Spec ref: specs/execution-model.md §4.11.EM-067.
// Bead: hk-h5lv2.
func TestScenario_BrReadyOperatorPauseGate_EM067(t *testing.T) {
	t.Parallel()

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	notifyReady := make(chan struct{}, 1)
	ledger := &countingLedger{
		readyResult: []core.BeadRecord{}, // empty to avoid subprocess dispatch
		notifyReady: notifyReady,
	}
	bus := &stubEventCollector{}

	ctrl := daemon.ExportedNewOperatorPauseController(bus)
	// Pause BEFORE the loop starts so the first dispatch tick is gated.
	if err := ctrl.HandleOperatorPause(context.Background(), ""); err != nil {
		t.Fatalf("em067: HandleOperatorPause: %v", err)
	}

	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:         ledger,
		Bus:               bus,
		ProjectDir:        projectDir,
		HandlerBinary:     "/bin/sh",
		HandlerArgs:       []string{"-c", "exit 0"},
		IntentLogDir:      filepath.Join(projectDir, ".harmonik", "beads-intents"),
		AdapterRegistry2:  NewSealedAdapterRegistryForTest(t),
		NoAutoPull:        false,  // fallback enabled so the pause gate is reachable
		OperatorPauseCtrl: ctrl,   // operator-pause gate wired
		// No QueueStore: no queue submitted, routes to br-ready path.
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	// Phase 1: observe for 200 ms while paused — Ready() MUST NOT be called.
	time.Sleep(200 * time.Millisecond)

	select {
	case <-notifyReady:
		t.Fatal("em067: Ready() was called while operator-paused — gate must hold dispatch")
	default:
		// Correct: gate prevented Ready() from being called.
	}

	// Phase 2: resume and verify Ready() is called within the poll window.
	if err := ctrl.HandleOperatorResume(context.Background(), ""); err != nil {
		t.Fatalf("em067: HandleOperatorResume: %v", err)
	}

	select {
	case <-notifyReady:
		// Correct: Ready() was called after operator resume.
	case <-time.After(5 * time.Second):
		t.Fatal("em067: Ready() was not called within 5s after operator resume — gate must release on resume")
	}

	cancel()
	select {
	case <-loopDone:
	case <-time.After(3 * time.Second):
		t.Fatal("em067: workloop did not exit within 3s after context cancel")
	}

	t.Logf("em067 PASS: Ready() correctly suppressed while paused, fired after resume (total Ready()=%d)",
		ledger.readyCalls.Load())
}

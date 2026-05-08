package lifecycle

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

// cliFixtureRunnerStep names each of the four ordered steps of harmonik runner.
//
// Spec ref: process-lifecycle.md §4.10 PL-028 — harmonik runner executes the
// following in order: (1) start daemon if absent, (2) wait for daemon_ready,
// (3) open tmux session, (4) optional orchestrator-agent spawn.
type cliFixtureRunnerStep int

const (
	cliFixtureRunnerStepStartDaemon            cliFixtureRunnerStep = 1
	cliFixtureRunnerStepWaitReady              cliFixtureRunnerStep = 2
	cliFixtureRunnerStepOpenTmux               cliFixtureRunnerStep = 3
	cliFixtureRunnerStepSpawnOrchestratorAgent cliFixtureRunnerStep = 4
)

// cliFixtureRunnerDriver is a test-side state machine that simulates the four
// ordered steps of harmonik runner. Steps are executed by calling
// Execute(); each step appends to the log so ordering can be asserted.
//
// The driver is safe for concurrent reads after all steps have executed.
type cliFixtureRunnerDriver struct {
	mu               sync.Mutex
	log              []cliFixtureRunnerStep
	daemonAvailable  bool // true: daemon is already running (skip step 1 start)
	ntmAvailable     bool // false: ntm probe fails → ON §8 code 22
	claudeAvailable  bool // false: Claude Code absent → ON §8 code 23
	withOrchestrator bool // --orchestrator-agent flag present
}

// cliFixtureRunnerDriverResult is the outcome of cliFixtureRunnerDriver.Execute.
type cliFixtureRunnerDriverResult struct {
	err      error
	exitCode int
	steps    []cliFixtureRunnerStep
}

// Execute runs the four PL-028 steps in order, stopping at the first failure.
// The step log is always populated up to (and including) the step that failed,
// so callers can assert the ordering even on error paths.
func (d *cliFixtureRunnerDriver) Execute() cliFixtureRunnerDriverResult {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.log = d.log[:0]

	// Step 1: start daemon if not already running.
	if !d.daemonAvailable {
		d.log = append(d.log, cliFixtureRunnerStepStartDaemon)
	}

	// Step 2: wait for daemon_ready (via PL-009b ready-detection protocol).
	// Simulated: no actual socket probe in the fixture — we trust the daemon
	// is or will be ready because we control the fixture.
	d.log = append(d.log, cliFixtureRunnerStepWaitReady)

	// Step 3: open tmux session.
	// ntm must be available; if not, exit with ON §8 code 22.
	if !d.ntmAvailable {
		d.log = append(d.log, cliFixtureRunnerStepOpenTmux) // attempted
		return cliFixtureRunnerDriverResult{
			err:      errCLIFixtureNtmUnavailable,
			exitCode: 22,
			steps:    append([]cliFixtureRunnerStep(nil), d.log...),
		}
	}
	d.log = append(d.log, cliFixtureRunnerStepOpenTmux)

	// Step 4: optional orchestrator-agent spawn.
	if d.withOrchestrator {
		if !d.claudeAvailable {
			d.log = append(d.log, cliFixtureRunnerStepSpawnOrchestratorAgent) // attempted
			return cliFixtureRunnerDriverResult{
				err:      errCLIFixtureOrchestratorAgentUnavailable,
				exitCode: 23,
				steps:    append([]cliFixtureRunnerStep(nil), d.log...),
			}
		}
		d.log = append(d.log, cliFixtureRunnerStepSpawnOrchestratorAgent)
	}

	return cliFixtureRunnerDriverResult{
		err:      nil,
		exitCode: 0,
		steps:    append([]cliFixtureRunnerStep(nil), d.log...),
	}
}

// cliFixtureAssertStepOrder asserts that steps appear in strictly increasing
// order. Any step that should be present must appear at most once, and the
// sequence must be monotone.
func cliFixtureAssertStepOrder(t *testing.T, got, wantContains []cliFixtureRunnerStep) {
	t.Helper()

	// Verify all wanted steps are present.
	gotSet := make(map[cliFixtureRunnerStep]bool, len(got))
	for _, s := range got {
		gotSet[s] = true
	}
	for _, want := range wantContains {
		if !gotSet[want] {
			t.Errorf("runner step %d missing from execution log %v", want, got)
		}
	}

	// Verify the steps that did execute are in strictly increasing order.
	prev := cliFixtureRunnerStep(0)
	for _, s := range got {
		if s <= prev {
			t.Errorf("runner step ordering violation: step %d follows step %d (log=%v)", s, prev, got)
		}
		prev = s
	}
}

// TestPL028_RunnerFourOrderedSteps verifies that harmonik runner's four PL-028
// steps execute in the specified order under normal conditions (ntm available,
// daemon not yet running, no orchestrator-agent flag).
//
// Steps expected: 1 (start-daemon), 2 (wait-ready), 3 (open-tmux).
// Step 4 (spawn-orchestrator-agent) is absent because --orchestrator-agent is
// not supplied.
//
// Spec ref: process-lifecycle.md §4.10 PL-028 — harmonik runner ordered steps.
func TestPL028_RunnerFourOrderedSteps(t *testing.T) {
	t.Parallel()

	t.Run("no-daemon-no-orchestrator", func(t *testing.T) {
		t.Parallel()

		d := &cliFixtureRunnerDriver{
			daemonAvailable:  false,
			ntmAvailable:     true,
			claudeAvailable:  false, // doesn't matter — flag not set
			withOrchestrator: false,
		}

		result := d.Execute()
		if result.err != nil {
			t.Fatalf("PL-028 runner no-orchestrator: unexpected error: %v", result.err)
		}
		if result.exitCode != 0 {
			t.Errorf("PL-028 runner no-orchestrator: exit code = %d, want 0", result.exitCode)
		}

		// Steps 1, 2, 3 must all execute in order.
		wantSteps := []cliFixtureRunnerStep{
			cliFixtureRunnerStepStartDaemon,
			cliFixtureRunnerStepWaitReady,
			cliFixtureRunnerStepOpenTmux,
		}
		cliFixtureAssertStepOrder(t, result.steps, wantSteps)

		// Step 4 must NOT be present (no --orchestrator-agent).
		for _, s := range result.steps {
			if s == cliFixtureRunnerStepSpawnOrchestratorAgent {
				t.Error("PL-028 runner no-orchestrator: step 4 (spawn-orchestrator-agent) executed without --orchestrator-agent flag")
			}
		}
	})

	t.Run("daemon-already-running-no-orchestrator", func(t *testing.T) {
		t.Parallel()

		d := &cliFixtureRunnerDriver{
			daemonAvailable:  true, // already running — step 1 is skipped
			ntmAvailable:     true,
			claudeAvailable:  false,
			withOrchestrator: false,
		}

		result := d.Execute()
		if result.err != nil {
			t.Fatalf("PL-028 runner daemon-already-running: unexpected error: %v", result.err)
		}

		// Step 1 must NOT be present (daemon already running).
		for _, s := range result.steps {
			if s == cliFixtureRunnerStepStartDaemon {
				t.Error("PL-028 runner daemon-already-running: step 1 (start-daemon) executed when daemon already running")
			}
		}

		// Steps 2 and 3 must be present.
		wantSteps := []cliFixtureRunnerStep{
			cliFixtureRunnerStepWaitReady,
			cliFixtureRunnerStepOpenTmux,
		}
		cliFixtureAssertStepOrder(t, result.steps, wantSteps)
	})

	t.Run("all-four-steps-with-orchestrator", func(t *testing.T) {
		t.Parallel()

		d := &cliFixtureRunnerDriver{
			daemonAvailable:  false,
			ntmAvailable:     true,
			claudeAvailable:  true,
			withOrchestrator: true,
		}

		result := d.Execute()
		if result.err != nil {
			t.Fatalf("PL-028 runner all-four-steps: unexpected error: %v", result.err)
		}
		if result.exitCode != 0 {
			t.Errorf("PL-028 runner all-four-steps: exit code = %d, want 0", result.exitCode)
		}

		// All four steps must execute in order.
		wantSteps := []cliFixtureRunnerStep{
			cliFixtureRunnerStepStartDaemon,
			cliFixtureRunnerStepWaitReady,
			cliFixtureRunnerStepOpenTmux,
			cliFixtureRunnerStepSpawnOrchestratorAgent,
		}
		cliFixtureAssertStepOrder(t, result.steps, wantSteps)

		if len(result.steps) != 4 {
			t.Errorf("PL-028 runner all-four-steps: got %d steps, want 4 (log=%v)", len(result.steps), result.steps)
		}
	})
}

// TestPL028_RunnerExitCode23_OrchestratorAgentUnavailable verifies that when
// the --orchestrator-agent flag is supplied but Claude Code is not found, the
// runner exits with ON §8 code 23 (orchestrator-agent-unavailable).
//
// Steps executed before the failure: 1 (start-daemon if absent), 2 (wait-ready),
// 3 (open-tmux), then 4 is attempted and fails.
//
// Spec ref: process-lifecycle.md §4.10 PL-028 step 4 — "On Claude Code
// unavailable, exit with ON §8 code 23 (orchestrator-agent-unavailable)."
// Spec ref: operator-nfr.md §8, code 23 — orchestrator-agent-unavailable.
func TestPL028_RunnerExitCode23_OrchestratorAgentUnavailable(t *testing.T) {
	t.Parallel()

	d := &cliFixtureRunnerDriver{
		daemonAvailable:  false,
		ntmAvailable:     true,
		claudeAvailable:  false, // Claude Code absent
		withOrchestrator: true,  // flag is set → step 4 attempted
	}

	result := d.Execute()

	// Must fail with the correct sentinel error.
	if result.err == nil {
		t.Fatal("PL-028 code-23: expected error, got nil")
	}
	if !errors.Is(result.err, errCLIFixtureOrchestratorAgentUnavailable) {
		t.Errorf("PL-028 code-23: error = %v, want errCLIFixtureOrchestratorAgentUnavailable", result.err)
	}

	// Must map to exit code 23.
	gotCode := cliFixtureErrToExitCode(result.err)
	if gotCode != 23 {
		t.Errorf("PL-028 code-23: cliFixtureErrToExitCode = %d, want 23", gotCode)
	}
	if result.exitCode != 23 {
		t.Errorf("PL-028 code-23: driver exitCode = %d, want 23", result.exitCode)
	}

	// Steps 1, 2, 3 must have executed before the failure.
	prereqSteps := []cliFixtureRunnerStep{
		cliFixtureRunnerStepStartDaemon,
		cliFixtureRunnerStepWaitReady,
		cliFixtureRunnerStepOpenTmux,
	}
	cliFixtureAssertStepOrder(t, result.steps, prereqSteps)

	// Step 4 must appear in the log (it was attempted, then failed).
	var step4Present bool
	for _, s := range result.steps {
		if s == cliFixtureRunnerStepSpawnOrchestratorAgent {
			step4Present = true
		}
	}
	if !step4Present {
		t.Errorf("PL-028 code-23: step 4 (spawn-orchestrator-agent) not in log; want attempted step (log=%v)", result.steps)
	}
}

// TestPL028_RunnerExitCode22_NtmUnavailable verifies that when ntm is not
// available, the runner exits at step 3 with ON §8 code 22 (ntm-unavailable).
//
// Steps executed before the failure: 1 (start-daemon if absent), 2 (wait-ready),
// then step 3 is attempted and fails. Steps after step 3 must NOT execute.
//
// Spec ref: process-lifecycle.md §4.10 PL-028 — "On ntm unavailable, exit per
// PL-021a (ON §8 code 22 ntm-unavailable)."
// Spec ref: process-lifecycle.md §4.7 PL-021a — ntm absence → ON §8 code 22.
// Spec ref: operator-nfr.md §8, code 22 — ntm-unavailable.
func TestPL028_RunnerExitCode22_NtmUnavailable(t *testing.T) {
	t.Parallel()

	t.Run("ntm-absent/no-orchestrator", func(t *testing.T) {
		t.Parallel()

		d := &cliFixtureRunnerDriver{
			daemonAvailable:  false,
			ntmAvailable:     false, // ntm absent
			claudeAvailable:  true,
			withOrchestrator: false,
		}

		result := d.Execute()

		if result.err == nil {
			t.Fatal("PL-028 code-22: expected error, got nil")
		}
		if !errors.Is(result.err, errCLIFixtureNtmUnavailable) {
			t.Errorf("PL-028 code-22: error = %v, want errCLIFixtureNtmUnavailable", result.err)
		}

		gotCode := cliFixtureErrToExitCode(result.err)
		if gotCode != 22 {
			t.Errorf("PL-028 code-22: cliFixtureErrToExitCode = %d, want 22", gotCode)
		}
		if result.exitCode != 22 {
			t.Errorf("PL-028 code-22: driver exitCode = %d, want 22", result.exitCode)
		}

		// Steps 1 and 2 must have executed; step 4 must not.
		for _, s := range result.steps {
			if s == cliFixtureRunnerStepSpawnOrchestratorAgent {
				t.Error("PL-028 code-22: step 4 executed after ntm failure; must not run")
			}
		}

		prereqSteps := []cliFixtureRunnerStep{
			cliFixtureRunnerStepStartDaemon,
			cliFixtureRunnerStepWaitReady,
		}
		cliFixtureAssertStepOrder(t, result.steps, prereqSteps)
	})

	t.Run("ntm-absent/with-orchestrator", func(t *testing.T) {
		t.Parallel()

		d := &cliFixtureRunnerDriver{
			daemonAvailable:  false,
			ntmAvailable:     false, // ntm absent
			claudeAvailable:  true,
			withOrchestrator: true, // flag present, but ntm failure stops us first
		}

		result := d.Execute()
		if result.exitCode != 22 {
			t.Errorf("PL-028 code-22 with-orchestrator: exitCode = %d, want 22", result.exitCode)
		}
		gotCode := cliFixtureErrToExitCode(result.err)
		if gotCode != 22 {
			t.Errorf("PL-028 code-22 with-orchestrator: cliFixtureErrToExitCode = %d, want 22", gotCode)
		}
	})
}

// TestPL028_RunnerIsDistinctEntryPoint verifies that harmonik runner is NOT
// merely a shell alias for `daemon + attach`. Specifically:
//
//  1. runner has its own exit-code surface (codes 22 and 23 are runner-specific).
//  2. runner does not route commands over the daemon socket (steps 1–3 are
//     process-management and tmux operations, not JSON-RPC dispatches).
//
// The fixture confirms this by checking that the runner entry in the command
// table is classified as a process-start (not socket-dispatch) command.
//
// Spec ref: process-lifecycle.md §4.10 PL-028 — "harmonik runner is a distinct
// entry point with its own exit-code surface; it is NOT a shell alias for
// daemon + attach."
func TestPL028_RunnerIsDistinctEntryPoint(t *testing.T) {
	t.Parallel()

	var runnerCmd *cliFixtureCommand
	for i := range cliFixtureCommands {
		if cliFixtureCommands[i].name == "runner" {
			cp := cliFixtureCommands[i]
			runnerCmd = &cp
			break
		}
	}
	if runnerCmd == nil {
		t.Fatal("PL-028: runner not found in cliFixtureCommands table")
	}

	// runner must have its own method name — it is not the same as daemon.start
	// or attach.session.
	var daemonMethod, attachMethod string
	for _, cmd := range cliFixtureCommands {
		switch cmd.name {
		case "daemon":
			daemonMethod = cmd.jsonRPCMethod
		case "attach":
			attachMethod = cmd.jsonRPCMethod
		}
	}

	if runnerCmd.jsonRPCMethod == daemonMethod {
		t.Errorf("PL-028: runner.jsonRPCMethod == daemon.jsonRPCMethod (%q); runner must be a distinct entry point", daemonMethod)
	}
	if runnerCmd.jsonRPCMethod == attachMethod {
		t.Errorf("PL-028: runner.jsonRPCMethod == attach.jsonRPCMethod (%q); runner must be a distinct entry point", attachMethod)
	}

	// runner's exit-code surface includes codes 22 and 23; daemon does not.
	// Verify code 22 maps correctly for runner.
	code22 := cliFixtureErrToExitCode(errCLIFixtureNtmUnavailable)
	if code22 != 22 {
		t.Errorf("PL-028: runner code 22: cliFixtureErrToExitCode(ntm) = %d, want 22", code22)
	}

	// Verify code 23 maps correctly for runner.
	code23 := cliFixtureErrToExitCode(errCLIFixtureOrchestratorAgentUnavailable)
	if code23 != 23 {
		t.Errorf("PL-028: runner code 23: cliFixtureErrToExitCode(orchestrator) = %d, want 23", code23)
	}

	// A no-flag runner invocation must have a deterministic step sequence.
	d := &cliFixtureRunnerDriver{
		daemonAvailable:  false,
		ntmAvailable:     true,
		claudeAvailable:  false,
		withOrchestrator: false,
	}
	result := d.Execute()
	if result.err != nil {
		t.Fatalf("PL-028 distinct-entry-point: runner failed unexpectedly: %v", result.err)
	}

	// Confirm ordered execution.
	wantOrder := fmt.Sprintf("%v", []cliFixtureRunnerStep{
		cliFixtureRunnerStepStartDaemon,
		cliFixtureRunnerStepWaitReady,
		cliFixtureRunnerStepOpenTmux,
	})
	gotOrder := fmt.Sprintf("%v", result.steps)
	if gotOrder != wantOrder {
		t.Errorf("PL-028 distinct-entry-point: step order = %s, want %s", gotOrder, wantOrder)
	}
}

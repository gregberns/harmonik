package daemon_test

// dot_sandbox_minted_hkdqo9u_test.go — the DOT cascade must consult the srt
// sandbox gate for EVERY harness, not only SessionIDCaptured ones (hk-dqo9u).
//
// # The bug
//
// The DOT cascade's sandbox wiring lived entirely inside
//
//	if h.SessionIDPolicy() == handlercontract.SessionIDCaptured { ... }
//
// with no else. sandboxSpawnForRun was therefore only ever CALLED for pi and
// codex. claude is SessionIDMinted (handlercontract/harness.go names ClaudeHarness
// as the non-captured example), so for a claude run under DOT the gate was never
// consulted, perRunSubstrate.sandboxSpawn was never assigned, and SpawnWindow
// spawned the agent BARE.
//
// It failed SILENTLY AND OPEN, which is the part that makes it worth a dedicated
// regression: `sandbox.harnesses: [claude-code]` in config.yaml read like
// protection and did nothing. No value of any config key could turn it on, and
// nothing was logged. Single mode never had the bug — workloop.go computes the
// same gate unconditionally and feeds both launch paths.
//
// Measured before it was found in source: a two-arm control on the same binary,
// config, and harness, differing only in workflow mode, showed DOT launching
// claude bare while single mode wrapped it in srt with a real per-run profile.
//
// # Why the assertion is on a seam and not on the substrate field
//
// The natural assertion — "perRunSubstrate.sandboxSpawn is non-nil" — is not
// reachable from a test. newPerRunSubstrate returns nil unless the substrate is a
// *tmuxSubstrate, and the DOT test fixtures necessarily use a test-double
// substrate, so the field would always be unobservable. tmuxsubstrate.go's own
// comment on newPerRunSubstrate documents this and solves it the same way for the
// runner arg (substrateRunnerObserver, hk-fxy9): observe the DECISION as it is
// made rather than the field it is stored in. sandboxGateObserver is that seam.
//
// The seam fires unconditionally at the launch site, BEFORE any policy branch.
// That is what makes this test a real regression guard: if the gate call is ever
// moved back inside the SessionIDCaptured branch, the observer simply never fires
// for claude and the test fails on observerCalled — the exact original defect.
//
// Bead: hk-dqo9u.

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/workflow"
)

// hkdqo9uClaudeRegistry returns a HarnessRegistry with the real ClaudeHarness
// registered under claude-code. The real harness is used deliberately rather than
// a stub: the defect turned on its SessionIDPolicy being Minted, so a stub that
// hardcoded the policy would assume away the thing under test.
func hkdqo9uClaudeRegistry(t *testing.T) *handlercontract.HarnessRegistry {
	t.Helper()
	reg := handlercontract.NewHarnessRegistry()
	if err := reg.Register(core.AgentTypeClaudeCode, daemon.ExportedNewClaudeHarness()); err != nil {
		t.Fatalf("hkdqo9uClaudeRegistry: register claude-code: %v", err)
	}
	return reg
}

// TestDotMode_MintedHarness_ConsultsSandboxGate is the RED-before/GREEN-after for
// hk-dqo9u: driving a DOT workflow whose implementer is claude (SessionIDMinted),
// with claude-code listed in sandbox.harnesses, MUST consult the srt gate and get
// a non-nil spawn decision.
//
// Pre-fix this failed on observerCalled: the gate sat inside the
// SessionIDCaptured branch, so for claude it was never called at all.
func TestDotMode_MintedHarness_ConsultsSandboxGate(t *testing.T) {
	skipRealDaemonE2EInShort(t)

	// NOT t.Parallel(): sandboxGateObserver is package-level shared state, same
	// constraint the substrateRunnerObserver tests operate under.

	projectDir := implReadyFixtureProjectDir(t)
	wtPath, parentSHA := implReadyFixtureWorktree(t, projectDir)
	scriptPath := implReadyFixtureHandlerScript(t)
	adapterReg := implReadyFixtureAdapterRegistry(t)

	dotPath := filepath.Join(dotE2EModuleRoot(), "specs", "examples", "review-loop.dot")
	graph, loadErr := workflow.LoadDotWorkflow(dotPath)
	if loadErr != nil {
		t.Fatalf("LoadDotWorkflow(%s): %v", dotPath, loadErr)
	}

	var (
		mu             sync.Mutex
		observerCalled bool
		gotAgentType   core.AgentType
		gotSpawnNonNil bool
	)
	daemon.ExportedSetSandboxGateObserver(func(agentType core.AgentType, spawn *daemon.ExportedSrtSpawnConfig) {
		mu.Lock()
		defer mu.Unlock()
		// Record only the claude-code decision. The graph's later nodes may
		// resolve other harnesses; the claim under test is specifically that the
		// MINTED harness reaches the gate.
		if agentType != core.AgentTypeClaudeCode {
			return
		}
		observerCalled = true
		gotAgentType = agentType
		gotSpawnNonNil = spawn != nil
	})
	defer daemon.ExportedSetSandboxGateObserver(nil)

	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:           &stubBeadLedger{},
		Bus:                 &stubEventCollector{},
		ProjectDir:          projectDir,
		HandlerBinary:       "/bin/sh",
		HandlerArgs:         []string{scriptPath},
		IntentLogDir:        filepath.Join(projectDir, ".harmonik", "beads-intents"),
		WorkflowModeDefault: core.WorkflowModeDot,
		AdapterRegistry2:    adapterReg,
		HarnessRegistry:     hkdqo9uClaudeRegistry(t),
		// The config that the bug rendered inert: backend srt, claude-code listed.
		SandboxCfg: daemon.SandboxConfig{
			Backend:   "srt",
			Harnesses: []string{"claude-code"},
		},
		// The handler hangs; we only need the launch path to reach the gate, which
		// happens well before agent_ready. A short timeout ends the run promptly.
		AgentReadyTimeout: 100 * time.Millisecond,
		HookStore:         daemon.ExportedNewHookSessionStore(),
	})

	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()

	_ = daemon.ExportedDriveDotWorkflow(
		ctx, deps,
		implReadyFixtureRunID(t),
		core.BeadID("dot-sandbox-minted-hkdqo9u-001"),
		wtPath, parentSHA,
		graph,
	)

	mu.Lock()
	defer mu.Unlock()

	// THE regression assertion. Pre-fix this is where it fails: the gate was
	// never called for a SessionIDMinted harness, so the observer never fired.
	if !observerCalled {
		t.Fatalf("hk-dqo9u FAIL: the DOT cascade never consulted the srt sandbox gate for claude-code "+
			"(SessionIDMinted). sandbox.harnesses=%v was silently ignored — this is the fail-open defect: "+
			"the config reads like protection and does nothing.", []string{"claude-code"})
	}
	if gotAgentType != core.AgentTypeClaudeCode {
		t.Errorf("hk-dqo9u: gate keyed off %q; want %q", gotAgentType, core.AgentTypeClaudeCode)
	}
	// With backend=srt and the harness listed, the gate's two config predicates
	// are both satisfied, so the decision must be a real spawn config. A nil here
	// would mean the call site was reached but the run still launches bare.
	if !gotSpawnNonNil {
		t.Errorf("hk-dqo9u FAIL: gate consulted for claude-code but returned a nil SrtSpawnConfig; "+
			"want non-nil (backend=%q, harnesses=%v both match, so the run must be sandboxed)",
			"srt", []string{"claude-code"})
	}
}

// TestDotMode_MintedHarness_NoSubstrateRefusesToLaunch pins the FAIL-CLOSED half
// of the substrate path (india's review of hk-dqo9u).
//
// The attach `prs.sandboxSpawn = sandboxSpawn` needs somewhere to attach to, and
// newPerRunSubstrate returns nil when the substrate is absent or is not a
// *tmuxSubstrate. Guarding the attach with `prs != nil` would make that case
// silently skip the sandbox and launch bare — relocating hk-dqo9u from "wrong
// branch" to "nil substrate" rather than fixing it, with the config still reading
// like protection and still doing nothing.
//
// Worse, verifySandboxEngaged runs BEFORE this point, so continuing would emit
// positive evidence that the sandbox engages and then launch unprotected.
//
// Note what this test does NOT rely on: the sandboxGateObserver assertion in the
// tests above proves the gate was CONSULTED, never that the sandbox was APPLIED.
// In this failure mode the observer fires with a non-nil verdict and those
// assertions pass while the run is unsandboxed. Consultation and application are
// separate properties and need separate tests; this is the application half.
func TestDotMode_MintedHarness_NoSubstrateRefusesToLaunch(t *testing.T) {
	skipRealDaemonE2EInShort(t)

	projectDir := implReadyFixtureProjectDir(t)
	wtPath, parentSHA := implReadyFixtureWorktree(t, projectDir)
	scriptPath := implReadyFixtureHandlerScript(t)
	adapterReg := implReadyFixtureAdapterRegistry(t)

	dotPath := filepath.Join(dotE2EModuleRoot(), "specs", "examples", "review-loop.dot")
	graph, loadErr := workflow.LoadDotWorkflow(dotPath)
	if loadErr != nil {
		t.Fatalf("LoadDotWorkflow(%s): %v", dotPath, loadErr)
	}

	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:           &stubBeadLedger{},
		Bus:                 &stubEventCollector{},
		ProjectDir:          projectDir,
		HandlerBinary:       "/bin/sh",
		HandlerArgs:         []string{scriptPath},
		IntentLogDir:        filepath.Join(projectDir, ".harmonik", "beads-intents"),
		WorkflowModeDefault: core.WorkflowModeDot,
		AdapterRegistry2:    adapterReg,
		HarnessRegistry:     hkdqo9uClaudeRegistry(t),
		// Sandbox IS configured for this harness...
		SandboxCfg: daemon.SandboxConfig{
			Backend:   "srt",
			Harnesses: []string{"claude-code"},
		},
		// ...and Substrate is deliberately left nil, so newPerRunSubstrate returns
		// nil and there is nothing to attach the spawn config to.
		AgentReadyTimeout: 100 * time.Millisecond,
		HookStore:         daemon.ExportedNewHookSessionStore(),
	})

	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()

	result := daemon.ExportedDriveDotWorkflow(
		ctx, deps,
		implReadyFixtureRunID(t),
		core.BeadID("dot-sandbox-minted-hkdqo9u-003"),
		wtPath, parentSHA,
		graph,
	)

	// Assert on the REASON, not merely on failure. `success == false` alone is
	// worthless here: the fixture's handler hangs, so a run that launches bare
	// still fails — with agent_ready_timeout. A mutation check confirmed exactly
	// that; with the fail-closed guard neutered, an assertion on success alone
	// still passed, i.e. the test bound to nothing. The run must fail BECAUSE the
	// sandbox could not be attached.
	if result.Success {
		t.Fatalf("hk-dqo9u FAIL-OPEN: the run SUCCEEDED with srt configured for claude-code but no " +
			"per-run substrate to attach the spawn config to — it launched unsandboxed.")
	}
	const wantReason = "no per-run substrate to attach it to"
	if !strings.Contains(result.Summary, wantReason) {
		t.Fatalf("hk-dqo9u FAIL-OPEN: run failed, but NOT for the sandbox reason.\n"+
			"  summary: %q\n"+
			"  want it to contain: %q\n"+
			"The run must REFUSE because the sandbox could not be attached. Failing for any other "+
			"reason (e.g. agent_ready_timeout) means the agent was launched BARE first and the "+
			"sandbox was silently skipped — which is hk-dqo9u relocated, not fixed. Note that "+
			"verifySandboxEngaged has already passed by this point, so a bare launch here would "+
			"carry positive evidence that the sandbox engaged.",
			result.Summary, wantReason)
	}
	t.Logf("refused for the right reason: summary=%q", result.Summary)
}

// TestDotMode_MintedHarness_UnlistedHarnessStaysUnsandboxed pins the other side of
// the gate so the fix above cannot degenerate into "always sandbox everything".
// The gate must still be CONSULTED for claude (that is the fix), but with
// claude-code absent from sandbox.harnesses the decision must be nil.
func TestDotMode_MintedHarness_UnlistedHarnessStaysUnsandboxed(t *testing.T) {
	skipRealDaemonE2EInShort(t)

	projectDir := implReadyFixtureProjectDir(t)
	wtPath, parentSHA := implReadyFixtureWorktree(t, projectDir)
	scriptPath := implReadyFixtureHandlerScript(t)
	adapterReg := implReadyFixtureAdapterRegistry(t)

	dotPath := filepath.Join(dotE2EModuleRoot(), "specs", "examples", "review-loop.dot")
	graph, loadErr := workflow.LoadDotWorkflow(dotPath)
	if loadErr != nil {
		t.Fatalf("LoadDotWorkflow(%s): %v", dotPath, loadErr)
	}

	var (
		mu             sync.Mutex
		observerCalled bool
		gotSpawnNonNil bool
	)
	daemon.ExportedSetSandboxGateObserver(func(agentType core.AgentType, spawn *daemon.ExportedSrtSpawnConfig) {
		mu.Lock()
		defer mu.Unlock()
		if agentType != core.AgentTypeClaudeCode {
			return
		}
		observerCalled = true
		gotSpawnNonNil = spawn != nil
	})
	defer daemon.ExportedSetSandboxGateObserver(nil)

	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:           &stubBeadLedger{},
		Bus:                 &stubEventCollector{},
		ProjectDir:          projectDir,
		HandlerBinary:       "/bin/sh",
		HandlerArgs:         []string{scriptPath},
		IntentLogDir:        filepath.Join(projectDir, ".harmonik", "beads-intents"),
		WorkflowModeDefault: core.WorkflowModeDot,
		AdapterRegistry2:    adapterReg,
		HarnessRegistry:     hkdqo9uClaudeRegistry(t),
		// backend is srt, but claude-code is NOT listed: gate consulted, no wrap.
		SandboxCfg: daemon.SandboxConfig{
			Backend:   "srt",
			Harnesses: []string{"pi"},
		},
		AgentReadyTimeout: 100 * time.Millisecond,
		HookStore:         daemon.ExportedNewHookSessionStore(),
	})

	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()

	_ = daemon.ExportedDriveDotWorkflow(
		ctx, deps,
		implReadyFixtureRunID(t),
		core.BeadID("dot-sandbox-minted-hkdqo9u-002"),
		wtPath, parentSHA,
		graph,
	)

	mu.Lock()
	defer mu.Unlock()

	if !observerCalled {
		t.Fatalf("hk-dqo9u: the gate must be CONSULTED for claude-code even when the harness is " +
			"unlisted — the consult is the fix; the nil verdict is the config's answer.")
	}
	if gotSpawnNonNil {
		t.Errorf("hk-dqo9u FAIL: claude-code is absent from sandbox.harnesses but the gate returned a " +
			"non-nil SrtSpawnConfig; the fix must not sandbox harnesses the operator did not list.")
	}
}

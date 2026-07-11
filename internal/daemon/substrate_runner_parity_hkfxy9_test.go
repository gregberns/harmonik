package daemon_test

// substrate_runner_parity_hkfxy9_test.go — PARITY coverage for the SUBSTRATE-SPAWN
// runner threading across EVERY newPerRunSubstrate call site in internal/daemon,
// extending launch_substrate_runner_threading_hkfxy9_test.go (which covers the two
// LIVE-fixed remote paths: review-loop implementer + DOT agentic node).
//
// # Why this file exists
//
// newPerRunSubstrate(sub, bin, runner) is the seam that decides WHERE the claude
// PROCESS spawns. For a REMOTE run the `runner` MUST be the per-run SSHRunner so
// the process lands on the WORKER's tmux server; a hardcoded nil spawns it against
// box A's tmux/-default session (absent on the worker) → launch_initiated wedge →
// agent_ready_timeout → no_commit (the hk-fxy9 / hk-538l defect).
//
// An audit of all six call sites (PART 1 of the chani-rs-integ task) found:
//
//   call-site                       launch path                runner    verdict
//   ─────────────────────────────── ────────────────────────── ───────── ─────────
//   workloop.go:3206                single-mode implementer     runRunner SAFE
//   reviewloop.go:349               review-loop implementer     runner    SAFE (LIVE FIX, covered)
//   reviewloop.go:1267              review-loop REVIEWER        nil       SAFE (intentional, box A)
//   dot_cascade.go:1078             DOT agentic node            runner    SAFE (LIVE FIX, covered)
//   dot_gate.go:329                 DOT cognition-gate          runner    SAFE (LIVE FIX, covered; hk-9fe2)
//   crewstart.go:247                crew test-double fallback   nil       N/A    (not an agent run)
//
// The three LIVE-fixed paths (review-loop implementer, DOT agentic node, DOT
// cognition-gate) have non-nil sentinel assertions — the first two in
// launch_substrate_runner_threading_hkfxy9_test.go, the third
// (TestDotCognitionGateSubstrateRunnerLeak_hk9fe2) below. This file documents
// the remaining three. Each is a t.Skip with the precise reason it is NOT a
// passing non-nil assertion here — either it is correctly nil by design, or it
// is only reachable through real-SSH / heavy-fixture machinery that has no
// pure-local seam.
//
// The skips are intentional regression DOCUMENTATION, not stubs: when a leak is
// closed (hk-9fe2) or a local seam is added for the single-mode/reviewer paths,
// the matching Skip should be replaced with the live assertion described in its body.

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	tmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	"github.com/gregberns/harmonik/internal/workflow/dot"
)

// hk9fe2FakeGateRegistry is a minimal core.Registry stub that resolves exactly
// one Cognition-tagged Gate ControlPoint by name. Only LookupByName is exercised
// by dispatchDotGateNode; the other Registry methods are unused no-ops.
type hk9fe2FakeGateRegistry struct {
	cp core.ControlPoint
}

func (r hk9fe2FakeGateRegistry) Register(core.ControlPoint) error { return nil }

func (r hk9fe2FakeGateRegistry) LookupByName(name string) (core.ControlPoint, bool) {
	if name == r.cp.Name {
		return r.cp, true
	}
	return core.ControlPoint{}, false
}

func (r hk9fe2FakeGateRegistry) LookupByTrigger(string) []core.ControlPoint {
	return []core.ControlPoint{}
}

func (r hk9fe2FakeGateRegistry) LookupByAttachPoint(core.AttachPoint) []core.ControlPoint {
	return []core.ControlPoint{}
}

func (r hk9fe2FakeGateRegistry) All() []core.ControlPoint { return []core.ControlPoint{r.cp} }

// hkfxy9ParityObserver installs the shared substrate-runner observer and returns
// the capture channel + a cleanup-registering helper, mirroring the setup used by
// TestReviewLoopThreadsRunnerIntoSubstrate_hkfxy9. Used directly by
// TestDotCognitionGateSubstrateRunnerLeak_hk9fe2 below.
func hkfxy9ParityObserver(t *testing.T) chan tmux.CommandRunner {
	t.Helper()
	captured := make(chan tmux.CommandRunner, 4)
	daemon.ExportedSetSubstrateRunnerObserver(func(r tmux.CommandRunner) {
		select {
		case captured <- r:
		default:
		}
	})
	t.Cleanup(func() { daemon.ExportedSetSubstrateRunnerObserver(nil) })
	return captured
}

// TestSingleModeWorkloopThreadsRunnerIntoSubstrate_hkfxy9 is the parity twin of
// TestReviewLoopThreadsRunnerIntoSubstrate_hkfxy9 for the SINGLE-MODE workloop
// implementer (workloop.go:3206). The source is SAFE — it threads `runRunner`
// (= rbc.sshRunner for a remote run) into newPerRunSubstrate, mirroring the
// review-loop wiring — but it is NOT unit-testable with a sentinel runner from a
// pure-local test:
//
//   - runRunner is non-nil ONLY when deps.workerRegistry.SelectWorker() returns a
//     worker (workloop.go:2441), and the runner is then hardcoded as
//     tmux.SSHRunner{Host: w.Host} (workloop.go:2445) — there is no seam to inject
//     the hk3susFakeRunner sentinel.
//   - Reaching newPerRunSubstrate (line 3206) first requires fetchBaseOnWorker
//     (line 2560, a REAL `ssh <host> git fetch`), which returns early on failure.
//     Against a bogus/fake host it fails before the substrate spawn, so the
//     observer never fires.
//
// The single-mode remote path is therefore covered END-TO-END by the SSH-gated
// scenario test scenario_remote_substrate_localhost_test.go
// (TestScenario_RemoteSubstrate_Localhost_E2E, build tag `scenario`), which drives
// a real `ssh localhost` worker through the whole lifecycle. To make THIS a
// pure-local unit assertion, the single-mode path would need a runner-injection
// seam analogous to ExportedRunReviewLoopWithRunner that bypasses the
// SSHRunner-construction + fetchBaseOnWorker steps.
func TestSingleModeWorkloopThreadsRunnerIntoSubstrate_hkfxy9(t *testing.T) {
	t.Skip("workloop.go:3206 single-mode threads runRunner (SAFE) but is only " +
		"reachable with a NON-nil runner via a real-SSH worker (fetchBaseOnWorker " +
		"gates the substrate spawn); no pure-local seam injects the sentinel. " +
		"Covered end-to-end by scenario_remote_substrate_localhost_test.go " +
		"(build tag `scenario`). Un-skip when a single-mode WithRunner seam exists.")
}

// TestReviewLoopReviewerSubstrateRunnerIsNil_hkfxy9 documents that the review-loop
// REVIEWER substrate runner (reviewloop.go:1267) is intentionally nil and that
// this is CORRECT, not a leak. For a remote run the implementer commits on the
// worker and PUSHES the run branch; box A then fetches it, and the REVIEWER runs
// against the implementer's pushed SHA on box A — so BOTH its spec runner
// (reviewloop.go:1204, explicitly `runner: nil`) and its substrate runner
// (reviewloop.go:1267) must be nil. A non-nil assertion here would be a
// REGRESSION, asserting the wrong thing.
//
// It is skip-documented rather than actively pinned-to-nil because reaching the
// reviewer phase from a unit test requires the implementer phase to first commit +
// agent_ready + emit an approve verdict (the hang-handler idiom used by the
// implementer test deliberately never gets there). That is a heavy fixture lift
// for a property already guaranteed by the literal `nil` at the call site and the
// load-bearing comment at reviewloop.go:1197-1204.
func TestReviewLoopReviewerSubstrateRunnerIsNil_hkfxy9(t *testing.T) {
	t.Skip("reviewloop.go:1267 reviewer substrate runner is intentionally nil " +
		"(the reviewer runs on box A against the implementer's pushed SHA — both " +
		"its spec runner reviewloop.go:1204 and substrate runner are correctly " +
		"nil). NOT a leak; a non-nil assert would be wrong. Reaching the reviewer " +
		"phase needs an implementer-approve fixture; the static nil + the " +
		"load-bearing comment at reviewloop.go:1197-1204 are the guarantee.")
}

// TestDotCognitionGateSubstrateRunnerLeak_hk9fe2 is the hk-9fe2 regression: it
// proves the DOT cognition-gate launch path (dispatchDotGateNode →
// buildCognitionGateEval → executeCognitionGate, dot_gate.go) now passes the
// non-nil CommandRunner it receives into newPerRunSubstrate, mirroring
// TestDotThreadsRunnerIntoSubstrate_hkfxy9 for the agentic-node path. Before the
// fix this call site hardcoded nil, so a REMOTE DOT run whose graph used a
// cognition gate would spawn the gate-evaluator's claude PROCESS against box A's
// tmux/-default session (absent on the worker) instead of the worker.
//
// The graph's start node is a Gate node whose gate_ref resolves (via
// hk9fe2FakeGateRegistry) to a Cognition-tagged ControlPoint, so
// dispatchDotGateNode routes into buildCognitionGateEval/executeCognitionGate —
// the exact call chain that previously hardcoded nil at dot_gate.go:329.
func TestDotCognitionGateSubstrateRunnerLeak_hk9fe2(t *testing.T) {
	skipRealDaemonE2EInShort(t) // spawns real tmux pane (sleep 3600 hang-handler) — reap can time out, leaving zombie pane that wedges sibling tests
	// NOT parallel: installs a process-global test seam + isolates ~/.claude.json.
	rlIsolateClaudeConfig(t)

	projectDir := implReadyFixtureProjectDir(t)
	wtPath, parentSHA := implReadyFixtureWorktree(t, projectDir)
	scriptPath := hkfxy9HangHandlerScript(t)
	adapterReg := implReadyFixtureAdapterRegistry(t)

	captured := hkfxy9ParityObserver(t)

	gateCP := core.ControlPoint{
		Name: "hk9fe2_cognition_gate",
		Kind: core.KindGate,
		Evaluator: core.Evaluator{
			Mode: core.ModeTagCognition,
			DelegationPath: &core.DelegationPath{
				Role:              "gate-evaluator",
				ModelClass:        "reviewer-tier-1",
				InputSchemaRef:    "hk9fe2-input",
				ResponseSchemaRef: "hk9fe2-response",
				PromptTemplateRef: "hk9fe2-prompt",
			},
		},
	}

	graph := &dot.Graph{
		StartNodeID:     "gate1",
		TerminalNodeIDs: []string{"close"},
		Nodes: []*dot.Node{
			{
				ID:      "gate1",
				Type:    core.NodeTypeGate,
				GateRef: gateCP.Name,
			},
		},
		UnknownAttrs: map[string]string{},
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
		AgentReadyTimeout:   100 * time.Millisecond,
		HookStore:           daemon.ExportedNewHookSessionStore(),
		CPRegistry:          hk9fe2FakeGateRegistry{cp: gateCP},
	})

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	want := hk3susFakeRunner{}
	_ = daemon.ExportedDriveDotWorkflowWithRunner(
		ctx, deps, implReadyFixtureRunID(t), core.BeadID("hk-9fe2-dot-gate-substrate-001"),
		"gate task", "bead body",
		wtPath, parentSHA, graph, want,
	)

	select {
	case got := <-captured:
		if got == nil {
			t.Fatal("DOT cognition-gate newPerRunSubstrate runner is nil; the gate-evaluator claude PROCESS would spawn against box A's tmux/-default session, not the worker (hk-9fe2 regression)")
		}
		if _, ok := got.(hk3susFakeRunner); !ok {
			t.Fatalf("DOT cognition-gate substrate runner = %T; want the sentinel hk3susFakeRunner passed into driveDotWorkflow", got)
		}
	default:
		t.Fatal("substrate-runner seam was never invoked — DOT cognition-gate launch path did not reach newPerRunSubstrate")
	}
}

// TestCodexRoutedSpecWriteAgentTaskBoxALeak_hkr36v documents a SIBLING leak found
// in the same audit — NOT a newPerRunSubstrate (substrate-spawn) site, but a
// SPEC-runner site, the hk-3sus family. buildCodexRoutedLaunchSpec
// (harnessregistry.go:147) writes the agent brief with the box-A-LOCAL
// workspace.WriteAgentTask(rc.workspacePath, …), whereas the claude path
// (claudelaunchspec.go:329) correctly uses
// workspace.WriteAgentTaskVia(ctx, rc.runner, rc.workspacePath, …). For a REMOTE
// codex run the agent-task.md would be written to box A's worktree path instead of
// the WORKER's, so the worker-side codex would launch without its brief.
//
// rc.runner IS in scope in buildCodexRoutedLaunchSpec (it is a claudeRunCtx), so
// unlike the cognition-gate this one IS a near-one-line swap to WriteAgentTaskVia
// — but it lives in the SPEC-runner family (hk-3sus / hk-r36v), not the
// SUBSTRATE-spawn family this test file pins, so the fix is deliberately left to
// hk-r36v to keep this change scoped. Skip-documented here so the audit finding is
// not lost; the codex routed spec also has no separate substrate-spawn call site
// (it reaches newPerRunSubstrate via the SAME review-loop/DOT sites already
// covered, which ARE correctly threaded).
func TestCodexRoutedSpecWriteAgentTaskBoxALeak_hkr36v(t *testing.T) {
	t.Skip("hk-r36v: harnessregistry.go:147 buildCodexRoutedLaunchSpec uses the " +
		"box-A-local workspace.WriteAgentTask instead of WriteAgentTaskVia(rc.runner) " +
		"(cf. claudelaunchspec.go:329) — a SPEC-runner leak (hk-3sus family), not a " +
		"newPerRunSubstrate site. rc.runner is in scope so it is a near-one-line fix, " +
		"but it belongs to hk-r36v to keep the substrate-spawn family scoped. The " +
		"codex routed spec has NO separate substrate-spawn site (it reuses the " +
		"already-covered review-loop/DOT sites).")
}

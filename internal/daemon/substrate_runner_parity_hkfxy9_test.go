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
//   dot_gate.go:325                 DOT cognition-gate          nil       LEAK   (hk-9fe2, latent)
//   crewstart.go:247                crew test-double fallback   nil       N/A    (not an agent run)
//
// The two LIVE-fixed paths already have non-nil sentinel assertions in
// launch_substrate_runner_threading_hkfxy9_test.go. This file documents the
// remaining four. Each is a t.Skip with the precise reason it is NOT a passing
// non-nil assertion here — either it is correctly nil by design, it is a confirmed
// latent leak not yet fixed (so a non-nil assert would be wrong), or it is only
// reachable through real-SSH / heavy-fixture machinery that has no pure-local seam.
//
// The skips are intentional regression DOCUMENTATION, not stubs: when a leak is
// closed (hk-9fe2) or a local seam is added for the single-mode/reviewer paths,
// the matching Skip should be replaced with the live assertion described in its body.

import (
	"testing"

	"github.com/gregberns/harmonik/internal/daemon"
	tmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// hkfxy9ParityObserver installs the shared substrate-runner observer and returns
// the capture channel + a cleanup-registering helper, mirroring the setup used by
// TestReviewLoopThreadsRunnerIntoSubstrate_hkfxy9. Kept here so a future un-skip
// can drop straight into the existing idiom without re-deriving the seam.
//
//nolint:unused // retained for the un-skip path; see per-test bodies below.
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

// TestDotCognitionGateSubstrateRunnerLeak_hk9fe2 documents the CONFIRMED latent
// leak at dot_gate.go:325: executeCognitionGate calls
// newPerRunSubstrate(deps.substrate, deps.handlerBinary, nil) with a HARDCODED nil
// runner. For a REMOTE DOT run whose default graph used a cognition gate, the gate
// agent's claude PROCESS would spawn against box A's tmux/-default session, not the
// worker — the same wedge class as hk-fxy9 / hk-538l, but for the gate node.
//
// Per the chani-rs-integ task guidance, a CONFIRMED leak gets a documented skip,
// NOT a passing non-nil assertion (which would fail today). The leak is NOT fixed
// in this test family because it is NOT a one-line runner pass-through: the
// in-code TODO(hk-538l) at dot_gate.go:317-324 spells out that the runner is not
// even in scope — neither dispatchDotGateNode (dot_gate.go:90) nor
// executeCognitionGate (dot_gate.go:252) takes a `runner` parameter, and making
// the path remote-correct also requires threading the worker binary path,
// worker session name/cwd, and making os.Remove + writeCognitionGateTask
// runner-aware. That is multi-function plumbing = scope-creep; it belongs to
// hk-9fe2, not here.
//
// LATENCY NOTE (why no live run hits it today): the default workflow.dot uses a
// tool-command commit_gate, not a cognition gate, so no production remote run
// exercises dot_gate.go:325 — hence "latent". When hk-9fe2 threads the runner,
// replace this Skip with: drive ExportedDriveDotWorkflowWithRunner over a graph
// whose start node is a cognition-gate node (with a cpRegistry holding a
// Cognition-mode Gate ControlPoint) and assert the captured runner is the
// hk3susFakeRunner sentinel, mirroring TestDotThreadsRunnerIntoSubstrate_hkfxy9.
func TestDotCognitionGateSubstrateRunnerLeak_hk9fe2(t *testing.T) {
	t.Skip("hk-9fe2: dot_gate.go:325 cognition-gate newPerRunSubstrate is passed a " +
		"HARDCODED nil — confirmed latent box-A leak (no `runner` in scope; the " +
		"TODO(hk-538l) at dot_gate.go:317-324 documents the multi-function fix). " +
		"Latent: the default workflow.dot uses a mechanism commit_gate, so no live " +
		"remote run hits it. Flip to a non-nil sentinel assert once hk-9fe2 threads " +
		"the runner through dispatchDotGateNode → executeCognitionGate.")
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

package daemon_test

// dot_gate_base_sha_hkt1t00_test.go — regression tests for HK_GATE_BASE_SHA
// injection into shell gate nodes (hk-t1t00).
//
// # Problem reproduced
//
// On a remote worker whose refs/remotes/origin/main lags real main, the
// commit_gate shell node runs scripts/scenario-gate.sh, which falls back to
// `git merge-base origin/main HEAD` to compute the diff base. A stale
// origin/main produces a merge-base far behind the run's actual branch-point,
// expanding the affected-set to hundreds of files → the full test suite
// exceeds the 900s gate timeout → daemon SIGKILLs the gate →
// failure_class=transient self-loop → traversal cap → cap_hit.
//
// # Fix (hk-t1t00)
//
// driveDotWorkflow now injects HK_GATE_BASE_SHA=parentSHA into the env passed
// to dispatchDotToolNode for every shell tool node. scripts/scenario-gate.sh
// already honours HK_GATE_BASE_SHA (resolve_base() checks it first), so it
// uses the run's own branch-point as the diff base regardless of the worker's
// ref freshness. The affected-set stays bounded to what this bead actually
// changed.
//
// # Tests (L1-style — no real SSH, no LLM)
//
// T1 (LOCAL run — nil runner): driveDotWorkflow passes HK_GATE_BASE_SHA to
// the shell node via cmd.Env; a tool_command that asserts the value exits 0.
//
// T2 (REMOTE run — RecordingRunner): driveDotWorkflow inlines
// HK_GATE_BASE_SHA as an export statement in the login-shell argv (the remote
// env-passing path from hk-230h); the RecordingRunner executes it locally,
// confirming the value is present.
//
// Bead: hk-t1t00. Spec ref: specs/remote-substrate.md (scenario-gate
// affected-set correctness).

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	tmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// hkt1t00ParentSHAFixtureDeps builds the raw WorkLoopDepsParams for the gate-base-sha
// tests. Only the fields required for driveDotWorkflow shell-node dispatch are
// populated (no real daemon, no real binary, no merger mu needed).
func hkt1t00ParentSHAFixtureParams(t *testing.T) daemon.WorkLoopDepsParams {
	t.Helper()
	bus := &stubEventCollector{}
	return toolNodeFixtureDeps(t, bus)
}

// hkt1t00RunID generates a fresh RunID for each test call.
func hkt1t00RunID(t *testing.T) core.RunID {
	t.Helper()
	u, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("hkt1t00RunID: uuid.NewV7: %v", err)
	}
	return core.RunID(u)
}

// hkt1t00GateCheckDOT returns a minimal DOT workflow graph whose sole shell
// tool node checks that HK_GATE_BASE_SHA == wantSHA via printenv (avoids the
// DOT parser's rejection of "$" in attribute values). Exit 0 on match (gate
// SUCCESS), exit 1 on mismatch (gate FAIL → test should detect the FAIL).
func hkt1t00GateCheckDOT(wantSHA string) string {
	// Use `printenv | grep -qxF <sha>` rather than `test "$VAR" = "<sha>"`
	// because the DOT attribute parser rejects '$' in quoted attribute values.
	// printenv outputs all env vars as KEY=VALUE lines; grep -qxF matches
	// the exact line "HK_GATE_BASE_SHA=<wantSHA>" (case-sensitive, no regex).
	cmd := `printenv | grep -qxF HK_GATE_BASE_SHA=` + wantSHA
	return toolNodeDOT("hkt1t00-gate-check", cmd, "30")
}

// ─────────────────────────────────────────────────────────────────────────────
// T1: LOCAL run — HK_GATE_BASE_SHA=parentSHA is visible in shell tool_command
// ─────────────────────────────────────────────────────────────────────────────

// TestDriveDotWorkflow_GateBaseSHA_Local verifies that driveDotWorkflow
// (LOCAL path, nil runner) injects HK_GATE_BASE_SHA=parentSHA into the shell
// tool node's environment. A tool_command that asserts the value must exit 0
// (SUCCESS). Before the fix the env var was absent and the assertion would
// exit 1 (FAIL+deterministic).
func TestDriveDotWorkflow_GateBaseSHA_Local(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	t.Parallel()

	const parentSHA = "abc1230000badc0ffee00000"
	deps := daemon.ExportedWorkLoopDeps(hkt1t00ParentSHAFixtureParams(t))
	graph := toolNodeFixtureGraph(t, hkt1t00GateCheckDOT(parentSHA))

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()

	result := daemon.ExportedDriveDotWorkflow(
		ctx, deps,
		hkt1t00RunID(t),
		core.BeadID("hk-t1t00-local"),
		t.TempDir(), parentSHA,
		graph,
	)

	if !result.Success {
		t.Errorf("T1: HK_GATE_BASE_SHA not set to parentSHA in LOCAL gate shell; "+
			"summary=%q — driveDotWorkflow must inject HK_GATE_BASE_SHA=parentSHA "+
			"into gateEnv before calling dispatchDotToolNode (hk-t1t00)", result.Summary)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// T2: REMOTE run — HK_GATE_BASE_SHA=parentSHA is visible via export inlining
// ─────────────────────────────────────────────────────────────────────────────

// TestDriveDotWorkflow_GateBaseSHA_Remote verifies that driveDotWorkflow
// (REMOTE path, RecordingRunner) inlines HK_GATE_BASE_SHA=parentSHA as an
// export statement in the login-shell argv dispatched to the worker (the
// remote env-passing path from hk-230h). RecordingRunner with nil CmdFunc
// executes the command locally, so the assertion is observable.
//
// This is the regression path for the stale-origin/main incident: a remote
// worker with a stale ref no longer expands the diff to hundreds of files
// because HK_GATE_BASE_SHA pins the base to the exact branch-point.
func TestDriveDotWorkflow_GateBaseSHA_Remote(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	t.Parallel()

	const parentSHA = "def4560000badc0ffee11111"
	deps := daemon.ExportedWorkLoopDeps(hkt1t00ParentSHAFixtureParams(t))
	graph := toolNodeFixtureGraph(t, hkt1t00GateCheckDOT(parentSHA))

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()

	// RecordingRunner with nil CmdFunc runs commands locally via
	// exec.CommandContext — it is NOT nil, so runnerIsLocalFS returns false
	// and dispatchDotToolNode takes the REMOTE branch (export inlining).
	rr := &tmux.RecordingRunner{}

	result := daemon.ExportedDriveDotWorkflowWithRunner(
		ctx, deps,
		hkt1t00RunID(t),
		core.BeadID("hk-t1t00-remote"),
		"", "",
		t.TempDir(), parentSHA,
		graph,
		rr,
	)

	if !result.Success {
		t.Errorf("T2: HK_GATE_BASE_SHA not set to parentSHA in REMOTE gate shell; "+
			"summary=%q — driveDotWorkflow must inject HK_GATE_BASE_SHA=parentSHA "+
			"into gateEnv; dispatchDotToolNode must inline it as export in the "+
			"remote login-shell argv (hk-t1t00 / hk-230h)", result.Summary)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// T3: empty parentSHA — HK_GATE_BASE_SHA is NOT injected (no regression)
// ─────────────────────────────────────────────────────────────────────────────

// TestDriveDotWorkflow_GateBaseSHA_EmptyParentSHA verifies that when
// parentSHA is "" (e.g. an edge-case bead with no branch-point), driveDotWorkflow
// does NOT inject HK_GATE_BASE_SHA into the gate's argv. Uses a RecordingRunner
// to inspect the argv rather than executing a shell command, sidestepping the
// DOT parser's rejection of "$" in attribute values.
func TestDriveDotWorkflow_GateBaseSHA_EmptyParentSHA(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	t.Parallel()

	deps := daemon.ExportedWorkLoopDeps(hkt1t00ParentSHAFixtureParams(t))
	// "exit 0" is a safe placeholder; we check the argv, not the exit code.
	graph := toolNodeFixtureGraph(t, toolNodeDOT("hkt1t00-no-sha", "exit 0", "30"))
	rr := &tmux.RecordingRunner{}

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()

	daemon.ExportedDriveDotWorkflowWithRunner(
		ctx, deps,
		hkt1t00RunID(t),
		core.BeadID("hk-t1t00-empty"),
		"", "",
		t.TempDir(), "", // empty parentSHA
		graph,
		rr,
	)

	// Inspect the recorded argv: HK_GATE_BASE_SHA must NOT appear when
	// parentSHA is "". An empty value injected as `export HK_GATE_BASE_SHA=;`
	// would break scenario-gate.sh's resolve_base (the script validates the
	// SHA with git rev-parse and warns on failure, losing the correct base).
	for _, call := range rr.Calls {
		for _, arg := range call.Args {
			if strings.Contains(arg, "HK_GATE_BASE_SHA") {
				t.Errorf("T3: HK_GATE_BASE_SHA appears in argv despite empty parentSHA; "+
					"arg=%q — driveDotWorkflow must NOT inject HK_GATE_BASE_SHA "+
					"when parentSHA == \"\" (hk-t1t00)", arg)
			}
		}
	}
}

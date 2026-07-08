//go:build scenario

// TestScenario_MergeRace_ST5 is the end-to-end proof of the Layer-1 agent seam:
// a two-node DOT workflow (alpha_node → beta_node) runs to run_completed using
// only harmonik-twin-claude — ZERO Claude API tokens. Bead: hk-psrnc.
//
// Design:
//   - WorkflowModeDot + projectDir/workflow.dot (two agentic nodes, sequential MVH).
//   - Each node runs the same YAML twin script: commit_on_cue writes
//     merge-race-sentinel.txt. Alpha commits first; beta commits a second revision
//     (same filename, different timestamp-derived content). Both commits land on the
//     run branch; daemon squash-merges into main.
//   - The daemon is started via daemon.Start in-process (never the fleet daemon).
//   - Uses real br binary (codex lifecycle pattern) for bead lifecycle coverage.
//   - Assertions: run_completed, agent_ready present (twin was invoked),
//     bead closed, zero budget_accrual events (structural: twin never calls Claude API).
//
// Spec ref: specs/scenario-harness.md §4 (fixture lifecycle, twin substitution).
// Bead: hk-psrnc.
package scenario

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// ─────────────────────────────────────────────────────────────────────────────
// Fixtures (mergeRaceST5 prefix — bead hk-psrnc)
// ─────────────────────────────────────────────────────────────────────────────

// mergeRaceST5FixtureWorkflowDot writes the two-node DOT workflow to
// <projectDir>/workflow.dot. The daemon reads it from this location when
// WorkflowModeDot is set and no per-bead workflow_ref label is present.
func mergeRaceST5FixtureWorkflowDot(t *testing.T, projectDir string) {
	t.Helper()
	content := `digraph "merge-race-two-node" {
    schema_version="1"; version="1.0"; workflow_id="merge-race-two-node";
    start_node="alpha_node"; terminal_node_ids="beta_node";
    alpha_node [type="agentic", agent_type="claude-twin", handler_ref="twins/claude-twin", idempotency_class="non-idempotent", role="winner: commits merge-race-sentinel.txt first"];
    beta_node  [type="agentic", agent_type="claude-twin", handler_ref="twins/claude-twin", idempotency_class="non-idempotent", role="loser: commits revised merge-race-sentinel.txt in shared worktree"];
    alpha_node -> beta_node;
}
`
	dotPath := filepath.Join(projectDir, "workflow.dot")
	if err := os.WriteFile(dotPath, []byte(content), 0o644); err != nil {
		t.Fatalf("mergeRaceST5FixtureWorkflowDot: WriteFile: %v", err)
	}
}

// mergeRaceST5FixtureAlphaScript writes the twin YAML script for both DOT
// nodes and returns its absolute path. The same script drives alpha_node and
// beta_node because (a) sequential MVH shares one worktree and (b)
// HARMONIK_NODE_ID is "bead/<id>", never the DOT node name, so per-node
// routing via env is impossible. Both nodes commit merge-race-sentinel.txt;
// alpha's content is commit-on-cue <ts1>, beta's is <ts2> (different ts →
// different file content → second commit succeeds).
func mergeRaceST5FixtureAlphaScript(t *testing.T) string {
	t.Helper()
	const scriptYAML = `heartbeat_mode: scripted
messages:
  - type: handler_capabilities
    payload:
      run_id: "run-hkpsrnc-mr-001"
      session_id: "sess-hkpsrnc-mr-001"
      protocol_versions_supported: [1]
    relative_timestamp_ms: 0
  - type: agent_ready
    payload:
      run_id: "run-hkpsrnc-mr-001"
      session_id: "sess-hkpsrnc-mr-001"
      capabilities: ["scripted", "commit_on_cue"]
    relative_timestamp_ms: 10
  - type: agent_output_chunk
    payload:
      run_id: "run-hkpsrnc-mr-001"
      session_id: "sess-hkpsrnc-mr-001"
      chunk_index: 0
      bytes_emitted: 64
    relative_timestamp_ms: 5
  - type: commit_on_cue
    payload:
      sentinel_name: "merge-race-sentinel.txt"
    relative_timestamp_ms: 0
  - type: outcome_emitted
    payload:
      run_id: "run-hkpsrnc-mr-001"
      session_id: "sess-hkpsrnc-mr-001"
      node_id: "alpha_node"
      outcome_status: "WORK_COMPLETE"
    relative_timestamp_ms: 5
  - type: agent_completed
    payload:
      run_id: "run-hkpsrnc-mr-001"
      session_id: "sess-hkpsrnc-mr-001"
      ended_at: "2026-01-01T00:00:00Z"
      exit_code: 0
      outcome_ref: "run-hkpsrnc-mr-001/outcome"
    relative_timestamp_ms: 0
`
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "alpha.yaml")
	if err := os.WriteFile(scriptPath, []byte(scriptYAML), 0o644); err != nil {
		t.Fatalf("mergeRaceST5FixtureAlphaScript: WriteFile: %v", err)
	}
	return scriptPath
}

// mergeRaceST5FixtureHandlerScript writes the wrapper shell script that
// invokes harmonik-twin-claude with --script-path and --worktree-path.
// The daemon-generated args (--session-id, --model, etc.) are intentionally
// NOT forwarded: the twin parses only its own flags and rejects unknown ones.
func mergeRaceST5FixtureHandlerScript(t *testing.T, twinBin, alphaScriptPath string) string {
	t.Helper()
	twinEsc := strings.ReplaceAll(twinBin, "'", "'\\''")
	alphaEsc := strings.ReplaceAll(alphaScriptPath, "'", "'\\''")
	script := fmt.Sprintf(`#!/bin/sh
set -e
WS="${HARMONIK_WORKSPACE_PATH:-$(pwd)}"
exec '%s' \
    --script-path='%s' \
    --worktree-path="${WS}"
`, twinEsc, alphaEsc)
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "merge_race_handler.sh")
	//nolint:gosec // G306: test-only script; chmod 0755 required for execution
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("mergeRaceST5FixtureHandlerScript: WriteFile: %v", err)
	}
	return scriptPath
}

// ─────────────────────────────────────────────────────────────────────────────
// TestScenario_MergeRace_ST5
// ─────────────────────────────────────────────────────────────────────────────

// TestScenario_MergeRace_ST5 is the end-to-end proof of the Layer-1 agent seam
// (bead hk-psrnc). Assertions:
//
//  1. run_completed event emitted (not run_failed).
//  2. agent_ready present in JSONL (proves harmonik-twin-claude was invoked).
//  3. Bead closed after run_completed.
//  4. No token-basis budget_accrual events (structural: twin never calls Claude API;
//     output_bytes accruals from agent_output_chunk are expected and excluded).
func TestScenario_MergeRace_ST5(t *testing.T) {
	if twinBinaryPath == "" {
		t.Skip("harmonik-twin-claude binary not built; skipping merge-race ST5")
	}

	realBrPath := codexLifecycleFixtureBrPath(t)
	projectDir, jsonlPath := codexLifecycleFixtureProjectDir(t)
	codexLifecycleFixtureGitRepo(t, projectDir)
	mergeRaceST5FixtureWorkflowDot(t, projectDir)

	alphaScript := mergeRaceST5FixtureAlphaScript(t)
	handlerScript := mergeRaceST5FixtureHandlerScript(t, twinBinaryPath, alphaScript)

	dbPath := filepath.Join(projectDir, ".beads", "beads.db")
	brWrapper := codexLifecycleFixtureBrWrapperScript(t, realBrPath, dbPath)
	beadID := codexLifecycleFixtureInitBr(t, realBrPath, projectDir, brWrapper)

	cfg := daemon.Config{
		ProjectDir:                 projectDir,
		JSONLLogPath:               jsonlPath,
		BrPath:                     brWrapper,
		HandlerBinary:              handlerScript,
		WorkflowModeDefault:        core.WorkflowModeDot,
		HandlerEnv:                 os.Environ(),
		SkipWALCheckpoint:          true,
		SkipBrHistoryRotation:      true,
		SkipRestartBackoff:         true,
		SkipBeadsMergeDriverConfig: true,
	}

	cancel, daemonDone := scenarioFixtureStartDaemon(t, cfg)

	// Wait for a terminal event (run_completed or run_failed).
	const terminalBudget = 120 * time.Second
	gotTerminal := scenarioCheckRunCompleted(t, jsonlPath, terminalBudget)

	// Allow bead close to propagate before shutting the daemon down.
	if gotTerminal {
		codexLifecycleFixturePollBeadClosed(t, brWrapper, beadID, 10*time.Second)
	}

	cancel()
	scenarioFixtureWaitDaemon(t, daemonDone, 10*time.Second)

	// ── Assertions ──────────────────────────────────────────────────────────

	lines := scenarioFixtureReadJSONLLines(t, jsonlPath)

	if !gotTerminal {
		t.Errorf("timed out waiting for run_completed/run_failed (budget=%s); JSONL:\n%s",
			terminalBudget, strings.Join(lines, "\n"))
		return
	}

	// 1. Must be run_completed, not run_failed.
	foundCompleted := false
	for _, line := range lines {
		if strings.Contains(line, string(core.EventTypeRunCompleted)) {
			foundCompleted = true
			break
		}
	}
	if !foundCompleted {
		t.Errorf("run_completed not found (run_failed?); JSONL:\n%s", strings.Join(lines, "\n"))
	}

	// 2. agent_ready must appear — proves the twin binary was actually invoked.
	agentReadyFound := false
	for _, line := range lines {
		if strings.Contains(line, "agent_ready") {
			agentReadyFound = true
			break
		}
	}
	if !agentReadyFound {
		t.Errorf("agent_ready not found in JSONL (twin never invoked?); JSONL:\n%s",
			strings.Join(lines, "\n"))
	}

	// 3. Bead must be closed.
	if !codexLifecycleFixturePollBeadClosed(t, brWrapper, beadID, 2*time.Second) {
		t.Errorf("bead %s not closed after run_completed", beadID)
	}

	// 4. Zero Claude API tokens: the watcher always emits budget_accrual with
	// cost_basis:"output_bytes" for agent_output_chunk events — that is expected
	// and NOT an API-token charge. Token-basis accruals (any cost_basis other
	// than "output_bytes") would indicate Claude API usage; none should appear
	// when the twin is the sole handler.
	for _, line := range lines {
		if strings.Contains(line, "budget_accrual") && !strings.Contains(line, `"output_bytes"`) {
			t.Errorf("token-basis budget_accrual found — Claude API unexpectedly invoked; line: %s", line)
			break
		}
	}

	t.Logf("PASS beadID=%s foundCompleted=%v agentReadyFound=%v lines=%d",
		beadID, foundCompleted, agentReadyFound, len(lines))
}

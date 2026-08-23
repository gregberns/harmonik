//go:build scenario

package scenario

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/workspace"
)

// TestScenario_Fix1_TrustEnvOverride verifies that workspace.EnsureWorktreeTrust
// respects the HARMONIK_CLAUDE_CONFIG_PATH environment variable so tests (and the
// daemon under test) never touch the real ~/.claude.json.
//
// The trust-lookup fix (hk-lj1p9.3) added priority-1 env-var override to
// defaultClaudeGlobalConfigPath: if HARMONIK_CLAUDE_CONFIG_PATH is set, the
// function returns that path verbatim, allowing test isolation without modifying
// the user's global Claude config.
//
// Without this fix the daemon would consult ~/.claude.json for each worktree's
// hasTrustDialogAccepted flag. If the entry is absent, the interactive trust
// prompt would block the session and HC-056 (agent_ready timeout) would fire.
//
// Assertions:
//  1. EnsureWorktreeTrust with HARMONIK_CLAUDE_CONFIG_PATH set writes to the
//     overridden path, not to ~/.claude.json.
//  2. The written file contains the worktree path as a trust entry.
//  3. A second call (idempotent) returns nil without re-writing.
func TestScenario_Fix1_TrustEnvOverride(t *testing.T) {
	tmpDir := t.TempDir()
	fakeConfigPath := filepath.Join(tmpDir, ".claude.json")
	worktreePath := t.TempDir()

	t.Setenv("HARMONIK_CLAUDE_CONFIG_PATH", fakeConfigPath)

	if err := workspace.EnsureWorktreeTrust(worktreePath); err != nil {
		t.Fatalf("Fix1: EnsureWorktreeTrust first call failed: %v", err)
	}

	if _, err := os.Stat(fakeConfigPath); err != nil {
		t.Fatalf("Fix1: expected fake config at %s to exist after EnsureWorktreeTrust, got: %v",
			fakeConfigPath, err)
	}

	// Assert: the trust entry is present in the fake config.
	//nolint:gosec // G304: fakeConfigPath is t.TempDir()-based; not user input
	data, err := os.ReadFile(fakeConfigPath)
	if err != nil {
		t.Fatalf("Fix1: ReadFile %s: %v", fakeConfigPath, err)
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("Fix1: unmarshal config: %v", err)
	}

	canonicalPath := worktreePath
	if resolved, err := filepath.EvalSymlinks(worktreePath); err == nil {
		canonicalPath = resolved
	}

	projects, ok := cfg["projects"].(map[string]interface{})
	if !ok {
		t.Fatalf("Fix1: config.projects field missing or wrong type; got: %v", cfg["projects"])
	}
	entry, ok := projects[canonicalPath].(map[string]interface{})
	if !ok {
		t.Fatalf("Fix1: config.projects[%q] missing; projects keys: %v",
			canonicalPath, scenarioFixtureMapKeys(projects))
	}
	trusted, _ := entry["hasTrustDialogAccepted"].(bool)
	if !trusted {
		t.Errorf("Fix1: hasTrustDialogAccepted is not true for path %q", canonicalPath)
	}

	if err := workspace.EnsureWorktreeTrust(worktreePath); err != nil {
		t.Errorf("Fix1: EnsureWorktreeTrust second call (idempotent) failed: %v", err)
	}

	t.Logf("Fix1 PASS: trust entry written to %s for path %q", fakeConfigPath, canonicalPath)
}

func scenarioFixtureMapKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// TestScenario_Fix2_SocketBoundBeforeTwin verifies that daemon.Start binds the
// Unix-domain socket at <ProjectDir>/.harmonik/daemon.sock BEFORE the work loop
// could dispatch a handler (twin) subprocess.
//
// The fix (hk-tjl40) wired RunSocketListener into daemon.Start so that
// hook-relay subprocesses (twin or real claude) can dial the daemon socket to
// deliver outcome_emitted envelopes. Before the fix, the socket was never bound,
// so any hook-relay dial attempt would fail (connection refused), the relay would
// exit non-zero, agent_failed would be emitted, and the bead would be reopened
// indefinitely.
//
// In the twin scenario: the twin emits agent_ready on its stdout, which is the
// handler.Watcher-readable path (CHB-022 twin-blind routing). The daemon socket
// is the hook-relay path (CHB-025), not the twin's NDJSON stdout path. The
// assertion here is weaker: the socket must exist with mode 0600 before
// the twin binary is launched (or shortly after daemon.Start, since BrPath="" skips
// the work loop and the twin is not actually dispatched in this scenario).
//
// The sequence:
//  1. daemon.Start called in a goroutine.
//  2. We poll for the socket file (mode 0600) within 5 seconds.
//  3. Socket found → assertion passes.
//  4. Context cancelled → daemon exits cleanly.
//
// Assertions:
//   - .harmonik/daemon.sock exists with mode 0o600 within 5s of Start.
func TestScenario_Fix2_SocketBoundBeforeTwin(t *testing.T) {
	t.Parallel()

	proj := scenarioFixtureProjectDir(t)

	cfg := daemon.Config{
		ProjectDir:          proj.projectDir,
		JSONLLogPath:        proj.jsonlPath,
		BrPath:              "", // no work loop — we only test socket binding
		WorkflowModeDefault: core.WorkflowModeDot,
	}

	cancel, done := scenarioFixtureStartDaemon(t, cfg)
	defer func() {
		cancel()
		scenarioFixtureWaitDaemon(t, done, 5*time.Second)
	}()

	const sockBudget = 5 * time.Second
	sockFound := scenarioFixturePollSocket(proj.sockPath, sockBudget)

	cancel() // stop daemon before assertion to avoid leaks

	if !sockFound {
		t.Errorf("Fix2: socket not found at %q with mode 0600 within %s; daemon.Start must bind socket (hk-tjl40)",
			proj.sockPath, sockBudget)
	} else {
		t.Logf("Fix2 PASS: socket at %q bound within %s", proj.sockPath, sockBudget)
	}
}

// TestScenario_Fix3_WaitUnblocksAfterTwinExit verifies that handler.Session.Wait
// returns promptly when the handler subprocess exits (subprocess path, no tmux).
//
// The fix (hk-smuku) addressed tmuxSubstrateSession.Wait deadlocking when the
// tmux window could not be found by name: the old code re-queried the active
// pane PID (which could be the daemon's own window) and looped forever.
// The subprocess path exercises the same contract: sess.Wait MUST unblock once
// the subprocess exits, regardless of window name stability.
//
// This scenario uses handler.Launch directly (the same call-site as beadRunOne)
// with either the built twin binary (--scenario single-happy-path → exits 0) or
// a minimal shell script that exits 0 immediately. In both cases sess.Wait MUST
// return within a bounded time after the subprocess exits.
//
// Assertions:
//   - sess.Wait returns within 10s of handler exit.
//   - watcher.Done() is closed (event stream complete).
//   - No error from sess.Wait.
func TestScenario_Fix3_WaitUnblocksAfterTwinExit(t *testing.T) {
	t.Parallel()

	var binary string
	var args []string

	if twinBinaryPath != "" {
		binary = twinBinaryPath
		args = []string{"--scenario", "single-happy-path"}
	} else {
		scriptDir := t.TempDir()
		scriptPath := filepath.Join(scriptDir, "exit-zero.sh")
		//nolint:gosec // G306: script is test-only, chmod 0755 required for execution
		if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatalf("Fix3: write handler script: %v", err)
		}
		binary = "/bin/sh"
		args = []string{scriptPath}
	}

	pub := &handlercontract.CollectingEmitter{}
	dl := handlercontract.NoopWatcherDeadLetter{}
	reg := handlercontract.NewAdapterRegistry()
	h := handler.NewHandler(pub, dl, reg)

	spec := handler.LaunchSpec{
		Binary:  binary,
		Args:    args,
		Env:     []string{},
		WorkDir: t.TempDir(),
		Role:    "implementer",
	}

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	sess, watcher, err := h.Launch(ctx, spec)
	if err != nil {
		t.Fatalf("Fix3: handler.Launch failed: %v", err)
	}

	select {
	case <-watcher.Done():
	case <-ctx.Done():
		t.Fatalf("Fix3: context done before watcher finished: %v", ctx.Err())
	}

	waitDone := make(chan error, 1)
	go func() { waitDone <- sess.Wait(ctx) }()

	select {
	case waitErr := <-waitDone:
		if waitErr != nil {
			t.Errorf("Fix3: sess.Wait returned non-nil error: %v", waitErr)
		} else {
			t.Logf("Fix3 PASS: sess.Wait unblocked cleanly after subprocess exit (hk-smuku)")
		}
	case <-time.After(10 * time.Second):
		t.Error("Fix3 FAIL: sess.Wait did not return within 10s after subprocess exit; " +
			"stored-PID wait fix (hk-smuku) may not be effective on the subprocess path")
	}

	_ = pub.EventTypes() // satisfy import
}

// TestScenario_Fix6_EvalSymlinksTrustLookup verifies that workspace.EnsureWorktreeTrust
// resolves symlinks before writing the trust entry, so the key matches what Claude
// Code stores after its own realpath() normalisation (e.g. /var/folders →
// /private/var/folders on macOS).
//
// The fix (hk-o5eww) added filepath.EvalSymlinks(worktreePath) at the top of
// EnsureWorktreeTrust: if EvalSymlinks returns a different path, the resolved
// path is used as the trust-entry key.
//
// Without this fix: the trust entry would be stored under the symlink path
// (e.g., /tmp/symlink → /private/tmp/target) and Claude Code would look up the
// canonical path (/private/tmp/target), find no entry, show the interactive
// trust dialog, block the session, and trigger HC-056.
//
// Setup: create a real directory and a symlink pointing to it. Call
// EnsureWorktreeTrust with the symlink path. Assert the trust entry is stored
// under the RESOLVED path, not the symlink path.
//
// Assertions:
//   - Trust entry key = resolved path (via EvalSymlinks).
//   - Trust entry key ≠ symlink path (unless they happen to resolve to the same).
func TestScenario_Fix6_EvalSymlinksTrustLookup(t *testing.T) {
	tmpDir := t.TempDir()
	fakeConfigPath := filepath.Join(tmpDir, ".claude.json")

	realDir := filepath.Join(tmpDir, "real-worktree")
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatalf("Fix6: MkdirAll realDir: %v", err)
	}
	symlinkDir := filepath.Join(tmpDir, "symlink-worktree")
	if err := os.Symlink(realDir, symlinkDir); err != nil {
		t.Skipf("Fix6: Symlink creation failed (may be a CI sandbox restriction): %v", err)
	}

	t.Setenv("HARMONIK_CLAUDE_CONFIG_PATH", fakeConfigPath)

	if err := workspace.EnsureWorktreeTrust(symlinkDir); err != nil {
		t.Fatalf("Fix6: EnsureWorktreeTrust with symlink path failed: %v", err)
	}

	// Read the written config.
	//nolint:gosec // G304: fakeConfigPath is t.TempDir()-based; not user input
	data, err := os.ReadFile(fakeConfigPath)
	if err != nil {
		t.Fatalf("Fix6: ReadFile %s: %v", fakeConfigPath, err)
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("Fix6: unmarshal config: %v", err)
	}

	projects, ok := cfg["projects"].(map[string]interface{})
	if !ok {
		t.Fatalf("Fix6: config.projects missing or wrong type; got: %v", cfg["projects"])
	}

	resolvedDir, evalErr := filepath.EvalSymlinks(symlinkDir)
	if evalErr != nil {
		t.Fatalf("Fix6: EvalSymlinks test-side call failed: %v", evalErr)
	}

	t.Logf("Fix6: symlink=%q resolved=%q projects keys=%v", symlinkDir, resolvedDir, scenarioFixtureMapKeys(projects))

	entry, ok := projects[resolvedDir].(map[string]interface{})
	if !ok {
		t.Errorf("Fix6 FAIL: trust entry not found under resolved path %q; projects keys=%v (hk-o5eww)",
			resolvedDir, scenarioFixtureMapKeys(projects))
		return
	}
	trusted, _ := entry["hasTrustDialogAccepted"].(bool)
	if !trusted {
		t.Errorf("Fix6: hasTrustDialogAccepted is not true under resolved path %q", resolvedDir)
	}

	if symlinkDir != resolvedDir {
		if _, foundUnderSymlink := projects[symlinkDir]; foundUnderSymlink {
			t.Errorf("Fix6 FAIL: trust entry also written under symlink path %q; should only be under resolved path %q",
				symlinkDir, resolvedDir)
		}
	}

	t.Logf("Fix6 PASS: trust entry under resolved path %q", resolvedDir)
}

// TestScenario_Fix10_AgentTaskSessionCompletion verifies that workspace.WriteAgentTask
// materialises the ## Session Completion section in agent-task.md.
//
// The fix (hk-cmybm layer 1) added buildAgentTaskContent's "## Session Completion"
// section with the /quit instruction. Before the fix, claude would complete work but
// the Stop hook would never fire (interactive TUI Stop fires on session exit, not after
// each response). Without /quit in the task file, the daemon's workloop would block
// on sess.Wait() indefinitely.
//
// The ## Session Completion section instructs claude to type /quit as its final action
// after committing all work, triggering the Stop hook → outcome_emitted envelope →
// workloop unblocks.
//
// Setup: call WriteAgentTask with a minimal payload. Read the written file. Assert
// the ## Session Completion section header and the /quit instruction are present.
//
// Assertions:
//  1. agent-task.md is created at the canonical path.
//  2. File contains "## Session Completion".
//  3. File contains "/quit" instruction.
//  4. File contains the bead body (## Task Description section).
func TestScenario_Fix10_AgentTaskSessionCompletion(t *testing.T) {
	t.Parallel()

	workspacePath := t.TempDir()

	payload := workspace.AgentTaskPayload{
		BeadID:        "scenario-fix10-bead",
		Title:         "Scenario Fix 10 Test Bead",
		Phase:         "implementer-initial",
		Iteration:     1,
		RunID:         "scenario-fix10-run-001",
		WorkspacePath: workspacePath,
		Body:          "Write a test file to verify agent-task.md materialization.",
		// Stated rather than left to the zero value: the /quit instruction is
		// claude's because claude is the REPL, and the section is now rendered
		// from this field (hk-quit-instruction-not-portable-ms55w). A test that
		// leans on the default would keep passing if the default changed.
		Completion: handlercontract.CompletionEventStreamThenQuit,
	}

	if err := workspace.WriteAgentTask(workspacePath, payload); err != nil {
		t.Fatalf("Fix10: WriteAgentTask failed: %v", err)
	}

	taskPath := workspace.AgentTaskPath(workspacePath)
	//nolint:gosec // G304: taskPath constructed from workspacePath (t.TempDir()) + known suffix
	data, err := os.ReadFile(taskPath)
	if err != nil {
		t.Fatalf("Fix10: ReadFile %s: %v", taskPath, err)
	}
	content := string(data)
	t.Logf("Fix10: agent-task.md content (%d bytes):\n%s", len(data), content)

	if !strings.Contains(content, "## Session Completion") {
		t.Errorf("Fix10 FAIL: agent-task.md does not contain '## Session Completion' section (hk-cmybm)")
	}

	if !strings.Contains(content, "/quit") {
		t.Errorf("Fix10 FAIL: agent-task.md does not contain '/quit' instruction; " +
			"claude will not exit the session and the workloop will block forever")
	}

	if !strings.Contains(content, payload.Body) {
		t.Errorf("Fix10 FAIL: agent-task.md does not contain the bead body (## Task Description)")
	}

	if !strings.Contains(content, "cannot detect") {
		t.Errorf("Fix10: agent-task.md does not contain 'cannot detect' explanation; " +
			"claude may not understand why /quit is mandatory")
	}

	t.Logf("Fix10 PASS: ## Session Completion section present with /quit instruction")
}

// TestScenario_TwinSmoke_SingleHappyPath is an additional scenario that verifies
// the built harmonik-twin-claude binary emits the expected event sequence when
// run directly (stdout-watcher topology, CHB-022).
//
// This scenario does not boot the daemon — it runs the twin directly and
// captures its NDJSON stdout, asserting the canonical single-happy-path sequence
// (handler_capabilities → agent_ready → agent_completed).
//
// The test is skipped when the twin binary could not be built (CI environments
// without the Go toolchain or with build failures).
//
// Assertions:
//  1. Twin exits 0.
//  2. NDJSON output contains "agent_ready" before "agent_completed".
//  3. "agent_completed" is the last known progress event.
func TestScenario_TwinSmoke_SingleHappyPath(t *testing.T) {
	t.Parallel()

	if twinBinaryPath == "" {
		t.Skip("TwinSmoke: harmonik-twin-claude binary not available (build failed or go toolchain absent)")
	}

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, twinBinaryPath, "--scenario", "single-happy-path") //nolint:gosec // twinBinaryPath from build
	var stdout strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = os.Stderr
	cmd.Env = []string{}
	cmd.Dir = t.TempDir()

	if err := cmd.Run(); err != nil {
		t.Fatalf("TwinSmoke: twin exited with error: %v", err)
	}

	gotTypes := scenarioParseNDJSONTypes(t, stdout.String())
	t.Logf("TwinSmoke: event types from twin: %v", gotTypes)

	readyIdx, completedIdx := -1, -1
	for i, et := range gotTypes {
		if et == "agent_ready" && readyIdx < 0 {
			readyIdx = i
		}
		if et == "agent_completed" && completedIdx < 0 {
			completedIdx = i
		}
	}

	if readyIdx < 0 {
		t.Errorf("TwinSmoke FAIL: agent_ready not emitted; got %v", gotTypes)
	}
	if completedIdx < 0 {
		t.Errorf("TwinSmoke FAIL: agent_completed not emitted; got %v", gotTypes)
	}
	if readyIdx >= 0 && completedIdx >= 0 && readyIdx >= completedIdx {
		t.Errorf("TwinSmoke FAIL: agent_ready (idx=%d) must precede agent_completed (idx=%d)", readyIdx, completedIdx)
	}

	if readyIdx >= 0 && completedIdx >= 0 && readyIdx < completedIdx {
		t.Logf("TwinSmoke PASS: agent_ready(idx=%d) → agent_completed(idx=%d)", readyIdx, completedIdx)
	}
}

func scenarioParseNDJSONTypes(t *testing.T, s string) []string {
	t.Helper()
	var types []string
	scanner := bufio.NewScanner(strings.NewReader(s))
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(line, &obj); err != nil {
			continue
		}
		var typStr string
		if raw, ok := obj["type"]; ok {
			_ = json.Unmarshal(raw, &typStr)
		}
		if typStr != "" {
			types = append(types, typStr)
		}
	}
	return types
}

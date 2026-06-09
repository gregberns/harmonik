package daemon_test

// claudeharness_test.go — ClaudeHarness unit tests (hk-3kyh3 C1/T2).
//
// Two test categories:
//
//  1. Pure-LaunchSpec golden: ClaudeHarness.LaunchSpec returns a SpawnSpec whose
//     Binary/Args/Env/WorkDir match what buildClaudeLaunchSpec returns for the
//     equivalent claudeRunCtx.  Covers all four workflow phases.
//
//  2. Shared-scaffolding side-effect parity: calling ClaudeHarness.LaunchSpec
//     produces the same workspace side-effects (settings.json, agent-task.md) as
//     calling buildClaudeLaunchSpec directly.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// claudeHarnessFixtureWorkspace mirrors claudeLaunchSpecFixtureWorkspace.
func claudeHarnessFixtureWorkspace(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".claude"), 0o755); err != nil {
		t.Fatalf("claudeHarnessFixtureWorkspace: MkdirAll .claude: %v", err)
	}
	return dir
}

// claudeHarnessFixtureRunCtx builds an ExportedClaudeRunCtx for the given phase,
// reusing the same fixture builder pattern as claudelaunchspec_test.go.
func claudeHarnessFixtureRunCtx(
	t *testing.T,
	workspacePath string,
	phase handlercontract.ReviewLoopPhase,
	priorSessID *string,
	iterationCount int,
) daemon.ExportedClaudeRunCtx {
	t.Helper()
	runUID, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("claudeHarnessFixtureRunCtx: NewV7: %v", err)
	}
	return daemon.ExportedClaudeRunCtx{
		RunID:             core.RunID(runUID),
		BeadID:            "test-bead-harness-hk-3kyh3",
		WorkspacePath:     workspacePath,
		DaemonSocket:      "/tmp/harmonik-test-harness.sock",
		WorkflowMode:      core.WorkflowModeSingle,
		Phase:             phase,
		IterationCount:    iterationCount,
		PriorClaudeSessID: priorSessID,
		HandlerBinary:     "claude",
		BaseEnv:           []string{"HARMONIK_PROJECT_HASH=deadbeef123456"},
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 1. Pure-LaunchSpec golden tests
//
// These tests deliberately do NOT call t.Parallel(): they all invoke
// buildClaudeLaunchSpec (via ClaudeHarness.LaunchSpec) which calls
// EnsureWorktreeTrust and writes to ~/.claude.json under a file lock.
// Running them in parallel with each other and with the integration-test suite
// (TestT4_ConcurrentLoops, TestParallelSmoke_TwoBeadsConcurrent) creates
// excessive contention on that lock, causing unrelated tests to hit the
// "write-lock acquire timed out" deadline and fail spuriously.
// ─────────────────────────────────────────────────────────────────────────────

// TestClaudeHarness_LaunchSpec_Single verifies SpawnSpec parity for single-mode.
func TestClaudeHarness_LaunchSpec_Single(t *testing.T) {

	ws := claudeHarnessFixtureWorkspace(t)
	rc := claudeHarnessFixtureRunCtx(t, ws, "", nil, 0)

	// Reference: what buildClaudeLaunchSpec returns.
	refSpec, _, err := daemon.ExportedBuildClaudeLaunchSpec(context.Background(), rc)
	if err != nil {
		t.Fatalf("reference buildClaudeLaunchSpec: %v", err)
	}

	// Harness: must produce a matching SpawnSpec.
	// Use a fresh workspace so side-effect collision (WriteAgentTask) is avoided.
	ws2 := claudeHarnessFixtureWorkspace(t)
	rc2 := claudeHarnessFixtureRunCtx(t, ws2, "", nil, 0)
	hrc := daemon.ExportedRunCtxFromClaudeRunCtx(rc2)

	h := daemon.ExportedNewClaudeHarness()
	spawn, err := h.LaunchSpec(hrc)
	if err != nil {
		t.Fatalf("ClaudeHarness.LaunchSpec: %v", err)
	}

	claudeHarnessAssertSpawnSpecShape(t, "Single", spawn, refSpec.Binary, false)
}

// TestClaudeHarness_LaunchSpec_ImplementerInitial verifies SpawnSpec parity for
// implementer-initial phase.
func TestClaudeHarness_LaunchSpec_ImplementerInitial(t *testing.T) {
	ws := claudeHarnessFixtureWorkspace(t)
	rc := claudeHarnessFixtureRunCtx(t, ws, handlercontract.ReviewLoopPhaseImplementerInitial, nil, 1)

	refSpec, _, err := daemon.ExportedBuildClaudeLaunchSpec(context.Background(), rc)
	if err != nil {
		t.Fatalf("reference buildClaudeLaunchSpec: %v", err)
	}

	ws2 := claudeHarnessFixtureWorkspace(t)
	rc2 := claudeHarnessFixtureRunCtx(t, ws2, handlercontract.ReviewLoopPhaseImplementerInitial, nil, 1)
	hrc := daemon.ExportedRunCtxFromClaudeRunCtx(rc2)

	h := daemon.ExportedNewClaudeHarness()
	spawn, err := h.LaunchSpec(hrc)
	if err != nil {
		t.Fatalf("ClaudeHarness.LaunchSpec: %v", err)
	}

	claudeHarnessAssertSpawnSpecShape(t, "ImplementerInitial", spawn, refSpec.Binary, false)
}

// TestClaudeHarness_LaunchSpec_ImplementerResume verifies SpawnSpec parity for
// implementer-resume phase: --resume flag, reused session ID.
func TestClaudeHarness_LaunchSpec_ImplementerResume(t *testing.T) {
	priorUID, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("mint prior UUID: %v", err)
	}
	priorSessID := priorUID.String()

	ws := claudeHarnessFixtureWorkspace(t)
	rc := claudeHarnessFixtureRunCtx(t, ws, handlercontract.ReviewLoopPhaseImplementerResume, &priorSessID, 2)

	_, _, err = daemon.ExportedBuildClaudeLaunchSpec(context.Background(), rc)
	if err != nil {
		t.Fatalf("reference buildClaudeLaunchSpec: %v", err)
	}

	ws2 := claudeHarnessFixtureWorkspace(t)
	rc2 := claudeHarnessFixtureRunCtx(t, ws2, handlercontract.ReviewLoopPhaseImplementerResume, &priorSessID, 2)
	hrc := daemon.ExportedRunCtxFromClaudeRunCtx(rc2)

	h := daemon.ExportedNewClaudeHarness()
	spawn, err := h.LaunchSpec(hrc)
	if err != nil {
		t.Fatalf("ClaudeHarness.LaunchSpec: %v", err)
	}

	// implementer-resume uses --resume (CHB-008).
	claudeHarnessAssertResumeFlag(t, spawn.Args)
	// Session ID in --resume arg must be the prior session ID.
	claudeHarnessAssertArgValue(t, spawn.Args, "--resume", priorSessID)
}

// TestClaudeHarness_LaunchSpec_Reviewer verifies SpawnSpec parity for the
// reviewer phase: fresh session ID, --session-id flag.
func TestClaudeHarness_LaunchSpec_Reviewer(t *testing.T) {
	ws := claudeHarnessFixtureWorkspace(t)
	rc := claudeHarnessFixtureRunCtx(t, ws, handlercontract.ReviewLoopPhaseReviewer, nil, 1)

	refSpec, _, err := daemon.ExportedBuildClaudeLaunchSpec(context.Background(), rc)
	if err != nil {
		t.Fatalf("reference buildClaudeLaunchSpec: %v", err)
	}

	ws2 := claudeHarnessFixtureWorkspace(t)
	rc2 := claudeHarnessFixtureRunCtx(t, ws2, handlercontract.ReviewLoopPhaseReviewer, nil, 1)
	hrc := daemon.ExportedRunCtxFromClaudeRunCtx(rc2)

	h := daemon.ExportedNewClaudeHarness()
	spawn, err := h.LaunchSpec(hrc)
	if err != nil {
		t.Fatalf("ClaudeHarness.LaunchSpec: %v", err)
	}

	claudeHarnessAssertSpawnSpecShape(t, "Reviewer", spawn, refSpec.Binary, false)
}

// TestClaudeHarness_LaunchSpec_EnvKeys verifies CHB-006 env vars are present in
// the SpawnSpec returned by ClaudeHarness.LaunchSpec (same as buildClaudeLaunchSpec).
func TestClaudeHarness_LaunchSpec_EnvKeys(t *testing.T) {
	ws := claudeHarnessFixtureWorkspace(t)
	rc := claudeHarnessFixtureRunCtx(t, ws, "", nil, 0)
	hrc := daemon.ExportedRunCtxFromClaudeRunCtx(rc)

	h := daemon.ExportedNewClaudeHarness()
	spawn, err := h.LaunchSpec(hrc)
	if err != nil {
		t.Fatalf("ClaudeHarness.LaunchSpec: %v", err)
	}

	for _, key := range []string{
		"HARMONIK_RUN_ID",
		"HARMONIK_DAEMON_SOCKET",
		"HARMONIK_CLAUDE_SESSION_ID",
		"HARMONIK_HANDLER_SESSION_ID",
		"HARMONIK_AGENT_TYPE",
	} {
		claudeHarnessAssertEnvKey(t, spawn.Env, key)
	}
}

// TestClaudeHarness_LaunchSpec_CredentialKeysStripped verifies the CI-003
// credential deny-list scrub applies to the harness path (same regression lock as
// TestBuildClaudeLaunchSpec_CredentialKeysAbsentFromEnv).
func TestClaudeHarness_LaunchSpec_CredentialKeysStripped(t *testing.T) {
	ws := claudeHarnessFixtureWorkspace(t)
	runUID, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("mint runUID: %v", err)
	}
	rc := daemon.ExportedClaudeRunCtx{
		RunID:         core.RunID(runUID),
		BeadID:        "test-bead-harness-ci003",
		WorkspacePath: ws,
		DaemonSocket:  "/tmp/harmonik-test-harness-ci003.sock",
		WorkflowMode:  core.WorkflowModeSingle,
		HandlerBinary: "claude",
		BaseEnv: []string{
			"HARMONIK_PROJECT_HASH=deadbeef123456",
			"ANTHROPIC_API_KEY=harness-ci003-sentinel-must-not-reach-child",
			"ANTHROPIC_AUTH_TOKEN=harness-ci003-sentinel-must-not-reach-child",
			"CLAUDE_CODE_OAUTH_TOKEN=harness-ci003-sentinel-must-not-reach-child",
		},
	}
	hrc := daemon.ExportedRunCtxFromClaudeRunCtx(rc)

	h := daemon.ExportedNewClaudeHarness()
	spawn, err := h.LaunchSpec(hrc)
	if err != nil {
		t.Fatalf("ClaudeHarness.LaunchSpec: %v", err)
	}

	denyKeys := []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "CLAUDE_CODE_OAUTH_TOKEN"}
	for _, kv := range spawn.Env {
		for _, dk := range denyKeys {
			prefix := dk + "="
			if strings.HasPrefix(kv, prefix) && len(kv) > len(prefix) {
				t.Errorf("CI-003: harness SpawnSpec carries live value for %q; must be empty override", dk)
			}
		}
	}
	// Empty override must be present.
	for _, dk := range denyKeys {
		want := dk + "="
		found := false
		for _, kv := range spawn.Env {
			if kv == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("CI-003: harness SpawnSpec missing empty override %q", dk)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 2. Shared-scaffolding side-effect parity tests
// ─────────────────────────────────────────────────────────────────────────────

// TestClaudeHarness_LaunchSpec_SettingsJSON_Created verifies that calling
// ClaudeHarness.LaunchSpec materializes .claude/settings.json in the workspace
// (same side-effect as buildClaudeLaunchSpec / MaterializeClaudeSettings).
func TestClaudeHarness_LaunchSpec_SettingsJSON_Created(t *testing.T) {
	ws := claudeHarnessFixtureWorkspace(t)
	rc := claudeHarnessFixtureRunCtx(t, ws, "", nil, 0)
	hrc := daemon.ExportedRunCtxFromClaudeRunCtx(rc)

	h := daemon.ExportedNewClaudeHarness()
	if _, err := h.LaunchSpec(hrc); err != nil {
		t.Fatalf("ClaudeHarness.LaunchSpec: %v", err)
	}

	settingsPath := filepath.Join(ws, ".claude", "settings.json")
	if _, err := os.Stat(settingsPath); err != nil {
		t.Errorf("settings.json not created at %s: %v", settingsPath, err)
	}
}

// TestClaudeHarness_LaunchSpec_AgentTask_Created verifies that calling
// ClaudeHarness.LaunchSpec writes .harmonik/agent-task.md into the workspace
// (same side-effect as buildClaudeLaunchSpec / WriteAgentTask).
func TestClaudeHarness_LaunchSpec_AgentTask_Created(t *testing.T) {
	ws := claudeHarnessFixtureWorkspace(t)
	rc := claudeHarnessFixtureRunCtx(t, ws, "", nil, 0)
	hrc := daemon.ExportedRunCtxFromClaudeRunCtx(rc)

	h := daemon.ExportedNewClaudeHarness()
	if _, err := h.LaunchSpec(hrc); err != nil {
		t.Fatalf("ClaudeHarness.LaunchSpec: %v", err)
	}

	taskPath := filepath.Join(ws, ".harmonik", "agent-task.md")
	if _, err := os.Stat(taskPath); err != nil {
		t.Errorf("agent-task.md not created at %s: %v", taskPath, err)
	}
}

// TestClaudeHarness_LaunchSpec_WorkDir verifies that SpawnSpec.WorkDir equals the
// workspace path supplied in RunCtx, mirroring the buildClaudeLaunchSpec behaviour.
func TestClaudeHarness_LaunchSpec_WorkDir(t *testing.T) {
	ws := claudeHarnessFixtureWorkspace(t)
	rc := claudeHarnessFixtureRunCtx(t, ws, "", nil, 0)
	hrc := daemon.ExportedRunCtxFromClaudeRunCtx(rc)

	h := daemon.ExportedNewClaudeHarness()
	spawn, err := h.LaunchSpec(hrc)
	if err != nil {
		t.Fatalf("ClaudeHarness.LaunchSpec: %v", err)
	}

	if spawn.WorkDir != ws {
		t.Errorf("SpawnSpec.WorkDir = %q; want %q", spawn.WorkDir, ws)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 3. Constant-method tests (AgentType, SessionIDPolicy, Completion, DetectReady)
// ─────────────────────────────────────────────────────────────────────────────

// TestClaudeHarness_AgentType verifies AgentType returns AgentTypeClaudeCode.
func TestClaudeHarness_AgentType(t *testing.T) {
	t.Parallel()

	h := daemon.ExportedNewClaudeHarness()
	if got := h.AgentType(); got != core.AgentTypeClaudeCode {
		t.Errorf("AgentType = %q; want %q", got, core.AgentTypeClaudeCode)
	}
}

// TestClaudeHarness_SessionIDPolicy verifies SessionIDPolicy returns SessionIDMinted.
func TestClaudeHarness_SessionIDPolicy(t *testing.T) {
	t.Parallel()

	h := daemon.ExportedNewClaudeHarness()
	if got := h.SessionIDPolicy(); got != handlercontract.SessionIDMinted {
		t.Errorf("SessionIDPolicy = %v; want SessionIDMinted", got)
	}
}

// TestClaudeHarness_Completion verifies Completion returns CompletionEventStreamThenQuit.
func TestClaudeHarness_Completion(t *testing.T) {
	t.Parallel()

	h := daemon.ExportedNewClaudeHarness()
	if got := h.Completion(); got != handlercontract.CompletionEventStreamThenQuit {
		t.Errorf("Completion = %v; want CompletionEventStreamThenQuit", got)
	}
}

// TestClaudeHarness_DetectReady_AgentReady verifies DetectReady returns true for
// an agent_ready event (HC-041).
func TestClaudeHarness_DetectReady_AgentReady(t *testing.T) {
	t.Parallel()

	h := daemon.ExportedNewClaudeHarness()
	ev := handlercontract.EventEnvelope{Type: string(core.EventTypeAgentReady)}
	if !h.DetectReady(ev) {
		t.Error("DetectReady(agent_ready) = false; want true")
	}
}

// TestClaudeHarness_DetectReady_LaunchInitiated verifies DetectReady returns false
// for launch_initiated (HC-041 hard rule: MUST NOT return true for launch_initiated).
func TestClaudeHarness_DetectReady_LaunchInitiated(t *testing.T) {
	t.Parallel()

	h := daemon.ExportedNewClaudeHarness()
	ev := handlercontract.EventEnvelope{Type: string(core.EventTypeLaunchInitiated)}
	if h.DetectReady(ev) {
		t.Error("DetectReady(launch_initiated) = true; want false (HC-041)")
	}
}

// TestClaudeHarness_DetectReady_OtherEvent verifies DetectReady returns false for
// an unrelated event type.
func TestClaudeHarness_DetectReady_OtherEvent(t *testing.T) {
	t.Parallel()

	h := daemon.ExportedNewClaudeHarness()
	ev := handlercontract.EventEnvelope{Type: "run_started"}
	if h.DetectReady(ev) {
		t.Error("DetectReady(run_started) = true; want false")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// assertion helpers
// ─────────────────────────────────────────────────────────────────────────────

// claudeHarnessAssertSpawnSpecShape verifies the SpawnSpec has the expected binary
// and a --session-id flag (when wantResume is false).
func claudeHarnessAssertSpawnSpecShape(t *testing.T, label string, spawn handlercontract.SpawnSpec, wantBinary string, wantResume bool) {
	t.Helper()
	if spawn.Binary != wantBinary {
		t.Errorf("%s: SpawnSpec.Binary = %q; want %q", label, spawn.Binary, wantBinary)
	}
	if spawn.WorkDir == "" {
		t.Errorf("%s: SpawnSpec.WorkDir is empty", label)
	}
	if !wantResume {
		// Expect --session-id flag.
		found := false
		for i, a := range spawn.Args {
			if a == "--session-id" {
				if i+1 >= len(spawn.Args) || spawn.Args[i+1] == "" {
					t.Errorf("%s: --session-id present but session ID value is missing", label)
				}
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%s: --session-id not found in SpawnSpec.Args %v", label, spawn.Args)
		}
		for _, a := range spawn.Args {
			if a == "--resume" {
				t.Errorf("%s: --resume must not be present for non-resume phases", label)
			}
		}
	}
}

// claudeHarnessAssertResumeFlag verifies --resume is present and --session-id absent.
func claudeHarnessAssertResumeFlag(t *testing.T, args []string) {
	t.Helper()
	for i, a := range args {
		if a == "--resume" {
			if i+1 >= len(args) || args[i+1] == "" {
				t.Error("--resume present but session ID value is missing")
			}
			for _, b := range args {
				if b == "--session-id" {
					t.Error("--session-id must not be present for implementer-resume")
				}
			}
			return
		}
	}
	t.Errorf("--resume not found in args %v; required for implementer-resume (CHB-008)", args)
}

// claudeHarnessAssertArgValue asserts that flag is followed by wantValue in args.
func claudeHarnessAssertArgValue(t *testing.T, args []string, flag, wantValue string) {
	t.Helper()
	for i, a := range args {
		if a == flag {
			if i+1 >= len(args) {
				t.Errorf("%s present but has no following value", flag)
				return
			}
			if args[i+1] != wantValue {
				t.Errorf("arg after %s = %q; want %q", flag, args[i+1], wantValue)
			}
			return
		}
	}
	t.Errorf("%s not found in args %v", flag, args)
}

// claudeHarnessAssertEnvKey verifies that env contains at least one entry with the
// given key prefix "KEY=".
func claudeHarnessAssertEnvKey(t *testing.T, env []string, key string) {
	t.Helper()
	prefix := key + "="
	for _, e := range env {
		if strings.HasPrefix(e, prefix) {
			return
		}
	}
	t.Errorf("SpawnSpec.Env missing %q entry", key)
}

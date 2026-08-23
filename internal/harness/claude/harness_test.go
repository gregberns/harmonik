package claude_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/harness/claude"
	"github.com/gregberns/harmonik/internal/harness/shared"
)

func claudeHarnessFixtureWorkspace(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".claude"), 0o700); err != nil {
		t.Fatalf("claudeHarnessFixtureWorkspace: MkdirAll .claude: %v", err)
	}
	return dir
}

func claudeHarnessFixtureRunCtx(
	t *testing.T,
	workspacePath string,
	phase handlercontract.ReviewLoopPhase,
	priorSessID *string,
	iterationCount int,
) shared.LaunchCtx {
	t.Helper()
	runUID, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("claudeHarnessFixtureRunCtx: NewV7: %v", err)
	}
	return shared.LaunchCtx{
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

func claudeHarnessRunCtxFrom(rc shared.LaunchCtx) handlercontract.RunCtx {
	return handlercontract.RunCtx{
		RunID:            rc.RunID,
		BeadID:           rc.BeadID,
		WorkspacePath:    rc.WorkspacePath,
		DaemonSocket:     rc.DaemonSocket,
		WorkflowMode:     rc.WorkflowMode,
		Phase:            rc.Phase,
		IterationCount:   rc.IterationCount,
		PriorSessionID:   rc.PriorClaudeSessID,
		HandlerBinary:    rc.HandlerBinary,
		DaemonBinaryPath: rc.DaemonBinaryPath,
		BaseEnv:          rc.BaseEnv,
		Model:            rc.Model,
		Effort:           rc.Effort,
		WorktreeRootPath: rc.WorktreeRootPath,
		BeadDescription:  rc.BeadDescription,
		NodePrompt:       rc.NodePrompt,
	}
}

// TestClaudeHarness_LaunchSpec_Single verifies SpawnSpec parity for single-mode.
func TestClaudeHarness_LaunchSpec_Single(t *testing.T) {
	ws := claudeHarnessFixtureWorkspace(t)
	rc := claudeHarnessFixtureRunCtx(t, ws, "", nil, 0)

	refSpec, _, err := claude.BuildLaunchSpec(context.Background(), rc)
	if err != nil {
		t.Fatalf("reference BuildLaunchSpec: %v", err)
	}

	ws2 := claudeHarnessFixtureWorkspace(t)
	rc2 := claudeHarnessFixtureRunCtx(t, ws2, "", nil, 0)
	hrc := claudeHarnessRunCtxFrom(rc2)

	h := claude.NewHarness()
	spawn, err := h.LaunchSpec(hrc)
	if err != nil {
		t.Fatalf("claude.Harness.LaunchSpec: %v", err)
	}

	claudeHarnessAssertSpawnSpecShape(t, "Single", spawn, refSpec.Binary, false)
}

// TestClaudeHarness_LaunchSpec_ImplementerInitial verifies SpawnSpec parity for
// implementer-initial phase.
func TestClaudeHarness_LaunchSpec_ImplementerInitial(t *testing.T) {
	ws := claudeHarnessFixtureWorkspace(t)
	rc := claudeHarnessFixtureRunCtx(t, ws, handlercontract.ReviewLoopPhaseImplementerInitial, nil, 1)

	refSpec, _, err := claude.BuildLaunchSpec(context.Background(), rc)
	if err != nil {
		t.Fatalf("reference BuildLaunchSpec: %v", err)
	}

	ws2 := claudeHarnessFixtureWorkspace(t)
	rc2 := claudeHarnessFixtureRunCtx(t, ws2, handlercontract.ReviewLoopPhaseImplementerInitial, nil, 1)
	hrc := claudeHarnessRunCtxFrom(rc2)

	h := claude.NewHarness()
	spawn, err := h.LaunchSpec(hrc)
	if err != nil {
		t.Fatalf("claude.Harness.LaunchSpec: %v", err)
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

	_, _, err = claude.BuildLaunchSpec(context.Background(), rc)
	if err != nil {
		t.Fatalf("reference BuildLaunchSpec: %v", err)
	}

	ws2 := claudeHarnessFixtureWorkspace(t)
	rc2 := claudeHarnessFixtureRunCtx(t, ws2, handlercontract.ReviewLoopPhaseImplementerResume, &priorSessID, 2)
	hrc := claudeHarnessRunCtxFrom(rc2)

	h := claude.NewHarness()
	spawn, err := h.LaunchSpec(hrc)
	if err != nil {
		t.Fatalf("claude.Harness.LaunchSpec: %v", err)
	}

	claudeHarnessAssertResumeFlag(t, spawn.Args)
	claudeHarnessAssertArgValue(t, spawn.Args, "--resume", priorSessID)
}

// TestClaudeHarness_LaunchSpec_Reviewer verifies SpawnSpec parity for the
// reviewer phase: fresh session ID, --session-id flag.
func TestClaudeHarness_LaunchSpec_Reviewer(t *testing.T) {
	ws := claudeHarnessFixtureWorkspace(t)
	rc := claudeHarnessFixtureRunCtx(t, ws, handlercontract.ReviewLoopPhaseReviewer, nil, 1)

	refSpec, _, err := claude.BuildLaunchSpec(context.Background(), rc)
	if err != nil {
		t.Fatalf("reference BuildLaunchSpec: %v", err)
	}

	ws2 := claudeHarnessFixtureWorkspace(t)
	rc2 := claudeHarnessFixtureRunCtx(t, ws2, handlercontract.ReviewLoopPhaseReviewer, nil, 1)
	hrc := claudeHarnessRunCtxFrom(rc2)

	h := claude.NewHarness()
	spawn, err := h.LaunchSpec(hrc)
	if err != nil {
		t.Fatalf("claude.Harness.LaunchSpec: %v", err)
	}

	claudeHarnessAssertSpawnSpecShape(t, "Reviewer", spawn, refSpec.Binary, false)
}

// TestClaudeHarness_LaunchSpec_EnvKeys verifies CHB-006 env vars are present in
// the SpawnSpec returned by claude.Harness.LaunchSpec (same as BuildLaunchSpec).
func TestClaudeHarness_LaunchSpec_EnvKeys(t *testing.T) {
	ws := claudeHarnessFixtureWorkspace(t)
	rc := claudeHarnessFixtureRunCtx(t, ws, "", nil, 0)
	hrc := claudeHarnessRunCtxFrom(rc)

	h := claude.NewHarness()
	spawn, err := h.LaunchSpec(hrc)
	if err != nil {
		t.Fatalf("claude.Harness.LaunchSpec: %v", err)
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
	rc := shared.LaunchCtx{
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
	hrc := claudeHarnessRunCtxFrom(rc)

	h := claude.NewHarness()
	spawn, err := h.LaunchSpec(hrc)
	if err != nil {
		t.Fatalf("claude.Harness.LaunchSpec: %v", err)
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

// TestClaudeHarness_LaunchSpec_SettingsJSON_Created verifies that calling
// claude.Harness.LaunchSpec materializes .claude/settings.json in the workspace
// (same side-effect as BuildLaunchSpec / MaterializeClaudeSettings).
func TestClaudeHarness_LaunchSpec_SettingsJSON_Created(t *testing.T) {
	ws := claudeHarnessFixtureWorkspace(t)
	rc := claudeHarnessFixtureRunCtx(t, ws, "", nil, 0)
	hrc := claudeHarnessRunCtxFrom(rc)

	h := claude.NewHarness()
	if _, err := h.LaunchSpec(hrc); err != nil {
		t.Fatalf("claude.Harness.LaunchSpec: %v", err)
	}

	settingsPath := filepath.Join(ws, ".claude", "settings.json")
	if _, err := os.Stat(settingsPath); err != nil {
		t.Errorf("settings.json not created at %s: %v", settingsPath, err)
	}
}

// TestClaudeHarness_LaunchSpec_AgentTask_Created verifies that calling
// claude.Harness.LaunchSpec writes .harmonik/agent-task.md into the workspace
// (same side-effect as BuildLaunchSpec / WriteAgentTask).
func TestClaudeHarness_LaunchSpec_AgentTask_Created(t *testing.T) {
	ws := claudeHarnessFixtureWorkspace(t)
	rc := claudeHarnessFixtureRunCtx(t, ws, "", nil, 0)
	hrc := claudeHarnessRunCtxFrom(rc)

	h := claude.NewHarness()
	if _, err := h.LaunchSpec(hrc); err != nil {
		t.Fatalf("claude.Harness.LaunchSpec: %v", err)
	}

	taskPath := filepath.Join(ws, ".harmonik", "agent-task.md")
	if _, err := os.Stat(taskPath); err != nil {
		t.Errorf("agent-task.md not created at %s: %v", taskPath, err)
	}
}

// TestClaudeHarness_LaunchSpec_WorkDir verifies that SpawnSpec.WorkDir equals the
// workspace path supplied in RunCtx, mirroring the BuildLaunchSpec behaviour.
func TestClaudeHarness_LaunchSpec_WorkDir(t *testing.T) {
	ws := claudeHarnessFixtureWorkspace(t)
	rc := claudeHarnessFixtureRunCtx(t, ws, "", nil, 0)
	hrc := claudeHarnessRunCtxFrom(rc)

	h := claude.NewHarness()
	spawn, err := h.LaunchSpec(hrc)
	if err != nil {
		t.Fatalf("claude.Harness.LaunchSpec: %v", err)
	}

	if spawn.WorkDir != ws {
		t.Errorf("SpawnSpec.WorkDir = %q; want %q", spawn.WorkDir, ws)
	}
}

// TestClaudeHarness_AgentType verifies AgentType returns AgentTypeClaudeCode.
func TestClaudeHarness_AgentType(t *testing.T) {
	t.Parallel()

	h := claude.NewHarness()
	if got := h.AgentType(); got != core.AgentTypeClaudeCode {
		t.Errorf("AgentType = %q; want %q", got, core.AgentTypeClaudeCode)
	}
}

// TestClaudeHarness_SessionIDPolicy verifies SessionIDPolicy returns SessionIDMinted.
func TestClaudeHarness_SessionIDPolicy(t *testing.T) {
	t.Parallel()

	h := claude.NewHarness()
	if got := h.SessionIDPolicy(); got != handlercontract.SessionIDMinted {
		t.Errorf("SessionIDPolicy = %v; want SessionIDMinted", got)
	}
}

// TestClaudeHarness_Completion verifies Completion returns CompletionEventStreamThenQuit.
func TestClaudeHarness_Completion(t *testing.T) {
	t.Parallel()

	h := claude.NewHarness()
	if got := h.Completion(); got != handlercontract.CompletionEventStreamThenQuit {
		t.Errorf("Completion = %v; want CompletionEventStreamThenQuit", got)
	}
}

// TestClaudeHarness_DetectReady_AgentReady verifies DetectReady returns true for
// an agent_ready event (HC-041).
func TestClaudeHarness_DetectReady_AgentReady(t *testing.T) {
	t.Parallel()

	h := claude.NewHarness()
	ev := handlercontract.EventEnvelope{Type: core.EventTypeAgentReady}
	if !h.DetectReady(ev) {
		t.Error("DetectReady(agent_ready) = false; want true")
	}
}

// TestClaudeHarness_DetectReady_LaunchInitiated verifies DetectReady returns false
// for launch_initiated (HC-041 hard rule: MUST NOT return true for launch_initiated).
func TestClaudeHarness_DetectReady_LaunchInitiated(t *testing.T) {
	t.Parallel()

	h := claude.NewHarness()
	ev := handlercontract.EventEnvelope{Type: core.EventTypeLaunchInitiated}
	if h.DetectReady(ev) {
		t.Error("DetectReady(launch_initiated) = true; want false (HC-041)")
	}
}

// TestClaudeHarness_DetectReady_OtherEvent verifies DetectReady returns false for
// an unrelated event type.
func TestClaudeHarness_DetectReady_OtherEvent(t *testing.T) {
	t.Parallel()

	h := claude.NewHarness()
	ev := handlercontract.EventEnvelope{Type: "run_started"}
	if h.DetectReady(ev) {
		t.Error("DetectReady(run_started) = true; want false")
	}
}

func claudeHarnessAssertSpawnSpecShape(t *testing.T, label string, spawn handlercontract.SpawnSpec, wantBinary string, wantResume bool) {
	t.Helper()
	if spawn.Binary != wantBinary {
		t.Errorf("%s: SpawnSpec.Binary = %q; want %q", label, spawn.Binary, wantBinary)
	}
	if spawn.WorkDir == "" {
		t.Errorf("%s: SpawnSpec.WorkDir is empty", label)
	}
	if !wantResume {
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

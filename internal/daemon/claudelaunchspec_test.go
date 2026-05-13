package daemon_test

// claudelaunchspec_test.go — unit tests for buildClaudeLaunchSpec (hk-gql20.13).
//
// Verifies the helper threads all bridge pieces correctly for all four workflow
// phases: single, implementer-initial, implementer-resume, reviewer.
//
// Key invariants tested:
//
//   - CHB-008: --session-id used for single/initial/reviewer; --resume for resume.
//   - CHB-009: reviewer always mints a fresh session ID, ignores priorClaudeSessID.
//   - CHB-007: CheckForbiddenFlags is invoked (forbidden flag injected via argv
//     detection path is not directly testable here — the helper builds argv
//     internally, so we verify the deny-list path via env-var injection).
//   - LaunchSpec fields are populated correctly per spec.
//   - claudeRunArtifacts carries claudeSessionID, sessionLogPath, handlerSessionID,
//     and preExecMsgs.
//
// Helper prefix: claudeLaunchSpecFixture (bead hk-gql20.13).

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

// claudeLaunchSpecFixtureWorkspace creates a temporary workspace directory
// suitable for MaterializeClaudeSettings and CheckSettingsLocalJSON.
// Returns the workspace path.
func claudeLaunchSpecFixtureWorkspace(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	// Ensure the .claude/ directory exists so MaterializeClaudeSettings does not
	// need to create it from scratch (it will, but this mirrors real worktree layout).
	if err := os.MkdirAll(filepath.Join(dir, ".claude"), 0o755); err != nil {
		t.Fatalf("claudeLaunchSpecFixtureWorkspace: MkdirAll .claude/: %v", err)
	}
	return dir
}

// claudeLaunchSpecFixtureRunCtx builds a claudeRunCtx for the given phase.
// workspacePath must be a valid temp directory (e.g. from claudeLaunchSpecFixtureWorkspace).
func claudeLaunchSpecFixtureRunCtx(
	t *testing.T,
	workspacePath string,
	phase handlercontract.ReviewLoopPhase,
	priorClaudeSessID *string,
	iterationCount int,
) daemon.ExportedClaudeRunCtx {
	t.Helper()
	runUID, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("claudeLaunchSpecFixtureRunCtx: NewV7 runID: %v", err)
	}
	return daemon.ExportedClaudeRunCtx{
		RunID:             core.RunID(runUID),
		BeadID:            "test-bead-gql20.13",
		WorkspacePath:     workspacePath,
		DaemonSocket:      "/tmp/harmonik-test-gql20.13.sock",
		WorkflowMode:      core.WorkflowModeSingle,
		Phase:             phase,
		IterationCount:    iterationCount,
		PriorClaudeSessID: priorClaudeSessID,
		HandlerBinary:     "claude",
		BaseEnv:           []string{"HARMONIK_PROJECT_HASH=deadbeef123456"},
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// TestBuildClaudeLaunchSpec — all four phases
// ─────────────────────────────────────────────────────────────────────────────

// TestBuildClaudeLaunchSpec_Single verifies the helper builds a correct
// LaunchSpec and artifacts for single-mode (no phase, fresh session).
func TestBuildClaudeLaunchSpec_Single(t *testing.T) {
	t.Parallel()

	ws := claudeLaunchSpecFixtureWorkspace(t)
	rc := claudeLaunchSpecFixtureRunCtx(t, ws, "", nil, 0)

	spec, arts, err := daemon.ExportedBuildClaudeLaunchSpec(context.Background(), rc)
	if err != nil {
		t.Fatalf("TestBuildClaudeLaunchSpec_Single: unexpected error: %v", err)
	}

	// LaunchSpec fields.
	if spec.Binary != "claude" {
		t.Errorf("Binary = %q; want %q", spec.Binary, "claude")
	}
	if spec.WorkDir != ws {
		t.Errorf("WorkDir = %q; want %q", spec.WorkDir, ws)
	}

	// Single-mode uses --session-id (CHB-008: not implementer-resume).
	claudeLaunchSpecAssertSessionIDFlag(t, spec.Args, false)

	// Artifacts non-empty.
	if arts.ClaudeSessionID == "" {
		t.Error("claudeSessionID must be non-empty")
	}
	if arts.SessionLogPath == "" {
		t.Error("sessionLogPath must be non-empty")
	}
	if arts.HandlerSessionID == "" {
		t.Error("handlerSessionID must be non-empty")
	}
	if len(arts.PreExecMsgs) != 4 {
		t.Errorf("preExecMsgs len = %d; want 4 (CHB-018)", len(arts.PreExecMsgs))
	}

	// CHB-006: required env vars present.
	claudeLaunchSpecAssertEnvKey(t, spec.Env, "HARMONIK_RUN_ID")
	claudeLaunchSpecAssertEnvKey(t, spec.Env, "HARMONIK_DAEMON_SOCKET")
	claudeLaunchSpecAssertEnvKey(t, spec.Env, "HARMONIK_CLAUDE_SESSION_ID")
	claudeLaunchSpecAssertEnvKey(t, spec.Env, "HARMONIK_HANDLER_SESSION_ID")
	claudeLaunchSpecAssertEnvKey(t, spec.Env, "HARMONIK_AGENT_TYPE")
}

// TestBuildClaudeLaunchSpec_ImplementerInitial verifies the helper for the
// implementer-initial phase: fresh session, --session-id flag.
func TestBuildClaudeLaunchSpec_ImplementerInitial(t *testing.T) {
	t.Parallel()

	ws := claudeLaunchSpecFixtureWorkspace(t)
	rc := claudeLaunchSpecFixtureRunCtx(t, ws, handlercontract.ReviewLoopPhaseImplementerInitial, nil, 1)

	spec, arts, err := daemon.ExportedBuildClaudeLaunchSpec(context.Background(), rc)
	if err != nil {
		t.Fatalf("TestBuildClaudeLaunchSpec_ImplementerInitial: unexpected error: %v", err)
	}

	// implementer-initial: --session-id (not --resume).
	claudeLaunchSpecAssertSessionIDFlag(t, spec.Args, false)

	if arts.ClaudeSessionID == "" {
		t.Error("claudeSessionID must be non-empty")
	}
	if len(arts.PreExecMsgs) != 4 {
		t.Errorf("preExecMsgs len = %d; want 4", len(arts.PreExecMsgs))
	}
}

// TestBuildClaudeLaunchSpec_ImplementerResume verifies the helper for the
// implementer-resume phase: reuses prior session ID, uses --resume flag (CHB-008).
func TestBuildClaudeLaunchSpec_ImplementerResume(t *testing.T) {
	t.Parallel()

	ws := claudeLaunchSpecFixtureWorkspace(t)
	// Mint a fake prior session ID.
	priorUID, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("mint priorUID: %v", err)
	}
	priorSessID := priorUID.String()

	rc := claudeLaunchSpecFixtureRunCtx(t, ws, handlercontract.ReviewLoopPhaseImplementerResume, &priorSessID, 2)

	spec, arts, err := daemon.ExportedBuildClaudeLaunchSpec(context.Background(), rc)
	if err != nil {
		t.Fatalf("TestBuildClaudeLaunchSpec_ImplementerResume: unexpected error: %v", err)
	}

	// implementer-resume: --resume (CHB-008).
	claudeLaunchSpecAssertResumeFlag(t, spec.Args)

	// Session ID is reused from prior (CHB-008).
	if arts.ClaudeSessionID != priorSessID {
		t.Errorf("claudeSessionID = %q; want prior session %q", arts.ClaudeSessionID, priorSessID)
	}

	if len(arts.PreExecMsgs) != 4 {
		t.Errorf("preExecMsgs len = %d; want 4", len(arts.PreExecMsgs))
	}
}

// TestBuildClaudeLaunchSpec_Reviewer verifies the helper for the reviewer phase:
// mints a fresh session ID even when priorClaudeSessID is provided (CHB-009).
func TestBuildClaudeLaunchSpec_Reviewer(t *testing.T) {
	t.Parallel()

	ws := claudeLaunchSpecFixtureWorkspace(t)
	// Supply a non-nil priorClaudeSessID — reviewer MUST ignore it (CHB-009).
	priorUID, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("mint priorUID: %v", err)
	}
	priorSessID := priorUID.String()

	rc := claudeLaunchSpecFixtureRunCtx(t, ws, handlercontract.ReviewLoopPhaseReviewer, &priorSessID, 1)

	spec, arts, err := daemon.ExportedBuildClaudeLaunchSpec(context.Background(), rc)
	if err != nil {
		t.Fatalf("TestBuildClaudeLaunchSpec_Reviewer: unexpected error: %v", err)
	}

	// Reviewer uses --session-id (fresh session; CHB-009 — never --resume).
	claudeLaunchSpecAssertSessionIDFlag(t, spec.Args, false)

	// Reviewer mints a fresh session ID — must differ from priorSessID (CHB-009).
	if arts.ClaudeSessionID == priorSessID {
		t.Errorf("reviewer claudeSessionID = %q; must NOT reuse prior session %q (CHB-009)",
			arts.ClaudeSessionID, priorSessID)
	}
	if arts.ClaudeSessionID == "" {
		t.Error("reviewer claudeSessionID must be non-empty")
	}

	if len(arts.PreExecMsgs) != 4 {
		t.Errorf("preExecMsgs len = %d; want 4", len(arts.PreExecMsgs))
	}
}

// TestBuildClaudeLaunchSpec_CheckForbiddenFlagsInvoked verifies that the
// deny-list guard (CHB-007) fires when the base environment contains a
// forbidden env var.  We inject CLAUDE_CODE_SKIP_PROMPT_HISTORY into baseEnv
// so it propagates through ClaudeEnvVars and triggers CheckForbiddenFlags.
func TestBuildClaudeLaunchSpec_CheckForbiddenFlagsInvoked(t *testing.T) {
	t.Parallel()

	ws := claudeLaunchSpecFixtureWorkspace(t)
	runUID, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("mint runUID: %v", err)
	}
	// Inject the forbidden env var via baseEnv.
	rc := daemon.ExportedClaudeRunCtx{
		RunID:             core.RunID(runUID),
		BeadID:            "test-bead-chb007",
		WorkspacePath:     ws,
		DaemonSocket:      "/tmp/harmonik-test-chb007.sock",
		WorkflowMode:      core.WorkflowModeSingle,
		Phase:             "",
		IterationCount:    0,
		PriorClaudeSessID: nil,
		HandlerBinary:     "claude",
		BaseEnv: []string{
			"HARMONIK_PROJECT_HASH=deadbeef123456",
			"CLAUDE_CODE_SKIP_PROMPT_HISTORY=1", // forbidden per CHB-007
		},
	}

	_, _, err = daemon.ExportedBuildClaudeLaunchSpec(context.Background(), rc)
	if err == nil {
		t.Error("expected error for forbidden env var CLAUDE_CODE_SKIP_PROMPT_HISTORY; got nil")
	}
}

// TestBuildClaudeLaunchSpec_ImplementerResume_NilPriorSessionErrors verifies
// that the helper returns an error when phase=implementer-resume but
// priorClaudeSessID is nil (CHB-008 structural constraint).
func TestBuildClaudeLaunchSpec_ImplementerResume_NilPriorSessionErrors(t *testing.T) {
	t.Parallel()

	ws := claudeLaunchSpecFixtureWorkspace(t)
	rc := claudeLaunchSpecFixtureRunCtx(t, ws, handlercontract.ReviewLoopPhaseImplementerResume, nil, 2)

	_, _, err := daemon.ExportedBuildClaudeLaunchSpec(context.Background(), rc)
	if err == nil {
		t.Error("expected error for implementer-resume with nil priorClaudeSessID; got nil")
	}
}

// TestBuildClaudeLaunchSpec_TwinBlind verifies the helper treats Binary as
// opaque: specifying "harmonik-twin-claude" produces the same spec shape.
func TestBuildClaudeLaunchSpec_TwinBlind(t *testing.T) {
	t.Parallel()

	ws := claudeLaunchSpecFixtureWorkspace(t)
	runUID, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("mint runUID: %v", err)
	}
	rc := daemon.ExportedClaudeRunCtx{
		RunID:             core.RunID(runUID),
		BeadID:            "test-bead-twin",
		WorkspacePath:     ws,
		DaemonSocket:      "/tmp/harmonik-test-twin.sock",
		WorkflowMode:      core.WorkflowModeSingle,
		Phase:             "",
		IterationCount:    0,
		PriorClaudeSessID: nil,
		HandlerBinary:     "harmonik-twin-claude",
		BaseEnv:           []string{"HARMONIK_PROJECT_HASH=deadbeef123456"},
	}

	spec, _, err := daemon.ExportedBuildClaudeLaunchSpec(context.Background(), rc)
	if err != nil {
		t.Fatalf("TestBuildClaudeLaunchSpec_TwinBlind: unexpected error: %v", err)
	}
	if spec.Binary != "harmonik-twin-claude" {
		t.Errorf("Binary = %q; want %q", spec.Binary, "harmonik-twin-claude")
	}
	// Args, Env, WorkDir shape must be identical to the claude case.
	claudeLaunchSpecAssertSessionIDFlag(t, spec.Args, false)
}

// ─────────────────────────────────────────────────────────────────────────────
// assertion helpers
// ─────────────────────────────────────────────────────────────────────────────

// claudeLaunchSpecAssertSessionIDFlag verifies that args contains
// "--session-id" followed by a non-empty UUID string.
// When wantResume is true, it additionally verifies "--resume" is absent.
func claudeLaunchSpecAssertSessionIDFlag(t *testing.T, args []string, wantResume bool) {
	t.Helper()
	if wantResume {
		// Should not reach this branch — use claudeLaunchSpecAssertResumeFlag.
		t.Error("claudeLaunchSpecAssertSessionIDFlag called with wantResume=true; use claudeLaunchSpecAssertResumeFlag")
		return
	}
	for i, a := range args {
		if a == "--session-id" {
			if i+1 >= len(args) || args[i+1] == "" {
				t.Error("--session-id present but session ID value is missing or empty")
			}
			// Verify --resume is absent (single / initial / reviewer phases).
			for _, b := range args {
				if b == "--resume" {
					t.Error("--resume must not be present for non-resume phases (CHB-008)")
				}
			}
			return
		}
	}
	t.Errorf("--session-id not found in args %v; required for non-resume phases (CHB-008)", args)
}

// claudeLaunchSpecAssertResumeFlag verifies that args contains
// "--resume" followed by a non-empty UUID string, and that "--session-id" is absent.
func claudeLaunchSpecAssertResumeFlag(t *testing.T, args []string) {
	t.Helper()
	for i, a := range args {
		if a == "--resume" {
			if i+1 >= len(args) || args[i+1] == "" {
				t.Error("--resume present but session ID value is missing or empty")
			}
			for _, b := range args {
				if b == "--session-id" {
					t.Error("--session-id must not be present for implementer-resume phase (CHB-008)")
				}
			}
			return
		}
	}
	t.Errorf("--resume not found in args %v; required for implementer-resume phase (CHB-008)", args)
}

// claudeLaunchSpecAssertEnvKey verifies that env contains at least one entry
// with the given key prefix "KEY=".
func claudeLaunchSpecAssertEnvKey(t *testing.T, env []string, key string) {
	t.Helper()
	prefix := key + "="
	for _, e := range env {
		if strings.HasPrefix(e, prefix) {
			return
		}
	}
	t.Errorf("env missing %q entry; have %v", key, env)
}

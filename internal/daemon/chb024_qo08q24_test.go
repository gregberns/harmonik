package daemon_test

// chb024_qo08q24_test.go — settings-precedence verification tests (CHB-024).
//
// Verifies that the daemon's buildClaudeLaunchSpec correctly enforces the
// Claude settings hierarchy: ${workspace}/.claude/settings.local.json takes
// precedence over ${workspace}/.claude/settings.json. When settings.local.json
// shadows the bridge's required hook entries the build MUST fail before exec'ing
// Claude.
//
// Spec ref: specs/claude-hook-bridge.md §4.9 CHB-024.
// Bead: hk-qo08q.24.
//
// Helper prefix: settingsPrecedenceFixture (implementer-protocol.md §Helper-prefix discipline).

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/handler"
)

// ─────────────────────────────────────────────────────────────────────────────
// Fixtures
// ─────────────────────────────────────────────────────────────────────────────

// settingsPrecedenceFixtureWorkspace creates a temporary workspace directory
// with the .claude/ subdirectory pre-created, matching a real worktree layout.
// Returns the workspace path.
func settingsPrecedenceFixtureWorkspace(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(filepath.Join(dir, ".claude"), 0o755); err != nil {
		t.Fatalf("settingsPrecedenceFixtureWorkspace: MkdirAll .claude/: %v", err)
	}
	return dir
}

// settingsPrecedenceFixtureWriteSettingsLocal writes
// ${workspacePath}/.claude/settings.local.json with the provided JSON content.
// This simulates a user-managed settings.local.json that Claude Code's settings
// hierarchy elevates above the bridge-materialized settings.json.
func settingsPrecedenceFixtureWriteSettingsLocal(t *testing.T, workspacePath, content string) {
	t.Helper()
	p := filepath.Join(workspacePath, ".claude", "settings.local.json")
	//nolint:gosec // G306: 0644 is correct for a test fixture JSON file
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("settingsPrecedenceFixtureWriteSettingsLocal: WriteFile: %v", err)
	}
}

// settingsPrecedenceFixtureRunCtx builds an ExportedClaudeRunCtx for exercising
// buildClaudeLaunchSpec with the given workspacePath.
func settingsPrecedenceFixtureRunCtx(t *testing.T, workspacePath string) daemon.ExportedClaudeRunCtx {
	t.Helper()
	runUID, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("settingsPrecedenceFixtureRunCtx: NewV7 runID: %v", err)
	}
	return daemon.ExportedClaudeRunCtx{
		RunID:             core.RunID(runUID),
		BeadID:            "test-bead-chb024",
		WorkspacePath:     workspacePath,
		DaemonSocket:      "/tmp/harmonik-chb024-test.sock",
		WorkflowMode:      core.WorkflowModeSingle,
		Phase:             "",
		IterationCount:    0,
		PriorClaudeSessID: nil,
		HandlerBinary:     "claude",
		BaseEnv:           []string{"HARMONIK_PROJECT_HASH=chb024test"},
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// CHB-024 settings-precedence verification tests
// ─────────────────────────────────────────────────────────────────────────────

// TestSettingsPrecedence_NoSettingsLocal_OK verifies that when no
// settings.local.json exists the daemon's buildClaudeLaunchSpec succeeds.
// This is the common case: bridge hooks in settings.json are reachable.
func TestSettingsPrecedence_NoSettingsLocal_OK(t *testing.T) {
	t.Parallel()

	ws := settingsPrecedenceFixtureWorkspace(t)
	rc := settingsPrecedenceFixtureRunCtx(t, ws)

	_, _, err := daemon.ExportedBuildClaudeLaunchSpec(context.Background(), rc)
	if err != nil {
		t.Errorf("TestSettingsPrecedence_NoSettingsLocal_OK: unexpected error: %v", err)
	}
}

// TestSettingsPrecedence_EmptySettingsLocal_OK verifies that an empty JSON
// object {} in settings.local.json is accepted: it defines no shadows per CHB-024.
func TestSettingsPrecedence_EmptySettingsLocal_OK(t *testing.T) {
	t.Parallel()

	ws := settingsPrecedenceFixtureWorkspace(t)
	settingsPrecedenceFixtureWriteSettingsLocal(t, ws, `{}`)
	rc := settingsPrecedenceFixtureRunCtx(t, ws)

	_, _, err := daemon.ExportedBuildClaudeLaunchSpec(context.Background(), rc)
	if err != nil {
		t.Errorf("TestSettingsPrecedence_EmptySettingsLocal_OK: unexpected error: %v", err)
	}
}

// TestSettingsPrecedence_DisableAllHooks_Rejected verifies that
// settings.local.json containing disableAllHooks:true causes buildClaudeLaunchSpec
// to return an error wrapping ErrStructural. The settings.local.json takes
// precedence over settings.json in Claude's hierarchy, so disableAllHooks:true
// would silence the bridge's hooks despite them being present in settings.json.
func TestSettingsPrecedence_DisableAllHooks_Rejected(t *testing.T) {
	t.Parallel()

	ws := settingsPrecedenceFixtureWorkspace(t)
	// settings.local.json precedence: disableAllHooks:true would override the
	// hooks materialized in settings.json by MaterializeClaudeSettings.
	settingsPrecedenceFixtureWriteSettingsLocal(t, ws, `{"disableAllHooks":true}`)
	rc := settingsPrecedenceFixtureRunCtx(t, ws)

	_, _, err := daemon.ExportedBuildClaudeLaunchSpec(context.Background(), rc)
	if err == nil {
		t.Fatal("TestSettingsPrecedence_DisableAllHooks_Rejected: expected error, got nil")
	}
	if !errors.Is(err, handler.ErrStructural) {
		t.Errorf("error does not wrap ErrStructural: %v", err)
	}
}

// TestSettingsPrecedence_HooksBlock_Rejected verifies that a non-empty hooks
// block in settings.local.json causes buildClaudeLaunchSpec to fail. Because
// settings.local.json takes precedence over settings.json in Claude's settings
// hierarchy, a hooks block in settings.local.json would shadow the bridge-required
// entries written to settings.json by MaterializeClaudeSettings.
func TestSettingsPrecedence_HooksBlock_Rejected(t *testing.T) {
	t.Parallel()

	ws := settingsPrecedenceFixtureWorkspace(t)
	// A hooks block in settings.local.json would shadow the bridge's hooks in
	// settings.json, silently preventing SessionStart/Stop events from reaching
	// the bridge relay per CHB-024.
	settingsPrecedenceFixtureWriteSettingsLocal(t, ws, `{"hooks":{"Stop":[{"matcher":"","hooks":[]}]}}`)
	rc := settingsPrecedenceFixtureRunCtx(t, ws)

	_, _, err := daemon.ExportedBuildClaudeLaunchSpec(context.Background(), rc)
	if err == nil {
		t.Fatal("TestSettingsPrecedence_HooksBlock_Rejected: expected error, got nil")
	}
	if !errors.Is(err, handler.ErrStructural) {
		t.Errorf("error does not wrap ErrStructural: %v", err)
	}
}

// TestSettingsPrecedence_MalformedSettingsLocal_Rejected verifies that a
// malformed settings.local.json causes buildClaudeLaunchSpec to fail. The
// daemon treats a malformed file as an unquantifiable shadow risk per CHB-024.
func TestSettingsPrecedence_MalformedSettingsLocal_Rejected(t *testing.T) {
	t.Parallel()

	ws := settingsPrecedenceFixtureWorkspace(t)
	settingsPrecedenceFixtureWriteSettingsLocal(t, ws, `{not valid json`)
	rc := settingsPrecedenceFixtureRunCtx(t, ws)

	_, _, err := daemon.ExportedBuildClaudeLaunchSpec(context.Background(), rc)
	if err == nil {
		t.Fatal("TestSettingsPrecedence_MalformedSettingsLocal_Rejected: expected error, got nil")
	}
	if !errors.Is(err, handler.ErrStructural) {
		t.Errorf("error does not wrap ErrStructural: %v", err)
	}
}

// TestSettingsPrecedence_EmptyHooksMap_OK verifies that an explicit empty hooks
// object {"hooks":{}} in settings.local.json is accepted. An empty hooks map
// defines no event-kind entries, so it does not shadow the bridge's entries in
// settings.json.
func TestSettingsPrecedence_EmptyHooksMap_OK(t *testing.T) {
	t.Parallel()

	ws := settingsPrecedenceFixtureWorkspace(t)
	// {"hooks":{}} — empty map: no event-kind shadow, bridge entries in settings.json
	// are still reachable.
	settingsPrecedenceFixtureWriteSettingsLocal(t, ws, `{"hooks":{}}`)
	rc := settingsPrecedenceFixtureRunCtx(t, ws)

	_, _, err := daemon.ExportedBuildClaudeLaunchSpec(context.Background(), rc)
	if err != nil {
		t.Errorf("TestSettingsPrecedence_EmptyHooksMap_OK: unexpected error: %v", err)
	}
}

// TestSettingsPrecedence_MultiEventHooksBlock_Rejected verifies that a hooks
// block covering multiple event-kinds in settings.local.json is rejected. Each
// event-kind entry would shadow the corresponding bridge entries in settings.json.
func TestSettingsPrecedence_MultiEventHooksBlock_Rejected(t *testing.T) {
	t.Parallel()

	ws := settingsPrecedenceFixtureWorkspace(t)
	settingsPrecedenceFixtureWriteSettingsLocal(t, ws,
		`{"hooks":{"SessionStart":[{}],"Stop":[{}],"SessionEnd":[{}]}}`,
	)
	rc := settingsPrecedenceFixtureRunCtx(t, ws)

	_, _, err := daemon.ExportedBuildClaudeLaunchSpec(context.Background(), rc)
	if err == nil {
		t.Fatal("TestSettingsPrecedence_MultiEventHooksBlock_Rejected: expected error, got nil")
	}
	if !errors.Is(err, handler.ErrStructural) {
		t.Errorf("error does not wrap ErrStructural: %v", err)
	}
}

// TestSettingsPrecedence_NonHookSettingsOK verifies that a settings.local.json
// that contains non-hook settings (e.g. permissions, theme) without a hooks
// block is accepted. Non-hook content does not affect the bridge's hooks in
// settings.json and is valid per the spec.
func TestSettingsPrecedence_NonHookSettingsOK(t *testing.T) {
	t.Parallel()

	ws := settingsPrecedenceFixtureWorkspace(t)
	settingsPrecedenceFixtureWriteSettingsLocal(t, ws,
		`{"theme":"dark","permissions":{"allow":["Read","Write"]}}`,
	)
	rc := settingsPrecedenceFixtureRunCtx(t, ws)

	_, _, err := daemon.ExportedBuildClaudeLaunchSpec(context.Background(), rc)
	if err != nil {
		t.Errorf("TestSettingsPrecedence_NonHookSettingsOK: unexpected error: %v", err)
	}
}

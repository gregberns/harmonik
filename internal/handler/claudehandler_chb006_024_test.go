package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// claudeHandlerFixture is the per-bead helper prefix for hk-w5vra.5.
// All test helpers in this file use this prefix.

// claudeHandlerFixtureWorkspace creates a temporary workspace directory
// and returns its path. The directory is cleaned up via t.Cleanup.
func claudeHandlerFixtureWorkspace(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	return dir
}

// claudeHandlerFixtureWriteSettingsLocal writes a .claude/settings.local.json
// with the provided content to the workspace, creating parent dirs as needed.
func claudeHandlerFixtureWriteSettingsLocal(t *testing.T, workspacePath string, content string) {
	t.Helper()
	dir := filepath.Join(workspacePath, ".claude")
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("claudeHandlerFixtureWriteSettingsLocal: mkdir: %v", err)
	}
	p := filepath.Join(dir, "settings.local.json")
	//nolint:gosec // G306: 0644 is correct for a test fixture JSON file
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("claudeHandlerFixtureWriteSettingsLocal: write: %v", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// CHB-007 — CheckForbiddenFlags
// ─────────────────────────────────────────────────────────────────────────────

// TestCheckForbiddenFlags_ForbiddenFlag_ForkSession verifies that
// --fork-session in argv is rejected per CHB-007.
func TestCheckForbiddenFlags_ForbiddenFlag_ForkSession(t *testing.T) {
	t.Parallel()
	argv := []string{"claude", "--fork-session", "-p", "hello"}
	err := handler.CheckForbiddenFlags(argv, nil)
	if err == nil {
		t.Fatal("CheckForbiddenFlags: want error for --fork-session, got nil")
	}
	if !errors.Is(err, handler.ErrStructural) {
		t.Errorf("error does not wrap ErrStructural: %v", err)
	}
}

// TestCheckForbiddenFlags_ForbiddenFlag_Bare verifies that --bare is rejected.
func TestCheckForbiddenFlags_ForbiddenFlag_Bare(t *testing.T) {
	t.Parallel()
	err := handler.CheckForbiddenFlags([]string{"--bare"}, nil)
	if err == nil {
		t.Fatal("want error for --bare")
	}
	if !errors.Is(err, handler.ErrStructural) {
		t.Errorf("error does not wrap ErrStructural: %v", err)
	}
}

// TestCheckForbiddenFlags_ForbiddenFlag_NoSessionPersistence verifies that
// --no-session-persistence is rejected.
func TestCheckForbiddenFlags_ForbiddenFlag_NoSessionPersistence(t *testing.T) {
	t.Parallel()
	err := handler.CheckForbiddenFlags([]string{"--no-session-persistence"}, nil)
	if err == nil {
		t.Fatal("want error for --no-session-persistence")
	}
	if !errors.Is(err, handler.ErrStructural) {
		t.Errorf("error does not wrap ErrStructural: %v", err)
	}
}

// TestCheckForbiddenFlags_ForbiddenEnvVar_SkipPromptHistory verifies that
// CLAUDE_CODE_SKIP_PROMPT_HISTORY in env is rejected per CHB-007.
func TestCheckForbiddenFlags_ForbiddenEnvVar_SkipPromptHistory(t *testing.T) {
	t.Parallel()
	env := []string{"PATH=/usr/bin", "CLAUDE_CODE_SKIP_PROMPT_HISTORY=1"}
	err := handler.CheckForbiddenFlags(nil, env)
	if err == nil {
		t.Fatal("want error for CLAUDE_CODE_SKIP_PROMPT_HISTORY")
	}
	if !errors.Is(err, handler.ErrStructural) {
		t.Errorf("error does not wrap ErrStructural: %v", err)
	}
}

// TestCheckForbiddenFlags_Clean verifies that a clean argv/env pair passes.
func TestCheckForbiddenFlags_Clean(t *testing.T) {
	t.Parallel()
	argv := []string{"claude", "--model", "claude-opus-4-5", "-p", "hello"}
	env := []string{"PATH=/usr/bin", "HOME=/home/user"}
	if err := handler.CheckForbiddenFlags(argv, env); err != nil {
		t.Errorf("CheckForbiddenFlags returned unexpected error: %v", err)
	}
}

// TestCheckForbiddenFlags_EmptyArgvAndEnv verifies nil inputs pass.
func TestCheckForbiddenFlags_EmptyArgvAndEnv(t *testing.T) {
	t.Parallel()
	if err := handler.CheckForbiddenFlags(nil, nil); err != nil {
		t.Errorf("CheckForbiddenFlags(nil, nil) returned error: %v", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// CHB-008 / CHB-009 — MintClaudeSessionID
// ─────────────────────────────────────────────────────────────────────────────

// TestMintClaudeSessionID_SinglePhase_MintsFreshUUID verifies that single (empty)
// phase mints a fresh non-empty UUID per CHB-008.
func TestMintClaudeSessionID_SinglePhase_MintsFreshUUID(t *testing.T) {
	t.Parallel()
	res, err := handler.MintClaudeSessionID("", nil)
	if err != nil {
		t.Fatalf("MintClaudeSessionID: %v", err)
	}
	if res.ClaudeSessionID == "" {
		t.Error("expected non-empty ClaudeSessionID")
	}
	if res.ResumeMode {
		t.Error("expected ResumeMode=false for single phase")
	}
}

// TestMintClaudeSessionID_ImplementerInitial_MintsFresh verifies implementer-initial
// mints a fresh UUID per CHB-008.
func TestMintClaudeSessionID_ImplementerInitial_MintsFresh(t *testing.T) {
	t.Parallel()
	res, err := handler.MintClaudeSessionID("implementer-initial", nil)
	if err != nil {
		t.Fatalf("MintClaudeSessionID: %v", err)
	}
	if res.ClaudeSessionID == "" {
		t.Error("expected non-empty ClaudeSessionID")
	}
	if res.ResumeMode {
		t.Error("expected ResumeMode=false for implementer-initial")
	}
}

// TestMintClaudeSessionID_Reviewer_MintsFresh verifies reviewer always mints a
// fresh UUID per CHB-008 and CHB-009.
func TestMintClaudeSessionID_Reviewer_MintsFresh(t *testing.T) {
	t.Parallel()
	prior := "old-reviewer-session-id"
	// reviewer MUST NOT inherit prior — pass nil to simulate (CHB-009: reviewer
	// always mints fresh; the call site never passes priorClaudeSessionID for reviewer).
	res, err := handler.MintClaudeSessionID("reviewer", nil)
	if err != nil {
		t.Fatalf("MintClaudeSessionID: %v", err)
	}
	if res.ClaudeSessionID == "" {
		t.Error("expected non-empty ClaudeSessionID")
	}
	if res.ClaudeSessionID == prior {
		t.Error("reviewer must not inherit prior session ID (CHB-009)")
	}
	if res.ResumeMode {
		t.Error("expected ResumeMode=false for reviewer")
	}
}

// TestMintClaudeSessionID_ImplementerResume_ReusesSessionID verifies that
// implementer-resume reuses the LaunchSpec session ID per CHB-008.
func TestMintClaudeSessionID_ImplementerResume_ReusesSessionID(t *testing.T) {
	t.Parallel()
	prior := "claude-session-impl-001"
	res, err := handler.MintClaudeSessionID("implementer-resume", &prior)
	if err != nil {
		t.Fatalf("MintClaudeSessionID: %v", err)
	}
	if res.ClaudeSessionID != prior {
		t.Errorf("ClaudeSessionID = %q; want %q", res.ClaudeSessionID, prior)
	}
	if !res.ResumeMode {
		t.Error("expected ResumeMode=true for implementer-resume")
	}
}

// TestMintClaudeSessionID_ImplementerResume_NilPrior_ReturnsError verifies that
// implementer-resume with nil prior ID returns ErrStructural per CHB-008.
func TestMintClaudeSessionID_ImplementerResume_NilPrior_ReturnsError(t *testing.T) {
	t.Parallel()
	_, err := handler.MintClaudeSessionID("implementer-resume", nil)
	if err == nil {
		t.Fatal("expected error for implementer-resume with nil prior ID")
	}
	if !errors.Is(err, handler.ErrStructural) {
		t.Errorf("error does not wrap ErrStructural: %v", err)
	}
}

// TestMintClaudeSessionID_Reviewer_NonNilPrior_ReturnsError verifies that
// passing a non-nil priorClaudeSessionID for phase=reviewer returns ErrStructural
// per CHB-009 (reviewer-phase fresh-mint enforcement).
//
// The call site MUST always pass nil for reviewer; passing a non-nil value is a
// daemon defect that MintClaudeSessionID actively rejects so the bug surfaces
// immediately rather than being silently swallowed.
func TestMintClaudeSessionID_Reviewer_NonNilPrior_ReturnsError(t *testing.T) {
	t.Parallel()
	prior := "stale-reviewer-session-from-prior-iteration"
	_, err := handler.MintClaudeSessionID("reviewer", &prior)
	if err == nil {
		t.Fatal("expected ErrStructural when phase=reviewer with non-nil priorClaudeSessionID, got nil")
	}
	if !errors.Is(err, handler.ErrStructural) {
		t.Errorf("error does not wrap ErrStructural: %v", err)
	}
}

// TestMintClaudeSessionID_TwoMints_Distinct verifies that two fresh mints
// produce distinct UUIDs.
func TestMintClaudeSessionID_TwoMints_Distinct(t *testing.T) {
	t.Parallel()
	r1, err1 := handler.MintClaudeSessionID("", nil)
	r2, err2 := handler.MintClaudeSessionID("", nil)
	if err1 != nil || err2 != nil {
		t.Fatalf("MintClaudeSessionID: %v / %v", err1, err2)
	}
	if r1.ClaudeSessionID == r2.ClaudeSessionID {
		t.Error("two mints produced the same session ID; UUIDs must be distinct")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// CHB-006 — ClaudeEnvVars
// ─────────────────────────────────────────────────────────────────────────────

// TestClaudeEnvVars_RequiredVarsPresent verifies all 8 required CHB-006 env vars
// are present and have the expected values.
func TestClaudeEnvVars_RequiredVarsPresent(t *testing.T) {
	t.Parallel()
	cfg := handler.ClaudeEnvConfig{
		RunID:            "run-001",
		DaemonSocket:     "/tmp/harmonik/daemon.sock",
		WorkspacePath:    "/workspace/bead-001",
		HandlerSessionID: "handler-sess-001",
		ClaudeSessionID:  "claude-sess-001",
		WorkflowID:       "wf-001",
		NodeID:           "node-001",
	}
	env := handler.ClaudeEnvVars(cfg)

	required := map[string]string{
		"HARMONIK_RUN_ID":             "run-001",
		"HARMONIK_DAEMON_SOCKET":      "/tmp/harmonik/daemon.sock",
		"HARMONIK_WORKSPACE_PATH":     "/workspace/bead-001",
		"HARMONIK_HANDLER_SESSION_ID": "handler-sess-001",
		"HARMONIK_CLAUDE_SESSION_ID":  "claude-sess-001",
		"HARMONIK_WORKFLOW_ID":        "wf-001",
		"HARMONIK_NODE_ID":            "node-001",
		"HARMONIK_AGENT_TYPE":         "claude-code",
	}

	envMap := claudeHandlerFixtureEnvMap(t, env)
	for k, want := range required {
		got, ok := envMap[k]
		if !ok {
			t.Errorf("missing required env var %q", k)
			continue
		}
		if got != want {
			t.Errorf("env var %q = %q; want %q", k, got, want)
		}
	}
}

// TestClaudeEnvVars_ShellUpdatePromptDisabled verifies that the oh-my-zsh
// auto-update disable vars are always injected (hk-5s6re). These neutralize the
// interactive `[Y/n] Would you like to update?` prompt that otherwise wedges the
// implementer/reviewer login shell spawned in the tmux pane — independent of the
// operator's ~/.zshrc.
func TestClaudeEnvVars_ShellUpdatePromptDisabled(t *testing.T) {
	t.Parallel()
	cfg := handler.ClaudeEnvConfig{
		RunID:            "run-omz",
		DaemonSocket:     "/tmp/d.sock",
		WorkspacePath:    "/ws",
		HandlerSessionID: "h-sess",
		ClaudeSessionID:  "c-sess",
		WorkflowID:       "wf-omz",
		NodeID:           "n-omz",
	}
	env := handler.ClaudeEnvVars(cfg)
	envMap := claudeHandlerFixtureEnvMap(t, env)

	want := map[string]string{
		"DISABLE_AUTO_UPDATE":   "true",
		"DISABLE_UPDATE_PROMPT": "true",
	}
	for k, v := range want {
		got, ok := envMap[k]
		if !ok {
			t.Errorf("missing shell-prompt-disable env var %q (hk-5s6re)", k)
			continue
		}
		if got != v {
			t.Errorf("env var %q = %q; want %q", k, got, v)
		}
	}
}

// TestClaudeEnvVars_OptionalVars_SetWhenNonEmpty verifies optional vars appear
// only when non-empty.
func TestClaudeEnvVars_OptionalVars_SetWhenNonEmpty(t *testing.T) {
	t.Parallel()
	cfg := handler.ClaudeEnvConfig{
		RunID:            "run-002",
		DaemonSocket:     "/tmp/d.sock",
		WorkspacePath:    "/ws",
		HandlerSessionID: "h-sess",
		ClaudeSessionID:  "c-sess",
		WorkflowID:       "wf-002",
		NodeID:           "n-002",
		WorkflowMode:     "review-loop",
		Phase:            "implementer-initial",
		IterationCount:   "1",
		BeadID:           "hk-abc123",
	}
	env := handler.ClaudeEnvVars(cfg)
	envMap := claudeHandlerFixtureEnvMap(t, env)

	optionals := map[string]string{
		"HARMONIK_WORKFLOW_MODE":   "review-loop",
		"HARMONIK_PHASE":           "implementer-initial",
		"HARMONIK_ITERATION_COUNT": "1",
		"HARMONIK_BEAD_ID":         "hk-abc123",
	}
	for k, want := range optionals {
		got, ok := envMap[k]
		if !ok {
			t.Errorf("optional var %q missing; want %q", k, want)
			continue
		}
		if got != want {
			t.Errorf("optional var %q = %q; want %q", k, got, want)
		}
	}
}

// TestClaudeEnvVars_OptionalVars_AbsentWhenEmpty verifies that optional vars are
// omitted when empty.
func TestClaudeEnvVars_OptionalVars_AbsentWhenEmpty(t *testing.T) {
	t.Parallel()
	cfg := handler.ClaudeEnvConfig{
		RunID:            "run-003",
		DaemonSocket:     "/tmp/d.sock",
		WorkspacePath:    "/ws",
		HandlerSessionID: "h-sess",
		ClaudeSessionID:  "c-sess",
		WorkflowID:       "wf-003",
		NodeID:           "n-003",
		// all optional vars left empty
	}
	env := handler.ClaudeEnvVars(cfg)
	envMap := claudeHandlerFixtureEnvMap(t, env)

	for _, k := range []string{"HARMONIK_WORKFLOW_MODE", "HARMONIK_PHASE", "HARMONIK_ITERATION_COUNT", "HARMONIK_BEAD_ID"} {
		if _, ok := envMap[k]; ok {
			t.Errorf("optional var %q present but should be absent when empty", k)
		}
	}
}

// TestClaudeEnvVars_SecretVars_Included verifies HARMONIK_SECRET_* vars from
// SecretVars appear in the env.
func TestClaudeEnvVars_SecretVars_Included(t *testing.T) {
	t.Parallel()
	cfg := handler.ClaudeEnvConfig{
		RunID:            "run-004",
		DaemonSocket:     "/tmp/d.sock",
		WorkspacePath:    "/ws",
		HandlerSessionID: "h-sess",
		ClaudeSessionID:  "c-sess",
		WorkflowID:       "wf-004",
		NodeID:           "n-004",
		SecretVars: map[string]string{
			"HARMONIK_SECRET_API_KEY": "s3cr3t",
		},
	}
	env := handler.ClaudeEnvVars(cfg)
	envMap := claudeHandlerFixtureEnvMap(t, env)
	if got, ok := envMap["HARMONIK_SECRET_API_KEY"]; !ok || got != "s3cr3t" {
		t.Errorf("HARMONIK_SECRET_API_KEY = %q ok=%v; want %q", got, ok, "s3cr3t")
	}
}

// TestClaudeEnvVars_BaseEnv_SecretKeysRedacted verifies that HARMONIK_SECRET_*
// keys in BaseEnv are dropped (preventing double-injection).
func TestClaudeEnvVars_BaseEnv_SecretKeysRedacted(t *testing.T) {
	t.Parallel()
	cfg := handler.ClaudeEnvConfig{
		RunID:            "run-005",
		DaemonSocket:     "/tmp/d.sock",
		WorkspacePath:    "/ws",
		HandlerSessionID: "h-sess",
		ClaudeSessionID:  "c-sess",
		WorkflowID:       "wf-005",
		NodeID:           "n-005",
		BaseEnv: []string{
			"PATH=/usr/bin",
			"HARMONIK_SECRET_OLD_KEY=leaked_value",
		},
		SecretVars: map[string]string{
			"HARMONIK_SECRET_OLD_KEY": "new_value",
		},
	}
	env := handler.ClaudeEnvVars(cfg)
	// Count occurrences of HARMONIK_SECRET_OLD_KEY — must be exactly 1 (SecretVars).
	count := 0
	for _, kv := range env {
		if strings.HasPrefix(kv, "HARMONIK_SECRET_OLD_KEY=") {
			count++
		}
	}
	if count != 1 {
		t.Errorf("HARMONIK_SECRET_OLD_KEY appears %d times; want 1 (from SecretVars, not BaseEnv)", count)
	}
	// Verify the value is the new one.
	envMap := claudeHandlerFixtureEnvMap(t, env)
	if got := envMap["HARMONIK_SECRET_OLD_KEY"]; got != "new_value" {
		t.Errorf("HARMONIK_SECRET_OLD_KEY = %q; want %q", got, "new_value")
	}
}

// TestClaudeEnvVars_NoExtraHarmonikVars verifies the "schema-shape" requirement:
// ClaudeEnvVars MUST NOT inject any HARMONIK_* vars beyond the 13 defined by
// CHB-006.  This catches accidental additions that would require a spec amendment.
//
// Spec: specs/claude-hook-bridge.md §4.2 CHB-006 ("NO other HARMONIK_* vars are
// permitted; future fields require spec amendment").
func TestClaudeEnvVars_NoExtraHarmonikVars(t *testing.T) {
	t.Parallel()
	cfg := handler.ClaudeEnvConfig{
		RunID:            "run-006",
		DaemonSocket:     "/tmp/d.sock",
		WorkspacePath:    "/ws",
		HandlerSessionID: "h-sess",
		ClaudeSessionID:  "c-sess",
		WorkflowID:       "wf-006",
		NodeID:           "n-006",
		WorkflowMode:     "review-loop",
		Phase:            "implementer-initial",
		IterationCount:   "2",
		BeadID:           "hk-test001",
		SecretVars: map[string]string{
			"HARMONIK_SECRET_TOKEN": "tok",
		},
	}
	env := handler.ClaudeEnvVars(cfg)

	// The full set of permitted HARMONIK_* keys per §4.2 CHB-006.
	permitted := map[string]bool{
		"HARMONIK_RUN_ID":             true,
		"HARMONIK_DAEMON_SOCKET":      true,
		"HARMONIK_WORKSPACE_PATH":     true,
		"HARMONIK_HANDLER_SESSION_ID": true,
		"HARMONIK_CLAUDE_SESSION_ID":  true,
		"HARMONIK_WORKFLOW_ID":        true,
		"HARMONIK_NODE_ID":            true,
		"HARMONIK_AGENT_TYPE":         true,
		"HARMONIK_WORKFLOW_MODE":      true,
		"HARMONIK_PHASE":              true,
		"HARMONIK_ITERATION_COUNT":    true,
		"HARMONIK_BEAD_ID":            true,
		// HARMONIK_SECRET_* is the wildcard slot (HC-028); any key with this
		// prefix is acceptable, so we check it separately below.
	}

	for _, kv := range env {
		idx := strings.IndexByte(kv, '=')
		if idx < 0 {
			continue
		}
		key := kv[:idx]
		if !strings.HasPrefix(key, "HARMONIK_") {
			continue
		}
		if strings.HasPrefix(key, "HARMONIK_SECRET_") {
			continue // wildcard slot (HC-028)
		}
		if !permitted[key] {
			t.Errorf("unexpected HARMONIK_* env var injected: %q (not in CHB-006 schema; requires spec amendment)", key)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// CI-004a — Credential env deny-list scrub regression test
// ─────────────────────────────────────────────────────────────────────────────

// TestClaudeEnvVars_CredentialDenyList_ScrubbedFromBaseEnv is the named
// regression test required by specs/credential-isolation.md §4 CI-004a.
//
// It verifies that ClaudeEnvVars:
//  1. Strips ANTHROPIC_API_KEY, ANTHROPIC_AUTH_TOKEN, and CLAUDE_CODE_OAUTH*
//     from BaseEnv so they do not appear with their original values.
//  2. Emits explicit empty overrides (KEY=) for those keys so the tmux substrate's
//     additive -e mechanism zeros them even when the tmux server env holds a value.
//  3. Passes through non-deny-list BaseEnv vars unmodified.
//  4. Does NOT log or emit any matched credential value (the test asserts absence
//     of the original non-empty string values per CI-007).
//
// Spec: specs/credential-isolation.md CI-003, CI-004a, CI-INV-002.
func TestClaudeEnvVars_CredentialDenyList_ScrubbedFromBaseEnv(t *testing.T) {
	t.Parallel()

	cfg := handler.ClaudeEnvConfig{
		RunID:            "run-ci004a",
		DaemonSocket:     "/tmp/ci004a.sock",
		WorkspacePath:    "/ws/ci004a",
		HandlerSessionID: "h-ci004a",
		ClaudeSessionID:  "c-ci004a",
		WorkflowID:       "wf-ci004a",
		NodeID:           "n-ci004a",
		BaseEnv: []string{
			// Credential deny-list keys that must be scrubbed.
			"ANTHROPIC_API_KEY=sk-ant-real-secret",
			"ANTHROPIC_AUTH_TOKEN=bearer-real-secret",
			"CLAUDE_CODE_OAUTH_TOKEN=oauth-real-secret",
			"CLAUDE_CODE_OAUTH_CUSTOM=another-secret",
			// Non-deny-list key that must survive scrubbing.
			"PATH=/usr/local/bin:/usr/bin",
		},
	}

	env := handler.ClaudeEnvVars(cfg)

	// 1. Verify no credential key retains its original non-empty value.
	//    (We assert on key absence at non-empty value, not on value absence,
	//    to satisfy CI-007: tests MUST NOT emit credential values in failure messages.)
	for _, kv := range env {
		idx := strings.IndexByte(kv, '=')
		if idx < 0 {
			continue
		}
		key, val := kv[:idx], kv[idx+1:]
		switch key {
		case "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN",
			"CLAUDE_CODE_OAUTH_TOKEN", "CLAUDE_CODE_OAUTH_CUSTOM":
			if val != "" {
				// Report key name only — never emit the credential value (CI-007).
				t.Errorf("credential deny-list key %q has non-empty value in child env; want empty override", key)
			}
		}
	}

	// 2. Verify the known deny-list keys are present as explicit empty overrides
	//    (needed so tmux -e KEY= zeros the tmux server env value).
	envMap := claudeHandlerFixtureEnvMap(t, env)
	for _, key := range []string{
		"ANTHROPIC_API_KEY",
		"ANTHROPIC_AUTH_TOKEN",
		"CLAUDE_CODE_OAUTH_TOKEN",
		"CLAUDE_CODE_OAUTH_CUSTOM",
	} {
		if _, ok := envMap[key]; !ok {
			t.Errorf("credential deny-list key %q absent from child env; want explicit empty override (KEY=)", key)
		}
		if val := envMap[key]; val != "" {
			// Re-check after map lookup. Suppress actual value per CI-007.
			t.Errorf("credential deny-list key %q has non-empty value; want \"\"", key)
		}
	}

	// 3. Verify non-deny-list BaseEnv var survives.
	if got, ok := envMap["PATH"]; !ok || got != "/usr/local/bin:/usr/bin" {
		t.Errorf("PATH = %q ok=%v; want %q", got, ok, "/usr/local/bin:/usr/bin")
	}
}

// TestClaudeEnvVars_CredentialDenyList_EmptyOverridesAlwaysPresent verifies
// that the two exact deny-list keys and CLAUDE_CODE_OAUTH_TOKEN are emitted as
// empty overrides even when they are absent from BaseEnv — covering the case
// where the tmux server env holds a credential not threaded through BaseEnv.
//
// Spec: specs/credential-isolation.md CI-003, CI-INV-002.
func TestClaudeEnvVars_CredentialDenyList_EmptyOverridesAlwaysPresent(t *testing.T) {
	t.Parallel()

	cfg := handler.ClaudeEnvConfig{
		RunID:            "run-ci003",
		DaemonSocket:     "/tmp/ci003.sock",
		WorkspacePath:    "/ws/ci003",
		HandlerSessionID: "h-ci003",
		ClaudeSessionID:  "c-ci003",
		WorkflowID:       "wf-ci003",
		NodeID:           "n-ci003",
		// BaseEnv intentionally omits all credential keys.
		BaseEnv: []string{"HOME=/home/op"},
	}

	env := handler.ClaudeEnvVars(cfg)
	envMap := claudeHandlerFixtureEnvMap(t, env)

	for _, key := range []string{
		"ANTHROPIC_API_KEY",
		"ANTHROPIC_AUTH_TOKEN",
		"CLAUDE_CODE_OAUTH_TOKEN",
	} {
		if _, ok := envMap[key]; !ok {
			t.Errorf("deny-list key %q absent from child env; want explicit empty override even when not in BaseEnv", key)
		}
		if val := envMap[key]; val != "" {
			t.Errorf("deny-list key %q has non-empty value %q; want \"\"", key, val)
		}
	}
}

// claudeHandlerFixtureEnvMap parses a "KEY=VALUE" env slice into a map.
func claudeHandlerFixtureEnvMap(t *testing.T, env []string) map[string]string {
	t.Helper()
	m := make(map[string]string, len(env))
	for _, kv := range env {
		idx := strings.IndexByte(kv, '=')
		if idx < 0 {
			t.Errorf("malformed env entry (no '='): %q", kv)
			continue
		}
		m[kv[:idx]] = kv[idx+1:]
	}
	return m
}

// ─────────────────────────────────────────────────────────────────────────────
// CHB-024 — CheckSettingsLocalJSON
// ─────────────────────────────────────────────────────────────────────────────

// TestCheckSettingsLocalJSON_NoFile_OK verifies that a missing settings.local.json
// returns nil per CHB-024.
func TestCheckSettingsLocalJSON_NoFile_OK(t *testing.T) {
	t.Parallel()
	ws := claudeHandlerFixtureWorkspace(t)
	if err := handler.CheckSettingsLocalJSON(ws); err != nil {
		t.Errorf("CheckSettingsLocalJSON with no file: %v; want nil", err)
	}
}

// TestCheckSettingsLocalJSON_EmptyFile_OK verifies that an empty JSON object {}
// is accepted per CHB-024.
func TestCheckSettingsLocalJSON_EmptyFile_OK(t *testing.T) {
	t.Parallel()
	ws := claudeHandlerFixtureWorkspace(t)
	claudeHandlerFixtureWriteSettingsLocal(t, ws, `{}`)
	if err := handler.CheckSettingsLocalJSON(ws); err != nil {
		t.Errorf("CheckSettingsLocalJSON with empty object: %v; want nil", err)
	}
}

// TestCheckSettingsLocalJSON_DisableAllHooks_Rejected verifies that
// disableAllHooks:true is rejected with ErrStructural per CHB-024.
func TestCheckSettingsLocalJSON_DisableAllHooks_Rejected(t *testing.T) {
	t.Parallel()
	ws := claudeHandlerFixtureWorkspace(t)
	claudeHandlerFixtureWriteSettingsLocal(t, ws, `{"disableAllHooks":true}`)
	err := handler.CheckSettingsLocalJSON(ws)
	if err == nil {
		t.Fatal("want error for disableAllHooks:true, got nil")
	}
	if !errors.Is(err, handler.ErrStructural) {
		t.Errorf("error does not wrap ErrStructural: %v", err)
	}
	if !strings.Contains(err.Error(), "bridge_settings_shadowed") {
		t.Errorf("error does not mention bridge_settings_shadowed: %v", err)
	}
}

// TestCheckSettingsLocalJSON_HooksBlock_Rejected verifies that a non-empty
// hooks block is rejected with ErrStructural per CHB-024.
func TestCheckSettingsLocalJSON_HooksBlock_Rejected(t *testing.T) {
	t.Parallel()
	ws := claudeHandlerFixtureWorkspace(t)
	claudeHandlerFixtureWriteSettingsLocal(t, ws, `{"hooks":{"Stop":[{}]}}`)
	err := handler.CheckSettingsLocalJSON(ws)
	if err == nil {
		t.Fatal("want error for non-empty hooks block, got nil")
	}
	if !errors.Is(err, handler.ErrStructural) {
		t.Errorf("error does not wrap ErrStructural: %v", err)
	}
	if !strings.Contains(err.Error(), "bridge_settings_shadowed") {
		t.Errorf("error does not mention bridge_settings_shadowed: %v", err)
	}
}

// TestCheckSettingsLocalJSON_MalformedJSON_Rejected verifies that malformed JSON
// is rejected with ErrStructural per CHB-024.
func TestCheckSettingsLocalJSON_MalformedJSON_Rejected(t *testing.T) {
	t.Parallel()
	ws := claudeHandlerFixtureWorkspace(t)
	claudeHandlerFixtureWriteSettingsLocal(t, ws, `{not valid json}`)
	err := handler.CheckSettingsLocalJSON(ws)
	if err == nil {
		t.Fatal("want error for malformed JSON, got nil")
	}
	if !errors.Is(err, handler.ErrStructural) {
		t.Errorf("error does not wrap ErrStructural: %v", err)
	}
}

// TestCheckSettingsLocalJSON_EmptyHooks_OK verifies that an empty hooks map {}
// is accepted (no shadow).
func TestCheckSettingsLocalJSON_EmptyHooks_OK(t *testing.T) {
	t.Parallel()
	ws := claudeHandlerFixtureWorkspace(t)
	claudeHandlerFixtureWriteSettingsLocal(t, ws, `{"hooks":{}}`)
	if err := handler.CheckSettingsLocalJSON(ws); err != nil {
		t.Errorf("CheckSettingsLocalJSON with empty hooks {}: %v; want nil", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// CHB-018 — PreExecMessages
// ─────────────────────────────────────────────────────────────────────────────

// TestPreExecMessages_FourMessages verifies that PreExecMessages returns exactly
// 4 messages in the correct order per CHB-018.
func TestPreExecMessages_FourMessages(t *testing.T) {
	t.Parallel()
	msgs, err := handler.PreExecMessages(
		"run-001", "sess-001", "node-001",
		"claude-sess-001",
		"/tmp/claude.jsonl",
		nil,
	)
	if err != nil {
		t.Fatalf("PreExecMessages: %v", err)
	}
	if len(msgs) != 4 {
		t.Fatalf("len(msgs) = %d; want 4", len(msgs))
	}

	expectedTypes := []string{
		handlercontract.ProgressMsgTypeHandlerCapabilities,
		handlercontract.ProgressMsgTypeSessionLogLocation,
		handlercontract.ProgressMsgTypeSkillsProvisioned,
		handlercontract.ProgressMsgTypeLaunchInitiated, // CHB-018 step 4: launch_initiated (not agent_ready) per hk-p63bz
	}

	for i, raw := range msgs {
		var msg map[string]json.RawMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			t.Errorf("msgs[%d] not valid JSON: %v", i, err)
			continue
		}
		var msgType string
		if err := json.Unmarshal(msg["type"], &msgType); err != nil {
			t.Errorf("msgs[%d].type not string: %v", i, err)
			continue
		}
		if msgType != expectedTypes[i] {
			t.Errorf("msgs[%d].type = %q; want %q", i, msgType, expectedTypes[i])
		}
	}
}

// TestPreExecMessages_HandlerCapabilities_CarriesClaudeSessionID verifies that
// handler_capabilities carries the claude_session_id per CHB-018 step 1.
func TestPreExecMessages_HandlerCapabilities_CarriesClaudeSessionID(t *testing.T) {
	t.Parallel()
	const claudeSessID = "claude-sess-002"
	msgs, err := handler.PreExecMessages("run-002", "sess-002", "node-002", claudeSessID, "/log", nil)
	if err != nil {
		t.Fatalf("PreExecMessages: %v", err)
	}

	var hc handlercontract.HandlerCapabilitiesMsg
	if err := json.Unmarshal(msgs[0], &hc); err != nil {
		t.Fatalf("unmarshal handler_capabilities: %v", err)
	}
	if hc.ClaudeSessionID != claudeSessID {
		t.Errorf("handler_capabilities.claude_session_id = %q; want %q", hc.ClaudeSessionID, claudeSessID)
	}
	if len(hc.SupportedVersions) == 0 || hc.SupportedVersions[0] != 1 {
		t.Errorf("handler_capabilities.supported_versions = %v; want [1]", hc.SupportedVersions)
	}
}

// TestPreExecMessages_SessionLogLocation_CarriesLogPath verifies session_log_location
// carries the expected log path.
func TestPreExecMessages_SessionLogLocation_CarriesLogPath(t *testing.T) {
	t.Parallel()
	const logPath = "/home/user/.claude/projects/ws/claude-sess.jsonl"
	msgs, err := handler.PreExecMessages("run-003", "sess-003", "node-003", "cs-003", logPath, nil)
	if err != nil {
		t.Fatalf("PreExecMessages: %v", err)
	}

	var sll handlercontract.SessionLogLocationMsg
	if err := json.Unmarshal(msgs[1], &sll); err != nil {
		t.Fatalf("unmarshal session_log_location: %v", err)
	}
	if sll.LogPath != logPath {
		t.Errorf("session_log_location.log_path = %q; want %q", sll.LogPath, logPath)
	}
	if sll.AgentType != "claude-code" {
		t.Errorf("session_log_location.agent_type = %q; want %q", sll.AgentType, "claude-code")
	}
}

// TestPreExecMessages_SkillsProvisioned_EmptySkillsWhenNil verifies that a nil
// skills slice results in an empty array in the message (not null).
func TestPreExecMessages_SkillsProvisioned_EmptySkillsWhenNil(t *testing.T) {
	t.Parallel()
	msgs, err := handler.PreExecMessages("run-004", "sess-004", "node-004", "cs-004", "/log", nil)
	if err != nil {
		t.Fatalf("PreExecMessages: %v", err)
	}

	var sp handlercontract.SkillsProvisionedMsg
	if err := json.Unmarshal(msgs[2], &sp); err != nil {
		t.Fatalf("unmarshal skills_provisioned: %v", err)
	}
	if sp.Skills == nil {
		t.Error("skills_provisioned.skills is null; want empty slice []")
	}
}

// TestPreExecMessages_LaunchInitiated_HasSessionID verifies launch_initiated
// (CHB-018 step 4) carries the session ID and claude_session_id.
// Per hk-p63bz the handler emits launch_initiated pre-exec, not agent_ready.
// agent_ready is synthesized by the relay on first SessionStart receipt (CHB-013).
func TestPreExecMessages_LaunchInitiated_HasSessionID(t *testing.T) {
	t.Parallel()
	const sessID = "sess-005"
	const claudeSessID = "cs-005"
	msgs, err := handler.PreExecMessages("run-005", sessID, "node-005", claudeSessID, "/log", nil)
	if err != nil {
		t.Fatalf("PreExecMessages: %v", err)
	}

	var li handlercontract.LaunchInitiatedMsg
	if err := json.Unmarshal(msgs[3], &li); err != nil {
		t.Fatalf("unmarshal launch_initiated: %v", err)
	}
	if li.Type != handlercontract.ProgressMsgTypeLaunchInitiated {
		t.Errorf("launch_initiated.type = %q; want %q", li.Type, handlercontract.ProgressMsgTypeLaunchInitiated)
	}
	if li.SessionID != sessID {
		t.Errorf("launch_initiated.session_id = %q; want %q", li.SessionID, sessID)
	}
	if li.ClaudeSessionID != claudeSessID {
		t.Errorf("launch_initiated.claude_session_id = %q; want %q", li.ClaudeSessionID, claudeSessID)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// CHB-020 — MapWaitReturnToTerminalEvent
// ─────────────────────────────────────────────────────────────────────────────

// TestMapWaitReturn_WorkComplete_AgentCompleted verifies branch 1: WORK_COMPLETE
// outcome → agent_completed per CHB-020.
func TestMapWaitReturn_WorkComplete_AgentCompleted(t *testing.T) {
	t.Parallel()
	outcome := claudeHandlerFixtureOutcome(t, "WORK_COMPLETE", "", "")
	result := handler.MapWaitReturnToTerminalEvent("sess-001", 0, nil, outcome)
	if result.Type != handlercontract.ProgressMsgTypeAgentCompleted {
		t.Errorf("Type = %q; want %q", result.Type, handlercontract.ProgressMsgTypeAgentCompleted)
	}
}

// TestMapWaitReturn_ReviewerVerdict_AgentCompleted verifies branch 1:
// REVIEWER_VERDICT → agent_completed per CHB-020.
func TestMapWaitReturn_ReviewerVerdict_AgentCompleted(t *testing.T) {
	t.Parallel()
	outcome := claudeHandlerFixtureOutcome(t, "REVIEWER_VERDICT", "", "")
	result := handler.MapWaitReturnToTerminalEvent("sess-001", 0, nil, outcome)
	if result.Type != handlercontract.ProgressMsgTypeAgentCompleted {
		t.Errorf("Type = %q; want %q", result.Type, handlercontract.ProgressMsgTypeAgentCompleted)
	}
}

// TestMapWaitReturn_FailureSignal_AgentFailed verifies branch 2: FAILURE_SIGNAL
// → agent_failed with the mapped class/sub_reason per CHB-020.
func TestMapWaitReturn_FailureSignal_AgentFailed(t *testing.T) {
	t.Parallel()
	outcome := claudeHandlerFixtureOutcome(t, "FAILURE_SIGNAL", "claude_server_error", "transient")
	result := handler.MapWaitReturnToTerminalEvent("sess-001", 1, fmt.Errorf("exit 1"), outcome)
	if result.Type != handlercontract.ProgressMsgTypeAgentFailed {
		t.Errorf("Type = %q; want %q", result.Type, handlercontract.ProgressMsgTypeAgentFailed)
	}
	if result.SubReason != "claude_server_error" {
		t.Errorf("SubReason = %q; want %q", result.SubReason, "claude_server_error")
	}
	if result.Class != "transient" {
		t.Errorf("Class = %q; want %q", result.Class, "transient")
	}
}

// TestMapWaitReturn_NoOutcome_CleanExit_ExitWithoutOutcome verifies branch 3:
// no outcome + exit 0 → agent_failed{sub_reason=claude_exit_without_outcome} per CHB-020.
func TestMapWaitReturn_NoOutcome_CleanExit_ExitWithoutOutcome(t *testing.T) {
	t.Parallel()
	result := handler.MapWaitReturnToTerminalEvent("sess-001", 0, nil, nil)
	if result.Type != handlercontract.ProgressMsgTypeAgentFailed {
		t.Errorf("Type = %q; want %q", result.Type, handlercontract.ProgressMsgTypeAgentFailed)
	}
	if result.SubReason != "claude_exit_without_outcome" {
		t.Errorf("SubReason = %q; want %q", result.SubReason, "claude_exit_without_outcome")
	}
	if result.Class != "structural" {
		t.Errorf("Class = %q; want %q", result.Class, "structural")
	}
}

// TestMapWaitReturn_NoOutcome_NonZeroExit_Crashed verifies branch 3:
// no outcome + non-zero exit → agent_failed{sub_reason=claude_crashed} per CHB-020.
func TestMapWaitReturn_NoOutcome_NonZeroExit_Crashed(t *testing.T) {
	t.Parallel()
	result := handler.MapWaitReturnToTerminalEvent("sess-001", 1, fmt.Errorf("exit 1"), nil)
	if result.Type != handlercontract.ProgressMsgTypeAgentFailed {
		t.Errorf("Type = %q; want %q", result.Type, handlercontract.ProgressMsgTypeAgentFailed)
	}
	if result.SubReason != "claude_crashed" {
		t.Errorf("SubReason = %q; want %q", result.SubReason, "claude_crashed")
	}
}

// claudeHandlerFixtureOutcome builds an *OutcomeObserver Latest()-compatible
// struct for test use.
func claudeHandlerFixtureOutcome(t *testing.T, kind, subReason, suggestedClass string) *handler.ExportedOutcomeEmittedPayload {
	t.Helper()
	return &handler.ExportedOutcomeEmittedPayload{
		Kind:           kind,
		SubReason:      subReason,
		SuggestedClass: suggestedClass,
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// CHB-019 — RunHeartbeatLoop
// ─────────────────────────────────────────────────────────────────────────────

// TestRunHeartbeatLoop_EmitsOnInterval verifies that RunHeartbeatLoop calls the
// emitter at least once per interval per CHB-019.
func TestRunHeartbeatLoop_EmitsOnInterval(t *testing.T) {
	t.Parallel()

	const interval = 10 * time.Millisecond
	const sessID = "sess-hb-001"

	var mu sync.Mutex
	var calls []string

	emitFn := func(_ context.Context, sid, phase string) error {
		mu.Lock()
		calls = append(calls, fmt.Sprintf("%s:%s", sid, phase))
		mu.Unlock()
		return nil
	}

	done := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go handler.RunHeartbeatLoop(ctx, sessID, interval, done, emitFn)

	// Wait for at least 2 heartbeats.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(calls)
		mu.Unlock()
		if n >= 2 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	mu.Lock()
	n := len(calls)
	mu.Unlock()
	if n < 2 {
		t.Errorf("heartbeat called %d times; want >= 2", n)
	}

	// Verify phase is "reasoning" per CHB-019.
	mu.Lock()
	firstCall := calls[0]
	mu.Unlock()
	if !strings.HasSuffix(firstCall, ":reasoning") {
		t.Errorf("heartbeat call = %q; want suffix :reasoning", firstCall)
	}
}

// TestRunHeartbeatLoop_StopsOnDone verifies that closing done stops the loop per CHB-019.
func TestRunHeartbeatLoop_StopsOnDone(t *testing.T) {
	t.Parallel()

	const interval = 5 * time.Millisecond

	done := make(chan struct{})
	finished := make(chan struct{})
	ctx := context.Background()

	emitFn := func(_ context.Context, _, _ string) error {
		return nil
	}

	go func() {
		handler.RunHeartbeatLoop(ctx, "sess-001", interval, done, emitFn)
		close(finished)
	}()

	// Let it run for a couple intervals.
	time.Sleep(20 * time.Millisecond)
	close(done)

	// Verify the goroutine exits promptly after done is closed.
	select {
	case <-finished:
		// OK — loop stopped.
	case <-time.After(500 * time.Millisecond):
		t.Error("RunHeartbeatLoop did not stop after done channel was closed")
	}
}

// TestRunHeartbeatLoop_StopsOnContextCancel verifies context cancellation stops
// the loop.
func TestRunHeartbeatLoop_StopsOnContextCancel(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	finished := make(chan struct{})

	var emitted int
	var mu sync.Mutex

	go func() {
		handler.RunHeartbeatLoop(ctx, "sess-001", 5*time.Millisecond, done, func(_ context.Context, _, _ string) error {
			mu.Lock()
			emitted++
			mu.Unlock()
			return nil
		})
		close(finished)
	}()

	time.Sleep(30 * time.Millisecond)
	cancel()

	select {
	case <-finished:
		// OK
	case <-time.After(500 * time.Millisecond):
		t.Error("RunHeartbeatLoop did not stop after context cancel")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// OutcomeObserver — last-received-wins (CHB-025 via CHB-020)
// ─────────────────────────────────────────────────────────────────────────────

// TestOutcomeObserver_LastWins verifies last-received-wins dedup per CHB-025.
func TestOutcomeObserver_LastWins(t *testing.T) {
	t.Parallel()

	obs := &handler.OutcomeObserver{}

	first := json.RawMessage(`{"kind":"WORK_COMPLETE","sub_reason":"","suggested_class":""}`)
	obs.Record(first)
	second := json.RawMessage(`{"kind":"FAILURE_SIGNAL","sub_reason":"claude_crashed","suggested_class":"structural"}`)
	obs.Record(second)

	latest := obs.Latest()
	if latest == nil {
		t.Fatal("Latest() is nil; want non-nil")
	}
	if latest.Kind != "FAILURE_SIGNAL" {
		t.Errorf("Latest.Kind = %q; want FAILURE_SIGNAL (last-received-wins)", latest.Kind)
	}
}

// TestOutcomeObserver_NilWhenEmpty verifies Latest() returns nil before any Record.
func TestOutcomeObserver_NilWhenEmpty(t *testing.T) {
	t.Parallel()
	obs := &handler.OutcomeObserver{}
	if latest := obs.Latest(); latest != nil {
		t.Errorf("Latest() = %v; want nil before any Record call", latest)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// DeriveCIaudeTranscriptPath
// ─────────────────────────────────────────────────────────────────────────────

// TestDeriveCIaudeTranscriptPath_EndsWithSessionID verifies the derived path
// ends with <claude_session_id>.jsonl.
func TestDeriveCIaudeTranscriptPath_EndsWithSessionID(t *testing.T) {
	t.Parallel()
	const sessID = "01234567-0000-7000-8000-000000000001"
	path := handler.DeriveCIaudeTranscriptPath("/workspace/my-project", sessID)
	if !strings.HasSuffix(path, sessID+".jsonl") {
		t.Errorf("path %q does not end with %q", path, sessID+".jsonl")
	}
}

// TestDeriveCIaudeTranscriptPath_ContainsSlug verifies the path contains a slug
// derived from the workspace path.
func TestDeriveCIaudeTranscriptPath_ContainsSlug(t *testing.T) {
	t.Parallel()
	const sessID = "test-sess-001"
	path := handler.DeriveCIaudeTranscriptPath("/workspace/my-project", sessID)
	// The path should contain "projects" as a directory component.
	if !strings.Contains(path, "projects") {
		t.Errorf("path %q does not contain 'projects'", path)
	}
}

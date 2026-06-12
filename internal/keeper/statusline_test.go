package keeper_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/keeper"
)

// repoScriptPath returns the absolute path to scripts/keeper-statusline.sh
// by walking up from the package directory to the repo root.
func repoScriptPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Skip("runtime.Caller failed; cannot locate script")
	}
	// this file lives at internal/keeper/statusline_test.go
	// repo root is three directories up: internal/keeper → internal → repo root
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	p := filepath.Join(repoRoot, "scripts", "keeper-statusline.sh")
	if _, err := os.Stat(p); err != nil {
		t.Skipf("script not found at %q: %v", p, err)
	}
	return p
}

// TestKeeperStatuslineScript verifies that scripts/keeper-statusline.sh
// writes a well-formed .ctx file when given a sample Claude Code statusLine
// JSON payload via stdin (legacy format — no token counts).
func TestKeeperStatuslineScript(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available; skipping script test")
	}
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq not available; skipping script test")
	}

	script := repoScriptPath(t)
	projectDir := t.TempDir()

	// Legacy payload: only used_percentage, no total_input_tokens or context_window_size.
	sampleJSON := `{"session_id":"test-session-42","context_window":{"used_percentage":65.3}}`

	cmd := exec.Command("bash", script)
	cmd.Env = append(os.Environ(),
		"HARMONIK_PROJECT="+projectDir,
		"HARMONIK_AGENT=test-agent",
	)
	cmd.Stdin = strings.NewReader(sampleJSON)

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("script exited with error: %v\noutput: %s", err, out)
	}

	ctxPath := filepath.Join(projectDir, ".harmonik", "keeper", "test-agent.ctx")
	raw, err := os.ReadFile(ctxPath)
	if err != nil {
		t.Fatalf("ctx file not created at %q: %v", ctxPath, err)
	}

	var cf keeper.CtxFile
	if err := json.Unmarshal(raw, &cf); err != nil {
		t.Fatalf("ctx file is not valid JSON: %v\ncontent: %s", err, raw)
	}

	if cf.Pct != 65.3 {
		t.Errorf("pct = %v; want 65.3", cf.Pct)
	}
	if cf.SessionID != "test-session-42" {
		t.Errorf("session_id = %q; want %q", cf.SessionID, "test-session-42")
	}
	if cf.Ts == "" {
		t.Error("ts field is empty; want an RFC 3339 timestamp")
	}
	// Legacy payload has no token data — both fields must default to zero.
	if cf.Tokens != 0 {
		t.Errorf("tokens = %d; want 0 (absent in legacy payload)", cf.Tokens)
	}
	if cf.WindowSize != 0 {
		t.Errorf("window_size = %d; want 0 (absent in legacy payload)", cf.WindowSize)
	}
}

// TestKeeperStatuslineScript_WithTokenCounts verifies that the script correctly
// emits total_input_tokens and context_window_size when present in the payload.
func TestKeeperStatuslineScript_WithTokenCounts(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available; skipping script test")
	}
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq not available; skipping script test")
	}

	script := repoScriptPath(t)
	projectDir := t.TempDir()

	// Full payload including absolute token counts (modern Claude Code format).
	sampleJSON := `{"session_id":"tok-session-1","context_window":{"used_percentage":28.0,"total_input_tokens":280000},"context_window_size":1000000}`

	cmd := exec.Command("bash", script)
	cmd.Env = append(os.Environ(),
		"HARMONIK_PROJECT="+projectDir,
		"HARMONIK_AGENT=tok-agent",
	)
	cmd.Stdin = strings.NewReader(sampleJSON)

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("script exited with error: %v\noutput: %s", err, out)
	}

	ctxPath := filepath.Join(projectDir, ".harmonik", "keeper", "tok-agent.ctx")
	raw, err := os.ReadFile(ctxPath)
	if err != nil {
		t.Fatalf("ctx file not created at %q: %v", ctxPath, err)
	}

	var cf keeper.CtxFile
	if err := json.Unmarshal(raw, &cf); err != nil {
		t.Fatalf("ctx file is not valid JSON: %v\ncontent: %s", err, raw)
	}

	if cf.Pct != 28.0 {
		t.Errorf("pct = %v; want 28.0", cf.Pct)
	}
	if cf.Tokens != 280000 {
		t.Errorf("tokens = %d; want 280000", cf.Tokens)
	}
	if cf.WindowSize != 1000000 {
		t.Errorf("window_size = %d; want 1000000", cf.WindowSize)
	}
	if cf.SessionID != "tok-session-1" {
		t.Errorf("session_id = %q; want %q", cf.SessionID, "tok-session-1")
	}
}

// TestKeeperStatuslineScript_1MModelInference verifies that the script infers
// window_size=1000000 when the model contains "[1m]" and context_window_size is absent.
func TestKeeperStatuslineScript_1MModelInference(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available; skipping script test")
	}
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq not available; skipping script test")
	}

	script := repoScriptPath(t)
	projectDir := t.TempDir()

	// [1m] model payload: context_window_size is absent (Claude Code omits it for Opus-4.8 [1m]).
	sampleJSON := `{"session_id":"opus-1m-session","model":"claude-opus-4-8 [1m]","context_window":{"used_percentage":15.0,"total_input_tokens":150000}}`

	cmd := exec.Command("bash", script)
	cmd.Env = append(os.Environ(),
		"HARMONIK_PROJECT="+projectDir,
		"HARMONIK_AGENT=opus-agent",
	)
	cmd.Stdin = strings.NewReader(sampleJSON)

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("script exited with error: %v\noutput: %s", err, out)
	}

	ctxPath := filepath.Join(projectDir, ".harmonik", "keeper", "opus-agent.ctx")
	raw, err := os.ReadFile(ctxPath)
	if err != nil {
		t.Fatalf("ctx file not created at %q: %v", ctxPath, err)
	}

	var cf keeper.CtxFile
	if err := json.Unmarshal(raw, &cf); err != nil {
		t.Fatalf("ctx file is not valid JSON: %v\ncontent: %s", err, raw)
	}

	if cf.Pct != 15.0 {
		t.Errorf("pct = %v; want 15.0", cf.Pct)
	}
	if cf.Tokens != 150000 {
		t.Errorf("tokens = %d; want 150000", cf.Tokens)
	}
	if cf.WindowSize != 1000000 {
		t.Errorf("window_size = %d; want 1000000 (inferred from [1m] model)", cf.WindowSize)
	}
}

// TestKeeperStatuslineScript_EnvWindowSizeOverride verifies that HARMONIK_KEEPER_WINDOW_SIZE
// overrides window_size when context_window_size is absent from the payload.
func TestKeeperStatuslineScript_EnvWindowSizeOverride(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available; skipping script test")
	}
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq not available; skipping script test")
	}

	script := repoScriptPath(t)
	projectDir := t.TempDir()

	// Payload with no context_window_size and no recognizable model.
	sampleJSON := `{"session_id":"env-override-session","context_window":{"used_percentage":20.0,"total_input_tokens":200000}}`

	cmd := exec.Command("bash", script)
	cmd.Env = append(os.Environ(),
		"HARMONIK_PROJECT="+projectDir,
		"HARMONIK_AGENT=env-agent",
		"HARMONIK_KEEPER_WINDOW_SIZE=500000",
	)
	cmd.Stdin = strings.NewReader(sampleJSON)

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("script exited with error: %v\noutput: %s", err, out)
	}

	ctxPath := filepath.Join(projectDir, ".harmonik", "keeper", "env-agent.ctx")
	raw, err := os.ReadFile(ctxPath)
	if err != nil {
		t.Fatalf("ctx file not created at %q: %v", ctxPath, err)
	}

	var cf keeper.CtxFile
	if err := json.Unmarshal(raw, &cf); err != nil {
		t.Fatalf("ctx file is not valid JSON: %v\ncontent: %s", err, raw)
	}

	if cf.WindowSize != 500000 {
		t.Errorf("window_size = %d; want 500000 (from HARMONIK_KEEPER_WINDOW_SIZE)", cf.WindowSize)
	}
}

// TestKeeperStatuslineScript_1MModelObjectFormInference verifies that the script infers
// window_size=1000000 when .model is a nested {id, display_name} object and the id
// contains "[1m]" (the object form used by newer Claude Code versions).
func TestKeeperStatuslineScript_1MModelObjectFormInference(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available; skipping script test")
	}
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq not available; skipping script test")
	}

	script := repoScriptPath(t)
	projectDir := t.TempDir()

	// Nested-object model form: Claude Code emits .model as {id, display_name}.
	// context_window_size is absent (Claude Code omits it for [1m] models in this format).
	sampleJSON := `{"session_id":"opus-obj-session","model":{"id":"claude-opus-4-8[1m]","display_name":"Opus"},"context_window":{"used_percentage":12.0,"total_input_tokens":120000}}`

	cmd := exec.Command("bash", script)
	cmd.Env = append(os.Environ(),
		"HARMONIK_PROJECT="+projectDir,
		"HARMONIK_AGENT=obj-agent",
	)
	cmd.Stdin = strings.NewReader(sampleJSON)

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("script exited with error: %v\noutput: %s", err, out)
	}

	ctxPath := filepath.Join(projectDir, ".harmonik", "keeper", "obj-agent.ctx")
	raw, err := os.ReadFile(ctxPath)
	if err != nil {
		t.Fatalf("ctx file not created at %q: %v", ctxPath, err)
	}

	var cf keeper.CtxFile
	if err := json.Unmarshal(raw, &cf); err != nil {
		t.Fatalf("ctx file is not valid JSON: %v\ncontent: %s", err, raw)
	}

	if cf.WindowSize != 1000000 {
		t.Errorf("window_size = %d; want 1000000 (inferred from nested model.id containing [1m])", cf.WindowSize)
	}
	if cf.Tokens != 120000 {
		t.Errorf("tokens = %d; want 120000", cf.Tokens)
	}
}

// TestKeeperStatuslineScript_NestedContextWindowSize verifies that the script reads
// context_window_size from .context_window.context_window_size (nested path used by
// some Claude Code versions) when the top-level .context_window_size is absent.
func TestKeeperStatuslineScript_NestedContextWindowSize(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available; skipping script test")
	}
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq not available; skipping script test")
	}

	script := repoScriptPath(t)
	projectDir := t.TempDir()

	// context_window_size is nested under .context_window (documented schema path),
	// not at the top level. No top-level .context_window_size field is present.
	sampleJSON := `{"session_id":"nested-ws-session","context_window":{"used_percentage":25.0,"total_input_tokens":250000,"context_window_size":1000000}}`

	cmd := exec.Command("bash", script)
	cmd.Env = append(os.Environ(),
		"HARMONIK_PROJECT="+projectDir,
		"HARMONIK_AGENT=nested-agent",
	)
	cmd.Stdin = strings.NewReader(sampleJSON)

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("script exited with error: %v\noutput: %s", err, out)
	}

	ctxPath := filepath.Join(projectDir, ".harmonik", "keeper", "nested-agent.ctx")
	raw, err := os.ReadFile(ctxPath)
	if err != nil {
		t.Fatalf("ctx file not created at %q: %v", ctxPath, err)
	}

	var cf keeper.CtxFile
	if err := json.Unmarshal(raw, &cf); err != nil {
		t.Fatalf("ctx file is not valid JSON: %v\ncontent: %s", err, raw)
	}

	if cf.WindowSize != 1000000 {
		t.Errorf("window_size = %d; want 1000000 (from .context_window.context_window_size)", cf.WindowSize)
	}
	if cf.Tokens != 250000 {
		t.Errorf("tokens = %d; want 250000", cf.Tokens)
	}
}

// TestKeeperStatuslineScript_SkipsOnMissingPct verifies that the script does
// not write a .ctx file when the percentage field is absent from the input JSON.
func TestKeeperStatuslineScript_SkipsOnMissingPct(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available; skipping script test")
	}
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq not available; skipping script test")
	}

	script := repoScriptPath(t)
	projectDir := t.TempDir()

	// JSON with no context_window field (e.g. right after /clear).
	sampleJSON := `{"session_id":"after-clear"}`

	cmd := exec.Command("bash", script)
	cmd.Env = append(os.Environ(),
		"HARMONIK_PROJECT="+projectDir,
		"HARMONIK_AGENT=test-agent",
	)
	cmd.Stdin = strings.NewReader(sampleJSON)

	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("script exited with error: %v\noutput: %s", err, out)
	}

	ctxPath := filepath.Join(projectDir, ".harmonik", "keeper", "test-agent.ctx")
	if _, err := os.Stat(ctxPath); err == nil {
		t.Errorf("ctx file was created but should have been skipped when pct is absent")
	}
}

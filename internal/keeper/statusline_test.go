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

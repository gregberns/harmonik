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

func repoScriptPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Skip("runtime.Caller failed; cannot locate script")
	}
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

	sampleJSON := `{"session_id":"test-session-42","context_window":{"used_percentage":65.3}}`

	//nolint:gosec // G204: test invokes the repository statusline script with a controlled bash path and arguments
	cmd := exec.CommandContext(t.Context(), "bash", script)
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
	//nolint:gosec // G304: ctxPath is derived from this test's t.TempDir fixture
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

	sampleJSON := `{"session_id":"tok-session-1","context_window":{"used_percentage":28.0,"total_input_tokens":280000},"context_window_size":1000000}`

	//nolint:gosec // G204: test invokes the repository statusline script with a controlled bash path and arguments
	cmd := exec.CommandContext(t.Context(), "bash", script)
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
	//nolint:gosec // G304: ctxPath is derived from this test's t.TempDir fixture
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
// the effective window_size (500k by default) when the model contains "[1m]" and
// context_window_size is absent, and recomputes pct relative to that effective window
// so the keeper's pct guard fires at the correct fill level (hk-d8dj0).
func TestKeeperStatuslineScript_1MModelInference(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available; skipping script test")
	}
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq not available; skipping script test")
	}

	script := repoScriptPath(t)
	projectDir := t.TempDir()

	sampleJSON := `{"session_id":"opus-1m-session","model":"claude-opus-4-8 [1m]","context_window":{"used_percentage":15.0,"total_input_tokens":150000}}`

	//nolint:gosec // G204: test invokes the repository statusline script with a controlled bash path and arguments
	cmd := exec.CommandContext(t.Context(), "bash", script)
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
	//nolint:gosec // G304: ctxPath is derived from this test's t.TempDir fixture
	raw, err := os.ReadFile(ctxPath)
	if err != nil {
		t.Fatalf("ctx file not created at %q: %v", ctxPath, err)
	}

	var cf keeper.CtxFile
	if err := json.Unmarshal(raw, &cf); err != nil {
		t.Fatalf("ctx file is not valid JSON: %v\ncontent: %s", err, raw)
	}

	if cf.Pct != 30.0 {
		t.Errorf("pct = %v; want 30.0 (150000/500000*100, effective fill)", cf.Pct)
	}
	if cf.Tokens != 150000 {
		t.Errorf("tokens = %d; want 150000", cf.Tokens)
	}
	if cf.WindowSize != 500000 {
		t.Errorf("window_size = %d; want 500000 (effective [1m] window, hk-d8dj0)", cf.WindowSize)
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

	sampleJSON := `{"session_id":"env-override-session","context_window":{"used_percentage":20.0,"total_input_tokens":200000}}`

	//nolint:gosec // G204: test invokes the repository statusline script with a controlled bash path and arguments
	cmd := exec.CommandContext(t.Context(), "bash", script)
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
	//nolint:gosec // G304: ctxPath is derived from this test's t.TempDir fixture
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
// the effective window_size (500k) when .model is a nested {id, display_name} object
// and the id contains "[1m]" (the object form used by newer Claude Code versions).
func TestKeeperStatuslineScript_1MModelObjectFormInference(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available; skipping script test")
	}
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq not available; skipping script test")
	}

	script := repoScriptPath(t)
	projectDir := t.TempDir()

	sampleJSON := `{"session_id":"opus-obj-session","model":{"id":"claude-opus-4-8[1m]","display_name":"Opus"},"context_window":{"used_percentage":12.0,"total_input_tokens":120000}}`

	//nolint:gosec // G204: test invokes the repository statusline script with a controlled bash path and arguments
	cmd := exec.CommandContext(t.Context(), "bash", script)
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
	//nolint:gosec // G304: ctxPath is derived from this test's t.TempDir fixture
	raw, err := os.ReadFile(ctxPath)
	if err != nil {
		t.Fatalf("ctx file not created at %q: %v", ctxPath, err)
	}

	var cf keeper.CtxFile
	if err := json.Unmarshal(raw, &cf); err != nil {
		t.Fatalf("ctx file is not valid JSON: %v\ncontent: %s", err, raw)
	}

	if cf.WindowSize != 500000 {
		t.Errorf("window_size = %d; want 500000 (effective [1m] window, hk-d8dj0)", cf.WindowSize)
	}
	if cf.Tokens != 120000 {
		t.Errorf("tokens = %d; want 120000", cf.Tokens)
	}
	if cf.Pct != 24.0 {
		t.Errorf("pct = %v; want 24.0 (120000/500000*100, effective fill)", cf.Pct)
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

	sampleJSON := `{"session_id":"nested-ws-session","context_window":{"used_percentage":25.0,"total_input_tokens":250000,"context_window_size":1000000}}`

	//nolint:gosec // G204: test invokes the repository statusline script with a controlled bash path and arguments
	cmd := exec.CommandContext(t.Context(), "bash", script)
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
	//nolint:gosec // G304: ctxPath is derived from this test's t.TempDir fixture
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

// TestKeeperStatuslineScript_1M_EffectivePct verifies the hk-d8dj0 fix: a [1m] session
// at 372k tokens reports pct ~74.4 (tokens/500k*100) and window_size=500k, not the
// Claude-Code-reported 37.2 (tokens/1M*100). This is the concrete scenario that caused
// admiral to run keeper-less to 372k: with window=1M the pct guard (cf.Pct < 80%)
// always evaluated TRUE (37.2 < 80), so warn/act never fired despite tokens > 200k abs.
func TestKeeperStatuslineScript_1M_EffectivePct(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available; skipping script test")
	}
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq not available; skipping script test")
	}

	script := repoScriptPath(t)
	projectDir := t.TempDir()

	sampleJSON := `{"session_id":"1m-effective-pct","model":"claude-opus-4-8 [1m]","context_window":{"used_percentage":37.2,"total_input_tokens":372000}}`

	//nolint:gosec // G204: test invokes the repository statusline script with a controlled bash path and arguments
	cmd := exec.CommandContext(t.Context(), "bash", script)
	cmd.Env = append(os.Environ(),
		"HARMONIK_PROJECT="+projectDir,
		"HARMONIK_AGENT=effective-pct-agent",
	)
	cmd.Stdin = strings.NewReader(sampleJSON)

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("script exited with error: %v\noutput: %s", err, out)
	}

	ctxPath := filepath.Join(projectDir, ".harmonik", "keeper", "effective-pct-agent.ctx")
	//nolint:gosec // G304: ctxPath is derived from this test's t.TempDir fixture
	raw, err := os.ReadFile(ctxPath)
	if err != nil {
		t.Fatalf("ctx file not created at %q: %v", ctxPath, err)
	}

	var cf keeper.CtxFile
	if err := json.Unmarshal(raw, &cf); err != nil {
		t.Fatalf("ctx file is not valid JSON: %v\ncontent: %s", err, raw)
	}

	if cf.WindowSize != 500000 {
		t.Errorf("window_size = %d; want 500000 (effective [1m] window)", cf.WindowSize)
	}
	if cf.Tokens != 372000 {
		t.Errorf("tokens = %d; want 372000", cf.Tokens)
	}
	const wantPct = 74.4
	if cf.Pct < wantPct-0.1 || cf.Pct > wantPct+0.1 {
		t.Errorf("pct = %v; want ~%.1f (372000/500000*100, effective fill — must not be 37.2)", cf.Pct, wantPct)
	}
}

// TestKeeperStatuslineScript_1M_FractionOverride verifies that
// HARMONIK_KEEPER_1M_EFFECTIVE_FRACTION overrides the default 0.5 fraction, allowing
// operators to tune the effective window without a code change.
func TestKeeperStatuslineScript_1M_FractionOverride(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available; skipping script test")
	}
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq not available; skipping script test")
	}

	script := repoScriptPath(t)
	projectDir := t.TempDir()

	sampleJSON := `{"session_id":"fraction-override","model":"claude-opus-4-8 [1m]","context_window":{"used_percentage":60.0,"total_input_tokens":600000}}`

	//nolint:gosec // G204: test invokes the repository statusline script with a controlled bash path and arguments
	cmd := exec.CommandContext(t.Context(), "bash", script)
	cmd.Env = append(os.Environ(),
		"HARMONIK_PROJECT="+projectDir,
		"HARMONIK_AGENT=fraction-agent",
		"HARMONIK_KEEPER_1M_EFFECTIVE_FRACTION=0.6",
	)
	cmd.Stdin = strings.NewReader(sampleJSON)

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("script exited with error: %v\noutput: %s", err, out)
	}

	ctxPath := filepath.Join(projectDir, ".harmonik", "keeper", "fraction-agent.ctx")
	//nolint:gosec // G304: ctxPath is derived from this test's t.TempDir fixture
	raw, err := os.ReadFile(ctxPath)
	if err != nil {
		t.Fatalf("ctx file not created at %q: %v", ctxPath, err)
	}

	var cf keeper.CtxFile
	if err := json.Unmarshal(raw, &cf); err != nil {
		t.Fatalf("ctx file is not valid JSON: %v\ncontent: %s", err, raw)
	}

	if cf.WindowSize != 600000 {
		t.Errorf("window_size = %d; want 600000 (floor(1M*0.6))", cf.WindowSize)
	}
	if cf.Pct != 100.0 {
		t.Errorf("pct = %v; want 100.0 (600000/600000*100, clamped to 100)", cf.Pct)
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

	sampleJSON := `{"session_id":"after-clear"}`

	//nolint:gosec // G204: test invokes the repository statusline script with a controlled bash path and arguments
	cmd := exec.CommandContext(t.Context(), "bash", script)
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

//nolint:gosec // Test runs fixed git and statusline commands with temporary paths.
func TestKeeperStatuslineScript_LinkedWorktreeWritesPrimaryProjectGauge(t *testing.T) {
	for _, tool := range []string{"bash", "jq", "git"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not available; skipping linked-worktree script test", tool)
		}
	}

	primary := filepath.Join(t.TempDir(), "primary")
	linked := filepath.Join(t.TempDir(), "linked")
	if err := os.MkdirAll(primary, 0o750); err != nil {
		t.Fatal(err)
	}
	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", append([]string{"-C", primary}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=keeper-test", "GIT_AUTHOR_EMAIL=keeper@test.invalid", "GIT_COMMITTER_NAME=keeper-test", "GIT_COMMITTER_EMAIL=keeper@test.invalid")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	runGit("init", "-q")
	if err := os.WriteFile(filepath.Join(primary, "seed"), []byte("seed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit("add", "seed")
	runGit("commit", "-q", "-m", "seed")
	runGit("worktree", "add", "-q", "-b", "linked-test", linked)

	cmd := exec.CommandContext(t.Context(), "bash", repoScriptPath(t))
	cmd.Dir = linked
	cmd.Env = append(os.Environ(), "HARMONIK_PROJECT=", "HARMONIK_AGENT=linked-agent")
	cmd.Stdin = strings.NewReader(`{"session_id":"linked-session","context_window":{"used_percentage":42}}`)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("script exited with error: %v\noutput: %s", err, out)
	}

	primaryGauge := filepath.Join(primary, ".harmonik", "keeper", "linked-agent.ctx")
	if _, err := os.Stat(primaryGauge); err != nil {
		t.Fatalf("primary project gauge not created at %q: %v", primaryGauge, err)
	}
	if _, err := os.Stat(filepath.Join(linked, ".harmonik", "keeper", "linked-agent.ctx")); !os.IsNotExist(err) {
		t.Fatalf("status line wrote gauge in linked worktree; stat err = %v", err)
	}
}

package workspace

// claudeconfigdir_via_hkqxvc2_test.go — gate-runnable tests for the SSH-aware
// isolated-config preparation (hk-qxvc2). All remote work is intercepted by a
// RecordingRunner; NO real ssh and NO real worker are touched.
//
// Coverage:
//   - REMOTE (runner != nil): the preparation runs `python3 - <worktree>` with the
//     program piped on STDIN (NOT via -c — the SSH argv-resplit hazard), and the
//     returned path is the worker-absolute <worktree>/.harmonik/claude-config.
//   - The stdin-piped program carries the isolated-config contract (mkdir the dir,
//     seed from the worker's own ~/.claude.json, upsert realpath-normalized trust).
//   - A real python3 end-to-end (HOME redirected) proves the isolated
//     <dir>/.claude.json is created, seeded, and trusted for the worktree.
//   - LOCAL (runner == nil): delegates to PrepareIsolatedClaudeConfigDir (no runner
//     call recorded).

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// isoCfgProbe is a typed view over the isolated .claude.json used to assert the
// seeded content without blank type-assertions (errcheck check-type-assertions).
type isoCfgProbe struct {
	FirstStartTime string `json:"firstStartTime"`
	WorkerOnlyKey  string `json:"workerOnlyKey"`
	Projects       map[string]struct {
		HasTrustDialogAccepted bool `json:"hasTrustDialogAccepted"`
	} `json:"projects"`
}

// trustedForAnyOf reports whether the config trusts worktree path wt or its
// realpath — the program keys trust by realpath, which may equal wt on filesystems
// without symlinks in the path.
func (c isoCfgProbe) trustedForAnyOf(wt, realWt string) bool {
	if e, ok := c.Projects[realWt]; ok && e.HasTrustDialogAccepted {
		return true
	}
	e, ok := c.Projects[wt]
	return ok && e.HasTrustDialogAccepted
}

func TestPrepareIsolatedClaudeConfigDirVia_Remote_ShapeAndPath(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	rr := newNoOpRecorder()

	dir, err := PrepareIsolatedClaudeConfigDirVia(ctx, rr, z8ekWorkerWt)
	if err != nil {
		t.Fatalf("PrepareIsolatedClaudeConfigDirVia (remote): %v", err)
	}

	// Returned dir is the worker-absolute isolated config path.
	wantDir := filepath.Join(z8ekWorkerWt, ".harmonik", "claude-config")
	if dir != wantDir {
		t.Errorf("returned dir = %q, want %q", dir, wantDir)
	}

	if len(rr.Calls) != 1 {
		t.Fatalf("expected exactly 1 runner call, got %d: %v", len(rr.Calls), rr.Calls)
	}
	call := rr.Calls[0]
	if call.Name != "python3" {
		t.Fatalf("call.Name = %q, want python3", call.Name)
	}
	// Argv must be `- <worktreePath>` — `-` = read program from stdin (NOT `-c`).
	if len(call.Args) != 2 || call.Args[0] != "-" {
		t.Fatalf("call.Args = %v, want [- <worktreePath>] (program via stdin, not -c)", call.Args)
	}
	if call.Args[1] != z8ekWorkerWt {
		t.Errorf("worktree arg = %q, want %q", call.Args[1], z8ekWorkerWt)
	}
	// Regression guard: the program must NOT travel as a `-c` argv token.
	for _, a := range call.Args {
		if a == "-c" {
			t.Errorf("program passed via -c (SSH argv-resplit hazard): args=%v", call.Args)
		}
	}

	// The stdin-piped program must carry the isolated-config contract.
	prog := workerIsolatedConfigProgram
	for _, want := range []string{
		"claude-config", "hasTrustDialogAccepted", "realpath",
		"expanduser", "makedirs", "os.replace", "sys.argv[1]", fallbackFirstStartTime,
	} {
		if !strings.Contains(prog, want) {
			t.Errorf("isolated-config program (stdin) missing %q:\n%s", want, prog)
		}
	}
}

// TestPrepareIsolatedClaudeConfigDirVia_PipesProgramOnStdin physically proves the
// program bytes reach python3's STDIN (not the command line).
func TestPrepareIsolatedClaudeConfigDirVia_PipesProgramOnStdin(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	capFile := filepath.Join(t.TempDir(), "stdin.txt")
	rr := &tmux.RecordingRunner{
		CmdFunc: func(c context.Context, name string, _ ...string) *exec.Cmd {
			if name == "python3" {
				//nolint:gosec // G204: capFile is a test-controlled temp path, not user input
				return exec.CommandContext(c, "sh", "-c", "cat > "+capFile)
			}
			return exec.CommandContext(c, "true")
		},
	}

	if _, err := PrepareIsolatedClaudeConfigDirVia(ctx, rr, z8ekWorkerWt); err != nil {
		t.Fatalf("PrepareIsolatedClaudeConfigDirVia: %v", err)
	}
	got, err := os.ReadFile(capFile) //nolint:gosec // G304: test-controlled temp path
	if err != nil {
		t.Fatalf("read captured stdin: %v (nothing piped on stdin?)", err)
	}
	if string(got) != workerIsolatedConfigProgram {
		t.Errorf("stdin-piped program mismatch.\n got: %q\nwant: %q", got, workerIsolatedConfigProgram)
	}
}

// homeRedirectRunner returns a RecordingRunner that forces HOME=<home> on every
// spawned command, so the python program's os.path.expanduser("~") resolves to a
// temp dir instead of the real ~/.claude.json.
func homeRedirectRunner(home string) *tmux.RecordingRunner {
	return &tmux.RecordingRunner{
		CmdFunc: func(c context.Context, name string, args ...string) *exec.Cmd {
			cmd := exec.CommandContext(c, name, args...)
			cmd.Env = append(os.Environ(), "HOME="+home)
			return cmd
		},
	}
}

// TestPrepareIsolatedClaudeConfigDirVia_RealPythonSeedsAndTrusts runs the ACTUAL
// preparation end-to-end against a real python3 with HOME redirected, proving the
// isolated <worktree>/.harmonik/claude-config/.claude.json is created, seeded from
// the (redirected) worker ~/.claude.json, and trusted for the worktree.
func TestPrepareIsolatedClaudeConfigDirVia_RealPythonSeedsAndTrusts(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not on PATH")
	}
	ctx := context.Background()

	home := t.TempDir()
	// Seed a "worker" onboarded ~/.claude.json with a distinctive key to prove the
	// copy path (not the fallback) ran.
	writeClaudeCfg(t, filepath.Join(home, ".claude.json"), map[string]interface{}{
		"firstStartTime": "2026-02-03T04:05:06.789Z",
		"workerOnlyKey":  "copied",
	})

	wt := filepath.Join(t.TempDir(), "worktrees", "run-qxvc2")
	if err := os.MkdirAll(wt, 0o750); err != nil {
		t.Fatalf("mkdir worktree: %v", err)
	}

	dir, err := PrepareIsolatedClaudeConfigDirVia(ctx, homeRedirectRunner(home), wt)
	if err != nil {
		t.Fatalf("PrepareIsolatedClaudeConfigDirVia (real python): %v", err)
	}
	wantDir := filepath.Join(wt, ".harmonik", "claude-config")
	if dir != wantDir {
		t.Errorf("returned dir = %q, want %q", dir, wantDir)
	}

	data, err := os.ReadFile(filepath.Join(dir, ".claude.json")) //nolint:gosec // G304: test-controlled temp path
	if err != nil {
		t.Fatalf("read isolated config: %v (preparation did not run)", err)
	}
	var cfg isoCfgProbe
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("isolated config not valid JSON: %v\n%s", err, data)
	}
	// Seeded from the worker's own config (copy path, not fallback).
	if cfg.WorkerOnlyKey != "copied" {
		t.Errorf("worker config not copied into isolated dir: %+v", cfg)
	}
	if cfg.FirstStartTime != "2026-02-03T04:05:06.789Z" {
		t.Errorf("firstStartTime = %q, want copied worker value", cfg.FirstStartTime)
	}
	// Trust entry upserted, keyed by realpath of the worktree.
	realWt, err := filepath.EvalSymlinks(wt)
	if err != nil {
		realWt = wt
	}
	if !cfg.trustedForAnyOf(wt, realWt) {
		t.Errorf("no trusted worktree entry for %q (or %q):\n%s", realWt, wt, data)
	}

	// The isolated dir is 0700 (holds a copy of onboarded config w/ oauth metadata).
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Errorf("isolated dir perm = %o, want 0700", perm)
	}
}

// TestPrepareIsolatedClaudeConfigDirVia_RealPythonFallback proves that when the
// worker's ~/.claude.json is absent, the isolated config falls back to the minimal
// onboarding-complete config and is still trusted.
func TestPrepareIsolatedClaudeConfigDirVia_RealPythonFallback(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not on PATH")
	}
	ctx := context.Background()

	home := t.TempDir() // no ~/.claude.json seeded → source missing
	wt := filepath.Join(t.TempDir(), "worktrees", "run-qxvc2-fb")
	if err := os.MkdirAll(wt, 0o750); err != nil {
		t.Fatalf("mkdir worktree: %v", err)
	}

	dir, err := PrepareIsolatedClaudeConfigDirVia(ctx, homeRedirectRunner(home), wt)
	if err != nil {
		t.Fatalf("PrepareIsolatedClaudeConfigDirVia (fallback): %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, ".claude.json")) //nolint:gosec // G304: test-controlled temp path
	if err != nil {
		t.Fatalf("read isolated config: %v", err)
	}
	var cfg isoCfgProbe
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("isolated config not valid JSON: %v\n%s", err, data)
	}
	if cfg.FirstStartTime != fallbackFirstStartTime {
		t.Errorf("fallback firstStartTime = %q, want %q", cfg.FirstStartTime, fallbackFirstStartTime)
	}
	realWt, err := filepath.EvalSymlinks(wt)
	if err != nil {
		realWt = wt
	}
	if !cfg.trustedForAnyOf(wt, realWt) {
		t.Errorf("fallback config not trusted:\n%s", data)
	}
}

// TestPrepareIsolatedClaudeConfigDirVia_LocalDelegates asserts that with a nil
// runner the helper delegates to the box-A-local PrepareIsolatedClaudeConfigDir
// (NFR7) and issues NO runner call.
func TestPrepareIsolatedClaudeConfigDirVia_LocalDelegates(t *testing.T) {
	ctx := context.Background()
	wt := t.TempDir()

	srcDir := t.TempDir()
	srcCfg := filepath.Join(srcDir, ".claude.json")
	writeClaudeCfg(t, srcCfg, map[string]interface{}{"firstStartTime": "2026-01-01T00:00:00.000Z"})
	withIsolatedConfigSource(t, srcCfg)

	dir, err := PrepareIsolatedClaudeConfigDirVia(ctx, nil, wt)
	if err != nil {
		t.Fatalf("PrepareIsolatedClaudeConfigDirVia (local): %v", err)
	}
	wantDir := filepath.Join(wt, ".harmonik", "claude-config")
	if dir != wantDir {
		t.Errorf("returned dir = %q, want %q", dir, wantDir)
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude.json")); err != nil {
		t.Errorf("local isolated config not written: %v", err)
	}
}

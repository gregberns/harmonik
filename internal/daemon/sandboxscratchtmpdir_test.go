package daemon_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/projectconfig"
)

func scratchProfileInput(t *testing.T) daemon.SandboxProfileInput {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve temp root: %v", err)
	}
	return daemon.SandboxProfileInput{
		WorktreePath:      root,
		GitDir:            filepath.Join(root, ".git"),
		RunID:             "scratch-tmpdir-run",
		DaemonSockPath:    filepath.Join(root, "daemon.sock"),
		AllowLocalBinding: true,
	}
}

func scratchPiSandboxConfig() projectconfig.SandboxConfig {
	return projectconfig.SandboxConfig{
		Backend:   "srt",
		Harnesses: []string{"pi"},
		Network:   projectconfig.SandboxNetworkConfig{AllowLocalBinding: true},
	}
}

func profileAllowWrite(t *testing.T, in daemon.SandboxProfileInput) []string {
	t.Helper()
	raw, err := daemon.GenerateSandboxProfile(in)
	if err != nil {
		t.Fatalf("GenerateSandboxProfile: %v", err)
	}
	var settings struct {
		Filesystem struct {
			AllowWrite []string `json:"allowWrite"`
		} `json:"filesystem"`
	}
	if err := json.Unmarshal(raw, &settings); err != nil {
		t.Fatalf("decode profile %s: %v", raw, err)
	}
	return settings.Filesystem.AllowWrite
}

// TestSandboxProfileGrantsTheRunAWritableScratchDirectory pins the first half of
// the promise: the run has somewhere to write that is not the worktree checkout
// and not one harness's hardcoded path.
func TestSandboxProfileGrantsTheRunAWritableScratchDirectory(t *testing.T) {
	in := scratchProfileInput(t)
	allowWrite := profileAllowWrite(t, in)

	scratch := daemon.SandboxScratchDir(in.WorktreePath)
	if !slices.Contains(allowWrite, scratch) {
		t.Fatalf("profile grants no scratch directory: allowWrite=%v, want it to contain %q.\n"+
			"Without this a run that writes an ordinary temp file is refused, which is how a "+
			"finished commit was thrown away (hk-sandbox-no-writable-tmpdir-7484h).", allowWrite, scratch)
	}

	if !isUnderDir(t, in.WorktreePath, scratch) {
		t.Errorf("the granted scratch directory %q is not under this run's worktree %q.\n"+
			"It must be the run's own: a shared path is written into by every concurrent run at once, "+
			"and it outlives the worktree whose removal was supposed to take it away (hk-guapd).",
			scratch, in.WorktreePath)
	}

	stateDir := filepath.Join(in.WorktreePath, ".harmonik")
	if !isUnderDir(t, stateDir, scratch) {
		t.Errorf("the granted scratch directory %q is not under the run's gitignored state directory %q.\n"+
			"A temp directory inside the checkout puts the agent's scratch files in its own `git status`; "+
			"one inside .git puts them beside the object store. Neither is somewhere to spool a commit message.",
			scratch, stateDir)
	}

	for _, shared := range []string{"/tmp", "/private/tmp", "/var/tmp", "/private/var/tmp", "/"} {
		if slices.Contains(allowWrite, shared) {
			t.Errorf("profile grants the world-shared temp root %q — the scratch directory must be per-run, "+
				"not a blanket temp grant (hk-guapd)", shared)
		}
	}
}

func isUnderDir(t *testing.T, dir, path string) bool {
	t.Helper()
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// TestSandboxWrapPointsTheChildTmpdirAtAGrantedScratchDirectory is the
// assertion that catches the original defect. The profile said what the run may
// write; the wrap decided where the child's temp files go; nothing compared the
// two, and they disagreed.
func TestSandboxWrapPointsTheChildTmpdirAtAGrantedScratchDirectory(t *testing.T) {
	in := scratchProfileInput(t)
	spawn := daemon.ExportedSandboxSpawnForRun(scratchPiSandboxConfig(), core.AgentType("pi"), in)
	if spawn == nil {
		t.Fatalf("gate returned nil spawn — a pi run under backend=srt must be wrapped")
	}

	_, _, extraEnv, err := daemon.ExportedSandboxWrapExecArgv(spawn, "pi", []string{"--mode", "json"})
	if err != nil {
		t.Fatalf("exec wrap: %v", err)
	}

	const key = "CLAUDE_CODE_TMPDIR="
	var childTmpDir string
	for _, kv := range extraEnv {
		if after, ok := strings.CutPrefix(kv, key); ok {
			childTmpDir = after
		}
	}
	if childTmpDir == "" {
		t.Fatalf("the wrap sets no child temp directory: env=%v.\n"+
			"srt reads CLAUDE_CODE_TMPDIR from its own environment and injects TMPDIR into the "+
			"sandboxed child; with it unset the child gets /tmp/claude, one harness's path.", extraEnv)
	}

	if !slices.Contains(profileAllowWrite(t, in), childTmpDir) {
		t.Fatalf("the child's TMPDIR %q is not in the profile's write grant %v — "+
			"the child is pointed at a directory it may not write", childTmpDir, profileAllowWrite(t, in))
	}

	info, statErr := os.Stat(childTmpDir)
	if statErr != nil {
		t.Fatalf("the child's TMPDIR %q does not exist on disk: %v", childTmpDir, statErr)
	}
	if !info.IsDir() {
		t.Fatalf("the child's TMPDIR %q is not a directory", childTmpDir)
	}
}

func removeDeniedCanary(t *testing.T) {
	t.Helper()
	if err := os.Remove(deniedCanaryPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("clear the /tmp denial canary %s: %v", deniedCanaryPath, err)
	}
}

var deniedCanaryPath = fmt.Sprintf("/tmp/harmonik-scratch-tmpdir-denied-canary-%d-%d.txt",
	os.Getpid(), time.Now().UnixNano())

// TestAnUnwrappedLaunchSetsNoTmpdirForTheChild pins the premise the
// implementer's instructions have to survive: on a launch the gate declines to
// wrap, harmonik contributes NO environment, so the child has no TMPDIR from
// anyone.
//
// The gate declines on three live paths — a non-srt backend, a harness absent
// from sandbox.harnesses, and every remote run (DaemonSockPath is a tcp://
// reverse tunnel there). Nothing downstream fills the gap: handler.go assigns
// cmd.Env = spec.Env outright, pi's buildPiEnv emits no TMPDIR, and
// RemoteExecArgv rebuilds the remote `env K=V …` prefix from that same explicit
// slice over a non-login shell.
//
// This test exists because every OTHER test here asserts the sandboxed path,
// and that gap is exactly how an instruction reading "$TMPDIR/commit-msg.txt"
// — which expands to "/commit-msg.txt" when TMPDIR is empty — got as far as
// review. The instruction's own defence is
// TestCommitMessagePathIsWritableWhateverTmpdirTheRunHas in internal/workspace.
func TestAnUnwrappedLaunchSetsNoTmpdirForTheChild(t *testing.T) {
	base := scratchProfileInput(t)

	remoteIn := base
	remoteIn.DaemonSockPath = "tcp://127.0.0.1:53211"

	for _, tc := range []struct {
		name      string
		cfg       projectconfig.SandboxConfig
		agentType core.AgentType
		in        daemon.SandboxProfileInput
	}{
		{"backend_is_not_srt", projectconfig.SandboxConfig{Backend: "none", Harnesses: []string{"pi"}}, core.AgentType("pi"), base},
		{"harness_not_in_the_sandbox_list", scratchPiSandboxConfig(), core.AgentType("claude-code"), base},
		{"remote_run_reaches_the_daemon_over_a_tunnel", scratchPiSandboxConfig(), core.AgentType("pi"), remoteIn},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spawn := daemon.ExportedSandboxSpawnForRun(tc.cfg, tc.agentType, tc.in)
			if spawn != nil {
				t.Fatalf("the gate wrapped a launch it must decline")
			}

			bin, args, extraEnv, err := daemon.ExportedSandboxWrapExecArgv(spawn, "pi", []string{"--mode", "json"})
			if err != nil {
				t.Fatalf("exec wrap: %v", err)
			}
			if bin != "pi" || !slices.Equal(args, []string{"--mode", "json"}) {
				t.Fatalf("an unwrapped launch must pass argv through unchanged, got %q %v", bin, args)
			}
			for _, kv := range extraEnv {
				if strings.HasPrefix(kv, "TMPDIR=") || strings.HasPrefix(kv, "CLAUDE_CODE_TMPDIR=") {
					t.Fatalf("an unwrapped launch contributed %q; the agent's instructions assume it does not "+
						"and carry their own default", kv)
				}
			}
			if len(extraEnv) != 0 {
				t.Fatalf("an unwrapped launch must contribute no environment, got %v", extraEnv)
			}
		})
	}
}

// TestSandboxedShellWritesItsCommitMessageToTmpdirAndStillCannotWriteTmp
// reproduces the reported failure against a real srt: a process inside the
// sandbox writes a commit-message file to $TMPDIR and it lands in the run's
// scratch directory, while the same process's write to /tmp is still refused.
//
// Skips when srt is absent. The unit tests above hold the contract in that case.
func TestSandboxedShellWritesItsCommitMessageToTmpdirAndStillCannotWriteTmp(t *testing.T) {
	if _, err := exec.LookPath("srt"); err != nil {
		t.Skip("srt binary not available; the in-sandbox write repro requires srt")
	}

	in := scratchProfileInput(t)
	spawn := daemon.ExportedSandboxSpawnForRun(scratchPiSandboxConfig(), core.AgentType("pi"), in)
	if spawn == nil {
		t.Fatalf("gate returned nil spawn — a pi run under backend=srt must be wrapped")
	}
	spawn.SrtBinary = "srt"

	removeDeniedCanary(t)
	t.Cleanup(func() { removeDeniedCanary(t) })

	scriptPath := filepath.Join(in.WorktreePath, "write-commit-msg.sh")
	script := "printf 'docs(x): y' > \"$TMPDIR/commit-msg.txt\"\n" +
		"printf 'leaked' > " + deniedCanaryPath + "\n" +
		"exit 0\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o600); err != nil {
		t.Fatalf("write probe script: %v", err)
	}

	bin, args, extraEnv, err := daemon.ExportedSandboxWrapExecArgv(spawn, "sh", []string{scriptPath})
	if err != nil {
		t.Fatalf("exec wrap: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...) //nolint:gosec // G204: argv is produced by the daemon's own wrap, not caller input.
	cmd.Env = append(os.Environ(), extraEnv...)
	out, runErr := cmd.CombinedOutput()

	wrote, readErr := os.ReadFile(filepath.Join(daemon.SandboxScratchDir(in.WorktreePath), "commit-msg.txt"))
	if readErr != nil {
		t.Fatalf("the sandboxed shell could not write a commit-message file to $TMPDIR: %v\n"+
			"srt exit=%v output=%q\n"+
			"This is the reported failure: the agent is told to commit with `git commit -F <file>` and "+
			"has nowhere to put the file (hk-sandbox-no-writable-tmpdir-7484h).",
			readErr, runErr, strings.TrimSpace(string(out)))
	}
	if string(wrote) != "docs(x): y" {
		t.Fatalf("commit-message file holds %q, want %q", wrote, "docs(x): y")
	}

	if _, statErr := os.Stat(deniedCanaryPath); statErr == nil {
		t.Fatalf("the sandboxed shell wrote %s — the scratch grant must be the run's own directory, "+
			"never /tmp. srt output=%q", deniedCanaryPath, strings.TrimSpace(string(out)))
	}
}

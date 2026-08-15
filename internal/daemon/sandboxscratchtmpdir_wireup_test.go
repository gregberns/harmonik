package daemon

// sandboxscratchtmpdir_wireup_test.go — the environment the sandbox wrap
// produces reaches the process a launch actually starts
// (hk-sandbox-no-writable-tmpdir-7484h).
//
// sandboxscratchtmpdir_test.go pins the WRAP: given a spawn config it returns
// CLAUDE_CODE_TMPDIR pointing at a directory the profile grants. That is one
// half of a two-part fact, and it is the half a test can assert without leaving
// the wrap. The other half is that a launch APPLIES what the wrap returned.
// Exactly two lines do that, one per launch path:
//
//	runAgentLaunch                    spec.Env = append(spec.Env, wrapEnv...)
//	(*perRunSubstrate).SpawnWindow    in.Env   = append(in.Env, wrapped.Env...)
//
// Before this file, deleting either line left the whole daemon package green:
// every other test reaches the wrap through the export seam and applies the env
// itself (cmd.Env = append(os.Environ(), extraEnv...)). So the shape this bead
// was filed for — two pieces of code naming a directory and no test that they
// name the SAME one — had simply moved up one level, from "profile vs wrap" to
// "wrap vs launch".
//
// These two tests therefore observe the env on the FAR side of each line: the
// environment the srt process is given on the exec path, and the params handed
// to `tmux new-window` on the substrate path. Neither reads the wrap's return
// value, because reading it is what let the defect through.

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/harness/shared"
	tmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	"github.com/gregberns/harmonik/internal/projectconfig"
	"github.com/gregberns/harmonik/internal/runloop"
	"github.com/gregberns/harmonik/internal/substrate"
)

// childTmpDirEntry is the one environment entry both launch paths must carry.
// Spelled here from the same constant production uses, so a rename of the knob
// moves the test with the code instead of leaving a stale literal behind.
func childTmpDirEntry(worktreePath string) string {
	return srtChildTmpDirEnvVar + "=" + SandboxScratchDir(worktreePath)
}

// wireupSandboxConfig is the live deployment's gate config for agentType: srt
// backend, that harness listed.
func wireupSandboxConfig(agentType core.AgentType) projectconfig.SandboxConfig {
	return projectconfig.SandboxConfig{
		Backend:   "srt",
		Harnesses: []string{string(agentType)},
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Substrate path — tmux new-window
// ─────────────────────────────────────────────────────────────────────────────

// recordingWindowAdapter is a tmux.Adapter that keeps the NewWindowIn params it
// was called with. Everything else is inherited inert from noopTmuxAdapter, so
// this fake states exactly one thing: what the substrate asked tmux for.
type recordingWindowAdapter struct {
	noopTmuxAdapter

	mu     sync.Mutex
	params []tmux.NewWindowIn
}

func (a *recordingWindowAdapter) NewWindowIn(_ context.Context, p tmux.NewWindowIn) tmux.Outcome {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.params = append(a.params, p)
	return tmux.Outcome{Handle: tmux.WindowHandle("harmonik-wireup:w"), PaneID: "%1"}
}

// recorded returns the single NewWindowIn params, failing when the count is not
// one — a spawn that never reached tmux and a spawn that reached it twice are
// both states in which the assertions below would measure nothing.
func (a *recordingWindowAdapter) recorded(t *testing.T) tmux.NewWindowIn {
	t.Helper()
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.params) != 1 {
		t.Fatalf("tmux new-window calls = %d, want exactly 1", len(a.params))
	}
	return a.params[0]
}

// TestTheSubstrateLaunchPathGivesTheSpawnedWindowTheSandboxScratchTmpdir
// watches the substrate wire-up from outside it: the assertion is on the params
// the tmux adapter receives, which is the last thing the daemon controls before
// the agent's environment belongs to tmux.
//
// srt reads CLAUDE_CODE_TMPDIR from its OWN environment, so on this path the
// value has to survive as far as `tmux new-window -e`. A wrap that computed it
// and a SpawnWindow that dropped it would leave the sandboxed agent with
// TMPDIR=/tmp/claude — one harness's path, and not the directory the profile
// was generated to grant.
func TestTheSubstrateLaunchPathGivesTheSpawnedWindowTheSandboxScratchTmpdir(t *testing.T) {
	t.Parallel()

	worktree := t.TempDir()
	adapter := &recordingWindowAdapter{}
	prs := newPerRunSubstrate(&tmuxSubstrate{adapter: adapter, sessionName: "harmonik-wireup"}, "handler", nil)
	if prs == nil {
		t.Fatal("newPerRunSubstrate returned nil — the per-run substrate is what carries the sandbox spawn config, so there is nothing to test")
	}
	prs.sandboxSpawn = sandboxSpawnForRun(wireupSandboxConfig(core.AgentTypePi), core.AgentTypePi, SandboxProfileInput{
		WorktreePath:   worktree,
		GitDir:         filepath.Join(worktree, ".git"),
		RunID:          "substrate-tmpdir-wireup",
		DaemonSockPath: filepath.Join(worktree, "daemon.sock"),
	})
	if prs.sandboxSpawn == nil {
		t.Fatal("the gate declined to wrap a pi run under backend=srt — this test would then assert the absence of an env nothing was asked to produce")
	}

	// A harness-supplied entry, so the assertion below can tell "the sandbox env
	// was appended" from "the environment was replaced wholesale".
	const harnessEntry = "PI_CODING_AGENT_DIR=/nonexistent/agent-dir"
	if _, err := prs.SpawnWindow(t.Context(), handler.SubstrateSpawn{
		WindowName: "wireup",
		Cwd:        worktree,
		Env:        []string{harnessEntry},
		Argv:       []string{"pi", "--mode", "json"},
	}); err != nil {
		t.Fatalf("SpawnWindow: %v", err)
	}

	params := adapter.recorded(t)

	// The paired positive: this spawn really did take the sandbox branch. Without
	// it an env-absence assertion is satisfied for free by a run that was never
	// wrapped at all.
	if !strings.Contains(params.Command, "--settings") {
		t.Fatalf("the spawned command %q is not srt-wrapped, so the env under test was never produced", params.Command)
	}

	want := childTmpDirEntry(worktree)
	if !containsEntry(params.Env, want) {
		t.Fatalf("tmux new-window env = %v, want it to contain %q.\n"+
			"The argv was srt-wrapped and the env was not applied with it, so srt falls back to "+
			"TMPDIR=/tmp/claude for the sandboxed child — one harness's path, not the per-run scratch "+
			"directory this run's profile grants (hk-sandbox-no-writable-tmpdir-7484h).", params.Env, want)
	}
	if !containsEntry(params.Env, harnessEntry) {
		t.Errorf("tmux new-window env = %v, want it to still contain the harness's own entry %q — "+
			"the sandbox env is APPENDED to the harness environment, never assigned over it", params.Env, harnessEntry)
	}
}

// containsEntry reports whether env holds the exact "KEY=VALUE" entry.
func containsEntry(env []string, entry string) bool {
	for _, kv := range env {
		if kv == entry {
			return true
		}
	}
	return false
}

// ─────────────────────────────────────────────────────────────────────────────
// Exec path — the srt process runAgentLaunch starts
// ─────────────────────────────────────────────────────────────────────────────

// wireupHarness is a SessionIDCaptured, ProcessExit harness: the two answers
// that send runAgentLaunch down the EXEC path, where spec.Env becomes the
// child's environment outright (handler.Launch: cmd.Env = spec.Env). Everything
// else is an inert stub, as in agentlaunch_earlyannouncement_test.go.
type wireupHarness struct{ earlyAnnounceHarness }

// NewSessionIDInterceptor passes the stream through and announces nothing. The
// stub srt exits on its own, so this launch needs no announcement kill.
func (h *wireupHarness) NewSessionIDInterceptor(inner io.Reader, _ func(string), _ func()) io.Reader {
	return inner
}

// TestTheExecLaunchPathGivesTheSrtProcessTheSandboxScratchTmpdir watches the
// exec wire-up from outside it: the assertion is on the environment the srt
// PROCESS actually received, dumped by a stub srt on PATH and read back off
// disk. Nothing here reads sandboxWrapExecArgv's return value.
//
// The stub answers two different invocations. The engagement probe
// (verifySandboxEngaged) must see a refused write and a non-zero exit, or the
// launch is abandoned before the wrap ever happens. The launch itself is
// recognised by a marker this test puts in the agent's own argv, and that is
// the invocation whose environment is recorded.
func TestTheExecLaunchPathGivesTheSrtProcessTheSandboxScratchTmpdir(t *testing.T) {
	const launchMarker = "harmonik-tmpdir-wireup-marker"

	projectDir := t.TempDir()
	worktree := filepath.Join(projectDir, ".harmonik", "worktrees", "run")
	if err := os.MkdirAll(worktree, 0o700); err != nil {
		t.Fatalf("make worktree: %v", err)
	}
	envDump := filepath.Join(projectDir, "srt-env.txt")
	argvDump := filepath.Join(projectDir, "srt-argv.txt")

	stubDir := t.TempDir()
	stub := "#!/bin/sh\n" +
		"case \" $* \" in\n" +
		"  *" + launchMarker + "*)\n" +
		"    env > " + envDump + "\n" +
		"    printf '%s\\n' \"$@\" > " + argvDump + "\n" +
		"    exit 0\n" +
		"    ;;\n" +
		"esac\n" +
		// The engagement probe. Report the denial a working sandbox reports and
		// do NOT perform the probe's write, which is what verifySandboxEngaged
		// reads as "the sandbox engaged".
		"echo 'sandbox_init: deny file-write-create' >&2\n" +
		"exit 1\n"
	if err := os.WriteFile(filepath.Join(stubDir, "srt"), []byte(stub), 0o700); err != nil { //nolint:gosec // G306: the stub must be executable
		t.Fatalf("write srt stub: %v", err)
	}
	t.Setenv("PATH", stubDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	harness := &wireupHarness{earlyAnnounceHarness{agentType: core.AgentTypeCodex}}
	reg := handlercontract.NewHarnessRegistry()
	if err := reg.Register(core.AgentTypeCodex, harness); err != nil {
		t.Fatalf("register harness: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	res := runAgentLaunch(ctx, agentLaunchInput{
		Env: runloop.RunEnv{
			ProjectDir:              projectDir,
			AgentReadyTimeout:       time.Second,
			RemoteAgentReadyTimeout: time.Second,
			SandboxCfg:              wireupSandboxConfig(core.AgentTypeCodex),
		},
		Ports: runloop.RunPorts{
			Emitter: alaunchDiscardEmitter{},
			Clock:   substrate.SystemClock{},
		},
		Handles: runloop.SharedHandles{
			AdapterRegistry: handlercontract.NewAdapterRegistry(),
			HarnessRegistry: reg,
			HookStore:       &surviveGateHookStore{},
		},
		RunID:     z8ekRunID(t),
		LogPrefix: "daemon: exec-path tmpdir wire-up test",
		Spec: handler.LaunchSpec{
			Binary:  "/bin/sh",
			Args:    []string{"-c", "exit 0 # " + launchMarker},
			WorkDir: worktree,
			Env:     []string{"PATH=" + os.Getenv("PATH")},
		},
		Artifacts:     shared.LaunchArtifacts{ResolvedAgentType: core.AgentTypeCodex},
		WorktreePath:  worktree,
		DaemonSocket:  filepath.Join(projectDir, ".harmonik", "daemon.sock"),
		BaseSubstrate: &alaunchSpySubstrate{},
	})
	res.Cleanup()

	if res.Fail == agentLaunchPrelaunchFailed {
		t.Fatalf("the launch was refused before it started: %v.\n"+
			"Nothing was exec'd, so the environment assertion below would measure nothing.", res.FailErr)
	}

	// The paired positive: prove the recorded invocation is the LAUNCH and not
	// the engagement probe, and that it carried the srt wrap.
	argv, err := os.ReadFile(argvDump) //nolint:gosec // G304: path is this test's own temp dir
	if err != nil {
		t.Fatalf("the stub srt never ran the agent launch: %v (Fail=%v FailErr=%v).\n"+
			"Either the gate declined to wrap this run or the launch stopped earlier — check the fixture "+
			"before trusting the environment assertion below.", err, res.Fail, res.FailErr)
	}
	if !strings.Contains(string(argv), "--settings") {
		t.Fatalf("the recorded srt argv %q carries no --settings profile, so this is not the wrapped launch", argv)
	}

	raw, err := os.ReadFile(envDump) //nolint:gosec // G304: path is this test's own temp dir
	if err != nil {
		t.Fatalf("the stub srt recorded no environment: %v", err)
	}
	got := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")

	want := childTmpDirEntry(worktree)
	if !containsEntry(got, want) {
		t.Fatalf("the srt process's environment = %v, want it to contain %q.\n"+
			"runAgentLaunch wrapped the argv and did not apply the wrap's env, so srt falls back to "+
			"TMPDIR=/tmp/claude for the sandboxed child — one harness's path, not the per-run scratch "+
			"directory this run's profile grants (hk-sandbox-no-writable-tmpdir-7484h).", got, want)
	}
	if !containsEntry(got, "PATH="+os.Getenv("PATH")) {
		t.Errorf("the srt process's environment = %v, want it to still contain the launch spec's own PATH — "+
			"the sandbox env is APPENDED to spec.Env, never assigned over it", got)
	}

	// A granted path is not a directory: srt stats the child's TMPDIR before any
	// sandboxed process runs, so the launch has to have created it too.
	scratch := SandboxScratchDir(worktree)
	info, statErr := os.Stat(scratch)
	if statErr != nil {
		t.Fatalf("the launch pointed the child at %q and never created it: %v", scratch, statErr)
	}
	if !info.IsDir() {
		t.Fatalf("the child's scratch path %q is not a directory", scratch)
	}
}

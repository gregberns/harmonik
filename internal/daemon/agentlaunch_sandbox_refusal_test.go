package daemon

// agentlaunch_sandbox_refusal_test.go — the launch path must refuse to run a
// real agent when the sandbox engagement probe cannot prove the sandbox applied.
//
// sandboxengagement_proof_test.go pins the probe's own decision. That is not the
// whole property. The probe's doc comment says the CALLER must treat a non-nil
// return as fatal and refuse to launch the agent unsandboxed, and a probe that
// decides correctly while the launch proceeds anyway is the same hole. These two
// tests observe the launch RESULT, so the refusal has to be acted on and not
// merely computed.
//
// The probe honors spawn.SrtBinary, but nothing in production sets it, so the
// launch path resolves "srt" from PATH. These tests put a stub srt first on PATH
// and let the real resolution run. That keeps the wiring under test instead of
// stepping around it.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/harness/shared"
	"github.com/gregberns/harmonik/internal/projectconfig"
	"github.com/gregberns/harmonik/internal/runloop"
	"github.com/gregberns/harmonik/internal/substrate"
)

// sandboxRefusalInput builds a launch that the sandbox gate WILL arm: the
// backend is srt and the run's harness is listed, so sandboxSpawnForRun returns
// a spawn config and the engagement probe runs.
//
// It also puts stubBody on PATH as "srt". t.Setenv makes these tests serial,
// which is the price of exercising the real PATH lookup.
func sandboxRefusalInput(t *testing.T, spy handler.Substrate, stubBody string) agentLaunchInput {
	t.Helper()

	stubDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(stubDir, "srt"), []byte("#!/bin/sh\n"+stubBody+"\n"), 0o700); err != nil { //nolint:gosec // G306: the stub must be executable
		t.Fatalf("write srt stub: %v", err)
	}
	t.Setenv("PATH", stubDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	projectDir := t.TempDir()
	worktree := filepath.Join(projectDir, ".harmonik", "worktrees", "run")
	if err := os.MkdirAll(worktree, 0o700); err != nil {
		t.Fatalf("make worktree: %v", err)
	}

	return agentLaunchInput{
		Env: runloop.RunEnv{
			ProjectDir:              projectDir,
			AgentReadyTimeout:       time.Second,
			RemoteAgentReadyTimeout: time.Second,
			SandboxCfg: projectconfig.SandboxConfig{
				Backend:   "srt",
				Harnesses: []string{string(core.AgentTypeClaudeCode)},
			},
		},
		Ports: runloop.RunPorts{
			Emitter: alaunchDiscardEmitter{},
			Clock:   substrate.SystemClock{},
		},
		Handles: runloop.SharedHandles{
			AdapterRegistry: handlercontract.NewAdapterRegistry(),
		},
		RunID:     z8ekRunID(t),
		LogPrefix: "daemon: sandbox refusal test",
		Spec: handler.LaunchSpec{
			Binary:  "/bin/true",
			WorkDir: worktree,
			Env:     []string{"PATH=/usr/bin"},
		},
		Artifacts:    shared.LaunchArtifacts{ResolvedAgentType: core.AgentTypeClaudeCode},
		WorktreePath: worktree,
		// Absolute and non-empty, so profile generation always succeeds and a
		// refusal can only come from the engagement decision.
		DaemonSocket:  filepath.Join(projectDir, ".harmonik", "daemon.sock"),
		BaseSubstrate: spy,
	}
}

// TestRunAgentLaunch_AnUnprovenSandboxStopsTheLaunchBeforeTheAgentStarts is the
// property the whole gate exists for. The stub srt runs the probe's write
// unsandboxed and exits 0, which is the apply failure the probe was written to
// catch. The agent must never start.
func TestRunAgentLaunch_AnUnprovenSandboxStopsTheLaunchBeforeTheAgentStarts(t *testing.T) {
	spy := &alaunchSpySubstrate{}
	in := sandboxRefusalInput(t, spy, "sh -c \"$4\"\nexit 0")

	res := runAgentLaunch(context.Background(), in)
	res.Cleanup()

	if got := spy.count(); got != 0 {
		t.Errorf("substrate SpawnWindow calls = %d, want 0.\n"+
			"The sandbox could not be proven to have engaged and the agent was launched anyway. "+
			"That is a real agent running unsandboxed on the operator's box, which is the single thing this gate exists to stop.", got)
	}
	if res.Fail != agentLaunchPrelaunchFailed {
		t.Errorf("Fail = %v, want agentLaunchPrelaunchFailed (%v) — the refusal is not recorded, so the caller cannot tell this run from one that launched",
			res.Fail, agentLaunchPrelaunchFailed)
	}
	if res.FailErr == nil {
		t.Fatal("FailErr = nil, want the engagement refusal; the operator gets no reason for the refused launch")
	}
	if !strings.Contains(res.FailErr.Error(), "sandbox") {
		t.Errorf("FailErr = %q, want it to name the sandbox as the cause", res.FailErr)
	}
	if res.Session != nil {
		t.Errorf("Session = %#v, want nil", res.Session)
	}
}

// TestRunAgentLaunch_AProvenSandboxLetsTheAgentStart is the control. Without it
// the zero-spawn assertion above proves nothing, because a launch that could
// never reach the substrate under these inputs satisfies it for free.
//
// The same input with a stub srt that denies the probe's write and reports the
// denial must reach the substrate. The spy then refuses the spawn, so the run
// ends as a launch error rather than a pre-launch refusal — the two failures are
// different facts and the caller reads them differently.
func TestRunAgentLaunch_AProvenSandboxLetsTheAgentStart(t *testing.T) {
	spy := &alaunchSpySubstrate{}
	in := sandboxRefusalInput(t, spy, "echo \"sandbox_init: deny file-write-create\" >&2\nexit 1")

	res := runAgentLaunch(context.Background(), in)
	res.Cleanup()

	if got := spy.count(); got != 1 {
		t.Fatalf("substrate SpawnWindow calls = %d, want 1.\n"+
			"A sandboxed run whose sandbox DID engage never reached the substrate. Fix this fixture before trusting the refusal test's zero-spawn assertion.", got)
	}
	if res.Fail == agentLaunchPrelaunchFailed {
		t.Errorf("Fail = agentLaunchPrelaunchFailed (%v) with FailErr %v, want the launch to pass the gate — a proven sandbox must not refuse the run",
			res.Fail, res.FailErr)
	}
}

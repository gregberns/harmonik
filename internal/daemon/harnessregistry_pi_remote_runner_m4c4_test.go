package daemon

// harnessregistry_pi_remote_runner_m4c4_test.go — remote-substrate M4-C4 (T6):
// the Pi harness onto the SSH runner.
//
// A worker-selected Pi run must spawn the Pi PROCESS on the remote box via the
// SAME SSHRunner the tmux/Claude path uses, WITHOUT altering Pi's landed provider
// wiring ({Provider, BaseURL, API} — decision 6). Pi is a SessionIDCaptured,
// argv-driven ProcessExit harness, so the dispatch path forces spec.Substrate=nil
// (exec path) to expose a stdout pipe for session-id capture. handler.Launch's
// exec path consults spec.Runner: nil ⇒ exec.CommandContext (LOCAL, byte-identical
// NFR7); non-nil ⇒ the runner builds the *exec.Cmd (an SSHRunner tunnels it to the
// worker host).
//
// buildCodexRoutedLaunchSpec is the shared builder for every non-claude harness
// (codex + pi). These tests drive it directly with a PiHarness and a
// RecordingRunner standing in for the per-run worker runner (no real ssh / worker
// touched — same idiom as harnessregistry_remote_hkr36v_test.go).

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	tmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// piRemoteRunCtx builds a fully-configured pi claudeRunCtx (provider tuple +
// base_url) so buildPiLaunchSpec succeeds and emits the base_url wiring.
func piRemoteRunCtx(t *testing.T, ws string, runner tmux.CommandRunner) claudeRunCtx {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(ws, ".harmonik"), 0o750); err != nil {
		t.Fatalf("mkdir .harmonik: %v", err)
	}
	return claudeRunCtx{
		runID:           z8ekRunID(t),
		beadID:          "hk-m4c4-pi",
		workspacePath:   ws,
		phase:           "implementer-initial",
		iterationCount:  1,
		beadTitle:       "pi remote runner",
		beadDescription: "route the pi process to the worker via SSHRunner",
		handlerBinary:   "pi",
		// Landed pi provider config (pi-provider-switch). base_url points at a
		// locally-hosted OpenAI-compatible endpoint (e.g. the DGX). Decision 6: M4
		// must NOT touch this — only WHICH host the pi process runs on changes.
		provider:  "openrouter",
		model:     "openrouter/qwen/qwen3-coder",
		apiKeyEnv: "OPENROUTER_API_KEY",
		baseURL:   "http://dgx.local:8080/v1",
		api:       "openai",
		runner:    runner,
	}
}

// TestBuildPiRoutedLaunchSpec_NoWorker_RunnerNil_NFR7 verifies that with no worker
// selected (rc.runner == nil) the assembled handler.LaunchSpec carries a nil
// Runner, so handler.Launch takes the byte-identical exec.CommandContext local
// path (NFR7: zero/disabled workers ⇒ byte-identical LOCAL pi).
func TestBuildPiRoutedLaunchSpec_NoWorker_RunnerNil_NFR7(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "sk-test-m4c4")
	ctx := context.Background()

	rc := piRemoteRunCtx(t, t.TempDir(), nil) // LOCAL run
	h := NewPiHarness("pi", "openrouter", "openrouter/qwen/qwen3-coder", "OPENROUTER_API_KEY", "", "", "")

	spec, _, err := buildCodexRoutedLaunchSpec(ctx, rc, h, core.AgentTypePi)
	if err != nil {
		t.Fatalf("buildCodexRoutedLaunchSpec (local pi): %v", err)
	}
	if spec.Runner != nil {
		t.Errorf("pi LaunchSpec.Runner = %#v; want nil — a nil runner is the NFR7 "+
			"byte-identical LOCAL exec.CommandContext path", spec.Runner)
	}
}

// TestBuildPiRoutedLaunchSpec_Worker_ThreadsRunner verifies that a worker-selected
// pi run threads the per-run runner VERBATIM into the assembled handler.LaunchSpec,
// so handler.Launch's exec path spawns the pi process through it. In production the
// per-run runner is the worker's tmux.SSHRunner{Host}; here a RecordingRunner
// stands in (no real ssh) and identity is asserted.
func TestBuildPiRoutedLaunchSpec_Worker_ThreadsRunner(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "sk-test-m4c4")
	ctx := context.Background()

	rr := newNoOpRecorderZ8ek()
	rc := piRemoteRunCtx(t, t.TempDir(), rr) // REMOTE run (worker selected)
	h := NewPiHarness("pi", "openrouter", "openrouter/qwen/qwen3-coder", "OPENROUTER_API_KEY", "", "", "")

	spec, _, err := buildCodexRoutedLaunchSpec(ctx, rc, h, core.AgentTypePi)
	if err != nil {
		t.Fatalf("buildCodexRoutedLaunchSpec (remote pi): %v", err)
	}
	if spec.Runner == nil {
		t.Fatal("pi LaunchSpec.Runner = nil for a worker-selected run — the per-run " +
			"runner was NOT threaded into the pi exec path (M4-C4); the pi process " +
			"would spawn on box A instead of the worker")
	}
	if spec.Runner != rr {
		t.Errorf("pi LaunchSpec.Runner = %#v; want the per-run runner threaded verbatim "+
			"(no wrapping) so an SSHRunner{Host} routes the process to the worker", spec.Runner)
	}
}

// TestPiSSHRunner_RoutesProcessToWorkerHost proves the routing property directly:
// the tmux.SSHRunner threaded into the pi LaunchSpec builds an
// `ssh <host> -- pi ...` invocation, so the pi PROCESS runs on the selected worker.
func TestPiSSHRunner_RoutesProcessToWorkerHost(t *testing.T) {
	t.Parallel()
	const workerHost = "gb-mbp"
	runner := tmux.SSHRunner{Host: workerHost}

	// The exact call handler.Launch's exec path makes: Runner.Command(binary, args...).
	cmd := runner.Command(context.Background(), "pi", "--mode", "json", "task text")
	if filepath.Base(cmd.Path) != "ssh" {
		t.Fatalf("SSHRunner.Command produced %q; want an ssh invocation (process routed to worker)", cmd.Path)
	}
	joined := strings.Join(cmd.Args, " ")
	if !strings.Contains(joined, workerHost) {
		t.Errorf("ssh argv %v does not target worker host %q", cmd.Args, workerHost)
	}
	if !strings.Contains(joined, "pi") {
		t.Errorf("ssh argv %v does not carry the pi binary", cmd.Args)
	}
}

// TestBuildPiRoutedLaunchSpec_ProviderConfigIntactAcrossHosts verifies decision 6:
// the base_url provider wiring is byte-identical whether the run is LOCAL (nil
// runner) or REMOTE (worker runner). M4 changes only the host — never the provider
// config. Asserted on the injected PI_CODING_AGENT_DIR env and the generated
// models.json baseUrl.
func TestBuildPiRoutedLaunchSpec_ProviderConfigIntactAcrossHosts(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "sk-test-m4c4")
	ctx := context.Background()

	readBaseURL := func(t *testing.T, runner tmux.CommandRunner) string {
		t.Helper()
		rc := piRemoteRunCtx(t, t.TempDir(), runner)
		h := NewPiHarness("pi", "openrouter", "openrouter/qwen/qwen3-coder", "OPENROUTER_API_KEY", "", "", "")
		spec, _, err := buildCodexRoutedLaunchSpec(ctx, rc, h, core.AgentTypePi)
		if err != nil {
			t.Fatalf("buildCodexRoutedLaunchSpec: %v", err)
		}
		var agentDir string
		for _, kv := range spec.Env {
			if strings.HasPrefix(kv, "PI_CODING_AGENT_DIR=") {
				agentDir = strings.TrimPrefix(kv, "PI_CODING_AGENT_DIR=")
			}
		}
		if agentDir == "" {
			t.Fatal("PI_CODING_AGENT_DIR not injected; base_url provider wiring missing")
		}
		raw, readErr := os.ReadFile(filepath.Join(agentDir, "models.json")) //nolint:gosec // G304: agentDir is test-injected via PI_CODING_AGENT_DIR, not user input
		if readErr != nil {
			t.Fatalf("read models.json: %v", readErr)
		}
		var parsed struct {
			Providers map[string]struct {
				BaseURL string `json:"baseUrl"`
			} `json:"providers"`
		}
		if jsonErr := json.Unmarshal(raw, &parsed); jsonErr != nil {
			t.Fatalf("parse models.json %q: %v", string(raw), jsonErr)
		}
		return parsed.Providers["openrouter"].BaseURL
	}

	const wantBaseURL = "http://dgx.local:8080/v1"
	localBaseURL := readBaseURL(t, nil)
	remoteBaseURL := readBaseURL(t, newNoOpRecorderZ8ek())

	if localBaseURL != wantBaseURL {
		t.Errorf("LOCAL models.json baseUrl = %q; want %q", localBaseURL, wantBaseURL)
	}
	if remoteBaseURL != wantBaseURL {
		t.Errorf("REMOTE models.json baseUrl = %q; want %q — the runner (host) must "+
			"NOT alter the provider wiring (decision 6)", remoteBaseURL, wantBaseURL)
	}
	if localBaseURL != remoteBaseURL {
		t.Errorf("base_url differs by host: local=%q remote=%q; the provider config "+
			"must be UNCHANGED regardless of which host the pi process runs on",
			localBaseURL, remoteBaseURL)
	}
}

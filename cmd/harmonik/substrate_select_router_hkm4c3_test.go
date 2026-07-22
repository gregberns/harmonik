package main

import (
	"context"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/workers"
)

type hkqxvc2ReviewerSubstrateSpy struct{}

func (*hkqxvc2ReviewerSubstrateSpy) SpawnWindow(context.Context, handler.SubstrateSpawn) (handler.SubstrateSession, error) {
	return nil, nil
}

// M4-C3 (T5) — composition-root runner selection for the Codex driver.
//
// These tests pin the three acceptance criteria for making the Codex driver's
// spawn seam worker-selectable at the wire/root while keeping the driver blind
// (RS-017 twin-blindness):
//   (a) zero workers ⇒ LOCAL codex, byte-identical (NFR7);
//   (b) one healthy worker ⇒ codex process routed over SSHRunner{Host};
//   (c) the driver package never imports workers (blindness, structural).

// oneWorkerRegistry builds a live registry with a single ssh worker in the
// requested enabled state — the same shape workers.NewRegistry produces at boot.
func oneWorkerRegistry(enabled bool) *workers.Registry {
	return workers.NewRegistry(workers.Config{
		Version: 1,
		Workers: []workers.Worker{{
			Name:      "gb-mbp",
			Transport: "ssh",
			Host:      "gb-mbp",
			MaxSlots:  1,
			Enabled:   enabled,
		}},
	})
}

// TestCodexRouter_NFR7_ZeroWorkersLocal proves the zero/disabled-worker path is
// byte-identical LOCAL codex: with no registry bound (NFR7) or a disabled
// worker, Command produces a plain local exec of the codex binary — argv[0] is
// the binary itself, NOT an `ssh` wrapper.
func TestCodexRouter_NFR7_ZeroWorkersLocal(t *testing.T) {
	cases := []struct {
		name string
		bind func(*codexWorkerRoutingRunner)
	}{
		{"no registry bound", func(r *codexWorkerRoutingRunner) {}},
		{"nil registry (no worker configured)", func(r *codexWorkerRoutingRunner) { r.setRegistry(nil) }},
		{"registry with disabled worker", func(r *codexWorkerRoutingRunner) { r.setRegistry(oneWorkerRegistry(false)) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := &codexWorkerRoutingRunner{}
			tc.bind(r)
			cmd := r.Command(context.Background(), "codex", "app-server")
			if cmd.Args[0] == "ssh" {
				t.Fatalf("expected LOCAL exec (NFR7), got ssh-wrapped command: %v", cmd.Args)
			}
			// Byte-identical to a bare LocalRunner: same argv.
			wantArgs := []string{"codex", "app-server"}
			if len(cmd.Args) != len(wantArgs) {
				t.Fatalf("argv mismatch: got %v want %v", cmd.Args, wantArgs)
			}
			for i := range wantArgs {
				if cmd.Args[i] != wantArgs[i] {
					t.Fatalf("argv[%d]=%q want %q (full: %v)", i, cmd.Args[i], wantArgs[i], cmd.Args)
				}
			}
		})
	}
}

// nonSSHWorkerRegistry builds a live registry with a single ENABLED but non-ssh
// worker. SelectWorker would bind such a worker, but the router only sandboxes
// over ssh — so with requireBoundary set, Command must REFUSE rather than fall
// through to LocalRunner.
func nonSSHWorkerRegistry() *workers.Registry {
	return workers.NewRegistry(workers.Config{
		Version: 1,
		Workers: []workers.Worker{{
			Name:      "gb-mbp-local",
			Transport: "local",
			Host:      "",
			MaxSlots:  1,
			Enabled:   true,
		}},
	})
}

// TestCodexRouter_RequireBoundary_RefusesUnsandboxed_HK5H759 pins the fail-closed
// runner: when requireBoundary is set (the codexdriver crew path), Command must
// return the deliberately-nonexistent refusal argv0 for EVERY non-(enabled+ssh)
// state — no registry, nil registry, a disabled ssh worker, or an enabled non-ssh
// worker — so exec.Start fails instead of running codex danger-full-access
// UNSANDBOXED on the daemon host. Only an enabled ssh worker routes remotely.
func TestCodexRouter_RequireBoundary_RefusesUnsandboxed_HK5H759(t *testing.T) {
	refuseCases := []struct {
		name string
		bind func(*codexWorkerRoutingRunner)
	}{
		{"no registry bound", func(r *codexWorkerRoutingRunner) {}},
		{"nil registry", func(r *codexWorkerRoutingRunner) { r.setRegistry(nil) }},
		{"disabled ssh worker", func(r *codexWorkerRoutingRunner) { r.setRegistry(oneWorkerRegistry(false)) }},
		{"enabled non-ssh worker", func(r *codexWorkerRoutingRunner) { r.setRegistry(nonSSHWorkerRegistry()) }},
	}
	for _, tc := range refuseCases {
		t.Run("refuse/"+tc.name, func(t *testing.T) {
			r := &codexWorkerRoutingRunner{requireBoundary: true}
			tc.bind(r)
			cmd := r.Command(context.Background(), "codex", "app-server")
			if cmd.Args[0] != refusedIsolationBoundaryArgv0 {
				t.Fatalf("expected fail-closed refusal argv0 %q, got %v", refusedIsolationBoundaryArgv0, cmd.Args)
			}
		})
	}

	t.Run("allow/enabled ssh worker routes remotely", func(t *testing.T) {
		r := &codexWorkerRoutingRunner{requireBoundary: true}
		r.setRegistry(oneWorkerRegistry(true))
		cmd := r.Command(context.Background(), "codex", "app-server")
		if cmd.Args[0] != "ssh" {
			t.Fatalf("enabled ssh worker IS the boundary; expected ssh routing, got %v", cmd.Args)
		}
	})
}

// TestCodexRouter_HealthyWorkerRoutesSSH proves a worker-selected run routes the
// codex process to the worker over SSHRunner{Host}: once the live registry with
// one healthy ssh worker is late-bound (as the daemon does via
// WorkerRegistryObserver), Command emits `ssh <host> -- <codex ...>`.
func TestCodexRouter_HealthyWorkerRoutesSSH(t *testing.T) {
	r := &codexWorkerRoutingRunner{}
	r.setRegistry(oneWorkerRegistry(true))

	cmd := r.Command(context.Background(), "codex", "app-server")
	if cmd.Args[0] != "ssh" {
		t.Fatalf("expected remote routing over ssh, got local exec: %v", cmd.Args)
	}
	joined := strings.Join(cmd.Args, " ")
	if !strings.Contains(joined, "gb-mbp") {
		t.Fatalf("ssh command does not target the worker host gb-mbp: %v", cmd.Args)
	}
	if !strings.Contains(joined, "codex") {
		t.Fatalf("ssh command does not carry the remote codex argv: %v", cmd.Args)
	}
}

// TestCodexRouter_SelectSubstrateWiresObserver proves the composition root wires
// the late-binding observer for the Codex path and that invoking it (as the
// daemon does) flips the runner from LOCAL to remote for a healthy worker —
// end-to-end proof that HARMONIK_SUBSTRATE=codexdriver + a selected worker
// routes remotely, with no per-run substrate reconstruction.
func TestCodexRouter_SelectSubstrateWiresObserver(t *testing.T) {
	t.Setenv(substrateSelectEnv, "codexdriver")
	router := &codexWorkerRoutingRunner{}
	// Exercise the same runner selectSubstrate injects: pre-bind proves the seam.
	opts, _ := codexSubstrateOptions("codex", router)
	rr, ok := opts.Runner.(*codexWorkerRoutingRunner)
	if !ok {
		t.Fatalf("Options.Runner is not the worker-routing runner: %T", opts.Runner)
	}
	// Before the daemon binds a registry: LOCAL (NFR7).
	if got := rr.Command(context.Background(), "codex").Args[0]; got == "ssh" {
		t.Fatalf("pre-bind expected local, got ssh: %v", got)
	}
	// Daemon binds the live registry (WorkerRegistryObserver) → remote.
	rr.setRegistry(oneWorkerRegistry(true))
	if got := rr.Command(context.Background(), "codex").Args[0]; got != "ssh" {
		t.Fatalf("post-bind expected ssh routing, got %v", got)
	}
}

// TestSelectSubstrate_RequireIsolationBoundary_HK5H759 is the composition-root
// contract for the fail-closed guard signal: selectSubstrate returns
// requireIsolationBoundary=true ONLY on the codexdriver path (permissive sandbox
// posture) and false for the tmux default (nothing to guard). The daemon keys
// its fail-closed refusal off this bool, so it must track the substrate choice.
func TestSelectSubstrate_RequireIsolationBoundary_HK5H759(t *testing.T) {
	tmuxSpy := &hkqxvc2ReviewerSubstrateSpy{}
	t.Run("codexdriver_requires_boundary", func(t *testing.T) {
		t.Setenv(substrateSelectEnv, "codexdriver")
		sub, _, requireBoundary, reviewerSubstrate := selectSubstrate(tmuxSpy, "codex")
		if !requireBoundary {
			t.Fatal("codexdriver path must require an isolation boundary (fail-closed signal)")
		}
		if sub == tmuxSpy {
			t.Fatal("codexdriver path must return the codex substrate as its primary substrate")
		}
		if reviewerSubstrate != tmuxSpy {
			t.Fatalf("hk-qxvc2: codexdriver reviewer substrate = %T, want tmux spy", reviewerSubstrate)
		}
	})
	t.Run("tmux_default_no_boundary", func(t *testing.T) {
		t.Setenv(substrateSelectEnv, "")
		sub, _, requireBoundary, reviewerSubstrate := selectSubstrate(tmuxSpy, "codex")
		if requireBoundary {
			t.Fatal("tmux default has no permissive posture; must NOT require a boundary")
		}
		if sub != tmuxSpy || reviewerSubstrate != tmuxSpy {
			t.Fatalf("hk-qxvc2: tmux path substrates = %T / %T, want tmux spy for both", sub, reviewerSubstrate)
		}
	})
}

// TestCodexDriverBlindToWorkers is the RS-017 structural guard: the Codex driver
// package must never import internal/workers — all worker/selection logic lives
// at the composition root. A regression that leaks worker awareness into the
// driver (a runtime worker/test branch) trips this.
func TestCodexDriverBlindToWorkers(t *testing.T) {
	driverDir := filepath.Join("..", "..", "internal", "codexdriver")
	entries, err := os.ReadDir(driverDir)
	if err != nil {
		t.Fatalf("read codexdriver dir: %v", err)
	}
	fset := token.NewFileSet()
	sawGo := false
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		sawGo = true
		f, perr := parser.ParseFile(fset, filepath.Join(driverDir, name), nil, parser.ImportsOnly)
		if perr != nil {
			t.Fatalf("parse %s: %v", name, perr)
		}
		for _, imp := range f.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			if strings.Contains(path, "internal/workers") {
				t.Fatalf("%s imports %q — driver must stay BLIND to workers (RS-017)", name, path)
			}
		}
	}
	if !sawGo {
		t.Fatal("no non-test .go files found in codexdriver — guard would be vacuous")
	}
}

package daemon

// workerregistry_bootwire_test.go — boot-wiring tests for the remote-substrate
// worker registry (remote-substrate B4/B6).
//
// These tests prove the BOOT path — buildWorkerRegistry, the helper that
// newWorkLoopDeps calls to populate deps.workerRegistry — activates remote
// routing. Prior to the wire, deps.workerRegistry was always nil in production
// (NewRegistry/RunHealthCheck were never invoked outside tests), so every bead
// ran local regardless of .harmonik/workers.yaml.
//
// Gate-runnable: the B6 health-check probes are intercepted by a RecordingRunner
// whose CmdFunc delegates to exec.Command("true")/("false") — no real ssh or
// remote host is needed.
//
// Bead ref: hk-rs-b4-bootwire-b44z, hk-rs-b6-healthcheck-isda.

import (
	"context"
	"os/exec"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
	tmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	"github.com/gregberns/harmonik/internal/workers"
)

// oneWorkerCfg returns a v1 workers.Config with a single worker.
func oneWorkerCfg(enabled bool) workers.Config {
	return workers.Config{
		Version: 1,
		Workers: []workers.Worker{
			{
				Name:      "gb-mbp",
				Transport: "ssh",
				Host:      "gb-mbp.local",
				OS:        "darwin",
				RepoPath:  "/Users/worker/harmonik",
				MaxSlots:  2,
				Enabled:   enabled,
			},
		},
	}
}

// passingRunner intercepts every health probe with exec.Command("true") so all
// four probes succeed (worker stays healthy).
func passingRunner() *tmux.RecordingRunner {
	return &tmux.RecordingRunner{
		CmdFunc: func(_ context.Context, _ string, _ ...string) *exec.Cmd {
			return exec.Command("true")
		},
	}
}

// failingRunner intercepts every health probe with exec.Command("false") so the
// first probe (tmux_version) fails, marking the worker unhealthy.
func failingRunner() *tmux.RecordingRunner {
	return &tmux.RecordingRunner{
		CmdFunc: func(_ context.Context, _ string, _ ...string) *exec.Cmd {
			return exec.Command("false")
		},
	}
}

// TestBuildWorkerRegistry_EnabledWorkerActivatesRemoteRouting proves that the
// boot path — not a test-injected registry — produces a non-nil registry whose
// SelectWorker() returns the configured worker. This is the wire that was
// missing: with it, deps.workerRegistry is non-nil and the dispatch path at
// workloop.go (deps.workerRegistry.SelectWorker()) routes remote.
func TestBuildWorkerRegistry_EnabledWorkerActivatesRemoteRouting(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	bus := &handlercontract.CollectingEmitter{}

	// Boot path with a healthy worker (all probes pass).
	reg := buildWorkerRegistryWithRunner(ctx, oneWorkerCfg(true), bus, passingRunner())

	if reg == nil {
		t.Fatal("buildWorkerRegistry: expected non-nil registry for an enabled worker, got nil (remote routing would never activate)")
	}
	w := reg.SelectWorker()
	if w == nil {
		t.Fatal("SelectWorker: expected the configured worker after a passing boot health check, got nil")
	}
	if w.Name != "gb-mbp" {
		t.Fatalf("SelectWorker: got worker name %q, want %q", w.Name, "gb-mbp")
	}
	reg.ReleaseSlot()

	// A passing health check must NOT emit a worker_unhealthy event.
	for _, et := range bus.EventTypes() {
		if et == string(core.EventTypeWorkerUnhealthy) {
			t.Fatalf("passing boot health check emitted unexpected %q event", et)
		}
	}
}

// TestBuildWorkerRegistry_NoWorkerStaysLocal proves NFR7: an empty config (no
// workers) yields a nil registry, so the dispatch path keeps the existing
// local-only branch (deps.workerRegistry == nil ⇒ SelectWorker is never called).
func TestBuildWorkerRegistry_NoWorkerStaysLocal(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	bus := &handlercontract.CollectingEmitter{}

	// Zero-value config (the missing-workers.yaml case).
	if reg := buildWorkerRegistryWithRunner(ctx, workers.Config{}, bus, passingRunner()); reg != nil {
		t.Fatalf("buildWorkerRegistry: expected nil registry for empty config (NFR7 local-only), got %#v", reg)
	}

	// A configured-but-disabled worker is also local-only.
	if reg := buildWorkerRegistryWithRunner(ctx, oneWorkerCfg(false), bus, passingRunner()); reg != nil {
		t.Fatalf("buildWorkerRegistry: expected nil registry for a disabled worker (NFR7 local-only), got %#v", reg)
	}
}

// TestBuildWorkerRegistry_UnhealthyWorkerSkippedAndEventEmitted proves the B6
// boot health check is wired: a worker that fails a probe at boot is disabled in
// the registry (SelectWorker returns nil → its beads run local) and a
// worker_unhealthy event is emitted.
func TestBuildWorkerRegistry_UnhealthyWorkerSkippedAndEventEmitted(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	bus := &handlercontract.CollectingEmitter{}

	reg := buildWorkerRegistryWithRunner(ctx, oneWorkerCfg(true), bus, failingRunner())
	if reg == nil {
		t.Fatal("buildWorkerRegistry: expected non-nil registry even for an unhealthy worker (config entries are never deleted, FR11)")
	}
	if w := reg.SelectWorker(); w != nil {
		t.Fatalf("SelectWorker: expected nil after a failing boot health check (worker disabled), got %q", w.Name)
	}

	// The failing boot health check MUST emit exactly one worker_unhealthy event.
	got := 0
	for _, et := range bus.EventTypes() {
		if et == string(core.EventTypeWorkerUnhealthy) {
			got++
		}
	}
	if got != 1 {
		t.Fatalf("worker_unhealthy emission: got %d events, want 1 (boot health check unwired?)", got)
	}
}

// TestBootHealthRunner_TransportResolution verifies bootHealthRunner returns an
// SSHRunner for transport "ssh" and nil for any other transport (probes skipped).
func TestBootHealthRunner_TransportResolution(t *testing.T) {
	t.Parallel()

	ssh := bootHealthRunner(oneWorkerCfg(true))
	if _, ok := ssh.(tmux.SSHRunner); !ok {
		t.Fatalf("bootHealthRunner: transport ssh → got %T, want tmux.SSHRunner", ssh)
	}

	other := oneWorkerCfg(true)
	other.Workers[0].Transport = "local"
	if r := bootHealthRunner(other); r != nil {
		t.Fatalf("bootHealthRunner: unsupported transport → got %T, want nil", r)
	}

	if r := bootHealthRunner(workers.Config{}); r != nil {
		t.Fatalf("bootHealthRunner: empty config → got %T, want nil", r)
	}
}

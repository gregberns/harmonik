package workers_test

import (
	"context"
	"encoding/json"
	"os/exec"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/workers"
)

// probeFailRunner is a test CommandRunner that fails commands by name.
// It maps each probe's command name to a controlled exit code.
//
//	tmux_version:   name == "tmux"
//	claude_version: name == "claude"
//	git_rev_parse:  name == "git"
//	api_key_absent: name == "sh"
type probeFailRunner struct {
	// failName is the command binary name to fail. "" means all succeed.
	failName string
}

func (r probeFailRunner) Command(ctx context.Context, name string, args ...string) *exec.Cmd {
	if r.failName != "" && name == r.failName {
		return exec.CommandContext(ctx, "sh", "-c", "exit 1")
	}
	return exec.CommandContext(ctx, "sh", "-c", "exit 0")
}

// workerCfg returns a Config with one enabled worker.
func workerCfg() workers.Config {
	return workers.Config{
		Version: 1,
		Workers: []workers.Worker{
			{
				Name:      "test-worker",
				Transport: "ssh",
				Host:      "host.example.com",
				OS:        "darwin",
				RepoPath:  "/repo",
				MaxSlots:  4,
				Enabled:   true,
			},
		},
	}
}

// captureEmit returns an EmitFunc that appends (eventType, payload) pairs to a
// slice so tests can inspect emitted events.
func captureEmit(events *[]struct {
	Type    core.EventType
	Payload []byte
},
) workers.EmitFunc {
	return func(ctx context.Context, et core.EventType, b []byte) error {
		*events = append(*events, struct {
			Type    core.EventType
			Payload []byte
		}{et, b})
		return nil
	}
}

// TestRunHealthCheck_ClaudeVersionFails asserts that a worker is marked unhealthy
// when the claude --version probe fails, the config entry is retained, and a
// worker_unhealthy event is emitted naming the failing probe.
func TestRunHealthCheck_ClaudeVersionFails(t *testing.T) {
	cfg := workerCfg()
	reg := workers.NewRegistry(cfg)
	runner := probeFailRunner{failName: "claude"}

	var captured []struct {
		Type    core.EventType
		Payload []byte
	}
	emit := captureEmit(&captured)

	if err := workers.RunHealthCheck(context.Background(), runner, cfg, reg, emit); err != nil {
		t.Fatalf("RunHealthCheck returned unexpected error: %v", err)
	}

	// Worker must be disabled in the registry.
	if w := reg.SelectWorker(); w != nil {
		t.Fatalf("SelectWorker: expected nil for unhealthy worker, got %+v", *w)
	}

	// Config entry must be retained (not deleted).
	if len(cfg.Workers) != 1 {
		t.Fatalf("cfg.Workers: expected 1 entry retained, got %d", len(cfg.Workers))
	}
	if cfg.Workers[0].Name != "test-worker" {
		t.Fatalf("cfg.Workers[0].Name: expected %q, got %q", "test-worker", cfg.Workers[0].Name)
	}

	// A worker_unhealthy event must have been emitted.
	if len(captured) != 1 {
		t.Fatalf("expected 1 event emitted, got %d", len(captured))
	}
	if captured[0].Type != core.EventTypeWorkerUnhealthy {
		t.Fatalf("event type: got %q, want %q", captured[0].Type, core.EventTypeWorkerUnhealthy)
	}

	var payload workers.WorkerUnhealthyPayload
	if err := json.Unmarshal(captured[0].Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.WorkerName != "test-worker" {
		t.Fatalf("payload.WorkerName: got %q, want %q", payload.WorkerName, "test-worker")
	}
	if payload.FailingProbe != "claude_version" {
		t.Fatalf("payload.FailingProbe: got %q, want %q", payload.FailingProbe, "claude_version")
	}
	if payload.DetectedAt == "" {
		t.Fatal("payload.DetectedAt must not be empty")
	}
}

// TestRunHealthCheck_APIKeyPresent asserts that a worker is marked unhealthy when
// ANTHROPIC_API_KEY is set in the remote environment (api_key_absent probe fails).
// This is the NFR4/D2 fail-closed requirement.
func TestRunHealthCheck_APIKeyPresent(t *testing.T) {
	cfg := workerCfg()
	reg := workers.NewRegistry(cfg)
	// "sh" is the command used by the api_key_absent probe.
	runner := probeFailRunner{failName: "sh"}

	var captured []struct {
		Type    core.EventType
		Payload []byte
	}
	emit := captureEmit(&captured)

	if err := workers.RunHealthCheck(context.Background(), runner, cfg, reg, emit); err != nil {
		t.Fatalf("RunHealthCheck returned unexpected error: %v", err)
	}

	// Worker must be disabled.
	if w := reg.SelectWorker(); w != nil {
		t.Fatalf("SelectWorker: expected nil when API key present, got %+v", *w)
	}

	// Config entry must be retained.
	if len(cfg.Workers) != 1 {
		t.Fatalf("cfg.Workers: expected 1 entry retained, got %d", len(cfg.Workers))
	}

	// A worker_unhealthy event must have been emitted naming api_key_absent.
	if len(captured) != 1 {
		t.Fatalf("expected 1 event emitted, got %d", len(captured))
	}
	var payload workers.WorkerUnhealthyPayload
	if err := json.Unmarshal(captured[0].Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.FailingProbe != "api_key_absent" {
		t.Fatalf("payload.FailingProbe: got %q, want %q", payload.FailingProbe, "api_key_absent")
	}
}

// TestRunHealthCheck_HealthyWorkerSelectable asserts that a worker passing all
// probes remains selectable in the registry.
func TestRunHealthCheck_HealthyWorkerSelectable(t *testing.T) {
	cfg := workerCfg()
	reg := workers.NewRegistry(cfg)
	runner := probeFailRunner{} // all succeed

	if err := workers.RunHealthCheck(context.Background(), runner, cfg, reg, nil); err != nil {
		t.Fatalf("RunHealthCheck returned unexpected error: %v", err)
	}

	w := reg.SelectWorker()
	if w == nil {
		t.Fatal("SelectWorker: expected non-nil for healthy worker, got nil")
	}
	reg.ReleaseSlot()
}

// TestRunHealthCheck_Rerunnable asserts that calling RunHealthCheck twice with
// the same runner does not panic and the worker state is consistent.
func TestRunHealthCheck_Rerunnable(t *testing.T) {
	cfg := workerCfg()
	reg := workers.NewRegistry(cfg)
	runner := probeFailRunner{failName: "claude"}

	for i := range 2 {
		if err := workers.RunHealthCheck(context.Background(), runner, cfg, reg, nil); err != nil {
			t.Fatalf("iteration %d: RunHealthCheck returned error: %v", i, err)
		}
	}

	// Still unhealthy after two calls.
	if w := reg.SelectWorker(); w != nil {
		t.Fatalf("after 2 calls: expected nil for unhealthy worker, got %+v", *w)
	}

	// Config entry retained.
	if len(cfg.Workers) != 1 {
		t.Fatalf("cfg.Workers: expected 1 entry retained, got %d", len(cfg.Workers))
	}
}

// TestRunHealthCheck_DisabledWorkerSkipped asserts that a worker with Enabled=false
// in the config is skipped by the health check.
func TestRunHealthCheck_DisabledWorkerSkipped(t *testing.T) {
	cfg := workers.Config{
		Version: 1,
		Workers: []workers.Worker{
			{
				Name:     "disabled-worker",
				Enabled:  false,
				RepoPath: "/repo",
			},
		},
	}
	reg := workers.NewRegistry(cfg)
	// fail all commands — but since the worker is disabled, RunHealthCheck must not probe it.
	runner := probeFailRunner{failName: "tmux"}

	var captured []struct {
		Type    core.EventType
		Payload []byte
	}
	emit := captureEmit(&captured)

	if err := workers.RunHealthCheck(context.Background(), runner, cfg, reg, emit); err != nil {
		t.Fatalf("RunHealthCheck returned error: %v", err)
	}

	// No events must be emitted for a skipped (Enabled=false) worker.
	if len(captured) != 0 {
		t.Fatalf("expected 0 events for skipped worker, got %d", len(captured))
	}
}

package workers

// health.go — boot-time worker health check (remote-substrate B6).
//
// RunHealthCheck runs four probes against each enabled worker. Any failure marks
// that worker unhealthy in the Registry (SetEnabledByName(name, false)) and emits
// a typed worker_unhealthy event. Config entries are never deleted (FR11).
//
// Bead ref: hk-rs-b6-healthcheck-isda.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// EmitFunc is the callback type for emitting events. Its signature matches
// eventbus.EventBus.Emit so that bus.Emit can be passed directly in production.
// A nil EmitFunc suppresses event emission without error.
type EmitFunc func(ctx context.Context, eventType core.EventType, payload []byte) error

// WorkerUnhealthyPayload is the typed event payload for the worker_unhealthy event
// (remote-substrate B6, §8.16).
//
// Emitted by RunHealthCheck when a health probe fails for an enabled worker.
// Durability class: O (ordinary — operator observability).
type WorkerUnhealthyPayload struct {
	// WorkerName is the name of the worker that failed the probe.
	WorkerName string `json:"worker_name"`
	// FailingProbe is the name of the first probe that failed.
	// One of: tmux_version, claude_version, git_rev_parse, api_key_absent.
	FailingProbe string `json:"failing_probe"`
	// Detail is a human-readable failure description.
	Detail string `json:"detail"`
	// DetectedAt is the RFC 3339 wall-clock timestamp at probe failure.
	DetectedAt string `json:"detected_at"`
}

func init() {
	if err := core.RegisterEventType("worker_unhealthy", func() core.EventPayload { return &WorkerUnhealthyPayload{} }); err != nil {
		panic("workers: init: register worker_unhealthy: " + err.Error())
	}
}

// RunHealthCheck runs four health probes against each enabled worker in cfg
// via runner. Results:
//
//   - Failing worker: disabled in reg by name (SetEnabledByName(name, false));
//     worker_unhealthy event emitted via emit naming the first failing probe.
//   - Passing worker: re-enabled in reg by name (SetEnabledByName(name, true)) so
//     the call is safe to repeat after a transient outage (re-runnable, FR11).
//
// Config entries are never removed.
//
// Probes run in order; the first failure short-circuits the remaining probes
// for that worker:
//
//  1. tmux_version:   tmux -V
//  2. claude_version: claude --version
//  3. git_rev_parse:  git -C <repo_path> rev-parse HEAD
//  4. api_key_absent: sh -c 'test -z "$ANTHROPIC_API_KEY"'
//
// The api_key_absent probe is fail-closed per NFR4/D2: a remote worker that
// already has ANTHROPIC_API_KEY set in its environment would use its own key
// rather than the one harmonik injects, so it is rejected.
//
// emit may be nil; event emission is skipped without error when nil.
//
// Bead ref: hk-rs-b6-healthcheck-isda.
func RunHealthCheck(ctx context.Context, runner tmux.CommandRunner, cfg Config, reg *Registry, emit EmitFunc) error {
	for _, w := range cfg.Workers {
		if !w.Enabled {
			continue
		}
		probeName, detail, err := runProbes(ctx, runner, w)
		if err != nil {
			// Target this specific worker by name. SetEnabledByName is a no-op
			// (returns an error we intentionally ignore) when w is not the
			// worker held by reg, so a failing worker never flips a different
			// worker's Enabled state. This matters once more than one worker is
			// configured; with the v1 single-worker cap it matches SetEnabled.
			_, _ = reg.SetEnabledByName(w.Name, false) //nolint:errcheck // no-op (error intentionally ignored) when w is not the worker held by reg — see comment above
			emitUnhealthyEvent(ctx, w.Name, probeName, detail, emit)
			continue
		}
		_, _ = reg.SetEnabledByName(w.Name, true) //nolint:errcheck // no-op (error intentionally ignored) when w is not the worker held by reg — see comment above
	}
	return nil
}

// probeSpec describes a single health-check probe.
type probeSpec struct {
	name string   // event payload probe name
	argv []string // command + args passed to runner.Command
}

// runProbes executes all four health probes against w using runner.
// Returns the probe name, a human-readable detail string, and a non-nil error
// on first failure. Returns ("", "", nil) when all probes pass.
func runProbes(ctx context.Context, runner tmux.CommandRunner, w Worker) (probeName, detail string, err error) {
	probes := []probeSpec{
		{name: "tmux_version", argv: []string{"tmux", "-V"}},
		{name: "claude_version", argv: []string{"claude", "--version"}},
		{name: "git_rev_parse", argv: []string{"git", "-C", w.RepoPath, "rev-parse", "HEAD"}},
		{name: "api_key_absent", argv: []string{"sh", "-c", `test -z "$ANTHROPIC_API_KEY"`}},
	}
	for _, ps := range probes {
		cmd := runner.Command(ctx, ps.argv[0], ps.argv[1:]...)
		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &out
		if rerr := cmd.Run(); rerr != nil {
			return ps.name, fmt.Sprintf("probe %s failed: %v", ps.name, rerr), rerr
		}
	}
	return "", "", nil
}

// emitUnhealthyEvent marshals and emits a worker_unhealthy event.
// No-op when emit is nil.
func emitUnhealthyEvent(ctx context.Context, workerName, probeName, detail string, emit EmitFunc) {
	if emit == nil {
		return
	}
	p := WorkerUnhealthyPayload{
		WorkerName:   workerName,
		FailingProbe: probeName,
		Detail:       detail,
		DetectedAt:   time.Now().UTC().Format(time.RFC3339),
	}
	b, err := json.Marshal(p)
	if err != nil {
		return
	}
	_ = emit(ctx, core.EventTypeWorkerUnhealthy, b)
}

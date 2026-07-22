package workers

// bootwire.go — boot-time construction of the remote-worker Registry
// (remote-substrate B4/B6), lifted out of internal/daemon/workloop.go by P2
// unit E4c (plans/2026-07-21-p2-extraction/E4-ssh.md §4).
//
// The daemon's newWorkLoopDeps calls BuildRegistry to populate
// deps.workerRegistry; everything the construction needs — Config, NewRegistry,
// RunHealthCheck, EmitFunc and the tmux.CommandRunner transport seam — already
// lives in this package, so the wiring belongs here rather than in the monolith.
//
// Bead ref: hk-rs-b4-bootwire-b44z, hk-rs-b6-healthcheck-isda, hk-qmyis.

import (
	"context"
	"log/slog"

	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// BuildRegistry turns the loaded Config into a live *Registry and runs the
// boot-time health check (remote-substrate B4/B6).
//
// It returns nil — keeping the dispatch path on the existing local-only branch
// (NFR7) — ONLY when NO worker is CONFIGURED (empty workers.yaml). When at least
// one worker is configured it ALWAYS builds the registry, even if every worker
// booted with enabled:false, so a later live `harmonik worker enable <name>`
// (hk-xjbvi) can flip the worker selectable WITHOUT a daemon restart. A
// disabled-at-boot worker is still local-only at dispatch time: SelectWorker
// returns nil while Enabled==false, so dispatch behaviour is byte-identical to
// the old nil-for-disabled case until an operator enables it. When a worker is
// configured it:
//
//  1. Constructs the registry via NewRegistry (B5 selection + slot tracking).
//  2. Runs RunHealthCheck over the worker's transport runner (B6), which probes
//     tmux/claude/git/no-API-key, disables (SetEnabled(false)) any worker that
//     fails a probe, and emits a worker_unhealthy event via emit.
//     A worker that fails the boot health check is therefore SelectWorker()-skipped
//     so its beads run locally rather than against an unhealthy host. The runner
//     is nil for an all-disabled config (BootHealthRunner skips disabled workers),
//     so an all-disabled config builds the registry but runs no probes.
//
// The runner for the health check is tmux.SSHRunner{Host: worker.Host} for
// transport "ssh" (the only supported transport); other transports run no probes
// and the worker stays enabled as configured.
//
// Bead ref: hk-rs-b4-bootwire-b44z, hk-rs-b6-healthcheck-isda.
func BuildRegistry(ctx context.Context, cfg Config, emit EmitFunc) *Registry {
	return BuildRegistryWithRunner(ctx, cfg, emit, BootHealthRunner(cfg))
}

// BuildRegistryWithRunner is the runner-injectable core of BuildRegistry.
// Production passes the transport-resolved runner from BootHealthRunner; tests
// pass a recording/no-op runner so the boot path is exercisable without real ssh.
//
// runner == nil ⇒ the B6 boot health check is skipped (the worker stays enabled
// as configured); this is also the unsupported-transport AND all-disabled
// behaviour (the registry is built but no probes run).
func BuildRegistryWithRunner(ctx context.Context, cfg Config, emit EmitFunc, runner tmux.CommandRunner) *Registry {
	// Build the registry whenever a worker is CONFIGURED — not only when one is
	// ENABLED — so a live `worker enable` (hk-xjbvi) has a registry to flip without
	// a restart. An all-disabled config still dispatches local-only because
	// SelectWorker returns nil while Enabled==false (verified identical to the old
	// nil-registry path). Zero configured workers stays nil (NFR7 local-only).
	if len(cfg.Workers) == 0 {
		return nil
	}

	// hk-qmyis: make "workers.yaml has entries but none enabled" visible at
	// startup — previously this looked identical to "no workers.yaml at all"
	// because both paths were silent. The registry is rebuilt at process start
	// only, so editing workers.yaml under a running daemon is a no-op until the
	// next restart; the warning below says so explicitly.
	enabledCount := 0
	for _, w := range cfg.Workers {
		if w.Enabled {
			enabledCount++
		}
	}
	slog.InfoContext(ctx, "worker_registry_init", "workers_loaded", len(cfg.Workers), "workers_enabled", enabledCount)
	if enabledCount == 0 {
		slog.WarnContext(ctx, "remote routing DISABLED (0 enabled workers); restart the daemon after editing workers.yaml to pick up changes")
	}

	reg := NewRegistry(cfg)

	// B6 boot health check: probe each enabled worker over its transport runner.
	// On a probe failure the worker is disabled in-registry and a worker_unhealthy
	// event is emitted, so SelectWorker() skips it and the run falls back to local.
	if runner != nil {
		if err := RunHealthCheck(ctx, runner, cfg, reg, emit); err != nil {
			slog.ErrorContext(ctx, "worker boot health check failed", "error", err)
		}
	}
	return reg
}

// BootHealthRunner resolves the CommandRunner used for the boot health-check
// probes against the (single, v1) enabled worker. Returns an SSHRunner for
// transport "ssh"; nil for any other transport (probes skipped, worker stays
// enabled as configured).
func BootHealthRunner(cfg Config) tmux.CommandRunner {
	for _, w := range cfg.Workers {
		if !w.Enabled {
			continue
		}
		if w.Transport == "ssh" {
			return tmux.SSHRunner{Host: w.Host}
		}
		return nil
	}
	return nil
}

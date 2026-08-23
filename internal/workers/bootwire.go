package workers

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
	if len(cfg.Workers) == 0 {
		return nil
	}

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

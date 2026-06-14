package main

import "github.com/gregberns/harmonik/internal/workers"

// applyWorkerOverrides merges CLI flag overrides into cfg following
// flag > file > default precedence. Only flags present in explicitFlags
// (i.e. explicitly passed on the command line) override the file value.
// When no worker entry exists in the file, overrides are silently ignored
// (wiring only — B5/B6/B10 own registry/health/dispatch).
// Returns a copy; the input cfg is not mutated.
//
// Bead ref: hk-rs-b4-bootwire-b44z.
func applyWorkerOverrides(cfg workers.Config, explicitFlags map[string]bool, hostFlag string, enabledFlag bool) workers.Config {
	if len(cfg.Workers) == 0 {
		return cfg
	}
	// Copy the workers slice so the caller's Config is not mutated.
	out := cfg
	copied := make([]workers.Worker, len(cfg.Workers))
	copy(copied, cfg.Workers)
	out.Workers = copied

	if explicitFlags["worker-host"] {
		out.Workers[0].Host = hostFlag
	}
	if explicitFlags["worker-enabled"] {
		out.Workers[0].Enabled = enabledFlag
	}
	return out
}

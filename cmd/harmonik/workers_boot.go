package main

import "github.com/gregberns/harmonik/internal/workers"

func applyWorkerOverrides(cfg workers.Config, explicitFlags map[string]bool, hostFlag string, enabledFlag bool) workers.Config {
	idx := workers.PrimaryWorkerIndex(cfg)
	if idx < 0 {
		return cfg
	}
	out := cfg
	copied := make([]workers.Worker, len(cfg.Workers))
	copy(copied, cfg.Workers)
	out.Workers = copied

	if explicitFlags["worker-host"] {
		out.Workers[idx].Host = hostFlag
	}
	if explicitFlags["worker-enabled"] {
		out.Workers[idx].Enabled = enabledFlag
	}
	return out
}

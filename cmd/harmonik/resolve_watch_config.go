package main

import (
	"fmt"
	"strings"

	"github.com/gregberns/harmonik/internal/projectconfig"
)

type requiredWatchValue struct {
	keyPath     string
	description string
	satisfied   bool
}

// WatchConfigMissingError is returned when one or more required watch values are
// unset. Unlike KeeperConfigMissingError (whose Missing is []string), Missing
// here carries both the key path AND its description — enabling
// "KeyPath — Description" error rendering.
type WatchConfigMissingError struct {
	// ProjectDir is the project root whose .harmonik/config.yaml needs the keys.
	ProjectDir string
	// Missing is every unsatisfied required watch value (satisfied=false).
	Missing []requiredWatchValue
}

func (e *WatchConfigMissingError) Error() string {
	dir := e.ProjectDir
	if dir == "" {
		dir = "<project>"
	}
	parts := make([]string, len(e.Missing))
	for i, m := range e.Missing {
		parts[i] = m.keyPath + " — " + m.description
	}
	return fmt.Sprintf(
		"refusing to start watch — required watch config values are unset in %s: %s. "+
			"Fix: run 'harmonik watch config --example' to see the watch: block template, "+
			"add it to %s/.harmonik/config.yaml.",
		dir, strings.Join(parts, "; "), dir)
}

func allWatchValues(cfg projectconfig.WatchConfig) []requiredWatchValue {
	return []requiredWatchValue{
		{
			keyPath:     "watch.status_target",
			description: "comms --to target for crew status feeds; defaults to 'captain' when absent",
			satisfied:   true, // always: defaults to "captain" (§7 exception, WE7)
		},
		{
			keyPath:     "watch.opsmonitor_target",
			description: "comms --to target for ops-monitor watch-class signals; defaults to 'captain' when absent",
			satisfied:   true, // always: defaults to "captain" (§7 exception, WE7)
		},
		{
			keyPath:     "watch.absent_thresh_s",
			description: "seconds watch may be absent from comms-who before watch-down fires (WE9 dual-probe; fail-loud when unset)",
			satisfied:   cfg.AbsentThreshSec > 0,
		},
		{
			keyPath:     "watch.stall_ticks",
			description: "consecutive ops-monitor ticks the watch cursor may be frozen (with pending events) before watch-stalled fires (WE9 cursor-advancement; fail-loud when unset)",
			satisfied:   cfg.StallTicks > 0,
		},
		{
			keyPath:     "watch.liveness_interval",
			description: "Go duration string (e.g. '1h') for the watch<->captain mutual-liveness ping schedule (WE6; fail-loud when unset)",
			satisfied:   cfg.LivenessInterval != "",
		},
		{
			keyPath:     "watch.digest_interval",
			description: "Go duration string (e.g. '1h') for the watch verify-services-up schedule (WE6; fail-loud when unset)",
			satisfied:   cfg.DigestInterval != "",
		},
		{
			keyPath:     "watch.staffing_starvation_grace",
			description: "consecutive ops-monitor digests a 'ready lane + free slot' condition may persist with NO captain staffing action before the watch escalates the staffing-starvation backstop (fail-loud when unset)",
			satisfied:   cfg.StaffingStarvationGrace > 0,
		},
	}
}

func checkMissingWatchValues(cfg projectconfig.WatchConfig) []requiredWatchValue {
	var missing []requiredWatchValue
	for _, v := range allWatchValues(cfg) {
		if !v.satisfied {
			missing = append(missing, v)
		}
	}
	return missing
}

// ResolveWatchTargets returns the effective routing targets for crew status feeds
// and ops-monitor watch-class signals. Both default to "captain" when absent from
// config (NOT fail-loud — §7 exception, WE7 load-bearing).
//
// The "captain" default is LOAD-BEARING: it preserves existing captain-directed
// routing when the watch: block is absent, making WE7 inert until a runtime
// config flip.
func ResolveWatchTargets(cfg projectconfig.WatchConfig) (statusTarget, opsmonitorTarget string) {
	statusTarget = cfg.StatusTarget
	if statusTarget == "" {
		statusTarget = "captain"
	}
	opsmonitorTarget = cfg.OpsmonitorTarget
	if opsmonitorTarget == "" {
		opsmonitorTarget = "captain"
	}
	return statusTarget, opsmonitorTarget
}

const watchConfigExampleBlock = `watch:
  # Target routing — both default to 'captain' when absent (LOAD-BEARING default).
  # Flip to 'watch' ONLY after MVP-standup AND 'keeper doctor watch' is green (WE7 §11).
  # watch.status_target: comms --to target for crew status feeds; defaults to 'captain' when absent
  status_target: captain
  # watch.opsmonitor_target: comms --to target for ops-monitor watch-class signals; defaults to 'captain' when absent
  opsmonitor_target: captain
  # Liveness thresholds (WE9 — fail-loud when unset with opsmonitor_target=watch).
  # watch.absent_thresh_s: seconds watch may be absent from comms-who before watch-down fires (WE9 dual-probe; fail-loud when unset)
  absent_thresh_s: 600
  # watch.stall_ticks: consecutive ops-monitor ticks the watch cursor may be frozen (with pending events) before watch-stalled fires (WE9 cursor-advancement; fail-loud when unset)
  stall_ticks: 3
  # Schedule intervals (WE6 — fail-loud when unset; NO literal fallback in daemon).
  # watch.liveness_interval: Go duration string (e.g. '1h') for the watch<->captain mutual-liveness ping schedule (WE6; fail-loud when unset)
  liveness_interval: 1h
  # watch.digest_interval: Go duration string (e.g. '1h') for the watch verify-services-up schedule (WE6; fail-loud when unset)
  digest_interval: 1h
  # Staffing-starvation backstop (fail-loud when unset).
  # watch.staffing_starvation_grace: consecutive ops-monitor digests a 'ready lane + free slot' condition may persist with NO captain staffing action before the watch escalates the staffing-starvation backstop (fail-loud when unset)
  staffing_starvation_grace: 3
`

func watchConfigExampleYAML() string {
	return watchConfigExampleBlock
}

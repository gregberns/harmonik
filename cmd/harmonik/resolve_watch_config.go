package main

import (
	"fmt"
	"strings"

	"github.com/gregberns/harmonik/internal/daemon"
)

// resolve_watch_config.go — watch routing target resolver + single-source carrier.
//
// Introduces requiredWatchValue{KeyPath, Description string, satisfied bool},
// which extends the requiredKeeperValue pattern (resolve_keeper_config.go) with
// a Description field. The Description feeds BOTH the missing-key error
// (rendered "KeyPath — Description") AND watchConfigExampleYAML() so the two
// cannot drift (parity-tested by TestWatchConfigParityWE7).
//
// WE7 §7 exception: the two TARGET keys (status_target, opsmonitor_target)
// default to "captain" and are NOT fail-loud. Behavioral keys (e.g.
// escalation_target, WE9+) will use satisfied=cfg.KeyPresent and will
// fail-loud when absent.
//
// ROLLOUT GATE: merging WE7 is inert — defaults preserve existing captain-
// directed behavior. Flip to "watch" ONLY after MVP-standup AND
// 'keeper doctor watch' is green.
//
// Bead ref: hk-we7-sender-redirect-clhh8.

// requiredWatchValue describes one watch config value: its dotted yaml key path,
// a human description that is the SINGLE source of truth for BOTH the missing-key
// error (rendered "KeyPath — Description") and the --example output (parity-tested
// by TestWatchConfigParityWE7). Extends requiredKeeperValue with Description.
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

// allWatchValues returns the canonical list of ALL watch config values.
// The Description field is the SINGLE source of truth shared by
// checkMissingWatchValues (error path) and watchConfigExampleYAML (--example).
//
// TARGET KEYS (WE7): satisfied=true always (default "captain", NOT fail-loud).
// BEHAVIORAL KEYS (WE9+): satisfied based on config presence (fail-loud when absent).
func allWatchValues(cfg daemon.WatchConfig) []requiredWatchValue {
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
	}
}

// checkMissingWatchValues returns every requiredWatchValue where satisfied=false.
// For WE7 (target keys only), this is always empty — target keys default to
// "captain" and are never fail-loud. Future behavioral keys (WE9+) will populate
// this slice when absent from config.
func checkMissingWatchValues(cfg daemon.WatchConfig) []requiredWatchValue {
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
func ResolveWatchTargets(cfg daemon.WatchConfig) (statusTarget, opsmonitorTarget string) {
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

// watchConfigExampleBlock is the complete, commented watch: block template.
// The comment text for each key MUST appear verbatim in the corresponding
// Description field of allWatchValues() — TestWatchConfigParityWE7 enforces
// this single-source-of-truth invariant.
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
`

// watchConfigExampleYAML returns the complete watch: example block.
// Single source of truth for 'harmonik watch config --example'.
func watchConfigExampleYAML() string {
	return watchConfigExampleBlock
}

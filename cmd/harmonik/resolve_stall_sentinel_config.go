package main

// resolve_stall_sentinel_config.go — stall-sentinel config resolver (hk-hm09z).
//
// # OPERATOR-FACING CHOKEPOINT — imposes NO built-in defaults at runtime.
//
// ResolveStallSentinelConfig is the validation gate for the stall_sentinel: block
// in .harmonik/config.yaml. Per the no-hardcoded-thresholds mandate (the same
// operator-philosophy that governs the keeper and watch config), the product
// imposes ZERO baked stall-sentinel defaults: EVERY required value must be set
// by the operator. When a required value is unset the resolver AGGREGATES all the
// missing keys and returns a single *StallSentinelConfigMissingError so the
// sentinel REFUSES TO START — it never silently defaults.
//
// Why it lives in cmd/harmonik (NOT internal/daemon or internal/sentinel): the
// resolver needs daemon.StallSentinelConfig (the parsed .harmonik/config.yaml
// stall_sentinel: block), and depguard bans internal packages from importing
// internal/daemon (.golangci.yml). This mirrors the keeper resolver pattern
// (resolve_keeper_config.go) and the watch resolver pattern (resolve_watch_config.go).
//
// # Config layout (stall_sentinel: block)
//
//	stall_sentinel:
//	  escalation:
//	    tier1_crew:     <Go duration>  # X — escalate to crew after this stall age
//	    tier2_captain:  <Go duration>  # Y — escalate to captain
//	    tier3_operator: <Go duration>  # Z — escalate to operator mailbox
//	  detection:
//	    run_silence_stall:     <Go duration>  # Layer A: heartbeat-gap trigger
//	    review_finalize_stall: <Go duration>  # Layer A: review-stall trigger
//	    run_max_age:           <Go duration>  # Layer A: run-age backstop
//	    lane_noprogress_stall: <Go duration>  # Layer B: no-forward-progress trigger
//
// All 7 keys are required. Absent → refuse to start. Off/0s is a valid explicit
// value when the operator consciously disables a threshold; the resolver accepts
// zero when the key is PRESENT (duration = 0s) but reports it as MISSING when
// the duration is zero because the key was ABSENT from config.yaml.
//
// # Fail-loud
//
// *StallSentinelConfigMissingError — one or more required values are UNSET.
// Aggregates ALL missing keys into ONE error (operator sees them all at once).
// The message names the real dotted yaml key paths and points at
// 'harmonik sentinel config --example'.
//
// Bead ref: hk-hm09z.
// Spec ref: .kerf/works/stall-sentinel/SPEC.md §3/§5, plans/2026-07-02-stall-sentinel/DESIGN.md §3/§5.

import (
	"fmt"
	"strings"
	"time"

	"github.com/gregberns/harmonik/internal/daemon"
)

// requiredStallSentinelValue describes one required stall-sentinel config value:
// its dotted yaml key path (for the missing-value error and --example parity),
// a human description, and whether the operator supplied it.
type requiredStallSentinelValue struct {
	keyPath     string
	description string
	satisfied   bool
}

// StallSentinelConfigMissingError is returned by ResolveStallSentinelConfig when
// one or more REQUIRED stall-sentinel values are unset (no config value).
// It aggregates EVERY missing key (not just the first) so the operator can fix
// them all in one pass, and its message names the real dotted yaml key paths plus
// points at 'harmonik sentinel config --example'.
//
// Bead ref: hk-hm09z.
type StallSentinelConfigMissingError struct {
	// ProjectDir is the project root whose .harmonik/config.yaml needs the keys.
	ProjectDir string
	// Missing is every unsatisfied required stall-sentinel value.
	Missing []requiredStallSentinelValue
}

func (e *StallSentinelConfigMissingError) Error() string {
	dir := e.ProjectDir
	if dir == "" {
		dir = "<project>"
	}
	parts := make([]string, len(e.Missing))
	for i, m := range e.Missing {
		parts[i] = m.keyPath + " — " + m.description
	}
	return fmt.Sprintf(
		"refusing to start stall-sentinel — required stall_sentinel config values are unset in %s: %s. "+
			"Fix: run 'harmonik sentinel config --example' to see the stall_sentinel: block template, "+
			"add it to %s/.harmonik/config.yaml.",
		dir, strings.Join(parts, "; "), dir)
}

// allStallSentinelValues returns the canonical list of ALL stall-sentinel config
// values. The Description field is the SINGLE source of truth shared by
// checkMissingStallSentinelValues (error path) and stallSentinelConfigExampleYAML
// (--example). A field is satisfied when its parsed duration is non-zero (set to
// any valid Go duration, including 0s which the operator explicitly wrote as the
// key).
//
// NOTE: 0-duration values can only arise from an explicit `key: 0s` in config
// (parseDurationField returns 0 for empty string = absent, and 0 for "0s" = set).
// Since "absent" and "0s" map to the same zero time.Duration, the resolver treats
// ALL zero values as missing. This is intentional: 0s thresholds are nonsensical
// for stall detection (they'd fire on every tick), and the example block shows
// non-zero starting values the operator must own.
func allStallSentinelValues(cfg daemon.StallSentinelConfig) []requiredStallSentinelValue {
	return []requiredStallSentinelValue{
		// ── escalation tiers (X/Y/Z) ──
		{
			keyPath:     "stall_sentinel.escalation.tier1_crew",
			description: "Go duration after stall detection before escalating to the crew (Tier 1 / X; fail-loud when unset)",
			satisfied:   cfg.Tier1Crew > 0,
		},
		{
			keyPath:     "stall_sentinel.escalation.tier2_captain",
			description: "Go duration after stall detection before escalating to the captain (Tier 2 / Y; fail-loud when unset)",
			satisfied:   cfg.Tier2Captain > 0,
		},
		{
			keyPath:     "stall_sentinel.escalation.tier3_operator",
			description: "Go duration after stall detection before escalating to the operator mailbox (Tier 3 / Z; fail-loud when unset)",
			satisfied:   cfg.Tier3Operator > 0,
		},
		// ── Layer A per-run detection thresholds ──
		{
			keyPath:     "stall_sentinel.detection.run_silence_stall",
			description: "Go duration of no agent_heartbeat/agent_message before Layer A heartbeat-gap stall fires (fail-loud when unset)",
			satisfied:   cfg.RunSilenceStall > 0,
		},
		{
			keyPath:     "stall_sentinel.detection.review_finalize_stall",
			description: "Go duration after reviewer_verdict before Layer A review-finalize stall fires when no run_completed follows (fail-loud when unset)",
			satisfied:   cfg.ReviewFinalizeStall > 0,
		},
		{
			keyPath:     "stall_sentinel.detection.run_max_age",
			description: "Go duration backstop: any run older than this with no terminal event triggers Layer A run-age stall (fail-loud when unset)",
			satisfied:   cfg.RunMaxAge > 0,
		},
		// ── Layer B lane no-forward-progress detection threshold ──
		{
			keyPath:     "stall_sentinel.detection.lane_noprogress_stall",
			description: "Go duration a lane may have an expectation of progress with zero forward-progress events before Layer B fires (fail-loud when unset)",
			satisfied:   cfg.LaneNoprogressStall > 0,
		},
	}
}

// checkMissingStallSentinelValues returns every requiredStallSentinelValue where
// satisfied=false. Empty result = all required values supplied.
func checkMissingStallSentinelValues(cfg daemon.StallSentinelConfig) []requiredStallSentinelValue {
	var missing []requiredStallSentinelValue
	for _, v := range allStallSentinelValues(cfg) {
		if !v.satisfied {
			missing = append(missing, v)
		}
	}
	return missing
}

// ResolvedStallSentinelConfig is the post-validation stall-sentinel config the
// sentinel startup path uses. Every field is the EFFECTIVE value as supplied by
// the operator; no field is left at a zero/default value (the missing-value gate
// above guaranteed all required values are present).
//
// Bead ref: hk-hm09z.
type ResolvedStallSentinelConfig struct {
	// Escalation tiers (X/Y/Z).
	Tier1Crew     time.Duration
	Tier2Captain  time.Duration
	Tier3Operator time.Duration
	// Layer A per-run detection thresholds.
	RunSilenceStall     time.Duration
	ReviewFinalizeStall time.Duration
	RunMaxAge           time.Duration
	// Layer B lane no-forward-progress threshold.
	LaneNoprogressStall time.Duration
}

// ResolveStallSentinelConfig validates the parsed stall_sentinel: config block
// and returns the resolved config the stall-sentinel startup path uses. It
// aggregates ALL missing keys into a single *StallSentinelConfigMissingError so
// the operator can fix them in one pass.
//
// projectDir names the project root (used in the missing-value message so the
// operator knows which .harmonik/config.yaml to edit).
//
// Bead ref: hk-hm09z.
func ResolveStallSentinelConfig(cfg daemon.StallSentinelConfig, projectDir string) (ResolvedStallSentinelConfig, error) {
	if missing := checkMissingStallSentinelValues(cfg); len(missing) > 0 {
		return ResolvedStallSentinelConfig{}, &StallSentinelConfigMissingError{
			ProjectDir: projectDir,
			Missing:    missing,
		}
	}
	return ResolvedStallSentinelConfig{
		Tier1Crew:           cfg.Tier1Crew,
		Tier2Captain:        cfg.Tier2Captain,
		Tier3Operator:       cfg.Tier3Operator,
		RunSilenceStall:     cfg.RunSilenceStall,
		ReviewFinalizeStall: cfg.ReviewFinalizeStall,
		RunMaxAge:           cfg.RunMaxAge,
		LaneNoprogressStall: cfg.LaneNoprogressStall,
	}, nil
}

// stallSentinelConfigExampleBlock is the complete, commented stall_sentinel: block
// template. The comment text for each key MUST appear verbatim in the corresponding
// Description field of allStallSentinelValues() — tests enforce this single-source-
// of-truth invariant.
//
// Suggested starting values: pick values wider than real incident durations so the
// sentinel is sensitive but not noisy. The operator owns and tunes these numbers.
const stallSentinelConfigExampleBlock = `stall_sentinel:
  # Escalation tiers — X/Y/Z in the DESIGN.md brief.
  # Each is a Go duration string (e.g. '10m', '1h30m'). All three are required.
  escalation:
    # stall_sentinel.escalation.tier1_crew: Go duration after stall detection before escalating to the crew (Tier 1 / X; fail-loud when unset)
    tier1_crew: 10m
    # stall_sentinel.escalation.tier2_captain: Go duration after stall detection before escalating to the captain (Tier 2 / Y; fail-loud when unset)
    tier2_captain: 25m
    # stall_sentinel.escalation.tier3_operator: Go duration after stall detection before escalating to the operator mailbox (Tier 3 / Z; fail-loud when unset)
    tier3_operator: 45m
  # Detection thresholds — tune to match your fleet's typical activity cadence.
  # All are Go duration strings. All four are required.
  detection:
    # stall_sentinel.detection.run_silence_stall: Go duration of no agent_heartbeat/agent_message before Layer A heartbeat-gap stall fires (fail-loud when unset)
    run_silence_stall: 20m
    # stall_sentinel.detection.review_finalize_stall: Go duration after reviewer_verdict before Layer A review-finalize stall fires when no run_completed follows (fail-loud when unset)
    review_finalize_stall: 20m
    # stall_sentinel.detection.run_max_age: Go duration backstop: any run older than this with no terminal event triggers Layer A run-age stall (fail-loud when unset)
    run_max_age: 4h
    # stall_sentinel.detection.lane_noprogress_stall: Go duration a lane may have an expectation of progress with zero forward-progress events before Layer B fires (fail-loud when unset)
    lane_noprogress_stall: 25m
`

// stallSentinelConfigExampleYAML returns the complete stall_sentinel: example block.
// Single source of truth for 'harmonik sentinel config --example'.
func stallSentinelConfigExampleYAML() string {
	return stallSentinelConfigExampleBlock
}

package main

import (
	"fmt"
	"io"
	"os"
)

// keeper_config_example.go — `harmonik keeper config --example` + the SINGLE
// source-of-truth keeper: config template.
//
// Operator-philosophy change: harmonik imposes NO built-in keeper defaults at
// RUNTIME (ResolveKeeperConfig refuses to start on any unset required value). The
// migration is one command: `harmonik keeper config --example` prints a COMPLETE,
// COMMENTED keeper: block with a sensible SUGGESTED starting value for every
// required key. The operator pastes it into .harmonik/config.yaml and OWNS/tunes
// the numbers from there.
//
// The suggested values are sourced from internal/keeper's Default* consts. That is
// ALLOWED here (and ONLY here + `harmonik init`): this is a template the operator
// copies and edits, NOT a runtime fallback. keeperConfigExampleBlock is the single
// source of truth shared by this command AND init_cmd.go so the two can never drift.
//
// LOAD-BEARING round-trip invariant (asserted in a test): the block this prints,
// pasted under schema_version: 1, MUST parse via daemon.LoadProjectConfig and
// resolve via ResolveKeeperConfig with ZERO missing-value errors. If you add a new
// required keeper value, you MUST add a line here or the round-trip test fails.

// keeperConfigExampleBlock is the complete, commented keeper: block — every
// operator-required key with a suggested starting value. It is a standalone YAML
// fragment (the `keeper:` mapping) so it can be embedded under schema_version: 1 in
// .harmonik/config.yaml. Indentation is two-space, matching the rest of config.yaml.
//
// Suggested values mirror the keeper.Default* consts (thresholds.go). These are a
// STARTING POINT the operator owns — NOT a runtime default.
const keeperConfigExampleBlock = `keeper:
  # context_thresholds — the warn/act/force token band + pct-of-window caps.
  # Token fields are plain integers. INVARIANT: warn < act < force_act.
  context_thresholds:
    warn_abs_tokens: 200000          # emit a wrap-up WARNING at this token count
    act_abs_tokens: 215000           # drive the handoff/clear ACT cycle here (> warn)
    force_act_abs_tokens: 240000     # unconditional forced clear (> act); or set force_act_abs_offset instead
    idle_floor_abs_tokens: 150000    # floor below which an idle crew is NOT idle-restarted
    warn_pct_ceil: 0.70              # pct-of-window cap for the warn gate; fraction in (0,1]
    act_pct_ceil: 0.85               # pct-of-window cap for the act gate; fraction in (0,1] (> warn_pct_ceil)
  # hard_ceiling — SID-independent backstop trip-wire above the normal band.
  hard_ceiling:
    mode: alarm                      # off | alarm | restart  (off → abs_tokens not required)
    abs_tokens: 280000               # backstop token trigger (must be > force_act in restart mode)
    cooldown: 5m                     # re-trigger cooldown (Go duration string)
  # timings — watcher/cycler cadence + waits. ALL durations are Go duration STRINGS.
  timings:
    poll_interval: 5s                # watcher gauge-poll cadence
    cycler_poll_interval: 200ms      # cycler nonce/settle poll cadence
    idle_quiesce: 8s                 # min gauge quiescence before the pane is "idle"
    staleness: 2m                    # gauge-staleness window before the gauge is treated as absent
    handoff_timeout: 3m              # cycler handoff-nonce wait
    clear_settle: 3s                 # post-/clear settle wait for a new session id
    boot_grace: 5m                   # young-session guard after a session_id change; 0s disables it
  # cadence — cooldowns / backoffs / recovery windows. ALL durations are strings.
  cadence:
    warn_cooldown: 30s               # cooldown between repeated warn firings
    no_gauge_backoff: 30s            # re-poll backoff after a no_gauge reading
    respawn_grace: 20s               # idle-respawn grace window
    respawn_cooldown: 90s            # idle-respawn cooldown
    live_recover_grace: 5m           # live-pane-recovery grace window
    live_recover_cooldown: 5m        # live-pane-recovery cooldown
    force_retry_interval: 2m         # forced-clear retry interval
    idle_restart_cooldown: 30m       # idle-restart cooldown
    hard_ceiling_cooldown: 5m        # hard-ceiling backstop cooldown
    blind_keeper_threshold: 5m       # blind-keeper alarm threshold
    hold_ttl: 45m                    # HOLD-marker backstop TTL (co-working override expiry)
    reap_decisions_cadence: 90s      # orphan-decision reaper scan interval (default 90s; 0 uses default)
  # budgets — integer counts.
  budgets:
    heartbeat_max_misses: 12         # watcher heartbeat miss budget
    max_handoff_timeouts: 3          # consecutive handoff-timeout escalation count (0 disables escalation)
  # self_service — OPTIONAL (not required to start). Crews self-restart by default.
  self_service:
    enabled: true                    # arm the actionable self-service restart-handshake warn text
    grace_seconds: 30                # grace before the self-service instruction repeats
    instruct_only_when_idle: false   # only inject the self-service instruction when idle
    crews_enabled: true              # crews self-restart by default (absent also resolves true)
`

// keeperConfigExampleYAML returns the complete keeper: example block. It is the
// single source of truth shared by `harmonik keeper config --example` and
// `harmonik init`'s generated config.yaml so the two cannot drift.
func keeperConfigExampleYAML() string {
	return keeperConfigExampleBlock
}

// runKeeperConfig implements `harmonik keeper config`. The only supported form is
// `harmonik keeper config --example`, which prints the complete starting keeper:
// block to stdout. Any other invocation prints usage to stderr and exits non-zero.
func runKeeperConfig(args []string) int {
	return runKeeperConfigTo(args, os.Stdout, os.Stderr)
}

// runKeeperConfigTo is the io-injectable core of runKeeperConfig (testable without
// touching the process stdio).
func runKeeperConfigTo(args []string, stdout, stderr io.Writer) int {
	example := false
	for _, a := range args {
		switch a {
		case "--example":
			example = true
		case "-h", "--help":
			fmt.Fprint(stderr, keeperConfigUsage)
			return 0
		default:
			fmt.Fprintf(stderr, "harmonik keeper config: unknown argument %q\n\n", a)
			fmt.Fprint(stderr, keeperConfigUsage)
			return 2
		}
	}
	if !example {
		fmt.Fprint(stderr, keeperConfigUsage)
		return 2
	}
	fmt.Fprint(stdout, keeperConfigExampleYAML())
	return 0
}

const keeperConfigUsage = `usage: harmonik keeper config --example

Prints a COMPLETE, commented keeper: config block to stdout. harmonik imposes no
built-in keeper defaults at runtime — every value must be set by the operator, or
the keeper refuses to start. Paste this block into <project>/.harmonik/config.yaml
(under schema_version: 1) and tune the numbers.

  harmonik keeper config --example >> .harmonik/config.yaml
`

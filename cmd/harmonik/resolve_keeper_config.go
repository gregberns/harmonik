package main

// resolve_keeper_config.go — the SINGLE keeper threshold/band precedence resolver
// (hk-4pnv).
//
// # OPERATOR-FACING CHOKEPOINT — imposes NO built-in defaults at runtime.
//
// ResolveKeeperConfig is the path `harmonik keeper` actually uses
// (runKeeperSubcommand → ResolveKeeperConfig → buildKeeperConfigs). Per the
// operator-philosophy decision, the PRODUCT must NOT apply any baked-in keeper
// number at runtime: EVERY keeper value must be set by the operator, via the
// keeper: block in .harmonik/config.yaml OR via an explicit CLI flag. When a
// required value is unset (no config value AND no flag) the resolver AGGREGATES
// all the missing keys and returns a single *KeeperConfigMissingError so the
// keeper REFUSES TO START — it never silently uses keeper.Default*.
//
// This is DISTINCT from internal/keeper's library-level applyDefaults
// (watcher.go / cycle.go), which is retained for programmatic construction and
// the unit-test suite. applyDefaults is the LIBRARY fallback; ResolveKeeperConfig
// is the OPERATOR-FACING enforcement gate. The keeper.Default* consts still exist,
// but here they are used ONLY as the suggested values printed by
// `harmonik keeper config --example` (a template the operator copies and OWNS),
// never as a runtime fallback.
//
// Why it lives in cmd/harmonik (NOT internal/keeper): depguard bans
// internal/keeper from importing internal/daemon (.golangci.yml:133, session-keeper
// spec hk-ekap1). The resolver needs daemon.KeeperConfig (the parsed
// .harmonik/config.yaml keeper: block), so it cannot live in internal/keeper.
//
// # Precedence (per field)
//
//	FLAG > CONFIG > (MISSING → refuse to start)
//
// A value is SATISFIED if the operator set it via config (the keeper: block) OR via
// an explicit CLI flag. Otherwise it is MISSING and reported in the aggregated
// error. There is NO silent DEFAULT layer at runtime any more.
//
// # Preserved semantics (exact, from the pre-hk-4pnv inline ladder)
//
//   - Tighten-only pct: an explicit --warn-pct / --act-pct may only move the band
//     EARLIER (lower ceil), never loosen it. The min(abs, pctCeil*window) gate
//     already enforces "earlier of the two fires"; the resolver additionally
//     REJECTS a pct flag that is looser than the resolved (config-or-default) ceil.
//   - Force-act precedence: an explicit force_act_abs_tokens WINS; the
//     force_act_abs_offset is used ONLY when the absolute is unset
//     (effective force = act + offset).
//   - Sentinels are NOT "unset means default":
//     boot_grace 0 = disabled; warn_cooldown NEGATIVE = disabled;
//     max_handoff_timeouts 0 = no-escalation. These pass through untouched.
//
// # Fail-loud (operator decision, OVERRIDES the bead's original revert-to-defaults
// text)
//
// Two fail-loud classes, BOTH refuse to start:
//   - *KeeperConfigMissingError — one or more required values are UNSET (no config,
//     no flag). All missing keys are aggregated into ONE error so the operator can
//     fix everything in a single pass; the message names the real yaml key paths and
//     points at `harmonik keeper config --example`.
//   - *KeeperConfigError — a bad/invalid VALUE that IS present: invalid enum,
//     pct>1, a cross-field invariant violation (warn < act < force_act < hard_ceiling,
//     warn_pct < act_pct), or a bad CLI flag.
//
// Missing-value errors take precedence: they are checked FIRST so the operator is not
// shown a confusing band-inversion error on top of "you didn't set the value". It
// NEVER silently defaults. The keeper start path logs the error to stderr (and an
// event when reachable) and refuses to start, so a misconfiguration is LEARNED.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/keeper"
)

// KeeperConfigError is the loud failure returned by ResolveKeeperConfig on any bad
// config value, bad CLI flag, or cross-field invariant violation. It carries a
// Field (the offending key, for the operator) and a human Reason. It is the
// fail-fast signal that the keeper start path surfaces (stderr + event) and then
// refuses to start on — matching the daemon block's posture. Refs: hk-4pnv.
type KeeperConfigError struct {
	// Field names the offending knob (e.g. "act_pct_ceil", "--warn-pct",
	// "warn<act<force_act<hard_ceiling").
	Field string
	// Reason is the human-readable explanation.
	Reason string
}

func (e *KeeperConfigError) Error() string {
	return fmt.Sprintf("keeper config: %s: %s", e.Field, e.Reason)
}

// KeeperConfigMissingError is returned by ResolveKeeperConfig when one or more
// REQUIRED keeper values are unset — neither in the keeper: block nor via a CLI
// flag. It aggregates EVERY missing key (not just the first) so the operator can
// fix them all in one pass, and its message names the real dotted yaml key paths
// plus the one-command migration (`harmonik keeper config --example`).
//
// It is a sibling of *KeeperConfigError (which is for a bad PRESENT value); the
// keeper start path surfaces both via the same stderr + event path and refuses to
// start. Refs: keeper operator-required-config change.
type KeeperConfigMissingError struct {
	// ProjectDir is the project root whose .harmonik/config.yaml needs the keys
	// (named in the fix instruction so the operator knows which file to edit).
	ProjectDir string
	// Missing is the dotted yaml key paths the operator must set, e.g.
	// "keeper.context_thresholds.warn_abs_tokens". Aggregated, never first-only.
	Missing []string
}

func (e *KeeperConfigMissingError) Error() string {
	dir := e.ProjectDir
	if dir == "" {
		dir = "<project>"
	}
	return fmt.Sprintf(
		"refusing to start — harmonik no longer applies built-in keeper defaults; "+
			"every value must be set by the operator. Missing %d value(s): %s. "+
			"Fix: run 'harmonik keeper config --example' to print a complete starting keeper: block, "+
			"add it to %s/.harmonik/config.yaml, then tune the numbers. "+
			"(Each value can alternatively be set via its CLI flag.)",
		len(e.Missing), strings.Join(e.Missing, ", "), dir)
}

// KeeperFlags are the parsed CLI flags the resolver consumes. Each *Set bool
// records whether the caller explicitly passed the flag, so the resolver can tell
// "caller passed 0" (an explicit choice) from "caller omitted the flag" (defer to
// config/default). This mirrors the fs.Visit() detection the inline ladder did.
type KeeperFlags struct {
	WarnAbsTokens int64
	WarnAbsSet    bool
	ActAbsTokens  int64
	ActAbsSet     bool
	WarnPct       int // raw --warn-pct (percent, 0..100); ceil = WarnPct/100
	WarnPctSet    bool
	ActPct        int // raw --act-pct (percent, 0..100); ceil = ActPct/100
	ActPctSet     bool

	// ── TIER-1 flags (hk-4gtu): a small set of high-traffic tunables get a CLI
	// flag (FLAG > CONFIG > DEFAULT). The long-tail cadence/budget knobs are
	// config-only — they are still THREADED from config, just not flag-settable.
	// Each *Set bool records explicit presence so a 0 flag can override a config
	// value when intended. ──
	Staleness          time.Duration
	StalenessSet       bool
	IdleQuiesce        time.Duration
	IdleQuiesceSet     bool
	PollInterval       time.Duration
	PollIntervalSet    bool
	HandoffTimeout     time.Duration
	HandoffTimeoutSet  bool
	BootGrace          time.Duration // 0-disabled sentinel honored when BootGraceSet
	BootGraceSet       bool
	IdleFloorAbsTokens int64
	IdleFloorSet       bool
	HardCeilingAbs     int64
	HardCeilingAbsSet  bool
	HardCeilingMode    string // raw token off|alarm|restart; "" = defer to config/default
	HardCeilingModeSet bool
}

// ResolvedKeeperConfig is the NEUTRAL resolved keeper band/threshold config the
// start path translates into Watcher/Cycler config. Every field is the EFFECTIVE
// post-precedence value (abs tokens are pre-window: the pctCeil cap is applied at
// runtime once the gauge reports a window, identically to today). Defaults are
// already filled — no field is left at a defer-to-default zero (except the
// sentinels, which carry their special meaning).
type ResolvedKeeperConfig struct {
	WarnAbsTokens     int64
	ActAbsTokens      int64
	ForceActAbsTokens int64
	WarnPctCeil       float64
	ActPctCeil        float64

	// Hard ceiling (SID-independent backstop). Mode "" = compiled default.
	HardCeilingAbsTokens int64
	// HardCeilingMode is the resolved backstop mode (CONFIG > DEFAULT=Alarm). The
	// zero value (HardCeilingModeAlarm) IS the compiled default, so an unset config
	// resolves to Alarm. Validated fail-loud (∈ off|alarm|restart) belt-and-suspenders
	// over the daemon parse layer. Refs: hk-4gtu, hk-n6kn.
	HardCeilingMode keeper.HardCeilingMode

	// ── Watcher timing / cadence / budget (hk-4gtu) ──────────────────────────
	// Every field is the EFFECTIVE post-precedence value (FLAG>CONFIG>DEFAULT for
	// the tier-1 ones, CONFIG>DEFAULT for the long tail), read from the keeper
	// Default* consts so defaults live in ONE place. The start path copies each
	// into the WatcherConfig literal so a config value DEMONSTRABLY reaches keeper
	// behaviour (R2: parse-but-drop is a violation).
	PollInterval         time.Duration
	IdleQuiesce          time.Duration
	Staleness            time.Duration
	RespawnGrace         time.Duration
	RespawnCooldown      time.Duration
	LiveRecoverGrace     time.Duration
	LiveRecoverCooldown  time.Duration
	NoGaugeBackoff       time.Duration
	HardCeilingCooldown  time.Duration
	BlindKeeperThreshold time.Duration
	HoldTTL              time.Duration
	HeartbeatMaxMisses   int
	// ReapDecisionsCadence is the orphan-reaper scan interval (CONFIG>DEFAULT;
	// optional — zero resolves to DefaultReapDecisionsCadence via applyDefaults).
	// Refs: hk-jrftk.
	ReapDecisionsCadence time.Duration

	// ── Cycler timing / cadence / budget (hk-4gtu) ───────────────────────────
	HandoffTimeout       time.Duration
	ClearSettle          time.Duration
	CyclerPollInterval   time.Duration
	ForceRetryInterval   time.Duration
	IdleRestartAbsTokens int64
	IdleRestartCooldown  time.Duration

	// ── Sentinel-bearing pass-throughs (hk-4gtu) ─────────────────────────────
	// These carry special meaning the start path honors END-TO-END; the resolver
	// forwards the CONFIGURED value verbatim and does NOT apply a non-sentinel
	// default (so the zero/negative is preserved into the construction literal):
	//   BootGrace          0  = disabled (young-session guard off)
	//   WarnCooldown    < 0   = disabled (warn-firing cooldown off)
	//   MaxHandoffTimeouts 0  = no-escalation
	// BootGrace's non-sentinel production value (DefaultBootGracePeriod) is fed at
	// the construction site when unconfigured — NOT via applyDefaults — preserving
	// the opt-in-per-construction-site contract (thresholds.go DefaultBootGracePeriod).
	BootGrace          time.Duration
	BootGraceSet       bool // true when CONFIG or FLAG set boot_grace (honor a 0 = disabled)
	WarnCooldown       time.Duration
	MaxHandoffTimeouts int

	// ── self_service (hk-vs4u) ───────────────────────────────────────────────
	// Threaded CONFIG-only (no flags). These reach WatcherConfig so watcher.go can
	// select the actionable self-service restart-handshake warn text vs the lighter
	// advisory (selectWarnText).
	//
	// SelfServiceCrewsEnabled is resolved UNSET→TRUE: the raw config carries a *bool
	// (nil when keeper.self_service.crews_enabled is absent), and the operator
	// decision (hk-vs4u) is that crews self-restart by default. An absent key
	// resolves to true here; an explicit `crews_enabled: false` resolves to false.
	SelfServiceEnabled              bool
	SelfServiceGraceSeconds         int
	SelfServiceInstructOnlyWhenIdle bool
	SelfServiceCrewsEnabled         bool

	// Warn-text overrides (CONFIG-only; empty = compiled default). DefaultWarnText is
	// the lighter advisory for non-captain agents; ActionableWarnText is the R3
	// self-service restart-handshake override (the deprecated on_demand_warn_text is
	// aliased onto it in projectconfig.go). Refs: hk-vs4u.
	DefaultWarnText    string
	ActionableWarnText string
}

// requiredKeeperValue describes one operator-required keeper value: its dotted
// yaml key path (for the missing-value error) and whether the operator supplied it
// (config present OR flag set). Used by checkMissingKeeperValues to aggregate every
// unset value into a single *KeeperConfigMissingError.
type requiredKeeperValue struct {
	keyPath   string
	satisfied bool
}

// checkMissingKeeperValues returns the dotted yaml key paths of every required
// keeper value the operator did NOT set (no config value AND no CLI flag), in a
// stable order. Empty result = all required values supplied. See the file header:
// the keeper imposes NO built-in defaults, so an unset required value refuses to
// start.
//
// SPECIAL CASES (legitimate explicit values that are SATISFIED, not missing):
//   - hard_ceiling.mode == "off" → an explicit choice; hard_ceiling.abs_tokens is
//     then NOT required (an off ceiling never trips, so the trigger is moot).
//   - boot_grace presence is honored even for "0s" (explicit "disable boot grace");
//     daemon.KeeperConfig.Present.BootGrace is true for any non-empty string.
func checkMissingKeeperValues(flags KeeperFlags, cfg daemon.KeeperConfig) []string {
	p := cfg.Present

	// hard_ceiling.mode resolves to "off" only via an EXPLICIT config value (no flag
	// default). When the operator sets mode: off, abs_tokens is not required.
	modeIsOff := (flags.HardCeilingModeSet && flags.HardCeilingMode == "off") ||
		(!flags.HardCeilingModeSet && p.HardCeilingMode && cfg.HardCeilingMode == "off")

	req := []requiredKeeperValue{
		// ── thresholds ──
		{"keeper.context_thresholds.warn_abs_tokens", flags.WarnAbsSet || p.WarnAbsTokens},
		{"keeper.context_thresholds.act_abs_tokens", flags.ActAbsSet || p.ActAbsTokens},
		// force_act: satisfied by either the absolute OR the offset (force = act + offset).
		{"keeper.context_thresholds.force_act_abs_tokens (or force_act_abs_offset)", p.ForceActAbsTokens || p.ForceActAbsOffset},
		{"keeper.context_thresholds.warn_pct_ceil", flags.WarnPctSet || p.WarnPctCeil},
		{"keeper.context_thresholds.act_pct_ceil", flags.ActPctSet || p.ActPctCeil},
		{"keeper.context_thresholds.idle_floor_abs_tokens", flags.IdleFloorSet || p.IdleFloorAbsTokens},
		// ── hard_ceiling ──
		{"keeper.hard_ceiling.mode", flags.HardCeilingModeSet || p.HardCeilingMode},
		// abs_tokens required UNLESS mode is explicitly off.
		{"keeper.hard_ceiling.abs_tokens", modeIsOff || flags.HardCeilingAbsSet || p.HardCeilingAbsTokens},
		// ── timings ──
		{"keeper.timings.poll_interval", flags.PollIntervalSet || p.PollInterval},
		{"keeper.timings.cycler_poll_interval", p.CyclerPollInterval},
		{"keeper.timings.idle_quiesce", flags.IdleQuiesceSet || p.IdleQuiesce},
		{"keeper.timings.staleness", flags.StalenessSet || p.Staleness},
		{"keeper.timings.handoff_timeout", flags.HandoffTimeoutSet || p.HandoffTimeout},
		{"keeper.timings.clear_settle", p.ClearSettle},
		{"keeper.timings.boot_grace", flags.BootGraceSet || p.BootGrace},
		// ── cadence ──
		{"keeper.cadence.warn_cooldown", p.WarnCooldown},
		{"keeper.cadence.no_gauge_backoff", p.NoGaugeBackoff},
		{"keeper.cadence.respawn_grace", p.RespawnGrace},
		{"keeper.cadence.respawn_cooldown", p.RespawnCooldown},
		{"keeper.cadence.live_recover_grace", p.LiveRecoverGrace},
		{"keeper.cadence.live_recover_cooldown", p.LiveRecoverCooldown},
		{"keeper.cadence.force_retry_interval", p.ForceRetryInterval},
		{"keeper.cadence.idle_restart_cooldown", p.IdleRestartCooldown},
		{"keeper.cadence.hard_ceiling_cooldown", p.HardCeilingCooldown},
		{"keeper.cadence.blind_keeper_threshold", p.BlindKeeperThreshold},
		{"keeper.cadence.hold_ttl", p.HoldTTL},
		// ── budgets ──
		{"keeper.budgets.heartbeat_max_misses", p.HeartbeatMaxMisses},
		{"keeper.budgets.max_handoff_timeouts", p.MaxHandoffTimeouts},
	}

	var missing []string
	for _, r := range req {
		if !r.satisfied {
			missing = append(missing, r.keyPath)
		}
	}
	return missing
}

// ResolveKeeperConfig implements FLAG > CONFIG (no runtime DEFAULT layer) per field
// and validates the cross-field band invariants. It is the OPERATOR-FACING chokepoint
// (see file header): an unset required value aggregates into a *KeeperConfigMissingError
// (refuse to start), and a bad PRESENT value returns a *KeeperConfigError — NEVER a
// silent default. projectDir names the file to fix in the missing-value message.
func ResolveKeeperConfig(flags KeeperFlags, cfg daemon.KeeperConfig, projectDir string) (ResolvedKeeperConfig, error) {
	// ── Missing-value gate (checked FIRST, precedence over cross-field errors). ──
	// Every required value must be set by the operator (config or flag); harmonik
	// imposes NO built-in default at runtime. Aggregate ALL missing keys.
	if missing := checkMissingKeeperValues(flags, cfg); len(missing) > 0 {
		return ResolvedKeeperConfig{}, &KeeperConfigMissingError{
			ProjectDir: projectDir,
			Missing:    missing,
		}
	}

	var out ResolvedKeeperConfig

	// ── warn-abs / act-abs: FLAG > CONFIG > DEFAULT ──
	out.WarnAbsTokens = resolveInt64(
		flags.WarnAbsTokens, flags.WarnAbsSet,
		cfg.WarnAbsTokens,
		keeper.DefaultWarnAbsTokens)
	out.ActAbsTokens = resolveInt64(
		flags.ActAbsTokens, flags.ActAbsSet,
		cfg.ActAbsTokens,
		keeper.DefaultActAbsTokens)

	// ── force-act precedence: explicit ABS wins; OFFSET used only when abs unset ──
	// No CLI flag for force-act. CONFIG abs > (act + CONFIG-or-DEFAULT offset).
	switch {
	case cfg.ForceActAbsTokens > 0:
		out.ForceActAbsTokens = cfg.ForceActAbsTokens
	default:
		offset := keeper.DefaultForceActAbsOffset
		if cfg.ForceActAbsOffset > 0 {
			offset = cfg.ForceActAbsOffset
		}
		out.ForceActAbsTokens = out.ActAbsTokens + offset
	}

	// ── pct ceils: FLAG (tighten-only) > CONFIG > DEFAULT ──
	// First resolve CONFIG-or-DEFAULT, then a flag may only TIGHTEN (lower) it.
	out.WarnPctCeil = resolveFloat(cfg.WarnPctCeil, keeper.DefaultWarnPctCeil)
	out.ActPctCeil = resolveFloat(cfg.ActPctCeil, keeper.DefaultActPctCeil)

	if flags.WarnPctSet {
		ceil, err := pctFlagCeil("--warn-pct", flags.WarnPct)
		if err != nil {
			return ResolvedKeeperConfig{}, err
		}
		// Tighten-only: a flag may move warn EARLIER (lower ceil), never loosen it.
		if ceil > out.WarnPctCeil {
			return ResolvedKeeperConfig{}, &KeeperConfigError{
				Field:  "--warn-pct",
				Reason: fmt.Sprintf("tighten-only: %.2f is looser (higher) than the resolved warn ceil %.2f; a pct flag may only move the band earlier", ceil, out.WarnPctCeil),
			}
		}
		out.WarnPctCeil = ceil
	}
	if flags.ActPctSet {
		ceil, err := pctFlagCeil("--act-pct", flags.ActPct)
		if err != nil {
			return ResolvedKeeperConfig{}, err
		}
		if ceil > out.ActPctCeil {
			return ResolvedKeeperConfig{}, &KeeperConfigError{
				Field:  "--act-pct",
				Reason: fmt.Sprintf("tighten-only: %.2f is looser (higher) than the resolved act ceil %.2f; a pct flag may only move the band earlier", ceil, out.ActPctCeil),
			}
		}
		out.ActPctCeil = ceil
	}

	// ── hard ceiling abs (FLAG > CONFIG > DEFAULT). The SID-independent backstop. ──
	out.HardCeilingAbsTokens = resolveInt64(
		flags.HardCeilingAbs, flags.HardCeilingAbsSet,
		cfg.HardCeilingAbsTokens,
		keeper.HardCeilingAbsTokens)

	// ── hard ceiling MODE (FLAG > CONFIG > DEFAULT=Alarm). Validate fail-loud ──
	// (belt-and-suspenders over the daemon parse layer, which already rejects an
	// unknown mode string). The zero value HardCeilingModeAlarm IS the compiled
	// default, so an absent mode resolves to Alarm. Refs: hk-4gtu, hk-n6kn.
	modeStr := ""
	switch {
	case flags.HardCeilingModeSet:
		modeStr = flags.HardCeilingMode
	case cfg.HardCeilingMode != "":
		modeStr = cfg.HardCeilingMode
	}
	if modeStr != "" {
		switch modeStr {
		case "off", "alarm", "restart":
			out.HardCeilingMode = keeper.ParseHardCeilingMode(modeStr)
		default:
			return ResolvedKeeperConfig{}, &KeeperConfigError{
				Field:  "hard_ceiling.mode",
				Reason: fmt.Sprintf("%q is not a valid mode; must be one of off, alarm, restart", modeStr),
			}
		}
	} else {
		// Unreachable in the operator-facing path (hard_ceiling.mode is REQUIRED, so
		// the missing-value gate already refused). Kept as a defensive belt for any
		// programmatic caller; it is NOT a runtime product default.
		out.HardCeilingMode = keeper.HardCeilingModeAlarm
	}

	// ── Watcher timing/cadence/budget. Every value is operator-supplied (the ──
	// missing-value gate above guaranteed it), so these resolve to the config (or
	// flag) value; the keeper.Default* args are unreachable belts, NOT runtime
	// defaults. Every configured value reaches the constructed Watcher/Cycler.
	out.PollInterval = resolveDur(
		flags.PollInterval, flags.PollIntervalSet, cfg.PollInterval, keeper.DefaultPollInterval)
	out.IdleQuiesce = resolveDur(
		flags.IdleQuiesce, flags.IdleQuiesceSet, cfg.IdleQuiesce, keeper.DefaultIdleQuiesce)
	out.Staleness = resolveDur(
		flags.Staleness, flags.StalenessSet, cfg.Staleness, keeper.DefaultStaleness)
	out.RespawnGrace = resolveDur(0, false, cfg.RespawnGrace, keeper.DefaultRespawnGrace)
	out.RespawnCooldown = resolveDur(0, false, cfg.RespawnCooldown, keeper.DefaultRespawnCooldown)
	out.LiveRecoverGrace = resolveDur(0, false, cfg.LiveRecoverGrace, keeper.DefaultLiveRecoverGrace)
	out.LiveRecoverCooldown = resolveDur(0, false, cfg.LiveRecoverCooldown, keeper.DefaultLiveRecoverCooldown)
	out.NoGaugeBackoff = resolveDur(0, false, cfg.NoGaugeBackoff, keeper.DefaultNoGaugeBackoff)
	out.HoldTTL = resolveDur(0, false, cfg.HoldTTL, keeper.DefaultHoldTTL)
	out.HardCeilingCooldown = resolveDur(0, false, cfg.CadenceHardCeilingCooldown, keeper.DefaultHardCeilingCooldown)
	out.BlindKeeperThreshold = resolveDur(0, false, cfg.BlindKeeperThreshold, keeper.DefaultBlindKeeperThreshold)
	out.HeartbeatMaxMisses = resolveInt(0, false, cfg.HeartbeatMaxMisses, keeper.DefaultMaxHeartbeatMisses)
	// ReapDecisionsCadence: CONFIG > DEFAULT (not required; applyDefaults fills
	// DefaultReapDecisionsCadence when the resolved value is zero). Refs: hk-jrftk.
	out.ReapDecisionsCadence = resolveDur(0, false, cfg.ReapDecisionsCadence, keeper.DefaultReapDecisionsCadence)

	// ── Cycler timing/cadence/budget ──
	out.HandoffTimeout = resolveDur(
		flags.HandoffTimeout, flags.HandoffTimeoutSet, cfg.HandoffTimeout, keeper.DefaultHandoffTimeout)
	out.ClearSettle = resolveDur(0, false, cfg.ClearSettle, keeper.DefaultClearSettle)
	out.CyclerPollInterval = resolveDur(0, false, cfg.CyclerPollInterval, keeper.DefaultCyclerPollInterval)
	out.ForceRetryInterval = resolveDur(0, false, cfg.ForceRetryInterval, keeper.DefaultForceRetryInterval)
	out.IdleRestartAbsTokens = resolveInt64(
		flags.IdleFloorAbsTokens, flags.IdleFloorSet, cfg.IdleFloorAbsTokens, keeper.DefaultIdleRestartAbsTokens)
	out.IdleRestartCooldown = resolveDur(0, false, cfg.IdleRestartCooldown, keeper.DefaultIdleRestartCooldown)

	// ── Sentinel-bearing pass-throughs: forward the CONFIGURED value verbatim. ──
	// BootGrace: FLAG > CONFIG; 0 = disabled is HONORED when explicitly set. Presence
	// is the raw-string signal (cfg.Present.BootGrace is true even for "0s"), so an
	// explicit `boot_grace: 0s` resolves as SET+0 (disabled), not "unset". boot_grace
	// is operator-required (the missing-value gate guarantees one of flag/config is
	// present), so the default branch is unreachable in the operator path.
	switch {
	case flags.BootGraceSet:
		out.BootGrace = flags.BootGrace
		out.BootGraceSet = true
	case cfg.Present.BootGrace:
		out.BootGrace = cfg.BootGrace
		out.BootGraceSet = true
	default:
		out.BootGrace = 0
		out.BootGraceSet = false
	}
	// WarnCooldown: a NEGATIVE config value (disable sentinel) and 0 (zero sentinel
	// → applyDefaults fills 30s) both pass through untouched. The watcher's
	// applyDefaults owns the sentinel translation; we only forward the configured
	// value (0 when unconfigured → default applies downstream).
	out.WarnCooldown = cfg.WarnCooldown
	// MaxHandoffTimeouts: 0 = no-escalation sentinel; forward verbatim (0 when
	// unconfigured → cycler applyDefaults fills DefaultMaxHandoffTimeouts).
	out.MaxHandoffTimeouts = cfg.MaxHandoffTimeouts

	// ── self_service (hk-vs4u): CONFIG-only thread-through ──
	// crews_enabled resolves UNSET→TRUE (operator decision: crews self-restart). The
	// raw config carries a *bool: nil (absent) → true; non-nil → the explicit value.
	out.SelfServiceEnabled = cfg.SelfServiceEnabled
	out.SelfServiceGraceSeconds = cfg.SelfServiceGraceSeconds
	out.SelfServiceInstructOnlyWhenIdle = cfg.SelfServiceInstructOnlyWhenIdle
	if cfg.SelfServiceCrewsEnabled == nil {
		out.SelfServiceCrewsEnabled = true
	} else {
		out.SelfServiceCrewsEnabled = *cfg.SelfServiceCrewsEnabled
	}
	out.DefaultWarnText = cfg.DefaultWarnText
	out.ActionableWarnText = cfg.ActionableWarnText

	// ── cross-field invariants (fail-loud — NEVER revert to defaults) ──
	// Band ordering: warn < act < force_act < hard_ceiling.
	if !(out.WarnAbsTokens < out.ActAbsTokens) {
		return ResolvedKeeperConfig{}, &KeeperConfigError{
			Field:  "warn<act",
			Reason: fmt.Sprintf("band inversion: warn_abs_tokens (%d) must be < act_abs_tokens (%d)", out.WarnAbsTokens, out.ActAbsTokens),
		}
	}
	if !(out.ActAbsTokens < out.ForceActAbsTokens) {
		return ResolvedKeeperConfig{}, &KeeperConfigError{
			Field:  "act<force_act",
			Reason: fmt.Sprintf("band inversion: act_abs_tokens (%d) must be < force_act_abs_tokens (%d)", out.ActAbsTokens, out.ForceActAbsTokens),
		}
	}
	// Hard-ceiling band checks apply ONLY when the ceiling is armed (mode != off). When
	// mode is explicitly off the ceiling never trips and abs_tokens is not required (may
	// be 0), so the band ordering against force_act is moot — skip it (otherwise a
	// legitimate `mode: off` with no abs_tokens would spuriously fail force_act < 0).
	if out.HardCeilingMode != keeper.HardCeilingModeOff {
		// Hard-ceiling restart sanity (hk-z8d0): a RESTART-mode ceiling at or below the
		// effective force_act threshold is nonsensical — force_act already restarts via
		// the act/force cycle there, so a restart ceiling could never fire later than
		// force_act and would only double-fire. Reject fail-loud (operator rule) with a
		// restart-mode-specific, clearly-named field. Checked BEFORE the generic
		// force_act<hard_ceiling band invariant below so restart-mode operators get the
		// intent-naming error rather than the generic band message; for non-restart
		// modes the generic band check still applies.
		if out.HardCeilingMode == keeper.HardCeilingModeRestart &&
			out.HardCeilingAbsTokens <= out.ForceActAbsTokens {
			return ResolvedKeeperConfig{}, &KeeperConfigError{
				Field:  "hard_ceiling.abs_tokens",
				Reason: fmt.Sprintf("restart-mode hard ceiling (%d) must be > force_act_abs_tokens (%d): a restart ceiling at/below force_act is nonsensical (force_act already restarts via the cycle there)", out.HardCeilingAbsTokens, out.ForceActAbsTokens),
			}
		}
		if !(out.ForceActAbsTokens < out.HardCeilingAbsTokens) {
			return ResolvedKeeperConfig{}, &KeeperConfigError{
				Field:  "force_act<hard_ceiling",
				Reason: fmt.Sprintf("band inversion: force_act_abs_tokens (%d) must be < hard_ceiling_abs_tokens (%d)", out.ForceActAbsTokens, out.HardCeilingAbsTokens),
			}
		}
	}
	// pct ordering: warn_pct < act_pct.
	if !(out.WarnPctCeil < out.ActPctCeil) {
		return ResolvedKeeperConfig{}, &KeeperConfigError{
			Field:  "warn_pct<act_pct",
			Reason: fmt.Sprintf("band inversion: warn_pct_ceil (%.2f) must be < act_pct_ceil (%.2f)", out.WarnPctCeil, out.ActPctCeil),
		}
	}

	return out, nil
}

// resolveInt64 applies FLAG > CONFIG > DEFAULT for an int64 token field. flagSet
// distinguishes an explicit flag value (which WINS even if 0) from an omitted flag
// (defer to config/default). A config value ≤ 0 is treated as not configured.
func resolveInt64(flagVal int64, flagSet bool, cfgVal, def int64) int64 {
	if flagSet {
		return flagVal
	}
	if cfgVal > 0 {
		return cfgVal
	}
	return def
}

// resolveFloat applies CONFIG > DEFAULT for a pct field (≤ 0 = not configured).
// The flag layer is handled separately because pct flags are tighten-only.
func resolveFloat(cfgVal, def float64) float64 {
	if cfgVal > 0 {
		return cfgVal
	}
	return def
}

// resolveDur applies FLAG > CONFIG > DEFAULT for a non-sentinel duration field.
// flagSet distinguishes an explicit flag (which WINS even if 0) from an omitted
// one. A config value ≤ 0 is treated as not configured. NOT for sentinel-bearing
// durations (boot_grace, warn_cooldown) — those are forwarded verbatim. hk-4gtu.
func resolveDur(flagVal time.Duration, flagSet bool, cfgVal, def time.Duration) time.Duration {
	if flagSet {
		return flagVal
	}
	if cfgVal > 0 {
		return cfgVal
	}
	return def
}

// resolveInt applies FLAG > CONFIG > DEFAULT for a non-sentinel int budget field
// (≤ 0 = not configured). NOT for the MaxHandoffTimeouts 0-no-escalation sentinel.
// hk-4gtu.
func resolveInt(flagVal int, flagSet bool, cfgVal, def int) int {
	if flagSet {
		return flagVal
	}
	if cfgVal > 0 {
		return cfgVal
	}
	return def
}

// pctFlagCeil converts a raw --warn-pct/--act-pct percent (0..100) into a ceil
// fraction (0..1], failing loud on an out-of-range value. A pct flag that resolves
// to a ceil > 1 (or ≤ 0) is a bad flag → *KeeperConfigError (NEVER silently
// clamped/defaulted).
func pctFlagCeil(name string, pct int) (float64, error) {
	if pct <= 0 || pct > 100 {
		return 0, &KeeperConfigError{
			Field:  name,
			Reason: fmt.Sprintf("%d is out of range; a context-use percentage must be in (0, 100]", pct),
		}
	}
	return float64(pct) / 100.0, nil
}

// emitKeeperConfigRejected appends a durable session_keeper_config_rejected event
// to the project's keeper events.jsonl so a fail-loud refusal-to-start is not
// stderr-only (hk-4pnv). Best-effort: the FileEmitter logs its own write failures
// via slog, so any emit error here is intentionally ignored. The Field is carried
// through when err is a *KeeperConfigError so the operator sees which knob was bad.
func emitKeeperConfigRejected(projectDir, agentName string, err error) {
	field := "config"
	var kce *KeeperConfigError
	var kme *KeeperConfigMissingError
	switch {
	case errors.As(err, &kce):
		field = kce.Field
	case errors.As(err, &kme):
		// Missing-value class: there is no single offending field; record the count of
		// unset required keys so the event is still actionable.
		field = fmt.Sprintf("missing-required(%d)", len(kme.Missing))
	}
	payload, marshalErr := json.Marshal(core.SessionKeeperConfigRejectedPayload{
		AgentName: agentName,
		Field:     field,
		Reason:    err.Error(),
	})
	if marshalErr != nil {
		return
	}
	emitter := keeper.NewFileEmitter(projectDir)
	_ = emitter.EmitWithRunID(context.Background(), core.RunID{}, "session_keeper_config_rejected", payload)
}

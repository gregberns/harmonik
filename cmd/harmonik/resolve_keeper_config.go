package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/keeper"
	"github.com/gregberns/harmonik/internal/projectconfig"
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
	// OperatorTurnLookback is the max age of an inbound operator user turn that
	// auto-engages a hold (Gate 5d). Zero disables the gate. Refs: hk-74iyd.
	OperatorTurnLookback time.Duration
	// PostAnswerGrace is the min duration after the agent's last real text response
	// before ACT may fire (Gate 5e). Zero disables the gate. Refs: hk-74iyd.
	PostAnswerGrace time.Duration

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
	SettleWarnText     string
	// LeaderDeferText / CrewDeferText are the K2 leader defer-message and K7
	// crew keeper-message body overrides (empty = compiled default / off).
	// CONFIG-only, carried verbatim to WatcherConfig; consumption is T3+. Refs:
	// SK-032; park-resume-protocol §9 (K7 crew hook default-off).
	LeaderDeferText string
	CrewDeferText   string
}

type requiredKeeperValue struct {
	keyPath   string
	satisfied bool
}

func checkMissingKeeperValues(flags KeeperFlags, cfg projectconfig.KeeperConfig) []string { //nolint:cyclop // checkMissingKeeperValues is at/over the threshold after branch edits; splitting mid-release is riskier than the marginal complexity
	p := cfg.Present

	modeIsOff := (flags.HardCeilingModeSet && flags.HardCeilingMode == "off") ||
		(!flags.HardCeilingModeSet && p.HardCeilingMode && cfg.HardCeilingMode == "off")

	req := []requiredKeeperValue{
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
		{"keeper.cadence.operator_turn_lookback", p.OperatorTurnLookback},
		{"keeper.cadence.post_answer_grace", p.PostAnswerGrace},
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
func ResolveKeeperConfig(flags KeeperFlags, cfg projectconfig.KeeperConfig, projectDir string) (ResolvedKeeperConfig, error) { //nolint:gocognit,cyclop,funlen // ResolveKeeperConfig is at/over the threshold after branch edits; splitting mid-release is riskier than the marginal complexity
	if missing := checkMissingKeeperValues(flags, cfg); len(missing) > 0 {
		return ResolvedKeeperConfig{}, &KeeperConfigMissingError{
			ProjectDir: projectDir,
			Missing:    missing,
		}
	}

	var out ResolvedKeeperConfig

	out.WarnAbsTokens = resolveInt64(
		flags.WarnAbsTokens, flags.WarnAbsSet,
		cfg.WarnAbsTokens,
		keeper.DefaultWarnAbsTokens)
	out.ActAbsTokens = resolveInt64(
		flags.ActAbsTokens, flags.ActAbsSet,
		cfg.ActAbsTokens,
		keeper.DefaultActAbsTokens)

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

	out.WarnPctCeil = resolveFloat(cfg.WarnPctCeil, keeper.DefaultWarnPctCeil)
	out.ActPctCeil = resolveFloat(cfg.ActPctCeil, keeper.DefaultActPctCeil)

	if flags.WarnPctSet {
		ceil, err := pctFlagCeil("--warn-pct", flags.WarnPct)
		if err != nil {
			return ResolvedKeeperConfig{}, err
		}
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

	out.HardCeilingAbsTokens = resolveInt64(
		flags.HardCeilingAbs, flags.HardCeilingAbsSet,
		cfg.HardCeilingAbsTokens,
		keeper.HardCeilingAbsTokens)

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
		out.HardCeilingMode = keeper.HardCeilingModeAlarm
	}

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
	out.OperatorTurnLookback = resolveDur(0, false, cfg.OperatorTurnLookback, 0)
	out.PostAnswerGrace = resolveDur(0, false, cfg.PostAnswerGrace, 0)
	out.BlindKeeperThreshold = resolveDur(0, false, cfg.BlindKeeperThreshold, keeper.DefaultBlindKeeperThreshold)
	out.HeartbeatMaxMisses = resolveInt(0, false, cfg.HeartbeatMaxMisses, keeper.DefaultMaxHeartbeatMisses)
	out.ReapDecisionsCadence = resolveDur(0, false, cfg.ReapDecisionsCadence, keeper.DefaultReapDecisionsCadence)

	out.HandoffTimeout = resolveDur(
		flags.HandoffTimeout, flags.HandoffTimeoutSet, cfg.HandoffTimeout, keeper.DefaultHandoffTimeout)
	out.ClearSettle = resolveDur(0, false, cfg.ClearSettle, keeper.DefaultClearSettle)
	out.CyclerPollInterval = resolveDur(0, false, cfg.CyclerPollInterval, keeper.DefaultCyclerPollInterval)
	out.ForceRetryInterval = resolveDur(0, false, cfg.ForceRetryInterval, keeper.DefaultForceRetryInterval)
	out.IdleRestartAbsTokens = resolveInt64(
		flags.IdleFloorAbsTokens, flags.IdleFloorSet, cfg.IdleFloorAbsTokens, keeper.DefaultIdleRestartAbsTokens)
	out.IdleRestartCooldown = resolveDur(0, false, cfg.IdleRestartCooldown, keeper.DefaultIdleRestartCooldown)

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
	out.WarnCooldown = cfg.WarnCooldown
	out.MaxHandoffTimeouts = cfg.MaxHandoffTimeouts

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
	out.SettleWarnText = cfg.SettleWarnText
	out.LeaderDeferText = cfg.LeaderDeferText
	out.CrewDeferText = cfg.CrewDeferText

	if out.WarnAbsTokens >= out.ActAbsTokens {
		return ResolvedKeeperConfig{}, &KeeperConfigError{
			Field:  "warn<act",
			Reason: fmt.Sprintf("band inversion: warn_abs_tokens (%d) must be < act_abs_tokens (%d)", out.WarnAbsTokens, out.ActAbsTokens),
		}
	}
	if out.ActAbsTokens >= out.ForceActAbsTokens {
		return ResolvedKeeperConfig{}, &KeeperConfigError{
			Field:  "act<force_act",
			Reason: fmt.Sprintf("band inversion: act_abs_tokens (%d) must be < force_act_abs_tokens (%d)", out.ActAbsTokens, out.ForceActAbsTokens),
		}
	}
	if out.HardCeilingMode != keeper.HardCeilingModeOff {
		if out.HardCeilingMode == keeper.HardCeilingModeRestart &&
			out.HardCeilingAbsTokens <= out.ForceActAbsTokens {
			return ResolvedKeeperConfig{}, &KeeperConfigError{
				Field:  "hard_ceiling.abs_tokens",
				Reason: fmt.Sprintf("restart-mode hard ceiling (%d) must be > force_act_abs_tokens (%d): a restart ceiling at/below force_act is nonsensical (force_act already restarts via the cycle there)", out.HardCeilingAbsTokens, out.ForceActAbsTokens),
			}
		}
		if out.ForceActAbsTokens >= out.HardCeilingAbsTokens {
			return ResolvedKeeperConfig{}, &KeeperConfigError{
				Field:  "force_act<hard_ceiling",
				Reason: fmt.Sprintf("band inversion: force_act_abs_tokens (%d) must be < hard_ceiling_abs_tokens (%d)", out.ForceActAbsTokens, out.HardCeilingAbsTokens),
			}
		}
	}
	if !(out.WarnPctCeil < out.ActPctCeil) {
		return ResolvedKeeperConfig{}, &KeeperConfigError{
			Field:  "warn_pct<act_pct",
			Reason: fmt.Sprintf("band inversion: warn_pct_ceil (%.2f) must be < act_pct_ceil (%.2f)", out.WarnPctCeil, out.ActPctCeil),
		}
	}

	return out, nil
}

func resolveInt64(flagVal int64, flagSet bool, cfgVal, def int64) int64 {
	if flagSet {
		return flagVal
	}
	if cfgVal > 0 {
		return cfgVal
	}
	return def
}

func resolveFloat(cfgVal, def float64) float64 {
	if cfgVal > 0 {
		return cfgVal
	}
	return def
}

func resolveDur(flagVal time.Duration, flagSet bool, cfgVal, def time.Duration) time.Duration {
	if flagSet {
		return flagVal
	}
	if cfgVal > 0 {
		return cfgVal
	}
	return def
}

func resolveInt(flagVal int, flagSet bool, cfgVal, def int) int {
	if flagSet {
		return flagVal
	}
	if cfgVal > 0 {
		return cfgVal
	}
	return def
}

func pctFlagCeil(name string, pct int) (float64, error) {
	if pct <= 0 || pct > 100 {
		return 0, &KeeperConfigError{
			Field:  name,
			Reason: fmt.Sprintf("%d is out of range; a context-use percentage must be in (0, 100]", pct),
		}
	}
	return float64(pct) / 100.0, nil
}

func emitKeeperConfigRejected(projectDir, agentName string, err error) {
	field := "config"
	var kce *KeeperConfigError
	var kme *KeeperConfigMissingError
	switch {
	case errors.As(err, &kce):
		field = kce.Field
	case errors.As(err, &kme):
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
	if emitErr := emitter.EmitWithRunID(context.Background(), core.RunID{}, "session_keeper_config_rejected", payload); emitErr != nil {
		return
	}
}

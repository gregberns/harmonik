package main

// resolve_keeper_config.go — the SINGLE keeper threshold/band precedence resolver
// (hk-4pnv).
//
// Why it lives in cmd/harmonik (NOT internal/keeper): depguard bans
// internal/keeper from importing internal/daemon (.golangci.yml:133, session-keeper
// spec hk-ekap1). The resolver needs daemon.KeeperConfig (the parsed
// .harmonik/config.yaml keeper: block), so it cannot live in internal/keeper. It
// lives here, reads the compiled defaults from internal/keeper's exported Default*
// consts (one source of truth), and returns a NEUTRAL resolved struct that the
// keeper start path translates into Watcher/Cycler config.
//
// # Precedence (per field)
//
//	FLAG > CONFIG > DEFAULT
//
// A zero / empty value at a level DEFERS to the next level down. The compiled
// DEFAULT is the keeper.Default* const — defaults live in ONE place.
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
// On ANY bad config value, invalid enum, malformed duration (already caught
// upstream in daemon.parseKeeperBlock), pct>1, a cross-field invariant violation
// (must hold warn < act < force_act < hard_ceiling, and warn_pct < act_pct), OR a
// bad CLI flag — ResolveKeeperConfig returns a *KeeperConfigError. It NEVER
// silently defaults and NEVER reverts the block to compiled defaults. The keeper
// start path logs the error to stderr (and an event when reachable) and refuses to
// start, so a misconfiguration is LEARNED, never masked.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	HeartbeatMaxMisses   int

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
}

// ResolveKeeperConfig implements FLAG > CONFIG > DEFAULT per field and validates
// the cross-field band invariants, returning a *KeeperConfigError (NOT a silent
// default) on any violation. Defaults are read from keeper.Default* so the band
// lives in ONE place. See the file header for the full contract.
func ResolveKeeperConfig(flags KeeperFlags, cfg daemon.KeeperConfig) (ResolvedKeeperConfig, error) {
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
		out.HardCeilingMode = keeper.HardCeilingModeAlarm // zero value = compiled default
	}

	// ── Watcher timing/cadence/budget: tier-1 = FLAG>CONFIG>DEFAULT; the long ──
	// tail = CONFIG>DEFAULT. Reading every default from keeper.Default* so the
	// band/cadence lives in ONE place. R2: every Default* must be reachable from
	// config, and every configured value must reach the constructed Watcher/Cycler.
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
	out.HardCeilingCooldown = resolveDur(0, false, cfg.CadenceHardCeilingCooldown, keeper.DefaultHardCeilingCooldown)
	out.BlindKeeperThreshold = resolveDur(0, false, cfg.BlindKeeperThreshold, keeper.DefaultBlindKeeperThreshold)
	out.HeartbeatMaxMisses = resolveInt(0, false, cfg.HeartbeatMaxMisses, keeper.DefaultMaxHeartbeatMisses)

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
	// BootGrace: FLAG > CONFIG; 0 = disabled is HONORED when explicitly set. When
	// neither set, BootGraceSet=false signals the start path to feed
	// DefaultBootGracePeriod at the construction site (opt-in-per-site contract).
	switch {
	case flags.BootGraceSet:
		out.BootGrace = flags.BootGrace
		out.BootGraceSet = true
	case cfg.BootGrace != 0:
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
	if !(out.ForceActAbsTokens < out.HardCeilingAbsTokens) {
		return ResolvedKeeperConfig{}, &KeeperConfigError{
			Field:  "force_act<hard_ceiling",
			Reason: fmt.Sprintf("band inversion: force_act_abs_tokens (%d) must be < hard_ceiling_abs_tokens (%d)", out.ForceActAbsTokens, out.HardCeilingAbsTokens),
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
	if errors.As(err, &kce) {
		field = kce.Field
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

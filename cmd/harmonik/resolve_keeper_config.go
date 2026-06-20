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

	// Sentinel-bearing pass-throughs (NOT defaulted by this resolver — their zero
	// /negative carries special meaning the start path honors):
	//   BootGrace          0  = disabled (young-session guard off)
	//   WarnCooldown    < 0   = disabled (warn-firing cooldown off)
	//   MaxHandoffTimeouts 0  = no-escalation
	// Their non-sentinel defaults are applied DOWNSTREAM (Watcher/Cycler
	// applyDefaults), exactly as before — the resolver only forwards the configured
	// value.
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

	// ── hard ceiling (CONFIG > DEFAULT). The SID-independent backstop. ──
	if cfg.HardCeilingAbsTokens > 0 {
		out.HardCeilingAbsTokens = cfg.HardCeilingAbsTokens
	} else {
		out.HardCeilingAbsTokens = keeper.HardCeilingAbsTokens
	}

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

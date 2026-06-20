package keeper

import "time"

// thresholds.go is the SINGLE source of truth for the keeper warn/act/force
// context-band. Both WatcherConfig.applyDefaults and CyclerConfig.applyDefaults
// reference the constants below, and every effective-threshold computation routes
// through minAbsOrPctCeil — so the two configs can never drift out of sync, and
// the min(abs, pctCeil*window) formula exists in exactly one place.
//
// Changing any value here is a deliberate band-retune: an operator decision (the
// operator HARD-NO on widening the band stands — see codename:keeper-redesign),
// never a side effect of a refactor. The thresholds_test.go defaults-PIN locks
// these values. Refs: hk-bpkv, hk-lhu2, hk-odhh, hk-8hr1.
//
// INVARIANT: warn < act < force_act (asserted in thresholds_test.go).
// TA1 band-retune (hk-8hr1): warn=200K / act=215K / force_act=240K — restart
// EARLIER to cap cache-read token spend. On a 1M window the abs values win
// (~20-24% of window); on a 200K window the pctCeil caps fire first (~70-95%).
// The 15K warn→act gap is intentional: the handoff is written via injected
// /session-handoff during the cycle, not from band width.

const (
	// Pct-based fallbacks (used when CtxFile.Tokens==0 or WindowSize==0 — older
	// Claude Code versions without absolute-token counts).
	defaultWarnPct = 80.0
	defaultActPct  = 90.0
	// defaultForceActPctOffset derives ForceActPct from ActPct so a custom
	// --act-pct never opens a dead zone above act but below force-clear (hk-6el).
	defaultForceActPctOffset = 5.0 // ForceActPct = ActPct + this

	// Absolute-token thresholds (preferred when Tokens + WindowSize are present).
	// TA1 band-retune (hk-8hr1): warn=200K / act=215K to restart EARLIER and cap
	// cache-read token spend. Operator-authorized 2026-06-17.
	defaultWarnAbsTokens = 200_000
	defaultActAbsTokens  = 215_000
	// defaultForceActAbsOffset derives ForceActAbsTokens from ActAbsTokens.
	// force_act = 215K + 25K = 240K (hk-8hr1). Satisfies warn<act<force_act.
	defaultForceActAbsOffset = 25_000 // ForceActAbsTokens = ActAbsTokens + this

	// Pct-of-window caps. The effective threshold is min(abs, pctCeil*window),
	// so the gate fires early enough on both 200k and 1M windows.
	defaultWarnPctCeil = 0.70
	defaultActPctCeil  = 0.85
	// defaultForceActPctCeilOffset derives ForceActPctCeil from ActPctCeil.
	defaultForceActPctCeilOffset = 0.10 // ForceActPctCeil = ActPctCeil + this

	// defaultFallbackWindowSize is the assumed context-window size used for the
	// pct-ceil cap when the gauge reports WindowSize==0 (e.g. [1m]-class models
	// whose window size cannot be inferred). Set via --window-size.
	defaultFallbackWindowSize = 200_000
)

// DefaultWarnAbsTokens and DefaultActAbsTokens are the exported forms of the
// absolute-token band thresholds. Use these wherever the values must be
// referenced outside this package (e.g. cmd/harmonik/keeper_cmd.go warning
// messages) so the printed text stays in sync with the live defaults. Refs: hk-cu7g.
const (
	DefaultWarnAbsTokens = defaultWarnAbsTokens
	DefaultActAbsTokens  = defaultActAbsTokens
)

// HardCeilingAbsTokens is the SID-independent absolute-token hard ceiling
// (hk-34ac). When any watched pane's token count meets or exceeds this value
// the keeper forces a handoff+restart regardless of whether the session_id
// binding is correct. This is a last-resort backstop so a mis-bound keeper
// cannot silently allow a session to overflow.
//
// NOTE: This value is deliberately ABOVE the normal band (warn=200K /
// act=215K / force_act=240K). It does NOT change the warn/act/force_act
// thresholds; it is an additional independent trip-wire. Refs: hk-34ac.
const HardCeilingAbsTokens int64 = 280_000

// DefaultBootGracePeriod is the YOUNG-SESSION guard window: the minimum time a
// session must run after a session_id CHANGE before the keeper will restart it.
// It is LOAD-BEARING under the aggressive earlier band (hk-8hr1): warn=200K /
// act=215K restarts much sooner, so without this guard the keeper could clear a
// session that just finished a /session-resume and has barely begun work. The
// force-act ceiling (240K) bypasses this grace — a session already that full is
// genuinely at risk of pane-overflow regardless of age. Wired into the live
// keeper command (cmd/harmonik/keeper_cmd.go). NOT applied as a CyclerConfig
// applyDefaults default: that would suppress the immediate-fire semantics every
// non-keeper caller (and the test suite) relies on; the guard is opt-in per
// construction site, and the production site opts in. Refs: hk-4f8, hk-ibb.
const DefaultBootGracePeriod = 5 * time.Minute

// minAbsOrPctCeil returns the effective absolute-token threshold for windowSize:
// min(abs, int64(pctCeil*windowSize)) when windowSize>0 AND pctCeil>0, otherwise
// abs. This is the one shared implementation of the keeper band formula; the
// pctCeil>0 guard preserves the watcher's historical behaviour for a zero ceil.
func minAbsOrPctCeil(abs int64, pctCeil float64, windowSize int64) int64 {
	if windowSize > 0 && pctCeil > 0 {
		pctBased := int64(pctCeil * float64(windowSize))
		if pctBased < abs {
			return pctBased
		}
	}
	return abs
}

// EffectiveBandTokens resolves the EFFECTIVE warn/act/force absolute-token band
// the keeper will actually fire on, for honest banner display (W7, hk-x7s). It
// applies the compiled defaults to any 0 input, then runs the SAME tighten-only
// min(abs, pctCeil*window) formula the live gate uses — so an explicit --warn-pct
// / --act-pct can only move the threshold EARLIER (never later) than the abs band.
// windowSize 0 means "window unknown at startup"; the abs values are then returned
// unchanged (the pct ceil is applied at runtime once the gauge reports a window).
// ForceActAbsTokens is derived as ActAbsTokens+defaultForceActAbsOffset when its
// input is 0, mirroring applyDefaults.
func EffectiveBandTokens(warnAbs, actAbs, forceActAbs int64, warnPctCeil, actPctCeil float64, windowSize int64) (warn, act, force int64) {
	if warnAbs <= 0 {
		warnAbs = defaultWarnAbsTokens
	}
	if actAbs <= 0 {
		actAbs = defaultActAbsTokens
	}
	if forceActAbs <= 0 {
		forceActAbs = actAbs + defaultForceActAbsOffset
	}
	if warnPctCeil <= 0 {
		warnPctCeil = defaultWarnPctCeil
	}
	if actPctCeil <= 0 {
		actPctCeil = defaultActPctCeil
	}
	warn = minAbsOrPctCeil(warnAbs, warnPctCeil, windowSize)
	act = minAbsOrPctCeil(actAbs, actPctCeil, windowSize)
	force = minAbsOrPctCeil(forceActAbs, actPctCeil+defaultForceActPctCeilOffset, windowSize)
	return warn, act, force
}

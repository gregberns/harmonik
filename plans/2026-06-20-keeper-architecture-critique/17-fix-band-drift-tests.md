# hk-j4m6 — Fix stale keeper band-drift integration tests

## Problem
Two integration-tagged keeper tests asserted the OLD context band (270k/300k/340k).
The keeper band was retuned to the TA1 earlier-restart values (warn 200k / act 215k /
force_act 240k) in `internal/keeper/thresholds.go` (hk-8hr1, operator-authorized
2026-06-17). The code is correct; the tests were stale and failing.

The separate 300k cap (ctx-watchdog) is a DIFFERENT mechanism — `thresholds.go` was
NOT touched.

## Canonical band (source of truth: thresholds.go)
- warn  = 200_000
- act   = 215_000
- force = 240_000 (act + 25k offset)
- ActPctCeil 0.85, WarnPctCeil 0.70, force pctceil 0.95 (unchanged)
- Pct fallbacks: warn 80 / act 90 / force 95 (unchanged)

## Changes — `internal/keeper/cycle_twin_e2e_integration_test.go`
1. `TestIntegration_TwinE2E_DefaultsPin`: ActAbsTokens 300k→215k, WarnAbsTokens
   270k→200k, ForceActAbsTokens 340k→240k. Doc comment updated (Refs hk-8hr1).
   Pct-ceil and pct values unchanged (already canonical).
2. `TestIntegration_TwinE2E_GaugeStateTransitions`: updated 1M-window boundary cases
   to the new band — below-act 299999→214999, at-act 300000→215000,
   below-warn 269999→199999, act-but-not-crisp 300000→215000,
   force-bypasses-crisp 340000→240000. The 200k-window ceil cases (0.85*200k=170k)
   were UNCHANGED since ActPctCeil stayed 0.85.

## Verification
- `go test -tags integration ./internal/keeper/... -run 'DefaultsPin|GaugeStateTransitions'` → ok (1.3s)
- `go test ./internal/keeper/...` (default suite) → ok (2.9s), no regression
- tmux sessions: 9 before, 9 after both runs — NO `*-flywheel` leak observed
  (bead hk-0ouc not triggered).

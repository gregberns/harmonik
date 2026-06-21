package main

import (
	"errors"
	"testing"

	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/keeper"
)

// resolve_keeper_config_hk4pnv_test.go — table-driven coverage for the single
// keeper threshold/band precedence resolver (hk-4pnv). Covers:
//   - FLAG > CONFIG precedence per representative field (no runtime DEFAULT layer)
//   - tighten-only pct (a loosening pct flag is REJECTED)
//   - force-act precedence (explicit abs wins over offset)
//   - fail-loud: band inversion / bad value / bad flag → *KeeperConfigError
//     (NOT a silent revert-to-defaults)
//
// Operator-philosophy change: the resolver imposes NO runtime default, so every
// case starts from completeTestKeeperConfig() (all required values set) and mutates
// the field(s) under test. The suggested (keeper.Default*) values double as the
// "what a complete config resolves to" baseline.

// TestResolveKeeperConfig_PrecedenceAndSemantics is the success-path table: each
// case mutates the complete baseline and asserts the resolved band field equals the
// expected post-precedence value.
func TestResolveKeeperConfig_PrecedenceAndSemantics(t *testing.T) {
	tests := []struct {
		name   string
		flags  KeeperFlags
		mutate func(*daemon.KeeperConfig)
		// expected resolved values
		wantWarnAbs  int64
		wantActAbs   int64
		wantForce    int64
		wantWarnCeil float64
		wantActCeil  float64
	}{
		{
			name:         "complete config resolves to its set values (suggested baseline)",
			wantWarnAbs:  keeper.DefaultWarnAbsTokens,
			wantActAbs:   keeper.DefaultActAbsTokens,
			wantForce:    keeper.DefaultActAbsTokens + keeper.DefaultForceActAbsOffset,
			wantWarnCeil: keeper.DefaultWarnPctCeil,
			wantActCeil:  keeper.DefaultActPctCeil,
		},
		{
			name: "config overrides baseline (warn-abs/act-abs)",
			mutate: func(c *daemon.KeeperConfig) {
				c.WarnAbsTokens = 180_000
				c.ActAbsTokens = 190_000
				// force_act must stay above act; baseline force_act (240k) is fine.
			},
			wantWarnAbs:  180_000,
			wantActAbs:   190_000,
			wantForce:    keeper.DefaultActAbsTokens + keeper.DefaultForceActAbsOffset,
			wantWarnCeil: keeper.DefaultWarnPctCeil,
			wantActCeil:  keeper.DefaultActPctCeil,
		},
		{
			name:  "flag beats config (warn-abs)",
			flags: KeeperFlags{WarnAbsTokens: 160_000, WarnAbsSet: true},
			mutate: func(c *daemon.KeeperConfig) {
				c.WarnAbsTokens = 180_000
				c.ActAbsTokens = 190_000
			},
			wantWarnAbs:  160_000, // flag wins
			wantActAbs:   190_000, // config wins (no flag)
			wantForce:    keeper.DefaultActAbsTokens + keeper.DefaultForceActAbsOffset,
			wantWarnCeil: keeper.DefaultWarnPctCeil,
			wantActCeil:  keeper.DefaultActPctCeil,
		},
		{
			name: "explicit force_act_abs WINS over offset",
			mutate: func(c *daemon.KeeperConfig) {
				c.ForceActAbsTokens = 250_000
				c.Present.ForceActAbsTokens = true
				c.ForceActAbsOffset = 99_000
				c.Present.ForceActAbsOffset = true
				c.HardCeilingAbsTokens = 300_000 // keep force_act < hard_ceiling
			},
			wantWarnAbs:  keeper.DefaultWarnAbsTokens,
			wantActAbs:   keeper.DefaultActAbsTokens,
			wantForce:    250_000, // abs wins; the 99k offset is ignored
			wantWarnCeil: keeper.DefaultWarnPctCeil,
			wantActCeil:  keeper.DefaultActPctCeil,
		},
		{
			name: "force_act offset used only when abs unset",
			mutate: func(c *daemon.KeeperConfig) {
				c.ForceActAbsTokens = 0 // unset the baseline absolute
				c.Present.ForceActAbsTokens = false
				c.ForceActAbsOffset = 30_000
				c.Present.ForceActAbsOffset = true
			},
			wantWarnAbs:  keeper.DefaultWarnAbsTokens,
			wantActAbs:   keeper.DefaultActAbsTokens,
			wantForce:    keeper.DefaultActAbsTokens + 30_000, // act + config offset
			wantWarnCeil: keeper.DefaultWarnPctCeil,
			wantActCeil:  keeper.DefaultActPctCeil,
		},
		{
			name:         "tighten-only: a LOWER pct flag is honored (moves band earlier)",
			flags:        KeeperFlags{WarnPct: 30, WarnPctSet: true, ActPct: 35, ActPctSet: true},
			wantWarnAbs:  keeper.DefaultWarnAbsTokens,
			wantActAbs:   keeper.DefaultActAbsTokens,
			wantForce:    keeper.DefaultActAbsTokens + keeper.DefaultForceActAbsOffset,
			wantWarnCeil: 0.30, // 30/100, lower than 0.70 baseline → tightened
			wantActCeil:  0.35, // 35/100, lower than 0.85 baseline → tightened
		},
		{
			name: "config pct ceil overrides baseline",
			mutate: func(c *daemon.KeeperConfig) {
				c.WarnPctCeil = 0.60
				c.ActPctCeil = 0.80
			},
			wantWarnAbs:  keeper.DefaultWarnAbsTokens,
			wantActAbs:   keeper.DefaultActAbsTokens,
			wantForce:    keeper.DefaultActAbsTokens + keeper.DefaultForceActAbsOffset,
			wantWarnCeil: 0.60,
			wantActCeil:  0.80,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := completeTestKeeperConfig()
			if tc.mutate != nil {
				tc.mutate(&cfg)
			}
			got, err := ResolveKeeperConfig(tc.flags, cfg, t.TempDir())
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.WarnAbsTokens != tc.wantWarnAbs {
				t.Errorf("WarnAbsTokens = %d, want %d", got.WarnAbsTokens, tc.wantWarnAbs)
			}
			if got.ActAbsTokens != tc.wantActAbs {
				t.Errorf("ActAbsTokens = %d, want %d", got.ActAbsTokens, tc.wantActAbs)
			}
			if got.ForceActAbsTokens != tc.wantForce {
				t.Errorf("ForceActAbsTokens = %d, want %d", got.ForceActAbsTokens, tc.wantForce)
			}
			if got.WarnPctCeil != tc.wantWarnCeil {
				t.Errorf("WarnPctCeil = %v, want %v", got.WarnPctCeil, tc.wantWarnCeil)
			}
			if got.ActPctCeil != tc.wantActCeil {
				t.Errorf("ActPctCeil = %v, want %v", got.ActPctCeil, tc.wantActCeil)
			}
		})
	}
}

// TestResolveKeeperConfig_FailLoud asserts the operator-decision posture: a bad
// PRESENT value, a band inversion, or a loosening/out-of-range pct flag returns a
// *KeeperConfigError — NOT a silent default and NOT a revert-to-defaults. Each case
// starts from a complete config so the missing-value gate does not pre-empt the
// value-validation error under test.
func TestResolveKeeperConfig_FailLoud(t *testing.T) {
	tests := []struct {
		name      string
		flags     KeeperFlags
		mutate    func(*daemon.KeeperConfig)
		wantField string // expected error's Field
	}{
		{
			name: "band inversion: warn >= act (abs)",
			mutate: func(c *daemon.KeeperConfig) {
				c.WarnAbsTokens = 220_000
				c.ActAbsTokens = 210_000
			},
			wantField: "warn<act",
		},
		{
			name: "band inversion: act >= force_act (explicit force below act)",
			mutate: func(c *daemon.KeeperConfig) {
				c.ActAbsTokens = 215_000
				c.ForceActAbsTokens = 200_000
			},
			wantField: "act<force_act",
		},
		{
			name: "band inversion: force_act >= hard_ceiling",
			mutate: func(c *daemon.KeeperConfig) {
				c.ForceActAbsTokens = 300_000
				c.HardCeilingAbsTokens = 290_000
			},
			wantField: "force_act<hard_ceiling",
		},
		{
			name: "band inversion: warn_pct >= act_pct (config)",
			mutate: func(c *daemon.KeeperConfig) {
				c.WarnPctCeil = 0.90
				c.ActPctCeil = 0.80
			},
			wantField: "warn_pct<act_pct",
		},
		{
			// hk-z8d0: restart-mode ceiling AT force_act is nonsensical.
			name: "restart-mode hard ceiling == force_act is rejected",
			mutate: func(c *daemon.KeeperConfig) {
				c.WarnAbsTokens = 200_000
				c.ActAbsTokens = 215_000
				c.ForceActAbsTokens = 240_000
				c.HardCeilingAbsTokens = 240_000 // == force_act
				c.HardCeilingMode = "restart"
			},
			wantField: "hard_ceiling.abs_tokens",
		},
		{
			name: "restart-mode hard ceiling below force_act is rejected",
			mutate: func(c *daemon.KeeperConfig) {
				c.WarnAbsTokens = 200_000
				c.ActAbsTokens = 215_000
				c.ForceActAbsTokens = 240_000
				c.HardCeilingAbsTokens = 230_000 // < force_act
				c.HardCeilingMode = "restart"
			},
			wantField: "hard_ceiling.abs_tokens",
		},
		{
			name:      "tighten-only violated: a LOOSER warn-pct flag is rejected",
			flags:     KeeperFlags{WarnPct: 95, WarnPctSet: true}, // 0.95 > 0.70 baseline
			wantField: "--warn-pct",
		},
		{
			name:      "tighten-only violated: a LOOSER act-pct flag is rejected",
			flags:     KeeperFlags{ActPct: 99, ActPctSet: true}, // 0.99 > 0.85 baseline
			wantField: "--act-pct",
		},
		{
			name:      "bad flag: out-of-range warn-pct (>100)",
			flags:     KeeperFlags{WarnPct: 150, WarnPctSet: true},
			wantField: "--warn-pct",
		},
		{
			name:      "bad flag: zero/negative act-pct explicitly set",
			flags:     KeeperFlags{ActPct: 0, ActPctSet: true},
			wantField: "--act-pct",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := completeTestKeeperConfig()
			if tc.mutate != nil {
				tc.mutate(&cfg)
			}
			got, err := ResolveKeeperConfig(tc.flags, cfg, t.TempDir())
			if err == nil {
				t.Fatalf("expected a *KeeperConfigError, got nil (resolved=%+v) — fail-loud must NOT silently default", got)
			}
			var kce *KeeperConfigError
			if !errors.As(err, &kce) {
				t.Fatalf("expected *KeeperConfigError, got %T: %v", err, err)
			}
			if kce.Field != tc.wantField {
				t.Errorf("error Field = %q, want %q (reason: %s)", kce.Field, tc.wantField, kce.Reason)
			}
			// Fail-loud must NOT revert to defaults: the returned struct is the zero value.
			if (got != ResolvedKeeperConfig{}) {
				t.Errorf("on error the resolved struct must be zero (no revert-to-defaults), got %+v", got)
			}
		})
	}
}

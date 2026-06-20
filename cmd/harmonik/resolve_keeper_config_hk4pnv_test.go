package main

import (
	"errors"
	"testing"

	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/keeper"
)

// resolve_keeper_config_hk4pnv_test.go — table-driven coverage for the single
// keeper threshold/band precedence resolver (hk-4pnv). Covers:
//   - FLAG > CONFIG > DEFAULT precedence per representative field
//   - tighten-only pct (a loosening pct flag is REJECTED)
//   - force-act precedence (explicit abs wins over offset)
//   - fail-loud: band inversion / bad value / bad flag → *KeeperConfigError
//     (NOT a silent revert-to-defaults)

// TestResolveKeeperConfig_PrecedenceAndSemantics is the success-path table: each
// case asserts the resolved band field equals the expected post-precedence value.
func TestResolveKeeperConfig_PrecedenceAndSemantics(t *testing.T) {
	tests := []struct {
		name  string
		flags KeeperFlags
		cfg   daemon.KeeperConfig
		// expected resolved values (0 = "assert equals keeper default")
		wantWarnAbs  int64
		wantActAbs   int64
		wantForce    int64
		wantWarnCeil float64
		wantActCeil  float64
	}{
		{
			name:         "all defaults (no flag, no config)",
			wantWarnAbs:  keeper.DefaultWarnAbsTokens,
			wantActAbs:   keeper.DefaultActAbsTokens,
			wantForce:    keeper.DefaultActAbsTokens + keeper.DefaultForceActAbsOffset,
			wantWarnCeil: keeper.DefaultWarnPctCeil,
			wantActCeil:  keeper.DefaultActPctCeil,
		},
		{
			name:         "config overrides default (warn-abs/act-abs)",
			cfg:          daemon.KeeperConfig{WarnAbsTokens: 180_000, ActAbsTokens: 190_000},
			wantWarnAbs:  180_000,
			wantActAbs:   190_000,
			wantForce:    190_000 + keeper.DefaultForceActAbsOffset,
			wantWarnCeil: keeper.DefaultWarnPctCeil,
			wantActCeil:  keeper.DefaultActPctCeil,
		},
		{
			name:         "flag beats config beats default (warn-abs)",
			flags:        KeeperFlags{WarnAbsTokens: 160_000, WarnAbsSet: true},
			cfg:          daemon.KeeperConfig{WarnAbsTokens: 180_000, ActAbsTokens: 190_000},
			wantWarnAbs:  160_000, // flag wins
			wantActAbs:   190_000, // config wins (no flag)
			wantForce:    190_000 + keeper.DefaultForceActAbsOffset,
			wantWarnCeil: keeper.DefaultWarnPctCeil,
			wantActCeil:  keeper.DefaultActPctCeil,
		},
		{
			name:         "explicit force_act_abs WINS over offset",
			cfg:          daemon.KeeperConfig{ForceActAbsTokens: 250_000, ForceActAbsOffset: 99_000},
			wantWarnAbs:  keeper.DefaultWarnAbsTokens,
			wantActAbs:   keeper.DefaultActAbsTokens,
			wantForce:    250_000, // abs wins; the 99k offset is ignored
			wantWarnCeil: keeper.DefaultWarnPctCeil,
			wantActCeil:  keeper.DefaultActPctCeil,
		},
		{
			name:         "force_act offset used only when abs unset",
			cfg:          daemon.KeeperConfig{ForceActAbsOffset: 30_000},
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
			wantWarnCeil: 0.30, // 30/100, lower than 0.70 default → tightened
			wantActCeil:  0.35, // 35/100, lower than 0.85 default → tightened
		},
		{
			name:         "config pct ceil overrides default",
			cfg:          daemon.KeeperConfig{WarnPctCeil: 0.60, ActPctCeil: 0.80},
			wantWarnAbs:  keeper.DefaultWarnAbsTokens,
			wantActAbs:   keeper.DefaultActAbsTokens,
			wantForce:    keeper.DefaultActAbsTokens + keeper.DefaultForceActAbsOffset,
			wantWarnCeil: 0.60,
			wantActCeil:  0.80,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveKeeperConfig(tc.flags, tc.cfg)
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
// value, a band inversion, or a loosening/out-of-range pct flag returns a
// *KeeperConfigError — NOT a silent default and NOT a revert-to-defaults.
func TestResolveKeeperConfig_FailLoud(t *testing.T) {
	tests := []struct {
		name      string
		flags     KeeperFlags
		cfg       daemon.KeeperConfig
		wantField string // substring expected in the error's Field
	}{
		{
			name:      "band inversion: warn >= act (abs)",
			cfg:       daemon.KeeperConfig{WarnAbsTokens: 220_000, ActAbsTokens: 210_000},
			wantField: "warn<act",
		},
		{
			name:      "band inversion: act >= force_act (explicit force below act)",
			cfg:       daemon.KeeperConfig{ActAbsTokens: 215_000, ForceActAbsTokens: 200_000},
			wantField: "act<force_act",
		},
		{
			name:      "band inversion: force_act >= hard_ceiling",
			cfg:       daemon.KeeperConfig{ForceActAbsTokens: 300_000, HardCeilingAbsTokens: 290_000},
			wantField: "force_act<hard_ceiling",
		},
		{
			name:      "band inversion: warn_pct >= act_pct (config)",
			cfg:       daemon.KeeperConfig{WarnPctCeil: 0.90, ActPctCeil: 0.80},
			wantField: "warn_pct<act_pct",
		},
		{
			name:      "tighten-only violated: a LOOSER warn-pct flag is rejected",
			flags:     KeeperFlags{WarnPct: 95, WarnPctSet: true}, // 0.95 > 0.70 default
			wantField: "--warn-pct",
		},
		{
			name:      "tighten-only violated: a LOOSER act-pct flag is rejected",
			flags:     KeeperFlags{ActPct: 99, ActPctSet: true}, // 0.99 > 0.85 default
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
			got, err := ResolveKeeperConfig(tc.flags, tc.cfg)
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
			// Fail-loud must NOT revert to defaults: the returned struct is the zero
			// value, never a defaulted band.
			if (got != ResolvedKeeperConfig{}) {
				t.Errorf("on error the resolved struct must be zero (no revert-to-defaults), got %+v", got)
			}
		})
	}
}

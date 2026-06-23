package daemon_test

// projectconfig_hkexg3_test.go — unit tests for the keeperBlockAbsent helper
// (hk-exg3), the explicit field-by-field zero check that replaces the
// `raw.Keeper == (rawKeeperConfig{})` empty-block sentinel so a future
// slice / map / nested-non-comparable keeper sub-field cannot break
// compilation of the absent-file fast path.
//
// Covers:
//   - keeperBlockAbsent returns true for a zero rawKeeperConfig.
//   - keeperBlockAbsent returns false when ANY field is set (one sub-test per
//     field, so a newly-added field that is forgotten in the helper is caught).
//   - A keeper-only config file (keeper block present) under schema_version: 1
//     does NOT incorrectly trip ErrUnsupportedConfigVersion (the absent-file
//     fast path must NOT swallow a real keeper block).
//
// Helper prefix: keeperAbsentFixture (implementer-protocol.md §Helper-prefix discipline).
//
// Bead: hk-exg3.

import (
	"errors"
	"testing"

	"github.com/gregberns/harmonik/internal/daemon"
)

// keeperAbsentFixtureDir reuses projCfgFixtureDir to write a config.yaml.
func keeperAbsentFixtureDir(t *testing.T, yamlContent string) string {
	t.Helper()
	return projCfgFixtureDir(t, yamlContent)
}

// ─────────────────────────────────────────────────────────────────────────────
// keeperBlockAbsent: zero value
// ─────────────────────────────────────────────────────────────────────────────

func TestKeeperBlockAbsent_ZeroValue_True(t *testing.T) {
	t.Parallel()

	if !daemon.ExportedKeeperBlockAbsent(daemon.ExportedRawKeeperConfig{}) {
		t.Errorf("keeperBlockAbsent(zero): want true, got false")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// keeperBlockAbsent: any field set → false (one sub-test per field)
// ─────────────────────────────────────────────────────────────────────────────

func TestKeeperBlockAbsent_AnyFieldSet_False(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		raw  daemon.ExportedRawKeeperConfig
	}{
		{
			name: "WarnAbsTokens",
			raw: daemon.ExportedRawKeeperConfig{
				ContextThresholds: daemon.ExportedRawKeeperContextThresholds{WarnAbsTokens: 270000},
			},
		},
		{
			name: "ActAbsTokens",
			raw: daemon.ExportedRawKeeperConfig{
				ContextThresholds: daemon.ExportedRawKeeperContextThresholds{ActAbsTokens: 300000},
			},
		},
		{
			name: "ForceActAbsTokens",
			raw: daemon.ExportedRawKeeperConfig{
				ContextThresholds: daemon.ExportedRawKeeperContextThresholds{ForceActAbsTokens: 340000},
			},
		},
		{
			name: "ActPctCeil",
			raw: daemon.ExportedRawKeeperConfig{
				ContextThresholds: daemon.ExportedRawKeeperContextThresholds{ActPctCeil: 0.85},
			},
		},
		{
			name: "WarnPctCeil",
			raw: daemon.ExportedRawKeeperConfig{
				ContextThresholds: daemon.ExportedRawKeeperContextThresholds{WarnPctCeil: 0.70},
			},
		},
		{
			name: "DefaultWarnText",
			raw: daemon.ExportedRawKeeperConfig{
				WarnMessages: daemon.ExportedRawKeeperWarnMessages{DefaultWarnText: "wrap up"},
			},
		},
		{
			name: "OnDemandWarnText",
			raw: daemon.ExportedRawKeeperConfig{
				WarnMessages: daemon.ExportedRawKeeperWarnMessages{OnDemandWarnText: "restart now"},
			},
		},
		// hk-74iyd: conversation-aware ACT suppression cadence fields.
		{
			name: "OperatorTurnLookback",
			raw:  daemon.ExportedRawKeeperConfig{Cadence: daemon.ExportedRawKeeperCadence{OperatorTurnLookback: "5m"}},
		},
		{
			name: "PostAnswerGrace",
			raw:  daemon.ExportedRawKeeperConfig{Cadence: daemon.ExportedRawKeeperCadence{PostAnswerGrace: "30s"}},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if daemon.ExportedKeeperBlockAbsent(tc.raw) {
				t.Errorf("keeperBlockAbsent(%s set): want false, got true", tc.name)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// A keeper-only block under schema_version: 1 must NOT trip ErrUnsupportedConfigVersion
// ─────────────────────────────────────────────────────────────────────────────

// TestKeeperOnlyBlock_DoesNotTripUnsupportedVersion confirms that a config file
// carrying only a keeper: block (no agents, no daemon) under schema_version: 1
// loads cleanly — keeperBlockAbsent correctly reports the block PRESENT, so the
// absent-file fast path is NOT taken and the version check (1 == 1) passes.
func TestKeeperOnlyBlock_DoesNotTripUnsupportedVersion(t *testing.T) {
	t.Parallel()

	root := keeperAbsentFixtureDir(t, `
schema_version: 1
keeper:
  context_thresholds:
    warn_abs_tokens: 270000
`)
	cfg, err := daemon.ExportedLoadProjectConfig(root)
	if err != nil {
		t.Fatalf("LoadProjectConfig: unexpected error: %v", err)
	}

	var unsupported *daemon.ExportedErrUnsupportedConfigVersion
	if errors.As(err, &unsupported) {
		t.Fatalf("keeper-only block under schema_version: 1 incorrectly tripped ErrUnsupportedConfigVersion: %v", err)
	}

	if cfg.Keeper.WarnAbsTokens != 270000 {
		t.Errorf("keeper-only block: WarnAbsTokens: want 270000, got %d", cfg.Keeper.WarnAbsTokens)
	}
}

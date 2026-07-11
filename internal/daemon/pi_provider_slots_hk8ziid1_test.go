package daemon_test

// pi_provider_slots_hk8ziid1_test.go — struct-shape tests for the
// per-provider slot-accounting config seam (hk-8ziid.1, C1-design).
// docs/design/pi-multi-provider-slot-accounting.md
//
// Helper prefix: none needed — reuses projCfgFixtureDir from
// projectconfig_hkbfvk7_test.go (same package, same test binary).

import (
	"testing"

	"github.com/gregberns/harmonik/internal/daemon"
)

// TestPiHarnessConfig_ProviderSlots_ParsedFromConfig asserts
// harnesses.pi.provider_slots round-trips through LoadProjectConfig into
// PiHarnessConfig.ProviderSlots, keyed by provider string (not profile name).
func TestPiHarnessConfig_ProviderSlots_ParsedFromConfig(t *testing.T) {
	t.Parallel()

	root := projCfgFixtureDir(t, `
schema_version: 1
harnesses:
  pi:
    provider: openrouter
    model: deepseek/deepseek-v4-flash
    api_key_env: OPENROUTER_API_KEY
    provider_slots:
      openrouter: 4
      ornith: 2
`)
	cfg, err := daemon.ExportedLoadProjectConfig(root)
	if err != nil {
		t.Fatalf("LoadProjectConfig: unexpected error: %v", err)
	}

	slots := cfg.Harnesses.Pi.ProviderSlots
	if got := slots["openrouter"]; got != 4 {
		t.Errorf("ProviderSlots[openrouter] = %d, want 4", got)
	}
	if got := slots["ornith"]; got != 2 {
		t.Errorf("ProviderSlots[ornith] = %d, want 2", got)
	}
	if len(slots) != 2 {
		t.Errorf("ProviderSlots has %d entries, want 2 (got %v)", len(slots), slots)
	}
}

// TestPiHarnessConfig_ProviderSlots_AbsentIsNil asserts that omitting
// provider_slots leaves ProviderSlots nil — the backward-compat invariant
// that every provider is unbounded when the block is never configured.
func TestPiHarnessConfig_ProviderSlots_AbsentIsNil(t *testing.T) {
	t.Parallel()

	root := projCfgFixtureDir(t, `
schema_version: 1
harnesses:
  pi:
    provider: openrouter
    model: deepseek/deepseek-v4-flash
    api_key_env: OPENROUTER_API_KEY
`)
	cfg, err := daemon.ExportedLoadProjectConfig(root)
	if err != nil {
		t.Fatalf("LoadProjectConfig: unexpected error: %v", err)
	}

	if cfg.Harnesses.Pi.ProviderSlots != nil {
		t.Errorf("ProviderSlots = %v, want nil when provider_slots absent", cfg.Harnesses.Pi.ProviderSlots)
	}
}

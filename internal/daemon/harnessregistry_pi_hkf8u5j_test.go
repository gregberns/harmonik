package daemon_test

// harnessregistry_pi_hkf8u5j_test.go — Pi harness config→registry seam coverage
// (hk-f8u5j: pilot phase-0.5 wiring).
//
// Verifies that when newHarnessRegistry is called with a non-empty PiHarnessConfig,
// the registered PiHarness carries the provider, model, and api_key_env fields
// through from config — i.e., the wiring from ResolvePiConfig output → NewPiHarness
// is intact and the pi harness is not registered with empty strings.

import (
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// TestHarnessRegistry_PiHarness_ConfiguredFields_NonEmpty verifies that when
// newHarnessRegistry receives a PiHarnessConfig with all required fields set,
// the registered PiHarness has non-empty provider, model, and api_key_env fields.
//
// This is the Phase-0.5 wiring assertion (hk-f8u5j): without this wiring,
// buildPiLaunchSpec always fails with "apiKeyEnv must be non-empty" because
// the PiHarness was registered with empty strings regardless of config.
func TestHarnessRegistry_PiHarness_ConfiguredFields_NonEmpty(t *testing.T) {
	t.Parallel()

	piCfg := daemon.PiHarnessConfig{
		Provider:  "openrouter",
		Model:     "openrouter/qwen/qwen3-coder",
		APIKeyEnv: "OPENROUTER_API_KEY",
	}

	reg, err := daemon.ExportedNewHarnessRegistryWithPi(piCfg)
	if err != nil {
		t.Fatalf("ExportedNewHarnessRegistryWithPi: %v", err)
	}

	h, err := reg.ForAgent(core.AgentTypePi)
	if err != nil {
		t.Fatalf("ForAgent(pi): %v", err)
	}

	ph, ok := h.(*daemon.PiHarness)
	if !ok {
		t.Fatalf("ForAgent(pi) returned %T; want *daemon.PiHarness", h)
	}

	provider, model, apiKeyEnv, _ := daemon.ExportedPiHarnessFields(ph)

	if provider == "" {
		t.Error("PiHarness.provider is empty after config wiring; want non-empty")
	}
	if model == "" {
		t.Error("PiHarness.model is empty after config wiring; want non-empty")
	}
	if apiKeyEnv == "" {
		t.Error("PiHarness.apiKeyEnv is empty after config wiring; want non-empty")
	}

	if provider != piCfg.Provider {
		t.Errorf("PiHarness.provider = %q; want %q", provider, piCfg.Provider)
	}
	if model != piCfg.Model {
		t.Errorf("PiHarness.model = %q; want %q", model, piCfg.Model)
	}
	if apiKeyEnv != piCfg.APIKeyEnv {
		t.Errorf("PiHarness.apiKeyEnv = %q; want %q", apiKeyEnv, piCfg.APIKeyEnv)
	}
}

// TestHarnessRegistry_PiHarness_EmptyConfig_FieldsEmpty verifies that when
// newHarnessRegistry receives an empty PiHarnessConfig (harnesses.pi absent from
// config.yaml), the registered PiHarness carries empty fields — so the
// "refuse to start" error from buildPiLaunchSpec surfaces at dispatch time
// rather than silently using a wrong default.
func TestHarnessRegistry_PiHarness_EmptyConfig_FieldsEmpty(t *testing.T) {
	t.Parallel()

	reg, err := daemon.ExportedNewHarnessRegistry() // zero PiHarnessConfig
	if err != nil {
		t.Fatalf("ExportedNewHarnessRegistry: %v", err)
	}

	h, err := reg.ForAgent(core.AgentTypePi)
	if err != nil {
		t.Fatalf("ForAgent(pi): %v", err)
	}

	ph, ok := h.(*daemon.PiHarness)
	if !ok {
		t.Fatalf("ForAgent(pi) returned %T; want *daemon.PiHarness", h)
	}

	provider, model, apiKeyEnv, _ := daemon.ExportedPiHarnessFields(ph)

	if provider != "" {
		t.Errorf("PiHarness.provider = %q; want empty when config absent", provider)
	}
	if model != "" {
		t.Errorf("PiHarness.model = %q; want empty when config absent", model)
	}
	if apiKeyEnv != "" {
		t.Errorf("PiHarness.apiKeyEnv = %q; want empty when config absent", apiKeyEnv)
	}
}

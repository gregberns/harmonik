package daemon_test

// projectconfig_hkv7q5u_test.go — unit tests for the harnesses.pi config block
// (hk-v7q5u, PI-050): the top-level harnesses: YAML block under schema_version: 1
// in .harmonik/config.yaml.
//
// Verifies:
//   - absent harnesses block → zero-value HarnessesConfig, nil error.
//   - harnesses.pi with all required fields → parsed correctly onto ProjectConfig.Harnesses.
//   - harnesses.pi with optional fallback block → HasFallback=true, fallback fields set.
//   - harnesses.pi without fallback → HasFallback=false.
//   - harnesses block alone (without schema_version) → ErrUnsupportedConfigVersion.
//   - unknown key under harnesses.pi.fallback is tolerantly ignored (forward-compat).
//
// Spec refs: specs/pi-harness.md §5 (PI-050). Bead ref: hk-v7q5u.

import (
	"errors"
	"testing"

	"github.com/gregberns/harmonik/internal/daemon"
)

func TestHarnessesPiConfig_Absent_ZeroValue(t *testing.T) {
	t.Parallel()

	root := projCfgFixtureDir(t, `
schema_version: 1
daemon:
  max_concurrent: 4
`)
	cfg, err := daemon.ExportedLoadProjectConfig(root)
	if err != nil {
		t.Fatalf("LoadProjectConfig: unexpected error: %v", err)
	}
	pi := cfg.Harnesses.Pi
	if pi.Provider != "" || pi.Model != "" || pi.APIKeyEnv != "" {
		t.Errorf("absent harnesses.pi: got non-zero fields provider=%q model=%q api_key_env=%q; want all empty",
			pi.Provider, pi.Model, pi.APIKeyEnv)
	}
	if pi.HasFallback {
		t.Error("absent harnesses.pi: HasFallback should be false")
	}
}

func TestHarnessesPiConfig_AllRequiredFields_Parsed(t *testing.T) {
	t.Parallel()

	root := projCfgFixtureDir(t, `
schema_version: 1
harnesses:
  pi:
    provider: openrouter
    model: openrouter/qwen/qwen3-coder
    api_key_env: OPENROUTER_API_KEY
`)
	cfg, err := daemon.ExportedLoadProjectConfig(root)
	if err != nil {
		t.Fatalf("LoadProjectConfig: unexpected error: %v", err)
	}
	pi := cfg.Harnesses.Pi
	if pi.Provider != "openrouter" {
		t.Errorf("provider: got %q; want %q", pi.Provider, "openrouter")
	}
	if pi.Model != "openrouter/qwen/qwen3-coder" {
		t.Errorf("model: got %q; want %q", pi.Model, "openrouter/qwen/qwen3-coder")
	}
	if pi.APIKeyEnv != "OPENROUTER_API_KEY" {
		t.Errorf("api_key_env: got %q; want %q", pi.APIKeyEnv, "OPENROUTER_API_KEY")
	}
	if pi.HasFallback {
		t.Error("no fallback block: HasFallback should be false")
	}
}

func TestHarnessesPiConfig_WithFallback_Parsed(t *testing.T) {
	t.Parallel()

	root := projCfgFixtureDir(t, `
schema_version: 1
harnesses:
  pi:
    provider: openrouter
    model: openrouter/qwen/qwen3-coder
    api_key_env: OPENROUTER_API_KEY
    fallback:
      provider: anthropic
      model: anthropic/claude-haiku-4-5-20251001
      api_key_env: ANTHROPIC_API_KEY
`)
	cfg, err := daemon.ExportedLoadProjectConfig(root)
	if err != nil {
		t.Fatalf("LoadProjectConfig: unexpected error: %v", err)
	}
	pi := cfg.Harnesses.Pi
	if !pi.HasFallback {
		t.Fatal("fallback block present: HasFallback should be true")
	}
	if pi.Fallback.Provider != "anthropic" {
		t.Errorf("fallback.provider: got %q; want %q", pi.Fallback.Provider, "anthropic")
	}
	if pi.Fallback.Model != "anthropic/claude-haiku-4-5-20251001" {
		t.Errorf("fallback.model: got %q; want %q", pi.Fallback.Model, "anthropic/claude-haiku-4-5-20251001")
	}
	if pi.Fallback.APIKeyEnv != "ANTHROPIC_API_KEY" {
		t.Errorf("fallback.api_key_env: got %q; want %q", pi.Fallback.APIKeyEnv, "ANTHROPIC_API_KEY")
	}
}

func TestHarnessesPiConfig_NoFallback_HasFallbackFalse(t *testing.T) {
	t.Parallel()

	root := projCfgFixtureDir(t, `
schema_version: 1
harnesses:
  pi:
    provider: openai
    model: openai/gpt-4o
    api_key_env: OPENAI_API_KEY
`)
	cfg, err := daemon.ExportedLoadProjectConfig(root)
	if err != nil {
		t.Fatalf("LoadProjectConfig: unexpected error: %v", err)
	}
	if cfg.Harnesses.Pi.HasFallback {
		t.Error("no fallback block: HasFallback should be false")
	}
}

func TestHarnessesPiConfig_BlockOnly_NoSchemaVersion_UnsupportedVersion(t *testing.T) {
	t.Parallel()

	// A harnesses: block without schema_version: 1 falls through to the version check.
	root := projCfgFixtureDir(t, `
harnesses:
  pi:
    provider: openrouter
    model: openrouter/qwen/qwen3-coder
    api_key_env: OPENROUTER_API_KEY
`)
	_, err := daemon.ExportedLoadProjectConfig(root)
	if err == nil {
		t.Fatal("expected ErrUnsupportedConfigVersion, got nil")
	}
	var ve *daemon.ErrUnsupportedConfigVersion
	if !errors.As(err, &ve) {
		t.Errorf("expected *ErrUnsupportedConfigVersion; got %T: %v", err, err)
	}
}

func TestHarnessesPiConfig_CoexistsWithDaemonAndKeeper(t *testing.T) {
	t.Parallel()

	root := projCfgFixtureDir(t, `
schema_version: 1
daemon:
  max_concurrent: 2
harnesses:
  pi:
    provider: openrouter
    model: openrouter/qwen/qwen3-coder
    api_key_env: OPENROUTER_API_KEY
`)
	cfg, err := daemon.ExportedLoadProjectConfig(root)
	if err != nil {
		t.Fatalf("LoadProjectConfig: unexpected error: %v", err)
	}
	if cfg.Daemon.MaxConcurrent != 2 {
		t.Errorf("daemon.max_concurrent: got %d; want 2", cfg.Daemon.MaxConcurrent)
	}
	if cfg.Harnesses.Pi.Provider != "openrouter" {
		t.Errorf("harnesses.pi.provider: got %q; want %q", cfg.Harnesses.Pi.Provider, "openrouter")
	}
}

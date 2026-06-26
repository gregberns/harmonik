package main

// resolve_pi_config_test.go — unit tests for ResolvePiConfig (hk-v7q5u, PI-051/PI-052).
//
// Verifies:
//   - all required fields present → returns cfg unchanged.
//   - all three required fields missing → PiConfigMissingError with all three keys.
//   - each required field missing individually → missing key in error.
//   - model shape validation: valid shapes pass, invalid chars fail (PI-052).
//   - model length limit: ≤128 chars passes, >128 fails.
//   - fallback present with all fields → accepted.
//   - fallback present with partial fields → missing error on the absent ones.
//   - fallback absent (HasFallback=false) → no fallback validation run.
//   - missing error message names yaml paths + 'harmonik pi config --example'.
//
// Spec refs: PI-051, PI-052. Bead ref: hk-v7q5u.

import (
	"errors"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/daemon"
)

// fullPiCfg is a valid PiHarnessConfig with all required fields set.
func fullPiCfg() daemon.PiHarnessConfig {
	return daemon.PiHarnessConfig{
		Provider:  "openrouter",
		Model:     "openrouter/qwen/qwen3-coder",
		APIKeyEnv: "OPENROUTER_API_KEY",
	}
}

func TestResolvePiConfig_AllRequired_OK(t *testing.T) {
	t.Parallel()
	cfg := fullPiCfg()
	got, err := ResolvePiConfig(cfg, "/proj")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Provider != cfg.Provider || got.Model != cfg.Model || got.APIKeyEnv != cfg.APIKeyEnv {
		t.Errorf("resolved cfg differs: %+v", got)
	}
}

func TestResolvePiConfig_AllMissing_AggregatesAllThree(t *testing.T) {
	t.Parallel()
	_, err := ResolvePiConfig(daemon.PiHarnessConfig{}, "/proj")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var me *PiConfigMissingError
	if !errors.As(err, &me) {
		t.Fatalf("expected *PiConfigMissingError; got %T: %v", err, err)
	}
	if len(me.Missing) != 3 {
		t.Errorf("expected 3 missing keys; got %d: %v", len(me.Missing), me.Missing)
	}
	wantKeys := []string{
		"harnesses.pi.provider",
		"harnesses.pi.model",
		"harnesses.pi.api_key_env",
	}
	for _, want := range wantKeys {
		found := false
		for _, got := range me.Missing {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing key %q not in error.Missing: %v", want, me.Missing)
		}
	}
}

func TestResolvePiConfig_MissingProvider(t *testing.T) {
	t.Parallel()
	cfg := fullPiCfg()
	cfg.Provider = ""
	_, err := ResolvePiConfig(cfg, "/proj")
	var me *PiConfigMissingError
	if !errors.As(err, &me) {
		t.Fatalf("expected *PiConfigMissingError; got %T: %v", err, err)
	}
	if len(me.Missing) != 1 || me.Missing[0] != "harnesses.pi.provider" {
		t.Errorf("expected [harnesses.pi.provider]; got %v", me.Missing)
	}
}

func TestResolvePiConfig_MissingModel(t *testing.T) {
	t.Parallel()
	cfg := fullPiCfg()
	cfg.Model = ""
	_, err := ResolvePiConfig(cfg, "/proj")
	var me *PiConfigMissingError
	if !errors.As(err, &me) {
		t.Fatalf("expected *PiConfigMissingError; got %T: %v", err, err)
	}
	if len(me.Missing) != 1 || me.Missing[0] != "harnesses.pi.model" {
		t.Errorf("expected [harnesses.pi.model]; got %v", me.Missing)
	}
}

func TestResolvePiConfig_MissingAPIKeyEnv(t *testing.T) {
	t.Parallel()
	cfg := fullPiCfg()
	cfg.APIKeyEnv = ""
	_, err := ResolvePiConfig(cfg, "/proj")
	var me *PiConfigMissingError
	if !errors.As(err, &me) {
		t.Fatalf("expected *PiConfigMissingError; got %T: %v", err, err)
	}
	if len(me.Missing) != 1 || me.Missing[0] != "harnesses.pi.api_key_env" {
		t.Errorf("expected [harnesses.pi.api_key_env]; got %v", me.Missing)
	}
}

func TestResolvePiConfig_MissingErrorMessage_NamesPathsAndExample(t *testing.T) {
	t.Parallel()
	_, err := ResolvePiConfig(daemon.PiHarnessConfig{}, "/my/project")
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "harmonik pi config --example") {
		t.Errorf("error message should mention 'harmonik pi config --example'; got: %s", msg)
	}
	if !strings.Contains(msg, "harnesses.pi.provider") {
		t.Errorf("error message should name harnesses.pi.provider; got: %s", msg)
	}
	if !strings.Contains(msg, "/my/project") {
		t.Errorf("error message should name the project dir; got: %s", msg)
	}
}

// Model shape tests (PI-052, HC-055a: ^[A-Za-z0-9._:/-]+$, ≤128 chars).

func TestResolvePiConfig_ModelShape_ValidVariants(t *testing.T) {
	t.Parallel()
	validModels := []string{
		"openrouter/qwen/qwen3-coder",
		"anthropic/claude-haiku-4-5-20251001",
		"openai/gpt-4o",
		"model.with.dots",
		"model:with:colons",
		"a",
		strings.Repeat("a", 128), // exactly 128 chars
	}
	for _, m := range validModels {
		m := m
		t.Run(m[:min(len(m), 30)], func(t *testing.T) {
			t.Parallel()
			cfg := fullPiCfg()
			cfg.Model = m
			if _, err := ResolvePiConfig(cfg, "/proj"); err != nil {
				t.Errorf("valid model %q: unexpected error: %v", m, err)
			}
		})
	}
}

func TestResolvePiConfig_ModelShape_InvalidChars(t *testing.T) {
	t.Parallel()
	invalidModels := []string{
		"model with space",
		"model\twith\ttab",
		"model@provider",
		"model#hash",
		"model$var",
		"model!exclaim",
		"model\x00null",
	}
	for _, m := range invalidModels {
		m := m
		t.Run(m, func(t *testing.T) {
			t.Parallel()
			cfg := fullPiCfg()
			cfg.Model = m
			_, err := ResolvePiConfig(cfg, "/proj")
			var pe *PiConfigError
			if !errors.As(err, &pe) {
				t.Errorf("invalid model %q: expected *PiConfigError; got %T: %v", m, err, err)
			}
		})
	}
}

func TestResolvePiConfig_ModelShape_TooLong(t *testing.T) {
	t.Parallel()
	cfg := fullPiCfg()
	cfg.Model = strings.Repeat("a", 129) // 129 chars — over limit
	_, err := ResolvePiConfig(cfg, "/proj")
	var pe *PiConfigError
	if !errors.As(err, &pe) {
		t.Fatalf("expected *PiConfigError; got %T: %v", err, err)
	}
	if !strings.Contains(pe.Reason, "128") {
		t.Errorf("error reason should mention 128-char limit; got: %s", pe.Reason)
	}
}

// Fallback tests.

func TestResolvePiConfig_Fallback_AllFields_OK(t *testing.T) {
	t.Parallel()
	cfg := fullPiCfg()
	cfg.HasFallback = true
	cfg.Fallback = daemon.PiFallbackConfig{
		Provider:  "anthropic",
		Model:     "anthropic/claude-haiku-4-5-20251001",
		APIKeyEnv: "ANTHROPIC_API_KEY",
	}
	if _, err := ResolvePiConfig(cfg, "/proj"); err != nil {
		t.Fatalf("unexpected error with complete fallback: %v", err)
	}
}

func TestResolvePiConfig_Fallback_PartialFields_AggregatesMissing(t *testing.T) {
	t.Parallel()
	cfg := fullPiCfg()
	cfg.HasFallback = true
	// Leave fallback.provider and fallback.api_key_env empty.
	cfg.Fallback = daemon.PiFallbackConfig{
		Model: "anthropic/claude-haiku-4-5-20251001",
	}
	_, err := ResolvePiConfig(cfg, "/proj")
	var me *PiConfigMissingError
	if !errors.As(err, &me) {
		t.Fatalf("expected *PiConfigMissingError; got %T: %v", err, err)
	}
	wantMissing := map[string]bool{
		"harnesses.pi.fallback.provider":    true,
		"harnesses.pi.fallback.api_key_env": true,
	}
	for _, got := range me.Missing {
		if !wantMissing[got] {
			t.Errorf("unexpected missing key %q", got)
		}
		delete(wantMissing, got)
	}
	for want := range wantMissing {
		t.Errorf("expected missing key %q not in error.Missing: %v", want, me.Missing)
	}
}

func TestResolvePiConfig_Fallback_Absent_NoValidation(t *testing.T) {
	t.Parallel()
	cfg := fullPiCfg()
	cfg.HasFallback = false
	// Fallback fields left empty — should not cause an error since HasFallback=false.
	if _, err := ResolvePiConfig(cfg, "/proj"); err != nil {
		t.Fatalf("HasFallback=false with empty fallback fields: unexpected error: %v", err)
	}
}

func TestResolvePiConfig_Fallback_ModelShape_Invalid(t *testing.T) {
	t.Parallel()
	cfg := fullPiCfg()
	cfg.HasFallback = true
	cfg.Fallback = daemon.PiFallbackConfig{
		Provider:  "anthropic",
		Model:     "invalid model with spaces",
		APIKeyEnv: "ANTHROPIC_API_KEY",
	}
	_, err := ResolvePiConfig(cfg, "/proj")
	var pe *PiConfigError
	if !errors.As(err, &pe) {
		t.Fatalf("expected *PiConfigError; got %T: %v", err, err)
	}
	if pe.Field != "harnesses.pi.fallback.model" {
		t.Errorf("expected field harnesses.pi.fallback.model; got %q", pe.Field)
	}
}

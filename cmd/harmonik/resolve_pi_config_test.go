package main

// resolve_pi_config_test.go — unit tests for ResolvePiConfig (hk-v7q5u, PI-051/PI-052;
// api_key_file validation added by hk-xmfoi, PI-040/PI-050).
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
	"os"
	"path/filepath"
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

// ── api_key_file tests (PI-040/PI-050, hk-xmfoi) ──────────────────────────────

// TestResolvePiConfig_APIKeyFile_Unset_OK verifies that an absent (empty) api_key_file
// is accepted without error — the field is optional.
func TestResolvePiConfig_APIKeyFile_Unset_OK(t *testing.T) {
	t.Parallel()
	cfg := fullPiCfg()
	cfg.APIKeyFile = ""
	if _, err := ResolvePiConfig(cfg, "/proj"); err != nil {
		t.Fatalf("absent api_key_file: unexpected error: %v", err)
	}
}

// TestResolvePiConfig_APIKeyFile_SetReadableNonEmpty_OK verifies that a set, readable,
// non-empty file is accepted and the expanded path is stored in the returned config.
func TestResolvePiConfig_APIKeyFile_SetReadableNonEmpty_OK(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	keyFile := filepath.Join(dir, "openrouter.key")
	if err := os.WriteFile(keyFile, []byte("sk-or-test-key-value"), 0o600); err != nil {
		t.Fatalf("setup: write key file: %v", err)
	}
	cfg := fullPiCfg()
	cfg.APIKeyFile = keyFile
	got, err := ResolvePiConfig(cfg, "/proj")
	if err != nil {
		t.Fatalf("readable non-empty file: unexpected error: %v", err)
	}
	if got.APIKeyFile != keyFile {
		t.Errorf("APIKeyFile = %q; want %q (expanded path stored)", got.APIKeyFile, keyFile)
	}
}

// TestResolvePiConfig_APIKeyFile_SetButMissing_FailsLoud verifies that a configured
// api_key_file path that does not exist fails with *PiConfigError (R1 fail-loud).
func TestResolvePiConfig_APIKeyFile_SetButMissing_FailsLoud(t *testing.T) {
	t.Parallel()
	cfg := fullPiCfg()
	cfg.APIKeyFile = "/nonexistent-path-harmonik-test-xmfoi/openrouter.key"
	_, err := ResolvePiConfig(cfg, "/proj")
	var pe *PiConfigError
	if !errors.As(err, &pe) {
		t.Fatalf("expected *PiConfigError for missing file; got %T: %v", err, err)
	}
	if pe.Field != "harnesses.pi.api_key_file" {
		t.Errorf("PiConfigError.Field = %q; want %q", pe.Field, "harnesses.pi.api_key_file")
	}
	if !strings.Contains(pe.Reason, "not readable") {
		t.Errorf("PiConfigError.Reason should mention readable; got: %s", pe.Reason)
	}
}

// TestResolvePiConfig_APIKeyFile_SetButEmpty_FailsLoud verifies that a configured
// api_key_file that exists but is empty (or whitespace-only) fails with *PiConfigError.
func TestResolvePiConfig_APIKeyFile_SetButEmpty_FailsLoud(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	keyFile := filepath.Join(dir, "empty.key")
	if err := os.WriteFile(keyFile, []byte("   \n"), 0o600); err != nil {
		t.Fatalf("setup: write empty key file: %v", err)
	}
	cfg := fullPiCfg()
	cfg.APIKeyFile = keyFile
	_, err := ResolvePiConfig(cfg, "/proj")
	var pe *PiConfigError
	if !errors.As(err, &pe) {
		t.Fatalf("expected *PiConfigError for empty file; got %T: %v", err, err)
	}
	if pe.Field != "harnesses.pi.api_key_file" {
		t.Errorf("PiConfigError.Field = %q; want %q", pe.Field, "harnesses.pi.api_key_file")
	}
	if !strings.Contains(pe.Reason, "empty") {
		t.Errorf("PiConfigError.Reason should mention empty; got: %s", pe.Reason)
	}
}

// TestResolvePiConfig_APIKeyFile_TildeExpanded verifies that a ~ prefix is expanded
// to the user home directory and the expanded path is stored in the returned config.
func TestResolvePiConfig_APIKeyFile_TildeExpanded(t *testing.T) {
	t.Parallel()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home dir:", err)
	}
	// Write a real key file under home so the validation passes.
	keyFile := filepath.Join(home, ".harmonik-test-xmfoi-expand.key")
	if err := os.WriteFile(keyFile, []byte("sk-or-tilde-test"), 0o600); err != nil {
		t.Fatalf("setup: write key file: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(keyFile) })

	cfg := fullPiCfg()
	cfg.APIKeyFile = "~/.harmonik-test-xmfoi-expand.key"
	got, err := ResolvePiConfig(cfg, "/proj")
	if err != nil {
		t.Fatalf("tilde path: unexpected error: %v", err)
	}
	if got.APIKeyFile != keyFile {
		t.Errorf("APIKeyFile = %q; want expanded path %q", got.APIKeyFile, keyFile)
	}
}

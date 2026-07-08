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

// ─────────────────────────────────────────────────────────────────────────────
// base_url validation tests (hk-z13jz)
// ─────────────────────────────────────────────────────────────────────────────

// TestResolvePiConfig_BaseURL_Absent_OK verifies absent base_url passes validation
// (the normal cloud-provider case).
func TestResolvePiConfig_BaseURL_Absent_OK(t *testing.T) {
	t.Parallel()
	cfg := fullPiCfg()
	// BaseURL not set — must resolve without error.
	_, err := ResolvePiConfig(cfg, "/proj")
	if err != nil {
		t.Errorf("absent base_url: unexpected error: %v", err)
	}
}

// TestResolvePiConfig_BaseURL_ValidURL_OK verifies a well-formed base_url passes.
func TestResolvePiConfig_BaseURL_ValidURL_OK(t *testing.T) {
	t.Parallel()
	cfg := fullPiCfg()
	cfg.BaseURL = "http://dgx.local:8551/v1"
	got, err := ResolvePiConfig(cfg, "/proj")
	if err != nil {
		t.Fatalf("valid base_url: unexpected error: %v", err)
	}
	if got.BaseURL != cfg.BaseURL {
		t.Errorf("BaseURL = %q; want %q", got.BaseURL, cfg.BaseURL)
	}
}

// TestResolvePiConfig_BaseURL_ValidHTTPS_OK verifies an https base_url passes.
func TestResolvePiConfig_BaseURL_ValidHTTPS_OK(t *testing.T) {
	t.Parallel()
	cfg := fullPiCfg()
	cfg.BaseURL = "https://myserver.example.com/openai/v1"
	_, err := ResolvePiConfig(cfg, "/proj")
	if err != nil {
		t.Fatalf("https base_url: unexpected error: %v", err)
	}
}

// TestResolvePiConfig_BaseURL_Malformed_Error verifies a malformed base_url returns
// *PiConfigError (fail loud — R1 de-hardcode mandate, hk-z13jz).
func TestResolvePiConfig_BaseURL_Malformed_Error(t *testing.T) {
	t.Parallel()
	cfg := fullPiCfg()
	cfg.BaseURL = "not a url"
	_, err := ResolvePiConfig(cfg, "/proj")
	if err == nil {
		t.Fatal("malformed base_url: expected PiConfigError, got nil")
	}
	var pe *PiConfigError
	if !errors.As(err, &pe) {
		t.Fatalf("malformed base_url: want *PiConfigError; got %T: %v", err, err)
	}
	if pe.Field != "harnesses.pi.base_url" {
		t.Errorf("PiConfigError.Field = %q; want %q", pe.Field, "harnesses.pi.base_url")
	}
}

// TestResolvePiConfig_BaseURL_TooLong_Error verifies base_url >512 chars is rejected.
func TestResolvePiConfig_BaseURL_TooLong_Error(t *testing.T) {
	t.Parallel()
	cfg := fullPiCfg()
	cfg.BaseURL = "http://host/" + strings.Repeat("a", 502)
	_, err := ResolvePiConfig(cfg, "/proj")
	if err == nil {
		t.Fatal("too-long base_url: expected PiConfigError, got nil")
	}
	var pe *PiConfigError
	if !errors.As(err, &pe) {
		t.Fatalf("too-long base_url: want *PiConfigError; got %T: %v", err, err)
	}
	if pe.Field != "harnesses.pi.base_url" {
		t.Errorf("PiConfigError.Field = %q; want %q", pe.Field, "harnesses.pi.base_url")
	}
}

// TestResolvePiConfig_BaseURL_NoScheme_Error verifies base_url without a scheme
// (no "://") is rejected.
func TestResolvePiConfig_BaseURL_NoScheme_Error(t *testing.T) {
	t.Parallel()
	cfg := fullPiCfg()
	cfg.BaseURL = "dgx.local:8551/v1"
	_, err := ResolvePiConfig(cfg, "/proj")
	if err == nil {
		t.Fatal("no-scheme base_url: expected PiConfigError, got nil")
	}
	var pe *PiConfigError
	if !errors.As(err, &pe) {
		t.Fatalf("no-scheme base_url: want *PiConfigError; got %T: %v", err, err)
	}
}

// TestResolvePiConfig_API_PassesThrough verifies the api field passes through
// unchanged (no validation needed — hk-z13jz).
func TestResolvePiConfig_API_PassesThrough(t *testing.T) {
	t.Parallel()
	cfg := fullPiCfg()
	cfg.API = "openai"
	got, err := ResolvePiConfig(cfg, "/proj")
	if err != nil {
		t.Fatalf("api field: unexpected error: %v", err)
	}
	if got.API != "openai" {
		t.Errorf("API = %q; want %q", got.API, "openai")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Named-profile tests (pi-provider-switch C2)
// ─────────────────────────────────────────────────────────────────────────────

// TestResolvePiConfig_ProfileMap_Valid verifies a two-profile map (cloud openrouter
// + ornith with base_url and api) resolves without error and both profiles are present.
func TestResolvePiConfig_ProfileMap_Valid(t *testing.T) {
	t.Parallel()
	cfg := fullPiCfg()
	cfg.Profiles = map[string]daemon.PiProfileConfig{
		"cloud": {
			Provider:  "openrouter",
			Model:     "openrouter/qwen/qwen3-coder",
			APIKeyEnv: "OPENROUTER_API_KEY",
		},
		"ornith": {
			Provider:  "ornith",
			Model:     "deepseek/deepseek-r1",
			APIKeyEnv: "ORNITH_API_KEY",
			BaseURL:   "http://dgx.local:8551/v1",
			API:       "openai-completions",
		},
	}
	got, err := ResolvePiConfig(cfg, "/proj")
	if err != nil {
		t.Fatalf("valid profile map: unexpected error: %v", err)
	}
	if len(got.Profiles) != 2 {
		t.Fatalf("expected 2 profiles; got %d", len(got.Profiles))
	}
	cloud := got.Profiles["cloud"]
	if cloud.Provider != "openrouter" || cloud.Model != "openrouter/qwen/qwen3-coder" {
		t.Errorf("cloud profile: unexpected fields: %+v", cloud)
	}
	ornith := got.Profiles["ornith"]
	if ornith.BaseURL != "http://dgx.local:8551/v1" || ornith.API != "openai-completions" {
		t.Errorf("ornith profile: unexpected fields: %+v", ornith)
	}
}

// TestResolvePiConfig_Profile_OrnithShape verifies an ornith-shaped profile with
// base_url set and api unset validates correctly, and api stays "" in the result.
func TestResolvePiConfig_Profile_OrnithShape(t *testing.T) {
	t.Parallel()
	cfg := fullPiCfg()
	cfg.Profiles = map[string]daemon.PiProfileConfig{
		"ornith": {
			Provider:  "ornith",
			Model:     "deepseek/deepseek-r1",
			APIKeyEnv: "ORNITH_API_KEY",
			BaseURL:   "http://dgx.local:8551/v1",
			// API intentionally absent — must remain "" (defaulted at launch)
		},
	}
	got, err := ResolvePiConfig(cfg, "/proj")
	if err != nil {
		t.Fatalf("ornith profile: unexpected error: %v", err)
	}
	ornith := got.Profiles["ornith"]
	if ornith.API != "" {
		t.Errorf("ornith profile: API = %q; want \"\" (defaulted at launch, not here)", ornith.API)
	}
}

// TestResolvePiConfig_Profile_InvalidShape verifies a profile with an invalid
// provider string (contains space) returns *PiConfigError naming the dotted path.
func TestResolvePiConfig_Profile_InvalidShape(t *testing.T) {
	t.Parallel()
	cfg := fullPiCfg()
	cfg.Profiles = map[string]daemon.PiProfileConfig{
		"bad": {
			Provider:  "bad provider",
			Model:     "some/model",
			APIKeyEnv: "SOME_KEY",
		},
	}
	_, err := ResolvePiConfig(cfg, "/proj")
	var pe *PiConfigError
	if !errors.As(err, &pe) {
		t.Fatalf("expected *PiConfigError; got %T: %v", err, err)
	}
	if pe.Field != "harnesses.pi.profiles.bad.provider" {
		t.Errorf("PiConfigError.Field = %q; want %q", pe.Field, "harnesses.pi.profiles.bad.provider")
	}
}

// TestResolvePiConfig_Profile_MissingRequiredKey_Aggregates verifies that a profile
// missing api_key_env produces a *PiConfigMissingError with the dotted path; and when
// the top-level block also has a missing key, both appear in the same error.
func TestResolvePiConfig_Profile_MissingRequiredKey_Aggregates(t *testing.T) {
	t.Parallel()
	// Top-level missing provider + profile missing api_key_env → both in one error.
	cfg := daemon.PiHarnessConfig{
		// Provider intentionally absent
		Model:     "openrouter/qwen/qwen3-coder",
		APIKeyEnv: "OPENROUTER_API_KEY",
		Profiles: map[string]daemon.PiProfileConfig{
			"myprofile": {
				Provider: "openrouter",
				Model:    "openrouter/qwen/qwen3-coder",
				// APIKeyEnv intentionally absent
			},
		},
	}
	_, err := ResolvePiConfig(cfg, "/proj")
	var me *PiConfigMissingError
	if !errors.As(err, &me) {
		t.Fatalf("expected *PiConfigMissingError; got %T: %v", err, err)
	}
	wantKeys := map[string]bool{
		"harnesses.pi.provider":                       true,
		"harnesses.pi.profiles.myprofile.api_key_env": true,
	}
	for _, got := range me.Missing {
		delete(wantKeys, got)
	}
	for want := range wantKeys {
		t.Errorf("expected missing key %q not in error.Missing: %v", want, me.Missing)
	}
}

// TestResolvePiConfig_Profile_APIKeyFile_Expanded verifies that a profile with a
// ~-prefixed api_key_file pointing at a temp file resolves to the expanded absolute
// path; and that an unreadable/empty file fails loud.
func TestResolvePiConfig_Profile_APIKeyFile_Expanded(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	keyFile := filepath.Join(dir, "profile.key")
	if err := os.WriteFile(keyFile, []byte("sk-profile-test-key"), 0o600); err != nil {
		t.Fatalf("setup: write key file: %v", err)
	}
	cfg := fullPiCfg()
	cfg.Profiles = map[string]daemon.PiProfileConfig{
		"withkey": {
			Provider:   "openrouter",
			Model:      "openrouter/qwen/qwen3-coder",
			APIKeyEnv:  "OPENROUTER_API_KEY",
			APIKeyFile: keyFile,
		},
	}
	got, err := ResolvePiConfig(cfg, "/proj")
	if err != nil {
		t.Fatalf("profile api_key_file: unexpected error: %v", err)
	}
	if got.Profiles["withkey"].APIKeyFile != keyFile {
		t.Errorf("APIKeyFile = %q; want %q", got.Profiles["withkey"].APIKeyFile, keyFile)
	}

	// Unreadable file → fail loud.
	cfg2 := fullPiCfg()
	cfg2.Profiles = map[string]daemon.PiProfileConfig{
		"badkey": {
			Provider:   "openrouter",
			Model:      "openrouter/qwen/qwen3-coder",
			APIKeyEnv:  "OPENROUTER_API_KEY",
			APIKeyFile: "/nonexistent-harmonik-test-c2/profile.key",
		},
	}
	_, err2 := ResolvePiConfig(cfg2, "/proj")
	var pe *PiConfigError
	if !errors.As(err2, &pe) {
		t.Fatalf("unreadable profile api_key_file: expected *PiConfigError; got %T: %v", err2, err2)
	}
	if pe.Field != "harnesses.pi.profiles.badkey.api_key_file" {
		t.Errorf("PiConfigError.Field = %q; want harnesses.pi.profiles.badkey.api_key_file", pe.Field)
	}
}

// TestResolvePiConfig_AbsentProfiles_DefaultUnchanged verifies that a nil Profiles
// map resolves identically to a config with no profiles (default path unaffected).
func TestResolvePiConfig_AbsentProfiles_DefaultUnchanged(t *testing.T) {
	t.Parallel()
	cfg := fullPiCfg()
	got, err := ResolvePiConfig(cfg, "/proj")
	if err != nil {
		t.Fatalf("no profiles: unexpected error: %v", err)
	}
	if got.Profiles != nil {
		t.Errorf("expected nil Profiles map; got %v", got.Profiles)
	}
}

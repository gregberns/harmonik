package supervisecmd

// Regression tests for resolveAPIKey: supervise start must resolve ANTHROPIC_API_KEY
// from the Pi-scoped non-committed source and persist it into config.json so the
// shim can inject it into Pi's env on a fresh boot.
//
// Spec ref: specs/credential-isolation.md §4.4 CI-006.

import (
	"os"
	"path/filepath"
	"testing"
)

// unsetenvWithRestore calls os.Unsetenv and registers a t.Cleanup that restores
// the prior value (or re-unsets if absent), preventing env contamination across
// tests regardless of execution order.
func unsetenvWithRestore(t *testing.T, key string) {
	t.Helper()
	prior, had := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("unsetenv %s: %v", key, err)
	}
	t.Cleanup(func() {
		if had {
			_ = os.Setenv(key, prior)
		} else {
			_ = os.Unsetenv(key)
		}
	})
}

// TestResolveAPIKey_EnvVar verifies that resolveAPIKey returns the value of
// ANTHROPIC_API_KEY from the ambient environment when it is set.
func TestResolveAPIKey_EnvVar(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-env-var-key")

	dir := t.TempDir()
	got, err := resolveAPIKey(dir, false)
	if err != nil {
		t.Fatalf("resolveAPIKey from env: unexpected error: %v", err)
	}
	if got != "sk-ant-env-var-key" {
		t.Errorf("resolveAPIKey from env: got %q, want %q", got, "sk-ant-env-var-key")
	}
}

// TestResolveAPIKey_DotEnvFile verifies that resolveAPIKey falls back to reading
// ANTHROPIC_API_KEY from a .env file at the project root when the env var is absent.
func TestResolveAPIKey_DotEnvFile(t *testing.T) {
	unsetenvWithRestore(t, "ANTHROPIC_API_KEY")

	dir := t.TempDir()
	envContent := "# comment line\nANTHROPIC_API_KEY=sk-ant-dotenv-key\nOTHER_VAR=irrelevant\n"
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(envContent), 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	got, err := resolveAPIKey(dir, false)
	if err != nil {
		t.Fatalf("resolveAPIKey from .env: unexpected error: %v", err)
	}
	if got != "sk-ant-dotenv-key" {
		t.Errorf("resolveAPIKey from .env: got %q, want %q", got, "sk-ant-dotenv-key")
	}
}

// TestResolveAPIKey_EnvVarTakesPrecedenceOverDotEnv verifies env var wins when both
// ANTHROPIC_API_KEY env var and .env file are present.
func TestResolveAPIKey_EnvVarTakesPrecedenceOverDotEnv(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-from-env")

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("ANTHROPIC_API_KEY=sk-ant-from-file\n"), 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	got, err := resolveAPIKey(dir, false)
	if err != nil {
		t.Fatalf("resolveAPIKey precedence: unexpected error: %v", err)
	}
	if got != "sk-ant-from-env" {
		t.Errorf("resolveAPIKey precedence: got %q, want env-var value %q", got, "sk-ant-from-env")
	}
}

// TestResolveAPIKey_EmptyWhenNeitherPresent verifies that resolveAPIKey returns ""
// without error when neither the env var nor a .env file is present and require=false.
// OAuth-authed deployments that don't pass --require-api-key must not be broken.
func TestResolveAPIKey_EmptyWhenNeitherPresent(t *testing.T) {
	unsetenvWithRestore(t, "ANTHROPIC_API_KEY")

	dir := t.TempDir() // no .env file
	got, err := resolveAPIKey(dir, false)
	if err != nil {
		t.Fatalf("resolveAPIKey no source, require=false: unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("resolveAPIKey no source: got %q, want empty string", got)
	}
}

// TestResolveAPIKey_RequireAPIKeyErrorWhenNeitherPresent verifies the CI-006
// fail-closed contract: when require=true and no source resolves, resolveAPIKey
// must return a non-nil error so that RunStart exits 1 instead of booting Pi
// with an empty key (hk-0ziuw).
func TestResolveAPIKey_RequireAPIKeyErrorWhenNeitherPresent(t *testing.T) {
	unsetenvWithRestore(t, "ANTHROPIC_API_KEY")

	dir := t.TempDir() // no .env file
	got, err := resolveAPIKey(dir, true)
	if err == nil {
		t.Fatalf("resolveAPIKey no source, require=true: expected error, got nil (key=%q)", got)
	}
	if got != "" {
		t.Errorf("resolveAPIKey error path: got non-empty key %q, want empty", got)
	}
}

// TestResolveAPIKey_RequireAPIKeySucceedsWithEnvVar verifies that --require-api-key
// does not block resolution when the env var is set.
func TestResolveAPIKey_RequireAPIKeySucceedsWithEnvVar(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-required-env")

	dir := t.TempDir()
	got, err := resolveAPIKey(dir, true)
	if err != nil {
		t.Fatalf("resolveAPIKey env var, require=true: unexpected error: %v", err)
	}
	if got != "sk-ant-required-env" {
		t.Errorf("resolveAPIKey require=true env var: got %q, want %q", got, "sk-ant-required-env")
	}
}

// TestResolveAPIKey_RequireAPIKeySucceedsWithDotEnv verifies that --require-api-key
// does not block resolution when the .env file supplies the key.
func TestResolveAPIKey_RequireAPIKeySucceedsWithDotEnv(t *testing.T) {
	unsetenvWithRestore(t, "ANTHROPIC_API_KEY")

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("ANTHROPIC_API_KEY=sk-ant-required-dotenv\n"), 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	got, err := resolveAPIKey(dir, true)
	if err != nil {
		t.Fatalf("resolveAPIKey .env, require=true: unexpected error: %v", err)
	}
	if got != "sk-ant-required-dotenv" {
		t.Errorf("resolveAPIKey require=true .env: got %q, want %q", got, "sk-ant-required-dotenv")
	}
}

// TestResolveAPIKey_DotEnvSkipsCommentAndBlankLines verifies that the .env parser
// correctly ignores comment lines and blank lines and still finds the key.
func TestResolveAPIKey_DotEnvSkipsCommentAndBlankLines(t *testing.T) {
	unsetenvWithRestore(t, "ANTHROPIC_API_KEY")

	dir := t.TempDir()
	envContent := "\n# top comment\n\nFOO=bar\n\n# another comment\nANTHROPIC_API_KEY=sk-ant-parse-key\n"
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(envContent), 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	got, err := resolveAPIKey(dir, false)
	if err != nil {
		t.Fatalf("resolveAPIKey .env parse: unexpected error: %v", err)
	}
	if got != "sk-ant-parse-key" {
		t.Errorf("resolveAPIKey .env parse: got %q, want %q", got, "sk-ant-parse-key")
	}
}

// TestWriteConfigAtomic_APIKeyRoundTrip verifies that Config.APIKey survives a
// WriteConfigAtomic → ReadConfig round-trip, confirming the field is persisted.
// This is the end-to-end CI-006 check: key resolved at start time, stored in
// config.json, available to the shim at exec time.
func TestWriteConfigAtomic_APIKeyRoundTrip(t *testing.T) {
	dir := t.TempDir()

	cfg := Config{
		SchemaVersion: 1,
		Command:       []string{"claude", "--pi"},
		APIKey:        "sk-ant-config-roundtrip",
	}
	if err := WriteConfigAtomic(dir, cfg); err != nil {
		t.Fatalf("WriteConfigAtomic: %v", err)
	}

	got, err := ReadConfig(dir)
	if err != nil {
		t.Fatalf("ReadConfig: %v", err)
	}
	if got.APIKey != cfg.APIKey {
		t.Errorf("APIKey round-trip: got %q, want %q", got.APIKey, cfg.APIKey)
	}
}

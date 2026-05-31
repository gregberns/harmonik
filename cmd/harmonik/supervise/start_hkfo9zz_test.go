package supervisecmd

// Regression tests for hk-fo9zz: supervise start must resolve ANTHROPIC_API_KEY
// from the Pi-scoped non-committed source and persist it into config.json so the
// shim can inject it into Pi's env on a fresh boot.
//
// Spec ref: specs/credential-isolation.md §4.4 CI-006.

import (
	"os"
	"path/filepath"
	"testing"
)

// TestResolveAPIKey_EnvVar verifies that resolveAPIKey returns the value of
// ANTHROPIC_API_KEY from the ambient environment when it is set.
func TestResolveAPIKey_EnvVar(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-env-var-key")

	dir := t.TempDir()
	got := resolveAPIKey(dir)
	if got != "sk-ant-env-var-key" {
		t.Errorf("resolveAPIKey from env: got %q, want %q", got, "sk-ant-env-var-key")
	}
}

// TestResolveAPIKey_DotEnvFile verifies that resolveAPIKey falls back to reading
// ANTHROPIC_API_KEY from a .env file at the project root when the env var is absent.
func TestResolveAPIKey_DotEnvFile(t *testing.T) {
	if err := os.Unsetenv("ANTHROPIC_API_KEY"); err != nil {
		t.Fatalf("unsetenv: %v", err)
	}

	dir := t.TempDir()
	envContent := "# comment line\nANTHROPIC_API_KEY=sk-ant-dotenv-key\nOTHER_VAR=irrelevant\n"
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(envContent), 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	got := resolveAPIKey(dir)
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

	got := resolveAPIKey(dir)
	if got != "sk-ant-from-env" {
		t.Errorf("resolveAPIKey precedence: got %q, want env-var value %q", got, "sk-ant-from-env")
	}
}

// TestResolveAPIKey_EmptyWhenNeitherPresent verifies that resolveAPIKey returns ""
// when neither the env var nor a .env file is present — Pi may use OAuth.
func TestResolveAPIKey_EmptyWhenNeitherPresent(t *testing.T) {
	if err := os.Unsetenv("ANTHROPIC_API_KEY"); err != nil {
		t.Fatalf("unsetenv: %v", err)
	}

	dir := t.TempDir() // no .env file
	got := resolveAPIKey(dir)
	if got != "" {
		t.Errorf("resolveAPIKey no source: got %q, want empty string", got)
	}
}

// TestResolveAPIKey_DotEnvSkipsCommentAndBlankLines verifies that the .env parser
// correctly ignores comment lines and blank lines and still finds the key.
func TestResolveAPIKey_DotEnvSkipsCommentAndBlankLines(t *testing.T) {
	if err := os.Unsetenv("ANTHROPIC_API_KEY"); err != nil {
		t.Fatalf("unsetenv: %v", err)
	}

	dir := t.TempDir()
	envContent := "\n# top comment\n\nFOO=bar\n\n# another comment\nANTHROPIC_API_KEY=sk-ant-parse-key\n"
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(envContent), 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	got := resolveAPIKey(dir)
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

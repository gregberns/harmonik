package supervisecmd

// CI-005 regression tests: buildPiEnv scoped-injection builder.
//
// Verifies that:
//  1. Credential deny-list keys (ANTHROPIC_API_KEY, ANTHROPIC_AUTH_TOKEN,
//     CLAUDE_CODE_OAUTH*) are stripped from the ambient env.
//  2. The scoped apiKey is injected as ANTHROPIC_API_KEY when non-empty.
//  3. Non-deny-list keys pass through unchanged.
//  4. When apiKey is empty, no ANTHROPIC_API_KEY entry appears in the result.
//
// Spec: specs/credential-isolation.md §4.3 CI-005.

import (
	"os"
	"strings"
	"testing"
)

func TestBuildPiEnv_StripsCredentialDenyListKeys(t *testing.T) {
	denyKeys := []string{
		"ANTHROPIC_API_KEY",
		"ANTHROPIC_AUTH_TOKEN",
		"CLAUDE_CODE_OAUTH_TOKEN",
		"CLAUDE_CODE_OAUTH_EXTRA",
	}
	for _, k := range denyKeys {
		t.Setenv(k, "should-be-stripped")
	}
	t.Setenv("HARMONIK_TEST_PASSTHROUGH", "keep-me")

	env := buildPiEnv("")

	envMap := toEnvMap(env)
	for _, k := range denyKeys {
		if _, present := envMap[k]; present {
			t.Errorf("deny-list key %q must be absent from Pi env; got %q", k, envMap[k])
		}
	}
	// Passthrough key must survive.
	if v, ok := envMap["HARMONIK_TEST_PASSTHROUGH"]; !ok || v != "keep-me" {
		t.Errorf("HARMONIK_TEST_PASSTHROUGH = %q ok=%v; want %q", v, ok, "keep-me")
	}
}

func TestBuildPiEnv_InjectsAPIKeyWhenNonEmpty(t *testing.T) {
	// Ensure no ambient ANTHROPIC_API_KEY leaks in.
	if err := os.Unsetenv("ANTHROPIC_API_KEY"); err != nil {
		t.Fatalf("unsetenv: %v", err)
	}

	env := buildPiEnv("sk-ant-pi-scoped-key")

	envMap := toEnvMap(env)
	if got, ok := envMap["ANTHROPIC_API_KEY"]; !ok || got != "sk-ant-pi-scoped-key" {
		t.Errorf("ANTHROPIC_API_KEY = %q ok=%v; want %q", got, ok, "sk-ant-pi-scoped-key")
	}
}

func TestBuildPiEnv_NoKeyWhenEmpty(t *testing.T) {
	if err := os.Unsetenv("ANTHROPIC_API_KEY"); err != nil {
		t.Fatalf("unsetenv: %v", err)
	}

	env := buildPiEnv("")

	for _, kv := range env {
		if strings.HasPrefix(kv, "ANTHROPIC_API_KEY=") {
			t.Errorf("unexpected ANTHROPIC_API_KEY entry in Pi env: %q", kv)
		}
	}
}

func TestBuildPiEnv_ScopedKeyOverridesAmbient(t *testing.T) {
	// Ambient key must be stripped; scoped key must win.
	t.Setenv("ANTHROPIC_API_KEY", "ambient-key-must-not-pass")

	env := buildPiEnv("scoped-key")

	envMap := toEnvMap(env)
	if got, ok := envMap["ANTHROPIC_API_KEY"]; !ok || got != "scoped-key" {
		t.Errorf("ANTHROPIC_API_KEY = %q ok=%v; want %q", got, ok, "scoped-key")
	}
	// Ensure no duplicate entries.
	count := 0
	for _, kv := range env {
		if strings.HasPrefix(kv, "ANTHROPIC_API_KEY=") {
			count++
		}
	}
	if count != 1 {
		t.Errorf("ANTHROPIC_API_KEY appears %d times; want exactly 1", count)
	}
}

// toEnvMap converts a "KEY=VALUE" slice to a map. Last value wins on duplicate.
func toEnvMap(env []string) map[string]string {
	m := make(map[string]string, len(env))
	for _, kv := range env {
		idx := strings.IndexByte(kv, '=')
		if idx < 0 {
			m[kv] = ""
			continue
		}
		m[kv[:idx]] = kv[idx+1:]
	}
	return m
}

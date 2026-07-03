package daemon_test

// projectconfig_hksv3vg_test.go — unit tests for ~ expansion in api_key_file
// at the daemon config parse layer (hk-sv3vg).
//
// Verifies:
//   - api_key_file with a ~/... prefix is expanded to the absolute home path.
//   - api_key_file without ~ is left unchanged.
//   - absent api_key_file stays empty.
//
// Spec refs: PI-050. Bead ref: hk-sv3vg.

import (
	"os"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/daemon"
)

func TestHarnessesPiAPIKeyFile_TildeExpanded(t *testing.T) {
	t.Parallel()

	root := projCfgFixtureDir(t, `
schema_version: 1
harnesses:
  pi:
    provider: openrouter
    model: openrouter/qwen/qwen3-coder
    api_key_env: OPENROUTER_API_KEY
    api_key_file: ~/.config/harmonik/openrouter.key
`)
	cfg, err := daemon.ExportedLoadProjectConfig(root)
	if err != nil {
		t.Fatalf("LoadProjectConfig: unexpected error: %v", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("os.UserHomeDir: %v", err)
	}
	want := home + "/.config/harmonik/openrouter.key"
	got := cfg.Harnesses.Pi.APIKeyFile
	if got != want {
		t.Errorf("api_key_file tilde expansion: got %q; want %q", got, want)
	}
	if strings.HasPrefix(got, "~") {
		t.Errorf("api_key_file still contains literal ~: %q", got)
	}
}

func TestHarnessesPiAPIKeyFile_AbsolutePathUnchanged(t *testing.T) {
	t.Parallel()

	root := projCfgFixtureDir(t, `
schema_version: 1
harnesses:
  pi:
    provider: openrouter
    model: openrouter/qwen/qwen3-coder
    api_key_env: OPENROUTER_API_KEY
    api_key_file: /etc/secrets/openrouter.key
`)
	cfg, err := daemon.ExportedLoadProjectConfig(root)
	if err != nil {
		t.Fatalf("LoadProjectConfig: unexpected error: %v", err)
	}
	want := "/etc/secrets/openrouter.key"
	got := cfg.Harnesses.Pi.APIKeyFile
	if got != want {
		t.Errorf("absolute api_key_file: got %q; want %q", got, want)
	}
}

func TestHarnessesPiAPIKeyFile_Absent_Empty(t *testing.T) {
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
	if cfg.Harnesses.Pi.APIKeyFile != "" {
		t.Errorf("absent api_key_file: got %q; want empty", cfg.Harnesses.Pi.APIKeyFile)
	}
}

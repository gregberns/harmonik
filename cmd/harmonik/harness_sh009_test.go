package main

// harness_sh009_test.go — contract tests for SH-009 twin-search-path
// precedence resolution.
//
// SH-009: The twin-search-path source is, in precedence order:
//
//	(i)  the harness CLI flag --twin-search-path <dir>
//	(ii) the environment variable HARMONIK_TWIN_SEARCH_PATH
//	(iii) the in-tree default <repo-root>/twins/
//
// Helper prefix: sh009 (per implementer-protocol.md §Helper-prefix discipline).
// Spec ref: specs/scenario-harness.md §4.3 SH-009.
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

import (
	"path/filepath"
	"testing"
)

// TestResolveTwinSearchPaths_FlagTakesPrecedence verifies that a non-empty CLI
// flag value takes precedence over both the env var and the default (SH-009 (i)).
func TestResolveTwinSearchPaths_FlagTakesPrecedence(t *testing.T) {
	t.Parallel()

	got := resolveTwinSearchPaths("/from/flag", "/from/env", "/the/cwd")

	if len(got) != 1 || got[0] != "/from/flag" {
		t.Errorf("SH-009(i): flag precedence: want [/from/flag], got %v", got)
	}
}

// TestResolveTwinSearchPaths_EnvFallback verifies that when the CLI flag is
// empty the HARMONIK_TWIN_SEARCH_PATH environment variable is used (SH-009 (ii)).
func TestResolveTwinSearchPaths_EnvFallback(t *testing.T) {
	t.Parallel()

	got := resolveTwinSearchPaths("", "/from/env", "/the/cwd")

	if len(got) != 1 || got[0] != "/from/env" {
		t.Errorf("SH-009(ii): env fallback: want [/from/env], got %v", got)
	}
}

// TestResolveTwinSearchPaths_DefaultTwinsDir verifies that when both the CLI
// flag and the environment variable are absent the default is <cwd>/twins/
// (SH-009 (iii)).
func TestResolveTwinSearchPaths_DefaultTwinsDir(t *testing.T) {
	t.Parallel()

	cwd := "/the/repo/root"
	got := resolveTwinSearchPaths("", "", cwd)

	want := filepath.Join(cwd, "twins")
	if len(got) != 1 || got[0] != want {
		t.Errorf("SH-009(iii): default dir: want [%s], got %v", want, got)
	}
}

// TestResolveTwinSearchPaths_FlagOverridesNonEmptyEnv verifies that a non-empty
// CLI flag overrides a non-empty env var (SH-009 (i) > (ii) strict ordering).
func TestResolveTwinSearchPaths_FlagOverridesNonEmptyEnv(t *testing.T) {
	t.Parallel()

	got := resolveTwinSearchPaths("/flag/path", "/env/path", "/cwd")

	if len(got) != 1 || got[0] != "/flag/path" {
		t.Errorf("flag must override non-empty env: want [/flag/path], got %v", got)
	}
}

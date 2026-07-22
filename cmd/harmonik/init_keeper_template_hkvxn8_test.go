package main

// init_keeper_template_hkvxn8_test.go — verifies the `harmonik init` config.yaml
// keeper template.
//
// Contract (operator-required-config change; supersedes the original hk-vxn8
// commented-template contract):
//   - The generated config.yaml emits schema_version: 1 UNCOMMENTED and a COMPLETE,
//     UNCOMMENTED keeper: block (the same source of truth as
//     `harmonik keeper config --example`). harmonik imposes NO built-in keeper
//     defaults at runtime, so a new project must ship with every required value set.
//   - Parsing the generated file yields a COMPLETE daemon.KeeperConfig, and
//     ResolveKeeperConfig on it returns ZERO missing-value errors — a new project
//     starts cleanly.
//
// The artifact under test is exactly what writeConfigYAML emits — obtained by
// CALLING writeConfigYAML, not by re-composing its parts (see renderedInitConfig).
//
// It also guards the init-template-drift fix (⑤): the generated config must set
// sentinel.liveness_no_progress_n uncommented (else the daemon refuses to boot —
// GovernorConfig fails loud, hk-drygf), must fold in the harnesses.pi block so
// the Pi harness is dispatchable out of the box, and must fold in the codex: block
// so codex.stale_wal_max_bytes — REQUIRED with no compiled default — is set before
// the first codex launch (hk-yhvrh; the round-trip through the real guard lives in
// init_codex_block_hkyhvrh_test.go).

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/digest"
)

// renderedInitConfig returns the exact config.yaml body `harmonik init` writes, by
// running the REAL writeConfigYAML into a temp project and reading the file back.
//
// It deliberately does NOT re-compose `configYAMLContent + …ExampleYAML()` itself
// (as it once did): a helper that rebuilds the composition stays green even when a
// block is dropped from writeConfigYAML, so it pins the helper rather than the
// production call site. Going through writeConfigYAML makes every assertion below
// sensitive to the wiring an operator actually gets (hk-yhvrh).
func renderedInitConfig(t *testing.T) string {
	t.Helper()
	projectRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectRoot, ".harmonik"), 0o750); err != nil {
		t.Fatalf("mkdir .harmonik: %v", err)
	}
	if rc := writeConfigYAML(projectRoot, "main", "hk", false, io.Discard, io.Discard); rc != 0 {
		t.Fatalf("writeConfigYAML returned %d, want 0", rc)
	}
	raw, err := os.ReadFile(filepath.Join(projectRoot, ".harmonik", "config.yaml")) //nolint:gosec // G304: path built from this test's own t.TempDir()
	if err != nil {
		t.Fatalf("read generated config.yaml: %v", err)
	}
	return string(raw)
}

// writeRenderedInitConfig writes the rendered body into <tmp>/.harmonik/config.yaml
// and returns the project root.
func writeRenderedInitConfig(t *testing.T, body string) string {
	t.Helper()
	repoRoot := t.TempDir()
	dir := filepath.Join(repoRoot, ".harmonik")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir .harmonik: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(body), 0o644); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}
	return repoRoot
}

// TestInitKeeperTemplate_SchemaAndKeeperBlockUncommented asserts schema_version: 1
// is uncommented AND the keeper block (with its required tunables) is uncommented.
func TestInitKeeperTemplate_SchemaAndKeeperBlockUncommented(t *testing.T) {
	body := renderedInitConfig(t)

	if !lineUncommented(body, "schema_version: 1") {
		t.Fatalf("expected uncommented `schema_version: 1` line; template:\n%s", body)
	}

	// The required keeper tunables are now UNCOMMENTED (real config, not a template).
	// sentinel.liveness_no_progress_n and the harnesses.pi block join them: the
	// former is boot-required (⑤); the latter makes Pi dispatchable out of the box.
	mustBeUncommented := []string{
		"keeper:",
		"warn_abs_tokens: 200000",
		"act_abs_tokens: 215000",
		"warn_pct_ceil: 0.70",
		"act_pct_ceil: 0.85",
		"mode: alarm",
		"poll_interval: 5s",
		"hold_ttl: 45m",
		"max_handoff_timeouts: 3",
		"liveness_no_progress_n: 10",
		"harnesses:",
		"provider: openrouter",
		"model: openrouter/qwen/qwen3-coder",
		"api_key_env: OPENROUTER_API_KEY",
		// hk-yhvrh: the codex: block — REQUIRED with no compiled default, so a fresh
		// project whose first run selects codex fails loud at spec-build without it.
		"codex:",
		"stale_wal_max_bytes: 1048576",
	}
	for _, key := range mustBeUncommented {
		if !lineUncommented(body, key) {
			t.Errorf("keeper key %q must be UNCOMMENTED in the generated config (no runtime defaults), but it is not", key)
		}
	}
}

// TestInitKeeperTemplate_ResolvesCleanly asserts the generated config parses AND
// resolves with ZERO missing-value errors — a new project starts cleanly.
func TestInitKeeperTemplate_ResolvesCleanly(t *testing.T) {
	body := renderedInitConfig(t)
	repoRoot := writeRenderedInitConfig(t, body)

	cfg, err := daemon.LoadProjectConfig(repoRoot)
	if err != nil {
		t.Fatalf("LoadProjectConfig on the generated config must succeed, got: %v", err)
	}

	// Daemon block sanity (the file loaded past the version gate).
	if cfg.Daemon.MaxConcurrent != 4 {
		t.Fatalf("expected daemon.max_concurrent=4 from template, got %d", cfg.Daemon.MaxConcurrent)
	}

	// The keeper block must resolve with NO missing-value errors.
	_, rerr := ResolveKeeperConfig(KeeperFlags{}, cfg.Keeper, repoRoot)
	if rerr != nil {
		var kme *KeeperConfigMissingError
		if errors.As(rerr, &kme) {
			t.Fatalf("the generated keeper block is INCOMPLETE — still missing: %v", kme.Missing)
		}
		t.Fatalf("resolving the generated keeper block failed: %v", rerr)
	}

	// Sanity: the suggested band reached the parsed config.
	if cfg.Keeper.WarnAbsTokens != 200000 {
		t.Errorf("expected warn_abs_tokens=200000 from the generated config, got %d", cfg.Keeper.WarnAbsTokens)
	}

	// ⑤ boot-critical guarantee 1: the sentinel governor config resolves without
	// the fail-loud ErrMissingLivenessNoProgressN. This is the exact check
	// daemon.Start runs at boot (seedGovernorDeps → GovernorConfig); before the
	// init-template fix it returned that error and the daemon refused to start.
	sentinelCfg, serr := digest.LoadSentinelConfig(repoRoot)
	if serr != nil {
		t.Fatalf("LoadSentinelConfig on the generated config must succeed, got: %v", serr)
	}
	if _, gerr := sentinelCfg.GovernorConfig(); gerr != nil {
		t.Fatalf("the generated sentinel block must yield a bootable governor config, got: %v", gerr)
	}

	// ⑤ boot-critical guarantee 2: the harnesses.pi block resolves — provider,
	// model, and api_key_env are all present, so the Pi harness is dispatchable
	// out of the box (the operator tunes the suggested values).
	if _, perr := ResolvePiConfig(cfg.Harnesses.Pi, repoRoot); perr != nil {
		t.Fatalf("the generated harnesses.pi block must resolve, got: %v", perr)
	}
}

// lineUncommented reports whether the body has a non-comment line whose trimmed
// text contains needle.
func lineUncommented(body, needle string) bool {
	for _, ln := range strings.Split(body, "\n") {
		t := strings.TrimSpace(ln)
		if strings.HasPrefix(t, "#") {
			continue
		}
		if strings.Contains(t, needle) {
			return true
		}
	}
	return false
}

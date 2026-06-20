package main

// init_keeper_template_hkvxn8_test.go — verifies the `harmonik init` config.yaml
// keeper template (hk-vxn8).
//
// Contract (hk-vxn8):
//   - The generated config.yaml emits schema_version: 1 UNCOMMENTED and a FULLY
//     COMMENTED keeper: block. Because every keeper line is commented, parsing
//     the file yields an ALL-DEFAULT (zero) daemon.KeeperConfig — a third party
//     does NOT inherit this operator's locked 200k/215k/240k band.
//   - Uncommenting a SINGLE keeper line applies exactly that one override and
//     does NOT trip ErrUnsupportedConfigVersion (schema_version: 1 is present).
//
// The template is the exact const `harmonik init` writes (configYAMLContent),
// rendered the same way writeConfigYAML renders it, so this test pins the real
// emitted artifact without needing br/harmonik/git on PATH.
//
// Bead ref: hk-vxn8.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/daemon"
)

// renderInitConfig writes the rendered configYAMLContent template (the artifact
// `harmonik init` emits) into <repoRoot>/.harmonik/config.yaml and returns the
// repoRoot so daemon.LoadProjectConfig can read it back.
func renderInitConfig(t *testing.T, body string) string {
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

// TestInitKeeperTemplate_SchemaUncommented_RestCommented asserts the emitted
// template has schema_version: 1 uncommented and every keeper line commented.
func TestInitKeeperTemplate_SchemaUncommented_RestCommented(t *testing.T) {
	body := fmt.Sprintf(configYAMLContent, "main")

	// schema_version: 1 is UNCOMMENTED (a real top-level line, not a comment).
	if !lineUncommented(body, "schema_version: 1") {
		t.Fatalf("expected uncommented `schema_version: 1` line; template:\n%s", body)
	}

	// Every keeper tunable line is COMMENTED. These needles are distinctive to
	// the keeper block (the annotated `key: value` forms) so they cannot collide
	// with uncommented daemon-block keys (e.g. workflow_mode).
	mustBeCommented := []string{
		"warn_abs_tokens: 200000",
		"act_abs_tokens: 215000",
		"force_act_abs_tokens: 240000",
		"force_act_abs_offset: 25000",
		"idle_floor_abs_tokens: 150000",
		"warn_pct_ceil: 0.70",
		"act_pct_ceil: 0.85",
		"abs_tokens: 280000",
		"poll_interval: 5s",
		"idle_quiesce: 8s",
		"handoff_timeout: 3m",
		"clear_settle: 3s",
		"boot_grace: 5m",
		"max_boot_grace_total: 10m",
		"warn_cooldown: 30s",
		"no_gauge_backoff: 30s",
		"respawn_grace: 20s",
		"respawn_cooldown: 90s",
		"live_recover_grace: 5m",
		"live_recover_cooldown: 5m",
		"force_retry_interval: 2m",
		"idle_restart_cooldown: 30m",
		"hard_ceiling_cooldown: 5m",
		"blind_keeper_threshold: 5m",
		"heartbeat_max_misses: 12",
		"max_handoff_timeouts: 3",
		"grace_seconds: 30",
		"instruct_only_when_idle: true",
		"crews_enabled: true",
		"default_warn_text:",
		"on_demand_warn_text:",
		"actionable_warn_text:",
	}
	for _, key := range mustBeCommented {
		if lineUncommented(body, key) {
			t.Errorf("keeper tunable %q must be COMMENTED in the emitted template, found uncommented", key)
		}
	}

	// crews_enabled documents default: true (operator decision).
	if !strings.Contains(body, "crews_enabled: true") || !strings.Contains(body, "default: true") {
		t.Errorf("expected crews_enabled annotated `default: true`; template:\n%s", body)
	}
	// hard_ceiling.mode documents default: alarm + the enum.
	if !strings.Contains(body, "mode: alarm") || !strings.Contains(body, "off|alarm|restart") {
		t.Errorf("expected hard_ceiling mode annotated `default: alarm  # off|alarm|restart`")
	}
}

// TestInitKeeperTemplate_AllDefaultWhenCommented asserts that parsing the
// as-emitted config (everything commented) yields an ALL-DEFAULT KeeperConfig:
// the keeper block is absent / all-zero so a third party inherits the compiled
// defaults, NOT this operator's band.
func TestInitKeeperTemplate_AllDefaultWhenCommented(t *testing.T) {
	body := fmt.Sprintf(configYAMLContent, "main")
	repoRoot := renderInitConfig(t, body)

	cfg, err := daemon.LoadProjectConfig(repoRoot)
	if err != nil {
		t.Fatalf("LoadProjectConfig on the as-emitted template must succeed (no version error), got: %v", err)
	}

	// Daemon block IS present and parsed (sanity: the file did load past the
	// version gate).
	if cfg.Daemon.MaxConcurrent != 4 {
		t.Fatalf("expected daemon.max_concurrent=4 from template, got %d", cfg.Daemon.MaxConcurrent)
	}

	// Keeper block is all-zero (every line commented → nothing configured).
	if want := (daemon.KeeperConfig{}); cfg.Keeper != want {
		t.Fatalf("expected ALL-DEFAULT (zero) KeeperConfig from the commented template, got %+v", cfg.Keeper)
	}
}

// TestInitKeeperTemplate_SingleUncommentOverride asserts that uncommenting ONE
// keeper line applies exactly that override, with NO version error.
func TestInitKeeperTemplate_SingleUncommentOverride(t *testing.T) {
	body := fmt.Sprintf(configYAMLContent, "main")

	// Uncomment exactly the keeper: header, context_thresholds:, and the one
	// warn_abs_tokens line. (YAML requires the parent keys present to nest the
	// override; a third party uncommenting one tunable uncomments its parents.)
	// Match the precise structured lines (the `# ` + exact text), so the prose
	// "# keeper: configures ..." header line is never touched.
	patched := uncommentKeeperLines(body,
		"keeper:",
		"  context_thresholds:",
		"    warn_abs_tokens:",
	)

	repoRoot := renderInitConfig(t, patched)
	cfg, err := daemon.LoadProjectConfig(repoRoot)
	if err != nil {
		t.Fatalf("single-uncomment override must NOT trip a version error, got: %v", err)
	}

	if cfg.Keeper.WarnAbsTokens != 200000 {
		t.Fatalf("expected warn_abs_tokens override = 200000, got %d", cfg.Keeper.WarnAbsTokens)
	}
	// Exactly ONE override: every other keeper field stays at its not-configured
	// zero value (act stays 0 so the resolver later applies its compiled default).
	if cfg.Keeper.ActAbsTokens != 0 {
		t.Errorf("expected act_abs_tokens to remain UNconfigured (0), got %d", cfg.Keeper.ActAbsTokens)
	}
	if cfg.Keeper.ForceActAbsTokens != 0 {
		t.Errorf("expected force_act_abs_tokens to remain UNconfigured (0), got %d", cfg.Keeper.ForceActAbsTokens)
	}
}

// lineUncommented reports whether the template has a non-comment line whose
// trimmed text contains needle.
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

// uncommentKeeperLines strips the leading "# " from the FIRST commented line
// whose text (after the "# ") is exactly `content` or `content` immediately
// followed by a space (so a trailing "# default: ..." annotation still matches),
// simulating a third party uncommenting a single tunable and its parent keys.
// Each content string is matched once. The exact/space-anchored match means the
// prose "# keeper: configures ..." header (where "keeper:" is followed by
// "configures", not a space-then-value) is never accidentally uncommented for
// the bare "keeper:" / nested-key contents, which carry their own indentation.
func uncommentKeeperLines(body string, contents ...string) string {
	lines := strings.Split(body, "\n")
	done := make([]bool, len(contents))
	// Pass 1: EXACT match (header lines like "# keeper:" / "#   context_thresholds:").
	// Exact-first ensures the structured "# keeper:" line wins over the prose
	// "# keeper: configures ..." header line earlier in the template.
	for i, ln := range lines {
		rest, ok := strings.CutPrefix(ln, "# ")
		if !ok {
			continue
		}
		for ci, want := range contents {
			if done[ci] || rest != want {
				continue
			}
			lines[i] = rest
			done[ci] = true
			break
		}
	}
	// Pass 2: SPACE-anchored prefix (tunable lines like
	// "#     warn_abs_tokens: 200000  # default: ...").
	for i, ln := range lines {
		rest, ok := strings.CutPrefix(ln, "# ")
		if !ok {
			continue
		}
		for ci, want := range contents {
			if done[ci] || !strings.HasPrefix(rest, want+" ") {
				continue
			}
			lines[i] = rest
			done[ci] = true
			break
		}
	}
	for ci, want := range contents {
		if !done[ci] {
			panic(fmt.Sprintf("uncommentKeeperLines: no commented line `# %s` found in template", want))
		}
	}
	return strings.Join(lines, "\n")
}

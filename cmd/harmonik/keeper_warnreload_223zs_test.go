package main

// keeper_warnreload_223zs_test.go — T4 (hk-keeper-delivery-mtime-reread-223zs):
// keeperReloadWarnMessagesFn is the injected SK-034 live-reload closure. It must
// re-parse the REAL config with the same strict loader as startup (so an unknown
// key still yields *ErrUnknownConfigKey — not silently absorbed), return ONLY the
// keeper.warn_messages overrides, and ignore threshold edits (scoping).

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/daemon"
)

func TestKeeperReloadWarnMessagesFn_ReturnsWarnTexts_223zs(t *testing.T) {
	projectDir := writeE1mdcProject(t, e1mdcConfigWithKeys)

	texts, err := keeperReloadWarnMessagesFn(projectDir)()
	if err != nil {
		t.Fatalf("reload: unexpected error: %v", err)
	}
	if texts.LeaderDeferText != "finish the unit, then harmonik keeper restart-now --agent x" {
		t.Errorf("LeaderDeferText = %q; want the config value", texts.LeaderDeferText)
	}
	if texts.CrewDeferText != "crew finish-then-self-restart" {
		t.Errorf("CrewDeferText = %q; want the config value", texts.CrewDeferText)
	}
}

func TestKeeperReloadWarnMessagesFn_UnknownKeyRejected_223zs(t *testing.T) {
	// A live edit that introduces an unknown warn_messages key must be REJECTED by
	// the reload (strict decode), so the watcher keeps the last-good text.
	badConfig := e1mdcConfigBase + `  warn_messages:
    leader_defer_text: "ok"
    leader_defer_txet: "typo introduced by a live edit"
`
	projectDir := writeE1mdcProject(t, badConfig)

	_, err := keeperReloadWarnMessagesFn(projectDir)()
	if err == nil {
		t.Fatal("reload of a config with an unknown warn_messages key must error; got nil")
	}
	var uerr *daemon.ErrUnknownConfigKey
	if !errors.As(err, &uerr) {
		t.Fatalf("error type = %T (%v); want *ErrUnknownConfigKey", err, err)
	}
	if uerr.KeyPath != "keeper.warn_messages.leader_defer_txet" {
		t.Errorf("KeyPath = %q; want %q", uerr.KeyPath, "keeper.warn_messages.leader_defer_txet")
	}
}

func TestKeeperReloadWarnMessagesFn_ThresholdEditIgnored_223zs(t *testing.T) {
	// Scoping proof: editing a threshold value in the file does NOT change the
	// warn-text overrides the reload returns (thresholds are not part of the
	// live-reload surface; the watcher applies only warn texts).
	projectDir := writeE1mdcProject(t, e1mdcConfigWithKeys)
	fn := keeperReloadWarnMessagesFn(projectDir)

	before, err := fn()
	if err != nil {
		t.Fatalf("reload (before): %v", err)
	}

	// Rewrite the config with a DIFFERENT warn threshold but identical warn_messages.
	edited := strings.Replace(e1mdcConfigWithKeys, "warn_abs_tokens: 180000", "warn_abs_tokens: 170000", 1)
	if edited == e1mdcConfigWithKeys {
		t.Fatal("test setup: threshold substitution did not change the config")
	}
	if err := os.WriteFile(filepath.Join(projectDir, ".harmonik", "config.yaml"), []byte(edited), 0o600); err != nil {
		t.Fatalf("rewrite config: %v", err)
	}

	after, err := fn()
	if err != nil {
		t.Fatalf("reload (after threshold edit): %v", err)
	}
	if after != before {
		t.Errorf("threshold edit leaked into the warn-text reload: before=%+v after=%+v", before, after)
	}
}

func TestKeeperReloadWarnMessagesFn_InvalidThresholdValueDoesNotBreakReload_223zs(t *testing.T) {
	// A live edit that sets a well-typed but SEMANTICALLY-invalid threshold (warn
	// above act — a band-ordering violation) must NOT break the warn_messages
	// reload: value validation lives in ResolveKeeperConfig (startup-bound), not in
	// the parse the reload runs, so the wording edit still applies live.
	bad := strings.Replace(e1mdcConfigWithKeys, "warn_abs_tokens: 180000", "warn_abs_tokens: 999999999", 1)
	if bad == e1mdcConfigWithKeys {
		t.Fatal("test setup: substitution did not change the config")
	}
	projectDir := writeE1mdcProject(t, bad)

	texts, err := keeperReloadWarnMessagesFn(projectDir)()
	if err != nil {
		t.Fatalf("reload must not fail on a semantically-invalid threshold value (only Resolve validates): %v", err)
	}
	if texts.LeaderDeferText != "finish the unit, then harmonik keeper restart-now --agent x" {
		t.Errorf("warn text not returned despite the invalid threshold value: %q", texts.LeaderDeferText)
	}
}

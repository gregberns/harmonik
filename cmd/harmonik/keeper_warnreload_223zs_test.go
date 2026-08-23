package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/projectconfig"
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
	badConfig := e1mdcConfigBase + `  warn_messages:
    leader_defer_text: "ok"
    leader_defer_txet: "typo introduced by a live edit"
`
	projectDir := writeE1mdcProject(t, badConfig)

	_, err := keeperReloadWarnMessagesFn(projectDir)()
	if err == nil {
		t.Fatal("reload of a config with an unknown warn_messages key must error; got nil")
	}
	var uerr *projectconfig.ErrUnknownConfigKey
	if !errors.As(err, &uerr) {
		t.Fatalf("error type = %T (%v); want *ErrUnknownConfigKey", err, err)
	}
	if uerr.KeyPath != "keeper.warn_messages.leader_defer_txet" {
		t.Errorf("KeyPath = %q; want %q", uerr.KeyPath, "keeper.warn_messages.leader_defer_txet")
	}
}

func TestKeeperReloadWarnMessagesFn_ThresholdEditIgnored_223zs(t *testing.T) {
	projectDir := writeE1mdcProject(t, e1mdcConfigWithKeys)
	fn := keeperReloadWarnMessagesFn(projectDir)

	before, err := fn()
	if err != nil {
		t.Fatalf("reload (before): %v", err)
	}

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

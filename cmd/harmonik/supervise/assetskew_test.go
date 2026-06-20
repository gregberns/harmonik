package supervisecmd

import (
	"encoding/json"
	"testing"
)

// TestAssetSyncAutoApplyDefaultOff — the safety-critical default: a zero-value
// Config (and a config.json with no asset_sync block) has AutoApply == false.
func TestAssetSyncAutoApplyDefaultOff(t *testing.T) {
	var cfg Config
	if cfg.AssetSync.AutoApply {
		t.Fatal("zero-value Config must have AssetSync.AutoApply == false")
	}

	// A config.json that omits asset_sync entirely must also unmarshal to OFF.
	parsed := Config{}
	if err := json.Unmarshal([]byte(`{"schema_version":1,"command":["claude"]}`), &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.AssetSync.AutoApply {
		t.Fatal("config.json without asset_sync must default AutoApply to false")
	}
}

// TestAssetSyncAutoApplyParsesWhenSet — when explicitly enabled in config.json the
// gate flips on (so the opt-in path is reachable).
func TestAssetSyncAutoApplyParsesWhenSet(t *testing.T) {
	parsed := Config{}
	if err := json.Unmarshal([]byte(`{"asset_sync":{"auto_apply":true}}`), &parsed); err != nil {
		t.Fatal(err)
	}
	if !parsed.AssetSync.AutoApply {
		t.Fatal("expected AutoApply=true when asset_sync.auto_apply is set")
	}
}

// TestRunAssetSkewCheckNoHookNoPanic — with no SkewCheckHook installed (the test
// package never installs it), RunAssetSkewCheck must be a safe no-op.
func TestRunAssetSkewCheckNoHookNoPanic(t *testing.T) {
	saved := SkewCheckHook
	SkewCheckHook = nil
	defer func() { SkewCheckHook = saved }()
	// Should not panic and should return promptly.
	RunAssetSkewCheck(t.TempDir(), Config{}, nil, nil)
}

// TestRunAssetSkewCheckHookInvoked — a stub hook reporting no skew exercises the
// non-skew branch without touching comms.
func TestRunAssetSkewCheckHookInvoked(t *testing.T) {
	saved := SkewCheckHook
	called := false
	SkewCheckHook = func(projectDir string) (AssetSkewVerdict, error) {
		called = true
		return AssetSkewVerdict{Skewed: false, BinaryDigest: "x"}, nil
	}
	defer func() { SkewCheckHook = saved }()

	RunAssetSkewCheck(t.TempDir(), Config{}, nil, nil)
	if !called {
		t.Fatal("expected SkewCheckHook to be invoked")
	}
}

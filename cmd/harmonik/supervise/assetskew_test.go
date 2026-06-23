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

// TestAutoApplyHookInvokedWhenEnabled — with AutoApply=true and safe candidates,
// AutoApplyHook IS invoked (and AutoApplyGateHook=nil is treated as not dispatching).
func TestAutoApplyHookInvokedWhenEnabled(t *testing.T) {
	savedHook := AutoApplyHook
	savedGate := AutoApplyGateHook
	defer func() {
		AutoApplyHook = savedHook
		AutoApplyGateHook = savedGate
	}()

	called := false
	AutoApplyHook = func(_ string) (int, error) {
		called = true
		return 1, nil
	}
	AutoApplyGateHook = nil // nil gate → not dispatching

	v := AssetSkewVerdict{Skewed: true, AutoApplyCandidates: 2, ChangedCount: 2}
	cfg := Config{AssetSync: AssetSyncConfig{AutoApply: true}}
	maybeAutoApply(t.TempDir(), cfg, v, nil, nil)

	if !called {
		t.Fatal("expected AutoApplyHook to be invoked when AutoApply=true with safe candidates")
	}
}

// TestAutoApplyHookNotInvokedWhenDisabled — with AutoApply=false, AutoApplyHook
// is never called regardless of candidate count.
func TestAutoApplyHookNotInvokedWhenDisabled(t *testing.T) {
	saved := AutoApplyHook
	defer func() { AutoApplyHook = saved }()

	called := false
	AutoApplyHook = func(_ string) (int, error) {
		called = true
		return 0, nil
	}

	v := AssetSkewVerdict{Skewed: true, AutoApplyCandidates: 3}
	cfg := Config{} // AutoApply is false by default
	maybeAutoApply(t.TempDir(), cfg, v, nil, nil)

	if called {
		t.Fatal("AutoApplyHook must NOT be invoked when AutoApply=false")
	}
}

// TestAutoApplyHookNotInvokedWhenDispatching — when AutoApplyGateHook reports
// the daemon is dispatching, AutoApplyHook is NOT invoked.
func TestAutoApplyHookNotInvokedWhenDispatching(t *testing.T) {
	savedHook := AutoApplyHook
	savedGate := AutoApplyGateHook
	defer func() {
		AutoApplyHook = savedHook
		AutoApplyGateHook = savedGate
	}()

	called := false
	AutoApplyHook = func(_ string) (int, error) {
		called = true
		return 0, nil
	}
	AutoApplyGateHook = func(_ string) (bool, string, error) {
		return true, "queue has in-flight work", nil // dispatching
	}

	v := AssetSkewVerdict{Skewed: true, AutoApplyCandidates: 2, ChangedCount: 2}
	cfg := Config{AssetSync: AssetSyncConfig{AutoApply: true}}
	maybeAutoApply(t.TempDir(), cfg, v, nil, nil)

	if called {
		t.Fatal("AutoApplyHook must NOT be invoked when the lull gate reports dispatching")
	}
}

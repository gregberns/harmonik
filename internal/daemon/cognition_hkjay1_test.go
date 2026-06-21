package daemon_test

// cognition_hkjay1_test.go — unit tests for TooBigSignal and ContextStaticSignal
// population in buildCognitionSignals (SS-012, P2-c: hk-jay1).
//
// Covers:
//   (a) TooBigSignal: Threshold=nil when warn_abs_tokens not configured (dark-when-unset).
//   (b) TooBigSignal: Threshold set, Tripped=false, Band="warn" when tokens < warn.
//   (c) TooBigSignal: Tripped=true, Band="warn" when tokens >= warn but < act.
//   (d) TooBigSignal: Band escalates to "act" and "force_act" correctly.
//   (e) ContextStaticSignal: StalenessS=nil when keeper.staleness not configured.
//   (f) ContextStaticSignal: StalenessS set correctly when Staleness configured.
//   (g) ContextStaticSignal: Flat=nil when StuckMinIntervals not in KeeperConfig (SS-1193).
//   (h) ContextStaticSignal: TokensUnchangedIntervals=0 (no history in single .ctx read).
//   (i) ThresholdRef always set regardless of config.
//
// Helper prefix: cognJay1 (implementer-protocol.md §Helper-prefix discipline).
//
// Bead: hk-jay1.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/keeper"
	"github.com/gregberns/harmonik/internal/lifecycle"
)

// cognJay1Fixture sets up a minimal project dir with a keeper gauge file for
// the given agent and returns the project dir path.
func cognJay1Fixture(t *testing.T, agent string, tokens, windowSize int64) string {
	t.Helper()
	root := t.TempDir()
	keeperDir := filepath.Join(root, ".harmonik", "keeper")
	if err := os.MkdirAll(keeperDir, 0o755); err != nil {
		t.Fatalf("cognJay1Fixture: MkdirAll: %v", err)
	}
	ts := time.Now().UTC().Format(time.RFC3339)
	var pct float64
	if windowSize > 0 {
		pct = float64(tokens) / float64(windowSize)
	}
	cf := map[string]any{
		"pct":         pct,
		"tokens":      tokens,
		"window_size": windowSize,
		"session_id":  "test-session-id",
		"ts":          ts,
	}
	data, err := json.Marshal(cf)
	if err != nil {
		t.Fatalf("cognJay1Fixture: json.Marshal: %v", err)
	}
	ctxPath := filepath.Join(keeperDir, agent+".ctx")
	if err := os.WriteFile(ctxPath, data, 0o644); err != nil {
		t.Fatalf("cognJay1Fixture: WriteFile ctx: %v", err)
	}
	return root
}

// cognJay1Cog builds the cognition block for agent directly via the test seam,
// bypassing the tmux liveness check. Returns nil if the ctx file is absent.
func cognJay1Cog(t *testing.T, projectDir string, kconfig daemon.KeeperConfig, agent string) *daemon.SessionCognition {
	t.Helper()
	ph := lifecycle.ComputeProjectHash(projectDir)
	lb := daemon.NewLiveStateBuilderForTest(projectDir, core.ProjectHash(ph), kconfig)
	return lb.BuildCognitionForTest(agent, "live-sid", "declared-sid", time.Now())
}

// ─────────────────────────────────────────────────────────────────────────────
// (a) Threshold=nil when warn_abs_tokens not configured
// ─────────────────────────────────────────────────────────────────────────────

func TestCognJay1_TooBig_ThresholdNilWhenUnconfigured(t *testing.T) {
	t.Parallel()
	root := cognJay1Fixture(t, "captain", 182_000, 200_000)
	cog := cognJay1Cog(t, root, daemon.KeeperConfig{}, "captain")
	if cog == nil {
		t.Fatal("expected cognition block, got nil")
	}
	if cog.Signals.TooBig.Threshold != nil {
		t.Errorf("Threshold: want nil when unconfigured, got %d", *cog.Signals.TooBig.Threshold)
	}
	if cog.Signals.TooBig.Tripped {
		t.Error("Tripped: want false when threshold unconfigured")
	}
	if cog.Signals.TooBig.Band != "" {
		t.Errorf("Band: want empty string when threshold unconfigured, got %q", cog.Signals.TooBig.Band)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// (b) Threshold set, Tripped=false, Band="warn" when tokens < warn
// ─────────────────────────────────────────────────────────────────────────────

func TestCognJay1_TooBig_NotTripped(t *testing.T) {
	t.Parallel()
	root := cognJay1Fixture(t, "captain", 182_304, 1_000_000)
	kconfig := daemon.KeeperConfig{WarnAbsTokens: 200_000}
	cog := cognJay1Cog(t, root, kconfig, "captain")
	if cog == nil {
		t.Fatal("expected cognition block, got nil")
	}
	sig := cog.Signals.TooBig
	if sig.Threshold == nil {
		t.Fatal("Threshold: want non-nil when warn_abs_tokens configured")
	}
	if *sig.Threshold != 200_000 {
		t.Errorf("Threshold: want 200000, got %d", *sig.Threshold)
	}
	if sig.Value != 182_304 {
		t.Errorf("Value: want 182304, got %d", sig.Value)
	}
	if sig.Tripped {
		t.Error("Tripped: want false when tokens < warn threshold")
	}
	if sig.Band != "warn" {
		t.Errorf("Band: want %q (reference level below threshold), got %q", "warn", sig.Band)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// (c) Tripped=true, Band="warn" when tokens >= warn but < act
// ─────────────────────────────────────────────────────────────────────────────

func TestCognJay1_TooBig_TrippedWarnBand(t *testing.T) {
	t.Parallel()
	root := cognJay1Fixture(t, "captain", 205_000, 1_000_000)
	kconfig := daemon.KeeperConfig{
		WarnAbsTokens: 200_000,
		ActAbsTokens:  215_000,
	}
	cog := cognJay1Cog(t, root, kconfig, "captain")
	if cog == nil {
		t.Fatal("expected cognition block, got nil")
	}
	sig := cog.Signals.TooBig
	if !sig.Tripped {
		t.Error("Tripped: want true when tokens >= warn threshold")
	}
	if sig.Band != "warn" {
		t.Errorf("Band: want %q, got %q", "warn", sig.Band)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// (d) Band escalation: act and force_act
// ─────────────────────────────────────────────────────────────────────────────

func TestCognJay1_TooBig_ActBand(t *testing.T) {
	t.Parallel()
	root := cognJay1Fixture(t, "captain", 220_000, 1_000_000)
	kconfig := daemon.KeeperConfig{
		WarnAbsTokens:     200_000,
		ActAbsTokens:      215_000,
		ForceActAbsTokens: 240_000,
	}
	cog := cognJay1Cog(t, root, kconfig, "captain")
	if cog == nil {
		t.Fatal("expected cognition block, got nil")
	}
	sig := cog.Signals.TooBig
	if !sig.Tripped {
		t.Errorf("Tripped: want true (tokens=%d >= act=%d)", 220_000, 215_000)
	}
	if sig.Band != "act" {
		t.Errorf("Band: want %q, got %q", "act", sig.Band)
	}
}

func TestCognJay1_TooBig_ForceActBand(t *testing.T) {
	t.Parallel()
	root := cognJay1Fixture(t, "captain", 245_000, 1_000_000)
	kconfig := daemon.KeeperConfig{
		WarnAbsTokens:     200_000,
		ActAbsTokens:      215_000,
		ForceActAbsTokens: 240_000,
	}
	cog := cognJay1Cog(t, root, kconfig, "captain")
	if cog == nil {
		t.Fatal("expected cognition block, got nil")
	}
	sig := cog.Signals.TooBig
	if !sig.Tripped {
		t.Errorf("Tripped: want true (tokens=%d >= force_act=%d)", 245_000, 240_000)
	}
	if sig.Band != "force_act" {
		t.Errorf("Band: want %q, got %q", "force_act", sig.Band)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// (e) ContextStaticSignal: StalenessS=nil when Staleness not configured
// ─────────────────────────────────────────────────────────────────────────────

func TestCognJay1_ContextStatic_StalenessNilWhenUnconfigured(t *testing.T) {
	t.Parallel()
	root := cognJay1Fixture(t, "captain", 100_000, 200_000)
	cog := cognJay1Cog(t, root, daemon.KeeperConfig{}, "captain")
	if cog == nil {
		t.Fatal("expected cognition block, got nil")
	}
	if cog.Signals.ContextStatic.StalenessS != nil {
		t.Errorf("StalenessS: want nil when unconfigured, got %d", *cog.Signals.ContextStatic.StalenessS)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// (f) ContextStaticSignal: StalenessS set correctly when Staleness configured
// ─────────────────────────────────────────────────────────────────────────────

func TestCognJay1_ContextStatic_StalenessSet(t *testing.T) {
	t.Parallel()
	root := cognJay1Fixture(t, "captain", 100_000, 200_000)
	kconfig := daemon.KeeperConfig{Staleness: 120 * time.Second}
	cog := cognJay1Cog(t, root, kconfig, "captain")
	if cog == nil {
		t.Fatal("expected cognition block, got nil")
	}
	cs := cog.Signals.ContextStatic
	if cs.StalenessS == nil {
		t.Fatal("StalenessS: want non-nil when Staleness configured")
	}
	if *cs.StalenessS != 120 {
		t.Errorf("StalenessS: want 120, got %d", *cs.StalenessS)
	}
	if cs.StalenessRef != "keeper.staleness" {
		t.Errorf("StalenessRef: want %q, got %q", "keeper.staleness", cs.StalenessRef)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// (g) Flat=nil when StuckMinIntervals not configured (SS-1193 dark-when-unset)
// ─────────────────────────────────────────────────────────────────────────────

func TestCognJay1_ContextStatic_FlatNilWhenStuckMinIntervalsUnset(t *testing.T) {
	t.Parallel()
	root := cognJay1Fixture(t, "captain", 100_000, 200_000)
	// Even with WarnAbsTokens and Staleness configured, Flat must be null
	// because stuck_min_intervals is not yet a KeeperConfig knob (SS-012 opt-in).
	kconfig := daemon.KeeperConfig{
		WarnAbsTokens: 200_000,
		Staleness:     120 * time.Second,
	}
	cog := cognJay1Cog(t, root, kconfig, "captain")
	if cog == nil {
		t.Fatal("expected cognition block, got nil")
	}
	cs := cog.Signals.ContextStatic
	if cs.Flat != nil {
		t.Errorf("Flat: want nil (dark) when stuck_min_intervals not in config, got %v", *cs.Flat)
	}
	if cs.StuckMinIntervals != nil {
		t.Errorf("StuckMinIntervals: want nil, got %d", *cs.StuckMinIntervals)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// (h) TokensUnchangedIntervals=0 (no history without multi-sample reads)
// ─────────────────────────────────────────────────────────────────────────────

func TestCognJay1_ContextStatic_TokensUnchangedIntervalsZero(t *testing.T) {
	t.Parallel()
	root := cognJay1Fixture(t, "captain", 150_000, 200_000)
	cog := cognJay1Cog(t, root, daemon.KeeperConfig{}, "captain")
	if cog == nil {
		t.Fatal("expected cognition block, got nil")
	}
	if cog.Signals.ContextStatic.TokensUnchangedIntervals != 0 {
		t.Errorf("TokensUnchangedIntervals: want 0, got %d",
			cog.Signals.ContextStatic.TokensUnchangedIntervals)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// (i) ThresholdRef always set regardless of config
// ─────────────────────────────────────────────────────────────────────────────

func TestCognJay1_TooBig_ThresholdRefAlwaysSet(t *testing.T) {
	t.Parallel()
	root := cognJay1Fixture(t, "captain", 50_000, 200_000)
	cog := cognJay1Cog(t, root, daemon.KeeperConfig{}, "captain")
	if cog == nil {
		t.Fatal("expected cognition block, got nil")
	}
	const wantRef = "keeper.context_thresholds.warn_abs_tokens"
	if cog.Signals.TooBig.ThresholdRef != wantRef {
		t.Errorf("ThresholdRef: want %q, got %q", wantRef, cog.Signals.TooBig.ThresholdRef)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Sanity: keeper.ReadCtxFile round-trip (used by cognJay1Fixture path)
// ─────────────────────────────────────────────────────────────────────────────

func TestCognJay1_CtxFileRoundtrip(t *testing.T) {
	t.Parallel()
	root := cognJay1Fixture(t, "paul", 175_000, 200_000)
	cf, _, err := keeper.ReadCtxFile(root, "paul")
	if err != nil {
		t.Fatalf("ReadCtxFile: %v", err)
	}
	if cf.Tokens != 175_000 {
		t.Errorf("Tokens: want 175000, got %d", cf.Tokens)
	}
}

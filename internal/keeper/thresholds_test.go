package keeper

import "testing"

// TestSharedThresholdDefaults_SingleSource pins the keeper warn/act/force band to
// its single source of truth (thresholds.go) and asserts that WatcherConfig and
// CyclerConfig resolve IDENTICAL shared values. Before the consolidation these
// two configs re-declared the warn defaults independently, which permitted silent
// drift (logmine: SKILL.md listed force-act as both 340k and 380k). A change to
// any value here is a deliberate band-retune — an operator decision, never a
// side effect of a refactor. Refs: hk-bpkv, codename:keeper-redesign.
func TestSharedThresholdDefaults_SingleSource(t *testing.T) {
	// Pin the named default constants (the single source of truth). These values
	// are operator-decided; do NOT change them to make a test pass.
	cases := []struct {
		name string
		got  float64
		want float64
	}{
		{"defaultWarnPct", defaultWarnPct, 80.0},
		{"defaultActPct", defaultActPct, 90.0},
		{"defaultForceActPctOffset", defaultForceActPctOffset, 5.0},
		{"defaultWarnAbsTokens", float64(defaultWarnAbsTokens), 270_000},
		{"defaultActAbsTokens", float64(defaultActAbsTokens), 300_000},
		{"defaultForceActAbsOffset", float64(defaultForceActAbsOffset), 40_000},
		{"defaultWarnPctCeil", defaultWarnPctCeil, 0.70},
		{"defaultActPctCeil", defaultActPctCeil, 0.85},
		{"defaultForceActPctCeilOffset", defaultForceActPctCeilOffset, 0.10},
		{"defaultFallbackWindowSize", float64(defaultFallbackWindowSize), 200_000},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s = %v; want %v (operator-pinned default — do not change in a refactor)", c.name, c.got, c.want)
		}
	}

	// WatcherConfig and CyclerConfig MUST agree on every shared warn-band value
	// after applyDefaults. This is the anti-drift invariant the consolidation buys.
	var w WatcherConfig
	w.applyDefaults()
	var cy CyclerConfig
	cy.applyDefaults()

	if w.WarnPct != cy.WarnPct {
		t.Errorf("WarnPct drift: watcher=%v cycler=%v", w.WarnPct, cy.WarnPct)
	}
	if w.WarnAbsTokens != cy.WarnAbsTokens {
		t.Errorf("WarnAbsTokens drift: watcher=%v cycler=%v", w.WarnAbsTokens, cy.WarnAbsTokens)
	}
	if w.WarnPctCeil != cy.WarnPctCeil {
		t.Errorf("WarnPctCeil drift: watcher=%v cycler=%v", w.WarnPctCeil, cy.WarnPctCeil)
	}

	// Resolved force-act defaults are operator-pinned (hk-lhu2): 340k / 0.95 / 95.
	if cy.ForceActAbsTokens != 340_000 {
		t.Errorf("ForceActAbsTokens = %d; want 340000 (act 300k + 40k offset)", cy.ForceActAbsTokens)
	}
	if cy.ForceActPctCeil != 0.95 {
		t.Errorf("ForceActPctCeil = %v; want 0.95 (act-ceil 0.85 + 0.10 offset)", cy.ForceActPctCeil)
	}
	if cy.ForceActPct != 95.0 {
		t.Errorf("ForceActPct = %v; want 95 (act-pct 90 + 5 offset)", cy.ForceActPct)
	}
}

// TestMinAbsOrPctCeil pins the single shared min(abs, pctCeil*window) formula that
// both watcher.go (belowWarnThreshold) and cycle.go (warn/act/forceAct thresholds)
// previously hand-re-implemented. Refs: hk-bpkv.
func TestMinAbsOrPctCeil(t *testing.T) {
	cases := []struct {
		name       string
		abs        int64
		pctCeil    float64
		windowSize int64
		want       int64
	}{
		// 200k window: the pct-ceil wins for warn (0.70*200k=140k < 270k).
		{"200k-window-pct-ceil-wins", 270_000, 0.70, 200_000, 140_000},
		// 1M window: the abs cap wins (0.70*1M=700k > 270k) — the [1m]-model case.
		{"1m-window-abs-cap-wins", 270_000, 0.70, 1_000_000, 270_000},
		// windowSize==0: abs returned unconditionally (no window data).
		{"zero-window-returns-abs", 270_000, 0.70, 0, 270_000},
		// pctCeil==0: abs returned (guard preserved from watcher.go).
		{"zero-pctceil-returns-abs", 300_000, 0, 200_000, 300_000},
		// act gate on a 200k window: 0.85*200k=170k < 300k.
		{"act-200k-pct-ceil-wins", 300_000, 0.85, 200_000, 170_000},
	}
	for _, c := range cases {
		if got := minAbsOrPctCeil(c.abs, c.pctCeil, c.windowSize); got != c.want {
			t.Errorf("%s: minAbsOrPctCeil(%d, %v, %d) = %d; want %d",
				c.name, c.abs, c.pctCeil, c.windowSize, got, c.want)
		}
	}
}

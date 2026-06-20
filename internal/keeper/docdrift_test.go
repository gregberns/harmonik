package keeper

// docdrift_test.go is the ANTI-DRIFT LOCK for the keeper configuration-surface
// table in docs/components/internal/keeper.md and the reconciled band comments in
// the keeper source. It parses the doc's `default` column and the band numbers and
// FAILS if any diverge from the Default* constants in thresholds.go — so the doc
// (and the in-code default comments) cannot silently rot away from the real band
// again (the bug hk-fgnk reconciled: docs/comments still said 270k/300k/340k after
// the TA1 retune to 200k/215k/240k). Refs: hk-fgnk, hk-8hr1.

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// repoRootFromTest walks up from this test file to the repo root (internal/keeper
// → internal → repo). Mirrors statusline_test.go's locator.
func repoRootFromTest(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Skip("runtime.Caller failed; cannot locate repo root")
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
}

// normalizeTokenLiteral strips separators/suffixes so 280k / 280000 / 280_000 / "280 000"
// all compare equal as an int64. Returns (value, true) when the input is a pure
// (optionally k-suffixed) integer literal; (0, false) otherwise.
func normalizeTokenLiteral(s string) (int64, bool) {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "_", "")
	s = strings.ReplaceAll(s, ",", "")
	s = strings.ReplaceAll(s, " ", "")
	mult := int64(1)
	if strings.HasSuffix(s, "k") || strings.HasSuffix(s, "K") {
		mult = 1000
		s = s[:len(s)-1]
	}
	if s == "" {
		return 0, false
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, false
	}
	return n * mult, true
}

// docDefaultExpect maps a dotted config key (as it appears in the doc table's first
// column) to the token value its `default` column MUST encode. Only token-valued
// rows are pinned here; duration/bool/pct/text rows are not numeric-token-checked.
// Each expected value is the live Default* constant — so editing the constant forces
// editing the doc (and vice-versa) or this test fails.
var docDefaultExpect = map[string]int64{
	"keeper.context_thresholds.warn_abs_tokens":       DefaultWarnAbsTokens,
	"keeper.context_thresholds.act_abs_tokens":        DefaultActAbsTokens,
	"keeper.context_thresholds.force_act_abs_tokens":  DefaultActAbsTokens + DefaultForceActAbsOffset,
	"keeper.context_thresholds.force_act_abs_offset":  DefaultForceActAbsOffset,
	"keeper.context_thresholds.idle_floor_abs_tokens": DefaultIdleRestartAbsTokens,
	"keeper.hard_ceiling.abs_tokens":                  DefaultHardCeilingTokens,
	"keeper.budgets.heartbeat_max_misses":             DefaultMaxHeartbeatMisses,
	"keeper.budgets.max_handoff_timeouts":             DefaultMaxHandoffTimeouts,
}

// docTableRowRe matches a markdown table row whose first cell is a backticked dotted
// key and captures the key (cell 1) and the default cell (cell 3).
//
//	| `keeper.context_thresholds.warn_abs_tokens` | `--warn-abs-tokens` | `200000` (...) | ... |
var docTableRowRe = regexp.MustCompile(
	"^\\|\\s*`([a-z0-9_.]+)`\\s*\\|[^|]*\\|([^|]*)\\|",
)

// leadingTokenRe pulls the first integer-ish literal out of a default cell, e.g.
// "`200000` (`DefaultWarnAbsTokens`)" → "200000"; "`280k` ..." → "280k".
var leadingTokenRe = regexp.MustCompile("`?([0-9][0-9_, ]*[kK]?)`?")

// TestKeeperDocDefaultsNoDrift parses docs/components/internal/keeper.md and asserts
// every token-valued default in the configuration-surface table equals its Default*
// constant. It fails on any drift, and on a missing/renamed row.
func TestKeeperDocDefaultsNoDrift(t *testing.T) {
	root := repoRootFromTest(t)
	docPath := filepath.Join(root, "docs", "components", "internal", "keeper.md")
	data, err := os.ReadFile(docPath) //nolint:gosec // test-only, fixed path
	if err != nil {
		t.Fatalf("reading keeper doc %s: %v", docPath, err)
	}

	seen := map[string]int64{}
	for _, line := range strings.Split(string(data), "\n") {
		m := docTableRowRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		key := m[1]
		want, pinned := docDefaultExpect[key]
		if !pinned {
			continue // non-token row (duration/bool/text) — not numeric-pinned
		}
		defaultCell := m[2]
		tm := leadingTokenRe.FindStringSubmatch(defaultCell)
		if tm == nil {
			t.Errorf("doc row %q: no numeric default found in cell %q", key, strings.TrimSpace(defaultCell))
			continue
		}
		got, ok := normalizeTokenLiteral(tm[1])
		if !ok {
			t.Errorf("doc row %q: default cell %q did not parse as a token literal", key, tm[1])
			continue
		}
		if got != want {
			t.Errorf("DOC DRIFT for %q: doc says %d, constant is %d — update the doc table AND the constant together", key, got, want)
		}
		seen[key] = got
	}

	for key := range docDefaultExpect {
		if _, ok := seen[key]; !ok {
			t.Errorf("doc table is MISSING a pinned row for %q (was the dotted key renamed or removed?)", key)
		}
	}
}

// staleBandLiterals are the post-TA1-retune-OBSOLETE band numbers. After hk-fgnk no
// keeper source comment or keeper doc may mention them as a default/threshold; their
// presence means a comment rotted back to the pre-retune band.
var staleBandLiterals = []string{"270000", "300000", "340000", "270k", "300k", "340k", "270_000", "300_000", "340_000"}

// reconciledSources are the files whose band comments hk-fgnk reconciled; they must
// stay free of the stale literals. (Test fixtures elsewhere legitimately set override
// values like force_act_abs_tokens: 340000 and are intentionally excluded.)
var reconciledSources = []string{
	filepath.Join("internal", "keeper", "cycle.go"),
	filepath.Join("internal", "keeper", "watcher.go"),
	filepath.Join("internal", "daemon", "projectconfig.go"),
	filepath.Join("docs", "components", "internal", "keeper.md"),
	filepath.Join("docs", "captain-restart.md"),
}

// TestKeeperReconciledCommentsNoStaleBand asserts the reconciled keeper source/doc
// files contain none of the obsolete 270k/300k/340k band literals — the lock that
// keeps hk-fgnk's reconciliation from silently regressing.
func TestKeeperReconciledCommentsNoStaleBand(t *testing.T) {
	root := repoRootFromTest(t)
	for _, rel := range reconciledSources {
		p := filepath.Join(root, rel)
		data, err := os.ReadFile(p) //nolint:gosec // test-only, fixed path set
		if err != nil {
			t.Errorf("reading reconciled source %s: %v", rel, err)
			continue
		}
		body := string(data)
		for _, lit := range staleBandLiterals {
			if strings.Contains(body, lit) {
				t.Errorf("STALE BAND in %s: found obsolete literal %q — the band is 200k/215k/240k (force-act); reconcile the comment", rel, lit)
			}
		}
	}
}

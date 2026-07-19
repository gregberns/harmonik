package brcli_test

// sweepclosebead_h3_test.go — H3 regression: a Cat 3c auto-close is a daemon
// INFERENCE (a trailer-bearing, unreverted commit is present), not an explicit
// operator/reviewer sign-off, so SweepCloseBead MUST flag the auto-closed bead
// with the "needs-attention" label for operator triage (mirroring CloseBead's
// needs-attention path).

import (
	"context"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
)

// TestH3_SweepCloseBead_AppliesNeedsAttentionLabel verifies SweepCloseBead issues
// BOTH the `close <bead>` write AND the `label add <bead> -l needs-attention`
// write, in that order.
func TestH3_SweepCloseBead_AppliesNeedsAttentionLabel(t *testing.T) {
	argsFile := t.TempDir() + "/argv.log"
	adapter := imrestFixtureAppendArgsAdapter(t, argsFile)
	bid := core.BeadID("hk-h3test")

	if err := adapter.SweepCloseBead(context.Background(), brcli.TimeoutConfig{}, bid); err != nil {
		t.Fatalf("SweepCloseBead: %v", err)
	}

	lines := imrestFixtureReadArgsLines(t, argsFile)
	if len(lines) != 2 {
		t.Fatalf("SweepCloseBead issued %d br calls, want 2 (close + label add); got %v", len(lines), lines)
	}
	if !strings.HasPrefix(lines[0], "close "+string(bid)) {
		t.Errorf("first br call = %q; want close %s", lines[0], bid)
	}
	if !strings.Contains(lines[1], "label add "+string(bid)) || !strings.Contains(lines[1], "needs-attention") {
		t.Errorf("second br call = %q; want `label add %s -l needs-attention`", lines[1], bid)
	}
}

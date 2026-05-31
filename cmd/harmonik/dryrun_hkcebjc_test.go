package main

// dryrun_hkcebjc_test.go — unit tests for --dry-run / --plan-only (hk-cebjc).
//
// Tests cover:
//   - printDryRunPlan output for single-mode beads
//   - printDryRunPlan output for review-loop-mode beads (shows reviewer count)
//   - printDryRunPlan output for dot-mode beads
//   - printDryRunPlan total line aggregation across multiple beads
//   - runUsage() mentions --dry-run and --plan-only
//
// All tests are parallel-safe (no shared state, no os.Args mutation).
//
// Bead ref: hk-cebjc.

import (
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/queue"
)

// dryRunFixtureBeads builds a slice of minimal BeadRecords for plan tests.
func dryRunFixtureBeads(ids []string, titles []string) []core.BeadRecord {
	recs := make([]core.BeadRecord, len(ids))
	for i, id := range ids {
		recs[i] = core.BeadRecord{
			BeadID:        core.BeadID(id),
			Title:         titles[i],
			BeadType:      "task",
			Status:        core.CoarseStatusOpen,
			AuditTrailRef: "audit-" + id,
		}
	}
	return recs
}

// TestDryRunPlanSingleMode verifies printDryRunPlan output for single-mode beads.
func TestDryRunPlanSingleMode(t *testing.T) {
	t.Parallel()

	recs := dryRunFixtureBeads([]string{"hk-aaa111"}, []string{"Fix the thing"})
	var buf strings.Builder
	printDryRunPlan(&buf, recs, string(core.WorkflowModeSingle), "", 1, queue.GroupKindStream)

	out := buf.String()
	if !strings.Contains(out, "hk-aaa111") {
		t.Errorf("expected bead ID in output; got:\n%s", out)
	}
	if !strings.Contains(out, "1 implementer") {
		t.Errorf("expected '1 implementer' in output for single mode; got:\n%s", out)
	}
	if strings.Contains(out, "reviewer") {
		t.Errorf("single mode should not mention reviewers; got:\n%s", out)
	}
	if !strings.Contains(out, "No changes written") {
		t.Errorf("expected 'No changes written' footer; got:\n%s", out)
	}
}

// TestDryRunPlanReviewLoopMode verifies printDryRunPlan output for review-loop beads,
// including per-bead reviewer count and the total summary line.
func TestDryRunPlanReviewLoopMode(t *testing.T) {
	t.Parallel()

	recs := dryRunFixtureBeads(
		[]string{"hk-bbb222", "hk-ccc333"},
		[]string{"Add feature A", "Add feature B"},
	)
	var buf strings.Builder
	printDryRunPlan(&buf, recs, string(core.WorkflowModeReviewLoop), "", 2, queue.GroupKindWave)

	out := buf.String()
	if !strings.Contains(out, "hk-bbb222") || !strings.Contains(out, "hk-ccc333") {
		t.Errorf("expected both bead IDs in output; got:\n%s", out)
	}
	if !strings.Contains(out, "reviewer") {
		t.Errorf("review-loop mode should mention reviewers; got:\n%s", out)
	}
	// Total line: 2 implementers + up to 6 reviewers (3 each × 2 beads)
	if !strings.Contains(out, "2 implementer") {
		t.Errorf("expected '2 implementer(s)' in total line; got:\n%s", out)
	}
	if !strings.Contains(out, "6 reviewer") {
		t.Errorf("expected '6 reviewer(s)' in total line (3 per bead × 2 beads); got:\n%s", out)
	}
	if !strings.Contains(out, "No changes written") {
		t.Errorf("expected 'No changes written' footer; got:\n%s", out)
	}
}

// TestDryRunPlanDotMode verifies printDryRunPlan output for dot-mode beads,
// which cannot enumerate agents statically.
func TestDryRunPlanDotMode(t *testing.T) {
	t.Parallel()

	recs := dryRunFixtureBeads([]string{"hk-ddd444"}, []string{"Dot workflow bead"})
	var buf strings.Builder
	printDryRunPlan(&buf, recs, string(core.WorkflowModeDot), "./my.dot", 1, queue.GroupKindStream)

	out := buf.String()
	if !strings.Contains(out, "hk-ddd444") {
		t.Errorf("expected bead ID in output; got:\n%s", out)
	}
	if !strings.Contains(out, "my.dot") {
		t.Errorf("expected workflow-ref path in output for dot mode; got:\n%s", out)
	}
	if !strings.Contains(out, "No changes written") {
		t.Errorf("expected 'No changes written' footer; got:\n%s", out)
	}
}

// TestDryRunPlanLongTitle verifies that printDryRunPlan truncates very long
// bead titles to keep output readable.
func TestDryRunPlanLongTitle(t *testing.T) {
	t.Parallel()

	longTitle := strings.Repeat("x", 80)
	recs := dryRunFixtureBeads([]string{"hk-eee555"}, []string{longTitle})
	var buf strings.Builder
	printDryRunPlan(&buf, recs, string(core.WorkflowModeSingle), "", 1, queue.GroupKindStream)

	out := buf.String()
	// The title should appear truncated (≤53 chars including the "..." suffix).
	if strings.Contains(out, longTitle) {
		t.Errorf("expected long title to be truncated in output; got full title in:\n%s", out)
	}
	if !strings.Contains(out, "...") {
		t.Errorf("expected truncation ellipsis in output; got:\n%s", out)
	}
}

// TestDryRunUsageMentionsFlags verifies that runUsage() documents --dry-run and --plan-only.
func TestDryRunUsageMentionsFlags(t *testing.T) {
	t.Parallel()

	out := streamDefaultFixtureCaptureUsage(t) // reuse helper from run_stream_default_test.go
	if !strings.Contains(out, "--dry-run") {
		t.Errorf("runUsage() does not mention --dry-run; got:\n%s", out)
	}
	if !strings.Contains(out, "--plan-only") {
		t.Errorf("runUsage() does not mention --plan-only; got:\n%s", out)
	}
}

package daemon_test

// harness_selection_dryrun_hkqxfj0_explore_test.go — exploratory tests for
// per-item harness resolution in the context of a dry-run queue scan.
//
// These tests exercise the harness-selection chain (resolveHarness) as it would
// be applied per-item during a dry-run: bead-label harness:<agent-type> (tier 1)
// overrides the global Config.DefaultHarness (tier 4 built-in = claude-code).
//
// Key property under test:
//   - A bead with label "harness:codex" resolves to AgentTypeCodex even when
//     the global default is AgentTypeClaudeCode.
//   - A bead without a harness label falls through to the global default.
//   - A mixed queue of beads shows per-item resolved harnesses correctly.
//
// The "shows" behaviour is captured by resolveHarnessPerItem (a helper that
// mirrors what a dry-run harness-summary step would produce), verified by
// TestHarnessSelectionDryRun_PrintPerItem.
//
// Spec ref: codex-harness C4/T4+T5; resolveHarness precedence chain.
// Bead: hk-qxfj0.

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// ─────────────────────────────────────────────────────────────────────────────
// Fixtures
// ─────────────────────────────────────────────────────────────────────────────

// harnessSelectionDryRunFixtureBead builds a BeadRecord with the given labels.
func harnessSelectionDryRunFixtureBead(id string, labels []string) core.BeadRecord {
	return core.BeadRecord{
		BeadID:        core.BeadID(id),
		Title:         "fixture bead " + id,
		BeadType:      "task",
		Status:        core.CoarseStatusOpen,
		Labels:        labels,
		AuditTrailRef: id,
	}
}

// nullBus discards all emitted events (no conflict expected in the success path).
type nullBus struct{}

func (nullBus) Emit(_ context.Context, _ core.EventType, _ []byte) error { return nil }
func (nullBus) EmitWithRunID(_ context.Context, _ core.RunID, _ core.EventType, _ []byte) error {
	return nil
}

// resolveHarnessPerItem resolves the harness for each bead in beads against
// globalDefault and returns a map from BeadID to resolved AgentType.
// This mirrors what a dry-run harness-summary step would compute per item.
func resolveHarnessPerItem(
	ctx context.Context,
	beads []core.BeadRecord,
	globalDefault core.AgentType,
) map[core.BeadID]core.AgentType {
	result := make(map[core.BeadID]core.AgentType, len(beads))
	bus := nullBus{}
	for _, b := range beads {
		resolved := daemon.ExportedResolveHarness(
			ctx,
			b,
			core.AgentType(""), // queue default absent (tier 2 stub)
			core.AgentType(""), // node default absent (tier 3)
			globalDefault,
			bus,
		)
		result[b.BeadID] = resolved
	}
	return result
}

// formatDryRunHarnessLine produces the per-item line a dry-run harness summary
// would display:
//
//	hk-abc123  harness=codex    (label: harness:codex)
//	hk-def456  harness=claude-code  (global default)
func formatDryRunHarnessLine(bead core.BeadRecord, resolved core.AgentType) string {
	// Detect whether tier 1 (bead label) was the source.
	labelSource := "(global default)"
	for _, lbl := range bead.Labels {
		if strings.HasPrefix(lbl, "harness:") {
			labelSource = fmt.Sprintf("(label: %s)", lbl)
			break
		}
	}
	return fmt.Sprintf("  %-12s  harness=%-12s  %s",
		string(bead.BeadID), string(resolved), labelSource)
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests
// ─────────────────────────────────────────────────────────────────────────────

// TestHarnessSelectionDryRun_CodexLabelOverridesGlobalDefault is the primary
// exploratory test: a single bead with "harness:codex" + global default
// AgentTypeClaudeCode resolves to AgentTypeCodex.
func TestHarnessSelectionDryRun_CodexLabelOverridesGlobalDefault(t *testing.T) {
	t.Parallel()

	bead := harnessSelectionDryRunFixtureBead("hk-codex-001", []string{"area:core", "harness:codex"})
	result := resolveHarnessPerItem(t.Context(), []core.BeadRecord{bead}, core.AgentTypeClaudeCode)

	got := result[bead.BeadID]
	if got != core.AgentTypeCodex {
		t.Errorf("resolveHarnessPerItem: got %q; want %q (harness:codex label must override global claude-code default)",
			got, core.AgentTypeCodex)
	}
}

// TestHarnessSelectionDryRun_NoLabelFallsToGlobalDefault verifies that a bead
// without a harness label inherits the global default.
func TestHarnessSelectionDryRun_NoLabelFallsToGlobalDefault(t *testing.T) {
	t.Parallel()

	bead := harnessSelectionDryRunFixtureBead("hk-nolabel-001", []string{"area:core"})
	result := resolveHarnessPerItem(t.Context(), []core.BeadRecord{bead}, core.AgentTypeClaudeCode)

	got := result[bead.BeadID]
	if got != core.AgentTypeClaudeCode {
		t.Errorf("resolveHarnessPerItem: got %q; want %q (no harness label → global default claude-code)",
			got, core.AgentTypeClaudeCode)
	}
}

// TestHarnessSelectionDryRun_MixedQueue verifies that a queue with both codex-
// labelled and unlabelled beads resolves each bead independently.
func TestHarnessSelectionDryRun_MixedQueue(t *testing.T) {
	t.Parallel()

	beads := []core.BeadRecord{
		harnessSelectionDryRunFixtureBead("hk-mixed-001", []string{"harness:codex"}),
		harnessSelectionDryRunFixtureBead("hk-mixed-002", []string{"area:queue"}),
		harnessSelectionDryRunFixtureBead("hk-mixed-003", []string{"harness:claude-code"}),
		harnessSelectionDryRunFixtureBead("hk-mixed-004", []string{"size:S"}),
	}
	globalDefault := core.AgentTypeClaudeCode

	result := resolveHarnessPerItem(t.Context(), beads, globalDefault)

	want := map[core.BeadID]core.AgentType{
		"hk-mixed-001": core.AgentTypeCodex,      // tier 1 harness:codex
		"hk-mixed-002": core.AgentTypeClaudeCode, // tier 4 global default
		"hk-mixed-003": core.AgentTypeClaudeCode, // tier 1 harness:claude-code (explicit)
		"hk-mixed-004": core.AgentTypeClaudeCode, // tier 4 global default
	}
	for id, wantHarness := range want {
		if got := result[id]; got != wantHarness {
			t.Errorf("resolveHarnessPerItem[%s]: got %q; want %q", id, got, wantHarness)
		}
	}
}

// TestHarnessSelectionDryRun_PrintPerItem exercises the dry-run output helper:
// the per-item harness line must contain the bead ID, the resolved harness, and
// the label source annotation.
func TestHarnessSelectionDryRun_PrintPerItem(t *testing.T) {
	t.Parallel()

	cases := []struct {
		bead        core.BeadRecord
		global      core.AgentType
		wantHarness string
		wantSource  string
	}{
		{
			bead:        harnessSelectionDryRunFixtureBead("hk-print-001", []string{"harness:codex"}),
			global:      core.AgentTypeClaudeCode,
			wantHarness: "codex",
			wantSource:  "label: harness:codex",
		},
		{
			bead:        harnessSelectionDryRunFixtureBead("hk-print-002", []string{}),
			global:      core.AgentTypeClaudeCode,
			wantHarness: "claude-code",
			wantSource:  "global default",
		},
		{
			bead:        harnessSelectionDryRunFixtureBead("hk-print-003", []string{"harness:codex"}),
			global:      core.AgentTypeCodex,
			wantHarness: "codex",
			wantSource:  "label: harness:codex",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(string(tc.bead.BeadID), func(t *testing.T) {
			t.Parallel()

			result := resolveHarnessPerItem(t.Context(), []core.BeadRecord{tc.bead}, tc.global)
			resolved := result[tc.bead.BeadID]

			line := formatDryRunHarnessLine(tc.bead, resolved)

			if !strings.Contains(line, string(tc.bead.BeadID)) {
				t.Errorf("formatDryRunHarnessLine: missing bead ID %q in %q", tc.bead.BeadID, line)
			}
			if !strings.Contains(line, tc.wantHarness) {
				t.Errorf("formatDryRunHarnessLine: missing harness %q in %q", tc.wantHarness, line)
			}
			if !strings.Contains(line, tc.wantSource) {
				t.Errorf("formatDryRunHarnessLine: missing source annotation %q in %q", tc.wantSource, line)
			}
		})
	}
}

// TestHarnessSelectionDryRun_AllResultsValidAgentType verifies that
// resolveHarnessPerItem always returns a valid AgentType regardless of inputs
// (AR-025 compliance for the dry-run path).
func TestHarnessSelectionDryRun_AllResultsValidAgentType(t *testing.T) {
	t.Parallel()

	beads := []core.BeadRecord{
		harnessSelectionDryRunFixtureBead("hk-ar025-001", nil),
		harnessSelectionDryRunFixtureBead("hk-ar025-002", []string{"harness:codex"}),
		harnessSelectionDryRunFixtureBead("hk-ar025-003", []string{"harness:claude-code"}),
		harnessSelectionDryRunFixtureBead("hk-ar025-004", []string{"harness:INVALID"}),
		harnessSelectionDryRunFixtureBead("hk-ar025-005", []string{"harness:", "harness:codex"}),
	}

	for _, global := range []core.AgentType{core.AgentTypeClaudeCode, core.AgentTypeCodex, core.AgentType("")} {
		result := resolveHarnessPerItem(t.Context(), beads, global)
		for _, b := range beads {
			got := result[b.BeadID]
			if !got.Valid() {
				t.Errorf("resolveHarnessPerItem[%s] global=%q: got invalid AgentType %q (AR-025 violation)",
					b.BeadID, global, got)
			}
		}
	}
}

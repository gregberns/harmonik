package daemon_test

// refresolve_hk30q6_test.go — unit tests for the EM-012a per-bead dot-file
// workflow-ref resolution (resolveWorkflowRef via ExportedResolveWorkflowRef).
//
// Acceptance criteria (hk-30q6):
//   (1) A bead label selects which .dot workflow file the daemon dispatches it
//       through (label-present → routes to the labeled .dot).
//   (2) Absence of a label falls back to the current default (returns "").
//
// These are pure string-resolver tests; no filesystem or daemon is involved.
//
// Bead: hk-30q6.
// Helper prefix: wfRefFixture (per implementer-protocol.md §Helper-prefix
// discipline).

import (
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// ─────────────────────────────────────────────────────────────────────────────
// Fixtures
// ─────────────────────────────────────────────────────────────────────────────

func wfRefFixtureBead(t *testing.T, labels []string) core.BeadRecord {
	t.Helper()
	return core.BeadRecord{
		BeadID:        core.BeadID("hk-30q6-fixture"),
		Title:         "workflow-ref fixture bead",
		BeadType:      "task",
		Status:        core.CoarseStatusOpen,
		Labels:        labels,
		AuditTrailRef: "hk-30q6-fixture",
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests
// ─────────────────────────────────────────────────────────────────────────────

// TestResolveWorkflowRefPrecedence is the primary table-driven test covering:
//   - label-present cases (tier 1): bead dot:<name> label routes to <name>.dot
//   - label-absent cases (tier 2–4): returns "" for caller fallthrough
//   - tier-0 per-item WorkflowRef override wins over any label
func TestResolveWorkflowRefPrecedence(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name            string
		beadLabels      []string
		itemWorkflowRef string // tier-0 per-item ref (from queue.Item.WorkflowRef)
		wantRef         string // "" means "fall through to project default / embedded"
	}{
		// ── Tier 0: per-item WorkflowRef wins over label ─────────────────────
		{
			name:            "tier0 explicit item ref wins over dot label",
			beadLabels:      []string{"dot:doc-safe"},
			itemWorkflowRef: "custom/override.dot",
			wantRef:         "custom/override.dot",
		},
		{
			name:            "tier0 explicit item ref preserved when no label",
			beadLabels:      nil,
			itemWorkflowRef: "strict-cascade.dot",
			wantRef:         "strict-cascade.dot",
		},
		{
			name:            "tier0 absolute item ref preserved",
			beadLabels:      []string{"dot:doc-safe"},
			itemWorkflowRef: "/abs/path/to/workflow.dot",
			wantRef:         "/abs/path/to/workflow.dot",
		},

		// ── Tier 1: per-bead dot:<name> label ────────────────────────────────
		{
			name:       "tier1 dot label with name appends .dot extension",
			beadLabels: []string{"dot:doc-safe"},
			wantRef:    "doc-safe.dot",
		},
		{
			name:       "tier1 dot label with .dot extension preserved as-is",
			beadLabels: []string{"dot:doc-safe.dot"},
			wantRef:    "doc-safe.dot",
		},
		{
			name:       "tier1 dot label with path component",
			beadLabels: []string{"dot:workflows/advisory"},
			wantRef:    "workflows/advisory.dot",
		},
		{
			name:       "tier1 dot label ignores unrelated labels",
			beadLabels: []string{"area:docs", "size:S", "dot:doc-safe", "workflow:dot"},
			wantRef:    "doc-safe.dot",
		},

		// ── Tier 1 edge: empty or ambiguous dot labels → fall through ─────────
		{
			name:       "tier1 empty dot label value falls through",
			beadLabels: []string{"dot:"},
			wantRef:    "",
		},
		{
			name:       "tier1 multiple dot labels falls through",
			beadLabels: []string{"dot:doc-safe", "dot:strict"},
			wantRef:    "",
		},

		// ── Tier 2–4: no dot label → returns "" (caller falls to project/embedded)
		{
			name:       "label absent returns empty string",
			beadLabels: nil,
			wantRef:    "",
		},
		{
			name:       "unrelated labels only returns empty string",
			beadLabels: []string{"area:daemon", "size:M", "workflow:dot"},
			wantRef:    "",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			bead := wfRefFixtureBead(t, tc.beadLabels)
			got := daemon.ExportedResolveWorkflowRef(bead, tc.itemWorkflowRef)

			if got != tc.wantRef {
				t.Errorf("resolveWorkflowRef: got %q; want %q", got, tc.wantRef)
			}
		})
	}
}

// TestResolveWorkflowRef_LabelPresent_RouteToLabeledDot verifies the primary
// acceptance criterion (hk-30q6 §DONE MEANS (1)): a bead with a dot:<name>
// label resolves to <name>.dot, not to the empty string.
func TestResolveWorkflowRef_LabelPresent_RouteToLabeledDot(t *testing.T) {
	t.Parallel()

	bead := wfRefFixtureBead(t, []string{"area:docs", "dot:doc-safe", "workflow:dot"})
	got := daemon.ExportedResolveWorkflowRef(bead, "")

	if got == "" {
		t.Error("resolveWorkflowRef: label-present case returned \"\"; want a non-empty ref (doc-safe.dot)")
	}
	if got != "doc-safe.dot" {
		t.Errorf("resolveWorkflowRef: got %q; want %q", got, "doc-safe.dot")
	}
}

// TestResolveWorkflowRef_LabelAbsent_Fallback verifies the primary acceptance
// criterion (hk-30q6 §DONE MEANS (2)): a bead with no dot: label returns ""
// so the caller falls through to the project-level / embedded default.
func TestResolveWorkflowRef_LabelAbsent_Fallback(t *testing.T) {
	t.Parallel()

	bead := wfRefFixtureBead(t, []string{"area:core", "size:S"})
	got := daemon.ExportedResolveWorkflowRef(bead, "")

	if got != "" {
		t.Errorf("resolveWorkflowRef: label-absent case returned %q; want \"\" (fallback to project default)", got)
	}
}

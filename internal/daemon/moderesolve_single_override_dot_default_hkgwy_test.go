package daemon_test

// moderesolve_single_override_dot_default_hkgwy_test.go — pinning tests for
// the interaction between the v1.0 daemon default (WorkflowModeDot per PL-004a)
// and an explicit per-bead workflow:single label.
//
// # What this pins
//
// EM-012a declares four resolution tiers. At v1.0:
//
//   - Tier 3 (daemon default): WorkflowModeDot — the v1.0 production default
//     per PL-004a; "absence of the field defaults the cached value to `dot`".
//   - Tier 4 (built-in fallback): also `dot` (hk-30vlb).
//
// A bead with an explicit workflow:single label MUST still resolve to `single`
// via tier-1, overriding both the tier-3 daemon default and tier-4 fallback.
// Additionally, the daemon MUST emit the review_bypassed audit event (hk-81n9r)
// whenever workflow:single resolves at tier-1 — the event fires regardless of
// what the daemon default would have been.
//
// The existing tier-1 test in moderesolve_test.go covers the case where the
// daemon default is WorkflowModeReviewLoop. This file covers the new v1.0
// production scenario where the daemon default is WorkflowModeDot.
//
// # Tests
//
//  1. TestResolveWorkflow_SingleLabelOverridesDotDefault — tier-1 resolves to
//     `single` even when the daemon default is `dot`.
//  2. TestResolveWorkflow_SingleLabelEmitsReviewBypassed — the review_bypassed
//     event is emitted with a valid payload when workflow:single fires.
//  3. TestResolveWorkflow_DotDefaultPreservesNonSingleLabels — non-single
//     workflow labels (review-loop, dot) still resolve correctly when the daemon
//     default is `dot`.
//
// # Spec refs
//   - specs/execution-model.md §4.3 EM-012a (four-tier mode-resolution)
//   - specs/process-lifecycle.md §4 PL-004a (daemon default = dot)
//   - hk-81n9r (review_bypassed audit event on workflow:single tier-1 resolve)
//
// Bead: hk-gwy.
// Helper prefix: singleOverrideDot (per implementer-protocol.md §Helper-prefix discipline).

import (
	"encoding/json"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// ─────────────────────────────────────────────────────────────────────────────
// Helpers (singleOverrideDot prefix per bead helper-prefix discipline)
// ─────────────────────────────────────────────────────────────────────────────

// singleOverrideDotBead builds a minimal BeadRecord for hk-gwy fixtures.
func singleOverrideDotBead(t *testing.T, labels []string) core.BeadRecord {
	t.Helper()
	return core.BeadRecord{
		BeadID:        core.BeadID("hk-gwy-fixture"),
		Title:         "hk-gwy fixture bead",
		BeadType:      "task",
		Status:        core.CoarseStatusOpen,
		Labels:        labels,
		AuditTrailRef: "hk-gwy-fixture",
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests
// ─────────────────────────────────────────────────────────────────────────────

// TestResolveWorkflow_SingleLabelOverridesDotDefault verifies that a bead
// carrying the explicit label workflow:single resolves to WorkflowModeSingle at
// tier-1 even when the daemon's WorkflowModeDefault is WorkflowModeDot (the
// v1.0 production default per PL-004a / EM-012a tier-3).
//
// Guards the invariant: single is reachable ONLY via an explicit per-bead label
// (EM-012a); the dot daemon default must NOT prevent tier-1 from firing.
func TestResolveWorkflow_SingleLabelOverridesDotDefault(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		beadLabels []string
	}{
		{
			name:       "workflow:single alone overrides dot daemon default",
			beadLabels: []string{"workflow:single"},
		},
		{
			name:       "workflow:single with unrelated labels overrides dot daemon default",
			beadLabels: []string{"area:daemon", "size:S", "workflow:single", "priority:1"},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			bus := &modeResolveFixtureBus{}
			bead := singleOverrideDotBead(t, tc.beadLabels)
			// v1.0 production daemon default per PL-004a.
			daemonDefault := core.WorkflowModeDot

			got := daemon.ExportedResolveWorkflowMode(t.Context(), bead, daemonDefault, bus)

			if got != core.WorkflowModeSingle {
				t.Errorf("workflow:single label with dot daemon default: got %q, want %q — "+
					"tier-1 must override the tier-3 dot default (EM-012a)", got, core.WorkflowModeSingle)
			}
		})
	}
}

// TestResolveWorkflow_SingleLabelEmitsReviewBypassed verifies that the daemon
// emits the review_bypassed audit event (hk-81n9r) with a valid payload when
// workflow:single resolves at tier-1, regardless of the daemon's default mode.
//
// Checked under both the v1.0 dot daemon default and the historical review-loop
// default to confirm the audit event is unconditional on the daemon default value.
func TestResolveWorkflow_SingleLabelEmitsReviewBypassed(t *testing.T) {
	t.Parallel()

	daemonDefaults := []struct {
		name          string
		daemonDefault core.WorkflowMode
	}{
		{"daemon default = dot (v1.0 production default)", core.WorkflowModeDot},
		{"daemon default = review-loop (historical default)", core.WorkflowModeReviewLoop},
	}

	for _, dd := range daemonDefaults {
		dd := dd
		t.Run(dd.name, func(t *testing.T) {
			t.Parallel()

			bus := &modeResolveFixtureBus{}
			bead := singleOverrideDotBead(t, []string{"workflow:single"})

			got := daemon.ExportedResolveWorkflowMode(t.Context(), bead, dd.daemonDefault, bus)

			if got != core.WorkflowModeSingle {
				t.Fatalf("review_bypassed setup: resolved to %q, want %q", got, core.WorkflowModeSingle)
			}

			// Locate the review_bypassed event.
			events := modeResolveFixtureBusEvents(t, bus)
			var bypassedPayload *core.ReviewBypassedPayload
			for _, e := range events {
				if e.EventType != core.EventTypeReviewBypassed {
					continue
				}
				var pl core.ReviewBypassedPayload
				if err := json.Unmarshal(e.Payload, &pl); err != nil {
					t.Fatalf("review_bypassed payload unmarshal: %v", err)
				}
				bypassedPayload = &pl
				break
			}

			if bypassedPayload == nil {
				t.Errorf("review_bypassed event NOT emitted when workflow:single resolves at tier-1 "+
					"(daemon default = %q); hk-81n9r requires it", dd.daemonDefault)
				return
			}

			// Validate payload shape per ReviewBypassedPayload.Valid().
			if !bypassedPayload.Valid() {
				t.Errorf("review_bypassed payload.Valid() = false; payload = %+v", bypassedPayload)
			}
			if bypassedPayload.BeadID != string(bead.BeadID) {
				t.Errorf("review_bypassed bead_id = %q; want %q", bypassedPayload.BeadID, bead.BeadID)
			}
			if bypassedPayload.Label != "workflow:single" {
				t.Errorf("review_bypassed label = %q; want %q", bypassedPayload.Label, "workflow:single")
			}

			// Confirm no bead_label_conflict fired (single label, valid mode — no conflict).
			for _, e := range events {
				if e.EventType == core.EventTypeBeadLabelConflict {
					t.Error("unexpected bead_label_conflict emitted for a clean workflow:single label")
				}
			}
		})
	}
}

// TestResolveWorkflow_DotDefaultPreservesNonSingleLabels verifies that non-single
// workflow labels (workflow:review-loop, workflow:dot) still resolve correctly
// when the daemon default is WorkflowModeDot. This guards against a regression
// where the dot-default change accidentally flattens tier-1 for non-single labels.
func TestResolveWorkflow_DotDefaultPreservesNonSingleLabels(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		labels   []string
		wantMode core.WorkflowMode
	}{
		{
			name:     "workflow:review-loop label overrides dot daemon default",
			labels:   []string{"workflow:review-loop"},
			wantMode: core.WorkflowModeReviewLoop,
		},
		{
			name:     "workflow:dot label with dot daemon default resolves to dot (no conflict)",
			labels:   []string{"workflow:dot"},
			wantMode: core.WorkflowModeDot,
		},
		{
			name:     "no workflow label with dot daemon default resolves to dot via tier-3",
			labels:   []string{"area:core", "size:M"},
			wantMode: core.WorkflowModeDot,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			bus := &modeResolveFixtureBus{}
			bead := singleOverrideDotBead(t, tc.labels)
			daemonDefault := core.WorkflowModeDot // v1.0 production default

			got := daemon.ExportedResolveWorkflowMode(t.Context(), bead, daemonDefault, bus)

			if got != tc.wantMode {
				t.Errorf("dot daemon default + labels %v: got %q, want %q",
					tc.labels, got, tc.wantMode)
			}

			// review_bypassed must NOT fire for non-single labels.
			for _, e := range modeResolveFixtureBusEvents(t, bus) {
				if e.EventType == core.EventTypeReviewBypassed {
					t.Errorf("unexpected review_bypassed event for labels %v (only workflow:single triggers it)",
						tc.labels)
				}
			}
		})
	}
}

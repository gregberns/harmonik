package daemon_test

import (
	"encoding/json"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

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
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			bus := &modeResolveFixtureBus{}
			bead := singleOverrideDotBead(t, tc.beadLabels)
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
// Checked under both the v1.0 dot daemon default and a stale, now-retired
// review-loop default to confirm the audit event is unconditional on the daemon
// default value — including when that value is no longer a valid mode at all.
func TestResolveWorkflow_SingleLabelEmitsReviewBypassed(t *testing.T) {
	t.Parallel()

	daemonDefaults := []struct {
		name          string
		daemonDefault core.WorkflowMode
	}{
		{"daemon default = dot (v1.0 production default)", core.WorkflowModeDot},
		{"daemon default = retired review-loop (stale, invalid)", core.WorkflowMode(core.WorkflowModeRetiredReviewLoop)},
	}

	for _, dd := range daemonDefaults {
		t.Run(dd.name, func(t *testing.T) {
			t.Parallel()

			bus := &modeResolveFixtureBus{}
			bead := singleOverrideDotBead(t, []string{"workflow:single"})

			got := daemon.ExportedResolveWorkflowMode(t.Context(), bead, dd.daemonDefault, bus)

			if got != core.WorkflowModeSingle {
				t.Fatalf("review_bypassed setup: resolved to %q, want %q", got, core.WorkflowModeSingle)
			}

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

			if !bypassedPayload.Valid() {
				t.Errorf("review_bypassed payload.Valid() = false; payload = %+v", bypassedPayload)
			}
			if bypassedPayload.BeadID != string(bead.BeadID) {
				t.Errorf("review_bypassed bead_id = %q; want %q", bypassedPayload.BeadID, bead.BeadID)
			}
			if bypassedPayload.Label != "workflow:single" {
				t.Errorf("review_bypassed label = %q; want %q", bypassedPayload.Label, "workflow:single")
			}

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
		name         string
		labels       []string
		wantMode     core.WorkflowMode
		wantConflict bool
	}{
		{
			name:         "retired workflow:review-loop label degrades to the dot daemon default",
			labels:       []string{"workflow:review-loop"},
			wantMode:     core.WorkflowModeDot,
			wantConflict: true,
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

			gotConflict := false
			for _, e := range modeResolveFixtureBusEvents(t, bus) {
				if e.EventType == core.EventTypeReviewBypassed {
					t.Errorf("unexpected review_bypassed event for labels %v (only workflow:single triggers it)",
						tc.labels)
				}
				if e.EventType == core.EventTypeBeadLabelConflict {
					gotConflict = true
				}
			}
			if gotConflict != tc.wantConflict {
				t.Errorf("bead_label_conflict emitted = %v for labels %v; want %v",
					gotConflict, tc.labels, tc.wantConflict)
			}
		})
	}
}

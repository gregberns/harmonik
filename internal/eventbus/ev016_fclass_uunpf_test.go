package eventbus

import (
	"testing"

	"github.com/gregberns/harmonik/internal/core"
)

// TestFsyncBoundaryEventTypes_G1_MissingFClassEntries asserts that the 15
// F-class event types identified by the hk-pqgtm conformance audit (G1 gap,
// event-model.md v0.4.0–v0.6.4 additions) are present in
// fsyncBoundaryEventTypes and that isFsyncBoundaryEvent returns true for each.
// Loss of any of these on a hard crash violates EV-016 / EV-INV-002.
//
// Spec ref: specs/event-model.md §4.4 EV-016, EV-INV-002; §8 taxonomy.
// Bead ref: hk-uunpf.
func TestFsyncBoundaryEventTypes_G1_MissingFClassEntries(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		et   core.EventType
	}{
		// §8.1a review-loop lifecycle
		{"reviewer_verdict", core.EventTypeReviewerVerdict},
		{"review_loop_cycle_complete", core.EventTypeReviewLoopCycleComplete},
		// §8.2 control-point lifecycle
		{"policy_expression_exceeded_cost", core.EventTypePolicyExpressionExceededCost},
		{"gate_definition_drift", core.EventTypeGateDefinitionDrift},
		{"gate_redefined_under_cat_6", core.EventTypeGateRedefinedUnderCat6},
		// §8.5 workspace lifecycle
		{"workspace_merge_status", core.EventTypeWorkspaceMergeStatus},
		// §8.10 queue lifecycle
		{"queue_submitted", core.EventTypeQueueSubmitted},
		{"queue_group_completed", core.EventTypeQueueGroupCompleted},
		{"queue_paused", core.EventTypeQueuePaused},
		{"queue_item_reconciled", core.EventTypeQueueItemReconciled},
		// §8.11 handler-pause lifecycle
		{"handler_paused", core.EventTypeHandlerPaused},
		{"handler_resumed", core.EventTypeHandlerResumed},
		// §8.12 daemon escalation
		{"decision_required", core.EventTypeDecisionRequired},
		{"decision_acknowledged", core.EventTypeDecisionAcknowledged},
		// §8.15 beads adapter
		{"bead_sync_failed", core.EventTypeBeadSyncFailed},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, ok := fsyncBoundaryEventTypes[tc.et]; !ok {
				t.Errorf("%q missing from fsyncBoundaryEventTypes (EV-016: must be F-class)", tc.et)
			}
			if !isFsyncBoundaryEvent(tc.et) {
				t.Errorf("isFsyncBoundaryEvent(%q) = false; want true (EV-016 F-class)", tc.et)
			}
		})
	}
}

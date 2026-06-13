package eventbus

import (
	"testing"

	"github.com/gregberns/harmonik/internal/core"
)

// TestDecisionEvents_AreFsyncBoundary asserts that all three hitl-decisions
// terminal/needed events are F-class (fsync-boundary) per SPEC §6 N1 — the
// load-bearing durability guard against Risk R1 (a lost decision_resolved
// leaving the blocked agent waiting forever). This is an in-package test so it
// can read the unexported fsyncBoundaryEventTypes map / isFsyncBoundaryEvent.
//
// Bead ref: hk-33p (component K1).
func TestDecisionEvents_AreFsyncBoundary(t *testing.T) {
	t.Parallel()

	for _, et := range []core.EventType{
		core.EventTypeDecisionNeeded,
		core.EventTypeDecisionResolved,
		core.EventTypeDecisionWithdrawn,
	} {
		et := et
		t.Run(string(et), func(t *testing.T) {
			t.Parallel()
			if _, ok := fsyncBoundaryEventTypes[et]; !ok {
				t.Errorf("%q missing from fsyncBoundaryEventTypes (N1: must be F-class)", et)
			}
			if !isFsyncBoundaryEvent(et) {
				t.Errorf("isFsyncBoundaryEvent(%q) = false; want true (N1 F-class)", et)
			}
		})
	}
}

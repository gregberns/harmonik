package daemon_test

// queue_perqueue_pause_globalflag_tigaf6_test.go — the one NQ-C1 per-queue pause
// assertion that stays in package daemon_test (hk-tigaf.6).
//
// Its six siblings moved to internal/queuewiring with the consumer they drive
// (P2 unit E3a). This one cannot follow: it asserts on
// daemon.OperatorPauseController, which is daemon-owned and is NOT part of the
// extracted queue-ownership layer. Splitting it out is cheaper than dragging
// operatorpause.go across the boundary.
//
// Acceptance criterion (per bead spec):
//   - Named pause does NOT set the global IsPaused flag (br-ready gate unaffected).
//
// Bead ref: hk-tigaf.6.
// Plan ref: plans/2026-07-21-p2-extraction/E3-queue-wiring.md §4 STEP 7.

import (
	"context"
	"testing"

	"github.com/gregberns/harmonik/internal/daemon"
)

// TestPerQueuePause_DoesNotSetGlobalFlag verifies that a per-queue pause does
// NOT set the OperatorPauseController.IsPaused() global flag (the EM-067
// br-ready gate must remain false).
func TestPerQueuePause_DoesNotSetGlobalFlag(t *testing.T) {
	t.Parallel()

	col := &stubEventCollector{}
	ctrl := daemon.ExportedNewOperatorPauseController(col)

	if err := ctrl.HandleOperatorPause(context.Background(), "investigate"); err != nil {
		t.Fatalf("HandleOperatorPause(named): %v", err)
	}

	if ctrl.IsPaused() {
		t.Error("IsPaused() = true after per-queue pause; br-ready gate should remain clear")
	}
}

package daemon_test

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

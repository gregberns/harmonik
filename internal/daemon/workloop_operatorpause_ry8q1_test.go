package daemon_test

// workloop_operatorpause_ry8q1_test.go — operator-pause br-ready gate test (hk-ry8q1).
//
// Acceptance criteria:
//   - When the daemon is operator-paused, the br-ready fallback path does NOT
//     call Ready() (the gate fires before entering the br-ready poll).
//   - After operator resume, the gate releases and Ready() is eventually called.
//
// The test uses a notifying ledger to detect Ready() calls deterministically
// without requiring a full subprocess dispatch (which is an integration concern
// covered by other tests).
//
// This mirrors TestHandlerPause_BrReadyPath_SkipOnPaused in workloop_handlerpause_kac8g_test.go
// but uses the OperatorPauseController rather than HandlerPauseController.
//
// Bead ref: hk-ry8q1.

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// notifyingLedger is a stubBeadLedger wrapper that sends on readyCh whenever
// Ready() is called. Used to detect when the gate releases without requiring
// a full subprocess dispatch.
type notifyingLedger struct {
	readyCh chan<- struct{} // non-blocking send
	inner   *stubBeadLedger
}

func (n *notifyingLedger) Ready(ctx context.Context) ([]core.BeadRecord, error) {
	// Non-blocking send: we just need to know the gate released.
	select {
	case n.readyCh <- struct{}{}:
	default:
	}
	return n.inner.Ready(ctx)
}

func (n *notifyingLedger) ShowBead(ctx context.Context, id core.BeadID) (core.BeadRecord, error) {
	return n.inner.ShowBead(ctx, id)
}

func (n *notifyingLedger) ClaimBead(ctx context.Context, brPath string, cfg brcli.TimeoutConfig, runID core.RunID, tid core.TransitionID, beadID core.BeadID) error {
	return n.inner.ClaimBead(ctx, brPath, cfg, runID, tid, beadID)
}

func (n *notifyingLedger) CloseBead(ctx context.Context, brPath string, cfg brcli.TimeoutConfig, runID core.RunID, tid core.TransitionID, beadID core.BeadID, success bool) error {
	return n.inner.CloseBead(ctx, brPath, cfg, runID, tid, beadID, success)
}

func (n *notifyingLedger) ReopenBead(ctx context.Context, brPath string, cfg brcli.TimeoutConfig, runID core.RunID, tid core.TransitionID, beadID core.BeadID, reason string) error {
	return n.inner.ReopenBead(ctx, brPath, cfg, runID, tid, beadID, reason)
}

// TestOperatorPause_BrReadyPath_HoldOnPaused verifies that when operatorPauseCtrl
// is paused, the br-ready fallback dispatch loop does NOT call Ready(). After
// resume, the gate releases and Ready() is eventually called.
func TestOperatorPause_BrReadyPath_HoldOnPaused(t *testing.T) {
	t.Parallel()

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	readyCh := make(chan struct{}, 1)
	inner := &stubBeadLedger{}
	ledger := &notifyingLedger{readyCh: readyCh, inner: inner}
	bus := &stubEventCollector{}

	ctrl := daemon.ExportedNewOperatorPauseController(bus)
	// Pause BEFORE the loop starts — first dispatch tick must be held.
	if err := ctrl.HandleOperatorPause(context.Background(), ""); err != nil {
		t.Fatalf("HandleOperatorPause: %v", err)
	}

	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:         ledger,
		Bus:               bus,
		ProjectDir:        projectDir,
		HandlerBinary:     "/bin/sh",
		HandlerArgs:       []string{"-c", "exit 0"},
		IntentLogDir:      filepath.Join(projectDir, ".harmonik", "beads-intents"),
		AdapterRegistry2:  NewSealedAdapterRegistryForTest(t),
		OperatorPauseCtrl: ctrl,
		// No QueueStore — uses br-ready fallback path.
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	// Allow several poll ticks; Ready() must NOT be called while paused.
	time.Sleep(250 * time.Millisecond)

	select {
	case <-readyCh:
		t.Fatal("Ready() was called while operator-paused — gate must hold dispatch")
	default:
		// Correct: gate prevented Ready() call.
	}

	// Resume: gate should release and Ready() should be called within the poll interval.
	if err := ctrl.HandleOperatorResume(context.Background(), ""); err != nil {
		t.Fatalf("HandleOperatorResume: %v", err)
	}

	select {
	case <-readyCh:
		// Correct: Ready() was called after resume.
	case <-time.After(5 * time.Second):
		t.Fatal("Ready() was not called within 5s after operator resume")
	}

	cancel()
	select {
	case <-loopDone:
	case <-time.After(3 * time.Second):
		t.Fatal("workloop did not exit after context cancellation")
	}
}

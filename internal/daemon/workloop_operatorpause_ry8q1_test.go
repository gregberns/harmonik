package daemon_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

type notifyingLedger struct {
	readyCh chan<- struct{} // non-blocking send
	inner   *stubBeadLedger
}

func (n *notifyingLedger) Ready(ctx context.Context) ([]core.BeadRecord, error) {
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
	if err := ctrl.HandleOperatorPause(context.Background(), ""); err != nil {
		t.Fatalf("HandleOperatorPause: %v", err)
	}

	deps := daemon.ExportedTestRuntime(daemon.TestRuntimeParams{
		BrAdapter:         ledger,
		Bus:               bus,
		ProjectDir:        projectDir,
		HandlerBinary:     "/bin/sh",
		HandlerArgs:       []string{"-c", "exit 0"},
		IntentLogDir:      filepath.Join(projectDir, ".harmonik", "beads-intents"),
		AdapterRegistry2:  NewSealedAdapterRegistryForTest(t),
		OperatorPauseCtrl: ctrl,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	time.Sleep(250 * time.Millisecond)

	select {
	case <-readyCh:
		t.Fatal("Ready() was called while operator-paused — gate must hold dispatch")
	default:
	}

	if err := ctrl.HandleOperatorResume(context.Background(), ""); err != nil {
		t.Fatalf("HandleOperatorResume: %v", err)
	}

	select {
	case <-readyCh:
	case <-time.After(5 * time.Second):
		t.Fatal("Ready() was not called within 5s after operator resume")
	}

	cancel()
	select {
	case <-loopDone:
	case <-time.After(daemon.ExportedDaemonExitHangBudget):
		t.Fatalf("workloop did not exit within %s after context cancellation", daemon.ExportedDaemonExitHangBudget)
	}
}

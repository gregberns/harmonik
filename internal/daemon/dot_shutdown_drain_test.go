package daemon_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
)

// These fixtures run the production DOT shutdown branch in beadRunOne. They
// are local by construction. dotFixtureOpts.Runner does not create a remote
// run: workloop.go takes that seam only when rbc is nil, and preMergeSync then
// correctly does nothing. The SSH worker drain proof is in the integration-
// gated scenario_remote_substrate_localhost_dot_test.go fixture.
func TestDotShutdownDrain_MergesCommittedRun(t *testing.T) {
	const beadID = core.BeadID("hk-dk0sf-committed-dot-run")
	marker := filepath.Join(t.TempDir(), "commit-ready")
	trace := &dotShutdownDrainTrace{}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	go dotShutdownDrainCancelAfterMarker(ctx, cancel, marker)

	result := runDotFixtureBead(t, beadID, dotFixtureOpts{
		RunContext:          ctx,
		WaitForRunTerminal:  true,
		ObserveTerminalStep: trace.record,
		HandlerScript: dotFixtureHandlerScript(t, "commit-then-wait.sh",
			dotFixtureCommitLines(beadID)+"touch "+marker+"\nwhile :; do sleep 1; done\n"),
	})

	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("handler did not commit before cancellation: marker stat: %v", err)
	}
	if closed := result.Ledger.closedIDs(); len(closed) != 1 || closed[0] != beadID {
		t.Fatalf("committed DOT run closed=%v, reopened=%v; want only %s closed", closed, result.Ledger.reopenedIDs(), beadID)
	}
	if reopened := result.Ledger.reopenedIDs(); len(reopened) != 0 {
		t.Fatalf("committed DOT run reopened=%v; want no reopen", reopened)
	}
	if _, err := os.Stat(filepath.Join(result.ProjectDir, "fixture-work.txt")); err != nil {
		t.Fatalf("committed DOT work was not merged into the target repository: %v", err)
	}
	if got, want := trace.snapshot(), []string{"close", string(core.EventTypeBeadClosed), string(core.EventTypeRunCompleted)}; !reflect.DeepEqual(got, want) {
		t.Errorf("shutdown-drain terminal order = %v, want %v", got, want)
	}
}

func TestDotShutdownDrain_CloseFailureReopensThenFails(t *testing.T) {
	const beadID = core.BeadID("hk-dk0sf-drain-close-failure")
	marker := filepath.Join(t.TempDir(), "commit-ready")
	trace := &dotShutdownDrainTrace{}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	go dotShutdownDrainCancelAfterMarker(ctx, cancel, marker)

	result := runDotFixtureBead(t, beadID, dotFixtureOpts{
		RunContext:          ctx,
		CloseError:          errors.New("ledger close refused"),
		WaitForRunTerminal:  true,
		ObserveTerminalStep: trace.record,
		HandlerScript: dotFixtureHandlerScript(t, "commit-then-wait.sh",
			dotFixtureCommitLines(beadID)+"touch "+marker+"\nwhile :; do sleep 1; done\n"),
	})

	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("handler did not commit before cancellation: marker stat: %v", err)
	}
	if closed := result.Ledger.closedIDs(); len(closed) != 0 {
		t.Fatalf("drain close failure closed=%v, want no closed bead", closed)
	}
	if reopened := result.Ledger.reopenedIDs(); len(reopened) != 1 || reopened[0] != beadID {
		t.Fatalf("drain close failure reopened=%v, want only %s", reopened, beadID)
	}
	if got, want := trace.snapshot(), []string{"close_failed", "reopen", string(core.EventTypeRunFailed)}; !reflect.DeepEqual(got, want) {
		t.Errorf("shutdown-drain failure order = %v, want %v", got, want)
	}
}

type dotShutdownDrainTrace struct {
	mu    sync.Mutex
	steps []string
}

func (t *dotShutdownDrainTrace) record(step string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.steps = append(t.steps, step)
}

func (t *dotShutdownDrainTrace) snapshot() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]string(nil), t.steps...)
}

func dotShutdownDrainCancelAfterMarker(ctx context.Context, cancel context.CancelFunc, marker string) {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := os.Stat(marker); err == nil {
			cancel()
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

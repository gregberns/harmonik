package daemon_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
)

func TestDotShutdownDrain_MergesCommittedRun(t *testing.T) {
	const beadID = core.BeadID("hk-dk0sf-committed-dot-run")
	marker := filepath.Join(t.TempDir(), "commit-ready")
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	go dotShutdownDrainCancelAfterMarker(ctx, cancel, marker)

	result := runDotFixtureBead(t, beadID, dotFixtureOpts{
		RunContext: ctx,
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

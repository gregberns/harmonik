package daemon_test

// workloop_stalecloser_integration_hk5t0s6_test.go — integration test for
// autoCloseStaleBlockersOnClaimFailure with the workloop claim-failure path.
//
// Verifies the end-to-end flow: ClaimBead fails (non-blocked error) ->
// autoCloseStaleBlockersOnClaimFailure fires -> stale blocker is auto-closed ->
// next retry's ClaimBead succeeds -> handler runs to completion.
//
// Bead ref: hk-5t0s6.

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/queue"
)

// ---------------------------------------------------------------------------
// Stubs
// ---------------------------------------------------------------------------

// hk5t0s6Ledger is a beadLedger stub where the first ClaimBead call for the
// target bead fails with a transient (non-"blocked") error, and subsequent
// calls succeed. ShowBead tracks call count per bead: the first call for the
// target returns CoarseStatusOpen (so the queue-path isBlocked guard passes
// through), while subsequent calls return CoarseStatusBlocked with dependency
// edges (so autoCloseStaleBlockersOnClaimFailure can find the stale blocker).
type hk5t0s6Ledger struct {
	mu               sync.Mutex
	targetID         core.BeadID
	blockerID        core.BeadID
	claimAttempts    int
	showBeadCalls    int // calls to ShowBead for targetID
	claimed          []core.BeadID
	closed           []core.BeadID
	reopened         []core.BeadID
}

func (l *hk5t0s6Ledger) Ready(_ context.Context) ([]core.BeadRecord, error) {
	return nil, nil
}

func (l *hk5t0s6Ledger) ShowBead(_ context.Context, id core.BeadID) (core.BeadRecord, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if id == l.targetID {
		l.showBeadCalls++
		if l.showBeadCalls == 1 {
			// First ShowBead call: the queue-path isBlocked guard (hk-n91y0)
			// checks status after ClaimBead fails. Return Open so the guard
			// does not intercept the error — letting it fall through to the
			// retry path where autoCloseStaleBlockersOnClaimFailure runs.
			return core.BeadRecord{
				BeadID:        l.targetID,
				Title:         "target bead for stale-blocker integration",
				BeadType:      "task",
				Status:        core.CoarseStatusOpen,
				AuditTrailRef: string(l.targetID),
				Edges: []core.DependencyEdge{
					{
						FromBeadID: l.blockerID,
						ToBeadID:   l.targetID,
						EdgeKind:   core.EdgeKindBlocks,
					},
				},
			}, nil
		}
		// Subsequent calls (from autoCloseStaleBlockersOnClaimFailure and
		// later): return Blocked so the function finds dependency edges and
		// checks beadAlreadySubsumedInMain for each blocker.
		return core.BeadRecord{
			BeadID:        l.targetID,
			Title:         "target bead for stale-blocker integration",
			BeadType:      "task",
			Status:        core.CoarseStatusBlocked,
			AuditTrailRef: string(l.targetID),
			Edges: []core.DependencyEdge{
				{
					FromBeadID: l.blockerID,
					ToBeadID:   l.targetID,
					EdgeKind:   core.EdgeKindBlocks,
				},
			},
		}, nil
	}
	return core.BeadRecord{BeadID: id, Status: core.CoarseStatusOpen}, nil
}

func (l *hk5t0s6Ledger) ClaimBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, id core.BeadID) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if id == l.targetID {
		l.claimAttempts++
		if l.claimAttempts == 1 {
			// First attempt: transient failure. The error message must NOT
			// contain "blocked" so the workloop's isBlocked guard (hk-n91y0)
			// does not intercept it and the retry path with
			// autoCloseStaleBlockersOnClaimFailure is reached.
			return errors.New("claim conflict: another run claimed first")
		}
	}
	// Subsequent attempts succeed.
	l.claimed = append(l.claimed, id)
	return nil
}

func (l *hk5t0s6Ledger) CloseBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, id core.BeadID, _ bool) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.closed = append(l.closed, id)
	return nil
}

func (l *hk5t0s6Ledger) ReopenBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, id core.BeadID, _ string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.reopened = append(l.reopened, id)
	return nil
}

func (l *hk5t0s6Ledger) claimedIDs() []core.BeadID {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]core.BeadID, len(l.claimed))
	copy(out, l.claimed)
	return out
}

// ---------------------------------------------------------------------------
// Test
// ---------------------------------------------------------------------------

// TestWorkLoop_AutoCloseStaleBlockersIntegration verifies the end-to-end
// integration between the workloop's claim-failure retry path and
// autoCloseStaleBlockersOnClaimFailure:
//
//  1. A queue with a single bead is dispatched.
//  2. First ClaimBead fails with a transient error (claim conflict).
//  3. autoCloseStaleBlockersOnClaimFailure fires, finds the blocker subsumed in
//     main (git log shows "Refs: <blockerID>"), and calls SweepCloseBead.
//  4. The workloop reverts the item to pending and retries.
//  5. Second ClaimBead succeeds.
//  6. Handler runs and exits (no-commit path -> ReopenBead).
//
// Assertions:
//   - The sweepCloseRecorder captured the blocker ID.
//   - ClaimBead was called at least twice for the target.
//   - The workloop exits cleanly.
//
// Bead ref: hk-5t0s6.
func TestWorkLoop_AutoCloseStaleBlockersIntegration(t *testing.T) {
	t.Parallel()

	const (
		targetID  = core.BeadID("hk-5t0s6-target")
		blockerID = core.BeadID("hk-5t0s6-blocker")
	)

	// Set up project dir and git repo with a commit referencing the blocker so
	// beadAlreadySubsumedInMain returns true for it.
	projectDir, _ := workloopFixtureProjectDir(t)
	hk5t0s6GitSetup(t, projectDir, string(blockerID))

	now := time.Now()
	q := &queue.Queue{
		SchemaVersion: 1,
		QueueID:       "5t0s6-integration-test",
		SubmittedAt:   now,
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindWave,
				Status:     queue.GroupStatusActive,
				Items: []queue.Item{
					{BeadID: targetID, Status: queue.ItemStatusPending},
				},
				CreatedAt: now,
			},
		},
	}

	qs := daemon.ExportedNewQueueStore()
	qs.SetQueue(q)

	ledger := &hk5t0s6Ledger{targetID: targetID, blockerID: blockerID}
	bus := &stubEventCollector{}
	recorder := &sweepCloseRecorder{}

	exitCtx, cancelExit := context.WithCancel(context.Background())

	p := daemon.WorkLoopDepsParams{
		BrAdapter:          ledger,
		Bus:                bus,
		ProjectDir:         projectDir,
		HandlerBinary:      "/bin/sh",
		HandlerArgs:        []string{"-c", "exit 0"},
		IntentLogDir:       filepath.Join(projectDir, ".harmonik", "beads-intents"),
		QueueStore:         qs,
		MaxConcurrent:      1,
		AdapterRegistry2:   NewSealedAdapterRegistryForTest(t),
		StaleBlockerCloser: recorder,
		CancelOnQueueExit:  cancelExit,
	}
	deps := daemon.ExportedWorkLoopDeps(p)

	testCtx, testCancel := context.WithTimeout(exitCtx, 30*time.Second)
	defer testCancel()

	loopDone := make(chan error, 1)
	go func() {
		loopDone <- daemon.ExportedRunWorkLoop(testCtx, deps)
	}()

	select {
	case err := <-loopDone:
		if err != nil {
			t.Errorf("runWorkLoop returned non-nil error: %v", err)
		}
	case <-time.After(25 * time.Second):
		t.Fatal("runWorkLoop did not exit within 25s — claim-failure retry with stale-blocker auto-close may be stuck")
	}

	// Verify the stale blocker was auto-closed via SweepCloseBead.
	closedIDs := recorder.closedIDs()
	if len(closedIDs) == 0 {
		t.Fatal("SweepCloseBead was never called — autoCloseStaleBlockersOnClaimFailure did not fire or did not find the subsumed blocker")
	}
	foundBlocker := false
	for _, id := range closedIDs {
		if id == blockerID {
			foundBlocker = true
		}
	}
	if !foundBlocker {
		t.Errorf("SweepCloseBead calls: want %s among closed IDs, got %v", blockerID, closedIDs)
	}

	// Verify ClaimBead was retried and succeeded on the second attempt.
	claimed := ledger.claimedIDs()
	foundTarget := false
	for _, id := range claimed {
		if id == targetID {
			foundTarget = true
		}
	}
	if !foundTarget {
		t.Errorf("target bead %s was never successfully claimed on retry; claimed=%v", targetID, claimed)
	}

	// Verify the queue reached a terminal state (paused-by-failure because the
	// handler makes no commit -> no-commit guard reopens the bead -> item fails).
	finalQ := daemon.ExportedQueueStoreOf(deps).Queue()
	if finalQ != nil {
		if finalQ.Status != queue.QueueStatusPausedByFailure {
			t.Errorf("queue.Status = %q; want paused-by-failure (no-commit handler -> item failure)", finalQ.Status)
		}
	}
}

// ---------------------------------------------------------------------------
// Git helpers
// ---------------------------------------------------------------------------

// hk5t0s6GitSetup initialises a git repo in dir with a commit on main that
// carries "Refs: <beadID>" so beadAlreadySubsumedInMain returns true for that ID.
func hk5t0s6GitSetup(t *testing.T, dir string, beadID string) {
	t.Helper()
	hk5t0s6Git(t, dir, "init", "--initial-branch=main")
	hk5t0s6Git(t, dir, "config", "user.email", "test@test.com")
	hk5t0s6Git(t, dir, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(dir, "init.txt"), []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}
	hk5t0s6Git(t, dir, "add", ".")
	hk5t0s6Git(t, dir, "commit", "-m", "fix\n\nRefs: "+beadID)
}

func hk5t0s6Git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

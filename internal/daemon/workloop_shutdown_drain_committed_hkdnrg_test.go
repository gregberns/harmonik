package daemon_test

// workloop_shutdown_drain_committed_hkdnrg_test.go — verifies that a
// committed-but-unmerged in-flight run is drained (merged + bead closed) on
// daemon shutdown rather than abandoned and re-dispatched.
//
// Setup: the worktree factory pre-commits a change so HEAD advances past
// headSHA before the handler binary launches. A long-running handler
// (sleep 60) stays alive while the test cancels the context (simulating
// SIGTERM). The shutdown path must detect the commit and drain to merge.
//
// Done means:
//   - bead is closed (CloseBead called), NOT reopened
//   - run_completed event emitted with success=true
//
// Helper prefix: drainDnrg (per implementer-protocol.md §Helper-prefix;
// bead hk-dnrg).
//
// Bead ref: hk-dnrg, hk-k0eg.

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// ─────────────────────────────────────────────────────────────────────────────
// drainDnrgLedger — tracks claim/close/reopen calls for the drain test.
// ─────────────────────────────────────────────────────────────────────────────

type drainDnrgLedger struct {
	mu      sync.Mutex
	ready   []core.BeadID
	closed  []core.BeadID
	opened  []core.BeadID
	claimed chan struct{}
	once    sync.Once
}

func (l *drainDnrgLedger) Ready(_ context.Context) ([]core.BeadRecord, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.ready) == 0 {
		return nil, nil
	}
	id := l.ready[0]
	l.ready = l.ready[1:]
	return []core.BeadRecord{{BeadID: id}}, nil
}

func (l *drainDnrgLedger) ShowBead(_ context.Context, id core.BeadID) (core.BeadRecord, error) {
	return core.BeadRecord{BeadID: id, Status: core.CoarseStatusOpen}, nil
}

func (l *drainDnrgLedger) ClaimBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID) error {
	l.once.Do(func() { close(l.claimed) })
	return nil
}

func (l *drainDnrgLedger) CloseBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, id core.BeadID, _ bool) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.closed = append(l.closed, id)
	return nil
}

func (l *drainDnrgLedger) ReopenBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, id core.BeadID, _ string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.opened = append(l.opened, id)
	return nil
}

func (l *drainDnrgLedger) closedIDs() []core.BeadID {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]core.BeadID, len(l.closed))
	copy(out, l.closed)
	return out
}

func (l *drainDnrgLedger) reopenedIDs() []core.BeadID {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]core.BeadID, len(l.opened))
	copy(out, l.opened)
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// Test
// ─────────────────────────────────────────────────────────────────────────────

// TestWorkLoop_ShutdownDrainsCommittedRun_hkdnrg verifies that a run which
// has already committed in its worktree is drained (merged + closed) rather
// than abandoned when the daemon context is cancelled (F56 / hk-dnrg).
func TestWorkLoop_ShutdownDrainsCommittedRun_hkdnrg(t *testing.T) {
	t.Parallel()

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	const beadID = core.BeadID("drain-dnrg-committed-001")
	ledger := &drainDnrgLedger{
		ready:   []core.BeadID{beadID},
		claimed: make(chan struct{}),
	}
	collector := &stubEventCollector{}

	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:    ledger,
		Bus:          collector,
		ProjectDir:   projectDir,
		HandlerBinary: "/bin/sh",
		// Long-running handler: stays alive while the test cancels the context.
		// The shutdown path must detect the pre-committed worktree HEAD and drain.
		HandlerArgs:      []string{"-c", "sleep 60"},
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		// workloopFixturePreCommitWorktreeFactory creates a real git worktree and
		// commits a dummy file so HEAD advances past headSHA before the handler
		// launches — this is the "committed-but-unmerged" scenario.
		WorktreeFactory: workloopFixturePreCommitWorktreeFactory,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	loopDone := make(chan error, 1)
	go func() {
		loopDone <- daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	// Wait for the bead to be claimed, confirming the worktree factory has run
	// and the pre-commit exists in the worktree.
	select {
	case <-ledger.claimed:
		t.Log("bead claimed — worktree commit is in place")
	case <-time.After(20 * time.Second):
		t.Fatal("timed out waiting for bead to be claimed")
	}

	// Give the handler a moment to launch so the session is fully running before
	// we simulate shutdown.
	time.Sleep(300 * time.Millisecond)

	// Simulate daemon shutdown by cancelling the context.
	cancel()

	// Wait for the workloop to exit. The drain path must complete the merge
	// within shutdownDrainTimeout (10s) plus stopHookGrace (3s) plus margin.
	select {
	case err := <-loopDone:
		if err != nil {
			t.Fatalf("workloop returned unexpected error: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("workloop did not exit within 30s after context cancellation")
	}

	// Assert: bead must be CLOSED (drained), not reopened.
	closedIDs := ledger.closedIDs()
	reopenedIDs := ledger.reopenedIDs()
	if len(closedIDs) == 0 {
		t.Fatalf("bead was not closed on shutdown-drain; reopened=%v", reopenedIDs)
	}
	if closedIDs[0] != beadID {
		t.Errorf("closed bead = %q; want %q", closedIDs[0], beadID)
	}
	if len(reopenedIDs) > 0 {
		t.Errorf("unexpected ReopenBead calls on shutdown-drain: %v", reopenedIDs)
	}

	// Assert: run_completed with success=true was emitted.
	foundSuccess := false
	for _, ev := range collector.allEvents() {
		if ev.EventType != string(core.EventTypeRunCompleted) {
			continue
		}
		var payload struct {
			Success bool   `json:"success"`
			Summary string `json:"summary"`
		}
		if jsonErr := json.Unmarshal(ev.Payload, &payload); jsonErr != nil {
			continue
		}
		if payload.Success && strings.Contains(payload.Summary, "shutdown-drain") {
			foundSuccess = true
			break
		}
	}
	if !foundSuccess {
		evTypes := collector.eventTypes()
		t.Errorf("run_completed with success=true and shutdown-drain summary not found; events: %v", evTypes)
	}
}

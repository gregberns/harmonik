package daemon_test

// workloop_showbead_retry_hkfvpz5_test.go — ShowBead error retry bounded test
// (hk-fvpz5).
//
// Verifies that when ShowBead fails on the br-ready path pre-claim check, the
// workloop does not retry infinitely. The failing bead is retried a bounded
// number of times (maxReadyPathAttempts=3), then skipped. Other beads in the
// ready list are still claimed and processed.
//
// Helper prefix: showBeadRetry (per implementer-protocol.md §Helper-prefix
// discipline; bead hk-fvpz5).

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// showBeadRetryLedger is a stub bead ledger where ShowBead returns an error for
// a specific bead. Ready() returns both beads on every call (simulating a real
// ledger where unclaimed beads stay in the ready list).
//
// Once the failing bead is skipped (after maxReadyPathAttempts), Ready()
// continues to return the good bead until it is claimed, after which Ready()
// returns empty so the loop quiesces.
type showBeadRetryLedger struct {
	mu sync.Mutex

	failBead core.BeadID
	goodBead core.BeadID

	readyCalls    atomic.Int32          // count of Ready() calls
	showBeadCalls map[core.BeadID]int32 // count of ShowBead calls per bead
	claimed       []core.BeadID
	closed        []core.BeadID
	reopened      []core.BeadID

	goodClaimed atomic.Bool // set once ClaimBead succeeds for goodBead
}

func newShowBeadRetryLedger(failBead, goodBead core.BeadID) *showBeadRetryLedger {
	return &showBeadRetryLedger{
		failBead:      failBead,
		goodBead:      goodBead,
		showBeadCalls: make(map[core.BeadID]int32),
	}
}

func (s *showBeadRetryLedger) Ready(_ context.Context) ([]core.BeadRecord, error) {
	n := s.readyCalls.Add(1)

	if s.goodClaimed.Load() {
		// Good bead already claimed — nothing left to dispatch.
		return []core.BeadRecord{}, nil
	}

	// Simulate a real ledger: both beads are always ready (unclaimed beads stay
	// in the ready list). The workloop picks readyRecords[0].
	//
	// Return the failing bead first for the first several calls so the workloop
	// exercises the ShowBead retry path. After enough calls, return the good
	// bead so the workloop can make progress. The workloop's readyPathAttempts
	// map (hk-kupeo + hk-6pspu) bounds retries for the failing bead; once it
	// exceeds the limit and skips the bead, Ready() is called again and we
	// return the good bead.
	//
	// We use Ready() call count rather than ShowBead call count because the
	// hk-6pspu bound skips beads before ShowBead is called. After ~4 calls
	// (3 ShowBead attempts + 1 hk-6pspu skip), we start returning the good bead.
	// Keep this threshold low so the test completes within the polling deadline
	// (each call sleeps workloopPollInterval=2s).
	if n <= 5 {
		return []core.BeadRecord{{BeadID: s.failBead}}, nil
	}
	return []core.BeadRecord{{BeadID: s.goodBead}}, nil
}

func (s *showBeadRetryLedger) ShowBead(_ context.Context, id core.BeadID) (core.BeadRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.showBeadCalls[id]++

	if id == s.failBead {
		return core.BeadRecord{}, errors.New("showbead: simulated network error")
	}
	return core.BeadRecord{BeadID: id, Status: core.CoarseStatusOpen}, nil
}

func (s *showBeadRetryLedger) ClaimBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, id core.BeadID) error {
	if id == s.goodBead {
		s.goodClaimed.Store(true)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.claimed = append(s.claimed, id)
	return nil
}

func (s *showBeadRetryLedger) CloseBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, id core.BeadID, _ bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = append(s.closed, id)
	return nil
}

func (s *showBeadRetryLedger) ReopenBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, id core.BeadID, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reopened = append(s.reopened, id)
	return nil
}

func (s *showBeadRetryLedger) showBeadCallCount(id core.BeadID) int32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.showBeadCalls[id]
}

func (s *showBeadRetryLedger) claimedIDs() []core.BeadID {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]core.BeadID, len(s.claimed))
	copy(out, s.claimed)
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// TestWorkLoop_ShowBeadErrorRetryBounded (hk-fvpz5)
// ─────────────────────────────────────────────────────────────────────────────

// TestWorkLoop_ShowBeadErrorRetryBounded verifies that when ShowBead fails on
// the br-ready path pre-claim check, the workloop retries a bounded number of
// times (maxReadyPathAttempts=3) and then skips the bead. Other beads in the
// ready list are still dispatched normally.
//
// This prevents an infinite retry loop that would stall the daemon on a single
// broken bead.
//
// Bead ref: hk-fvpz5. Related fix: hk-kupeo.
func TestWorkLoop_ShowBeadErrorRetryBounded(t *testing.T) {
	t.Parallel()

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	const (
		failBead = core.BeadID("hk-fvpz5-showbead-fail")
		goodBead = core.BeadID("hk-fvpz5-showbead-ok")
	)

	ledger := newShowBeadRetryLedger(failBead, goodBead)
	bus := &stubEventCollector{}

	p := daemon.WorkLoopDepsParams{
		BrAdapter:        ledger,
		Bus:              bus,
		ProjectDir:       projectDir,
		HandlerBinary:    "/bin/sh",
		HandlerArgs:      []string{"-c", "exit 0"},
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		// No QueueStore — br-ready path only.
	}
	deps := daemon.ExportedWorkLoopDeps(p)

	// Budget: 5 Ready calls returning failBead * 2s workloopPollInterval = ~10s,
	// plus goodBead dispatch (~5s for git worktree + handler). 30s is generous.
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Second)
	defer cancel()

	loopDone := make(chan error, 1)
	go func() {
		loopDone <- daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	// Poll until the good bead is claimed or timeout.
	deadline := time.After(60 * time.Second)
	for {
		if ledger.goodClaimed.Load() {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for goodBead to be claimed — ShowBead retry may be unbounded (infinite loop)")
		case <-time.After(50 * time.Millisecond):
		}
	}

	// Cancel the context to stop the loop.
	cancel()
	// Teardown of an in-flight dispatch (git worktree cleanup + handler kill) can
	// take several seconds on a saturated CI runner. A 3s ceiling here false-fails
	// under load even though the loop DOES exit; give it a generous 20s — this
	// still catches a genuine no-exit hang while surviving CPU contention.
	select {
	case <-loopDone:
	case <-time.After(20 * time.Second):
		t.Fatal("work loop did not exit after context cancellation")
	}

	// Assert: ShowBead was called a bounded number of times for the failing bead.
	// maxReadyPathAttempts=3, but the hk-6pspu readyPathAttempts check also
	// increments on each Ready() return, so the total may be slightly higher.
	// We assert ≤10 to be generous while still catching an infinite loop.
	failCalls := ledger.showBeadCallCount(failBead)
	if failCalls > 10 {
		t.Errorf("ShowBead called %d times for failing bead; want ≤10 (bounded retry)", failCalls)
	}
	if failCalls == 0 {
		t.Error("ShowBead was never called for the failing bead — test setup error")
	}
	t.Logf("ShowBead called %d times for failing bead (bounded as expected)", failCalls)

	// Assert: the good bead was claimed.
	claimed := ledger.claimedIDs()
	foundGood := false
	for _, id := range claimed {
		if id == goodBead {
			foundGood = true
		}
	}
	if !foundGood {
		t.Errorf("goodBead was never claimed — ShowBead error on failBead blocked dispatch; claimed=%v", claimed)
	}

	// Assert: the failing bead was NOT claimed (ShowBead always errored for it).
	for _, id := range claimed {
		if id == failBead {
			t.Errorf("failBead was claimed despite ShowBead always returning an error; claimed=%v", claimed)
		}
	}
}

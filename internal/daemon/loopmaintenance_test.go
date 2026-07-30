package daemon

import (
	"context"
	"testing"
	"time"

	ltmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// recordingReapAdapter is an ltmux.Adapter that records whether the coordinator
// reap reached tmux.
//
// The embedded interface is nil on purpose. Only ListSessions is implemented,
// because that is the first and only adapter method reapDeadCoordinatorSession
// calls before it decides there is nothing to kill. Any OTHER method the code
// under test starts calling panics on the nil interface, which is the outcome we
// want: a silent new tmux call in a maintenance pass should fail this test, not
// be absorbed by a permissive stub.
type recordingReapAdapter struct {
	ltmux.Adapter
	listSessionsCalls int
}

// ListSessions records the call and reports no live sessions, so the reap finds
// nothing to kill and never reaches KillSession.
func (a *recordingReapAdapter) ListSessions(context.Context) ([]string, error) {
	a.listSessionsCalls++
	return nil, nil
}

// haltFixture builds the deps and maintenance state the two subtests share.
//
// projectDir is a real temp dir with no coordinator sentinel file in it, so
// probeCoordinatorSentinel reports "not live" and the reap proceeds to the
// adapter. That is what makes the negative assertion below meaningful: the reap
// WOULD call tmux on this fixture, so observing no call proves the short-circuit
// rather than proving the fixture is inert.
//
// diskFreeBytesFunc is injected well above the watermark so the disk job takes
// its healthy path here. The disk-LOW branch is covered separately by
// TestDiskLowBranch below.
func haltFixture(t *testing.T) (*workLoopDeps, *recordingReapAdapter) {
	t.Helper()
	adapter := &recordingReapAdapter{}
	deps := &workLoopDeps{
		projectDir:              t.TempDir(),
		coordinatorReapAdapter:  adapter,
		coordinatorReapInterval: time.Nanosecond, // cadence is never the reason a call is skipped
		diskFreeBytesFunc:       func(string) (uint64, error) { return 1 << 62, nil },
	}
	return deps, adapter
}

// TestTickBeforeDispatchHaltShortCircuits pins the invariant loopmaintenance.go
// calls load-bearing: when the governor has an ARMED halt, tickBeforeDispatch
// reports it and runs NOTHING else on that tick.
//
// The "nothing else" half is the point of the test, not a bonus. The ordering
// comment in tickBeforeDispatch claims the halt check short-circuits the pass, and
// before this test nothing executable held that claim. The halt path had no
// coverage at all: scenario_flywheel_bt5_hk5pcr_test.go reaches sentinel.Evaluate
// returning ActivationHalt and stops there, never reaching onHalt, halted(), or
// the loop's exit.
func TestTickBeforeDispatchHaltShortCircuits(t *testing.T) {
	t.Run("halted governor reports halt and skips every other job", func(t *testing.T) {
		deps, adapter := haltFixture(t)
		m := &loopMaintenance{governor: &movementGovernor{haltRequested: true}}

		obs := m.tickBeforeDispatch(context.Background(), deps)

		if !obs.halt {
			t.Fatalf("armed governor halt: want observation.halt = true, got %+v", obs)
		}
		// The load-bearing assertion: the reap must not have run.
		if adapter.listSessionsCalls != 0 {
			t.Errorf("halt tick ran the coordinator reap: ListSessions called %d times, want 0",
				adapter.listSessionsCalls)
		}
		// The reap's clock must be untouched too. A stamped clock would mean the
		// job ran even if the adapter somehow was not reached.
		if !m.state.lastCoordinatorReap.IsZero() {
			t.Errorf("halt tick stamped lastCoordinatorReap = %v, want zero",
				m.state.lastCoordinatorReap)
		}
		// Same for the disk job: no probe, so no latch and no clock.
		if !m.state.lastDiskCheck.IsZero() {
			t.Errorf("halt tick ran the disk check: lastDiskCheck = %v, want zero",
				m.state.lastDiskCheck)
		}
		if obs.diskLow {
			t.Error("halt tick reported diskLow; the halt observation carries halt only")
		}
	})

	// Positive control. Without this, the assertions above would still pass if the
	// fixture simply could not reach tmux, and the test would prove nothing.
	t.Run("same fixture without the halt does run the reap", func(t *testing.T) {
		deps, adapter := haltFixture(t)
		m := &loopMaintenance{governor: &movementGovernor{haltRequested: false}}

		obs := m.tickBeforeDispatch(context.Background(), deps)

		if obs.halt {
			t.Error("no armed halt: want observation.halt = false")
		}
		if adapter.listSessionsCalls != 1 {
			t.Fatalf("unhalted tick: ListSessions called %d times, want 1 — "+
				"the negative subtest above is only meaningful if this fixture reaches tmux",
				adapter.listSessionsCalls)
		}
		if m.state.lastCoordinatorReap.IsZero() {
			t.Error("unhalted tick did not stamp lastCoordinatorReap")
		}
		if obs.diskLow {
			t.Error("free space was injected far above the watermark, so diskLow must be false")
		}
	})

	// A nil governor is the switched-off subsystem. It must read as "no halt"
	// rather than panicking, which is what lets runWorkLoop hold one code path for
	// both configurations.
	t.Run("absent governor subsystem never halts", func(t *testing.T) {
		deps, adapter := haltFixture(t)
		m := &loopMaintenance{governor: nil}

		obs := m.tickBeforeDispatch(context.Background(), deps)

		if obs.halt {
			t.Error("nil governor must not request a halt")
		}
		if adapter.listSessionsCalls != 1 {
			t.Errorf("nil governor should not short-circuit the pass: ListSessions called %d times, want 1",
				adapter.listSessionsCalls)
		}
	})
}

// diskLowFixture builds deps that drive the disk probe BELOW the watermark with
// every subprocess seam stubbed.
//
// Both real subprocess paths are replaced: goCacheCleanFunc stands in for
// `go clean -cache` (runGoCleanCache prefers it when non-nil) and
// worktreeReclaimFunc stands in for the `git worktree remove` / `worktree prune`
// sequence (runWorktreeReclaim prefers it the same way). Both are wired even
// though only one is reachable on this fixture, so that if the branch ever grows
// a new route to either subprocess the test records a call instead of spawning
// one.
//
// runRegistry is nil, which does two things on purpose. mergeOrRunInFlight
// reports "idle", so the branch takes the reap path rather than the
// merge-in-flight warning path. And reclaimStaleWorktrees returns 0 immediately
// on a nil registry, so the reclaim-was-sufficient early return is skipped and
// the go-cache reap is reached.
//
// bus is nil, so the disk_low event emit is skipped. The event payload is not
// what this test is about.
func diskLowFixture(t *testing.T, freeBytes uint64) (*workLoopDeps, *diskSeamCalls) {
	t.Helper()
	calls := &diskSeamCalls{}
	deps := &workLoopDeps{
		projectDir:        t.TempDir(),
		diskFreeBytesFunc: func(string) (uint64, error) { return freeBytes, nil },
		goCacheCleanFunc: func() error {
			calls.goClean++
			return nil
		},
		worktreeReclaimFunc: func(context.Context, string, []string) error {
			calls.worktreeReclaim++
			return nil
		},
	}
	// Fire on the first tick instead of waiting out diskCheckInterval.
	ExportedDiskCheckSetCheckInterval(deps, time.Nanosecond)
	return deps, calls
}

// diskSeamCalls counts the stubbed subprocess seams. Named fields rather than two
// bare *int returns, so a call site cannot silently transpose them.
type diskSeamCalls struct {
	goClean         int
	worktreeReclaim int
}

// TestDiskLowBranch covers the disk-below-watermark branch of the periodic disk
// check, which had NO coverage anywhere in the tree: diskcheck_hksxlb_test.go was
// deleted, leaving the export shims in export_maintenance_test.go with zero
// callers.
//
// An earlier version of this file claimed the branch was too expensive to test
// because it would run `go clean -cache` and `git worktree remove` for real. That
// was wrong. workLoopDeps has carried goCacheCleanFunc and worktreeReclaimFunc as
// seams for exactly this purpose the whole time, so the branch is cheap. The false
// claim is recorded here because a comment that talks a reader out of a test they
// could have written is worse than no comment.
func TestDiskLowBranch(t *testing.T) {
	// Below the watermark: latch set, cache reap attempted, no real subprocess.
	t.Run("below watermark sets diskLow and reaps the go cache", func(t *testing.T) {
		deps, calls := diskLowFixture(t, diskLowWatermarkDefault-1)
		ms := ExportedNewMaintState()

		ExportedRunPeriodicDiskCheck(context.Background(), deps, ms)

		if !ExportedDiskCheckDiskLow(ms) {
			t.Error("free space one byte below the watermark: want diskLow = true")
		}
		if calls.goClean != 1 {
			t.Errorf("go-clean seam called %d times, want 1 — the reactive reap is the point of this branch",
				calls.goClean)
		}
		// Nil runRegistry means no stale worktrees are enumerated, so the reclaim
		// seam is not reached on this path. Asserted so the fixture's shape stays
		// visible rather than implied.
		if calls.worktreeReclaim != 0 {
			t.Errorf("worktree-reclaim seam called %d times, want 0 on a nil run registry", calls.worktreeReclaim)
		}
	})

	// The latch must CLEAR when the disk recovers, using the same state handle.
	// This is the transition, not two independent probes.
	t.Run("recovery clears the diskLow latch", func(t *testing.T) {
		deps, calls := diskLowFixture(t, diskLowWatermarkDefault-1)
		ms := ExportedNewMaintState()

		ExportedRunPeriodicDiskCheck(context.Background(), deps, ms)
		if !ExportedDiskCheckDiskLow(ms) {
			t.Fatal("setup: want diskLow = true before testing recovery")
		}

		// Same deps, same state handle, disk now healthy.
		deps.diskFreeBytesFunc = func(string) (uint64, error) { return 1 << 62, nil }
		ExportedRunPeriodicDiskCheck(context.Background(), deps, ms)

		if ExportedDiskCheckDiskLow(ms) {
			t.Error("disk recovered above the watermark: want diskLow = false")
		}
		if calls.goClean != 1 {
			t.Errorf("go-clean seam called %d times, want 1 — the healthy path must NOT reap (hk-gjbpp)",
				calls.goClean)
		}
	})

	// The branch must be reachable through the new maintenance seam, not only by
	// calling runPeriodicDiskCheck directly, and the observation must carry the
	// latch out to the loop.
	t.Run("tickBeforeDispatch reports diskLow to the loop", func(t *testing.T) {
		deps, calls := diskLowFixture(t, diskLowWatermarkDefault-1)
		m := &loopMaintenance{}

		obs := m.tickBeforeDispatch(context.Background(), deps)

		if !obs.diskLow {
			t.Error("want observation.diskLow = true so the loop skips bead claiming this tick")
		}
		if obs.halt {
			t.Error("a low disk must not request a halt")
		}
		if !m.state.diskLow {
			t.Error("the latch must persist on loopMaintenance.state across ticks")
		}
		if calls.goClean != 1 {
			t.Errorf("go-clean seam called %d times through tickBeforeDispatch, want 1", calls.goClean)
		}
	})
}

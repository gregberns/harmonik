package daemon_test

// single_abort_phasecomplete_test.go — a legacy single input selects no-review
// DOT, and an aborted implementer node must still report its phase.
//
// implementer_phase_complete exists to close the diagnostic gap between
// run_started and the run's terminal, so a silent implementer failure leaves a
// structured record instead of nothing. The legacy input below is retained to
// prove run planning selects the no-review graph before execution.
//
// Bead: hk-aekon.

import (
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// singleFixtureAborter watches for the implementer to start, then cancels its
// context.
//
// latch selects which of the two cancellations it models, and they are the two
// the run path must tell apart. With the latch it is the StaleWatcher reaper
// reaping ONE run, which is that run's terminal. Without it, it is the daemon
// stopping and taking every live run's context down with it, which is a drain
// and not a failure. The cancelled context looks identical from the run's side;
// the latch is the only thing that distinguishes them.
type singleFixtureAborter struct {
	marker   string
	registry *daemon.RunRegistry
	latch    bool
	fired    atomic.Bool
	stop     chan struct{}
	done     chan struct{}
}

func newSingleFixtureAborter(t *testing.T, registry *daemon.RunRegistry, latch bool) *singleFixtureAborter {
	t.Helper()
	return &singleFixtureAborter{
		marker:   filepath.Join(t.TempDir(), "implementer-running"),
		registry: registry,
		latch:    latch,
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
}

// handlerScript is an implementer that announces it is running and then hangs,
// so the abort lands while the agent is live rather than racing its exit.
func (a *singleFixtureAborter) handlerScript(t *testing.T) string {
	t.Helper()
	return dotFixtureHandlerScript(t, "single-fixture-hang.sh",
		"printf 'x' > "+a.marker+"\nsleep 60\n")
}

func (a *singleFixtureAborter) start() {
	go func() {
		defer close(a.done)
		for {
			select {
			case <-a.stop:
				return
			case <-time.After(10 * time.Millisecond):
			}
			if _, err := os.Stat(a.marker); err != nil {
				continue
			}
			for _, h := range a.registry.Snapshot() {
				if h.Cancel == nil {
					continue
				}
				if a.latch {
					daemon.ExportedMarkRunAborted(h)
				}
				// Guard both directions. A latch that silently failed to take
				// would make the abort test assert nothing; a latch set when the
				// fixture asked for a shutdown would make the drain test assert
				// the opposite of what it claims.
				if daemon.ExportedRunHandleIsAborted(h) != a.latch {
					return // the test's own guard reports it
				}
				h.Cancel()
				a.fired.Store(true)
				return
			}
		}
	}()
}

func (a *singleFixtureAborter) finish() { close(a.stop); <-a.done }

// TestLegacySingleInput_NoReviewDOTAbortedRunReportsImplementerPhase is the
// claim. The implementer is aborted while it runs, and the graph must still
// emit implementer_phase_complete.
func TestLegacySingleInput_NoReviewDOTAbortedRunReportsImplementerPhase(t *testing.T) {
	t.Parallel()

	registry := daemon.ExportedNewRunRegistry()
	aborter := newSingleFixtureAborter(t, registry, true)
	script := aborter.handlerScript(t)
	aborter.start()

	const beadID = core.BeadID("hk-aekon-aborted-run")
	res := runDotFixtureBead(t, beadID, dotFixtureOpts{
		WorkflowMode:  core.WorkflowModeSingle,
		HandlerScript: script,
		RunRegistry:   registry,
	})
	aborter.finish()

	if !aborter.fired.Load() {
		t.Fatal("the fixture never aborted a run, so this test asserts nothing — fix the fixture before trusting the result")
	}
	// The graph must report its cancellation. Without this check the event below
	// could come from an ordinary completed node.
	if summary := dotFixtureRunFailedSummary(res); !strings.Contains(summary, "context cancelled during node") {
		t.Fatalf("run_failed summary = %q; want the DOT cancellation reason; events=%v", summary, res.Bus.eventTypes())
	}
	if !singleFixtureHasEvent(res, core.EventTypeImplementerPhaseComplete) {
		t.Errorf("bead %s was aborted and emitted no implementer_phase_complete; events=%v", beadID, res.Bus.eventTypes())
	}
}

// TestLegacySingleInput_NoReviewDOTShutdownDrainsRatherThanFails is the other
// half of the same branch. It pairs with the test above through the one field
// that differs between them — the latch — so neither can be read alone.
//
// The run is cancelled exactly as above but WITHOUT the reaper's latch, which is
// what a daemon-wide stop looks like. That is a drain: the run did not fail, it
// only did not finish, so RSM-021 parks it. Blaming a run for the daemon going
// down would re-run an agent that was doing nothing wrong.
//
// It asserts the reopen as well as the absence of run_failed. The absence is
// what the drain has to get right; the reopen adds the one case the absence
// cannot see, which is a run that CLOSES its bead instead of reopening it. A run
// that emits nothing at all is already caught upstream — runDotFixtureBead waits
// on a close or a reopen and fatals without one — so that is not what this
// assertion is for. dot_shutdown_drain_test.go covers the same branch from the
// ordinary shutdown side; this one is here because it shares a fixture with the
// abort test and so pins the discriminator itself.
func TestLegacySingleInput_NoReviewDOTShutdownDrainsRatherThanFails(t *testing.T) {
	t.Parallel()

	registry := daemon.ExportedNewRunRegistry()
	stopper := newSingleFixtureAborter(t, registry, false)
	script := stopper.handlerScript(t)
	stopper.start()

	const beadID = core.BeadID("hk-aekon-shutdown-drain")
	res := runDotFixtureBead(t, beadID, dotFixtureOpts{
		WorkflowMode:  core.WorkflowModeSingle,
		HandlerScript: script,
		RunRegistry:   registry,
	})
	stopper.finish()

	if !stopper.fired.Load() {
		t.Fatal("the fixture never cancelled a run, so this test asserts nothing — fix the fixture before trusting the result")
	}
	if summary := dotFixtureRunFailedSummary(res); summary != "" {
		t.Errorf("the daemon stopped and the run was blamed for it: run_failed summary = %q; want no run_failed at all; events=%v",
			summary, res.Bus.eventTypes())
	}
	if reopened := res.Ledger.reopenedIDs(); len(reopened) == 0 {
		t.Errorf("bead %s was not reopened after the daemon stopped, so the drain left it stranded in_progress with no terminal at all; events=%v",
			beadID, res.Bus.eventTypes())
	}
}

// TestLegacySingleInput_NoReviewDOTNormalRunReportsImplementerPhase is the
// control. The emit must still happen on the ordinary path.
func TestLegacySingleInput_NoReviewDOTNormalRunReportsImplementerPhase(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("hk-aekon-normal-run")
	res := runDotFixtureBead(t, beadID, dotFixtureOpts{
		WorkflowMode: core.WorkflowModeSingle,
	})

	if closed := res.Ledger.closedIDs(); len(closed) == 0 {
		t.Fatalf("bead %s was not closed by a committing implementer; events=%v", beadID, res.Bus.eventTypes())
	}
	if !singleFixtureHasEvent(res, core.EventTypeImplementerPhaseComplete) {
		t.Errorf("bead %s completed normally and emitted no implementer_phase_complete; events=%v", beadID, res.Bus.eventTypes())
	}
}

// singleFixtureHasEvent reports whether the run emitted at least one event of
// this type.
func singleFixtureHasEvent(res dotFixtureResult, want core.EventType) bool {
	for _, ev := range res.Bus.allEvents() {
		if ev.EventType == string(want) {
			return true
		}
	}
	return false
}

package daemon_test

// single_abort_phasecomplete_test.go — an aborted single-mode run must still
// report the implementer phase it just finished.
//
// implementer_phase_complete exists to close the diagnostic gap between
// run_started and the run's terminal, so a silent implementer failure leaves a
// structured record instead of nothing. The comment above the single-mode emit
// says it fires "regardless of how" the run exited. The abort branch returned
// above the emit, so the one exit that most needs the diagnostic was the one
// that produced none.
//
// The graph node checks its own cancellation AFTER the emit and does produce the
// event. This is single mode brought to the same order.
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

// singleFixtureAborter watches for the implementer to start, then does what
// every StaleWatcher reaper does: latch the run aborted and cancel its context.
// That pair is what the run path reads to tell a per-run abort apart from a
// daemon-wide shutdown.
type singleFixtureAborter struct {
	marker   string
	registry *daemon.RunRegistry
	fired    atomic.Bool
	stop     chan struct{}
	done     chan struct{}
}

func newSingleFixtureAborter(t *testing.T, registry *daemon.RunRegistry) *singleFixtureAborter {
	t.Helper()
	return &singleFixtureAborter{
		marker:   filepath.Join(t.TempDir(), "implementer-running"),
		registry: registry,
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
				daemon.ExportedMarkRunAborted(h)
				if !daemon.ExportedRunHandleIsAborted(h) {
					return // the latch did not take; the test's own guard reports it
				}
				h.Cancel()
				a.fired.Store(true)
				return
			}
		}
	}()
}

func (a *singleFixtureAborter) finish() { close(a.stop); <-a.done }

// TestSingleMode_AbortedRunStillReportsItsImplementerPhase is the claim. The
// implementer is aborted while it runs, and the run MUST still emit
// implementer_phase_complete.
func TestSingleMode_AbortedRunStillReportsItsImplementerPhase(t *testing.T) {
	t.Parallel()

	registry := daemon.ExportedNewRunRegistry()
	aborter := newSingleFixtureAborter(t, registry)
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
	// The run must have taken the ABORT branch, not the daemon-shutdown branch or
	// a plain failure. Without this the assertion below could be satisfied by any
	// run that happened to reach the emit some other way.
	//
	// The reason names the never-spawned reaper although the abort above is shaped
	// like the kill-consumer backstop. That is not a mismatch in the test: the run
	// path hardcodes this one reason for every reaper, because the abort latch is
	// all it can see and the latch does not say which reaper set it.
	if summary := dotFixtureRunFailedSummary(res); !strings.Contains(summary, "never_spawned_reaper") {
		t.Fatalf("run_failed summary = %q; want the per-run abort reason — the run did not take the abort branch, so the claim below is untested", summary)
	}
	if !singleFixtureHasEvent(res, core.EventTypeImplementerPhaseComplete) {
		t.Errorf("bead %s was aborted and emitted no implementer_phase_complete; events=%v.\n"+
			"The abort branch returns above the emit, so the exit that most needs the diagnostic produces none — and the comment above the emit claims it fires regardless of how the run exited.",
			beadID, res.Bus.eventTypes())
	}
}

// TestSingleMode_NormalRunReportsItsImplementerPhase is the control: the emit
// must still happen on the ordinary path. It is what keeps a "fix" that moved
// the emit somewhere unreachable from passing the test above.
func TestSingleMode_NormalRunReportsItsImplementerPhase(t *testing.T) {
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

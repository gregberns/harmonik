package daemon_test

// loopexit_test.go — one shared way to wait for a work loop to finish.
//
// Several tests in this package already wait on the milestone that proves the
// behaviour they are named for — a bead closed, a bead reopened, a rebase
// conflict rejected, N runs serialized — and then impose a SECOND, smaller
// stopwatch on the work loop's teardown. Under load that second stopwatch is
// what fails, after the test has already logged its own property as satisfied:
//
//	TestMergeToMain_NonFFReopen                  logged "rebase-conflict path OK",
//	                                             then failed on "work loop did not
//	                                             exit within 5s"
//	TestScenario_MultiBead_SerializedNCompletion logged "5 beads closed, 5 files on
//	                                             main, no merge commits", then failed
//	                                             on "did not exit within 10s of cancel"
//	TestSmokeLoop                                logged bead closed, run_started and
//	                                             run_completed all seen, then failed
//	                                             on a 5-second cancel deadline
//
// In each case the product behaviour under test was observed and correct. Only
// the stopwatch failed. Refs hk-scenario-budgets-structural-2z9dx.
//
// The teardown has no budget of its own to defend. Nothing in any spec says the
// loop must unwind in two seconds rather than four, and the fixtures pay costs
// the numbers were never set against — every dispatch incurs
// runloop.StopHookGrace, and a test with ten worktrees pays it ten times. So the
// bound this helper applies is the one bound the test really has: the test
// binary's own deadline, from `go test -timeout`. There is no number to tune
// here and nothing to re-tune when the box changes.
//
// A test whose SUBJECT is prompt teardown — "does the loop exit after cancel
// with a hanging twin" — keeps its own explicit budget. That number is the
// claim, not a stopwatch on top of one.
//
// Not to be confused with awaitWorkLoopExit in run_w3cp1_boiwe_hiqrl_test.go.
// That one answers a different question — did the loop return for the RIGHT
// REASON, a drained queue rather than an expired budget — and it needs the
// cancel context to tell those apart. Reach for it when the test has one. These
// two helpers came out of the same failure family (hk-33e6p there, this bead
// here) and the treatment there is the older of the two.

import (
	"testing"
	"time"
)

// workLoopTestBudget is the single ceiling a work-loop test gives the dispatch
// it waits on. It is a hang-stopper, not a performance assertion: no spec says a
// fixture dispatch must finish in twenty seconds, and none of these tests is
// named for how fast anything is.
//
// The budgets it replaces were set against the happy path and never against what
// the fixtures actually pay. Every dispatch incurs runloop.StopHookGrace of 3
// seconds, so a two-dispatch test has a 6-second floor and a ten-worktree test a
// 30-second one, before any git work. Measured alone, the tests using this
// constant ran 3.9 to 13.8 seconds against budgets of 6 to 25 — margins of 1.3x
// to 1.8x, on a box also running the other lane. That is what made them fail in
// batches while passing one at a time.
//
// 60 seconds is over four times the slowest measured run. It is deliberately
// loose. Tightening it back toward the observed runtime is how this returns.
const workLoopTestBudget = 60 * time.Second

// awaitLoopTeardownMargin is how long before the test binary's deadline
// awaitLoopTeardown gives up. A genuine hang is then reported as a named failure of
// this one test, instead of a binary-wide timeout panic that also takes down
// every parallel sibling and hides which test was stuck.
const awaitLoopTeardownMargin = 10 * time.Second

// awaitLoopTeardown blocks until ch is closed, bounded by the test binary's
// deadline rather than by a stopwatch of its own. Call it after the milestone
// that proves the test's property has already been observed.
//
// what names the thing being waited for, for the failure message.
func awaitLoopTeardown(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-awaitLoopTeardownDeadline(t):
		t.Fatalf("%s did not exit before the test deadline after context cancellation", what)
	}
}

// awaitLoopTeardownErr is awaitLoopTeardown for a loop that reports its exit through an
// error channel. It returns the loop's error for the caller to assert on.
func awaitLoopTeardownErr(t *testing.T, ch <-chan error, what string) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-awaitLoopTeardownDeadline(t):
		t.Fatalf("%s did not exit before the test deadline after context cancellation", what)
		return nil
	}
}

// awaitLoopTeardownDeadline returns a channel that fires awaitLoopTeardownMargin before
// the test binary's deadline, or never when the binary runs without a timeout.
func awaitLoopTeardownDeadline(t *testing.T) <-chan time.Time {
	t.Helper()
	deadline, ok := t.Deadline()
	if !ok {
		// `go test -timeout 0`. Nothing bounds the run, so nothing bounds this
		// wait either — a hang is then the operator's to interrupt, which is
		// what asking for no timeout means.
		return nil
	}
	remaining := time.Until(deadline) - awaitLoopTeardownMargin
	if remaining < time.Second {
		// So little of the run is left that the margin is meaningless. Wait a
		// beat rather than firing at once and reporting a hang that is really
		// an exhausted binary deadline.
		remaining = time.Second
	}
	timer := time.NewTimer(remaining)
	t.Cleanup(func() { timer.Stop() })
	return timer.C
}

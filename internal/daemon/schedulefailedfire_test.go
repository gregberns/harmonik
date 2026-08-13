package daemon

// schedulefailedfire_test.go — a scheduled fire that FAILS must still be
// recorded (hk-pbdti).
//
// # The claim these tests defend
//
// LastFire is the only thing that makes a due job stop being due. doFireAction
// used to return before store.MarkFired on every failure path — one return per
// action-kind case plus the default — so a job whose action failed recorded
// nothing, stayed due, and was re-fired by the work loop every
// workloopPollInterval (2s) with no backoff and no cap. It was measured at 50
// attempts in 50 ticks, and it reached every action kind, not just comms-send.
//
// The tests below drive the tick many times over a job that cannot succeed and
// require the schedule to advance anyway. The action error must still come back
// out of doFireAction: for a comms-send action that stderr line is the only sign
// an operator gets that the message reached nobody, so recording the fire and
// reporting the failure are asserted separately and cannot be traded for each
// other.
//
// # Helper prefix
//
// Helpers use the prefix "failedFire" per implementer-protocol.md
// §Helper-prefix discipline. The port/store/crew builder is newTickPort from
// scheduletick_test.go.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/schedule"
)

// failedFireTicks is the number of work-loop passes each table row drives. The
// pre-fix behaviour produced one fire attempt per pass; the fixed behaviour
// produces exactly one for the whole run, because the first attempt records
// LastFire and the daily boundary it just serviced is then in the past.
const failedFireTicks = 20

// errFailedFireAction is the failure a table row's injected double returns. A
// sentinel (not a bare fmt.Errorf) so the errors.Join test can assert with
// errors.Is that the ACTION's error survived being joined with the recording
// error.
var errFailedFireAction = errors.New("failed-fire test: the action failed")

// failedFireDueDaily returns a daily schedule whose instant is already past, so
// a freshly-added job is due on the first tick and — once LastFire is written —
// not due again for the rest of the test.
func failedFireDueDaily(t *testing.T) schedule.Schedule {
	t.Helper()
	return schedule.Schedule{Kind: schedule.ScheduleKindDaily, At: pastDailyAt(t), TZ: "UTC"}
}

// failedFireCountingSend returns a commsSendFunc that always fails, plus a
// counter of the calls it received.
func failedFireCountingSend() (send commsSendFunc, attempts func() int) {
	var mu sync.Mutex
	n := 0
	send = func(_ context.Context, _, _, _, _ string) error {
		mu.Lock()
		defer mu.Unlock()
		n++
		return errFailedFireAction
	}
	attempts = func() int {
		mu.Lock()
		defer mu.Unlock()
		return n
	}
	return send, attempts
}

// TestScheduleTickRecordsAFireThatFailedForEveryActionKind is the regression
// test for the storm. Every row is a failure mode that used to return before
// MarkFired.
func TestScheduleTickRecordsAFireThatFailedForEveryActionKind(t *testing.T) {
	cases := []struct {
		name string
		// arm puts the port into the failure mode and returns the action to
		// schedule plus, WHERE A DOUBLE EXISTS, a counter of action attempts.
		arm func(t *testing.T, port *schedulePort, crew *fakeCrewStarter) (schedule.Action, func() int)
		// wantAttempts is the attempt count required of a counted row. Compared
		// with !=, never >: a > 1 check still passes when a mutation stops the
		// action from firing at all.
		wantAttempts int
		// whyNoCounter records, for an uncounted row, what makes the attempts
		// unobservable. Both uncounted rows fail before any double can see them.
		whyNoCounter string
	}{
		{
			name: "command action whose binary does not exist",
			arm: func(t *testing.T, port *schedulePort, _ *fakeCrewStarter) (schedule.Action, func() int) {
				t.Helper()
				missing := filepath.Join(t.TempDir(), "no-such-binary")
				return schedule.Action{Kind: schedule.ActionKindCommand, Argv: []string{missing}}, nil
			},
			whyNoCounter: "cmd.Start fails at execve, so the binary never runs and there is nothing on the far side to count",
		},
		{
			name: "spawn-crew action whose crew handler returns an error",
			arm: func(t *testing.T, _ *schedulePort, crew *fakeCrewStarter) (schedule.Action, func() int) {
				t.Helper()
				crew.err = errFailedFireAction
				return schedule.Action{Kind: schedule.ActionKindSpawnCrew, Crew: "owl", Queue: "night"}, crew.count
			},
			wantAttempts: 1,
		},
		{
			name: "comms-send action whose send fails",
			arm: func(t *testing.T, port *schedulePort, _ *fakeCrewStarter) (schedule.Action, func() int) {
				t.Helper()
				send, count := failedFireCountingSend()
				port.commsSend = send
				return schedule.Action{Kind: schedule.ActionKindCommsSend, To: "nobody", Body: "ping"}, count
			},
			wantAttempts: 1,
		},
		{
			name: "action kind nothing handles",
			arm: func(t *testing.T, _ *schedulePort, _ *fakeCrewStarter) (schedule.Action, func() int) {
				t.Helper()
				return schedule.Action{Kind: "bogus"}, nil
			},
			whyNoCounter: "the default case dispatches no action at all, so no double is reached",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			port, store, crew := newTickPort(t)
			action, attempts := tc.arm(t, &port, crew)

			job := schedule.ScheduledJob{
				ID:            "failed-fire",
				Schedule:      failedFireDueDaily(t),
				Action:        action,
				Enabled:       true,
				OverlapPolicy: schedule.OverlapPolicyAllow,
			}
			if err := store.Add(job); err != nil {
				t.Fatalf("Add: %v", err)
			}

			for i := 0; i < failedFireTicks; i++ {
				runScheduleTick(context.Background(), port)
			}

			got, ok := store.Get("failed-fire")
			if !ok {
				t.Fatalf("job vanished from the store")
			}
			if got.LastFire == "" {
				t.Fatalf("LastFire is empty after %d ticks over a failing fire: the job is still due, "+
					"so the work loop re-fires it every poll interval forever", failedFireTicks)
			}
			// A failed fire records no pid. For the skip overlap policy that is the
			// right record: overlapBlocks treats only LastPID > 0 as a prior run
			// that may still be alive.
			if got.LastPID != 0 {
				t.Errorf("LastPID = %d after a failed fire, want 0", got.LastPID)
			}

			if attempts == nil {
				t.Logf("attempts not counted: %s", tc.whyNoCounter)
				return
			}
			if n := attempts(); n != tc.wantAttempts {
				t.Fatalf("action attempted %d times over %d ticks, want %d", n, failedFireTicks, tc.wantAttempts)
			}
		})
	}
}

// TestDoFireActionReturnsTheActionErrorAfterRecordingTheFire keeps the two
// halves of the fix from being traded against each other. Recording the fire
// stops the storm; returning the error is what puts the failure on the daemon's
// stderr through fireScheduledJob. A version that recorded the fire and returned
// nil would pass every row of the table above and leave an operator with a
// scheduled message that reaches nobody and says nothing.
func TestDoFireActionReturnsTheActionErrorAfterRecordingTheFire(t *testing.T) {
	cases := []struct {
		name   string
		arm    func(t *testing.T, port *schedulePort, crew *fakeCrewStarter) schedule.Action
		wantIn string
	}{
		{
			name: "command action with empty argv",
			arm: func(t *testing.T, _ *schedulePort, _ *fakeCrewStarter) schedule.Action {
				t.Helper()
				return schedule.Action{Kind: schedule.ActionKindCommand}
			},
			wantIn: "empty argv",
		},
		{
			name: "spawn-crew action with no crew handler wired",
			arm: func(t *testing.T, port *schedulePort, _ *fakeCrewStarter) schedule.Action {
				t.Helper()
				port.crewHandler = nil
				return schedule.Action{Kind: schedule.ActionKindSpawnCrew, Crew: "owl", Queue: "night"}
			},
			wantIn: "no crew handler wired",
		},
		{
			name: "comms-send action whose send fails",
			arm: func(t *testing.T, port *schedulePort, _ *fakeCrewStarter) schedule.Action {
				t.Helper()
				send, _ := failedFireCountingSend()
				port.commsSend = send
				return schedule.Action{Kind: schedule.ActionKindCommsSend, To: "nobody", Body: "ping"}
			},
			wantIn: errFailedFireAction.Error(),
		},
		{
			name: "action kind nothing handles",
			arm: func(t *testing.T, _ *schedulePort, _ *fakeCrewStarter) schedule.Action {
				t.Helper()
				return schedule.Action{Kind: "bogus"}
			},
			wantIn: `unknown action kind "bogus"`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			port, store, crew := newTickPort(t)
			job := schedule.ScheduledJob{
				ID:       "reports-failure",
				Schedule: failedFireDueDaily(t),
				Action:   tc.arm(t, &port, crew),
				Enabled:  true,
			}
			if err := store.Add(job); err != nil {
				t.Fatalf("Add: %v", err)
			}

			err := doFireAction(context.Background(), port, job, time.Now().UTC())
			if err == nil {
				t.Fatalf("doFireAction returned nil for a failed action: the daemon logs nothing " +
					"and the failure is invisible to an operator")
			}
			if !strings.Contains(err.Error(), tc.wantIn) {
				t.Errorf("error %q does not carry %q", err.Error(), tc.wantIn)
			}
			// The fire was recorded all the same — reporting and recording are not
			// alternatives.
			if got, _ := store.Get("reports-failure"); got.LastFire == "" {
				t.Errorf("LastFire empty: the fire was reported but not recorded")
			}
		})
	}
}

// TestDoFireActionJoinsTheActionErrorWhenTheRecordAlsoFails covers the doubly
// degraded path. When MarkFired fails too, the recording error must not replace
// the action error: the action failure is the one an operator can act on.
func TestDoFireActionJoinsTheActionErrorWhenTheRecordAlsoFails(t *testing.T) {
	port, store, _ := newTickPort(t)
	send, _ := failedFireCountingSend()
	port.commsSend = send

	job := schedule.ScheduledJob{
		ID:       "both-fail",
		Schedule: failedFireDueDaily(t),
		Action:   schedule.Action{Kind: schedule.ActionKindCommsSend, To: "nobody", Body: "ping"},
		Enabled:  true,
	}
	if err := store.Add(job); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// The store persists to <projectDir>/.harmonik/schedules.json through a temp
	// file in that directory. A readable but non-writable directory is what makes
	// the record — and only the record — fail.
	stateDir := filepath.Join(port.projectDir, ".harmonik")
	if err := os.Chmod(stateDir, 0o500); err != nil { //nolint:gosec // the test needs a readable, non-writable directory
		t.Fatalf("chmod state dir: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(stateDir, 0o700); err != nil { //nolint:gosec // restoring so t.TempDir cleanup can remove it
			t.Logf("restore state dir permissions: %v", err)
		}
	})

	err := doFireAction(context.Background(), port, job, time.Now().UTC())
	if err == nil {
		t.Fatalf("doFireAction returned nil when both the action and the record failed")
	}
	if !errors.Is(err, errFailedFireAction) {
		t.Errorf("the action's error was swallowed by the recording error: %v", err)
	}
	if !strings.Contains(err.Error(), "mark fired") {
		t.Errorf("the recording failure is not reported: %v", err)
	}
}

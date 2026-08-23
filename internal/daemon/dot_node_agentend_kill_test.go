package daemon_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/runloop"
)

func dotFixtureAgentEndThenLingerHandler(t *testing.T, bead core.BeadID) string {
	t.Helper()
	return dotFixtureHandlerScript(t, "dot-fixture-agent-end-linger.sh",
		dotFixtureCommitLines(bead)+
			`printf '{"type":"session","version":3,"id":"pi-fixture-session"}\n'`+"\n"+
			`printf '{"type":"agent_end","messages":[]}\n'`+"\n"+
			"while :; do sleep 1; done\n")
}

func dotFixtureAgentEndWillRetryHandler(t *testing.T, bead core.BeadID, markerPath string) string {
	t.Helper()
	return dotFixtureHandlerScript(t, "dot-fixture-agent-end-willretry.sh",
		"marker='"+markerPath+"'\n"+
			"trap 'echo killed-during-backoff > \"${marker}.bad\"; exit 143' TERM\n"+
			dotFixtureCommitLines(bead)+
			`printf '{"type":"session","version":3,"id":"pi-fixture-session"}\n'`+"\n"+
			`printf '{"type":"agent_end","messages":[],"willRetry":true}\n'`+"\n"+
			`printf '{"type":"auto_retry_start","attempt":1,"maxAttempts":3,"delayMs":2000}\n'`+"\n"+
			"sleep 2\n"+
			"echo survived > \"${marker}\"\n"+
			`printf '{"type":"agent_end","messages":[]}\n'`+"\n"+
			"while :; do sleep 1; done\n")
}

// TestDotNode_PiAgentEndWithWillRetryDoesNotKillDuringTheBackoff is the
// end-to-end statement of the retry fix, and it is the half a unit test on the
// parser cannot make: the marker file can only exist if the real daemon, on the
// real exec path, through the real Pi stdout interceptor, declined to kill a
// real child process during a real two-second wait.
//
// It also asserts the other half — that the run still ENDS. A watcher that
// suppressed the retry announcement and then failed to re-arm would leave Pi
// lingering until the 90-minute ceiling, and the bead would never close. So the
// same test that proves the kill was withheld proves it was only deferred.
//
// Read this beside TestDotNode_PiAgentEndKillAfterACommitClosesTheBead above,
// which is the same path with no flag on the announcement. The pair states the
// whole contract: end the session on the announcement that is real, and not on
// the one that is not.
func TestDotNode_PiAgentEndWithWillRetryDoesNotKillDuringTheBackoff(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("hk-pi-agentend-willretry-survives")

	markerPath := filepath.Join(t.TempDir(), "survived-the-backoff")

	opts := dotFixtureProcessExitOpts(t, core.AgentTypePi,
		dotFixtureAgentEndWillRetryHandler(t, beadID, markerPath))
	opts.HookOutcome = ""
	opts.WaitForRunTerminal = true

	res := runDotFixtureBead(t, beadID, opts)

	events := res.Bus.eventTypes()

	if _, err := os.Stat(markerPath); err != nil {
		detail := "the marker was never written"
		if _, bad := os.Stat(markerPath + ".bad"); bad == nil {
			detail = "the child caught SIGTERM during its backoff"
		}
		t.Errorf("pi was killed while it was retrying (%s; events=%v).\n"+
			"Pi emits an agent_end carrying willRetry before each attempt and only then retries.\n"+
			"Ending the session on that line kills pi inside its own backoff, and the daemon then\n"+
			"records the kill as a clean exit — so a refused endpoint reads as an agent that did nothing.",
			detail, events)
	}

	if reopened := res.Ledger.reopenedIDs(); len(reopened) > 0 {
		t.Errorf("bead %s was REOPENED although its agent retried and then finished (reopened=%v, events=%v)",
			beadID, reopened, events)
	}
	if closed := res.Ledger.closedIDs(); !slices.Contains(closed, beadID) {
		t.Errorf("bead %s was not closed after the terminal agent_end (closed=%v, events=%v).\n"+
			"Suppressing a retry announcement must not disarm the watcher: the real announcement still has to end the session,\n"+
			"or a retried run lingers to the 90-minute ceiling instead.",
			beadID, closed, events)
	}
	if slices.Contains(events, string(core.EventTypeRunFailed)) {
		t.Errorf("run for bead %s emitted run_failed although its agent committed, retried once and announced a clean end; events=%v",
			beadID, events)
	}
}

// TestDotNode_PiAgentEndKillAfterACommitClosesTheBead is the regression.
//
// The implementer commits, announces, and lingers until the daemon kills it.
// Nothing arrives on the hook socket — Pi reports nothing there, which is the
// real condition and is what shuts CHB-020 branch 1 to it — so the exit code is
// the only thing the classifier has to go on, and the daemon manufactured it.
func TestDotNode_PiAgentEndKillAfterACommitClosesTheBead(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("hk-pi-agentend-kill-closes")

	opts := dotFixtureProcessExitOpts(t, core.AgentTypePi,
		dotFixtureAgentEndThenLingerHandler(t, beadID))
	opts.HookOutcome = ""
	opts.WaitForRunTerminal = true

	res := runDotFixtureBead(t, beadID, opts)

	events := res.Bus.eventTypes()
	if reopened := res.Ledger.reopenedIDs(); len(reopened) > 0 {
		t.Errorf("bead %s was REOPENED after its agent committed real work and announced the end of its turn (reopened=%v, events=%v).\n"+
			"The daemon SIGTERMs a Pi agent ON its agent_end announcement, so the wait reports exit=-1 \"signal: terminated\".\n"+
			"Reading that manufactured exit code as a crash discards a commit the agent had already landed.",
			beadID, reopened, events)
	}
	if closed := res.Ledger.closedIDs(); !slices.Contains(closed, beadID) {
		t.Errorf("bead %s was not closed after a committing agent announced the end of its turn (closed=%v, events=%v)",
			beadID, closed, events)
	}
	if slices.Contains(events, string(core.EventTypeRunFailed)) {
		t.Errorf("run for bead %s emitted run_failed although its agent committed and announced a clean end; events=%v",
			beadID, events)
	}
}

// TestDotNodeTerminalFailure_AnnouncedEndIsExemptOnlyFromTheExitCode holds the
// classifier fail-closed.
//
// Every row carries the SAME exit state — -1, the signal death the daemon's own
// kill produces — so the only thing that varies is what else the agent left
// behind. That is what makes the table a statement about the exemption's SCOPE
// rather than about exit codes: an exemption that grew to cover a reported
// failure or an unreadable progress stream would pass a run nobody has any
// evidence about.
func TestDotNodeTerminalFailure_AnnouncedEndIsExemptOnlyFromTheExitCode(t *testing.T) {
	t.Parallel()

	const killedExitCode = -1

	failureSignal := &handler.ExportedOutcomeEmittedPayload{
		Kind:           "FAILURE_SIGNAL",
		SubReason:      "claude_reported_failure",
		SuggestedClass: "structural",
	}
	watcherErr := errors.New("handlercontract: malformed NDJSON line")

	for _, tc := range []struct {
		name        string
		announced   bool
		outcome     *handler.ExportedOutcomeEmittedPayload
		watcherErr  error
		wantFailure bool
		why         string
	}{
		{
			name:        "announced end, nothing else reported",
			announced:   true,
			wantFailure: false,
			why: "the agent said it was stopping and the daemon killed it for saying so; the exit code describes the DAEMON's act, " +
				"and nothing else here is evidence of a crash",
		},
		{
			name:        "unannounced signal death",
			announced:   false,
			wantFailure: true,
			why: "no announcement means the kill came from a watchdog, an abort or a stall — or from outside the daemon entirely. " +
				"A signal death nobody explained is still a crash, and exempting it would pass every stalled and aborted run",
		},
		{
			name:        "announced end, watcher could not read the stream",
			announced:   true,
			watcherErr:  watcherErr,
			wantFailure: true,
			why:         "a watcher error is not a claim about how the process died, so the announcement says nothing about it",
		},
		{
			name:        "announced end, agent reported FAILURE_SIGNAL",
			announced:   true,
			outcome:     failureSignal,
			wantFailure: true,
			why: "the agent itself reported that it failed. The announcement means the turn ENDED, never that it went well, " +
				"so a reported failure outranks it",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			reason, gotFailure := daemon.ExportedDotNodeTerminalFailure(
				"pi-fixture-session",
				runloop.ExitInfo{ExitCode: killedExitCode, AgentAnnouncedEnd: tc.announced},
				tc.outcome,
				tc.watcherErr,
			)
			if gotFailure != tc.wantFailure {
				t.Errorf("dotNodeTerminalFailure(exit=%d, announced=%v, outcome=%v, watcherErr=%v) failure = %v (reason %q); want %v.\n%s",
					killedExitCode, tc.announced, tc.outcome, tc.watcherErr, gotFailure, reason, tc.wantFailure, tc.why)
			}
			if !tc.wantFailure && reason != "" {
				t.Errorf("a non-failure carried the reason %q; a node that does not fail must name no failure", reason)
			}
			if tc.wantFailure && reason == "" {
				t.Error("a failure carried no reason, so nothing downstream can say why the node failed")
			}
		})
	}
}

// TestAnnouncementIsCleanExit_FailureKillOverridesTheAnnouncement pins the
// fail-closed half of the exemption.
//
// The end-to-end test above proves an announced end is credited. It cannot
// prove the converse, because it cannot make a watchdog fire inside the
// millisecond between the announcement and the kill it triggers. So the one
// input that decides whether a wedged agent can launder itself into a pass by
// printing the right line first is stated here instead.
//
// Bead: hk-pi-success-recorded-as-crash-j6mow.
func TestAnnouncementIsCleanExit_FailureKillOverridesTheAnnouncement(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name             string
		announcedEnd     bool
		killedForFailure bool
		want             bool
		why              string
	}{
		{
			name:         "announced its end and nothing else killed it",
			announcedEnd: true,
			want:         true,
			why:          "the daemon killed it for saying it was stopping; the exit describes the daemon's act",
		},
		{
			name: "never announced anything",
			want: false,
			why:  "an unannounced death is judged on its exit code exactly as before",
		},
		{
			name:             "announced its end, but a watchdog killed it too",
			announcedEnd:     true,
			killedForFailure: true,
			want:             false,
			why:              "a wedge that printed the announcement first is still a wedge; this is the fail-closed case",
		},
		{
			name:             "a watchdog killed it and it never announced",
			killedForFailure: true,
			want:             false,
			why:              "the plain stall, ready-timeout or abort case",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := daemon.ExportedAnnouncementIsCleanExit(tc.announcedEnd, tc.killedForFailure)
			if got != tc.want {
				t.Errorf("announcementIsCleanExit(announced=%v, killedForFailure=%v) = %v; want %v.\n\t%s",
					tc.announcedEnd, tc.killedForFailure, got, tc.want, tc.why)
			}
		})
	}
}

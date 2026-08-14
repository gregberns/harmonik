package daemon_test

// dot_node_agentend_kill_test.go — a kill the daemon issued because the agent
// said it was finished must not be read as a crash the agent suffered.
//
// Pi announces the end of its turn on its own NDJSON stdout
// (`{"type":"agent_end"}`) and its process exit is unreliable, so the daemon
// SIGTERMs it ON that announcement. Every kill arrives at the wait as the same
// three facts — SIGTERM, exit code -1, "signal: terminated" — and the DOT
// terminal classifier required exit 0. So a Pi node that read the task, made
// the change and committed it with a valid Refs: trailer was recorded
// `agent_failed class=structural sub_reason=claude_crashed exit=-1`, its bead
// was reopened and its commit was thrown away.
//
// Two tests, and they are deliberately different in kind:
//
//   - the run test drives the real sequence end to end — a real /bin/sh child
//     on the exec path, the real Pi stdout interceptor, the real announcement
//     kill, the real signal death — and asserts on what the run DID to the
//     bead. Nothing in it names the exemption, so it stays true whatever
//     mechanism carries it.
//   - the table holds the classifier FAIL-CLOSED. The exemption withdraws a
//     claim about how the process died and nothing more, so an unannounced
//     signal death, a watcher that could not read the stream, and a reported
//     FAILURE_SIGNAL must all still fail — and only a direct call can put those
//     three inputs beside the announcement.

import (
	"errors"
	"slices"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/runloop"
)

// dotFixtureAgentEndThenLingerHandler writes an implementer that behaves the
// way a real Pi turn behaves at its end: it commits real work, announces the
// end of its turn on stdout, and then DOES NOT EXIT.
//
// The lingering is the whole point. An implementer that announced and then
// exited 0 would be judged on the exit code it produced itself, and would pass
// before the fix as readily as after it — which is exactly how this defect hid:
// a Pi run passed when it happened to die before the SIGTERM landed and failed
// whenever the announcement kill actually did its job. Lingering makes the
// daemon's kill the thing that ends the process every time, so the wait always
// reports the signal death this test is about.
//
// The loop sleeps rather than blocking forever, the same reason test/twins/hang
// documents for the hanging twin: a shell parked on a blocking wait with no
// timer pending is a shell that can be optimised or scheduled into looking dead
// on its own, and a twin that dies by itself is a condition the test never
// produced. A one-second period also bounds how long the shell can take to
// notice a signal on any /bin/sh.
func dotFixtureAgentEndThenLingerHandler(t *testing.T, bead core.BeadID) string {
	t.Helper()
	return dotFixtureHandlerScript(t, "dot-fixture-agent-end-linger.sh",
		dotFixtureCommitLines(bead)+
			// The session header is Pi's first line and the daemon's proof the
			// child spawned; the announcement is its terminal event.
			`printf '{"type":"session","version":3,"id":"pi-fixture-session"}\n'`+"\n"+
			`printf '{"type":"agent_end","messages":[]}\n'`+"\n"+
			"while :; do sleep 1; done\n")
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
	// Nothing on the hook socket: the Pi harness has no Stop hook to report one.
	opts.HookOutcome = ""
	// Hold the loop open until the run itself reaches a terminal event, so the
	// run_failed assertion below reads a bus that has finished talking.
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

	// The signal death every daemon-issued kill produces, announced or not.
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

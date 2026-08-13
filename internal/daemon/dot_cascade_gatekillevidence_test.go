package daemon

// dot_cascade_gatekillevidence_test.go — WHICH lines of a gate log are evidence
// about how the gate ended.
//
// The kill detector was wrong in both directions at once, found live on
// 2026-08-12 by lane bravo driving a real bead end to end:
//
//   - hk-gate-selftest-fakes-a-kill-0hj0i — the gate log is a verbose replay of
//     the whole suite, so it contains the very strings the detector matches. It
//     found the daemon's OWN earlier diagnostic 13,000 lines from the end of a
//     gate that had genuinely failed, called it a kill, and told the operator
//     that NOTHING was known to be wrong with the change.
//   - hk-gate-error-143-still-deterministic-rhske — a signal that reaches only a
//     descendant is reported by the recipe shell as exit 128+N, so make prints
//     `Error 143` and never names the signal, and the top-level shell exits 2
//     cleanly. Neither detector fired and the implementer was sent to fix a
//     fault that does not exist. That is the OOM and SIGTERM shape, i.e. the one
//     a loaded box actually produces.
//
// The two repairs pull against each other: "prefer the recipe failure at the end
// over a kill signature earlier" and "an Error 143 recipe failure IS a kill".
// The precedence rule that holds both is REGIONAL:
//
//	only make's TERMINAL recipe-failure cascade is evidence, and within it a
//	signal WORD or a 128+N exit CODE means killed.
//
// Each test below pins one part of that rule, and they fail in opposite
// directions if it is replaced with either half on its own.
//
// # This file's own output is part of its subject
//
// A message this file writes lands in the next gate's log, where the classifier
// reads it. Two separate things keep that from re-arming the bug, and neither
// one is a claim that the other is unnecessary:
//
//   - gateReportf / gateFatalf put every message through gateEvidenceQuote,
//     which rewrites every string the classifier keys on.
//     TestNoMessageInThisFileReachesTheLogUnsanitized checks structurally that
//     no write bypasses them, and TestTheOutputGuardCatchesTheWaysItWasDefeated
//     measures what that check can and cannot see.
//   - TestNoStringLiteralInThisFileCanRelabelTheNextGate takes the file's own
//     string literals — the fixtures, which are what carries make's shapes into
//     a message — and plants each one, sanitized, ahead of a red cascade.
//
// The guard is not a proof that nothing escapes. It follows writes through
// testing handles, fmt, os.Stdout / os.Stderr, log and the print builtins, named
// by a dotted path of plain identifiers. A write through a value it cannot name
// that way — an io.Writer held in a variable, a handle reached through a method
// call, a helper in another file of this package — is outside it, and so is
// every other file.
//
// Sibling files in this package DO still put the classifier's own strings into
// a failed test's output, and all of them reach a real gate log. `make full`
// runs this package TWICE — `go test -short ./...`, then the scenario tier as
// `go test -v -race -tags=scenario ./internal/daemon` with no -short — and the
// scenario log is cat'd whole with no suppression. Measured 2026-08-12:
// TestDeterministicGateFail_TellsTheImplementerToFixTheFailure, which SKIPs
// under -short, runs and PASSes in that second tier.
//
// The frame that matters: every one of these escapes is failure-conditional.
// Measured over both green tiers of this package on 2026-08-12 — 1381 -short
// tests and 1441 scenario tests, all passing — the log contains ZERO occurrences
// of `*** [`, `] Error 127`, `: command not found` or `is not in std`. The gate
// ToolCommand echoes stay inside CombinedOutput and never reach test stdout, and
// the daemon's own column-0 stderr diagnostic is already sanitized. So the hole
// was armed and silent while the suite was green. It fires when a test FAILS,
// which is exactly when a gate is red and when the classifier's answer is the
// thing that matters. A reviewer reproduced it end to end from one of the sites
// below: the text `go test` writes when that test fails, planted ahead of a real
// red cascade, classified STRUCTURAL, and structural is what tells an
// implementer whose gate is genuinely red that NOTHING is known to be wrong with
// the change.
//
// Which half was covered, and what closed the rest:
//
//   - The `*** [` anchor is covered by position. A raw anchor printed by
//     t.Errorf always lands INDENTED, and gateLineIsIndented keeps an indented
//     line out of make's cascade. That is a reading-end repair; the limit on it
//     is stated on gateLineIsIndented and it is not zero-cost.
//   - The UNSCOPED signatures had no such cover. Their detectors scan the whole
//     log, so no position protects them. Both sites are now CLOSED at the
//     writing end — the t.Errorf in dot_cascade_gatecannotrun_hk2f3v4_test.go
//     (which carried `] Error 127` AND `: command not found`) and the one in
//     dot_cascade_gatekilled_test.go (`] Error 127`, out of a fixture five lines
//     above it) both route their message through gateEvidenceQuote. What keeps
//     them closed is TestNoSignatureInThisPackageReachesTheLogUnsanitized, which
//     reads every test file in this package rather than this one; a third such
//     site fails it by file and line.
//
// What is still open is a different and larger fix, and it is not a test's own
// message. dot_stranded_commit_note_test.go and dot_cascade_gatebackedge_test.go
// leak through unsanitized PRODUCTION paths that carry gate output verbatim —
// gateBackEdgeMessage appends notes raw, and gateFailureTail truncates and folds
// newlines without rewriting anything. Both are in dot_cascade_helpers.go.
// Routing a test's own message through a sanitizer does not touch either one,
// and neither does the package guard, which reads writes and not data flow out
// of production helpers.
//
// These run under -short. Most call the classifier with values; the ones that
// need a real *exec.ExitError get it from `/bin/sh -c 'exit N'` (shellExit), and
// TestTheRemoteReadingOfExit255FollowsTheRunner drives the real
// dispatchDotToolNode over a gate whose whole job is to exit.
//
// One test is heavier on purpose.
// TestARealMakeGateStaysRedWhenTheSuitePrintsThisPackagesOwnMessages builds a
// throwaway Go module in a temp dir and runs a real `make full` over it, because
// the chain this unit exists to break — a failing test prints a message, the
// message lands in the next gate's log, the classifier reads it, the implementer
// is told nothing is wrong — had only ever been confirmed one link at a time. It
// costs well under a second and skips when make is absent. No worktree, no
// agent, no network.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	tmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	"github.com/gregberns/harmonik/internal/workflow/dot"
)

// gateReportf reports a test failure with every string the gate classifier keys
// on rewritten, and gateFatalf does the same and stops the test.
//
// What they buy: `go test -v` replays a failure message into the log of the gate
// that ran the suite, and this package's detectors read that log. A message that
// carries make's recipe anchor is a line make never wrote, sitting where only
// make's own report belongs; a message that carries `] Error 127` is read by a
// detector that scans the whole log and has no position rule to save it. There
// is no margin to spend: a failure message is followed by `--- FAIL`, `FAIL`,
// the package line and `FAIL`, which is exactly gateCascadeGapLines of slack, so
// the message can still be inside the cascade window when make's real cascade
// follows.
func gateReportf(t *testing.T, format string, args ...any) {
	t.Helper()
	t.Error(gateEvidenceQuote(fmt.Sprintf(format, args...)))
}

func gateFatalf(t *testing.T, format string, args ...any) {
	t.Helper()
	t.Fatal(gateEvidenceQuote(fmt.Sprintf(format, args...)))
}

// retiredKillDiagnostic is the SHAPE of the message the classifier used to write
// to stderr for a killed gate: it quoted make's matched line word for word,
// anchor and all. `go test -v` replays stderr, so a line like this one reached
// the next gate's log, and the detector matched it there instead of the real
// failure at the end of the file.
//
// It is a FIXTURE, not a copy, and the wording below is close to but not
// identical to what that run wrote. The 2026-08-12 gate log IS kept, and this
// comment used to say it was gone — an assertion made without looking, in the
// one file whose whole subject is a comment that overran its code. Recover it
// and read the line for yourself. The commit is reachable from branch
// work/bravo-reachability, which is what to check out first if git ever answers
// `bad object` — the log is kept, but nothing here makes that permanent:
//
//	git show 6c0454eab:assessments/2026-08-12-1400-alpha-fixes-and-second-live-run/evidence/commit_gate.log.gz |
//	  gunzip | sed -n 3869p
//
// That is 17,214 lines, with the diagnostic at line 3869 and so 13,345 lines
// SHORT of the end. Short of, not past: the old detector scanned FORWARD from
// the top and returned on its first match, so it answered from this line and
// never read the real cascade 13,345 lines further down. The scan that replaced
// it starts at the end for exactly that reason.
//
// The fixture differs from that line only inside the gate-log path, in two
// places — the $TMPDIR hash segment, and the run id, cut to its first field.
//
// The test needs only one property from it — that it matches the detector BY
// CONTENT, a raw anchor and a signal word on one line — and it asserts that
// property rather than trusting the fixture, which is why nothing below changed
// when the artifact was found.
//
// The message the classifier writes TODAY is a separate question, and
// TestGateKillDiagnosticIsNotItselfAKillSignature derives that one from the
// producer instead of hand-copying it, so it cannot drift.
const retiredKillDiagnostic = `daemon: dot tool node "commit_gate" was KILLED mid-flight ` +
	`(gate output reports a signal kill: make[1]: *** [test-scenario] Terminated: 15) — ` +
	`it reached no verdict, so this is NOT a test failure; canceled, routed to ` +
	`close-needs-attention for triage; gate log: ` +
	`/var/folders/s9/T/TestGateKilledBySignal_MakeOutput294346514/001/.harmonik/gate-logs/019ff7c2/commit_gate.log`

// redGateCascade is the terminal cascade of a gate that RAN and found a fault:
// a test tier that failed, and make giving up above it.
const redGateCascade = "FAIL\nscenario skips: 2\n" +
	"make[1]: *** [test-scenario] Error 1\n" +
	"make: *** [full] Error 2\n"

// gateLogWithTranscript builds a gate log shaped like the live one: a long
// verbose transcript containing transcriptLine, then the terminal cascade.
func gateLogWithTranscript(transcriptLine, cascade string) []byte {
	var b strings.Builder
	b.WriteString("=== RUN   TestGateKilledBySignal_MakeOutput\n")
	b.WriteString(transcriptLine)
	b.WriteString("\n--- PASS: TestGateKilledBySignal_MakeOutput (0.01s)\n")
	for i := 0; i < 500; i++ {
		b.WriteString("--- PASS: TestSomethingElse (0.01s)\n")
	}
	b.WriteString("PASS\nok  \tgithub.com/gregberns/harmonik/test/scenario\t62.697s\n")
	b.WriteString(cascade)
	return []byte(b.String())
}

// shellExit runs `/bin/sh -c 'exit N'` and returns the *exec.ExitError. The gate
// is a shell command, so this is the exact error shape the classifier sees.
func shellExit(t *testing.T, code int) error {
	t.Helper()
	//nolint:gosec // G204: the exit code is an int this test chose; no external input reaches it
	err := exec.CommandContext(t.Context(), "/bin/sh", "-c", "exit "+strconv.Itoa(code)).Run()
	if err == nil {
		gateFatalf(t, "`exit %d` reported success", code)
	}
	return err
}

// classifyGateLog runs the real classifier over a LOCAL gate that exited 2 —
// what make itself exits when a recipe failed — and returns the class and the
// description the daemon would log.
func classifyGateLog(t *testing.T, log []byte) (class core.FailureClass, logDesc string) {
	t.Helper()
	return classifyDotToolNodeFailure(shellExit(t, 2), nil, nil, log, "commit_gate", 3600, false)
}

// TestRedGateStaysDeterministicWhenTheTranscriptReplaysAKill is the first
// direction: the gate RAN and found a fault, and a kill signature earlier in the
// transcript must not take that away. Without the terminal-cascade rule this
// input classifies canceled, and the implementer is told nothing is wrong with a
// change whose tests failed. (hk-gate-selftest-fakes-a-kill-0hj0i)
func TestRedGateStaysDeterministicWhenTheTranscriptReplaysAKill(t *testing.T) {
	t.Parallel()

	for name, transcript := range map[string]string{
		// The message the classifier used to write for a killed gate.
		"daemon diagnostic": retiredKillDiagnostic,
		// A gate step that echoes make's kill output as its own fixture. No go
		// test log prefix on it, so POSITION is the only thing that can rule it
		// out — which is the property this test is for.
		"echoed fixture": "make[1]: *** [test-scenario] Terminated: 15",
	} {
		// Positive evidence first: each of these lines DOES match the detector by
		// content, so the claim below is about WHERE it sits and nothing else. A
		// fixture that stopped matching would make the rest of this test vacuous.
		if _, ok := gateSignalKillOutputLine([]byte(transcript + "\n")); !ok {
			gateFatalf(t, "%s: the fixture no longer reads as a kill even when it IS the whole cascade, so this test can no longer fail", name)
		}

		log := gateLogWithTranscript(transcript, redGateCascade)

		if line, ok := gateSignalKillOutputLine(log); ok {
			gateReportf(t, "%s: a gate that ran and failed reads as killed (matched %q); the verdict is the cascade at the END of the log, not a line the suite printed", name, line)
		}

		if class, _ := classifyGateLog(t, log); class != core.FailureClassDeterministic {
			gateReportf(t, "%s: a red gate is classified %q; deterministic is what sends the implementer back to fix the failure it actually found", name, class)
		}
	}
}

// TestGateKilledThroughADescendantClassifiesCanceled is the opposite direction.
// The signal reached a child, so the exit state is a clean exit 2 and the output
// never says "Terminated" — the only evidence is make's 128+N exit code, and it
// is the LAST thing in the log. (hk-gate-error-143-still-deterministic-rhske)
func TestGateKilledThroughADescendantClassifiesCanceled(t *testing.T) {
	t.Parallel()

	for name, cascade := range map[string]string{
		// SIGTERM to a child: what a loaded box or an operator stop produces.
		"SIGTERM (143)": "FAIL\tgithub.com/gregberns/harmonik/internal/daemon\t120.0s\n" +
			"make[1]: *** [test-scenario] Error 143\nmake: *** [full] Error 2\n",
		// SIGKILL from the OOM killer.
		"OOM (137)": "make[1]: *** [test-scenario] Error 137\nmake: *** [full] Error 2\n",
		// GNU make with -w interleaves bookkeeping between the cascade members.
		"under make -w": "make[1]: *** [Makefile:41: test-scenario] Error 143\n" +
			"make[1]: Leaving directory '/repo'\nmake: *** [Makefile:10: full] Error 2\n",
	} {
		log := gateLogWithTranscript("--- PASS: TestUnrelated (0.01s)", cascade)

		class, desc := classifyGateLog(t, log)
		if class != core.FailureClassCanceled {
			gateReportf(t, "%s: a gate killed through a descendant is classified %q; the gate reached NO verdict, and deterministic is what tells the implementer to fix a fault nobody observed", name, class)
		}

		// Positive evidence that the class reached the reader, not just the log.
		msg := gateBackEdgeMessage(class, desc)
		if strings.Contains(msg, "Fix the failure") {
			gateReportf(t, "%s: the implementer is told to fix a failure the gate never observed:\n%s", name, msg)
		}
	}
}

// TestTerminalCascadeIsFoundBehindTrailingBlankLines pins the trim at the top of
// gateTerminalRecipeFailures. The cascade is the last thing MAKE writes, but the
// bytes the daemon captures do not have to end there: a shell that echoes after
// the gate, and a transport that flushes the stream, both leave empty lines
// after it. Empty lines are not recipe failures, so without the trim they are
// counted as gap, and past gateCascadeGapLines of them the whole cascade falls
// out of reach and a killed gate is read as a verdict.
func TestTerminalCascadeIsFoundBehindTrailingBlankLines(t *testing.T) {
	t.Parallel()

	// One more blank line than the gap tolerance allows, so the trim is the only
	// thing that can keep the cascade reachable. Tied to the constant, so it
	// still crosses the line if the tolerance changes.
	padding := strings.Repeat("\n", gateCascadeGapLines+1)
	log := gateLogWithTranscript("--- PASS: TestUnrelated (0.01s)",
		"make[1]: *** [test-scenario] Error 143\nmake: *** [full] Error 2\n"+padding)

	cascade := gateTerminalRecipeFailures(log)
	if len(cascade) != 2 {
		gateFatalf(t, "a cascade followed by %d blank lines yields %d recipe-failure lines, want 2: %q", gateCascadeGapLines+1, len(cascade), cascade)
	}
	if class, _ := classifyGateLog(t, log); class != core.FailureClassCanceled {
		gateReportf(t, "a killed gate whose output ends in blank lines is classified %q; the blank lines are not evidence and must not push make's own report out of reach", class)
	}
}

// TestGateKillDiagnosticIsNotItselfAKillSignature is the durable half of the
// first bug. A detector whose own diagnostic matches the detector arms every
// future run against itself, and no amount of care in one test prevents the next
// one from doing it again. The diagnostic must describe the evidence rather than
// replay it. (hk-gate-selftest-fakes-a-kill-0hj0i)
func TestGateKillDiagnosticIsNotItselfAKillSignature(t *testing.T) {
	t.Parallel()

	killed := []byte("make[1]: *** [test-scenario] Terminated: 15\nmake: *** [full] Terminated: 15\n")
	_, desc := classifyGateLog(t, killed)
	if desc == "" {
		gateFatalf(t, "a killed gate logged nothing at all")
	}
	if strings.Contains(desc, gateRecipeFailureAnchor) {
		gateReportf(t, "the kill diagnostic reproduces make's recipe anchor, so a later gate log that quotes it matches this detector:\n%s", desc)
	}
	// The anchor is only one of the strings the classifier keys on. The rest are
	// read anywhere in the log, so a diagnostic carrying one needs no position at
	// all to be believed. (BLOCKING 1, round 2)
	if isGateCannotRunError([]byte(desc)) || isGateBuildCacheInfraError([]byte(desc)) {
		gateReportf(t, "the kill diagnostic carries a string an UNSCOPED detector matches, so quoting it in a later gate log relabels that gate wherever the line lands:\n%s", desc)
	}
	// It must still name the recipe and the signal — a diagnostic that cannot
	// trip the detector is only useful if it still tells the reader what died.
	if !strings.Contains(desc, "test-scenario") || !strings.Contains(desc, "Terminated: 15") {
		gateReportf(t, "the kill diagnostic no longer says which recipe died or how:\n%s", desc)
	}

	// Feed the diagnostic back in as transcript, ahead of a real red cascade —
	// which is exactly what happened live.
	replayed := gateLogWithTranscript("daemon: "+desc, redGateCascade)
	if class, _ := classifyGateLog(t, replayed); class != core.FailureClassDeterministic {
		gateReportf(t, "a red gate whose log replays the daemon's own kill diagnostic is classified %q; the detector is matching itself again", class)
	}
}

// TestExitCodeInSignalRangeIsAKillAndBelowItIsAVerdict pins the boundary the
// 128+N reading turns on. Below 129 the number is an exit status a command
// chose, and those must stay verdicts — `Error 127` in particular belongs to the
// structural branch, which is the one that says a tool is missing.
func TestExitCodeInSignalRangeIsAKillAndBelowItIsAVerdict(t *testing.T) {
	t.Parallel()

	for _, line := range []string{
		"make[1]: *** [test-scenario] Error 129",
		"make[1]: *** [test-scenario] Error 137",
		"make[1]: *** [test-scenario] Error 143",
		"make[1]: *** [test-scenario] Error 255",
	} {
		if !gateRecipeLineNamesAKill(line) {
			gateReportf(t, "a 128+N exit code reads as a verdict: %q", line)
		}
	}
	for _, line := range []string{
		"make[1]: *** [test] Error 1",
		"make: *** [full] Error 2",
		"make[2]: *** [fmt-check] Error 127",
		"make[1]: *** [test-scenario] Error 128",
	} {
		if gateRecipeLineNamesAKill(line) {
			gateReportf(t, "an exit status the command chose reads as a kill: %q", line)
		}
	}

	// The structural branch still owns a missing tool, ahead of deterministic and
	// behind the kill branch.
	log := []byte("go: gofumpt not found\nmake[2]: *** [fmt-check] Error 127\nmake: *** [full] Error 2\n")
	if class, _ := classifyGateLog(t, log); class != core.FailureClassStructural {
		gateReportf(t, "a gate that could not RUN is classified %q, not structural", class)
	}
}

// TestRemoteKillIsStillSeenBehindSSHTrailerLines defends the case the output
// detector exists for. On a REMOTE run the exit status belongs to the local ssh
// client, so make's recipe line is the only evidence there is — and ssh writes a
// line of its own after the remote command ends. Scoping the detector to the end
// of the log must not put the kill out of reach.
func TestRemoteKillIsStillSeenBehindSSHTrailerLines(t *testing.T) {
	t.Parallel()

	log := []byte("--- PASS: TestSomething (0.01s)\n" +
		"make[1]: *** [test-scenario] Terminated: 15\n" +
		"make: *** [full] Terminated: 15\n" +
		"Shared connection to worker-01 closed.\n" +
		"Connection to worker-01 closed.\n")

	if _, ok := gateSignalKillOutputLine(log); !ok {
		gateFatalf(t, "a killed REMOTE gate reads as a clean exit once ssh has written its own trailer; the output detector is the only evidence a remote kill leaves")
	}
	class, _ := classifyDotToolNodeFailure(shellExit(t, 2), nil, nil, log, "commit_gate", 3600, true)
	if class != core.FailureClassCanceled {
		gateFatalf(t, "a killed remote gate is classified %q", class)
	}
}

// TestRemoteGateWhoseSSHFailedClassifiesCanceled covers the remote transport.
// ssh exits 255 when the connection failed or when the remote command died from
// a signal it cannot report; either way the gate reached no verdict. The same
// number from a LOCAL gate is an exit status the command chose, so the reading
// is available only on the remote path. It also swallows a remote gate that
// genuinely chose to exit 255 — see the `remote` paragraph on
// classifyDotToolNodeFailure for why that trade is taken.
// (hk-gate-error-143-still-deterministic-rhske)
func TestRemoteGateWhoseSSHFailedClassifiesCanceled(t *testing.T) {
	t.Parallel()

	// A killed remote gate whose output carries no make recipe line at all.
	log := []byte("go: downloading github.com/foo/bar v1.2.3\n")
	sshErr := shellExit(t, 255)

	class, _ := classifyDotToolNodeFailure(sshErr, nil, nil, log, "commit_gate", 3600, true)
	if class != core.FailureClassCanceled {
		gateReportf(t, "a remote gate whose ssh exited 255 is classified %q; the transport dropped or a signal ended it, so it reached no verdict", class)
	}

	local, _ := classifyDotToolNodeFailure(sshErr, nil, nil, log, "commit_gate", 3600, false)
	if local != core.FailureClassDeterministic {
		gateReportf(t, "a LOCAL gate that exited 255 is classified %q; there is no ssh on that path, so 255 is an exit status the gate command chose", local)
	}
}

// dispatchGateThatExits255 drives the REAL dispatchDotToolNode over a gate whose
// only act is to exit 255, and returns the failure class it produced.
//
// runner is the only thing that differs between the two calls in
// TestTheRemoteReadingOfExit255FollowsTheRunner, so the class this returns is
// derived from runner exactly the way production derives it. Calling the
// classifier with a bool literal proves nothing about that wiring: the
// production call site is what turns a runner into `remote`, and a test that
// passes the bool itself never touches it.
//
// A RecordingRunner with no CmdFunc runs the command locally, which is enough —
// dispatchDotToolNode only asks the runner for an *exec.Cmd, and a shell that
// exits 255 is the error shape ssh produces when the transport drops.
func dispatchGateThatExits255(t *testing.T, runner tmux.CommandRunner) core.FailureClass {
	t.Helper()
	node := &dot.Node{
		ID:          "commit_gate",
		Type:        core.NodeTypeNonAgentic,
		HandlerRef:  "shell",
		ToolCommand: "exit 255",
		Timeout:     "30",
	}
	outcome, err := dispatchDotToolNode(t.Context(), nil, gateLogNewRunID(t), runner, t.TempDir(), t.TempDir(), node, nil)
	if err != nil {
		gateFatalf(t, "dispatch: %v", err)
	}
	if outcome.FailureClass == nil {
		gateFatalf(t, "a gate FAIL carries no failure class")
	}
	return *outcome.FailureClass
}

// TestTheRemoteReadingOfExit255FollowsTheRunner pins the production wiring: the
// gate is remote when, and only when, a runner ran it. The classifier's `remote`
// parameter is only as good as the one call site that fills it in, and every
// other test in this file hands that parameter a literal.
func TestTheRemoteReadingOfExit255FollowsTheRunner(t *testing.T) {
	t.Parallel()

	if class := dispatchGateThatExits255(t, nil); class != core.FailureClassDeterministic {
		gateReportf(t, "a gate the daemon ran itself is classified %q for exit 255; no ssh ran, so 255 is an exit status the gate chose and the implementer must see it", class)
	}

	rr := &tmux.RecordingRunner{}
	if class := dispatchGateThatExits255(t, rr); class != core.FailureClassCanceled {
		gateReportf(t, "a gate a runner ran is classified %q for exit 255; that is ssh reporting a dropped transport or a signalled remote gate, and neither is a verdict about the code", class)
	}
	// Positive evidence that the runner path was the one taken, rather than the
	// class arriving for some other reason.
	if len(rr.Calls) == 0 {
		gateReportf(t, "the runner was never asked for a command, so the remote branch never ran")
	}
}

// TestTheSanitizerCoversEveryStringTheClassifierKeysOn is BLOCKING 1 from the
// round-2 review, and it is the assertion that the sanitizer's coverage is
// DERIVED from the detectors rather than kept beside them.
//
// gateEvidenceQuote used to strip make's recipe anchor and nothing else. The
// anchor is the entry condition for the CASCADE-SCOPED detectors, so it defended
// those completely — and it left every UNSCOPED detector wide open. The
// classifier reads `] Error 127` and `: command not found` anywhere in the log,
// with no position rule at all, so this file's own failure message
//
//	an exit status the command chose reads as a kill: "make[2]: recipe [fmt-check] Error 127"
//
// classified the NEXT genuinely red gate as structural, and the implementer was
// told "NOTHING is known to be wrong with your change".
//
// The loop below is over the production tables, not over a list written here, so
// it covers a signature added tomorrow.
func TestTheSanitizerCoversEveryStringTheClassifierKeysOn(t *testing.T) {
	t.Parallel()

	if len(gateUnscopedSignatures) == 0 {
		gateFatalf(t, "the unscoped-signature table is empty, so every assertion below is vacuous")
	}

	for _, sig := range gateUnscopedSignatures {
		if sig.quoted == "" {
			gateReportf(t, "signature %q has no rewritten form, so the sanitizer leaves it in place", sig.text)
			continue
		}
		if strings.Contains(sig.quoted, sig.text) {
			gateReportf(t, "signature %q rewrites to %q, which still contains it", sig.text, sig.quoted)
		}

		// The line a diagnostic would carry: make's own report about a recipe,
		// with this signature on it.
		raw := "make[2]: *** [fmt-check] " + sig.text
		clean := gateEvidenceQuote(raw)

		if strings.Contains(clean, sig.text) {
			gateReportf(t, "the sanitizer leaves %q in a message it rendered: %q", sig.text, clean)
		}
		if isGateCannotRunError([]byte(clean)) || isGateBuildCacheInfraError([]byte(clean)) {
			gateReportf(t, "a sanitized message still trips an unscoped detector: %q", clean)
		}

		// NEGATIVE CONTROL. The raw line must change the class, or the assertion
		// below measures nothing at all.
		if class, _ := classifyGateLog(t, gateLogWithTranscript(raw, redGateCascade)); class == core.FailureClassDeterministic {
			gateFatalf(t, "signature %q planted RAW in a red gate's transcript leaves the class deterministic, so this test cannot fail and proves nothing", sig.text)
		}
		if class, _ := classifyGateLog(t, gateLogWithTranscript(clean, redGateCascade)); class != core.FailureClassDeterministic {
			gateReportf(t, "a red gate whose transcript replays the sanitized form of %q is classified %q; the implementer is told NOTHING is wrong with a change whose tests failed", sig.text, class)
		}
	}

	// The cascade-scoped words are NOT rewritten, on purpose: naming the signal
	// is the value of the diagnostic. What makes that safe is that losing the
	// anchor puts the line outside the only region that reads them. Assert it
	// rather than assume it.
	if len(gateCascadeKillWords) == 0 {
		gateFatalf(t, "the cascade kill-word table is empty, so the loop below is vacuous")
	}
	for _, word := range gateCascadeKillWords {
		raw := "make[1]: *** [test-scenario] " + word + ": 15"
		clean := gateEvidenceQuote(raw)
		if !strings.Contains(clean, word) {
			gateReportf(t, "the sanitizer removed %q, so the diagnostic no longer says which signal ended the gate: %q", word, clean)
		}
		if n := len(gateTerminalRecipeFailures([]byte(clean))); n != 0 {
			gateReportf(t, "a sanitized line naming %q is still admitted to make's cascade (%d member(s)), so position no longer protects it: %q", word, n, clean)
		}
		// NEGATIVE CONTROL for the same claim.
		if n := len(gateTerminalRecipeFailures([]byte(raw))); n == 0 {
			gateFatalf(t, "the RAW line naming %q is not a cascade member either, so the check above proves nothing: %q", word, raw)
		}
	}
}

// TestALineAGoTestWroteIsNeverMakesOwnReport pins the second half of the
// position rule. Scoping to the tail of the log is not enough on its own,
// because the tail has a soft edge: gateTerminalRecipeFailures tolerates
// gateCascadeGapLines of slack between members, and go test's failure trailer is
// exactly that many lines. A raw anchor that escaped some OTHER test's failure
// message therefore lands inside the window, ahead of make's real cascade, and
// is admitted as its first member — the original bug with a four-line reach
// instead of a 13,000-line one.
//
// Sibling test files in this package do still write raw anchors into their
// failure messages, so this is not hypothetical; the exclusion is what covers
// them, because nothing in this file can reach their message sites.
func TestALineAGoTestWroteIsNeverMakesOwnReport(t *testing.T) {
	t.Parallel()

	// Two shapes, both live in this package today. go test renders a one-line
	// message with the source position on it, and a message written as
	// "…:\n%s" as an indented continuation line with no position at all. The
	// second is why the rule is INDENTATION and not go test's `file.go:NN: `
	// prefix: only the first line of a message carries that prefix, and the
	// anchor in this package's sibling files sits on the second.
	for name, escaped := range map[string]string{
		"one-line message":  "    dot_cascade_gatekilled_test.go:41: a gate that was killed reads as a clean exit: make[1]: *** [test-scenario] Terminated: 15",
		"continuation line": "        make[1]: *** [test-scenario] Terminated: 15",
	} {
		gateAssertEscapedAnchorIsNotEvidence(t, name, escaped)
	}
}

// gateAssertEscapedAnchorIsNotEvidence plants one line a TEST wrote, carrying a
// raw recipe anchor, inside the cascade gap window right ahead of make's real
// red cascade, and requires the gate to stay deterministic.
func gateAssertEscapedAnchorIsNotEvidence(t *testing.T, name, escaped string) {
	t.Helper()

	trailer := []string{
		"--- FAIL: TestGateSignalKillOutputLine (0.01s)",
		"FAIL",
		"FAIL\tgithub.com/gregberns/harmonik/internal/daemon\t1.0s",
		"FAIL",
	}
	// The trailer being exactly the tolerance is WHY the escaped line is in
	// reach. Tie the claim to the constant so it stays true if either changes.
	if len(trailer) != gateCascadeGapLines {
		gateFatalf(t, "go test's failure trailer is %d lines and the cascade gap tolerance is %d; this test no longer sets up the case it describes", len(trailer), gateCascadeGapLines)
	}

	log := []byte("=== RUN   TestGateSignalKillOutputLine\n" + escaped + "\n" +
		strings.Join(trailer, "\n") + "\n" +
		"make[1]: *** [test-unit] Error 1\n" +
		"make: *** [full] Error 2\n")

	// Positive evidence about the mechanism: the rule holds for the line go test
	// wrote and not for the lines make wrote.
	if !gateLineIsIndented(escaped) {
		// Reported, not fatal: the classification assertion below is the
		// consequence, and both cases in the caller's table still run.
		gateReportf(t, "%s: a line go test wrote does not read as indented, so nothing keeps it out of make's cascade: %q", name, escaped)
	}
	for _, made := range []string{"make[1]: *** [test-unit] Error 1", "make: *** [full] Error 2"} {
		if gateLineIsIndented(made) {
			gateReportf(t, "%s: a line MAKE wrote reads as indented, which would delete make's own report from the cascade: %q", name, made)
		}
	}

	cascade := gateTerminalRecipeFailures(log)
	if len(cascade) != 2 {
		gateReportf(t, "%s: the cascade has %d member(s), want the 2 make wrote: %q", name, len(cascade), cascade)
	}
	if class, _ := classifyGateLog(t, log); class != core.FailureClassDeterministic {
		gateReportf(t, "%s: a red gate whose log carries a raw anchor that escaped another test's failure message is classified %q; the implementer is told NOTHING is wrong with a change whose tests failed", name, class)
	}
}

// TestNoStringLiteralInThisFileCanRelabelTheNextGate is the end-to-end form of
// the same claim, and it is derived from the file rather than from a list.
//
// What carries make's shapes into a message is the FIXTURES — a log this file
// builds, a recipe line it hands the detector — and every one of them is a
// string literal here. So: take each literal the classifier would react to, put
// it through the sanitizer the way gateReportf does, plant it in the transcript
// of a gate that RAN and FAILED, and require the class to stay deterministic.
//
// What it does not cover: values this file never spells out, such as a string
// the classifier returned. TestGateKillDiagnosticIsNotItselfAKillSignature owns
// that one. `%q` is not a hole — Go quotes these strings without escaping any
// character a detector keys on, so the literal is representative of what a
// message carries.
func TestNoStringLiteralInThisFileCanRelabelTheNextGate(t *testing.T) {
	t.Parallel()

	fset := token.NewFileSet()
	file := gateParseThisFile(t, fset)

	var interesting, unscoped int
	ast.Inspect(file, func(n ast.Node) bool {
		lit, isLit := n.(*ast.BasicLit)
		if !isLit || lit.Kind != token.STRING {
			return true
		}
		text, err := strconv.Unquote(lit.Value)
		if err != nil || !gateLiteralIsClassifierBait(text) {
			return true
		}
		interesting++

		clean := gateEvidenceQuote(text)
		if class, _ := classifyGateLog(t, gateLogWithTranscript(clean, redGateCascade)); class != core.FailureClassDeterministic {
			gateReportf(t, "line %d: a red gate whose transcript replays the sanitized form of this file's own literal is classified %q; the implementer is told NOTHING is wrong with a change whose tests failed:\n%s",
				fset.Position(lit.Pos()).Line, class, clean)
		}

		// NEGATIVE CONTROL, for the literals that carry an unscoped signature:
		// the RAW form must flip the class, or the check above is vacuous for
		// exactly the case that broke live.
		if gateOutputHasAnySignature(text, gateUnscopedSignatures) {
			unscoped++
			if class, _ := classifyGateLog(t, gateLogWithTranscript(text, redGateCascade)); class == core.FailureClassDeterministic {
				gateFatalf(t, "line %d: this literal carries an unscoped detector string and yet leaves a red gate deterministic when planted RAW, so the sanitized check proves nothing", fset.Position(lit.Pos()).Line)
			}
		}
		return true
	})

	if interesting == 0 {
		gateFatalf(t, "no string literal in this file looks like anything the classifier reacts to; the walk found nothing and its clean result means nothing")
	}
	if unscoped == 0 {
		gateFatalf(t, "no literal in this file carries an UNSCOPED detector string, so the case that broke live has no negative control here")
	}
}

// gateLiteralIsClassifierBait reports whether a string is something the gate
// classifier reacts to at all: make's recipe anchor, a cascade kill word, or any
// unscoped signature.
func gateLiteralIsClassifierBait(text string) bool {
	if strings.Contains(text, gateRecipeFailureAnchor) || gateOutputHasAnySignature(text, gateUnscopedSignatures) {
		return true
	}
	for _, word := range gateCascadeKillWords {
		if strings.Contains(text, word) {
			return true
		}
	}
	return false
}

// gateTestOutputMethods are the testing-handle methods that put text in the test
// log. -v replays all of them, including the ones that do not fail.
var gateTestOutputMethods = map[string]bool{
	"Error": true, "Errorf": true, "Fatal": true, "Fatalf": true,
	"Log": true, "Logf": true, "Skip": true, "Skipf": true,
}

// gateSanitizerName is the one function that makes a string safe to write from
// this package. A write is exempt because the guard SAW this call on it, not
// because the enclosing function is on a list of approved names — a name list
// exempts a third helper that strips nothing.
const gateSanitizerName = "gateEvidenceQuote"

// gateOutputSites reports every call in file that puts text into the test log or
// onto a standard stream, split by whether the guard could see the message go
// through gateEvidenceQuote first.
//
// WHAT IT FOLLOWS. A call is a write when it is one of:
//
//   - a method in gateTestOutputMethods on a testing handle. Handles are found
//     by type — *testing.T / *testing.B / *testing.F / testing.TB — over every
//     parameter list in the file, function literals included, so a `t.Run`
//     closure whose parameter is not called `t` is covered and so is a helper
//     that takes testing.TB;
//   - fmt.Print* / fmt.Fprint*, log.*, a method on os.Stdout / os.Stderr, or the
//     print / println builtins.
//
// The receiver is resolved as a dotted path of plain identifiers, so
// `os.Stderr.WriteString` is followed and not just `os.Stderr`. A path whose
// last element is a handle name counts too, which covers a handle reached
// through a field.
//
// WHAT IT DOES NOT FOLLOW, stated because a guard that overstates its reach is
// the defect this file exists to repair: a write through a value the guard
// cannot name as a dotted path of identifiers — an io.Writer in a variable, a
// handle returned by a method call, a handle in a slice or a map — and any write
// made by a helper in ANOTHER file of this package. It reads one file; the
// package-wide tier is gateSignatureWriteSites, which trades this one's "every
// write" reach for "every file".
//
// A write is SANITIZED only when its message is exactly one argument and that
// argument is a direct call to gateEvidenceQuote. Anything looser lets a raw
// format string travel next to a sanitized value.
func gateOutputSites(fset *token.FileSet, file *ast.File) (unsanitized, sanitized []string) {
	handles := gateTestingHandleNames(file)
	ast.Inspect(file, func(n ast.Node) bool {
		call, isCall := n.(*ast.CallExpr)
		if !isCall {
			return true
		}
		channel, msgArgs, isWrite := gateOutputChannel(call, handles)
		if !isWrite {
			return true
		}
		site := fmt.Sprintf("%d: %s", fset.Position(call.Pos()).Line, channel)
		if gateArgsAreSanitized(msgArgs) {
			sanitized = append(sanitized, site)
		} else {
			unsanitized = append(unsanitized, site)
		}
		return true
	})
	return unsanitized, sanitized
}

// gateTestingHandleNames collects the name of every parameter in file whose type
// is one of testing's handles, from function declarations and function literals
// alike.
//
// It keys on names and ignores scope. Measured on this file, that adds checked
// sites rather than removing them: a name bound to a testing handle anywhere makes
// every write through that name a checked site everywhere, including where it is
// not a handle. That is not a proof that scope-blindness can only ever over-report,
// and it is not what keeps the guard honest — the miss to know about is a handle the
// collector never sees at all, such as one held in a local variable rather than
// taken as a parameter. The header comment on the guard states that limit.
func gateTestingHandleNames(file *ast.File) map[string]bool {
	names := map[string]bool{}
	collect := func(params *ast.FieldList) {
		if params == nil {
			return
		}
		for _, param := range params.List {
			if !gateTypeIsTestingHandle(param.Type) {
				continue
			}
			for _, name := range param.Names {
				names[name.Name] = true
			}
		}
	}
	ast.Inspect(file, func(n ast.Node) bool {
		switch fn := n.(type) {
		case *ast.FuncDecl:
			collect(fn.Type.Params)
		case *ast.FuncLit:
			collect(fn.Type.Params)
		}
		return true
	})
	return names
}

// gateTypeIsTestingHandle reports whether an AST type is a testing handle:
// *testing.T, *testing.B, *testing.F, or the testing.TB interface.
func gateTypeIsTestingHandle(expr ast.Expr) bool {
	if star, isStar := expr.(*ast.StarExpr); isStar {
		expr = star.X
	}
	sel, isSel := expr.(*ast.SelectorExpr)
	if !isSel {
		return false
	}
	pkg, isIdent := sel.X.(*ast.Ident)
	if !isIdent || pkg.Name != "testing" {
		return false
	}
	switch sel.Sel.Name {
	case "T", "B", "F", "TB":
		return true
	}
	return false
}

// gateOutputChannel classifies a call as a write to the test log or a standard
// stream, and returns the channel's name and the arguments that carry the
// message.
func gateOutputChannel(call *ast.CallExpr, handles map[string]bool) (channel string, msgArgs []ast.Expr, ok bool) {
	// The builtins take no package qualifier and write straight to stderr.
	if id, isIdent := call.Fun.(*ast.Ident); isIdent {
		if id.Name == "print" || id.Name == "println" {
			return id.Name, call.Args, true
		}
		return "", nil, false
	}
	sel, isSel := call.Fun.(*ast.SelectorExpr)
	if !isSel {
		return "", nil, false
	}
	path := gateSelectorPath(sel)
	if path == "" {
		return "", nil, false
	}
	recv := path[:strings.LastIndex(path, ".")]
	method := sel.Sel.Name

	switch {
	case gateReceiverIsTestingHandle(recv, handles):
		if gateTestOutputMethods[method] {
			return path, call.Args, true
		}
	case recv == "fmt":
		if strings.HasPrefix(method, "Fprint") {
			// The writer is the first argument; the message follows it.
			return path, call.Args[1:], true
		}
		if strings.HasPrefix(method, "Print") {
			return path, call.Args, true
		}
	case recv == "log", recv == "os.Stdout", recv == "os.Stderr":
		return path, call.Args, true
	}
	return "", nil, false
}

// gateReceiverIsTestingHandle reports whether a dotted receiver path names a
// testing handle, either outright (`t`) or as its last element (`fixture.t`).
func gateReceiverIsTestingHandle(recv string, handles map[string]bool) bool {
	if handles[recv] {
		return true
	}
	if i := strings.LastIndex(recv, "."); i >= 0 {
		return handles[recv[i+1:]]
	}
	return false
}

// gateSelectorPath renders a selector as a dotted path when every element of it
// is a plain identifier — `t.Errorf`, `os.Stderr.WriteString`. It returns "" for
// anything else, which the guard cannot follow; the doc on gateOutputSites says
// so.
func gateSelectorPath(sel *ast.SelectorExpr) string {
	switch base := sel.X.(type) {
	case *ast.Ident:
		return base.Name + "." + sel.Sel.Name
	case *ast.SelectorExpr:
		path := gateSelectorPath(base)
		if path == "" {
			return ""
		}
		return path + "." + sel.Sel.Name
	}
	return ""
}

// gateArgsAreSanitized reports whether a write's message is exactly one call to
// the sanitizer.
func gateArgsAreSanitized(args []ast.Expr) bool {
	if len(args) != 1 {
		return false
	}
	call, isCall := args[0].(*ast.CallExpr)
	if !isCall {
		return false
	}
	id, isIdent := call.Fun.(*ast.Ident)
	return isIdent && id.Name == gateSanitizerName
}

// gateParseThisFile parses the source file this call sits in.
func gateParseThisFile(t *testing.T, fset *token.FileSet) *ast.File {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		gateFatalf(t, "cannot find this file's own path, so the guard checked nothing")
	}
	file, err := parser.ParseFile(fset, thisFile, nil, parser.SkipObjectResolution)
	if err != nil {
		gateFatalf(t, "parse %s: %v", thisFile, err)
	}
	return file
}

// TestNoMessageInThisFileReachesTheLogUnsanitized keeps the first bug from being
// re-armed by this file's own output.
//
// It is structural on purpose. A list of the message sites known today goes
// stale the moment somebody adds the next one, and the bug it guards against is
// exactly the one a careful author reintroduces without noticing. What it can
// and cannot see is written on gateOutputSites, and
// TestTheOutputGuardCatchesTheWaysItWasDefeated measures it rather than
// asserting it.
func TestNoMessageInThisFileReachesTheLogUnsanitized(t *testing.T) {
	t.Parallel()

	// The guard is worth nothing if the strip does not strip.
	if q := gateEvidenceQuote("make[1]: " + gateRecipeFailureAnchor + "test-scenario] Error 143"); strings.Contains(q, gateRecipeFailureAnchor) {
		gateReportf(t, "gateEvidenceQuote leaves make's recipe anchor in place, so every message in this file carries it")
	}

	fset := token.NewFileSet()
	unsanitized, sanitized := gateOutputSites(fset, gateParseThisFile(t, fset))

	if len(unsanitized) > 0 {
		gateReportf(t, "these write to the test log without passing the text through %s, so it can be read as make's own report — or as a missing tool — in the next gate's log: %s",
			gateSanitizerName, strings.Join(unsanitized, ", "))
	}
	// Positive evidence that the walk found call sites at all. An empty result
	// from a broken walk looks exactly like a clean file.
	if len(sanitized) < 2 {
		gateFatalf(t, "the guard found %d sanitized write(s) (%s); this file has at least two, so the walk is not reading the file it thinks it is", len(sanitized), strings.Join(sanitized, ", "))
	}
}

// TestTheOutputGuardCatchesTheWaysItWasDefeated measures the guard instead of
// trusting it. Every hostile case below is a construct that DID walk past the
// guard's first version, verified by an independent reviewer with a raw anchor
// reaching the log; the guard's doc comment claimed the invariant was closed
// while four ordinary Go constructs went straight through it. A guard that is
// only read, never broken on purpose, measures nothing.
func TestTheOutputGuardCatchesTheWaysItWasDefeated(t *testing.T) {
	t.Parallel()

	for name, body := range map[string]string{
		// The four the reviewer got through.
		"helper taking testing.TB":    `func h(tb testing.TB) { tb.Errorf("x") }`,
		"t.Run closure param not `t`": `func A(t *testing.T) { t.Run("s", func(u *testing.T) { u.Errorf("x") }) }`,
		"println builtin":             `func B(t *testing.T) { println("x") }`,
		"os.Stderr.WriteString":       `func C(t *testing.T) { _, _ = os.Stderr.WriteString("x") }`,
		// The two it already caught — kept so a rewrite cannot lose them.
		"renamed top-level param":   `func D(u *testing.T) { u.Error("x") }`,
		"fmt.Fprintln to os.Stderr": `func E(t *testing.T) { fmt.Fprintln(os.Stderr, "x") }`,
		// Ways to look sanitized without being sanitized.
		"sanitized value, raw format":  `func F(t *testing.T) { t.Errorf("%s", gateEvidenceQuote("x")) }`,
		"sanitized value, raw suffix":  `func G(t *testing.T) { t.Error(gateEvidenceQuote("x") + "make[1]: *** [t] Error 1") }`,
		"benchmark handle":             `func H(b *testing.B) { b.Fatalf("x") }`,
		"log package":                  `func I(t *testing.T) { log.Printf("x") }`,
		"handle reached through field": `func J(t *testing.T) { f := fixture{t: t}; f.t.Errorf("x") }`,
	} {
		if got := gateScanSnippet(t, body); len(got) == 0 {
			gateReportf(t, "%s walks past the guard: %s", name, body)
		}
	}

	for name, body := range map[string]string{
		"the sanitizing helper itself": `func K(t *testing.T, f string, a ...any) { t.Error(gateEvidenceQuote(fmt.Sprintf(f, a...))) }`,
		"non-output methods":           `func L(t *testing.T) { t.Helper(); t.Parallel(); _ = t.TempDir() }`,
		"a call to such a helper":      `func M(t *testing.T) { t.Run("s", func(u *testing.T) { gateReportf(u, "x") }) }`,
		"Sprintf is not a write":       `func N(t *testing.T) { _ = fmt.Sprintf("make[1]: *** [t] Error 1") }`,
	} {
		if got := gateScanSnippet(t, body); len(got) > 0 {
			gateReportf(t, "%s is reported as an unsanitized write, which would push authors to work around the guard: %s → %s", name, body, strings.Join(got, ", "))
		}
	}
}

// gateScanSnippet runs the guard over one synthetic declaration and returns the
// unsanitized writes it found.
func gateScanSnippet(t *testing.T, body string) []string {
	t.Helper()
	src := "package p\n\nimport (\n\t\"fmt\"\n\t\"log\"\n\t\"os\"\n\t\"testing\"\n)\n\ntype fixture struct{ t *testing.T }\n\n" + body + "\n"
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "snippet.go", src, parser.SkipObjectResolution)
	if err != nil {
		gateFatalf(t, "parse snippet %q: %v", body, err)
	}
	unsanitized, _ := gateOutputSites(fset, file)
	return unsanitized
}

// gateSkipf skips the test with the message sanitized, the same way gateReportf
// reports one. It exists because the guard checks the CALL, not the name of the
// function around it, so a third helper needs no permission to be added.
func gateSkipf(t *testing.T, format string, args ...any) {
	t.Helper()
	t.Skip(gateEvidenceQuote(fmt.Sprintf(format, args...)))
}

// TestARealMakeGateStaysRedWhenTheSuitePrintsThisPackagesOwnMessages runs the
// whole chain rather than reasoning about it: a real `make`, a real `go test`
// that FAILS and prints the strings this package's detectors key on, the real
// dispatchDotToolNode, the real classifier, and the real message the implementer
// receives.
//
// Every other test here confirms one link. This one is the only place the links
// are joined, and the round-2 review said plainly that the end-to-end claim had
// been inferred from the pieces and never executed. Measured both ways on
// 2026-08-12: with the sanitizer covering make's anchor ALONE, this gate comes
// back structural and the implementer is told "NOTHING is known to be wrong with
// your change" after a genuinely RED gate; with the coverage derived from the
// detector tables it comes back deterministic and says "Fix the failure".
//
// The bait is built FROM gateUnscopedSignatures, so a signature added tomorrow
// is planted in a real gate log by this test without anyone editing it.
func TestARealMakeGateStaysRedWhenTheSuitePrintsThisPackagesOwnMessages(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("make"); err != nil {
		gateSkipf(t, "no make on PATH, so the real gate cannot be run here: %v", err)
	}

	var bait strings.Builder
	bait.WriteString("gate evidence:")
	for _, sig := range gateUnscopedSignatures {
		bait.WriteString(" make[2]: ")
		bait.WriteString(gateRecipeFailureAnchor)
		bait.WriteString("fmt-check] ")
		bait.WriteString(sig.text)
		bait.WriteString(";")
	}
	// What this file's own gateReportf would put in the next gate's log.
	message := gateEvidenceQuote(bait.String())

	wt := t.TempDir()
	write := func(name, body string) {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(wt, name)), 0o750); err != nil {
			gateFatalf(t, "mkdir for %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(wt, name), []byte(body), 0o600); err != nil {
			gateFatalf(t, "write %s: %v", name, err)
		}
	}
	// .harmonik exists so the gate's own log write has somewhere to land.
	if err := os.MkdirAll(filepath.Join(wt, ".harmonik"), 0o750); err != nil {
		gateFatalf(t, "mkdir .harmonik: %v", err)
	}
	write("go.mod", "module gateevidence\n\ngo 1.25\n")
	write("x_test.go", "package gateevidence\n\nimport \"testing\"\n\nfunc TestTheSuiteFails(t *testing.T) {\n\tt.Errorf(\"%s\", "+strconv.Quote(message)+")\n}\n")
	write("Makefile", "full: test-unit\n\ntest-unit:\n\tgo test ./...\n")

	node := &dot.Node{
		ID: "commit_gate", Type: core.NodeTypeNonAgentic, HandlerRef: "shell",
		ToolCommand: "make full", Timeout: "300",
	}
	outcome, err := dispatchDotToolNode(t.Context(), nil, gateLogNewRunID(t), nil, t.TempDir(), wt, node, nil)
	if err != nil {
		gateFatalf(t, "dispatch: %v", err)
	}
	if outcome.Status != core.OutcomeStatusFail || outcome.FailureClass == nil {
		gateFatalf(t, "a make gate whose suite FAILED did not fail: status=%q class=%v\n%s", outcome.Status, outcome.FailureClass, outcome.Notes)
	}

	// Positive evidence that the bait really travelled: the message the suite
	// printed has to be in the gate log the classifier read, or this test is
	// asserting about a log that never carried it.
	if !strings.Contains(outcome.Notes, message) {
		gateFatalf(t, "the suite's own failure message never reached the gate log, so nothing here was exercised; got:\n%s", outcome.Notes)
	}

	if *outcome.FailureClass != core.FailureClassDeterministic {
		gateReportf(t, "a genuinely RED make gate is classified %q because the suite printed this package's own diagnostics:\n%s", *outcome.FailureClass, outcome.Notes)
	}
	back := gateBackEdgeMessage(*outcome.FailureClass, outcome.Notes)
	if strings.Contains(back, "NOTHING is known to be wrong with your change") {
		gateReportf(t, "the implementer is told NOTHING is wrong after a gate that RAN and FAILED:\n%s", back)
	}
	if !strings.Contains(back, "Fix the failure") {
		gateReportf(t, "the implementer is not told to fix the failure the gate found:\n%s", back)
	}
}

// gateSanitizerExemptTables names every []gateOutputSignature table that is
// deliberately NOT reachable from gateEvidenceQuote, with the reason. It is
// empty: today every such table is unscoped, so every one is sanitized. An entry
// here is a written decision, which is the point — the test below fails on a new
// table until somebody either wires it into the sanitizer or says here why not.
var gateSanitizerExemptTables = map[string]string{}

// TestEveryDetectorTableIsReachableFromTheSanitizer holds the convention that
// the sanitizer's coverage is derived from the detectors' tables rather than
// maintained beside them.
//
// The convention is NOT self-enforcing, and the comment in the production file
// says so. slices.Concat is an ordinary expression: a new
// []gateOutputSignature table can be handed to gateOutputHasAnySignature and
// reach the classifier without ever being named in gateUnscopedSignatures, and
// then a detector string exists that gateEvidenceQuote does not rewrite — which
// is the whole defect this unit was opened for. This test is what makes the
// convention bite: it reads the production source and requires every
// package-level []gateOutputSignature table to be an operand of the expression
// that builds gateUnscopedSignatures, or to be listed above with a reason.
//
// It reads EVERY non-test .go file in this package, not just the one the tables
// live in today. A sibling file is the most likely way this package grows — the
// file holding the classifier is already over 1800 lines — and a detector table
// declared in one reaches the classifier exactly as well as a local one does.
//
// What it does NOT cover, so nobody reads it as a proof: a detector that matches
// a bare string with no table at all, a table declared inside a function, a
// table built by appending at run time (an init() that appends to a table is
// invisible to this and was confirmed by mutation to slip through), and a table
// in another PACKAGE. Those are outside what source text at this scope can see.
// The guard is a fence around the shape the code uses today, not a theorem.
func TestEveryDetectorTableIsReachableFromTheSanitizer(t *testing.T) {
	t.Parallel()

	fset := token.NewFileSet()
	files := gateParsePackage(t, fset, false)

	sanitized := map[string]bool{}
	declared := map[string]string{}

	for name, file := range files {
		gateCollectSignatureTables(file, name, declared, sanitized)
	}

	// Positive evidence: the shapes this test looks for still exist. Without
	// this the test passes loudly on a file it failed to understand.
	if len(declared) == 0 {
		gateFatalf(t, "no package-level []gateOutputSignature table was found in this package; this test is reading source it no longer understands")
	}
	if len(sanitized) == 0 {
		gateFatalf(t, "gateUnscopedSignatures was not found in this package, so nothing here is checking what the sanitizer reaches")
	}

	for name, where := range declared {
		if sanitized[name] {
			continue
		}
		if why, exempt := gateSanitizerExemptTables[name]; exempt {
			t.Log(gateEvidenceQuote(fmt.Sprintf("%s is exempt from the sanitizer: %s", name, why)))
			continue
		}
		gateReportf(t, "the detector table %s (%s) reaches the classifier but not gateEvidenceQuote, so a string this daemon matches on is one it can also write into the next gate's log unchanged. Add it to gateUnscopedSignatures, or record in gateSanitizerExemptTables why it does not need rewriting.", name, where)
	}
}

// gatePackageDir returns the directory of this package's source.
func gatePackageDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		gateFatalf(t, "cannot find this file's own path, so the guard checked nothing")
	}
	return filepath.Dir(thisFile)
}

// gateParsePackage parses every .go file in this package's directory, keyed by
// base name. It is what lets the guards below see a SIBLING file — the reach the
// single-file versions do not have, and the reach the defect needed.
func gateParsePackage(t *testing.T, fset *token.FileSet, testFiles bool) map[string]*ast.File {
	t.Helper()
	dir := gatePackageDir(t)
	entries, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil || len(entries) == 0 {
		gateFatalf(t, "no .go file found in %s, so the guard read nothing: %v", dir, err)
	}
	files := map[string]*ast.File{}
	for _, path := range entries {
		if strings.HasSuffix(path, "_test.go") != testFiles {
			continue
		}
		parsed, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			gateFatalf(t, "parse %s: %v", path, err)
		}
		files[filepath.Base(path)] = parsed
	}
	if len(files) == 0 {
		gateFatalf(t, "no matching .go file found in %s, so the guard read nothing", dir)
	}
	return files
}

// gateSignatureInText reports whether text carries an UNSCOPED signature — one
// whose detector reads the whole gate log, so no line position protects it.
func gateSignatureInText(text string) bool {
	for _, sig := range gateUnscopedSignatures {
		if strings.Contains(text, sig.text) {
			return true
		}
	}
	return false
}

// gateExprCarriesSignature reports whether an expression subtree contains a
// string literal with an unscoped signature in it, or names a variable that
// holds one at that point in the source.
func gateExprCarriesSignature(expr ast.Expr, spans []gateTaintSpan) bool {
	found := false
	ast.Inspect(expr, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.BasicLit:
			if node.Kind != token.STRING {
				return true
			}
			text := node.Value
			if unquoted, err := strconv.Unquote(text); err == nil {
				text = unquoted
			}
			if gateSignatureInText(text) {
				found = true
			}
		case *ast.Ident:
			if gateTaintedAt(spans, node.Name, node.Pos()) {
				found = true
			}
		}
		return !found
	})
	return found
}

// gateTaintSpan is one variable that holds an unscoped signature, and the span
// of source over which it does. A span, not a file-wide name: `out` is the
// obvious name for a loop variable, both of the files this guard has to read use
// it twice in ONE function, and only one of the two loops carries a signature.
// Keying on the name alone reported the safe loop as well and turned 2 real
// findings into 12.
type gateTaintSpan struct {
	name     string
	from, to token.Pos
}

// gateTaintSpans finds the variables in file that hold an unscoped signature,
// each scoped to the source span where it holds one.
//
// It is a deliberately SHALLOW pass, and naming its limits is the point. It
// follows a range over a signature-bearing expression (scoped to that loop's
// body) and a var / const / := whose right-hand side is signature-bearing
// (scoped to the rest of the enclosing function). It does NOT follow a value
// through a function call, a struct field, a map, a channel, or another file.
//
// Not following CALL RESULTS is deliberate rather than a gap left open. A node
// built with a signature in its ToolCommand is passed to a dispatch helper, and
// treating everything that call returns as tainted reported eight `t.Fatalf("
// dispatch: %v", err)` sites whose text cannot carry a signature. Sanitizing
// those would have made real diagnostics worse and taught the next author that
// the guard cries wolf.
//
// It exists at all because both real sites interpolate a loop variable rather
// than a literal — `for _, out := range cannotRun { t.Errorf("...%s", out) }` —
// so a literals-only scan reports neither.
func gateTaintSpans(file *ast.File) []gateTaintSpan {
	var spans []gateTaintSpan
	add := func(target ast.Expr, from, to token.Pos) {
		if id, isIdent := target.(*ast.Ident); isIdent && id.Name != "_" {
			spans = append(spans, gateTaintSpan{name: id.Name, from: from, to: to})
		}
	}
	// A stack of enclosing function ends, so an assignment taints only the rest
	// of its own function.
	var funcEnds []token.Pos
	var walk func(n ast.Node) bool
	walk = func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.FuncDecl:
			if node.Body != nil {
				funcEnds = append(funcEnds, node.Body.End())
				ast.Inspect(node.Body, walk)
				funcEnds = funcEnds[:len(funcEnds)-1]
			}
			return false
		case *ast.FuncLit:
			funcEnds = append(funcEnds, node.Body.End())
			ast.Inspect(node.Body, walk)
			funcEnds = funcEnds[:len(funcEnds)-1]
			return false
		case *ast.RangeStmt:
			if gateExprCarriesSignature(node.X, spans) {
				add(node.Key, node.Body.Pos(), node.Body.End())
				add(node.Value, node.Body.Pos(), node.Body.End())
			}
		case *ast.AssignStmt:
			if len(funcEnds) == 0 {
				return true
			}
			end := funcEnds[len(funcEnds)-1]
			for i, rhs := range node.Rhs {
				if _, isCall := rhs.(*ast.CallExpr); isCall {
					continue
				}
				if !gateExprCarriesSignature(rhs, spans) {
					continue
				}
				if len(node.Rhs) == len(node.Lhs) {
					add(node.Lhs[i], node.End(), end)
					continue
				}
				for _, lhs := range node.Lhs {
					add(lhs, node.End(), end)
				}
			}
		case *ast.ValueSpec:
			// A package-level var has no enclosing function, so it holds a
			// signature for the rest of the file.
			end := file.End()
			if len(funcEnds) > 0 {
				end = funcEnds[len(funcEnds)-1]
			}
			for i, name := range node.Names {
				if i < len(node.Values) {
					if _, isCall := node.Values[i].(*ast.CallExpr); isCall {
						continue
					}
					if gateExprCarriesSignature(node.Values[i], spans) {
						add(name, node.End(), end)
					}
				}
			}
		}
		return true
	}
	// Two passes: a package-level signature-bearing var can be declared after
	// the function that reads it.
	for range 2 {
		spans = spans[:0:0]
		ast.Inspect(file, walk)
	}
	return spans
}

// gateTaintedAt reports whether name holds an unscoped signature at pos.
func gateTaintedAt(spans []gateTaintSpan, name string, pos token.Pos) bool {
	for _, span := range spans {
		if span.name == name && pos >= span.from && pos <= span.to {
			return true
		}
	}
	return false
}

// gateSignatureWriteSites reports every write in file whose message carries an
// unscoped signature without going through gateEvidenceQuote.
func gateSignatureWriteSites(fset *token.FileSet, name string, file *ast.File) []string {
	handles := gateTestingHandleNames(file)
	spans := gateTaintSpans(file)
	var sites []string
	ast.Inspect(file, func(n ast.Node) bool {
		call, isCall := n.(*ast.CallExpr)
		if !isCall {
			return true
		}
		channel, msgArgs, isWrite := gateOutputChannel(call, handles)
		if !isWrite || gateArgsAreSanitized(msgArgs) {
			return true
		}
		for _, arg := range msgArgs {
			if gateExprCarriesSignature(arg, spans) {
				sites = append(sites, fmt.Sprintf("%s:%d: %s", name, fset.Position(call.Pos()).Line, channel))
				break
			}
		}
		return true
	})
	return sites
}

// TestNoSignatureInThisPackageReachesTheLogUnsanitized closes the hole the
// single-file guard leaves open.
//
// The defect this whole unit exists to remove is a string in a TEST's output
// being read as the gate's own report by the NEXT gate's classifier. The
// single-file guard above keeps this file clean, and that is not enough: `make
// full` runs this package twice and cats the scenario log whole, so a failing
// test in ANY file of this package puts its output in front of the classifier.
// A reviewer reproduced it end to end — the exact text `go test` writes when the
// t.Errorf in dot_cascade_gatecannotrun_hk2f3v4_test.go fails, planted ahead of
// a real red cascade, classifies STRUCTURAL, and structural tells an implementer
// whose gate is genuinely red that NOTHING is known to be wrong with the change.
//
// SCOPE, and it is narrower than the single-file guard on purpose. This tier
// reports only writes carrying an UNSCOPED signature — the ones whose detectors
// read the whole log, so that no line position protects them. It does NOT report
// a bare make anchor: an anchor written by a test always lands indented and
// gateLineIsIndented already keeps an indented line out of the cascade. Widening
// this to every unsanitized write in the package would flag several thousand
// ordinary t.Fatalf calls that carry no signature at all, and a guard nobody can
// keep green is a guard that gets deleted.
func TestNoSignatureInThisPackageReachesTheLogUnsanitized(t *testing.T) {
	t.Parallel()

	fset := token.NewFileSet()
	files := gateParsePackage(t, fset, true)

	var offenders []string
	for name, file := range files {
		offenders = append(offenders, gateSignatureWriteSites(fset, name, file)...)
	}
	sort.Strings(offenders)

	if len(offenders) > 0 {
		gateReportf(t, "these write a classifier signature into the test log without passing it through %s, so a FAILING test here can make the next genuinely-red gate read as structural: %s",
			gateSanitizerName, strings.Join(offenders, ", "))
	}

	// Positive evidence: the scan reached sibling files and understood them. If
	// the taint pass silently stops working, every file looks clean.
	if len(files) < 2 {
		gateFatalf(t, "the scan parsed %d test file(s) in this package; it is not reading the directory it thinks it is", len(files))
	}
	// Positive evidence that the taint pass still does the one thing the real
	// findings need: follow a range over a signature-bearing slice, and scope it
	// to that loop. Without this a pass that silently stopped resolving names
	// would report a clean package.
	probe := gateTaintSpans(files["dot_cascade_gatecannotrun_hk2f3v4_test.go"])
	var outSpans int
	for _, span := range probe {
		if span.name == "out" {
			outSpans++
		}
	}
	if outSpans != 1 {
		gateFatalf(t, "the taint pass found %d tainted span(s) for the loop variable `out` in dot_cascade_gatecannotrun_hk2f3v4_test.go; that file has exactly one signature-bearing loop, so this test is reading a file it no longer understands", outSpans)
	}
}

// gateCollectSignatureTables records every package-level []gateOutputSignature
// table declared in file, and every identifier the gateUnscopedSignatures
// expression is built from.
func gateCollectSignatureTables(file *ast.File, name string, declared map[string]string, sanitized map[string]bool) {
	for _, decl := range file.Decls {
		gd, isGen := decl.(*ast.GenDecl)
		if !isGen || gd.Tok != token.VAR {
			continue
		}
		for _, spec := range gd.Specs {
			vs, isValue := spec.(*ast.ValueSpec)
			if !isValue || len(vs.Names) != 1 || len(vs.Values) != 1 {
				continue
			}
			varName := vs.Names[0].Name

			// var X = []gateOutputSignature{…} — a detector table.
			if lit, isLit := vs.Values[0].(*ast.CompositeLit); isLit {
				if at, isArray := lit.Type.(*ast.ArrayType); isArray {
					if id, isIdent := at.Elt.(*ast.Ident); isIdent && id.Name == "gateOutputSignature" {
						declared[varName] = name
					}
				}
			}

			// var gateUnscopedSignatures = <expr over table names> — every
			// identifier in it is a table the sanitizer reaches.
			if varName == "gateUnscopedSignatures" {
				ast.Inspect(vs.Values[0], func(n ast.Node) bool {
					if id, isIdent := n.(*ast.Ident); isIdent {
						sanitized[id.Name] = true
					}
					return true
				})
			}
		}
	}
}

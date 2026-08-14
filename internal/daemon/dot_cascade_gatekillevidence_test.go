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
//   - The PRODUCTION paths that carry gate output verbatim are the larger half,
//     and they are not a test's own message. gateBackEdgeMessage quotes the
//     gate's last output back to the implementer and gateFailureTail folds it
//     onto one line for the stranded-commit reason; both now run it through
//     gateEvidenceQuote first. The fold is the worse of the two — it puts make's
//     anchor in the middle of one unindented line, where no position rule can
//     see it. Routing a test's own message through a sanitizer does not touch
//     either one, and neither does the package guard, which reads writes and not
//     data flow out of a production helper. What covers them is
//     TestAMessageQuotingGateOutputCannotRelabelTheNextGate, by behaviour: render
//     each message over gate output carrying every trigger, plant it in a red
//     gate's log both far from and hard against make's cascade, and require the
//     class to stay deterministic. Six cases: three renders — the two
//     gateBackEdgeMessage wordings and the stranded-commit note — at two plant
//     positions each. Re-measured in a scratch copy with each rewrite removed on
//     its own: without gateBackEdgeMessage's call 4 of the 6 fail (both back-edge
//     wordings, at both positions); without gateFailureTail's, 2 of 6 (the
//     stranded-commit note, at both positions); with both removed, 6 of 6. The
//     far-from-the-cascade plant is not dead weight — the unscoped-signature
//     detectors read the WHOLE log with no position scoping, so a message
//     thousands of lines from make's cascade reaches them.
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
//   - fmt.Print* / fmt.Fprint*, log.*, slog's level functions (Debug / Info /
//     Warn / Error, their Context spellings, Log and LogAttrs), a method on
//     os.Stdout / os.Stderr, or the print / println builtins.
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
// A dotted path is NECESSARY and not sufficient, and the list above is the whole
// set of receivers. `slog.ErrorContext(ctx, …)` reads like a dotted path that
// must therefore be covered — it is, now, and it was not until this was written.
// The one still outside is testify: 197 `require.*` / `assert.*` calls in this
// package. They are test-only and their text reaches the log through the testing
// handle, so it arrives INDENTED — which is cover for make's anchor and is NOT
// cover for an unscoped signature, whose detector reads the whole log. So a
// testify call that interpolates a detector string is invisible here. That is a
// stated limit, not a covered case, and the reason it is tolerable is that the
// package writes no such call today, not that one would be harmless.
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
	case recv == "slog":
		// slog reaches stderr through the default handler, at column 0 — the
		// position that matters here — and this package makes 58 such calls in
		// its production files (46 WarnContext, 12 InfoContext). An earlier
		// count of 59 came from a text grep, which also caught a slog.Default()
		// mention inside a // comment in daemon.go; this guard walks the AST and
		// never sees a comment. The ctx-taking spellings put the message after
		// the arguments the guard has no interest in, so skip those and keep the
		// rest: over-including an argument only ever adds a checked expression.
		switch method {
		case "Debug", "Info", "Warn", "Error":
			return path, call.Args, true
		case "DebugContext", "InfoContext", "WarnContext", "ErrorContext":
			return path, gateArgsAfter(call.Args, 1), true
		case "Log", "LogAttrs":
			// Log(ctx, level, msg, …) and LogAttrs(ctx, level, msg, …).
			return path, gateArgsAfter(call.Args, 2), true
		}
	}
	return "", nil, false
}

// gateArgsAfter drops the first n arguments of a call, and returns nothing
// rather than panicking on a call that has fewer.
func gateArgsAfter(args []ast.Expr, n int) []ast.Expr {
	if len(args) <= n {
		return nil
	}
	return args[n:]
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
		// slog is a PRODUCTION channel: no testing handle, straight to stderr.
		"slog level function":   `func O(out string) { slog.Error("x", "out", out) }`,
		"slog with a context":   `func P(ctx context.Context, out string) { slog.ErrorContext(ctx, "x", "out", out) }`,
		"slog.Log past its ctx": `func Q(ctx context.Context, out string) { slog.Log(ctx, slog.LevelError, "x", "out", out) }`,
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
		// Building a logger is not writing through one.
		"slog.New is not a write": `func R() { _ = slog.New(nil) }`,
		// THE ARGUMENT-SKIP ARITHMETIC. gateOutputChannel drops the leading ctx
		// on the Context spellings, and nothing else measures that number: the
		// positive cases above only ask whether the call is seen as a write,
		// which stays true for any skip. This case fails if the skip is wrong,
		// because a wrong skip leaves no message argument to recognise as
		// sanitized and the write is reported.
		"sanitized slog write": `func S(ctx context.Context, x string) { slog.ErrorContext(ctx, gateEvidenceQuote(x)) }`,
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

// gateOutputCarryingEveryTrigger builds gate output that carries every string
// the classifier reads: make's recipe anchor, every unscoped signature, and a
// recipe line that names a signal. It is built FROM the detector tables, so a
// signature added tomorrow is carried by this fixture without anyone editing it.
//
// The lines read oddly — a signature is pasted onto a recipe line that would not
// really carry it — because the fixture's only job is to hold every trigger at
// once. TestARealMakeGateStaysRedWhenTheSuitePrintsThisPackagesOwnMessages
// builds its bait the same way and for the same reason.
func gateOutputCarryingEveryTrigger() string {
	var b strings.Builder
	b.WriteString("--- FAIL: TestSomething (0.02s)\n    x_test.go:41: want 3, got 4\n")
	for _, sig := range gateUnscopedSignatures {
		b.WriteString("make[2]: " + gateRecipeFailureAnchor + "fmt-check] " + sig.text + "\n")
	}
	b.WriteString("make[1]: " + gateRecipeFailureAnchor + "test-scenario] Terminated: 15\n")
	b.WriteString("make: " + gateRecipeFailureAnchor + "full] Error 2\n")
	return b.String()
}

// gateLogWithMessageAgainstTheCascade puts message at column 0 immediately above
// make's terminal cascade — the position a daemon diagnostic on stderr lands in,
// and the one gateLineIsIndented cannot rule out.
func gateLogWithMessageAgainstTheCascade(message, cascade string) []byte {
	return []byte("--- PASS: TestUnrelated (0.01s)\n" +
		strings.TrimRight(message, "\n") + "\n" + cascade)
}

// TestAMessageQuotingGateOutputCannotRelabelTheNextGate closes the writing end
// on the PRODUCTION side.
//
// The reading-end repair scopes the kill detector to make's terminal cascade and
// throws out indented lines, and gateReportf keeps this package's own test
// messages clean. Neither one reaches a production helper that takes REAL gate
// output and pastes it into a message. THREE do: gateBackEdgeMessage quotes the
// gate's last output back to the implementer, gateFailureTail folds it onto one
// line for the stranded-commit reason, and gateKilledBySignal quotes make's own
// cascade line into the description dispatchDotToolNode writes to stderr. Those
// messages travel — into the reviewer-feedback file, into the next implementer's
// prompt, into the daemon's own stderr, and into the diagnostics a test prints
// when it checks either path — and any of those can land in the NEXT gate's log
// at column 0. (hk-e0yhw, hk-dqy37)
//
// So: render each one over gate output that carries every trigger, plant the
// result in the log of a gate that RAN and FAILED, and require the class to stay
// deterministic. Two placements, because two different rules protect the two
// halves and only one of them is a position rule.
//
// RENDER IT THE WAY ITS CALLER DOES. strandedCommitNote appears twice, bare and
// glued onto the column-0 sentence dot_cascade_core.go builds, and the two do
// not measure the same thing: the bare note begins with a space, which reads as
// indentation and hides the recipe-anchor rewrite's work. A cold reviewer found
// that the comment on strandedCommitNote drew a safety conclusion from the bare
// shape and was wrong about production. Measured over the ten sites here, with
// the anchor rewrite disabled four go red and all four are "against the
// cascade" — both back-edge messages, the note AS ITS CALLER WRITES IT, and the
// kill description. With the unscoped-signature rewrites disabled eight go red:
// everything except the kill description, whose content is one cascade line and
// carries no unscoped signature. Neither rewrite alone covers the other.
//
// The two mutations do not fail the same way, and the difference is the point.
// With the anchor rewrite off the class lands on canceled — the planted anchor
// plus a signal word reads as a kill, so a REAL failure is reported as one.
// With the signature rewrites off it lands on transient. Both are wrong and
// only one of them is the false all-clear, so a report that says only "red" is
// hiding which direction the classifier moved.
func TestAMessageQuotingGateOutputCannotRelabelTheNextGate(t *testing.T) {
	t.Parallel()

	notes := gateOutputCarryingEveryTrigger()

	plant := map[string]func(string) []byte{
		// Far above the cascade, where the daemon's 2026-08-12 diagnostic sat.
		// Only an UNSCOPED signature reaches the classifier from here.
		"far from the cascade": func(msg string) []byte {
			return gateLogWithTranscript(msg, redGateCascade)
		},
		// Hard against the cascade, where a recipe anchor joins it and a signal
		// word on the same line reads as a kill.
		"against the cascade": func(msg string) []byte {
			return gateLogWithMessageAgainstTheCascade(msg, redGateCascade)
		},
	}

	// NEGATIVE CONTROL FIRST. The raw output must flip the class in BOTH
	// placements, or the checks below prove nothing about either rule.
	for where, build := range plant {
		if class, _ := classifyGateLog(t, build(notes)); class == core.FailureClassDeterministic {
			gateFatalf(t, "%s: the raw gate output leaves a red gate deterministic, so this test cannot fail", where)
		}
	}

	for name, render := range map[string]func(string) string{
		"gateBackEdgeMessage/deterministic": func(n string) string {
			return gateBackEdgeMessage(core.FailureClassDeterministic, n)
		},
		"gateBackEdgeMessage/canceled": func(n string) string {
			return gateBackEdgeMessage(core.FailureClassCanceled, n)
		},
		"strandedCommitNote": func(n string) string {
			return strandedCommitNote(gateLogNewRunID(t), true, false,
				"commit_gate", "commit_gate", strandedHeadSHA, n)
		},
		// AS ITS CALLER WRITES IT. The bare note begins with a space, which
		// reads as indentation and hides the anchor rewrite's work here. Its
		// only production caller (dot_cascade_core.go) appends it to a sentence
		// that starts at column 0, and that is the shape a gate log receives.
		"strandedCommitNote/as its caller writes it": func(n string) string {
			return "dot: no-progress detected at iteration 2: HEAD did not advance" +
				strandedCommitNote(gateLogNewRunID(t), true, false,
					"commit_gate", "commit_gate", strandedHeadSHA, n)
		},
		// The THIRD carrier of raw gate output into a written message: the kill
		// description becomes the logDesc dispatchDotToolNode writes to stderr
		// at column 0.
		"gateKilledBySignal": func(n string) string {
			desc, _ := gateKilledBySignal(nil, []byte(n))
			return desc
		},
	} {
		msg := render(notes)
		if msg == "" {
			gateFatalf(t, "%s rendered nothing, so this case checked no message at all", name)
		}

		for where, build := range plant {
			if class, _ := classifyGateLog(t, build(msg)); class != core.FailureClassDeterministic {
				gateReportf(t, "%s planted %s: a red gate is classified %q, so the daemon's own message about one gate is read as evidence about the next one:\n%s",
					name, where, class, msg)
			}
		}

		// The message still has to SAY what died and how. Removing make's anchor
		// is what disarms the cascade-scoped detectors; the signal name is the
		// value of the diagnostic and must survive.
		if !strings.Contains(msg, "test-scenario") || !strings.Contains(msg, "Terminated: 15") {
			gateReportf(t, "%s no longer names the recipe that died or the signal that ended it:\n%s", name, msg)
		}
	}
}

// TestTheStrandedCommitExcerptCannotBuildOrRegrowASignature pins the ORDER of
// the three steps gateFailureTail can permute: fold, sanitize, bound. Each step
// is safe on its own, and only ONE of the six orders is. The other five each
// either miss the join the fold creates or let the rewrite regrow past the cut.
//
//   - Fold first. Folding a newline to a space JOINS two lines, and a line that
//     ends in `:` above a line that reads `command not found` carries a
//     signature together that neither carries apart. Sanitizing before the fold
//     therefore rewrites nothing and the join goes out unrewritten. That rules
//     out the three orders with sanitize ahead of fold.
//   - Bound last. gateEvidenceQuote makes text LONGER, so a rewrite after the
//     cut puts the excerpt back over gateFailureTailMaxBytes — and this excerpt
//     travels into run_failed, which an operator reads one line at a time. That
//     rules out the three orders with sanitize after bound.
//
// The two rules overlap on exactly one order (bound, sanitize, fold breaks
// both), so five of the six are out and fold-sanitize-bound is what survives.
// The JOIN onto gateFailureTailPrefix is a fourth step rather than a fourth
// permutation — it can only happen last — and
// TestTheGateOutputClauseCannotBuildASignatureAtTheSeam covers it.
func TestTheStrandedCommitExcerptCannotBuildOrRegrowASignature(t *testing.T) {
	t.Parallel()

	// THE JOIN. Neither line carries a signature; the fold makes one.
	const split = "/bin/sh: gofumpt:\ncommand not found\n"
	if gateOutputHasAnySignature(split, gateUnscopedSignatures) {
		gateFatalf(t, "the fixture already carries a signature before it is folded, so the join is not what this measures")
	}
	if !gateOutputHasAnySignature(strings.ReplaceAll(split, "\n", " "), gateUnscopedSignatures) {
		gateFatalf(t, "folding the fixture builds no signature, so this check cannot fail")
	}
	if joined := gateFailureTail(split); gateOutputHasAnySignature(joined, gateUnscopedSignatures) {
		gateReportf(t, "the excerpt built a detector signature out of two harmless lines, so a red gate whose log replays it reads as structural:%s", joined)
	}

	// THE REGROWTH. Long output, every line a signature, so every line grows
	// under the rewrite. The bound has to hold on what actually goes out.
	var long strings.Builder
	for range 40 {
		for _, sig := range gateUnscopedSignatures {
			long.WriteString("make[2]: " + gateRecipeFailureAnchor + "fmt-check] " + sig.text + "\n")
		}
	}
	got := gateFailureTail(long.String())
	if gateOutputHasAnySignature(got, gateUnscopedSignatures) {
		gateReportf(t, "the excerpt still carries a detector signature:%s", got)
	}
	if bound := gateFailureTailMaxBytes + len(gateFailureTailPrefix) + len("…"); len(got) > bound {
		gateReportf(t, "the excerpt is %d bytes against a bound of %d; the rewrite grew it back past the cut", len(got), bound)
	}
}

// TestTheGateOutputClauseCannotBuildASignatureAtTheSeam pins the fourth step:
// the JOIN between text this daemon writes and the gate output pasted onto it.
//
// It sweeps EVERY writer that makes that join, which is the correction a cold
// reviewer earned twice: first gateBackEdgeMessage, then gateKilledBySignal,
// whose gateKillOutputPrefix ends in `: ` exactly as gateFailureTailPrefix does
// and which was rebuilding `: command not found` onto a line the extractor had
// just sanitized. That one was a LIVE defect, not a latent one, because its
// description is written to stderr at column 0 where no indentation rule can
// rule it out. gateBackEdgeMessage pastes its constants onto `quoted`
// exactly as gateFailureTail pastes its prefix onto the excerpt; only the second
// one was guarded, and the reviewer re-armed the identical defect in the first —
// end the deterministic branch's constant in `: ` instead of a newline pair —
// with the whole gate suite still green. The back-edge messages are safe today
// because both constants end in `\n\n` and no signature starts with a newline.
// That is safety by an accident of wording, which is the exact thing this commit
// exists to stop shipping.
//
// The invariant, and it is stronger than any fixture: NO string gateFailureTail
// returns carries a detector signature that its sanitized excerpt does not
// already carry. gateEvidenceQuote cannot enforce that on its own. It runs on
// the excerpt BEFORE the paste, when the text is clean, and a signature can be
// split across the seam — the constant supplies the front of it and the gate's
// own output supplies the rest. Measured: gateFailureTailPrefix ends in `: `,
// the first two bytes of the `: command not found` signature, so an excerpt
// beginning `command not found` rebuilt the whole thing at the join, after the
// sanitizer had removed it. (hk-e0yhw)
//
// The probes are DERIVED from gateUnscopedSignatures, split at every byte, so a
// signature added to a detector table tomorrow is probed at this seam with no
// edit here. Each check reads the RETURNED string rather than the prefix, so it
// sees whatever the finished clause carries however the clause was built.
//
// It probes ONE seam per writer — the LEADING one. Every probe puts the REST of
// a signature at the head of the gate output, so it measures the constant in
// front of that output and nothing behind it.
//
// THAT IS NOT THE WHOLE PICTURE FOR THE KILL PATH, and the limit is stated here
// rather than implied. gateKilledBySignal returns a description, and
// classifyDotToolNodeFailure then embeds it MID-SENTENCE: `… was KILLED
// mid-flight (%s) — it reached no verdict …`. So a constant does follow the gate
// output at the next level of composition, which is a trailing seam, and this
// sweep does not probe it: the mirrored probe it would need is a signature's
// HEAD at the TAIL of the output, and no probe here is built that way.
//
// It holds today because the byte that follows is `)`, and no entry in
// gateUnscopedSignatures begins with `)`. That is safety by an accident of
// wording — the same thing this test condemns in the gateBackEdgeMessage
// paragraph above — so it is a KNOWN GAP and not a proof. Filed as hk-ranr2.
// Closing it means rendering the kill entry the way classifyDotToolNodeFailure
// writes it and adding the mirrored probes, the same correction "render it the
// way its caller does" made for strandedCommitNote.
//
// It asserts no LENGTH bound. It used to, and the check was dead: the probes run
// about 55 bytes and the longest string this sweep can produce is 72, against a
// bound of 218, so nothing here could ever reach it. A check that cannot fire
// reads as coverage and is not, which is the failure this file keeps finding in
// itself. The bound is measured where it can actually be reached, by
// TestTheCutAndTheSeamMarkerCannotBothLandOnOneExcerpt.
func TestTheGateOutputClauseCannotBuildASignatureAtTheSeam(t *testing.T) {
	t.Parallel()

	// EVERY writer that pastes a daemon-authored constant onto sanitized gate
	// output, not just the one that was caught doing it wrong.
	writers := map[string]func(string) string{
		"gateFailureTail": gateFailureTail,
		// The kill path pastes gateKillOutputPrefix onto a line the extractor
		// already sanitized, so it is a seam on the same terms. The probe has
		// to arrive as a cascade line the kill detector will actually return,
		// or this entry renders nothing and measures nothing.
		"gateKilledBySignal": func(n string) string {
			desc, ok := gateKilledBySignal(nil, []byte(
				n+" make[1]: "+gateRecipeFailureAnchor+"test-scenario] Terminated: 15\n"))
			if !ok {
				return ""
			}
			return desc
		},
		"gateBackEdgeMessage/deterministic": func(n string) string {
			return gateBackEdgeMessage(core.FailureClassDeterministic, n)
		},
		"gateBackEdgeMessage/canceled": func(n string) string {
			return gateBackEdgeMessage(core.FailureClassCanceled, n)
		},
	}

	// NEGATIVE CONTROL. The hazard has to be real TODAY, or every check below
	// runs over a seam that could not have built anything.
	if !gateOutputHasAnySignature(gateFailureTailPrefix+"command not found", gateUnscopedSignatures) {
		gateFatalf(t, "pasting %q straight onto an excerpt that begins `command not found` no longer builds a signature. If the prefix was made safe on its own, this control is what says so — replace it deliberately rather than leaving a check that cannot fail.", gateFailureTailPrefix)
	}

	// THE FIXTURE the defect was found on. Short enough to escape the cut, so
	// it reaches the join with no `…` already in front of it.
	if got := gateFailureTail("command not found"); gateOutputHasAnySignature(got, gateUnscopedSignatures) {
		gateReportf(t, "the clause built a detector signature at the join with its own prefix, so a red gate whose log replays this reason reads as structural:%s", got)
	}

	// DERIVED. Every signature, split at every byte: the message starts with the
	// REST of one, and the front of it is whatever the constant happens to end
	// with.
	for _, sig := range gateUnscopedSignatures {
		for i := 1; i < len(sig.text); i++ {
			notes := sig.text[i:] + " (while running the fmt-check recipe)"

			// The probe must reach the join CLEAN, or it measures the sanitizer
			// instead of the seam. Trimming and folding only ever REMOVE bytes
			// or swap one for one, so a probe clean here is clean in whatever
			// each writer does to it.
			if gateOutputHasAnySignature(gateEvidenceQuote(notes), gateUnscopedSignatures) {
				gateFatalf(t, "the probe still carries a whole signature after sanitizing, so splitting %q after %d byte(s) measures nothing about the join", sig.text, i)
			}

			for writer, render := range writers {
				got := render(notes)
				// A writer that renders NOTHING passes the check below without
				// measuring anything. One empty render is legitimate and only
				// one: the kill path quotes a line from make's cascade, and a
				// probe that begins with a SPACE cannot start a column-0 line,
				// so gateTerminalRecipeFailures drops it as indented and there
				// is no seam to reach. Any other empty render is a broken probe
				// reading as a clean result.
				//
				// The exception names the WRITER as well as the probe. Keyed on
				// the probe alone it would also excuse an empty render from
				// gateFailureTail or either gateBackEdgeMessage branch on those
				// same probes — writers that build a sentence and cannot
				// legitimately render nothing — which is wider than the claim
				// above and would go unnoticed while the suite stayed green.
				legitimatelyEmpty := writer == "gateKilledBySignal" && strings.HasPrefix(notes, " ")
				if got == "" && !legitimatelyEmpty {
					gateFatalf(t, "%s rendered nothing for the probe built from %q at %d byte(s), so this writer measures no seam at all", writer, sig.text, i)
				}
				if gateOutputHasAnySignature(got, gateUnscopedSignatures) {
					gateReportf(t, "%s: splitting %q after %d byte(s) and handing the rest to the gate output builds the signature back at the join:%s", writer, sig.text, i, got)
				}
			}
		}
	}
}

// TestTheCutAndTheSeamMarkerCannotBothLandOnOneExcerpt measures the one claim
// on gateFailureTail that was still only PROSE: that the cut and the seam marker
// exclude each other, so the clause cannot carry two markers and overrun the
// bound an operator's one-line reason is held to.
//
// The claim reads: a cut excerpt already begins with `…`, `…` begins no
// signature, so the seam guard finds nothing to break and adds nothing. Every
// other probe in this file is short and escapes the cut, so nothing exercised
// the two steps AT ONCE — which is exactly the shape the argument is about. The
// comment this replaces on the third step of gateFailureTail was a safety
// argument of the same kind, stated with the same confidence, and it was wrong.
//
// So: force the cut, and arrange the surviving tail to BEGIN with the phrase
// that bites at the seam. Then assert what actually matters — no signature in
// what goes out, and a length inside gateFailureTailMaxBytes + the prefix + one
// marker. That is the same bound TestGateFailureTail_KeepsTheEnd enforces from
// dot_stranded_commit_note_test.go — but only while the two spell the same three
// terms. This one DERIVES them from gateFailureTailPrefix and self-adjusts; that
// one spells the prefix as a literal and does not, which is deliberate and is
// what catches a change to the prefix itself. The comment on that literal says
// so; do not tidy either of them into agreement.
//
// The exclusivity HOLDS, and the margin is zero BY CONSTRUCTION rather than by
// luck. The bound and the emitted length are the same three terms, so `bound` is
// not a budget with slack left in it — it is the largest string this function
// can build. The assertion therefore reads "at most what is possible", and it
// fires the instant a FOURTH component appears: a second marker, a longer
// prefix, anything after the excerpt. Measured 218 against 218, and confirmed by
// mutation — make the marker unconditional so both steps land on one excerpt and
// this test and TestGateFailureTail_KeepsTheEnd both fail at 221. The zero is
// the point of the check and not a tightness to be relieved.
func TestTheCutAndTheSeamMarkerCannotBothLandOnOneExcerpt(t *testing.T) {
	t.Parallel()

	bound := gateFailureTailMaxBytes + len(gateFailureTailPrefix) + len("…")

	// The filler only has to be long enough to force the cut and to carry no
	// trigger of its own. It folds to spaces, so its bytes survive one for one
	// and the cut lands where the arithmetic below puts it.
	filler := strings.Repeat("go downloading something irrelevant\n", 8)

	var straddling int
	for _, sig := range gateUnscopedSignatures {
		for i := 1; i < len(sig.text); i++ {
			head := sig.text[i:]
			if len(head) > gateFailureTailMaxBytes {
				continue
			}
			// The surviving tail is exactly gateFailureTailMaxBytes bytes and
			// starts with the REST of a signature, so the cut hands the seam the
			// one excerpt that would bite it.
			tail := head + strings.Repeat("x", gateFailureTailMaxBytes-len(head))
			notes := filler + tail

			if gateJoinBuildsASignature(gateFailureTailPrefix, head) {
				straddling++
			}

			// PREMISE. Nothing in the probe is rewritten, so the sanitizer does
			// not move the bytes and the cut really does land on head. Without
			// this the arithmetic above is an assumption.
			folded := strings.ReplaceAll(strings.TrimSpace(notes), "\n", " ")
			if gateEvidenceQuote(folded) != folded {
				gateFatalf(t, "the probe built from %q split after %d byte(s) is rewritten by the sanitizer, so the cut no longer lands where this test places it", sig.text, i)
			}

			got := gateFailureTail(notes)

			// The probe is worthless if it did not truncate.
			if !strings.Contains(got, "…") {
				gateFatalf(t, "the probe is %d bytes and did not truncate, so it never exercised the cut and the seam together", len(notes))
			}
			if !strings.HasSuffix(got, tail) {
				gateFatalf(t, "the cut did not keep the tail this probe built, so it is not measuring the excerpt it thinks it is:%s", got)
			}
			if gateOutputHasAnySignature(got, gateUnscopedSignatures) {
				gateReportf(t, "a CUT excerpt whose tail begins with the rest of %q carries a signature out:%s", sig.text, got)
			}
			if len(got) > bound {
				gateReportf(t, "a cut excerpt that also met the seam is %d bytes against a bound of %d, so the cut and the marker BOTH landed and the reason an operator reads overran the bound dot_stranded_commit_note_test.go enforces:%s", len(got), bound, got)
			}
		}
	}

	// NEGATIVE CONTROL. At least one probe has to be one the seam would really
	// have bitten, or the sweep above never put the two steps in contact.
	if straddling == 0 {
		gateFatalf(t, "no probe here begins with a phrase that %q could build a signature with, so nothing in this test exercises the cut and the seam at once", gateFailureTailPrefix)
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

// gateAnchorInText reports whether text carries make's recipe anchor — the
// prefix that lets a line be read as a member of make's terminal cascade.
//
// It is a SEPARATE class from an unscoped signature, and the difference is
// position. An anchor is read only inside the cascade window and only on a line
// that is not indented, so a write through a testing handle cannot build one:
// go test indents it and gateLineIsIndented throws it out. A write that lands
// at column 0 has no such cover, so this class is asked only about those
// channels.
func gateAnchorInText(text string) bool {
	return strings.Contains(text, gateRecipeFailureAnchor)
}

// gateChannelLandsAtColumnZero reports whether text written through this
// channel reaches the gate log with nothing in front of it.
//
// go test indents everything a testing handle prints. Every other channel
// gateOutputChannel recognises — the standard streams, log, slog, the print
// builtins — writes straight out at column 0, which is the position make's own
// report occupies and the one no indentation rule can rule out.
func gateChannelLandsAtColumnZero(channel string, handles map[string]bool) bool {
	i := strings.LastIndex(channel, ".")
	if i < 0 {
		// The print / println builtins take no qualifier and go to stderr.
		return true
	}
	return !gateReceiverIsTestingHandle(channel[:i], handles)
}

// gateExprCarriesText reports whether an expression subtree contains a string
// literal that match recognises, or names a variable that holds one at that
// point in the source.
func gateExprCarriesText(expr ast.Expr, match func(string) bool, spans []gateTaintSpan) bool {
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
			if match(text) {
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

// gateTaintSpan is one variable that holds text a matcher recognises, and the
// span of source over which it does. A span, not a file-wide name: `out` is the
// obvious name for a loop variable, both of the files this guard has to read use
// it twice in ONE function, and only one of the two loops carries a signature.
// Keying on the name alone reported the safe loop as well and turned 2 real
// findings into 12.
type gateTaintSpan struct {
	name     string
	from, to token.Pos
}

// gateTaintSpans finds the variables in file that hold text match recognises,
// each scoped to the source span where they hold it.
//
// It is a deliberately SHALLOW pass, and naming its limits is the point. It
// follows a range over a signature-bearing expression (scoped to that loop's
// body) and a var / const / := whose right-hand side is signature-bearing. It
// does NOT follow a value through a function call, a struct field, a map, a
// channel, or another file.
//
// The SPAN of a declaration is where the name can be read, not where it was
// written, and the two differ at package scope. A local holds its signature
// from the assignment to the end of the enclosing function, because a local
// really cannot be read before it exists. A package-level var holds one over
// the WHOLE file: Go has no forward-declaration rule at package scope, so a
// function declared above a detector table reads it exactly as well as one
// declared below it. Measured: with the span starting at the declaration
// instead, a `fmt.Fprintf(os.Stderr, …)` in dispatchDotToolNode that
// interpolated gateCannotRunSignatures was reported by NOTHING — the table is
// declared 250 lines further down the same file.
//
// The span is half of that repair. The other half is the second walk at the
// bottom of this function, which is what puts the span in hand before the
// reader is visited, and NEITHER half reports that write on its own. Both are
// pinned by a plant above the table in gateProductionSnippetSource.
//
// The span is a widening, and it widens in the over-report direction: spans are
// keyed by NAME and blind to scope (see gateTestingHandleNames for the same
// trade), so a local that shares a name with a package-level detector table is
// now treated as carrying a signature everywhere in its file. Measured over
// this package with both halves in place: 102 non-test files, 280 test files,
// ZERO findings. For THIS guard that direction is the safe one — its failure
// mode is a false clean.
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
func gateTaintSpans(file *ast.File, match func(string) bool) []gateTaintSpan {
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
			if gateExprCarriesText(node.X, match, spans) {
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
				if !gateExprCarriesText(rhs, match, spans) {
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
			// signature over the whole file — from the first line, not from its
			// own declaration, because a function above it can read it.
			from, end := node.End(), file.End()
			if len(funcEnds) > 0 {
				end = funcEnds[len(funcEnds)-1]
			} else {
				from = file.Pos()
			}
			for i, name := range node.Names {
				if i < len(node.Values) {
					if _, isCall := node.Values[i].(*ast.CallExpr); isCall {
						continue
					}
					if gateExprCarriesText(node.Values[i], match, spans) {
						add(name, from, end)
					}
				}
			}
		}
		return true
	}
	// TWO passes, and the second one KEEPS what the first learned. Reading a
	// name above its own declaration is legal at package scope and the walk is
	// source-order, so the first pass reaches the reader before the span it
	// needs exists. Only the second pass has it in hand.
	//
	// Both halves are load-bearing and each is useless alone. The span has to
	// cover the whole file (see above) or the reader is outside it; the second
	// pass has to inherit the spans or the reader is visited before they exist.
	// This loop used to clear `spans` at the top of every pass, which made the
	// second one a pure re-execution of the first — it delivered nothing, and
	// the comment on it claimed it delivered exactly this.
	//
	// Two passes and not a fixed point: a chain of package-level vars N deep
	// needs N of them, and this resolves one hop. That is the depth every shape
	// in this package has today — a table read by a function — and it is a
	// stopping point chosen for those shapes, not a closed one.
	for range 2 {
		ast.Inspect(file, walk)
	}
	// The same span is found once per pass. Dedupe, so a count of spans means
	// what it says.
	return gateDedupeSpans(spans)
}

// gateDedupeSpans collapses spans that repeat. Two passes over the same file
// find the same span twice, and a duplicate would make a COUNT of spans wrong
// while changing no answer about any single position.
func gateDedupeSpans(spans []gateTaintSpan) []gateTaintSpan {
	seen := map[gateTaintSpan]bool{}
	out := spans[:0]
	for _, span := range spans {
		if seen[span] {
			continue
		}
		seen[span] = true
		out = append(out, span)
	}
	return out
}

// gateTaintedAt reports whether name holds matched text at pos.
func gateTaintedAt(spans []gateTaintSpan, name string, pos token.Pos) bool {
	for _, span := range spans {
		if span.name == name && pos >= span.from && pos <= span.to {
			return true
		}
	}
	return false
}

// gateSignatureWriteSites reports every write in file whose message reaches the
// gate log carrying something the classifier keys on, without going through
// gateEvidenceQuote. Two classes, and they differ in which channels they apply
// to, because they differ in what protects them:
//
//   - an UNSCOPED signature, on ANY channel. Its detector reads the whole log,
//     so no line position protects it and an indent buys nothing.
//   - make's RECIPE ANCHOR, on the channels that land at COLUMN 0 only. The
//     anchor is read only inside make's terminal cascade and only on a line
//     that is not indented, so go test's indent really is cover for a testing
//     handle — and it is no cover at all for a write straight to stderr, which
//     is the class this guard was named after and did not have.
//
// The anchor class is why this tier reads the whole package rather than one
// file. Its predecessor (gateOutputSites) reports every unsanitized write, and
// so it can only afford to read the single file it is kept clean in; a sibling
// file writing an anchor at column 0 was invisible to both tiers, and a planted
// one left the whole gate suite green (hk-gate-anchor-guard-one-file-x6t5k).
func gateSignatureWriteSites(fset *token.FileSet, name string, file *ast.File) []string {
	handles := gateTestingHandleNames(file)
	signatureSpans := gateTaintSpans(file, gateSignatureInText)
	anchorSpans := gateTaintSpans(file, gateAnchorInText)
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
		carries := gateArgsCarry(msgArgs, gateSignatureInText, signatureSpans)
		if !carries && gateChannelLandsAtColumnZero(channel, handles) {
			carries = gateArgsCarry(msgArgs, gateAnchorInText, anchorSpans)
		}
		if carries {
			sites = append(sites, fmt.Sprintf("%s:%d: %s", name, fset.Position(call.Pos()).Line, channel))
		}
		return true
	})
	return sites
}

// gateArgsCarry reports whether any of a write's message arguments carries text
// the matcher recognises.
func gateArgsCarry(args []ast.Expr, match func(string) bool, spans []gateTaintSpan) bool {
	for _, arg := range args {
		if gateExprCarriesText(arg, match, spans) {
			return true
		}
	}
	return false
}

// gateSnippetName is the file name the synthetic production file is parsed
// under, and gateSnippetPlantMarker marks each write in it that MUST be
// reported. The marker is a comment, so the parser ignores it and the line it
// sits on is the expected finding.
const (
	gateSnippetName        = "synthetic_production.go"
	gateSnippetPlantMarker = "// PLANTED"
)

// gateProductionSnippetSource is a production file in miniature, shaped like the
// daemon's own stderr diagnostic: no testing handle anywhere, a package-level
// detector table, and a loop variable that carries one of its strings into a
// write that lands at column 0.
//
// It is planted HERE and never in a real file of this package. A plant in a
// tracked file is a signature this daemon really can write into the next gate's
// log, which is the defect itself.
//
// The table is declared BETWEEN the two functions that read it, on purpose. That
// is the shape of the real file — dispatchDotToolNode writes its diagnostic 250
// lines above gateCannotRunSignatures — and a taint span that starts at the
// declaration reports the second write and misses the first, silently. Marking
// both plants is what makes that miss a failure.
//
// Six writes and three plants. The third plant carries make's recipe anchor and
// nothing else, and it is what pins the ANCHOR class: this same source with the
// anchor plant unreported is what the guard looked like when a column-0 anchor
// in a sibling file left the whole gate suite green.
//
// The other three writes are what keeps the guard usable: one sanitizes its
// message, one carries no signature at all, and one is an ordinary line beside
// the anchor plant. A pass that reported those would be one nobody could keep
// green.
const gateProductionSnippetSource = `package daemon

import (
	"fmt"
	"os"
)

func snippetDispatch(node string) {
	for _, sig := range snippetSignatures {
		fmt.Fprintf(os.Stderr, "daemon: node %s: %s\n", node, sig.text) // PLANTED
	}
}

var snippetSignatures = []gateOutputSignature{
	{text: ": command not found", quoted: ": command was not found"},
}

func snippetClassify(out string) {
	for _, sig := range snippetSignatures {
		fmt.Fprintf(os.Stderr, "gate: the log carries %s\n", sig.text) // PLANTED
		fmt.Fprint(os.Stderr, gateEvidenceQuote(sig.text))
	}
	fmt.Fprintf(os.Stderr, "gate: %d bytes of gate output\n", len(out))
}

func snippetKilled(node string) {
	fmt.Fprintf(os.Stderr, "gate: make[1]: *** [%s] Error 143\n", node) // PLANTED
	fmt.Fprintf(os.Stderr, "gate: node %s reached the end of its budget\n", node)
}
`

// gateScanProductionSnippet runs the package-wide scan over one synthetic
// production-shaped source. It returns the sites the scan reported and the sites
// the source MARKED, both in source order, so the caller compares one list with
// the other and never with a number it wrote down.
func gateScanProductionSnippet(t *testing.T, src string) (got, want []string) {
	t.Helper()
	for i, line := range strings.Split(src, "\n") {
		if strings.Contains(line, gateSnippetPlantMarker) {
			want = append(want, fmt.Sprintf("%s:%d: fmt.Fprintf", gateSnippetName, i+1))
		}
	}
	if len(want) < 3 {
		gateFatalf(t, "the synthetic production source marks %d plant(s); it needs the one ABOVE the detector table, the one below it, and the bare recipe anchor, or the span rule and the anchor class are not both being measured", len(want))
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, gateSnippetName, src, parser.SkipObjectResolution)
	if err != nil {
		gateFatalf(t, "parse the synthetic production source: %v", err)
	}
	return gateSignatureWriteSites(fset, gateSnippetName, file), want
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
// It reads PRODUCTION files too, not only test files. A test's message is
// indented by go test; a daemon diagnostic goes straight to stderr at column 0,
// which is the position make's own report occupies, so a production write is the
// worse of the two. Measured when this was widened: 102 non-test files in this
// package, zero findings — the cost of the wider scan is nothing and the next
// `fmt.Fprintf(os.Stderr, …)` that spells a signature is caught.
//
// SCOPE, and it is narrower than the single-file guard on purpose. It reports
// two classes and not every unsanitized write: an UNSCOPED signature on any
// channel, and make's RECIPE ANCHOR on the channels that land at column 0. See
// gateSignatureWriteSites for why the anchor class is asked of those channels
// only. Widening this to every unsanitized write in the package would flag
// several thousand ordinary t.Fatalf calls that carry nothing the classifier
// reads, and a guard nobody can keep green is a guard that gets deleted.
//
// The anchor class was the half of this guard that did not ship the first time.
// It read one file, so a column-0 anchor written from a SIBLING test file left
// the entire gate suite green — measured, not inferred
// (hk-gate-anchor-guard-one-file-x6t5k). The indented t.Errorf anchors this
// package really does write stay unreported, and that is the reading end's job:
// gateLineIsIndented keeps them out of the cascade.
//
// It reads WRITES, and not the data flowing into them. A production helper that
// pastes gate output into a message it RETURNS is invisible here — that is what
// gateBackEdgeMessage and gateFailureTail do, and what
// TestAMessageQuotingGateOutputCannotRelabelTheNextGate covers instead, by
// behaviour rather than by source text.
func TestNoSignatureInThisPackageReachesTheLogUnsanitized(t *testing.T) {
	t.Parallel()

	fset := token.NewFileSet()
	files := gateParsePackage(t, fset, true)
	for name, file := range gateParsePackage(t, fset, false) {
		files[name] = file
	}

	var offenders []string
	for name, file := range files {
		offenders = append(offenders, gateSignatureWriteSites(fset, name, file)...)
	}
	sort.Strings(offenders)

	if len(offenders) > 0 {
		gateReportf(t, "these write a classifier signature, or make's recipe anchor at column 0, into the gate's log without passing it through %s, and either kind makes the next genuinely-red gate read as structural. A TEST site fires only when that test fails, and go test indents it. A PRODUCTION site is worse on both counts: the daemon writes its diagnostic every time it classifies a failure, not only when something is wrong, and it lands on stderr at column 0 — the position make's own report occupies, where no indentation rule can rule it out. Sites: %s",
			gateSanitizerName, strings.Join(offenders, ", "))
	}

	// Positive evidence: the scan reached sibling files and understood them. If
	// the taint pass silently stops working, every file looks clean.
	if len(files) < 2 {
		gateFatalf(t, "the scan parsed %d file(s) in this package; it is not reading the directory it thinks it is", len(files))
	}
	// And that it reached the PRODUCTION half. Both halves come from the same
	// glob, so a widening that silently reverts leaves the rest of this test
	// looking exactly as clean as it does now.
	if files["dot_cascade_helpers.go"] == nil {
		gateFatalf(t, "the scan did not read the file the classifier lives in, so nothing here covers a diagnostic the daemon writes itself")
	}
	// Positive evidence that the taint pass still does the one thing the real
	// findings need: follow a range over a signature-bearing slice, and scope it
	// to that loop. Without this a pass that silently stopped resolving names
	// would report a clean package.
	probe := gateTaintSpans(files["dot_cascade_gatecannotrun_hk2f3v4_test.go"], gateSignatureInText)
	var outSpans int
	for _, span := range probe {
		if span.name == "out" {
			outSpans++
		}
	}
	if outSpans != 1 {
		gateFatalf(t, "the taint pass found %d tainted span(s) for the loop variable `out` in dot_cascade_gatecannotrun_hk2f3v4_test.go; that file has exactly one signature-bearing loop, so this test is reading a file it no longer understands", outSpans)
	}
	// Positive evidence for the WIDENING itself. Everything above proves the
	// production files were parsed and that nothing in them was reported; none
	// of it proves the pass can still produce a finding in one, and a scan that
	// quietly stopped finding things looks exactly like a clean package. So run
	// the same pass over a production-shaped source with writes planted in it,
	// and require exactly those writes back — by file and line.
	//
	// One plant sits ABOVE the detector table it reads. That one pins the span
	// rule: a package-level table taints its whole file, and the day it taints
	// only the source below its own declaration, this is what says so. The real
	// file has that shape and the miss is silent.
	sites, plants := gateScanProductionSnippet(t, gateProductionSnippetSource)
	if got, want := strings.Join(sites, ", "), strings.Join(plants, ", "); got != want {
		gateFatalf(t, "the scan over a synthetic production file reported %q; it must report exactly the planted writes, %q. A missing site means the guard no longer sees a signature the daemon writes to stderr at column 0 — and the plant above the table goes missing on its own if a package-level detector table stops tainting the whole file. An extra site means it now flags the sanitized write beside them.", got, want)
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

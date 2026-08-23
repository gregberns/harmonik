package daemon

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

func gateReportf(t *testing.T, format string, args ...any) {
	t.Helper()
	t.Error(gateEvidenceQuote(fmt.Sprintf(format, args...)))
}

func gateFatalf(t *testing.T, format string, args ...any) {
	t.Helper()
	t.Fatal(gateEvidenceQuote(fmt.Sprintf(format, args...)))
}

const retiredKillDiagnostic = `daemon: dot tool node "commit_gate" was KILLED mid-flight ` +
	`(gate output reports a signal kill: make[1]: *** [test-scenario] Terminated: 15) — ` +
	`it reached no verdict, so this is NOT a test failure; canceled, routed to ` +
	`close-needs-attention for triage; gate log: ` +
	`/var/folders/s9/T/TestGateKilledBySignal_MakeOutput294346514/001/.harmonik/gate-logs/019ff7c2/commit_gate.log`

const redGateCascade = "FAIL\nscenario skips: 2\n" +
	"make[1]: *** [test-scenario] Error 1\n" +
	"make: *** [full] Error 2\n"

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

func shellExit(t *testing.T, code int) error {
	t.Helper()
	//nolint:gosec // G204: the exit code is an int this test chose; no external input reaches it
	err := exec.CommandContext(t.Context(), "/bin/sh", "-c", "exit "+strconv.Itoa(code)).Run()
	if err == nil {
		gateFatalf(t, "`exit %d` reported success", code)
	}
	return err
}

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
		"daemon diagnostic": retiredKillDiagnostic,
		// A gate step that echoes make's kill output as its own fixture. No go
		// test log prefix on it, so POSITION is the only thing that can rule it
		// out — which is the property this test is for.
		"echoed fixture": "make[1]: *** [test-scenario] Terminated: 15",
	} {
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
	if isGateCannotRunError([]byte(desc)) || isGateBuildCacheInfraError([]byte(desc)) {
		gateReportf(t, "the kill diagnostic carries a string an UNSCOPED detector matches, so quoting it in a later gate log relabels that gate wherever the line lands:\n%s", desc)
	}
	if !strings.Contains(desc, "test-scenario") || !strings.Contains(desc, "Terminated: 15") {
		gateReportf(t, "the kill diagnostic no longer says which recipe died or how:\n%s", desc)
	}

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

		raw := "make[2]: *** [fmt-check] " + sig.text
		clean := gateEvidenceQuote(raw)

		if strings.Contains(clean, sig.text) {
			gateReportf(t, "the sanitizer leaves %q in a message it rendered: %q", sig.text, clean)
		}
		if isGateCannotRunError([]byte(clean)) || isGateBuildCacheInfraError([]byte(clean)) {
			gateReportf(t, "a sanitized message still trips an unscoped detector: %q", clean)
		}

		if class, _ := classifyGateLog(t, gateLogWithTranscript(raw, redGateCascade)); class == core.FailureClassDeterministic {
			gateFatalf(t, "signature %q planted RAW in a red gate's transcript leaves the class deterministic, so this test cannot fail and proves nothing", sig.text)
		}
		if class, _ := classifyGateLog(t, gateLogWithTranscript(clean, redGateCascade)); class != core.FailureClassDeterministic {
			gateReportf(t, "a red gate whose transcript replays the sanitized form of %q is classified %q; the implementer is told NOTHING is wrong with a change whose tests failed", sig.text, class)
		}
	}

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

	for name, escaped := range map[string]string{
		"one-line message":  "    dot_cascade_gatekilled_test.go:41: a gate that was killed reads as a clean exit: make[1]: *** [test-scenario] Terminated: 15",
		"continuation line": "        make[1]: *** [test-scenario] Terminated: 15",
	} {
		gateAssertEscapedAnchorIsNotEvidence(t, name, escaped)
	}
}

func gateAssertEscapedAnchorIsNotEvidence(t *testing.T, name, escaped string) {
	t.Helper()

	trailer := []string{
		"--- FAIL: TestGateSignalKillOutputLine (0.01s)",
		"FAIL",
		"FAIL\tgithub.com/gregberns/harmonik/internal/daemon\t1.0s",
		"FAIL",
	}
	if len(trailer) != gateCascadeGapLines {
		gateFatalf(t, "go test's failure trailer is %d lines and the cascade gap tolerance is %d; this test no longer sets up the case it describes", len(trailer), gateCascadeGapLines)
	}

	log := []byte("=== RUN   TestGateSignalKillOutputLine\n" + escaped + "\n" +
		strings.Join(trailer, "\n") + "\n" +
		"make[1]: *** [test-unit] Error 1\n" +
		"make: *** [full] Error 2\n")

	if !gateLineIsIndented(escaped) {
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

var gateTestOutputMethods = map[string]bool{
	"Error": true, "Errorf": true, "Fatal": true, "Fatalf": true,
	"Log": true, "Logf": true, "Skip": true, "Skipf": true,
}

const gateSanitizerName = "gateEvidenceQuote"

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

func gateOutputChannel(call *ast.CallExpr, handles map[string]bool) (channel string, msgArgs []ast.Expr, ok bool) {
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
			return path, call.Args[1:], true
		}
		if strings.HasPrefix(method, "Print") {
			return path, call.Args, true
		}
	case recv == "log", recv == "os.Stdout", recv == "os.Stderr":
		return path, call.Args, true
	case recv == "slog":
		switch method {
		case "Debug", "Info", "Warn", "Error":
			return path, call.Args, true
		case "DebugContext", "InfoContext", "WarnContext", "ErrorContext":
			return path, gateArgsAfter(call.Args, 1), true
		case "Log", "LogAttrs":
			return path, gateArgsAfter(call.Args, 2), true
		}
	}
	return "", nil, false
}

func gateArgsAfter(args []ast.Expr, n int) []ast.Expr {
	if len(args) <= n {
		return nil
	}
	return args[n:]
}

func gateReceiverIsTestingHandle(recv string, handles map[string]bool) bool {
	if handles[recv] {
		return true
	}
	if i := strings.LastIndex(recv, "."); i >= 0 {
		return handles[recv[i+1:]]
	}
	return false
}

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

	if q := gateEvidenceQuote("make[1]: " + gateRecipeFailureAnchor + "test-scenario] Error 143"); strings.Contains(q, gateRecipeFailureAnchor) {
		gateReportf(t, "gateEvidenceQuote leaves make's recipe anchor in place, so every message in this file carries it")
	}

	fset := token.NewFileSet()
	unsanitized, sanitized := gateOutputSites(fset, gateParseThisFile(t, fset))

	if len(unsanitized) > 0 {
		gateReportf(t, "these write to the test log without passing the text through %s, so it can be read as make's own report — or as a missing tool — in the next gate's log: %s",
			gateSanitizerName, strings.Join(unsanitized, ", "))
	}
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
		"far from the cascade": func(msg string) []byte {
			return gateLogWithTranscript(msg, redGateCascade)
		},
		// Hard against the cascade, where a recipe anchor joins it and a signal
		// word on the same line reads as a kill.
		"against the cascade": func(msg string) []byte {
			return gateLogWithMessageAgainstTheCascade(msg, redGateCascade)
		},
	}

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

	if !gateOutputHasAnySignature(gateFailureTailPrefix+"command not found", gateUnscopedSignatures) {
		gateFatalf(t, "pasting %q straight onto an excerpt that begins `command not found` no longer builds a signature. If the prefix was made safe on its own, this control is what says so — replace it deliberately rather than leaving a check that cannot fail.", gateFailureTailPrefix)
	}

	if got := gateFailureTail("command not found"); gateOutputHasAnySignature(got, gateUnscopedSignatures) {
		gateReportf(t, "the clause built a detector signature at the join with its own prefix, so a red gate whose log replays this reason reads as structural:%s", got)
	}

	for _, sig := range gateUnscopedSignatures {
		for i := 1; i < len(sig.text); i++ {
			notes := sig.text[i:] + " (while running the fmt-check recipe)"

			if gateOutputHasAnySignature(gateEvidenceQuote(notes), gateUnscopedSignatures) {
				gateFatalf(t, "the probe still carries a whole signature after sanitizing, so splitting %q after %d byte(s) measures nothing about the join", sig.text, i)
			}

			for writer, render := range writers {
				got := render(notes)
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

	filler := strings.Repeat("go downloading something irrelevant\n", 8)

	var straddling int
	for _, sig := range gateUnscopedSignatures {
		for i := 1; i < len(sig.text); i++ {
			head := sig.text[i:]
			if len(head) > gateFailureTailMaxBytes {
				continue
			}
			tail := head + strings.Repeat("x", gateFailureTailMaxBytes-len(head))
			notes := filler + tail

			if gateJoinBuildsASignature(gateFailureTailPrefix, head) {
				straddling++
			}

			folded := strings.ReplaceAll(strings.TrimSpace(notes), "\n", " ")
			if gateEvidenceQuote(folded) != folded {
				gateFatalf(t, "the probe built from %q split after %d byte(s) is rewritten by the sanitizer, so the cut no longer lands where this test places it", sig.text, i)
			}

			got := gateFailureTail(notes)

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

	if straddling == 0 {
		gateFatalf(t, "no probe here begins with a phrase that %q could build a signature with, so nothing in this test exercises the cut and the seam at once", gateFailureTailPrefix)
	}
}

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

func gatePackageDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		gateFatalf(t, "cannot find this file's own path, so the guard checked nothing")
	}
	return filepath.Dir(thisFile)
}

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

func gateSignatureInText(text string) bool {
	for _, sig := range gateUnscopedSignatures {
		if strings.Contains(text, sig.text) {
			return true
		}
	}
	return false
}

func gateAnchorInText(text string) bool {
	return strings.Contains(text, gateRecipeFailureAnchor)
}

func gateChannelLandsAtColumnZero(channel string, handles map[string]bool) bool {
	i := strings.LastIndex(channel, ".")
	if i < 0 {
		return true
	}
	return !gateReceiverIsTestingHandle(channel[:i], handles)
}

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

type gateTaintSpan struct {
	name     string
	from, to token.Pos
}

func gateTaintSpans(file *ast.File, match func(string) bool) []gateTaintSpan {
	var spans []gateTaintSpan
	add := func(target ast.Expr, from, to token.Pos) {
		if id, isIdent := target.(*ast.Ident); isIdent && id.Name != "_" {
			spans = append(spans, gateTaintSpan{name: id.Name, from: from, to: to})
		}
	}
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
	for range 2 {
		ast.Inspect(file, walk)
	}
	return gateDedupeSpans(spans)
}

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

func gateTaintedAt(spans []gateTaintSpan, name string, pos token.Pos) bool {
	for _, span := range spans {
		if span.name == name && pos >= span.from && pos <= span.to {
			return true
		}
	}
	return false
}

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

func gateArgsCarry(args []ast.Expr, match func(string) bool, spans []gateTaintSpan) bool {
	for _, arg := range args {
		if gateExprCarriesText(arg, match, spans) {
			return true
		}
	}
	return false
}

const (
	gateSnippetName        = "synthetic_production.go"
	gateSnippetPlantMarker = "// PLANTED"
)

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

	if len(files) < 2 {
		gateFatalf(t, "the scan parsed %d file(s) in this package; it is not reading the directory it thinks it is", len(files))
	}
	if files["dot_cascade_helpers.go"] == nil {
		gateFatalf(t, "the scan did not read the file the classifier lives in, so nothing here covers a diagnostic the daemon writes itself")
	}
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
	sites, plants := gateScanProductionSnippet(t, gateProductionSnippetSource)
	if got, want := strings.Join(sites, ", "), strings.Join(plants, ", "); got != want {
		gateFatalf(t, "the scan over a synthetic production file reported %q; it must report exactly the planted writes, %q. A missing site means the guard no longer sees a signature the daemon writes to stderr at column 0 — and the plant above the table goes missing on its own if a package-level detector table stops tainting the whole file. An extra site means it now flags the sanitized write beside them.", got, want)
	}
}

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

			if lit, isLit := vs.Values[0].(*ast.CompositeLit); isLit {
				if at, isArray := lit.Type.(*ast.ArrayType); isArray {
					if id, isIdent := at.Elt.(*ast.Ident); isIdent && id.Name == "gateOutputSignature" {
						declared[varName] = name
					}
				}
			}

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

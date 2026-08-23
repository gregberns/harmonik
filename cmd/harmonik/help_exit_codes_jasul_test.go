package main

import (
	"bytes"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/hookrelay"
)

var jasulHeadingRe = regexp.MustCompile(`^[A-Z][A-Z0-9 ()/|-]*$`)

func jasulExitCodeSection(help string) string {
	lines := strings.Split(help, "\n")
	out := make([]string, 0, len(lines))
	in := false
	for _, line := range lines {
		if !in {
			if strings.HasPrefix(line, "EXIT CODES") {
				in = true
			}
			continue
		}
		if jasulHeadingRe.MatchString(line) && strings.TrimSpace(line) != "" {
			break
		}
		out = append(out, line)
	}
	if !in {
		return ""
	}
	return strings.Join(out, "\n")
}

func jasulDocumentsCode(section string, code int) bool {
	want := strconv.Itoa(code)
	for _, line := range strings.Split(section, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || trimmed == line {
			continue // not indented: not an entry
		}
		fields := strings.Fields(trimmed)
		if len(fields) > 0 && fields[0] == want {
			return true
		}
	}
	return false
}

type jasulCase struct {
	name string
	args []string
	want int
}

func jasulCaptureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("jasulCaptureStdout: os.Pipe: %v", err)
	}
	orig := os.Stdout
	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		//nolint:errcheck // the pipe is drained until EOF; a read error yields
		// a short capture, which the caller's assertion reports on its own.
		_, _ = io.Copy(&buf, r)
		done <- buf.String()
	}()
	fn()
	os.Stdout = orig
	if closeErr := w.Close(); closeErr != nil {
		t.Fatalf("jasulCaptureStdout: close write end: %v", closeErr)
	}
	got := <-done
	if closeErr := r.Close(); closeErr != nil {
		t.Fatalf("jasulCaptureStdout: close read end: %v", closeErr)
	}
	return got
}

func jasulCaptureBoth(t *testing.T, fn func()) (stdout, stderr string) {
	t.Helper()
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatalf("jasulCaptureBoth: os.Pipe: %v", err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatalf("jasulCaptureBoth: os.Pipe: %v", err)
	}
	origOut, origErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outW, errW

	drain := func(r *os.File) <-chan string {
		ch := make(chan string, 1)
		go func() {
			var buf bytes.Buffer
			//nolint:errcheck // the pipe is drained until EOF; a short read shows
			// up as a failed assertion in the caller.
			_, _ = io.Copy(&buf, r)
			ch <- buf.String()
		}()
		return ch
	}
	outCh, errCh := drain(outR), drain(errR)

	fn()

	os.Stdout, os.Stderr = origOut, origErr
	if closeErr := outW.Close(); closeErr != nil {
		t.Fatalf("jasulCaptureBoth: close stdout writer: %v", closeErr)
	}
	if closeErr := errW.Close(); closeErr != nil {
		t.Fatalf("jasulCaptureBoth: close stderr writer: %v", closeErr)
	}
	stdout, stderr = <-outCh, <-errCh
	if closeErr := outR.Close(); closeErr != nil {
		t.Fatalf("jasulCaptureBoth: close stdout reader: %v", closeErr)
	}
	if closeErr := errR.Close(); closeErr != nil {
		t.Fatalf("jasulCaptureBoth: close stderr reader: %v", closeErr)
	}
	return stdout, stderr
}

func jasulAssert(t *testing.T, verb, help string, cases []jasulCase, run func(args []string) int) {
	t.Helper()
	section := jasulExitCodeSection(help)
	if section == "" {
		t.Fatalf("%s --help has no EXIT CODES section:\n%s", verb, help)
	}
	for _, tc := range cases {
		got := run(tc.args)
		if got != tc.want {
			t.Errorf("%s %v: exit code = %d, want %d", verb, tc.args, got, tc.want)
			continue
		}
		if !jasulDocumentsCode(section, got) {
			t.Errorf("%s %v really exits %d, but %s --help does not list %d:\n%s",
				verb, tc.args, got, verb, got, section)
		}
	}
}

// TestGraphHelpDocumentsRealExitCodes covers `harmonik graph`.
//
// Not parallel: jasulCaptureStdout swaps the process-global os.Stdout.
func TestGraphHelpDocumentsRealExitCodes(t *testing.T) {
	help := jasulCaptureStdout(t, func() {
		if code := runGraphSubcommand([]string{"--help"}); code != 0 {
			t.Errorf("graph --help: exit code = %d, want 0", code)
		}
	})

	cases := []jasulCase{
		{name: "no verb prints help", args: []string{}, want: 0},
		{name: "help flag", args: []string{"--help"}, want: 0},
		{name: "unrecognised verb", args: []string{"no-such-verb"}, want: 2},
	}
	jasulAssert(t, "graph", help, cases, func(args []string) int {
		var code int
		_ = jasulCaptureStdout(t, func() { code = runGraphSubcommand(args) })
		return code
	})
}

// TestHandlerHelpDocumentsRealExitCodes covers `harmonik handler`.
//
// The umbrella help must carry every code the two verbs can return, because an
// operator who reads only `handler --help` sees a 3 from `handler resume` and
// has nothing to look it up in.
func TestHandlerHelpDocumentsRealExitCodes(t *testing.T) {
	t.Parallel()

	var helpBuf, errBuf bytes.Buffer
	if code := runHandlerSubcommandIO([]string{"--help"}, &helpBuf, &errBuf); code != 0 {
		t.Fatalf("handler --help: exit code = %d, want 0", code)
	}

	project := t.TempDir()
	if err := os.MkdirAll(project+"/.harmonik", 0o750); err != nil {
		t.Fatalf("mkdir .harmonik: %v", err)
	}
	state := `{"schema_version":1,"handlers":{"claude-code":{"status":"live"}}}`
	if err := os.WriteFile(project+"/.harmonik/handler-state.json", []byte(state), 0o600); err != nil {
		t.Fatalf("write handler-state.json: %v", err)
	}

	cases := []jasulCase{
		{name: "help", args: []string{"--help"}, want: 0},
		{name: "status succeeds", args: []string{"status", "--project", project}, want: 0},
		{name: "no verb", args: []string{}, want: 1},
		{name: "unrecognised verb", args: []string{"no-such-verb"}, want: 1},
		{name: "resume unknown type", args: []string{"resume", "--type", "no-such-handler", "--project", project}, want: 2},
		{name: "resume already live", args: []string{"resume", "--type", "claude-code", "--project", project}, want: 3},
	}
	jasulAssert(t, "handler", helpBuf.String(), cases, func(args []string) int {
		var out, errOut bytes.Buffer
		return runHandlerSubcommandIO(args, &out, &errOut)
	})
}

// TestStartHelpDocumentsRealExitCodes covers `harmonik start`.
//
// The role launchers are injected, so no tmux session and no daemon RPC happens
// here. Codes 1 and 17 come from the launcher, so the injected launcher returns
// them and the test proves `start` passes them through unchanged rather than
// flattening them.
func TestStartHelpDocumentsRealExitCodes(t *testing.T) {
	t.Parallel()

	var helpBuf bytes.Buffer
	if err := startUsage(&helpBuf); err != nil {
		t.Fatalf("startUsage: %v", err)
	}
	help := helpBuf.String()

	okDispatch := startDispatch{
		captain: func([]string) int { return 0 },
		crew:    func([]string) int { return 0 },
	}
	cases := []struct {
		jasulCase
		dispatch startDispatch
	}{
		{jasulCase{"help", []string{"--help"}, 0}, okDispatch},
		{jasulCase{"captain launcher ok", []string{"captain"}, 0}, okDispatch},
		{jasulCase{"no role", []string{}, 2}, okDispatch},
		{jasulCase{"unknown role", []string{"no-such-role"}, 2}, okDispatch},
		{jasulCase{"name mixed with flags", []string{"crew", "paul", "--queue", "q"}, 2}, okDispatch},
		{jasulCase{"captain takes no positional", []string{"captain", "paul"}, 2}, okDispatch},
		{
			jasulCase{"captain launcher fails", []string{"captain"}, 1},
			startDispatch{captain: func([]string) int { return 1 }, crew: func([]string) int { return 0 }},
		},
		{
			jasulCase{"crew launcher finds no daemon", []string{"crew", "paul"}, 17},
			startDispatch{captain: func([]string) int { return 0 }, crew: func([]string) int { return 17 }},
		},
	}

	section := jasulExitCodeSection(help)
	if section == "" {
		t.Fatalf("start --help has no EXIT CODES section:\n%s", help)
	}
	for _, tc := range cases {
		var out, errOut bytes.Buffer
		got := runStartWith(tc.args, tc.dispatch, &out, &errOut)
		if got != tc.want {
			t.Errorf("start %v (%s): exit code = %d, want %d; stderr: %s",
				tc.args, tc.name, got, tc.want, errOut.String())
			continue
		}
		if !jasulDocumentsCode(section, got) {
			t.Errorf("start %v really exits %d, but start --help does not list %d:\n%s",
				tc.args, got, got, section)
		}
	}
}

// TestProjectHashHelpDocumentsRealExitCodes covers `harmonik project-hash`.
//
// Exit 2 is the new one. Before this bead the arg loop skipped anything it did
// not recognise, so `project-hash --proj /elsewhere` printed the hash of the
// CURRENT directory and exited 0. The caller cannot tell that from a correct
// answer, and the hash keys tmux sessions and process markers, so the wrong one
// gets acted on.
//
// Not parallel: jasulCaptureStdout swaps the process-global os.Stdout.
func TestProjectHashHelpDocumentsRealExitCodes(t *testing.T) {
	help := jasulCaptureStdout(t, func() {
		if code := runProjectHashSubcommand([]string{"--help"}); code != 0 {
			t.Errorf("project-hash --help: exit code = %d, want 0", code)
		}
	})

	cases := []jasulCase{
		{name: "prints a hash", args: []string{"--project", t.TempDir()}, want: 0},
		{name: "no such directory", args: []string{"--project", "/no-such-dir-jasul-12345"}, want: 1},
		{name: "misspelled flag", args: []string{"--proj", "/tmp"}, want: 2},
		{name: "stray positional", args: []string{"/tmp"}, want: 2},
		{name: "project with no value", args: []string{"--project"}, want: 2},
		{name: "project with an empty value", args: []string{"--project", ""}, want: 2},
		{name: "project= with an empty value", args: []string{"--project="}, want: 2},
	}
	jasulAssert(t, "project-hash", help, cases, func(args []string) int {
		var code int
		_ = jasulCaptureStdout(t, func() { code = runProjectHashSubcommand(args) })
		return code
	})
}

// TestProjectHashRefusesUnknownArgumentInsteadOfAnsweringWrong pins the exact
// failure the old loop had: an argument this command cannot honour must not
// produce the hash of the current directory.
//
// Three routes reach that failure. A misspelled flag was skipped by the old
// loop. An EMPTY --project value passed the "is there a token after the flag"
// guard and set an empty directory, which then fell through to the working
// directory — so the first repair of this defect did not close it, and a shell
// that expands "$P" to nothing hands the command exactly that. `--project=`
// carries the same empty value in the equals spelling.
//
// Not parallel: swaps os.Stdout.
func TestProjectHashRefusesUnknownArgumentInsteadOfAnsweringWrong(t *testing.T) {
	cases := []struct {
		name string
		args []string
		// wantErrSubstr is the diagnostic the run must print. The two empty-value
		// spellings must both name the empty value. `--project=` already exited 2
		// earlier in this same round of work, under the message "unrecognized
		// argument", so the exit code alone cannot tell the fix from the defect
		// there — only this does. Before the round it exited 0 with the wrong hash.
		wantErrSubstr string
	}{
		{"misspelled flag", []string{"--proj", t.TempDir()}, "unrecognized argument"},
		{"empty value after --project", []string{"--project", ""}, "--project needs a directory after it"},
		{"empty value in the equals spelling", []string{"--project="}, "--project needs a directory after it"},
	}
	for _, tc := range cases {
		var code int
		stdout, stderr := jasulCaptureBoth(t, func() {
			code = runProjectHashSubcommand(tc.args)
		})
		if code != 2 {
			t.Errorf("project-hash %v (%s): exit code = %d, want 2", tc.args, tc.name, code)
		}
		if strings.TrimSpace(stdout) != "" {
			t.Errorf("project-hash %v (%s) printed %q on stdout; a refused run must print no hash",
				tc.args, tc.name, stdout)
		}
		if !strings.Contains(stderr, tc.wantErrSubstr) {
			t.Errorf("project-hash %v (%s) stderr = %q, want it to contain %q",
				tc.args, tc.name, stderr, tc.wantErrSubstr)
		}
	}
}

// TestHookRelayHelpDocumentsRealExitCodes covers `harmonik hook-relay`.
//
// The help used to say only "The daemon must be running to receive events" and
// list no exit codes. A reader took that for a precondition the command checks,
// and it is not: the relay looks at the daemon only on a path that has an event
// to send. Every case below drives hookrelay.Run, which is the code main.go
// calls, so the exit codes are the real ones.
//
// Not parallel: mutates os.Args and the process environment.
func TestHookRelayHelpDocumentsRealExitCodes(t *testing.T) {
	mainFixtureResetFlags(t)
	mainFixtureSaveRestoreArgs(t, []string{"harmonik", "hook-relay", "--help"})

	var helpExit int
	help := jasulCaptureStdout(t, func() { helpExit = run() })
	if helpExit != 0 {
		t.Fatalf("hook-relay --help: exit code = %d, want 0", helpExit)
	}
	section := jasulExitCodeSection(help)
	if section == "" {
		t.Fatalf("hook-relay --help has no EXIT CODES section:\n%s", help)
	}

	goodEnv := &hookrelay.Env{
		RunID:            "run-1",
		DaemonSocket:     t.TempDir() + "/daemon.sock",
		WorkspacePath:    t.TempDir(),
		HandlerSessionID: "handler-1",
		ClaudeSessionID:  "claude-1",
		WorkflowID:       "wf-1",
		NodeID:           "node-1",
		AgentType:        "claude-code",
	}

	if got := hookrelay.Run("PreToolUse", strings.NewReader(""), io.Discard, goodEnv); got != 0 {
		t.Errorf("hook-relay PreToolUse: exit code = %d, want 0", got)
	} else if !jasulDocumentsCode(section, 0) {
		t.Errorf("hook-relay really exits 0 for an unforwarded kind, but the help does not list 0:\n%s", section)
	}

	sessionEnd := `{"session_id":"claude-1","hook_event_name":"SessionEnd","transcript_path":"/tmp/t.jsonl"}`
	if got := hookrelay.Run("SessionEnd", strings.NewReader(sessionEnd), io.Discard, goodEnv); got != 0 {
		t.Errorf("hook-relay SessionEnd: exit code = %d, want 0 (it must not reach the socket)", got)
	}
	sent := jasulKindsHelpSaysAreSent(help)
	if sent["SessionEnd"] {
		t.Errorf("hook-relay --help lists SessionEnd among the kinds it sends, but the relay never sends it:\n%s", help)
	}
	for _, kind := range []string{"SessionStart", "Stop", "StopFailure", "Notification"} {
		if !sent[kind] {
			t.Errorf("hook-relay --help does not list %s among the kinds it sends, but the relay sends it:\n%s", kind, help)
		}
	}

	for _, key := range jasulRequiredRelayEnvKeys {
		mainFixtureSaveRestoreEnv(t, key, "", false)
	}
	if got := hookrelay.Run("Stop", strings.NewReader(sessionEnd), io.Discard, nil); got != 1 {
		t.Errorf("hook-relay Stop with eight exported-but-empty variables: exit code = %d, want 1", got)
	}

	if got := hookrelay.Run("Stop", strings.NewReader("not json"), io.Discard, goodEnv); got != 1 {
		t.Errorf("hook-relay Stop with bad stdin: exit code = %d, want 1", got)
	} else if !jasulDocumentsCode(section, 1) {
		t.Errorf("hook-relay really exits 1 for bad stdin, but the help does not list 1:\n%s", section)
	}

	mismatch := `{"session_id":"someone-else","hook_event_name":"Stop","transcript_path":"/tmp/t.jsonl"}`
	if got := hookrelay.Run("Stop", strings.NewReader(mismatch), io.Discard, goodEnv); got != 1 {
		t.Errorf("hook-relay Stop with a mismatched session id: exit code = %d, want 1", got)
	}

	for _, key := range jasulRequiredRelayEnvKeys {
		mainFixtureSaveRestoreEnv(t, key, "", true)
	}
	mainFixtureSaveRestoreEnv(t, "HARMONIK_RUN_ID", "run-1", false)
	if got := hookrelay.Run("Stop", strings.NewReader(sessionEnd), io.Discard, nil); got != 1 {
		t.Errorf("hook-relay Stop with partial harmonik wiring: exit code = %d, want 1", got)
	}

	mainFixtureSaveRestoreEnv(t, "HARMONIK_RUN_ID", "", true)
	if got := hookrelay.Run("Stop", strings.NewReader(sessionEnd), io.Discard, nil); got != 0 {
		t.Errorf("hook-relay Stop outside a harmonik session: exit code = %d, want 0", got)
	}
}

// TestWakeHelpDocumentsRealExitCodes covers `harmonik wake`.
//
// The bead recorded `wake --help` promising "0 sessions nudged" while the verb
// exited 0 and printed "nudged" for a name that matched nothing. The refusal
// landed separately; this test holds the pair together so the promise and the
// behaviour cannot drift apart again.
//
// Not parallel: swaps os.Stdout, and the refusal path reads the project dir.
func TestWakeHelpDocumentsRealExitCodes(t *testing.T) {
	help := jasulCaptureStdout(t, func() {
		if err := wakeUsage(); err != nil {
			t.Errorf("wakeUsage: %v", err)
		}
	})
	section := jasulExitCodeSection(help)
	if section == "" {
		t.Fatalf("wake --help has no EXIT CODES section:\n%s", help)
	}

	project := t.TempDir()

	cases := []jasulCase{
		{name: "no target given", args: []string{"--project", project}, want: 1},
		{name: "name that cannot key a session", args: []string{"--agent", "../../etc", "--project", project}, want: 1},
		{name: "well-formed name this project has no session for", args: []string{"--agent", "nosuchagent", "--project", project}, want: 1},
	}
	for _, tc := range cases {
		var code int
		_ = jasulCaptureStdout(t, func() { code = runWakeSubcommand(t.Context(), tc.args) })
		if code != tc.want {
			t.Errorf("wake %v (%s): exit code = %d, want %d", tc.args, tc.name, code, tc.want)
			continue
		}
		if !jasulDocumentsCode(section, code) {
			t.Errorf("wake %v really exits %d, but wake --help does not list %d:\n%s",
				tc.args, code, code, section)
		}
	}

	if strings.Contains(section, "sessions nudged") {
		t.Errorf("wake --help still promises %q for exit 0:\n%s", "sessions nudged", section)
	}
}

var jasulRequiredRelayEnvKeys = []string{
	"HARMONIK_RUN_ID", "HARMONIK_DAEMON_SOCKET", "HARMONIK_WORKSPACE_PATH",
	"HARMONIK_HANDLER_SESSION_ID", "HARMONIK_CLAUDE_SESSION_ID",
	"HARMONIK_WORKFLOW_ID", "HARMONIK_NODE_ID", "HARMONIK_AGENT_TYPE",
}

func jasulKindsHelpSaysAreSent(help string) map[string]bool {
	const marker = "kinds to the"
	i := strings.Index(help, marker)
	out := map[string]bool{}
	if i < 0 {
		return out
	}
	rest := help[i:]
	if j := strings.Index(rest, "."); j >= 0 {
		rest = rest[:j]
	}
	for _, kind := range []string{
		"SessionStart", "Stop", "StopFailure", "SessionEnd", "Notification",
	} {
		if regexp.MustCompile(`\b` + kind + `\b`).MatchString(rest) {
			out[kind] = true
		}
	}
	return out
}

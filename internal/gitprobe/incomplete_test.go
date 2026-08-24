package gitprobe_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/gitprobe"
)

// errKilledBySignal returns the error Go reports for a process that a signal
// stopped. It runs a real process rather than building an *exec.ExitError by
// hand, because the field the predicate reads is filled in by the kernel and a
// hand-made value would prove only that the test agrees with itself.
func errKilledBySignal(t *testing.T) error {
	t.Helper()
	err := exec.CommandContext(context.Background(), "sh", "-c", "kill -SEGV $$").Run()
	if err == nil {
		t.Fatal("a process that segfaults itself exited cleanly")
	}
	return err
}

// errExitStatus returns the error Go reports for a process that ran and chose a
// non-zero exit status — an answer, not a failure to produce one.
func errExitStatus(t *testing.T) error {
	t.Helper()
	err := exec.CommandContext(context.Background(), "sh", "-c", "exit 128").Run()
	if err == nil {
		t.Fatal("a process that exits 128 reported success")
	}
	return err
}

// errCancelledMidRun returns the context and the error Go reports when the
// caller's context ends while the process is still running.
func errCancelledMidRun(t *testing.T) (context.Context, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	t.Cleanup(cancel)
	err := exec.CommandContext(ctx, "sh", "-c", "sleep 5").Run()
	if err == nil {
		t.Fatal("a process its context killed exited cleanly")
	}
	return ctx, err
}

// TestProcessDidNotRun_ACancelledCallerIsAnAnswer holds the distinction the
// predicate exists to make, and it is not one the error can make on its own:
// the SAME error means "the machine stopped git" under a live context and "I
// stopped git myself" under a cancelled one.
func TestProcessDidNotRun_ACancelledCallerIsAnAnswer(t *testing.T) {
	t.Parallel()
	ctx, err := errCancelledMidRun(t)

	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("a context-killed process gave %T, not an *exec.ExitError; this test no longer tests what it says", err)
	}

	if gitprobe.ProcessDidNotRun(ctx, err) {
		t.Error("a git the caller cancelled was reported as a git that never ran")
	}
	if !gitprobe.ProcessDidNotRun(context.Background(), err) {
		t.Error("the same signal-killed process was not recognised under a live context")
	}
}

func TestProcessDidNotRun(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"no error at all", nil, false},
		{"git was killed by a signal", errKilledBySignal(t), true},
		{"git chose its own exit status", errExitStatus(t), false},
		{"the caller cancelled the context", context.Canceled, false},
		{"the caller's deadline passed", context.DeadlineExceeded, false},
		{"there is no git on this machine", fmt.Errorf("start: %w", exec.ErrNotFound), false},
		{"the directory is gone", fmt.Errorf("chdir: %w", os.ErrNotExist), false},
		{"the machine had no process slot", fmt.Errorf("fork/exec: %w", syscall.EAGAIN), true},
		{"the machine had no memory", fmt.Errorf("fork/exec: %w", syscall.ENOMEM), true},
		{"an error from somewhere else", errors.New("something else"), false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := gitprobe.ProcessDidNotRun(context.Background(), tc.err); got != tc.want {
				t.Errorf("ProcessDidNotRun(%v) = %v; want %v", tc.err, got, tc.want)
			}
		})
	}
}

// fakeBinOnPath puts a program named name on PATH that counts its own
// invocations and then does what behaviour says. It returns the path of the
// counter file.
//
// The stub is a whole-process PATH swap, which is the only seam this package's
// bare exec has. Build any repository the test needs BEFORE calling it.
func fakeBinOnPath(t *testing.T, name, behaviour string) string {
	t.Helper()

	binDir := t.TempDir()
	countPath := filepath.Join(t.TempDir(), "invocations")

	script := "#!/bin/sh\n" +
		"n=$(cat " + countPath + " 2>/dev/null || echo 0)\n" +
		"n=$((n+1))\n" +
		"echo $n > " + countPath + "\n" +
		behaviour + "\n"

	//nolint:gosec // G306: a stub program has to be executable to stand in for one
	if err := os.WriteFile(filepath.Join(binDir, name), []byte(script), 0o700); err != nil {
		t.Fatalf("write the fake %s: %v", name, err)
	}
	// binDir goes in FRONT of the real PATH rather than replacing it: the stub
	// is a shell script and it needs the ordinary commands to still be there.
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return countPath
}

// fakeGitOnPath is fakeBinOnPath for git, and it hands over to the real git when
// behaviour lets it run on. A behaviour that ends the script itself keeps the
// real git out of the test.
func fakeGitOnPath(t *testing.T, behaviour string) string {
	t.Helper()

	// Look the real git up BEFORE the PATH swap, or the stub finds itself.
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatalf("find the real git: %v", err)
	}
	return fakeBinOnPath(t, "git", behaviour+"\n"+"exec "+realGit+" \"$@\"")
}

func invocations(t *testing.T, countPath string) int {
	t.Helper()
	//nolint:gosec // G304: countPath is a path this test just made under t.TempDir()
	raw, err := os.ReadFile(countPath)
	if err != nil {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatalf("read the invocation count %q: %v", raw, err)
	}
	return n
}

// TestOutput_RunsGitAgainWhenASignalStopsIt is the defect this file exists for.
// A forked child that dies before git can report used to end a run.
func TestOutput_RunsGitAgainWhenASignalStopsIt(t *testing.T) {
	repo, headSHA := initGitRepo(t)
	countPath := fakeGitOnPath(t, `if [ "$n" -le 1 ]; then kill -SEGV $$; fi`)

	out, err := gitprobe.Output(t.Context(), repo, "rev-parse", "HEAD")
	if err != nil {
		t.Fatalf("Output after one killed git = %v; want the SHA", err)
	}
	if got := strings.TrimSpace(string(out)); got != headSHA {
		t.Errorf("Output = %q; want %q", got, headSHA)
	}
	if got := invocations(t, countPath); got != 2 {
		t.Errorf("git ran %d times; want 2 — one killed, one that answered", got)
	}
}

// TestOutput_ReportsAnExitStatusTheFirstTime keeps the fix narrow. An exit
// status is git's own answer, and running the command again would only make a
// caller wait longer for the same one.
func TestOutput_ReportsAnExitStatusTheFirstTime(t *testing.T) {
	repo, _ := initGitRepo(t)
	countPath := fakeGitOnPath(t, `exit 128`)

	if _, err := gitprobe.Output(t.Context(), repo, "rev-parse", "HEAD"); err == nil {
		t.Fatal("Output reported success for a git that exited 128")
	}
	if got := invocations(t, countPath); got != 1 {
		t.Errorf("git ran %d times; want 1 — an exit status is an answer", got)
	}
}

func TestOutput_GivesUpWhenGitIsStoppedEveryTime(t *testing.T) {
	repo, _ := initGitRepo(t)
	countPath := fakeGitOnPath(t, `kill -SEGV $$`)

	if _, err := gitprobe.Output(t.Context(), repo, "rev-parse", "HEAD"); err == nil {
		t.Fatal("Output reported success though git never ran")
	}
	if got := invocations(t, countPath); got != 3 {
		t.Errorf("git ran %d times; want 3 — the attempts are spent, then it reports", got)
	}
}

// TestOutput_DoesNotRetryForACallerThatAlreadyCancelled measures the retry
// loop, not the fork: a caller that cancelled first gets one refusal and no
// waiting. The mid-run case, where git IS forked and then killed, is decided by
// ProcessDidNotRun and is held by the test above.
func TestOutput_DoesNotRetryForACallerThatAlreadyCancelled(t *testing.T) {
	repo, _ := initGitRepo(t)
	countPath := fakeGitOnPath(t, `kill -SEGV $$`)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := gitprobe.Output(ctx, repo, "rev-parse", "HEAD"); err == nil {
		t.Fatal("Output reported success on a cancelled context")
	}
	if got := invocations(t, countPath); got > 1 {
		t.Errorf("git ran %d times for a caller that had already cancelled; want at most 1", got)
	}
}

// TestCombinedOutputStdin_GivesTheWholeInputToEveryAttempt is the reason the
// input is a string and not an io.Reader. A reader is spent by the attempt that
// read it, so the attempt after a dead child would run against nothing. git
// would then answer for an empty input and the caller would get a clean report
// that says the opposite of the truth.
func TestCombinedOutputStdin_GivesTheWholeInputToEveryAttempt(t *testing.T) {
	repo, _ := initGitRepo(t)
	seen := filepath.Join(t.TempDir(), "stdin-of-the-second-git")
	// The first git reads its input and is then stopped, which is the worst
	// case: the input is gone and no answer came back for it.
	countPath := fakeGitOnPath(t,
		`if [ "$n" -le 1 ]; then cat > /dev/null; kill -SEGV $$; fi`+"\n"+
			"cat > "+seen+"\n"+
			"exit 0")

	const input = "one.txt\ntwo.txt\nthree.txt\n"
	if _, err := gitprobe.CombinedOutputStdin(t.Context(), repo, input, "check-ignore", "--stdin"); err != nil {
		t.Fatalf("CombinedOutputStdin after one killed git = %v; want the second git's answer", err)
	}
	if got := invocations(t, countPath); got != 2 {
		t.Fatalf("git ran %d times; want 2 — one killed, one that answered", got)
	}

	//nolint:gosec // G304: seen is a path this test just made under t.TempDir()
	raw, err := os.ReadFile(seen)
	if err != nil {
		t.Fatalf("read the stdin the second git saw: %v", err)
	}
	if string(raw) != input {
		t.Errorf("the second git read %q on stdin; want the whole input %q", raw, input)
	}
}

// TestCombinedOutputStdin_FeedsTheInputToARealGit holds the plain case: the
// string reaches git, and it reaches it whole.
func TestCombinedOutputStdin_FeedsTheInputToARealGit(t *testing.T) {
	t.Parallel()
	repo, _ := initGitRepo(t)

	// git's object format fixes this SHA for the content "hello", so an empty
	// or a short stdin cannot produce it.
	const helloBlob = "b6fc4c620b67d95f953a5c1c1230aaab5db5a1b0"

	out, err := gitprobe.CombinedOutputStdin(t.Context(), repo, "hello", "hash-object", "--stdin")
	if err != nil {
		t.Fatalf("CombinedOutputStdin: %v\n%s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != helloBlob {
		t.Errorf("git hashed the input to %q; want %q, the blob SHA of \"hello\"", got, helloBlob)
	}
}

// TestCommandOutput_RunsANamedBinaryAgainWhenASignalStopsIt holds the point of
// the two Command functions: a child that never ran is a fork condition, and a
// caller that runs a program other than git meets the same one.
func TestCommandOutput_RunsANamedBinaryAgainWhenASignalStopsIt(t *testing.T) {
	dir := t.TempDir()
	countPath := fakeBinOnPath(t, "probe-stub", `if [ "$n" -le 1 ]; then kill -SEGV $$; fi`+"\necho ready")

	out, err := gitprobe.CommandOutput(t.Context(), dir, "probe-stub")
	if err != nil {
		t.Fatalf("CommandOutput after one killed child = %v; want the program's output", err)
	}
	if got := strings.TrimSpace(string(out)); got != "ready" {
		t.Errorf("CommandOutput = %q; want %q", got, "ready")
	}
	if got := invocations(t, countPath); got != 2 {
		t.Errorf("probe-stub ran %d times; want 2 — one killed, one that answered", got)
	}
}

// TestCommandOutput_ReportsANamedBinaryExitStatusTheFirstTime keeps the fix as
// narrow for a named program as it is for git. An exit status is the program's
// own answer and it is returned unchanged.
func TestCommandOutput_ReportsANamedBinaryExitStatusTheFirstTime(t *testing.T) {
	dir := t.TempDir()
	countPath := fakeBinOnPath(t, "probe-stub", "exit 7")

	_, err := gitprobe.CommandOutput(t.Context(), dir, "probe-stub")
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("CommandOutput for a program that exited 7 gave %v; want an exit status", err)
	}
	if exitErr.ExitCode() != 7 {
		t.Errorf("exit code %d; want 7 — the program's own answer, unchanged", exitErr.ExitCode())
	}
	if got := invocations(t, countPath); got != 1 {
		t.Errorf("probe-stub ran %d times; want 1 — an exit status is an answer", got)
	}
}

// TestCommandCombinedOutput_FoldsInStandardError separates the two Command
// functions: one hands back what the program said to a caller, the other hands
// back everything it said.
func TestCommandCombinedOutput_FoldsInStandardError(t *testing.T) {
	dir := t.TempDir()
	fakeBinOnPath(t, "probe-stub", "echo to-stdout\necho to-stderr >&2")

	combined, err := gitprobe.CommandCombinedOutput(t.Context(), dir, "probe-stub")
	if err != nil {
		t.Fatalf("CommandCombinedOutput: %v", err)
	}
	if !strings.Contains(string(combined), "to-stdout") || !strings.Contains(string(combined), "to-stderr") {
		t.Errorf("CommandCombinedOutput = %q; want both streams", combined)
	}

	plain, err := gitprobe.CommandOutput(t.Context(), dir, "probe-stub")
	if err != nil {
		t.Fatalf("CommandOutput: %v", err)
	}
	if strings.Contains(string(plain), "to-stderr") {
		t.Errorf("CommandOutput = %q; want standard output alone", plain)
	}
}

// TestCommandOutput_NamesTheProgramInTheLogLineWhenItRetries keeps the log line
// useful now that the command is not always git. A reader who finds this line
// has to be able to see which program failed.
func TestCommandOutput_NamesTheProgramInTheLogLineWhenItRetries(t *testing.T) {
	dir := t.TempDir()
	fakeBinOnPath(t, "probe-stub", `if [ "$n" -le 1 ]; then kill -SEGV $$; fi`+"\necho ready")
	logs := captureLogs(t)

	if _, err := gitprobe.CommandOutput(t.Context(), dir, "probe-stub", "--once"); err != nil {
		t.Fatalf("CommandOutput after one killed child = %v; want the program's output", err)
	}
	if got := logs.String(); !strings.Contains(got, `"command":"probe-stub --once"`) {
		t.Errorf("the retry log line does not name the program that failed:\n%s", got)
	}
}

// TestCombinedOutput_StillRunsGitAgainWhenASignalStopsIt holds the retry for
// CombinedOutput, which now reaches the attempt loop through CommandCombinedOutput.
func TestCombinedOutput_StillRunsGitAgainWhenASignalStopsIt(t *testing.T) {
	repo, headSHA := initGitRepo(t)
	countPath := fakeGitOnPath(t, `if [ "$n" -le 1 ]; then kill -SEGV $$; fi`)

	out, err := gitprobe.CombinedOutput(t.Context(), repo, "rev-parse", "HEAD")
	if err != nil {
		t.Fatalf("CombinedOutput after one killed git = %v\n%s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != headSHA {
		t.Errorf("CombinedOutput = %q; want %q", got, headSHA)
	}
	if got := invocations(t, countPath); got != 2 {
		t.Errorf("git ran %d times; want 2 — one killed, one that answered", got)
	}
}

// TestCombinedOutput_StillFoldsInGitsStandardError holds the other half of what
// CombinedOutput promises its callers: git's own words, which git writes on
// standard error.
func TestCombinedOutput_StillFoldsInGitsStandardError(t *testing.T) {
	t.Parallel()
	repo, _ := initGitRepo(t)

	out, err := gitprobe.CombinedOutput(t.Context(), repo, "cat-file", "-p", "no-such-ref")
	if err == nil {
		t.Fatalf("CombinedOutput reported success for a name git cannot resolve:\n%s", out)
	}
	if !strings.Contains(string(out), "Not a valid object name") {
		t.Errorf("CombinedOutput = %q; want git's own words", out)
	}
}

// logCapture collects log lines for a test to read. It holds a lock because the
// default logger is process-wide and another test in this package can reach it.
type logCapture struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (c *logCapture) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.Write(p)
}

func (c *logCapture) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.String()
}

// captureLogs points the default logger at a buffer for the length of the test.
func captureLogs(t *testing.T) *logCapture {
	t.Helper()
	c := &logCapture{}
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(c, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return c
}

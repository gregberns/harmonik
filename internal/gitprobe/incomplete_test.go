package gitprobe_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
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

// fakeGitOnPath puts a git on PATH that counts its own invocations and behaves
// as behaviour says before it hands over to the real git. It returns the path of
// the counter file.
//
// The stub is a whole-process PATH swap, which is the only seam this package's
// bare exec has. Build any repository the test needs BEFORE calling it.
func fakeGitOnPath(t *testing.T, behaviour string) string {
	t.Helper()

	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatalf("find the real git: %v", err)
	}
	binDir := t.TempDir()
	countPath := filepath.Join(t.TempDir(), "invocations")

	script := "#!/bin/sh\n" +
		"n=$(cat " + countPath + " 2>/dev/null || echo 0)\n" +
		"n=$((n+1))\n" +
		"echo $n > " + countPath + "\n" +
		behaviour + "\n" +
		"exec " + realGit + " \"$@\"\n"

	//nolint:gosec // G306: a fake git has to be executable to stand in for one
	if err := os.WriteFile(filepath.Join(binDir, "git"), []byte(script), 0o700); err != nil {
		t.Fatalf("write the fake git: %v", err)
	}
	// binDir goes in FRONT of the real PATH rather than replacing it: the stub
	// is a shell script and it needs the ordinary commands to still be there.
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return countPath
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

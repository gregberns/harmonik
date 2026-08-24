package gitprobe

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// A forked child can die before it ever runs git.
//
// The observable trigger today is the race detector: a child segfaults inside
// the sanitizer runtime between fork and exec, so the parent reaps a process
// that was killed by a signal without git having started. The exposure is not
// test-only — fork pressure, EAGAIN and an out-of-memory condition end a child
// the same way — and the daemon runs git on the dispatch and merge critical
// path of every run.
//
// The case this file repairs is a process that never ran. A signal that lands
// in the MIDDLE of a git that was doing work is a different thing: it can leave
// an index.lock or a ref lock behind, and the command that runs again then gets
// git's own exit 128 saying the lock is there. That answer is returned to the
// caller unchanged. Recovering a half-finished git is not attempted here.
//
// A process that never ran gave no answer. Reporting its failure as if git had
// spoken makes a transient condition terminal, which is what this file exists
// to stop. A git that DID run and exited non-zero is an answer and is returned
// unchanged, however unwelcome it is.
//
// Bead: hk-jbtj6.

// incompleteRetryDelays holds the wait before each attempt after the first, so
// a command runs len(incompleteRetryDelays)+1 times at most. The delays are
// short on purpose: this is not a busy remote to back off from, it is a local
// fork that has to be given room to succeed.
var incompleteRetryDelays = []time.Duration{50 * time.Millisecond, 200 * time.Millisecond}

// ProcessDidNotRun reports whether err means the git process never ran to
// completion, so no exit status of its own explains the failure.
//
// It is true in two cases:
//
//   - git was killed by a signal. It may never have reached its own main;
//     either way it was stopped before it could report.
//   - the command never started, and the reason it did not start is one that
//     can clear on its own: no process slot (EAGAIN) or no memory (ENOMEM).
//
// It is false for every exit status git chose for itself, including the exit 1
// that `git merge-base --is-ancestor` uses to mean "no". It is also false for a
// missing git binary and a missing directory: both are answers, and running the
// command again cannot change either.
//
// It takes ctx because a cancelled caller is the one case the error alone
// cannot describe. exec.CommandContext kills the child when the context ends,
// so what arrives here is a signal-killed process that reads exactly like a
// child the machine stopped. The context is the only thing that tells the two
// apart, and a caller that cancelled has its answer already.
func ProcessDidNotRun(ctx context.Context, err error) bool {
	if err == nil {
		return false
	}
	if ctx.Err() != nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if errors.Is(err, exec.ErrNotFound) || errors.Is(err, fs.ErrNotExist) {
		return false
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		status, ok := exitErr.Sys().(syscall.WaitStatus)
		return ok && status.Signaled()
	}
	return errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.ENOMEM)
}

// runAttempts runs attempt until it produces an answer, or until the attempts
// are spent. It re-runs only while ProcessDidNotRun is true of the error.
//
// Every re-run is logged, and so is a failure that outlives them all. Diagnosing
// this defect the first time needed macOS crash reports symbolized against a
// rebuilt test binary, because the daemon said nothing at all about its own
// failure. The log line is the part that stops that happening twice.
func runAttempts(ctx context.Context, command string, attempt func() ([]byte, error)) ([]byte, error) {
	out, err := attempt()
	for i, delay := range incompleteRetryDelays {
		if !ProcessDidNotRun(ctx, err) {
			return out, err
		}
		slog.WarnContext(ctx, "git_process_did_not_run",
			"subsystem", "gitprobe",
			"command", command,
			"attempt", i+1,
			"attempts", len(incompleteRetryDelays)+1,
			"probe_error", err.Error(),
		)
		// This select ends the WAIT early; it is not the cancellation check.
		// That is ProcessDidNotRun, which reads the context. Do not delete the
		// context branch of the predicate on the strength of this line.
		select {
		case <-ctx.Done():
			return out, err
		case <-time.After(delay):
		}
		out, err = attempt()
	}
	if ProcessDidNotRun(ctx, err) {
		slog.ErrorContext(ctx, "git_process_did_not_run_giving_up",
			"subsystem", "gitprobe",
			"command", command,
			"attempts", len(incompleteRetryDelays)+1,
			"probe_error", err.Error(),
		)
	}
	return out, err
}

// Output runs `git <args...>` in dir and returns its standard output. The
// command runs again while the git process does not run to completion.
//
// Use it for a command that is safe to run more than once: any read-only probe,
// and a write that sets an exact value. It is the wrong tool for a command whose
// second run would not mean what its first run meant — `git commit` is the one
// on this path, and its caller checks the repository state instead.
func Output(ctx context.Context, dir string, args ...string) ([]byte, error) {
	return runAttempts(ctx, gitCommandLine(args), func() ([]byte, error) {
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = dir
		return cmd.Output()
	})
}

// CombinedOutput is Output with git's standard error folded into the returned
// bytes, for the callers that put git's own words in a failure reason.
func CombinedOutput(ctx context.Context, dir string, args ...string) ([]byte, error) {
	return runAttempts(ctx, gitCommandLine(args), func() ([]byte, error) {
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = dir
		return cmd.CombinedOutput()
	})
}

// gitCommandLine renders args for a log line, so a reader sees which git call
// failed rather than only the package that made it.
func gitCommandLine(args []string) string {
	return "git " + strings.Join(args, " ")
}

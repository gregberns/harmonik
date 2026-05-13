package tmux

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// OSAdapter is the production implementation of [Adapter]. It shells out to the
// tmux binary via exec.CommandContext, mirroring the orphansweep.go pattern in
// the parent lifecycle package.
//
// OSAdapter requires tmux ≥ 3.0 for -e env-injection support. Callers MUST
// invoke [OSAdapter.ProbeTmux] at daemon startup before issuing any window
// operations.
//
// All methods are safe for concurrent use (each invocation spawns an independent
// tmux subprocess; no shared mutable state).
//
// Spec ref: process-lifecycle.md §4.5 PL-021b — direct-tmux substrate
// implementation for the MVH.
type OSAdapter struct{}

// ProbeTmux checks whether the tmux binary is present on PATH and meets the
// minimum version requirement (major ≥ 3, i.e. tmux ≥ 3.0 for -e env-injection).
//
// Returns [ErrTmuxMissing] when the tmux binary is absent from PATH.
// Returns [*ErrTmuxFailure] when `tmux -V` exits non-zero.
// Returns a plain error when the version string cannot be parsed.
//
// Spec ref: process-lifecycle.md §4.5 PL-021b obligation 2 — "The daemon MUST
// probe tmux at PL-005 step 4 (Cat 0 pre-check) by invoking `tmux -V` and
// asserting major version ≥ 3.0."
func (OSAdapter) ProbeTmux(ctx context.Context) error {
	//nolint:gosec // G204: arguments are hard-coded constants, not user input
	cmd := exec.CommandContext(ctx, "tmux", "-V")
	out, err := cmd.Output()
	if err != nil {
		// exec.LookPath failure means tmux is not on PATH.
		if isNotFoundErr(err) {
			return ErrTmuxMissing
		}
		stderr := extractStderr(err)
		return &ErrTmuxFailure{Op: "-V", ExitCode: exitCodeOf(err), Stderr: stderr}
	}

	// `tmux -V` prints "tmux <major>.<minor>[suffix]" e.g. "tmux 3.4".
	versionStr := strings.TrimSpace(string(out))
	major, err := parseTmuxMajorVersion(versionStr)
	if err != nil {
		return fmt.Errorf("tmux: ProbeTmux: %w", err)
	}
	if major < 3 {
		return fmt.Errorf("tmux: version %q is below required 3.0 (major = %d)", versionStr, major)
	}
	return nil
}

// ListSessions returns the names of all live tmux sessions. Returns (nil, nil)
// when tmux is not running or has no sessions — not an error.
//
// Spec ref: process-lifecycle.md §4.5 PL-021c — window-level orphan sweep
// enumerates all sessions first.
func (OSAdapter) ListSessions(ctx context.Context) ([]string, error) {
	//nolint:gosec // G204: arguments are hard-coded constants, not user input
	out, err := exec.CommandContext(ctx, "tmux", "list-sessions", "-F", "#{session_name}").Output()
	if err != nil {
		// tmux exits non-zero when no sessions exist or server not running.
		// Return empty list, not an error (mirrors OSTmuxSessionLister in parent package).
		return nil, nil //nolint:nilerr // intentional: no-tmux / no-sessions is not an error
	}
	return parseLines(out), nil
}

// ListWindows returns the names of all windows in the named session. Returns
// [ErrNoSession] when the session does not exist.
//
// Spec ref: process-lifecycle.md §4.5 PL-021c — window-level orphan sweep
// enumerates windows per session to match hk-<hash6>- prefix.
func (OSAdapter) ListWindows(ctx context.Context, session string) ([]string, error) {
	//nolint:gosec // G204: session is a validated harmonik-managed session name, not raw user input
	cmd := exec.CommandContext(ctx, "tmux", "list-windows", "-t", session, "-F", "#{window_name}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		if isNoSessionErr(out) {
			return nil, ErrNoSession
		}
		return nil, &ErrTmuxFailure{Op: "list-windows", ExitCode: exitCodeOf(err), Stderr: strings.TrimSpace(string(out))}
	}
	return parseLines(out), nil
}

// NewWindowIn creates a new named tmux window inside params.Session using
// params.WindowName. The new window runs params.Command (or the default shell
// when Command is empty) with the given environment variables and working
// directory.
//
// On success, returns an Outcome whose Handle is a "session:window-name" string
// in tmux notation, usable with [KillWindow] and [WindowPanePID].
//
// Returns [ErrNoSession] when params.Session does not exist.
// Returns [ErrWindowCollision] when a window named params.WindowName already
// exists in the session.
// Returns [*ErrTmuxFailure] on other tmux invocation errors.
//
// The tmux invocation is: tmux new-window -d -t <session>: -n <window> [-c <cwd>]
// [-e KEY=VAL...] -- [argv...]
//
// Spec ref: process-lifecycle.md §4.5 PL-021b obligation 1 — "the daemon MUST
// create the subprocess via `tmux new-window -d -t <session>: -n <window-name>
// -c <cwd> -e KEY=VALUE [...] -- <binary> <argv...>`."
func (OSAdapter) NewWindowIn(ctx context.Context, params NewWindowIn) Outcome {
	args := buildNewWindowArgs(params)
	//nolint:gosec // G204: args are constructed from validated caller-supplied parameters
	cmd := exec.CommandContext(ctx, "tmux", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		outStr := strings.TrimSpace(string(out))
		if isNoSessionErr(out) {
			return Outcome{Err: ErrNoSession}
		}
		if isWindowCollisionErr(out) {
			return Outcome{Err: ErrWindowCollision}
		}
		return Outcome{Err: &ErrTmuxFailure{Op: "new-window", ExitCode: exitCodeOf(err), Stderr: outStr}}
	}

	// Construct the handle as "session:window-name" per the opaque format
	// documented in adapter.go.
	handle := WindowHandle(params.Session + ":" + params.WindowName)
	return Outcome{Handle: handle}
}

// KillWindow destroys the window identified by handle. Returns nil if the
// window has already been destroyed (idempotent). Returns [ErrNoSession] when
// the session referenced by the handle does not exist.
//
// Spec ref: process-lifecycle.md §4.5 PL-021b obligation 7 — "The substrate
// Kill operation MUST issue `tmux kill-window`."
// Spec ref: process-lifecycle.md §4.5 PL-021c — orphan sweep calls KillWindow
// for each matched window.
func (OSAdapter) KillWindow(ctx context.Context, handle WindowHandle) error {
	target := string(handle)
	//nolint:gosec // G204: target is a WindowHandle constructed from validated session/window names
	cmd := exec.CommandContext(ctx, "tmux", "kill-window", "-t", target)
	out, err := cmd.CombinedOutput()
	if err != nil {
		outStr := strings.TrimSpace(string(out))
		if isNoSessionErr(out) {
			return ErrNoSession
		}
		// "no window:" indicates the window is already gone — idempotent success.
		if isNoWindowErr(out) {
			return nil
		}
		return &ErrTmuxFailure{Op: "kill-window", ExitCode: exitCodeOf(err), Stderr: outStr}
	}
	return nil
}

// WindowPanePID returns the PID of the process running in the first pane of
// the window identified by handle.
//
// Uses `tmux display-message -p -t <handle> '#{pane_pid}'`.
//
// Returns [ErrNoSession] when the session is gone.
// Returns [*ErrTmuxFailure] when display-message fails.
//
// Spec ref: process-lifecycle.md §4.5 PL-021b — pane PID retrieved immediately
// after new-window to populate WindowHandle.PID in the design; here it is
// available on demand per the adapter.go interface contract.
func (OSAdapter) WindowPanePID(ctx context.Context, handle WindowHandle) (int, error) {
	target := string(handle)
	//nolint:gosec // G204: target is a WindowHandle constructed from validated session/window names
	cmd := exec.CommandContext(ctx, "tmux", "display-message", "-p", "-t", target, "#{pane_pid}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		outStr := strings.TrimSpace(string(out))
		if isNoSessionErr(out) {
			return 0, ErrNoSession
		}
		return 0, &ErrTmuxFailure{Op: "display-message", ExitCode: exitCodeOf(err), Stderr: outStr}
	}

	pidStr := strings.TrimSpace(string(out))
	pid, parseErr := strconv.Atoi(pidStr)
	if parseErr != nil {
		return 0, fmt.Errorf("tmux: WindowPanePID: parse pane_pid %q: %w", pidStr, parseErr)
	}
	return pid, nil
}

// ──────────────────────────────────────────────────────────────────────────────
// Internal helpers
// ──────────────────────────────────────────────────────────────────────────────

// buildNewWindowArgs constructs the argument slice for `tmux new-window` from
// a [NewWindowIn]. It follows the command shape:
//
//	new-window -d -t <session>: -n <window> [-c <cwd>] [-e K=V...] [-- <argv...>]
func buildNewWindowArgs(p NewWindowIn) []string {
	args := []string{
		"new-window",
		"-d",                  // detached (don't switch to the new window)
		"-t", p.Session + ":", // target session; trailing colon selects last window
		"-n", p.WindowName,
	}
	if p.WorkDir != "" {
		args = append(args, "-c", p.WorkDir)
	}
	for _, kv := range p.Env {
		args = append(args, "-e", kv)
	}
	if p.Command != "" {
		args = append(args, "--", p.Command)
	}
	return args
}

// parseTmuxMajorVersion extracts the major version integer from a tmux -V
// output string of the form "tmux <major>.<minor>[suffix]".
func parseTmuxMajorVersion(versionStr string) (int, error) {
	// Expected format: "tmux 3.4" or "tmux 3.4a".
	parts := strings.Fields(versionStr)
	if len(parts) < 2 {
		return 0, fmt.Errorf("parseTmuxMajorVersion: unexpected output %q", versionStr)
	}
	verPart := parts[1] // e.g. "3.4" or "3.4a"
	dotIdx := strings.IndexByte(verPart, '.')
	majorStr := verPart
	if dotIdx >= 0 {
		majorStr = verPart[:dotIdx]
	}
	major, err := strconv.Atoi(majorStr)
	if err != nil {
		return 0, fmt.Errorf("parseTmuxMajorVersion: cannot parse major from %q: %w", versionStr, err)
	}
	return major, nil
}

// parseLines splits output on newlines and returns non-empty, trimmed lines.
func parseLines(out []byte) []string {
	var names []string
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			names = append(names, line)
		}
	}
	return names
}

// isNotFoundErr reports whether err from exec.Command indicates the binary was
// not found on PATH (exec.ErrNotFound or ENOENT).
func isNotFoundErr(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), exec.ErrNotFound.Error()) ||
		strings.Contains(err.Error(), "executable file not found")
}

// isNoSessionErr reports whether the combined output from a tmux command
// indicates the target session does not exist.
func isNoSessionErr(out []byte) bool {
	lower := strings.ToLower(strings.TrimSpace(string(out)))
	return strings.Contains(lower, "no server running") ||
		strings.Contains(lower, "can't find session") ||
		strings.Contains(lower, "session not found") ||
		strings.Contains(lower, "no such session")
}

// isWindowCollisionErr reports whether the combined output indicates a window
// with the requested name already exists.
func isWindowCollisionErr(out []byte) bool {
	lower := strings.ToLower(strings.TrimSpace(string(out)))
	return strings.Contains(lower, "duplicate window name") ||
		strings.Contains(lower, "already exists")
}

// isNoWindowErr reports whether the combined output indicates the target window
// does not exist (already gone — idempotent kill).
func isNoWindowErr(out []byte) bool {
	lower := strings.ToLower(strings.TrimSpace(string(out)))
	return strings.Contains(lower, "no window") ||
		strings.Contains(lower, "can't find window")
}

// exitCodeOf extracts the exit code from an *exec.ExitError, returning 1 for
// any other error type.
func exitCodeOf(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if asErr := asExitError(err); asErr != nil {
		exitErr = asErr
		return exitErr.ExitCode()
	}
	return 1
}

// asExitError returns *exec.ExitError from err if the underlying error is one,
// otherwise nil.
func asExitError(err error) *exec.ExitError {
	if err == nil {
		return nil
	}
	if ee, ok := err.(*exec.ExitError); ok { //nolint:errorlint // direct type assertion, not wrapping check
		return ee
	}
	return nil
}

// extractStderr returns stderr output from an *exec.ExitError, or the error
// message string for non-exit errors.
func extractStderr(err error) string {
	if ee := asExitError(err); ee != nil {
		return strings.TrimSpace(string(ee.Stderr))
	}
	if err != nil {
		return err.Error()
	}
	return ""
}

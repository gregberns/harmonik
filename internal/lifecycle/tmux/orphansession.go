package tmux

import (
	"context"
	"log"
	"strings"
	"syscall"

	"github.com/gregberns/harmonik/internal/core"
)

// sessionOrphanPrefix returns the harmonik session name prefix for the given
// project hash: "harmonik-<12-char-hash>-". Only sessions whose name has this
// exact prefix belong to this project's daemon runs.
//
// The full 12-char hash is used here (unlike the 6-char hk-<hash6>- window
// prefix) because session names are created with the full project hash via
// lifecycle.TmuxSessionName.
//
// Spec ref: process-lifecycle.md §4.2 PL-006a — "Scope tmux session names
// (harmonik-<project_hash>-<session_name>)."
func sessionOrphanPrefix(projectHash core.ProjectHash) string {
	return "harmonik-" + string(projectHash) + "-"
}

// SweepOrphanTmuxSessions enumerates all live tmux sessions, filters those
// whose name matches the "harmonik-<12-char-hash>-" prefix for the current
// projectHash, and kills any session that is orphaned. A session is considered
// orphaned if either:
//
//   - All of its windows are zsh shells (zero non-zsh windows), meaning the
//     workload that owned the session has already exited, OR
//   - The first pane of its first window reports a PID that is no longer alive
//     (kill(pid, 0) returns ESRCH).
//
// excludeSessions is an optional set of session names to skip regardless of
// orphan status. Used by the PL-006d coordinator sentinel exclusion — sessions
// with a live supervisor process must not be killed. Nil means no exclusions.
//
// Sessions for OTHER project hashes are completely untouched.
//
// If adapter is nil, a no-op sweep is performed (returns 0, nil).
// Non-fatal errors (TOCTOU ErrNoSession, WindowPanePID failures) are logged
// and skipped; the sweep continues with remaining sessions.
//
// Returns the count of sessions killed.
//
// Spec ref: process-lifecycle.md §4.2 PL-006 — session-level orphan sweep.
// Spec ref: process-lifecycle.md §4.2 PL-006d — coordinator sentinel exclusion.
// Bead: hk-kqdpf.3.
func SweepOrphanTmuxSessions(
	ctx context.Context,
	projectHash core.ProjectHash,
	adapter Adapter,
	logger *log.Logger,
	excludeSessions map[string]struct{},
) (killed int, err error) {
	if adapter == nil {
		return 0, nil
	}

	prefix := sessionOrphanPrefix(projectHash)

	sessions, listErr := adapter.ListSessions(ctx)
	if listErr != nil {
		return 0, &sessionSweepError{op: "ListSessions", cause: listErr}
	}

	for _, session := range sessions {
		if !strings.HasPrefix(session, prefix) {
			// Different project hash or non-harmonik session — skip entirely.
			continue
		}

		if _, skip := excludeSessions[session]; skip {
			sessionSweepLog(logger, "SweepOrphanTmuxSessions: skipping coordinator session %q (PL-006d exclusion)", session)
			continue
		}

		orphaned := sessionIsOrphaned(ctx, adapter, session, logger)
		if !orphaned {
			sessionSweepLog(logger, "SweepOrphanTmuxSessions: session %q is live; skipping", session)
			continue
		}

		sessionSweepLog(logger, "SweepOrphanTmuxSessions: killing orphan session %q", session)
		if killErr := adapter.KillSession(ctx, session); killErr != nil {
			// ErrNoSession means the session vanished between our check and the kill —
			// treat as already-gone (non-fatal TOCTOU).
			sessionSweepLog(logger, "SweepOrphanTmuxSessions: kill-session %q error (proceeding): %v", session, killErr)
			// Still count it: we identified it as orphaned; the error is diagnostic.
		}
		killed++
	}

	return killed, nil
}

// sessionIsOrphaned reports whether a harmonik session is safe to kill.
// A session is orphaned when either:
//  1. It has zero non-zsh windows (the workload process has exited, leaving
//     only an idle shell pane), OR
//  2. The first pane of the first window reports a PID that is dead
//     (kill(pid, 0) returns ESRCH — no such process).
//
// If both checks are inconclusive (e.g., empty window list or PID read error),
// the function returns false to avoid false-positive kills.
func sessionIsOrphaned(
	ctx context.Context,
	adapter Adapter,
	session string,
	logger *log.Logger,
) bool {
	windows, listErr := adapter.ListWindows(ctx, session)
	if listErr != nil {
		// TOCTOU or other error: can't determine status; don't kill.
		sessionSweepLog(logger, "SweepOrphanTmuxSessions: ListWindows(%q) error: %v (skipping session)", session, listErr)
		return false
	}

	// Condition 1: zero non-zsh windows.
	nonZsh := countNonZshWindows(windows)
	if nonZsh == 0 {
		sessionSweepLog(logger, "SweepOrphanTmuxSessions: session %q has %d window(s), all zsh — orphaned", session, len(windows))
		return true
	}

	// Condition 2: first pane PID is dead.
	// Use a handle of "session:" to target the first window's first pane.
	firstHandle := WindowHandle(session + ":")
	pid, pidErr := adapter.WindowPanePID(ctx, firstHandle)
	if pidErr != nil {
		// Can't read PID: don't kill.
		sessionSweepLog(logger, "SweepOrphanTmuxSessions: WindowPanePID(%q) error: %v (skipping session)", firstHandle, pidErr)
		return false
	}

	if pid <= 0 {
		// Invalid PID (0 or negative): treat as dead.
		sessionSweepLog(logger, "SweepOrphanTmuxSessions: session %q pane PID %d invalid — orphaned", session, pid)
		return true
	}

	// kill(pid, 0) probes liveness without sending a signal.
	if err := syscall.Kill(pid, 0); err != nil {
		// ESRCH = no such process; EPERM = exists but we don't have permission
		// (still alive). Only ESRCH means dead.
		if isESRCH(err) {
			sessionSweepLog(logger, "SweepOrphanTmuxSessions: session %q pane PID %d is dead — orphaned", session, pid)
			return true
		}
	}

	return false
}

// countNonZshWindows returns the number of window names in the slice that do
// not equal "zsh" (case-sensitive). A window named exactly "zsh" is treated as
// an idle shell pane that can be abandoned; any other name indicates an active
// workload window.
func countNonZshWindows(windows []string) int {
	count := 0
	for _, w := range windows {
		if w != "zsh" {
			count++
		}
	}
	return count
}

// isESRCH reports whether err is ESRCH (no such process).
func isESRCH(err error) bool {
	return err == syscall.ESRCH
}

// sessionSweepLog writes a formatted log message to logger if non-nil.
func sessionSweepLog(logger *log.Logger, format string, args ...any) {
	if logger == nil {
		return
	}
	logger.Printf(format, args...)
}

// sessionSweepError wraps an error from a named session-sweep step.
type sessionSweepError struct {
	op    string
	cause error
}

func (e *sessionSweepError) Error() string {
	return "tmux: SweepOrphanTmuxSessions: " + e.op + ": " + e.cause.Error()
}

func (e *sessionSweepError) Unwrap() error { return e.cause }

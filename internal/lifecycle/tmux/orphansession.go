package tmux

import (
	"context"
	"errors"
	"log"
	"strings"
	"syscall"

	"github.com/gregberns/harmonik/internal/core"
)

func sessionOrphanPrefix(projectHash core.ProjectHash) string {
	return "harmonik-" + string(projectHash) + "-"
}

// SweepOrphanTmuxSessions enumerates all live tmux sessions, filters those
// whose name matches the "harmonik-<12-char-hash>-" prefix for the current
// projectHash, and kills any session that is orphaned. A session is considered
// orphaned if either:
//
//   - All of its windows are idle shells (zero non-idle-shell windows — see
//     idleShellNames), meaning the
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
			sessionSweepLog(logger, "SweepOrphanTmuxSessions: kill-session %q error (proceeding): %v", session, killErr)
			continue
		}
		killed++
	}

	return killed, nil
}

func sessionIsOrphaned(
	ctx context.Context,
	adapter Adapter,
	session string,
	logger *log.Logger,
) bool {
	windows, listErr := adapter.ListWindows(ctx, session)
	if listErr != nil {
		sessionSweepLog(logger, "SweepOrphanTmuxSessions: ListWindows(%q) error: %v (skipping session)", session, listErr)
		return false
	}

	nonShell := countNonShellWindows(windows)
	if nonShell == 0 {
		sessionSweepLog(logger, "SweepOrphanTmuxSessions: session %q has %d window(s), all idle shells — orphaned", session, len(windows))
		return true
	}

	firstHandle := WindowHandle(session + ":")
	pid, pidErr := adapter.WindowPanePID(ctx, firstHandle)
	if pidErr != nil {
		sessionSweepLog(logger, "SweepOrphanTmuxSessions: WindowPanePID(%q) error: %v (skipping session)", firstHandle, pidErr)
		return false
	}

	if pid <= 0 {
		sessionSweepLog(logger, "SweepOrphanTmuxSessions: session %q pane PID %d invalid — orphaned", session, pid)
		return true
	}

	if err := syscall.Kill(pid, 0); err != nil {
		if isESRCH(err) {
			sessionSweepLog(logger, "SweepOrphanTmuxSessions: session %q pane PID %d is dead — orphaned", session, pid)
			return true
		}
	}

	return false
}

var idleShellNames = map[string]struct{}{
	"zsh":  {},
	"bash": {},
	"fish": {},
	"sh":   {},
	"dash": {},
	"ksh":  {},
	"tcsh": {},
	"csh":  {},
}

func countNonShellWindows(windows []string) int {
	count := 0
	for _, w := range windows {
		if _, isShell := idleShellNames[w]; !isShell {
			count++
		}
	}
	return count
}

func isESRCH(err error) bool {
	return errors.Is(err, syscall.ESRCH)
}

func sessionSweepLog(logger *log.Logger, format string, args ...any) {
	if logger == nil {
		return
	}
	logger.Printf(format, args...)
}

type sessionSweepError struct {
	op    string
	cause error
}

func (e *sessionSweepError) Error() string {
	return "tmux: SweepOrphanTmuxSessions: " + e.op + ": " + e.cause.Error()
}

func (e *sessionSweepError) Unwrap() error { return e.cause }

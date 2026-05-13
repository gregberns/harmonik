package tmux

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/gregberns/harmonik/internal/core"
)

// windowSweepPollInterval is the cadence at which SweepOrphanTmuxWindows polls
// for window exit after kill-window. Tests shorten this without changing the
// production call-site signature.
//
// Spec ref: process-lifecycle.md §4.7 PL-021c — "poll for window exit at a
// 100 ms cadence up to a 2-second ceiling."
var windowSweepPollInterval = 100 * time.Millisecond

// windowSweepPollCeiling is the maximum time SweepOrphanTmuxWindows waits
// after kill-window before proceeding. Configurable per OQ-PL-002.
var windowSweepPollCeiling = 2 * time.Second

// SweepOrphanTmuxWindows enumerates all live tmux sessions, lists windows in
// each, and kills any window whose name begins with "hk-<hash6>-" for the
// current project hash. After killing, it polls at 100 ms cadence up to a 2 s
// ceiling for the windows to disappear, then returns the count killed.
//
// If adapter is nil, a no-op sweep is performed (returns 0, nil). This
// mirrors the nil-guard pattern in parent-package sweep functions.
//
// Non-fatal errors (kill failure on a window that has already disappeared,
// ErrNoSession on a session that vanished between ListSessions and ListWindows)
// are logged and ignored, matching the SweepOrphanTmuxSessions behaviour.
//
// Spec ref: process-lifecycle.md §4.7 PL-021c — "The orphan sweep of PL-006
// MUST be extended to cover orphan tmux windows in addition to orphan tmux
// sessions. The extension is required because the PL-021b $TMUX-reuse mode
// places harmonik-created windows inside an operator-owned session whose name
// does NOT match the harmonik-<project_hash>- prefix that PL-006 enumerates."
func SweepOrphanTmuxWindows(
	ctx context.Context,
	projectHash core.ProjectHash,
	adapter Adapter,
	logger *log.Logger,
) (killed int, err error) {
	if adapter == nil {
		return 0, nil
	}

	prefix := windowOrphanPrefix(projectHash)

	sessions, err := adapter.ListSessions(ctx)
	if err != nil {
		return 0, wrapWindowSweepErr("ListSessions", err)
	}

	// Track (session, window) pairs that we issue kill-window for, so we can
	// poll afterwards.
	var targets []windowTarget

	for _, session := range sessions {
		windows, listErr := adapter.ListWindows(ctx, session)
		if listErr != nil {
			if errors.Is(listErr, ErrNoSession) {
				// TOCTOU: session vanished between ListSessions and ListWindows.
				windowSweepLog(logger, "SweepOrphanTmuxWindows: session %q disappeared before ListWindows; skipping", session)
				continue
			}
			windowSweepLog(logger, "SweepOrphanTmuxWindows: ListWindows(%q) error (proceeding): %v", session, listErr)
			continue
		}

		for _, window := range windows {
			if !strings.HasPrefix(window, prefix) {
				continue
			}
			handle := WindowHandle(session + ":" + window)
			windowSweepLog(logger, "SweepOrphanTmuxWindows: killing window %q in session %q", window, session)
			if killErr := adapter.KillWindow(ctx, handle); killErr != nil {
				if errors.Is(killErr, ErrNoSession) {
					// Session disappeared mid-sweep — treat as already-gone.
					windowSweepLog(logger, "SweepOrphanTmuxWindows: session %q gone during kill (proceeding)", session)
					continue
				}
				windowSweepLog(logger, "SweepOrphanTmuxWindows: kill-window %q error (proceeding): %v", handle, killErr)
				// Non-fatal: a window already gone returns nil from KillWindow
				// (idempotent). Any other error is logged and we continue.
			}
			killed++
			targets = append(targets, windowTarget{session: session, window: window})
		}
	}

	if killed == 0 {
		return 0, nil
	}

	// Poll for window exit at 100 ms cadence up to the ceiling.
	// Re-list windows per session and check whether any target windows remain.
	deadline := time.Now().Add(windowSweepPollCeiling)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			windowSweepLog(logger, "SweepOrphanTmuxWindows: context cancelled during poll; proceeding")
			return killed, nil
		case <-time.After(windowSweepPollInterval):
		}

		anyRemain := windowSweepAnyRemain(ctx, adapter, targets, prefix, logger)
		if !anyRemain {
			windowSweepLog(logger, "SweepOrphanTmuxWindows: all matching windows exited after kill")
			break
		}
	}

	return killed, nil
}

// windowOrphanPrefix returns "hk-<hash6>-" where hash6 is the first 6 hex
// chars of projectHash. This matches the sentinel prefix assigned by
// windowNameSentinelPrefix when ownsSession=false (PL-021b $TMUX-reuse mode).
//
// Spec ref: process-lifecycle.md §4.7 PL-021c — "every window whose name begins
// with hk-<hash6>- where hash6 is the first 6 hex chars of this daemon's
// project hash."
func windowOrphanPrefix(projectHash core.ProjectHash) string {
	h := string(projectHash)
	if len(h) >= projectHashPrefixLen {
		return "hk-" + h[:projectHashPrefixLen] + "-"
	}
	// Defensive: valid ProjectHash is always 12 chars. Use full hash if short.
	return "hk-" + h + "-"
}

// windowTarget holds a (session, window) pair for post-kill polling.
type windowTarget struct {
	session string
	window  string
}

// windowSweepAnyRemain reports whether any of the targets are still visible
// in their respective sessions. It groups targets by session to minimise
// tmux list-windows calls.
func windowSweepAnyRemain(
	ctx context.Context,
	adapter Adapter,
	targets []windowTarget,
	prefix string,
	logger *log.Logger,
) bool {
	// Build a set of sessions we need to check.
	sessionSet := make(map[string]struct{}, len(targets))
	for _, tgt := range targets {
		sessionSet[tgt.session] = struct{}{}
	}

	// Build a lookup of (session, window) pairs that we killed.
	type windowKey struct{ session, window string }
	killedSet := make(map[windowKey]struct{}, len(targets))
	for _, tgt := range targets {
		killedSet[windowKey{tgt.session, tgt.window}] = struct{}{}
	}

	_ = prefix // prefix was applied at kill time; here we check only known targets

	for session := range sessionSet {
		windows, err := adapter.ListWindows(ctx, session)
		if err != nil {
			// Session gone — all its targets have gone too.
			continue
		}
		for _, w := range windows {
			if _, ok := killedSet[windowKey{session, w}]; ok {
				windowSweepLog(logger, "SweepOrphanTmuxWindows: window %q still present in session %q", w, session)
				return true
			}
		}
	}
	return false
}

// wrapWindowSweepErr wraps an error from a named sweep step with a descriptive
// prefix for the tmux/orphanwindow package.
func wrapWindowSweepErr(op string, err error) error {
	if err == nil {
		return nil
	}
	// Use fmt.Errorf pattern — import kept minimal; see package imports.
	return &windowSweepError{op: op, cause: err}
}

type windowSweepError struct {
	op    string
	cause error
}

func (e *windowSweepError) Error() string {
	return "tmux: SweepOrphanTmuxWindows: " + e.op + ": " + e.cause.Error()
}

func (e *windowSweepError) Unwrap() error { return e.cause }

// windowSweepLog writes a formatted log message to logger if non-nil.
func windowSweepLog(logger *log.Logger, format string, args ...any) {
	if logger == nil {
		return
	}
	logger.Printf(format, args...)
}

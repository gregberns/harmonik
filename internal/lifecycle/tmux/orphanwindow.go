package tmux

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/gregberns/harmonik/internal/core"
)

var windowSweepPollInterval = 100 * time.Millisecond

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

	var targets []windowTarget

	for _, session := range sessions {
		windows, listErr := adapter.ListWindows(ctx, session)
		if listErr != nil {
			if errors.Is(listErr, ErrNoSession) {
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
					windowSweepLog(logger, "SweepOrphanTmuxWindows: session %q gone during kill (proceeding)", session)
					continue
				}
				windowSweepLog(logger, "SweepOrphanTmuxWindows: kill-window %q error (proceeding): %v", handle, killErr)
			}
			killed++
			targets = append(targets, windowTarget{session: session, window: window})
		}
	}

	if killed == 0 {
		return 0, nil
	}

	deadline := time.Now().Add(windowSweepPollCeiling)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			windowSweepLog(logger, "SweepOrphanTmuxWindows: context cancelled during poll; proceeding")
			return killed, nil
		case <-time.After(windowSweepPollInterval):
		}

		anyRemain := windowSweepAnyRemain(ctx, adapter, targets, logger)
		if !anyRemain {
			windowSweepLog(logger, "SweepOrphanTmuxWindows: all matching windows exited after kill")
			break
		}
	}

	return killed, nil
}

func windowOrphanPrefix(projectHash core.ProjectHash) string {
	h := string(projectHash)
	if len(h) >= projectHashPrefixLen {
		return "hk-" + h[:projectHashPrefixLen] + "-"
	}
	return "hk-" + h + "-"
}

type windowTarget struct {
	session string
	window  string
}

func windowSweepAnyRemain(
	ctx context.Context,
	adapter Adapter,
	targets []windowTarget,
	logger *log.Logger,
) bool {
	sessionSet := make(map[string]struct{}, len(targets))
	for _, tgt := range targets {
		sessionSet[tgt.session] = struct{}{}
	}

	type windowKey struct{ session, window string }
	killedSet := make(map[windowKey]struct{}, len(targets))
	for _, tgt := range targets {
		killedSet[windowKey(tgt)] = struct{}{}
	}

	for session := range sessionSet {
		windows, err := adapter.ListWindows(ctx, session)
		if err != nil {
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

func wrapWindowSweepErr(op string, err error) error {
	if err == nil {
		return nil
	}
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

func windowSweepLog(logger *log.Logger, format string, args ...any) {
	if logger == nil {
		return
	}
	logger.Printf(format, args...)
}

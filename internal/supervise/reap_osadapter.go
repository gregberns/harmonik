package supervise

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// osReapAdapter is the production ReapAdapter: it shells out to tmux.
type osReapAdapter struct{}

// OSReapAdapter returns the tmux-backed ReapAdapter used by the CLI verb and the
// boot auto-reap path. Absence is never an error: no tmux server, no sessions,
// and no tmux binary at all each yield an empty list. Only an unexpected tmux
// failure (e.g. permission denied) is reported.
func OSReapAdapter() ReapAdapter { return osReapAdapter{} }

// reapListSep is an unlikely-to-collide field separator for the list-sessions
// format string (tmux session names cannot contain it).
const reapListSep = "\x1f"

// ListFlywheelSessions runs `tmux list-sessions` with a format that yields, per
// session, the name, pane_dead state, and creation epoch, then filters to the
// flywheel family.
//
// Absence is a no-op, not an error: when tmux is not installed, or the server is
// not running (tmux exits non-zero with one of the tmuxServerAbsent messages),
// this returns (nil, nil) — there are no orphans to reap on such a host, and a
// `supervise reap` there must still exit 0. Any OTHER non-zero exit (permission
// denied, a malformed format string, a hung server) is returned as an error with
// tmux's own output attached, so a genuine failure is never mistaken for "clean".
func (osReapAdapter) ListFlywheelSessions(ctx context.Context) ([]FlywheelSession, error) {
	// #{pane_dead} is a window/pane attribute; list-sessions reports it for the
	// session's active pane, which is the flywheel shim's single pane. This is
	// sufficient: a flywheel session has exactly one window/pane (the shim).
	format := strings.Join([]string{
		"#{session_name}",
		"#{pane_dead}",
		"#{session_created}",
	}, reapListSep)

	//nolint:gosec // G204: arguments are hard-coded constants, not user input.
	out, err := exec.CommandContext(ctx, "tmux", "list-sessions", "-F", format).CombinedOutput()
	if err != nil {
		if tmuxUnavailable(err, out) {
			// Intentional nil error: tmux/server absence means "no orphans here".
			return nil, nil
		}
		return nil, fmt.Errorf("supervise: list flywheel sessions: %w (output: %s)", err, strings.TrimSpace(string(out)))
	}

	sessions := make([]FlywheelSession, 0, strings.Count(string(out), "\n")+1)
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Split(line, reapListSep)
		if len(fields) < 3 {
			continue
		}
		name := strings.TrimSpace(fields[0])
		if !IsFlywheelOrphanName(name) {
			continue
		}
		sessions = append(sessions, FlywheelSession{
			Name:     name,
			PaneDead: strings.TrimSpace(fields[1]) == "1",
			Created:  parseSessionCreated(fields[2]),
		})
	}
	return sessions, nil
}

// KillSession runs `tmux kill-session -t =<name>`. The "=" anchor defeats tmux
// prefix/fuzzy matching so the kill targets ONLY the exact session.
//
// The target being already gone is a no-op: between the list and the kill either
// the session alone can disappear (tmux: "can't find session: <name>") or the
// whole tmux server can exit (tmux: "no server running on <socket>", or "error
// connecting to <socket> (No such file or directory)" once the socket is gone
// too). Both are the benign TOCTOU race the reaper is designed for — the orphan
// we wanted dead is dead. Every OTHER failure (permission denied, a wedged
// server) IS returned, and the reaper then refuses to count that session as
// reaped.
func (osReapAdapter) KillSession(ctx context.Context, name string) error {
	//nolint:gosec // G204: name is validated by IsFlywheelOrphanName before any kill.
	out, err := exec.CommandContext(ctx, "tmux", "kill-session", "-t", "="+name).CombinedOutput()
	if err != nil && !tmuxSessionAlreadyGone(out) && !tmuxServerAbsent(out) {
		return fmt.Errorf("supervise: kill tmux session %q: %w (output: %s)", name, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// tmuxUnavailable reports whether a failed tmux invocation failed only because
// tmux itself is unreachable — the binary is not installed (exec.ErrNotFound,
// detected structurally rather than by string match) or no server is running.
// os/exec.LookPath also reports a present-but-non-executable tmux as
// ErrNotFound, so that broken install reads as "absent" too.
func tmuxUnavailable(err error, out []byte) bool {
	return errors.Is(err, exec.ErrNotFound) || tmuxServerAbsent(out)
}

// tmuxServerAbsent reports whether tmux's output says the server this command
// targeted is not there. Measured against tmux 3.6a:
//
//	$ tmux -L gone kill-session -t =nosuch   # socket file present, server exited
//	no server running on /private/tmp/tmux-502/gone
//	$ tmux -L never list-sessions            # socket never existed
//	error connecting to /private/tmp/tmux-502/never (No such file or directory)
//
// Which one you get depends only on whether the socket file survived the
// server's exit, so both must be tolerated. A different connect errno (e.g.
// "(Permission denied)") is NOT absence and stays an error.
func tmuxServerAbsent(out []byte) bool {
	s := strings.ToLower(string(out))
	if strings.Contains(s, "no server running") {
		return true
	}
	return strings.Contains(s, "error connecting to") && strings.Contains(s, "no such file or directory")
}

// tmuxSessionAlreadyGone reports whether tmux's output says the exact session we
// targeted no longer exists ("can't find session: <name>") — the server is up,
// the orphan is already dead.
func tmuxSessionAlreadyGone(out []byte) bool {
	return strings.Contains(strings.ToLower(string(out)), "can't find session")
}

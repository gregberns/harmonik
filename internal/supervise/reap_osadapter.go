package supervise

import (
	"context"
	"os/exec"
	"strings"
)

// osReapAdapter is the production ReapAdapter: it shells out to tmux.
type osReapAdapter struct{}

// OSReapAdapter returns the tmux-backed ReapAdapter used by the CLI verb and the
// boot auto-reap path. It is safe when no tmux server is running (returns an
// empty list, not an error).
func OSReapAdapter() ReapAdapter { return osReapAdapter{} }

// reapListSep is an unlikely-to-collide field separator for the list-sessions
// format string (tmux session names cannot contain it).
const reapListSep = "\x1f"

// ListFlywheelSessions runs `tmux list-sessions` with a format that yields, per
// session, the name, pane_dead state, and creation epoch, then filters to the
// flywheel family. When the tmux server is absent or has no sessions, tmux exits
// non-zero and we return (nil, nil) — a no-op, mirroring OSAdapter.ListSessions.
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
	out, err := exec.CommandContext(ctx, "tmux", "list-sessions", "-F", format).Output()
	if err != nil {
		// No server / no sessions / tmux missing: treat as no-op (no orphans).
		return nil, nil //nolint:nilerr // intentional: absence is not an error.
	}

	var sessions []FlywheelSession
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
// prefix/fuzzy matching so the kill targets ONLY the exact session. Killing an
// absent session is a no-op (we swallow the error).
func (osReapAdapter) KillSession(ctx context.Context, name string) error {
	//nolint:gosec // G204: name is validated by IsFlywheelOrphanName before any kill.
	_ = exec.CommandContext(ctx, "tmux", "kill-session", "-t", "="+name).Run()
	return nil
}

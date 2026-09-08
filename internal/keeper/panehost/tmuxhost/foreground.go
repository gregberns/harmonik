package tmuxhost

import (
	"context"
	"os/exec"
	"strings"

	"github.com/gregberns/harmonik/internal/keeper/panehost"
)

var shellCmds = map[string]struct{}{
	"zsh":  {},
	"bash": {},
	"sh":   {},
	"fish": {},
	"dash": {},
	"csh":  {},
	"tcsh": {},
}

// foregroundState queries #{pane_current_command} ONCE and classifies it —
// the implementation behind Host.Foreground, which collapses the pre-KH-1
// IsPaneIdle+IsPaneAlive pair (documented mutually exclusive) into one enum
// (PRINCIPLES §2) instead of running the tmux probe twice. A non-shell
// command is ForegroundAgent (the managed agent, possibly hung mid-turn); a
// known shell is ForegroundShell (the agent has exited); a query error or
// empty result is ForegroundUnknown (fail-closed: neither the respawn gate
// nor live-pane recovery fires on Unknown).
func foregroundState(ctx context.Context, target string) panehost.ForegroundState {
	cmd := exec.CommandContext(ctx, "tmux", "display-message", "-t", target, "-p", "#{pane_current_command}")
	out, err := cmd.Output()
	if err != nil {
		return panehost.ForegroundUnknown
	}
	cur := strings.TrimSpace(string(out))
	if cur == "" {
		return panehost.ForegroundUnknown
	}
	if _, isShell := shellCmds[cur]; isShell {
		return panehost.ForegroundShell
	}
	return panehost.ForegroundAgent
}

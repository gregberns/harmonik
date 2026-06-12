package keeper

import (
	"context"
	"os/exec"
	"strings"
)

// shellCmds is the set of process names that indicate a tmux pane is idle
// (running a shell, not the managed agent). When pane_current_command matches
// any of these, the pane is considered available for respawn.
var shellCmds = map[string]struct{}{
	"zsh":  {},
	"bash": {},
	"sh":   {},
	"fish": {},
	"dash": {},
	"csh":  {},
	"tcsh": {},
}

// IsPaneIdle reports whether the tmux pane at target is running a shell
// (indicating the managed agent has exited). It uses `tmux display-message` to
// query #{pane_current_command}. Returns false on any tmux error so that a
// transient query failure never triggers an unintended respawn.
func IsPaneIdle(ctx context.Context, target string) bool {
	cmd := exec.CommandContext(ctx, "tmux", "display-message", "-t", target, "-p", "#{pane_current_command}")
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	cur := strings.TrimSpace(string(out))
	_, ok := shellCmds[cur]
	return ok
}

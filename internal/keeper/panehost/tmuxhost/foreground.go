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

// IsPaneAlive reports whether the tmux pane at target is running a NON-shell
// command — i.e. the managed agent process is still present (hung mid-turn, not
// exited). It is the gating signal for live-pane recovery (hk-75mr): a stale
// gauge over an ALIVE pane is the hung-agent case that the idle-respawn path
// (IsPaneIdle) does NOT cover and a /clear inject cannot reach.
//
// It queries #{pane_current_command} via `tmux display-message`. It returns
// false (fail-closed: do NOT force-restart) on ANY tmux error or an empty
// result, so a transient query failure never triggers an unintended restart.
// A non-empty command that is not a known shell counts as alive. IsPaneAlive
// and IsPaneIdle are mutually exclusive for any successful query.
func IsPaneAlive(ctx context.Context, target string) bool {
	cmd := exec.CommandContext(ctx, "tmux", "display-message", "-t", target, "-p", "#{pane_current_command}")
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	cur := strings.TrimSpace(string(out))
	if cur == "" {
		return false
	}
	_, isShell := shellCmds[cur]
	return !isShell
}

// foregroundState queries #{pane_current_command} ONCE and classifies it —
// the single-query implementation behind Host.Foreground, which collapses
// IsPaneIdle+IsPaneAlive (documented mutually exclusive) into one enum
// (PRINCIPLES §2) instead of running the tmux probe twice.
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

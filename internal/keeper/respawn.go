package keeper

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// ErrLiveRecoverIdentityUntrusted is returned by the LiveRecoverFn built by
// NewLiveRecoverViaRespawn when the bound .sid identity is absent or not a valid
// UUIDv4 — the closure refuses to force-restart an agent whose identity it
// cannot trust (fail-closed). Refs: hk-75mr, hk-8prq.
var ErrLiveRecoverIdentityUntrusted = errors.New("keeper: live-pane recovery refused — bound .sid identity absent or not a valid UUIDv4")

// NewLiveRecoverViaRespawn builds the gated ForceRestart action wired into
// WatcherConfig.LiveRecoverFn for the standalone keeper (hk-75mr). The action
// force-restarts the agent by running respawnCmd via `sh -c` — the same
// operator-supplied launch command the idle-respawn path uses; that script is
// responsible for killing the hung pane and re-launching (e.g. captain-launch.sh
// does `tmux kill-session … ; tmux new-session … 'claude …'`).
//
// The closure REFUSES (returns ErrLiveRecoverIdentityUntrusted, no restart) when
// the bound .sid identity is not a valid UUIDv4. This is defense-in-depth atop
// the watcher's own identity gate (maybeLivePaneRecover): a force-restart is the
// most destructive keeper action, so the action itself re-verifies identity at
// the moment of firing rather than trusting the caller.
//
// Returns nil when respawnCmd is empty — with no launch command there is no
// recovery action to wire, so live-pane recovery stays DISABLED (fail-closed).
// Refs: hk-75mr, hk-8prq.
func NewLiveRecoverViaRespawn(projectDir, respawnCmd string) func(ctx context.Context, agentName string) error {
	if respawnCmd == "" {
		return nil
	}
	return func(ctx context.Context, agentName string) error {
		sid, _, err := ReadSessionIDFile(projectDir, agentName)
		if err != nil || !isPrimarySID(sid) {
			if err != nil {
				return fmt.Errorf("%w (agent %q): %v", ErrLiveRecoverIdentityUntrusted, agentName, err)
			}
			return fmt.Errorf("%w (agent %q, sid=%q)", ErrLiveRecoverIdentityUntrusted, agentName, sid)
		}
		//nolint:gosec // G204: respawnCmd is operator-supplied via --respawn-cmd, not user input.
		cmd := exec.CommandContext(ctx, "sh", "-c", respawnCmd)
		return cmd.Run()
	}
}

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

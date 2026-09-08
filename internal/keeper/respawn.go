package keeper

import (
	"context"
	"errors"
	"fmt"
	"os/exec"

	"github.com/gregberns/harmonik/internal/keeper/panehost/tmuxhost"
)

// ErrLiveRecoverIdentityUntrusted is returned by the LiveRecoverFn built by
// NewLiveRecoverViaRespawn when the bound .sid identity is absent or not a valid
// UUIDv4 — the closure refuses to force-restart an agent whose identity it
// cannot trust (fail-closed). Refs: hk-75mr, hk-8prq.
var ErrLiveRecoverIdentityUntrusted = errors.New("keeper: live-pane recovery refused — bound .sid identity absent or not a valid UUIDv4")

// NewLiveRecoverViaRespawn builds the gated ForceRestart action wired into
// WatcherConfig.LiveRecoverFn for the standalone keeper (hk-75mr). The action
// force-restarts the agent by running respawnCmd via `sh -c` — the same
// operator-supplied launch command the idle-respawn path uses; that command is
// responsible for killing the hung pane and re-launching (e.g.
// `harmonik captain respawn …` does `tmux kill-session … ; tmux new-session … 'claude …'`).
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
				return fmt.Errorf("%w (agent %q): %w", ErrLiveRecoverIdentityUntrusted, agentName, err)
			}
			return fmt.Errorf("%w (agent %q, sid=%q)", ErrLiveRecoverIdentityUntrusted, agentName, sid)
		}
		cmd := exec.CommandContext(ctx, "sh", "-c", respawnCmd)
		return cmd.Run()
	}
}

// IsPaneIdle is a back-compat wrapper over tmuxhost.IsPaneIdle (KH-1: the
// pane-foreground probes moved to panehost/tmuxhost, where they also back the
// unified panehost.PaneHost.Foreground). See tmuxhost.IsPaneIdle for the full
// doc.
func IsPaneIdle(ctx context.Context, target string) bool {
	return tmuxhost.IsPaneIdle(ctx, target)
}

// IsPaneAlive is a back-compat wrapper over tmuxhost.IsPaneAlive.
func IsPaneAlive(ctx context.Context, target string) bool {
	return tmuxhost.IsPaneAlive(ctx, target)
}
